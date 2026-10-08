import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, POLL_MS, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, SaveRequest, SaveResponse, Session, Tag } from '../src/protocol.ts';
import { saveResponse } from '../src/normalize.ts';
import { summaryFailed, summaryPage, summaryReady } from './summary-fixture.ts';

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
function receipt(requestId: string, values: Partial<SaveResponse> = {}): SaveResponse {
  return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'sample.muxray.json', bytesWritten: '4096', error: null, ...values };
}
const pending = (id: string) => receipt(id, { pending: true, fileName: '', bytesWritten: '0' });
const canceled = (id: string) => receipt(id, { canceled: true, fileName: '', bytesWritten: '0' });
function tag(id: string): Tag {
  const span = { byteOffset: '13', bitOffset: 0, bitLength: 120 };
  return { id, typeCode: 8, typeName: 'audio', offset: '13', span, headerSpan: span, payloadSpan: span, rawTypeFlags: 8, filter: false, dataSize: 4, availableDataSize: 4, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 15, expected: 15, valid: true } };
}
function setup(overrides: Partial<API> = {}) {
  const clock = new Clock(), saves: SaveRequest[] = [];
  let opens = 0;
  const session = (id = `scan-${opens}`): Session => ({ sessionId: id, state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null, progress: { sourceSize: '10000', bytesScanned: '10000', tagCount: '2', diagnosticCount: 0 } });
  const api: API = {
    mediaCurves: async () => { throw Error('unexpected media curves'); },
    open: async requestId => { opens++; return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: `sample-${opens}.flv`, session: session(), error: null }; },
    openResult: async () => { throw Error('Unexpected open recovery'); }, status: async id => ({ apiVersion: 1, session: session(id), error: null }), cancel: async id => ({ apiVersion: 1, session: session(id), error: null }),
    close: async id => ({ apiVersion: 1, session: { ...session(id), closed: true }, error: null }),
    tags: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: '2', total: '2', limit: 100 }, tags: [tag('0'), tag('1')] } }),
    filteredTags: async () => { throw Error('Unexpected filtered query'); },
    tagTrends: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: '2', total: '2', limit: request.count }, contextStart: '0', contextScanned: 0,
      points: [0, 1].map(id => ({ tagId: String(id), offset: String(13 + id * 20), dts: String(id * 40), typeCode: 8, streamId: 0, dataSize: 4, availableDataSize: 4, payloadComplete: true, previousTagSizeState: 'valid', filtered: false,
        previousTagId: id ? '0' : null, previousDts: id ? '0' : null, deltaMs: id ? '40' : null, context: id ? 'found' : 'start' })) } }),
    diagnostics: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: '0', total: '0', limit: 100 }, diagnostics: [] } }),
    startDetail: async request => ({ apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'aacAudio', complete: true, truncated: false, fields: [], diagnostics: [] } }),
    detailResult: async () => { throw Error('Unexpected detail poll'); },
    startHex: async request => ({ apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: '10000', hex: 'aa'.repeat(512), eof: false } }),
    hexResult: async () => { throw Error('Unexpected Hex poll'); },
    startSummary: async request => summaryReady(request), summaryResult: async request => summaryReady(request), cancelSummary: async request => summaryFailed(request, 'canceled'), summaryEvents: async request => summaryPage(request),
    saveSnapshot: async request => { saves.push(request); return receipt(request.requestId); }, saveResult: async id => receipt(id), ...overrides,
  };
  return { controller: new Controller(api, clock), clock, saves, opens: () => opens };
}

