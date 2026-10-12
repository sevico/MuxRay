import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, POLL_MS, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, AVCSearchRequest, AVCSearchResponse, SaveRequest, SaveResponse, Session, Tag } from '../src/protocol.ts';
import { validateAVCSearch } from '../src/avc-search.ts';

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
function ready(q: AVCSearchRequest): AVCSearchResponse {
  const start = BigInt(q.start), next = start + 100n > 600n ? 600n : start + 100n;
  return { apiVersion: 1, ...q, state: 'ready', error: null, page: { start: q.start, next: String(next), total: '600', scanned: Number(next - start), bytesRead: '15',
    scanComplete: false, exhausted: next === 600n, filter: q.filter, rows: next === start ? [] : [{ tagId: q.start, dts: '0', offset: '13', declaredFrameType: 1,
      observedIDR: q.filter === 'unknown' ? 'unknown' : 'no', declarationRelation: q.filter === 'unknown' ? 'undetermined' : 'keyframe_without_idr', reason: '', span: { byteOffset: '29', bitOffset: 3, bitLength: 5 } }] } };
}
function failed(q: AVCSearchRequest, code = 'canceled'): AVCSearchResponse { return { apiVersion: 1, ...q, state: 'failed', error: { code, message: '' }, page: null }; }
function pending(q: AVCSearchRequest, state: 'pending' | 'canceling' = 'pending'): AVCSearchResponse { return { apiVersion: 1, ...q, state, error: null, page: null }; }
const unexpected = async (): Promise<never> => { throw Error('Unexpected API call'); };
function setup(overrides: Partial<API> = {}) {
  const clock = new Clock(), starts: AVCSearchRequest[] = [], reads: string[] = [];
  const session: Session = { sessionId: 'search', state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null,
    progress: { sourceSize: '10000', bytesScanned: '10000', tagCount: '600', diagnosticCount: 0 } };
  const span = { byteOffset: '13', bitOffset: 0, bitLength: 8 };
  const tag = (id: string): Tag => ({ id, typeCode: 9, typeName: 'video', offset: '13', span, headerSpan: span, payloadSpan: span, rawTypeFlags: 9, filter: false,
    dataSize: 1, availableDataSize: 1, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 12, expected: 12, valid: true } });
  const api: API = {
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'fixture.flv', session, error: null }), openResult: unexpected,
    status: async () => ({ apiVersion: 1, session, error: null }), cancel: unexpected, close: async () => ({ apiVersion: 1, session: { ...session, closed: true }, error: null }),
    tags: async request => ({ apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: String(BigInt(request.start) + 100n), total: '600', limit: 100 }, tags: [tag(request.start)] } }),
    filteredTags: unexpected, tagTrends: unexpected, diagnostics: unexpected,
    startDetail: async request => { reads.push(request.requestId); return { apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'avcVideo', complete: true, truncated: false, fields: [], diagnostics: [] } }; }, detailResult: unexpected,
    startHex: async request => { reads.push(request.requestId); return { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: '10000', hex: '00'.repeat(512), eof: false } }; }, hexResult: unexpected,
    startSummary: unexpected, summaryResult: unexpected, cancelSummary: unexpected, summaryEvents: unexpected, mediaCurves: unexpected, saveSnapshot: unexpected, saveResult: unexpected,
    startAVCSearch: async request => { starts.push(request); return ready(request); }, avcSearchResult: unexpected, cancelAVCSearch: unexpected, ...overrides,
  };
  return { controller: new Controller(api, clock), clock, starts, reads };
}
test('search is explicit, uses actual bounded next cursors, retains current page through navigation and fences old rows', async () => {
  const { controller, starts, reads } = setup(); await controller.open(); await settle(); assert.equal(starts.length, 0);
  controller.selectTag('0'); await settle(); controller.startAVCSearch(); await settle();
  const first = controller.getSnapshot().avcSearchPage!;
  controller.locateAVCSearchRow(first.rows[0], 'hex'); await settle(); assert.equal(controller.getSnapshot().hexSelection?.bitOffset, 3);
  assert.equal(controller.getSnapshot().avcSearchPage, first);
  controller.pageAVCSearch('next'); await settle(); controller.pageAVCSearch('next'); await settle(); controller.pageAVCSearch('previous'); await settle();
  assert.deepEqual(starts.map(q => q.start), ['0', '100', '200', '100']);
  controller.locateAVCSearchRow(first.rows[0], 'tag'); await settle(); assert.equal(controller.getSnapshot().selectedTag, null);
  const row = controller.getSnapshot().avcSearchPage!.rows[0]; controller.locateAVCSearchRow(row, 'tag'); await settle();
  assert.equal(controller.getSnapshot().selectedTag?.id, '100'); assert.deepEqual(reads, ['1', '3', '7']);
  controller.startAVCSearch('unknown'); await settle(); assert.equal(controller.getSnapshot().avcSearchPrevious, null); assert.equal(starts.at(-1)?.start, '0'); controller.dispose();
});
test('lost Start confirms the same identity and late replies cannot replace the confirmed page', async () => {
  const lost = deferred<AVCSearchResponse>(); let request!: AVCSearchRequest; let gets = 0;
  const { controller, clock } = setup({ startAVCSearch: q => { request = q; return lost.promise; }, avcSearchResult: async q => { gets++; assert.equal(q.requestId, request.requestId); return ready(request); } });
  await controller.open(); await settle(); controller.startAVCSearch(); clock.tick(REPLY_TIMEOUT_MS); await settle(); clock.tick(); await settle();
  assert.equal(gets, 1); assert.equal(controller.getSnapshot().avcSearchState, 'ready'); lost.resolve(failed(request)); await settle();
  assert.equal(controller.getSnapshot().avcSearchState, 'ready'); controller.dispose();
});
test('cancel fences active reply, blocks other reads until confirmation, and retries the unadvanced batch', async () => {
  const lost = deferred<AVCSearchResponse>(); const starts: AVCSearchRequest[] = []; let request!: AVCSearchRequest;
  const { controller, clock, reads } = setup({ startAVCSearch: q => { starts.push(q); request = q; return starts.length === 1 ? lost.promise : Promise.resolve(ready(q)); },
    cancelAVCSearch: async () => pending(request, 'canceling'), avcSearchResult: async () => failed(request) });
  await controller.open(); await settle(); controller.startAVCSearch(); controller.cancelAVCSearch(); await settle();
  controller.selectTag('0'); controller.jumpHex('13'); controller.startSummary(); assert.deepEqual(reads, []);
  lost.resolve(ready(request)); await settle(); assert.equal(controller.getSnapshot().avcSearchPage, null);
  clock.tick(); await settle(); assert.equal(controller.getSnapshot().avcSearchRunning, false);
  controller.retryAVCSearch(); await settle(); assert.deepEqual(starts.map(q => [q.requestId, q.start]), [['1', '0'], ['2', '0']]); controller.dispose();
});
test('lost cancel with pending confirmation remains blocked and can resend only the same cancellation', async () => {
  let request!: AVCSearchRequest; const calls: string[] = [];
  const { controller, clock } = setup({ startAVCSearch: async q => { request = q; return pending(q); }, cancelAVCSearch: async q => { calls.push(q.requestId); if (calls.length === 1) throw Error('lost'); return failed(request); }, avcSearchResult: async () => pending(request) });
  await controller.open(); await settle(); controller.startAVCSearch(); await settle(); controller.cancelAVCSearch(); await settle(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().avcSearchError, 'avc_search_cancel_unconfirmed'); assert.equal(controller.getSnapshot().avcSearchRunning, true);
  controller.retryAVCSearch(); await settle(); assert.deepEqual(calls, ['1', '1']); assert.equal(controller.getSnapshot().avcSearchRunning, false); controller.dispose();
});
test('mismatched identity cannot invalidate source, while matching source change clears evidence', async () => {
  for (const mismatch of [true, false]) {
    const { controller } = setup({ startAVCSearch: async q => ({ ...failed(q, 'source_changed'), sessionId: mismatch ? 'old' : q.sessionId }) });
    await controller.open(); await settle(); controller.startAVCSearch(); await settle();
    assert.equal(controller.getSnapshot().sourceChanged, !mismatch); assert.equal(controller.getSnapshot().avcSearchPage, null); controller.dispose();
  }
});
test('close and dispose discard pending replies and timers', async () => {
  for (const close of [true, false]) {
    const lost = deferred<AVCSearchResponse>(); let request!: AVCSearchRequest;
    const { controller, clock } = setup({ startAVCSearch: q => { request = q; return lost.promise; } });
    await controller.open(); await settle(); controller.startAVCSearch(); if (close) await controller.close(); else controller.dispose();
    lost.resolve(ready(request)); await settle(); assert.equal(controller.getSnapshot().avcSearchPage, null); assert.equal(clock.tasks.size, 0);
  }
});
test('a complete-search claim cannot overwrite the known partial scan boundary', async () => {
  const { controller } = setup({ startAVCSearch: async q => { const r = ready(q); r.page!.scanComplete = true; return r; } });
  await controller.open(); await settle(); controller.startAVCSearch(); await settle();
  assert.equal(controller.getSnapshot().avcSearchPage, null); assert.equal(controller.getSnapshot().avcSearchError, 'invalid_response'); controller.dispose();
});
test('page validation rejects unbound cursors, over-budget work, duplicates and out-of-source spans', () => {
  const q: AVCSearchRequest = { sessionId: 's', requestId: '1', start: '0', filter: 'mismatches' };
  validateAVCSearch(ready(q), q, '600', '10000');
  const mutations: ((r: AVCSearchResponse) => void)[] = [r => r.page!.next = '0', r => r.page!.bytesRead = '1048577', r => r.page!.rows.push(r.page!.rows[0]),
    r => r.page!.rows[0].span!.byteOffset = '10000', r => r.page!.filter = 'all', r => r.page!.rows[0].declarationRelation = 'undetermined', r => r.page!.exhausted = true];
  for (const mutate of mutations) { const r = ready(q); mutate(r); assert.throws(() => validateAVCSearch(r, q, '600', '10000')); }
});

