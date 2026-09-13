import type { SummaryKind, SummaryMetric, SummaryOverview, SummaryPage, SummaryResponse } from './protocol.ts';
import { unsigned } from './numbers.ts';

export const SUMMARY_KINDS: SummaryKind[] = ['keyframes', 'aac', 'avc', 'mp3', 'diagnostics'];
export function summaryKind(value: string): SummaryKind {
  if (value === 'keyframes' || value === 'aac' || value === 'avc' || value === 'mp3' || value === 'diagnostics') return value;
  throw new Error('Invalid summary event kind.');
}
function boundedText(value: string): void {
  if (typeof value !== 'string' || new TextEncoder().encode(value).length > 256) throw new Error('Invalid summary text.');
}
function metrics(values: SummaryMetric[], limit: number): void {
  if (!Array.isArray(values) || values.length > limit) throw new Error('Invalid summary metrics.');
  for (const metric of values) {
    boundedText(metric.section); boundedText(metric.name); boundedText(metric.unit);
    if (metric.value !== null) boundedText(metric.value);
  }
}
function boundedJSON(value: unknown): void {
  if (new TextEncoder().encode(JSON.stringify(value)).length > 256 * 1024) throw new Error('Summary response exceeds its budget.');
}
export function validateSummaryOverview(value: SummaryOverview): void {
  unsigned(value.sourceSize); unsigned(value.indexedTags);
  for (const flag of [value.scanComplete, value.scanHasErrors, value.complete, value.hasErrors]) if (typeof flag !== 'boolean') throw new Error('Invalid summary completeness.');
  metrics(value.metrics, 128);
  if (value.lists.length !== SUMMARY_KINDS.length || new Set(value.lists.map(item => item.kind)).size !== SUMMARY_KINDS.length) throw new Error('Invalid summary lists.');
  for (const list of value.lists) {
    summaryKind(list.kind);
    const retained = unsigned(list.retained), cap = list.kind === 'keyframes' ? 1024n : list.kind === 'aac' || list.kind === 'avc' ? 128n : 256n;
    if (retained > cap || typeof list.truncated !== 'boolean' || (list.observed === null && list.kind !== 'diagnostics')) throw new Error('Invalid summary retention.');
    if (list.observed !== null && unsigned(list.observed) < retained) throw new Error('Invalid summary count.');
  }
  boundedJSON(value);
}
export function validateSummaryResponse(value: SummaryResponse): void {
  if (unsigned(value.processed) > unsigned(value.total)) throw new Error('Invalid summary progress.');
  if (value.state === 'ready') {
    if (!value.overview || value.error || value.processed !== value.total || value.overview.indexedTags !== value.total) throw new Error('Invalid completed summary.');
    validateSummaryOverview(value.overview);
  } else if (value.state === 'failed') {
    if (!value.error || value.overview) throw new Error('Invalid failed summary.');
  } else if (value.state === 'pending' || value.state === 'canceling') {
    if (value.error || value.overview) throw new Error('Invalid pending summary.');
  } else throw new Error('Invalid summary state.');
}
export function validateSummaryPage(value: SummaryPage, start: string, count: number): void {
  const bounds = value.bounds, first = unsigned(bounds.start), next = unsigned(bounds.next), total = unsigned(bounds.total);
  if (bounds.start !== start || bounds.limit !== count || count < 1 || count > 100 || first > next || next > total ||
      !Array.isArray(value.events) || value.events.length > count || next - first !== BigInt(value.events.length) || (first < total && next === first)) throw new Error('Invalid summary page bounds.');
  for (const event of value.events) {
    unsigned(event.id); unsigned(event.offset);
    if (event.tagId !== null) unsigned(event.tagId);
    if (event.dts !== null && !/^-?(0|[1-9]\d*)$/.test(event.dts)) throw new Error('Invalid summary timestamp.');
    if (!Number.isInteger(event.bitOffset) || event.bitOffset < 0 || event.bitOffset > 7) throw new Error('Invalid summary bit offset.');
    boundedText(event.title); boundedText(event.severity); boundedText(event.message); metrics(event.values, 24);
  }
  boundedJSON(value);
}
