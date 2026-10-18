import type { API, PageRequest, PageResponse, Session, Tag, TagTrendPage, TagTrendPoint, TimeSearchRequest, TimeSearchResponse } from '../src/protocol.ts';
import { setup as setupCodec } from './codec-search-fixture.ts';
export { deferred, settle } from './codec-search-fixture.ts';

export function point(id = '8192'): TagTrendPoint {
  const n = BigInt(id), dts = n === 8192n ? 163720n : n * 20n % 4294967296n, prior = n - 1n;
  const previous = prior === 8192n ? 163720n : prior * 20n % 4294967296n;
  return { tagId: id, offset: (13n + n * 20n).toString(), dts: dts.toString(), typeCode: 9, streamId: 0,
    dataSize: 5, availableDataSize: 5, payloadComplete: true, previousTagSizeState: 'valid', filtered: false,
    previousTagId: n === 0n ? null : prior.toString(), previousDts: n === 0n ? null : previous.toString(), deltaMs: n === 0n ? null : (dts - previous).toString(), context: n === 0n ? 'start' : 'found' };
}
export function ready(q: TimeSearchRequest, start = '0', next = '4096', total = '10000'): TimeSearchResponse {
  const first = BigInt(start), end = BigInt(next), exhausted = next === total;
  return { apiVersion: 1, ...q, state: 'ready', error: null, page: { cursor: q.cursor, nextCursor: exhausted ? '0' : q.requestId,
    start, next, total, scanned: Number(end - first), filter: q.filter, gapThreshold: q.gapThreshold, exhausted, scanComplete: false,
    unknownPredecessors: 0, rows: first <= 8192n && end > 8192n ? [{ kind: 'regression', point: point() }] : [] } };
}
export function failed(q: TimeSearchRequest, code = 'canceled'): TimeSearchResponse { return { apiVersion: 1, ...q, state: 'failed', error: { code, message: '' }, page: null }; }
export function pending(q: TimeSearchRequest, state: 'pending' | 'canceling' = 'pending'): TimeSearchResponse { return { apiVersion: 1, ...q, state, error: null, page: null }; }
export function trend(q: PageRequest): PageResponse<TagTrendPage> {
  const start = BigInt(q.start), next = start + BigInt(q.count) < 10000n ? start + BigInt(q.count) : 10000n;
  const contextScanned = start === 0n ? 0 : 1;
  return { apiVersion: 1, error: null, page: { sessionId: q.sessionId, bounds: { start: q.start, next: next.toString(), total: '10000', limit: q.count },
    contextScanned, contextStart: (start - BigInt(contextScanned)).toString(), points: Array.from({ length: Number(next - start) }, (_, i) => point((start + BigInt(i)).toString())) } };
}
function tag(id: string): Tag {
  const p = point(id), span = { byteOffset: p.offset, bitOffset: 0, bitLength: 128 };
  return { id, typeCode: 9, typeName: 'video', offset: p.offset, span, headerSpan: { ...span, bitLength: 88 }, payloadSpan: { ...span, byteOffset: (BigInt(p.offset) + 11n).toString(), bitLength: 40 },
    rawTypeFlags: 9, filter: false, dataSize: 5, availableDataSize: 5, dts: p.dts, streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 16, expected: 16, valid: true } };
}
export function setup(overrides: Partial<API> = {}) {
  let sequence = 0;
  const session = (): Session => ({ sessionId: `time-${sequence}`, state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null,
    progress: { sourceSize: '201000', bytesScanned: '201000', tagCount: '10000', diagnosticCount: 0 } });
  const calls: TimeSearchRequest[] = [], trends: PageRequest[] = [], tokens = new Map<string, string>();
  const fixture = setupCodec({
    open: async requestId => { sequence++; return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'time.flv', session: session(), error: null }; },
    status: async () => ({ apiVersion: 1, session: session(), error: null }), close: async () => ({ apiVersion: 1, session: { ...session(), closed: true }, error: null }),
    tags: async q => { const start = BigInt(q.start), next = start + 100n < 10000n ? start + 100n : 10000n; return { apiVersion: 1, error: null, page: { sessionId: q.sessionId,
      bounds: { start: q.start, next: next.toString(), total: '10000', limit: 100 }, tags: Array.from({ length: Number(next - start) }, (_, i) => tag((start + BigInt(i)).toString())) } }; },
    tagTrends: async q => { trends.push(q); return trend(q); },
    startTimeSearch: async q => { calls.push(q); const start = q.cursor === '0' ? '0' : tokens.get(q.cursor)!; const next = (BigInt(start) + 4096n < 10000n ? BigInt(start) + 4096n : 10000n).toString(); tokens.set(q.requestId, next); return ready(q, start, next); },
    ...overrides,
  });
  return { ...fixture, calls, trends };
}
