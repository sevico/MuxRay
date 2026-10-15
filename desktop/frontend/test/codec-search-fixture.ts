import { Controller, POLL_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, CodecSearchRequest, CodecSearchResponse, CodecSearchRow, Session, Tag } from '../src/protocol.ts';
import { mp3PageDetail } from './mp3-page-fixture.ts';

export function row(kind: 'mp3' | 'sei' = 'mp3', patch: Partial<CodecSearchRow> = {}): CodecSearchRow {
  return { tagId: '0', dts: '0', offset: kind === 'mp3' ? '29' : '13', frameIndex: kind === 'mp3' ? 70 : null, nalIndex: kind === 'sei' ? 0 : null, messageIndex: kind === 'sei' ? 0 : null,
    kind: kind === 'mp3' ? 'bitrate' : 'pictureTiming', status: 'complete', complete: true, contextState: kind === 'sei' ? 'resolved' : '',
    span: { byteOffset: '29', bitOffset: 3, bitLength: 5 }, spsSpan: kind === 'sei' ? { byteOffset: '5', bitOffset: 0, bitLength: 32 } : null,
    evidenceSpan: kind === 'sei' ? { byteOffset: '20', bitOffset: 1, bitLength: 7 } : null,
    values: [{ name: kind === 'mp3' ? 'current.bitrateKbps' : 'spsId', value: kind === 'mp3' ? '192' : '0' }], ...patch };
}
export function ready(q: CodecSearchRequest, start = { tagId: '0', frameIndex: 0 }, next = { tagId: '2', frameIndex: 0 }): CodecSearchResponse {
  const item = row(q.kind, { tagId: start.tagId });
  if (q.kind === 'mp3') {
    if (q.filter === 'parameters') item.kind = 'parameters';
    if (q.filter === 'stops') { item.kind = 'stop'; item.status = 'malformedHeader'; item.complete = false; }
  } else {
    if (q.filter === 'bufferingPeriod') item.kind = 'bufferingPeriod';
    if (q.filter === 'unknown') { item.complete = false; item.status = 'unknown'; item.contextState = 'missing'; item.spsSpan = null; item.evidenceSpan = null; }
  }
  return { apiVersion: 1, ...q, state: 'ready', error: null, page: { cursor: q.cursor, nextCursor: q.requestId, kind: q.kind, filter: q.filter, start, next,
    total: '600', scannedTags: 2, scannedFrames: q.kind === 'mp3' ? 80 : 0, pending: 0, bytesRead: '320', exhausted: false, scanComplete: false, rows: [item] } };
}
export function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
export async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
export function failed(q: CodecSearchRequest, code = 'canceled'): CodecSearchResponse { return { apiVersion: 1, ...q, state: 'failed', error: { code, message: '' }, page: null }; }
export function pending(q: CodecSearchRequest, state: 'pending' | 'canceling' = 'pending'): CodecSearchResponse { return { apiVersion: 1, ...q, state, error: null, page: null }; }
const unexpected = async (): Promise<never> => { throw Error('Unexpected API call'); };
export function setup(overrides: Partial<API> = {}) {
  const clock = new Clock(), starts: CodecSearchRequest[] = [], reads: string[] = [];
  const session: Session = { sessionId: 'search', state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null,
    progress: { sourceSize: '10000', bytesScanned: '10000', tagCount: '600', diagnosticCount: 0 } };
  const span = { byteOffset: '13', bitOffset: 0, bitLength: 8 };
  const tag = (id: string): Tag => ({ id, typeCode: 9, typeName: 'video', offset: '13', span, headerSpan: span, payloadSpan: span, rawTypeFlags: 9, filter: false,
    dataSize: 1, availableDataSize: 1, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 12, expected: 12, valid: true } });
  const api: API = {
    startAVCSearch: unexpected, avcSearchResult: unexpected, cancelAVCSearch: unexpected,
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'fixture.flv', session, error: null }), openResult: unexpected,
    status: async () => ({ apiVersion: 1, session, error: null }), cancel: unexpected, close: async () => ({ apiVersion: 1, session: { ...session, closed: true }, error: null }),
    tags: async request => ({ apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: String(BigInt(request.start) + 100n), total: '600', limit: 100 }, tags: Array.from({ length: 100 }, (_, i) => tag(String(BigInt(request.start) + BigInt(i)))) } }),
    filteredTags: unexpected, tagTrends: unexpected, diagnostics: unexpected,
    startDetail: async request => { reads.push(request.requestId); return { apiVersion: 1, ...request, state: 'ready', error: null, detail: { ...mp3PageDetail(request.mp3PageStart ?? 0, request.mp3FrameIndex) } }; }, detailResult: unexpected,
    startHex: async request => { reads.push(request.requestId); return { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: '10000', hex: '00'.repeat(512), eof: false } }; }, hexResult: unexpected,
    startSummary: unexpected, summaryResult: unexpected, cancelSummary: unexpected, summaryEvents: unexpected, mediaCurves: unexpected, saveSnapshot: unexpected, saveResult: unexpected,
    startCodecSearch: async request => { starts.push(request); return ready(request); }, codecSearchResult: unexpected, cancelCodecSearch: unexpected, ...overrides,
  };
  return { controller: new Controller(api, clock), clock, starts, reads };
}
