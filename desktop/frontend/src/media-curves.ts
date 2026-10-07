import type { MediaCurvePage, MediaCurvePoint, MediaCurveRate, MediaCurveRequest, MediaCurveResponse, SummaryOverview } from './protocol.ts';
import { unsigned } from './numbers.ts';

export const CURVE_PAGE_SIZE = 64;
export const CURVE_MAX_POINTS = 128;
export const CURVE_WINDOW_TAGS = 256;
export type CurveMetric = 'audio' | 'video' | 'mp3' | 'gopAverage' | 'gopMax';
export const curveMetricNames: Record<CurveMetric, string> = { audio: '音频码率', video: '视频码率', mp3: 'MP3 码率', gopAverage: 'GOP 平均时长', gopMax: 'GOP 最长时长' };
function exact(value: string): bigint {
  if (typeof value !== 'string') throw Error('Curve integer must be an exact decimal string.');
  return unsigned(value);
}
function tag(value: string | null, start: bigint, next: bigint): void {
  if (value === null || exact(value) < start || exact(value) >= next) throw Error('Invalid curve closing Tag.');
}
function rate(value: MediaCurveRate, start: bigint, next: bigint): void {
  const bytes = exact(value.coveredBytes), duration = exact(value.coveredIntervalMs);
  if (duration === 0n) {
    if (bytes !== 0n || value.bitsPerSecond !== null || value.tagId !== null) throw Error('Invalid unknown curve rate.');
  } else {
    if (value.bitsPerSecond === null || exact(value.bitsPerSecond) !== bytes * 8000n / duration) throw Error('Invalid curve rate.');
    tag(value.tagId, start, next);
  }
}
export function validateMediaCurvePage(page: MediaCurvePage, request: MediaCurveRequest, summary?: SummaryOverview): void {
  const start = exact(page.bounds.start), next = exact(page.bounds.next), total = exact(page.bounds.total), indexed = exact(page.indexedTags);
  const count = request.count === 0 ? CURVE_PAGE_SIZE : request.count;
  if (!Number.isInteger(count) || count < 1 || count > CURVE_MAX_POINTS || page.bounds.limit !== count || page.bounds.start !== request.start ||
      page.windowTags !== CURVE_WINDOW_TAGS || typeof page.scanComplete !== 'boolean' || typeof page.complete !== 'boolean' || total > 3907n ||
      total !== (indexed + 255n) / 256n || start > total || next !== start + (total - start < BigInt(count) ? total - start : BigInt(count)) ||
      !Array.isArray(page.points) || page.points.length > count || BigInt(page.points.length) !== next - start) throw Error('Invalid curve bounds.');
  if (summary && (page.indexedTags !== summary.indexedTags || page.scanComplete !== summary.scanComplete || page.complete !== summary.complete)) throw Error('Invalid curve summary coverage.');
  for (let i = 0; i < page.points.length; i++) {
    const point = page.points[i], index = exact(point.index), first = exact(point.startTag), end = exact(point.nextTag);
    if (index !== start + BigInt(i) || first !== index * 256n || end !== (first + 256n < indexed ? first + 256n : indexed) || exact(point.unclassifiedTags) > end - first) throw Error('Invalid curve point.');
    rate(point.audio, first, end); rate(point.video, first, end); rate(point.mp3, first, end);
    const gop = point.gop, declared = exact(gop.declaredCount), closed = exact(gop.closedCount), duration = exact(gop.totalDurationMs);
    if (declared > end - first || closed > end - first) throw Error('Invalid curve GOP count.');
    if (closed === 0n) {
      if (duration !== 0n || gop.minDurationMs !== null || gop.maxDurationMs !== null || gop.averageDurationMs !== null || gop.tagId !== null) throw Error('Invalid unknown GOP duration.');
    } else {
      if (gop.minDurationMs === null || gop.maxDurationMs === null || gop.averageDurationMs === null) throw Error('Missing GOP duration.');
      const min = exact(gop.minDurationMs), max = exact(gop.maxDurationMs), average = exact(gop.averageDurationMs);
      if (min === 0n || min > average || average > max || average !== duration / closed || duration < min * closed || duration > max * closed) throw Error('Invalid GOP duration.');
      tag(gop.tagId, first, end);
    }
  }
  if (new TextEncoder().encode(JSON.stringify(page)).length > 256 * 1024) throw Error('Curve page exceeds its budget.');
}
export function validateMediaCurveResponse(response: MediaCurveResponse, request: MediaCurveRequest, summary?: SummaryOverview): void {
  if (response.sessionId !== request.sessionId || response.requestId !== request.requestId) throw Error('Invalid curve identity.');
  if (response.error !== null) {
    if (!response.error || typeof response.error.code !== 'string' || !response.error.code || typeof response.error.message !== 'string' || response.page !== null) throw Error('Invalid curve failure.');
  } else {
    if (!response.page) throw Error('Missing curve page.');
    validateMediaCurvePage(response.page, request, summary);
  }
}
export function curveValue(point: MediaCurvePoint, metric: CurveMetric): bigint | null {
  const value = metric === 'gopAverage' ? point.gop.averageDurationMs : metric === 'gopMax' ? point.gop.maxDurationMs : point[metric].bitsPerSecond;
  return value === null ? null : exact(value);
}
export function curveTag(point: MediaCurvePoint, metric: CurveMetric): string | null {
  return metric === 'gopAverage' || metric === 'gopMax' ? point.gop.tagId : point[metric].tagId;
}
export function curveDomain(points: MediaCurvePoint[], metric: CurveMetric): bigint {
  let max = 1n;
  for (const point of points) { const value = curveValue(point, metric); if (value !== null && value > max) max = value; }
  return max;
}
// Only bounded plot ratios become Number; source metrics and labels stay bigint/string.
export function curveY(value: bigint, max: bigint): number { return 200 - Number(value * 1000000n / max) / 1000000 * 170; }