function saved(requestId: string): SaveResponse {
  return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'evidence.json', bytesWritten: '1024', error: null };
}
test('snapshot v3 freezes current search identity through navigation, paging, filter changes and close without another search', async () => {
  const saving = deferred<SaveResponse>(), saves: SaveRequest[] = [];
  const { controller, starts } = setup({ saveSnapshot: request => { saves.push(request); return saving.promise; } });
  await controller.open(); await settle(); controller.startAVCSearch('unknown'); await settle();
  controller.locateAVCSearchRow(controller.getSnapshot().avcSearchPage!.rows[0], 'hex'); await settle();
  const waiting = controller.saveSnapshot();
  assert.equal(saves[0].snapshot.schemaVersion, 3);
  assert.deepEqual(saves[0].snapshot.avcSearch, { requestId: '1', start: '0', filter: 'unknown' });
  assert.equal(saves[0].snapshot.hexRequestId, '2'); assert.equal(starts.length, 1);
  assert.equal(Object.isFrozen(saves[0].snapshot.avcSearch), true);
  controller.pageAVCSearch('next'); await settle(); controller.startAVCSearch('all'); await settle();
  await controller.close();
  assert.deepEqual(saves[0].snapshot.avcSearch, { requestId: '1', start: '0', filter: 'unknown' });
  saving.resolve(saved(saves[0].requestId)); await waiting; controller.dispose();
});
test('snapshot includes a completed empty search page and never sends client result rows', async () => {
  const saves: SaveRequest[] = [];
  const { controller } = setup({ startAVCSearch: async q => { const response = ready(q); response.page!.rows = []; return response; },
    saveSnapshot: async request => { saves.push(request); return saved(request.requestId); } });
  await controller.open(); await settle(); await controller.saveSnapshot();
  assert.equal(saves[0].snapshot.avcSearch, undefined);
  controller.startAVCSearch(); await settle(); await controller.saveSnapshot();
  assert.deepEqual(saves[1].snapshot.avcSearch, { requestId: '1', start: '0', filter: 'mismatches' });
  assert.deepEqual(Object.keys(saves[1].snapshot.avcSearch!).sort(), ['filter', 'requestId', 'start']);
  controller.dispose();
});
test('snapshot omits replacement searches while pending, canceling or failed and exports only the completed retry', async () => {
  const saves: SaveRequest[] = []; let request!: AVCSearchRequest, calls = 0;
  const { controller, clock } = setup({ startAVCSearch: async q => { request = q; return ++calls === 2 ? pending(q) : ready(q); },
    cancelAVCSearch: async () => pending(request, 'canceling'), avcSearchResult: async () => failed(request),
    saveSnapshot: async q => { saves.push(q); return saved(q.requestId); } });
  await controller.open(); await settle(); controller.startAVCSearch(); await settle();
  controller.pageAVCSearch('next'); await settle(); await controller.saveSnapshot();
  controller.cancelAVCSearch(); await settle(); await controller.saveSnapshot();
  clock.tick(); await settle(); await controller.saveSnapshot();
  assert.equal(controller.getSnapshot().avcSearchError, 'canceled');
  assert.ok(saves.every(q => q.snapshot.avcSearch === undefined));
  controller.retryAVCSearch(); await settle(); await controller.saveSnapshot();
  assert.deepEqual(saves.at(-1)!.snapshot.avcSearch, { requestId: '3', start: '100', filter: 'mismatches' });
  controller.dispose();
});
test('snapshot after lost search reply uses the confirmed identity and clears it on a new session', async () => {
  const lost = deferred<AVCSearchResponse>(), saves: SaveRequest[] = []; let request!: AVCSearchRequest;
  const { controller, clock } = setup({ startAVCSearch: q => { request = q; return lost.promise; }, avcSearchResult: async () => ready(request),
    saveSnapshot: async q => { saves.push(q); return saved(q.requestId); } });
  await controller.open(); await settle(); controller.startAVCSearch(); clock.tick(REPLY_TIMEOUT_MS); await settle();
  await controller.saveSnapshot(); assert.equal(saves[0].snapshot.avcSearch, undefined);
  clock.tick(); await settle(); await controller.saveSnapshot();
  assert.deepEqual(saves[1].snapshot.avcSearch, { requestId: '1', start: '0', filter: 'mismatches' });
  lost.resolve(failed(request)); await settle(); await controller.close(); await controller.open(); await settle();
  await controller.saveSnapshot(); assert.equal(saves[2].snapshot.avcSearch, undefined); controller.dispose();
});