test('save normalization restores optional errors and preserves exact receipt text', () => {
  const value = saveResponse(JSON.parse('{"apiVersion":1,"requestId":"9007199254740993","pending":false,"canceled":false,"fileName":"诊断.json","bytesWritten":"4096"}'));
  assert.equal(value.error, null); assert.equal(value.requestId, '9007199254740993'); assert.equal(value.fileName, '诊断.json');
});
test('saving is explicit and freezes request IDs, visible pages and applied trend options', async () => {
  const saved = deferred<SaveResponse>(); let captured!: SaveRequest; let calls = 0;
  const { controller } = setup({ saveSnapshot: request => { calls++; captured = request; return saved.promise; } });
  await controller.saveSnapshot(); assert.equal(calls, 0); await controller.open(); await settle();
  controller.startSummary(); await settle(); controller.selectTag('0'); await settle(); controller.jumpHex('13'); await settle(); controller.loadTagTrends(); await settle();
  controller.setTrendOptions({ trendMetric: 'delta', trendType: 'audio', trendGapThreshold: '20' });
  const saving = controller.saveSnapshot(); await controller.saveSnapshot(); assert.equal(calls, 1);
  assert.deepEqual(captured.snapshot, { schemaVersion: 2, sessionId: 'scan-1', selectedTagId: '0', detailRequestId: '2', hexRequestId: '3', summaryRequestId: '1', summaryPage: { kind: 'keyframes', start: '0', count: 100 }, trend: { start: '0', count: 256, type: 'audio', metric: 'delta', gapThreshold: '20' } });
  controller.selectTag('1'); controller.selectSummaryKind('mp3'); controller.setTrendOptions({ trendMetric: 'size', trendType: 'video', trendGapThreshold: '500' }); await settle();
  assert.equal(captured.snapshot.selectedTagId, '0'); assert.equal(captured.snapshot.summaryPage?.kind, 'keyframes'); assert.equal(captured.snapshot.trend?.metric, 'delta');
  assert.equal(Object.isFrozen(captured.snapshot), true); assert.equal(Object.isFrozen(captured.snapshot.trend), true);
  saved.resolve(receipt(captured.requestId)); await saving; assert.equal(controller.getSnapshot().saveState, 'saved'); assert.equal(controller.getSnapshot().selectedTag?.id, '1'); controller.dispose();
});
test('unloaded sections stay absent while pending read IDs can be marked omitted by the host', async () => {
  const { controller, saves } = setup({ startDetail: async request => ({ apiVersion: 1, ...request, state: 'pending', error: null, detail: null }) });
  await controller.open(); await settle(); await controller.saveSnapshot();
  assert.deepEqual(saves[0].snapshot, { schemaVersion: 2, sessionId: 'scan-1', selectedTagId: null, detailRequestId: null, hexRequestId: null, summaryRequestId: null, summaryPage: null, trend: null });
  controller.selectTag('0'); await settle(); await controller.saveSnapshot(); assert.equal(saves[1].snapshot.detailRequestId, '1'); assert.equal(saves[1].snapshot.selectedTagId, '0'); controller.dispose();
});
test('a lost initial save reply confirms the original request without repeating dialog or write', async () => {
  const old = deferred<SaveResponse>(); const calls: string[] = [], queries: string[] = [];
  const { controller, clock } = setup({ saveSnapshot: request => { calls.push(request.requestId); return old.promise; }, saveResult: async id => { queries.push(id); return receipt(id); } });
  await controller.open(); await settle(); const saving = controller.saveSnapshot(); clock.tick(REPLY_TIMEOUT_MS); await saving;
  assert.equal(controller.getSnapshot().saveState, 'recovering'); assert.equal(controller.getSnapshot().savePending, true); clock.tick(); await settle();
  assert.deepEqual(calls, ['1']); assert.deepEqual(queries, ['1']); assert.equal(controller.getSnapshot().saveState, 'saved');
  old.resolve(canceled('1')); await settle(); assert.equal(controller.getSnapshot().saveState, 'saved'); controller.dispose();
});
test('save pending polls serially and a lost confirmation pauses until retrying the same ID', async () => {
  const lost = deferred<SaveResponse>(); let starts = 0; const queries: string[] = [];
  const { controller, clock } = setup({ saveSnapshot: async request => { starts++; return pending(request.requestId); }, saveResult: id => { queries.push(id); return queries.length === 1 ? lost.promise : Promise.resolve(receipt(id)); } });
  await controller.open(); await settle(); await controller.saveSnapshot(); clock.tick(); clock.tick(); assert.equal(queries.length, 1);
  clock.tick(REPLY_TIMEOUT_MS); await settle(); assert.equal(controller.getSnapshot().saveRetry, true); assert.equal(controller.getSnapshot().savePending, true); assert.equal(clock.tasks.size, 0);
  controller.retrySave(); controller.retrySave(); clock.tick(); await settle(); assert.deepEqual(queries, ['1', '1']); assert.equal(starts, 1); assert.equal(controller.getSnapshot().saveState, 'saved');
  lost.resolve(pending('1')); await settle(); assert.equal(controller.getSnapshot().saveState, 'saved'); controller.dispose();
});
test('closing retains the frozen save gate and receipt; new files cannot inherit its late callback', async () => {
  const old = deferred<SaveResponse>(); const { controller, clock, opens } = setup({ saveSnapshot: () => old.promise });
  await controller.open(); await settle(); const saving = controller.saveSnapshot(); await controller.close(); await controller.open();
  assert.equal(controller.getSnapshot().session, null); assert.equal(opens(), 1); assert.equal(controller.getSnapshot().savePending, true); assert.equal(controller.getSnapshot().saveSourceName, 'sample-1.flv');
  clock.tick(REPLY_TIMEOUT_MS); await saving; clock.tick(); await settle(); assert.equal(controller.getSnapshot().saveState, 'saved'); assert.equal(controller.getSnapshot().saveSourceName, 'sample-1.flv');
  await controller.open(); await settle(); assert.equal(opens(), 2); assert.equal(controller.getSnapshot().saveState, '');
  old.resolve(receipt('1', { error: { code: 'source_changed', message: 'Late' }, bytesWritten: '0' })); await settle();
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2'); assert.equal(controller.getSnapshot().sourceChanged, false); assert.equal(controller.getSnapshot().saveState, ''); controller.dispose();
});
test('save cancellation and authoritative failure release admission with a fresh next request ID', async () => {
  const requests: string[] = [];
  const { controller } = setup({ saveSnapshot: async request => { requests.push(request.requestId); return requests.length === 1 ? canceled(request.requestId) : receipt(request.requestId, { fileName: '', bytesWritten: '0', error: { code: 'write_failed', message: 'Failed' } }); } });
  await controller.open(); await settle(); await controller.saveSnapshot(); assert.equal(controller.getSnapshot().saveState, 'canceled'); assert.equal(controller.getSnapshot().savePending, false);
  controller.retrySave(); await settle(); assert.deepEqual(requests, ['1']); await controller.saveSnapshot(); assert.deepEqual(requests, ['1', '2']);
  assert.equal(controller.getSnapshot().saveState, 'failed'); assert.equal(controller.getSnapshot().saveError, 'write_failed'); assert.equal(controller.getSnapshot().saveRetry, false); assert.equal(controller.getSnapshot().savePending, false); controller.dispose();
});
test('unseen save cancellation from recovery releases the gate and never resends the original save', async () => {
  let starts = 0; const queries: string[] = [];
  const { controller, clock } = setup({ saveSnapshot: async () => { starts++; throw Error('Not delivered'); }, saveResult: async id => { queries.push(id); return canceled(id); } });
  await controller.open(); await settle(); await controller.saveSnapshot(); clock.tick(); await settle();
  assert.equal(starts, 1); assert.deepEqual(queries, ['1']); assert.equal(controller.getSnapshot().saveState, 'canceled'); assert.equal(controller.getSnapshot().savePending, false); controller.dispose();
});
test('malformed receipts keep the save uncertain until matching confirmation', async () => {
  for (const malformed of [
    receipt('2'), receipt('1', { apiVersion: 2 }), receipt('1', { pending: true }), receipt('1', { canceled: true }),
    receipt('1', { bytesWritten: '0' }), receipt('1', { bytesWritten: '01' }), receipt('1', { fileName: 'C:\\secret\\report.json' }),
    receipt('1', { bytesWritten: '2097153' }), receipt('1', { bytesWritten: '0', error: { code: '', message: 'Invalid' } }),
    receipt('1', { pending: true, canceled: true, bytesWritten: '0' }),
  ]) {
    const { controller, clock } = setup({ saveSnapshot: async () => malformed });
    await controller.open(); await settle(); await controller.saveSnapshot(); assert.equal(controller.getSnapshot().savePending, true); assert.equal(controller.getSnapshot().saveState, 'recovering');
    clock.tick(); await settle(); assert.equal(controller.getSnapshot().saveState, 'saved'); controller.dispose();
  }
});
test('a stale save confirmation cannot release a newer save or alter its status', async () => {
  const old = deferred<SaveResponse>(); let starts = 0, queries = 0;
  const { controller, clock } = setup({ saveSnapshot: async request => { starts++; return pending(request.requestId); }, saveResult: id => ++queries === 1 ? old.promise : Promise.resolve(receipt(id)) });
  await controller.open(); await settle(); await controller.saveSnapshot(); clock.tick(); clock.tick(REPLY_TIMEOUT_MS); await settle();
  controller.retrySave(); clock.tick(); await settle(); await controller.saveSnapshot(); assert.equal(starts, 2); assert.equal(controller.getSnapshot().savePending, true);
  old.resolve(receipt('1')); await settle(); assert.equal(controller.getSnapshot().savePending, true); clock.tick(); await settle(); assert.equal(controller.getSnapshot().saveState, 'saved'); controller.dispose();
});
test('disposal stops save confirmation and ignores every late result', async () => {
  for (const phase of ['initial', 'confirmation'] as const) {
    const old = deferred<SaveResponse>();
    const { controller, clock } = setup({ saveSnapshot: request => phase === 'initial' ? old.promise : Promise.resolve(pending(request.requestId)), saveResult: () => old.promise });
    await controller.open(); await settle(); const saving = controller.saveSnapshot(); if (phase === 'confirmation') { await saving; clock.tick(); }
    controller.dispose(); const state = controller.getSnapshot(); old.resolve(receipt('1')); await saving; await settle();
    assert.equal(controller.getSnapshot(), state); assert.equal(clock.tasks.size, 0);
  }
});
test('trend export preferences reset on a new session and invalid thresholds never enter the snapshot', async () => {
  const { controller, saves } = setup(); await controller.open(); await settle(); controller.loadTagTrends(); await settle();
  controller.setTrendOptions({ trendMetric: 'size', trendType: 'video', trendGapThreshold: '4294967295' });
  for (const value of ['0', '01', '4294967296']) assert.throws(() => controller.setTrendOptions({ trendGapThreshold: value }));
  await controller.saveSnapshot(); assert.equal(saves[0].snapshot.trend?.gapThreshold, '4294967295');
  await controller.close(); await controller.open(); await settle(); assert.equal(controller.getSnapshot().trendMetric, 'dts'); assert.equal(controller.getSnapshot().trendType, 'all'); assert.equal(controller.getSnapshot().trendGapThreshold, '1000'); controller.dispose();
});
test('source-change capture failure invalidates the matching view; already frozen saves still finish independently', async () => {
  const failed = receipt('1', { fileName: '', bytesWritten: '0', error: { code: 'source_changed', message: 'Changed' } });
  const first = setup({ saveSnapshot: async () => failed }); await first.controller.open(); await settle(); await first.controller.saveSnapshot();
  assert.equal(first.controller.getSnapshot().sourceChanged, true); assert.equal(first.controller.getSnapshot().saveState, 'failed'); assert.equal(first.controller.getSnapshot().savePending, false); first.controller.dispose();
  const second = setup({ saveSnapshot: async request => pending(request.requestId), startHex: async request => ({ apiVersion: 1, ...request, state: 'failed', window: null, error: { code: 'source_changed', message: 'Changed later' } }) });
  await second.controller.open(); await settle(); await second.controller.saveSnapshot(); second.controller.jumpHex('13'); await settle();
  assert.equal(second.controller.getSnapshot().sourceChanged, true); assert.equal(second.controller.getSnapshot().savePending, true);
  second.clock.tick(); await settle(); assert.equal(second.controller.getSnapshot().saveState, 'saved'); assert.equal(second.controller.getSnapshot().sourceChanged, true); second.controller.dispose();
});
