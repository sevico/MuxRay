import type { TimeSearchFilter, TimeSearchKind, TimeSearchRequest, TimeSearchResponse } from './protocol.ts';
import { unsigned } from './numbers.ts';
import { trendGap } from './tag-trends.ts';

export function timeSearchFilter(value: string): TimeSearchFilter {
  if (value === 'all' || value === 'regressions' || value === 'duplicates' || value === 'gaps') return value;
  throw Error('Invalid timestamp search filter.');
}
export function timeSearchKind(value: string): TimeSearchKind {
  if (value === 'regression' || value === 'duplicate' || value === 'gap' || value === 'unknown') return value;
  throw Error('Invalid timestamp observation kind.');
}
function decimal(value: string): bigint {
  if (typeof value !== 'string') throw Error('Invalid decimal wire value.');
  return unsigned(value);
}
function scalar(value: number, max: number): boolean { return Number.isInteger(value) && value >= 0 && value <= max; }

// Search predecessors are carried across batches, so unlike a trend window
// they may precede the current batch by more than the 4096-Tag lookback.
export function validateTimeSearch(response: TimeSearchResponse, request: TimeSearchRequest, total: string, sourceSize: string, expectedStart?: string | null): void {
  if (response.apiVersion !== 1 || response.sessionId !== request.sessionId || response.requestId !== request.requestId) throw Error('Invalid timestamp search identity.');
  if (response.state === 'failed') {
    if (!response.error || response.page) throw Error('Invalid timestamp search failure.');
    return;
  }
  if (response.error) throw Error('Invalid timestamp search error.');
  if (response.state === 'pending' || response.state === 'canceling') {
    if (response.page) throw Error('Invalid pending timestamp search.');
    return;
  }
  if (response.state !== 'ready' || !response.page) throw Error('Invalid timestamp search state.');
  const p = response.page, start = decimal(p.start), next = decimal(p.next), end = decimal(total), size = decimal(sourceSize);
  decimal(p.cursor); decimal(p.nextCursor); timeSearchFilter(p.filter);
  const gap = trendGap(p.gapThreshold);
  if (p.cursor !== request.cursor || p.filter !== request.filter || p.gapThreshold !== request.gapThreshold || p.total !== total ||
      !scalar(p.scanned, 4096) || next < start || next > end || next - start !== BigInt(p.scanned) ||
      (expectedStart !== undefined && expectedStart !== null && p.start !== expectedStart) || (request.cursor === '0' && start !== 0n) ||
      typeof p.exhausted !== 'boolean' || typeof p.scanComplete !== 'boolean' || p.exhausted !== (next === end) ||
      p.nextCursor !== (p.exhausted ? '0' : request.requestId) || (!p.exhausted && p.scanned === 0) ||
      !Array.isArray(p.rows) || p.rows.length > 64 || p.rows.length > p.scanned || !scalar(p.unknownPredecessors, p.rows.length)) throw Error('Invalid timestamp search bounds.');
  let previousID = start - 1n, previousOffset = -1n, unknown = 0;
  for (const row of p.rows) {
    timeSearchKind(row.kind);
    const point = row.point, id = decimal(point.tagId), offset = decimal(point.offset), dts = decimal(point.dts);
    if (id < start || id >= next || id <= previousID || offset <= previousOffset || dts > 4294967295n ||
        !scalar(point.typeCode, 31) || !scalar(point.streamId, 0xffffff) || !scalar(point.dataSize, 0xffffff) || !scalar(point.availableDataSize, point.dataSize) ||
        offset + 11n + BigInt(point.availableDataSize) > size || typeof point.filtered !== 'boolean' || typeof point.payloadComplete !== 'boolean' ||
        point.payloadComplete !== (point.availableDataSize === point.dataSize) || !['valid', 'missing', 'invalid'].includes(point.previousTagSizeState)) throw Error('Invalid timestamp observation point.');
    if (row.kind === 'unknown') {
      unknown++;
      if (point.context !== 'budget_exhausted' || point.previousTagId !== null || point.previousDts !== null || point.deltaMs !== null) throw Error('Invalid unknown predecessor.');
    } else {
      if (point.context !== 'found' || point.previousTagId === null || point.previousDts === null || point.deltaMs === null ||
          typeof point.deltaMs !== 'string' || !/^(0|-?[1-9][0-9]{0,9})$/.test(point.deltaMs)) throw Error('Missing timestamp predecessor.');
      const priorID = decimal(point.previousTagId), priorDts = decimal(point.previousDts), delta = BigInt(point.deltaMs);
      if (priorID >= id || priorDts > 4294967295n || delta !== dts - priorDts ||
          (row.kind === 'regression' ? delta >= 0n : row.kind === 'duplicate' ? delta !== 0n : delta <= gap) ||
          (p.filter !== 'all' && p.filter !== `${row.kind}s`)) throw Error('Invalid timestamp comparison.');
    }
    previousID = id; previousOffset = offset;
  }
  if (unknown !== p.unknownPredecessors) throw Error('Invalid unknown predecessor count.');
}
