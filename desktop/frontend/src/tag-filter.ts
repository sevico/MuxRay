import type { FilteredTagPage, FilteredTagRequest, Tag, TagFilter } from './protocol.ts';
import { unsigned } from './numbers.ts';

export const FILTER_SCAN_LIMIT = 4096;
export const FILTER_HISTORY_LIMIT = 256;
export const EMPTY_TAG_FILTER: TagFilter = { type: 'all', dtsMin: null, dtsMax: null, structure: 'all' };

export function validateTagFilter(filter: TagFilter): void {
  if (!filter || !['all', 'audio', 'video', 'script', 'other'].includes(filter.type) ||
      !['all', 'payload_incomplete', 'previous_size_missing', 'previous_size_invalid'].includes(filter.structure)) throw Error('Invalid filter.');
  for (const value of [filter.dtsMin, filter.dtsMax]) {
    if (value !== null && (typeof value !== 'string' || unsigned(value) > 4294967295n)) throw Error('Invalid DTS.');
  }
  if (filter.dtsMin !== null && filter.dtsMax !== null && unsigned(filter.dtsMin) > unsigned(filter.dtsMax)) throw Error('Invalid DTS range.');
}
export function sameTagFilter(a: TagFilter, b: TagFilter): boolean {
  return a.type === b.type && a.dtsMin === b.dtsMin && a.dtsMax === b.dtsMax && a.structure === b.structure;
}
function matches(tag: Tag, filter: TagFilter): boolean {
  const type = tag.typeCode === 8 ? 'audio' : tag.typeCode === 9 ? 'video' : tag.typeCode === 18 ? 'script' : 'other';
  const dts = unsigned(tag.dts);
  return dts <= 4294967295n && (filter.type === 'all' || type === filter.type) &&
    (filter.dtsMin === null || dts >= unsigned(filter.dtsMin)) && (filter.dtsMax === null || dts <= unsigned(filter.dtsMax)) &&
    (filter.structure === 'all' || (filter.structure === 'payload_incomplete' && !tag.payloadComplete) ||
      (filter.structure === 'previous_size_missing' && !tag.previousTagSize.present) ||
      (filter.structure === 'previous_size_invalid' && tag.previousTagSize.present && !tag.previousTagSize.valid));
}
export function validateFilteredTagPage(page: FilteredTagPage, request: FilteredTagRequest): void {
  validateTagFilter(page.filter);
  const start = unsigned(page.start), next = unsigned(page.next), total = unsigned(page.indexedTotal);
  if (page.sessionId !== request.sessionId || page.start !== request.start || !sameTagFilter(page.filter, request.filter) ||
      page.limit !== request.count || !Number.isInteger(page.limit) || page.limit < 1 || page.limit > 100 ||
      page.scanLimit !== FILTER_SCAN_LIMIT || !Number.isInteger(page.scanned) || page.scanned < 0 || page.scanned > page.scanLimit ||
      start > next || next > total || next - start !== BigInt(page.scanned) || typeof page.exhausted !== 'boolean' || page.exhausted !== (next === total) ||
      !Array.isArray(page.tags) || page.tags.length > page.limit || page.tags.length > page.scanned ||
      (!page.exhausted && page.scanned !== page.scanLimit && page.tags.length !== page.limit)) throw Error('Invalid filter page bounds.');
  let previous = start - 1n;
  for (const tag of page.tags) {
    const id = unsigned(tag.id);
    if (id <= previous || id < start || id >= next || !matches(tag, page.filter)) throw Error('Invalid filter match.');
    previous = id;
  }
}
