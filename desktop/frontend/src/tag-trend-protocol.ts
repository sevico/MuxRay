import type { PageRequest, TagTrendPage, TagTrendPoint } from './protocol.ts';
import { unsigned } from './numbers.ts';

export const TREND_PAGE_SIZE = 256;
export const TREND_MAX_POINTS = 512;
export const TREND_CONTEXT_LIMIT = 4096;
const maxDts = 4294967295n;
const maxOffset = 18446744073709551615n;

function coordinate(value: string): bigint {
  if (typeof value !== 'string') throw Error('Invalid exact coordinate.');
  return unsigned(value);
}
function rawDts(value: string): bigint {
  if (typeof value !== 'string') throw Error('Invalid raw DTS.');
  const parsed = unsigned(value);
  if (parsed > maxDts) throw Error('Raw DTS exceeds uint32.');
  return parsed;
}
function scalar(value: number, maximum: number): void {
  if (!Number.isInteger(value) || value < 0 || value > maximum) throw Error('Invalid scalar.');
}
function signedDelta(value: string): bigint {
  if (typeof value !== 'string' || !/^(0|-?[1-9][0-9]{0,9})$/.test(value)) throw Error('Invalid DTS interval.');
  const parsed = BigInt(value);
  if (parsed < -maxDts || parsed > maxDts) throw Error('DTS interval exceeds raw range.');
  return parsed;
}

// Source coordinates remain bigint. Only bounded point counts and raw FLV
// scalars become numbers, so a window beyond JS's safe integer stays exact.
export function validateTagTrendPage(page: TagTrendPage, request: PageRequest, sourceSize?: string): void {
  const { bounds } = page;
  const start = coordinate(bounds.start), next = coordinate(bounds.next), total = coordinate(bounds.total);
  scalar(bounds.limit, TREND_MAX_POINTS);
  scalar(page.contextScanned, TREND_CONTEXT_LIMIT);
  const contextStart = coordinate(page.contextStart);
  if (page.sessionId !== request.sessionId || bounds.start !== request.start || bounds.limit !== request.count || bounds.limit === 0 ||
      start > total || next !== start + (total - start < BigInt(bounds.limit) ? total - start : BigInt(bounds.limit)) ||
      contextStart > start || start - contextStart !== BigInt(page.contextScanned) || !Array.isArray(page.points) ||
      BigInt(page.points.length) !== next - start || page.points.length > TREND_MAX_POINTS ||
      (page.points.length === 0 && page.contextScanned !== 0)) throw Error('Invalid trend bounds.');
  const size = sourceSize === undefined ? maxOffset : coordinate(sourceSize);
  const tracks = new Map<string, TagTrendPoint>();
  let previousOffset = -1n;
  for (let i = 0; i < page.points.length; i++) {
    const point = page.points[i], id = coordinate(point.tagId), offset = coordinate(point.offset), dts = rawDts(point.dts);
    scalar(point.typeCode, 31); scalar(point.streamId, 0xffffff); scalar(point.dataSize, 0xffffff); scalar(point.availableDataSize, point.dataSize);
    if (id !== start + BigInt(i) || offset <= previousOffset || offset + 11n + BigInt(point.availableDataSize) > size ||
        typeof point.payloadComplete !== 'boolean' || point.payloadComplete !== (point.availableDataSize === point.dataSize) || typeof point.filtered !== 'boolean' ||
        !['valid', 'missing', 'invalid'].includes(point.previousTagSizeState)) throw Error('Invalid trend point.');
    previousOffset = offset;
    const key = `${point.typeCode}:${point.streamId}`, prior = tracks.get(key);
    if (point.context === 'found') {
      if (point.previousTagId === null || point.previousDts === null || point.deltaMs === null) throw Error('Missing trend predecessor.');
      const priorId = coordinate(point.previousTagId), priorDts = rawDts(point.previousDts);
      if (priorId < contextStart || priorId >= id || signedDelta(point.deltaMs) !== dts - priorDts ||
          (prior ? point.previousTagId !== prior.tagId || point.previousDts !== prior.dts : priorId >= start)) throw Error('Invalid trend predecessor.');
    } else {
      if (prior || point.previousTagId !== null || point.previousDts !== null || point.deltaMs !== null ||
          (point.context !== 'start' && point.context !== 'budget_exhausted') ||
          (point.context === 'start' ? contextStart !== 0n : contextStart === 0n || page.contextScanned !== TREND_CONTEXT_LIMIT)) throw Error('Invalid missing trend context.');
    }
    tracks.set(key, point);
  }
}
