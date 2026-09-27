import type { TagTrendPoint } from './protocol.ts';
import { unsigned } from './numbers.ts';

export type TrendMetric = 'dts' | 'size' | 'delta';
export type TrendType = 'all' | 'audio' | 'video' | 'script' | 'other';
export type TrendIssue = 'regression' | 'duplicate' | 'gap' | 'payload' | 'previous_missing' | 'previous_invalid';
export const TREND_GAP_DEFAULT = '1000';
export const TREND_ISSUE_PAGE_SIZE = 20;
export const trendIssueNames: Record<TrendIssue, string> = {
  regression: 'DTS 回退', duplicate: 'DTS 重复', gap: 'DTS 大间隔', payload: 'Payload 截断',
  previous_missing: 'PreviousTagSize 缺失', previous_invalid: 'PreviousTagSize 不一致',
};
export const trendTypeNames: Record<TrendType, string> = { all: '全部类型', audio: '音频', video: '视频', script: '脚本', other: '其他' };
export function trendType(point: TagTrendPoint): Exclude<TrendType, 'all'> {
  return point.typeCode === 8 ? 'audio' : point.typeCode === 9 ? 'video' : point.typeCode === 18 ? 'script' : 'other';
}
export function trendGap(text: string): bigint {
  const value = unsigned(text);
  if (value === 0n || value > 4294967295n) throw Error('Gap must be in 1..4294967295.');
  return value;
}
export function trendIssues(point: TagTrendPoint, gap: bigint): TrendIssue[] {
  const issues: TrendIssue[] = [];
  if (point.deltaMs !== null) {
    const delta = BigInt(point.deltaMs);
    if (delta < 0n) issues.push('regression');
    else if (delta === 0n) issues.push('duplicate');
    else if (delta > gap) issues.push('gap');
  }
  if (!point.payloadComplete) issues.push('payload');
  if (point.previousTagSizeState === 'missing') issues.push('previous_missing');
  if (point.previousTagSizeState === 'invalid') issues.push('previous_invalid');
  return issues;
}
export function trendContext(point: TagTrendPoint): string {
  if (point.context === 'start') return '索引起点之前没有同类型、同 Stream ID 的 Tag';
  if (point.context === 'budget_exhausted') return '前驱未知：已达到 4096 条回查上限';
  return `相对 Tag ${point.previousTagId}（DTS ${point.previousDts} ms）`;
}
export function trendValue(point: TagTrendPoint, metric: TrendMetric): bigint | null {
  return metric === 'size' ? BigInt(point.availableDataSize) : metric === 'dts' ? BigInt(point.dts) : point.deltaMs === null ? null : BigInt(point.deltaMs);
}

export const TREND_PLOT = { width: 1000, height: 246, left: 92, right: 974, top: 24, bottom: 196 };
export function trendHitWidth(start: string, next: string): number {
  const count = unsigned(next) - unsigned(start);
  if (count < 1n || count > 512n) throw Error('Invalid trend window.');
  return count === 1n ? 18 : Math.min(18, (TREND_PLOT.right - TREND_PLOT.left) / Number(count - 1n) * .9);
}
// Only the bounded relative ratio becomes a Number. Global IDs/offsets never do.
export function trendX(id: string, start: string, next: string): number {
  const first = unsigned(start), last = unsigned(next) - 1n, value = unsigned(id);
  if (value < first || value > last) throw Error('Point outside window.');
  if (last === first) return (TREND_PLOT.left + TREND_PLOT.right) / 2;
  return TREND_PLOT.left + Number((value - first) * 1000000n / (last - first)) / 1000000 * (TREND_PLOT.right - TREND_PLOT.left);
}
export function trendDomain(points: TagTrendPoint[], metric: TrendMetric): { min: bigint; max: bigint } {
  const values = points.flatMap(point => {
    const value = trendValue(point, metric);
    return value === null ? [] : metric === 'size' ? [value, BigInt(point.dataSize)] : [value];
  });
  let min = 0n, max = 0n;
  for (const value of values) { if (value < min) min = value; if (value > max) max = value; }
  if (min === max) max = min + 1n;
  return { min, max };
}
export function trendY(value: bigint, domain: { min: bigint; max: bigint }): number {
  if (domain.max <= domain.min || value < domain.min || value > domain.max) throw Error('Invalid plot domain.');
  return TREND_PLOT.bottom - Number((value - domain.min) * 1000000n / (domain.max - domain.min)) / 1000000 * (TREND_PLOT.bottom - TREND_PLOT.top);
}
