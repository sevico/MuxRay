import type { AVCSearchFilter, AVCSearchRequest, AVCSearchResponse } from './protocol.ts';
import { keyframeRelations } from './codec-details.ts';
import { unsigned } from './numbers.ts';
import { selectionFromSpan, validateSelection } from './hex.ts';

export function avcSearchFilter(value: string): AVCSearchFilter {
  if (value === 'mismatches' || value === 'unknown' || value === 'all') return value;
  throw Error('Invalid AVC search filter.');
}

// Bind every page to its admitted cursor, source index and filter. A canceled
// batch never supplies a cursor that could skip unexamined Tags on retry.
export function validateAVCSearch(response: AVCSearchResponse, request: AVCSearchRequest, total: string, sourceSize: string): void {
  if (response.apiVersion !== 1 || response.sessionId !== request.sessionId || response.requestId !== request.requestId) throw Error('Invalid AVC search identity.');
  if (response.state === 'failed') {
    if (!response.error || response.page) throw Error('Invalid AVC search failure.');
    return;
  }
  if (response.error) throw Error('Invalid AVC search error.');
  if (response.state === 'pending' || response.state === 'canceling') {
    if (response.page) throw Error('Invalid pending AVC search.');
    return;
  }
  if (response.state !== 'ready' || !response.page) throw Error('Invalid AVC search state.');
  const page = response.page, start = unsigned(page.start), next = unsigned(page.next), end = unsigned(page.total), size = unsigned(sourceSize);
  if (page.start !== request.start || page.filter !== request.filter || page.total !== total || next < start || next > end ||
      next - start !== BigInt(page.scanned) || !Number.isInteger(page.scanned) || page.scanned < 0 || page.scanned > 256 ||
      (next === start && start !== end) || unsigned(page.bytesRead) > 1048576n ||
      page.exhausted !== (next === end) || typeof page.scanComplete !== 'boolean' || !Array.isArray(page.rows) || page.rows.length > 64 || page.rows.length > page.scanned) throw Error('Invalid AVC search bounds.');
  let previous = start - 1n;
  for (const row of page.rows) {
    const id = unsigned(row.tagId), offset = unsigned(row.offset);
    unsigned(row.dts);
    if (id <= previous || id < start || id >= next || offset >= size ||
        (row.declaredFrameType !== null && (!Number.isInteger(row.declaredFrameType) || row.declaredFrameType < 0 || row.declaredFrameType > 15)) ||
        !['yes', 'no', 'unknown'].includes(row.observedIDR) || !Object.hasOwn(keyframeRelations, row.declarationRelation) ||
        typeof row.reason !== 'string' || row.reason.length > 128 ||
        (page.filter === 'unknown' && row.observedIDR !== 'unknown') ||
        (page.filter === 'mismatches' && !['keyframe_without_idr', 'inter_with_idr'].includes(row.declarationRelation))) throw Error('Invalid AVC search row.');
    if (row.span) {
      validateSelection(selectionFromSpan(row.span, 'NAL evidence'));
      if (unsigned(row.span.byteOffset) < offset || unsigned(row.span.byteOffset) * 8n + BigInt(row.span.bitOffset + row.span.bitLength) > size * 8n) throw Error('Invalid AVC search source.');
    }
    previous = id;
  }
}
