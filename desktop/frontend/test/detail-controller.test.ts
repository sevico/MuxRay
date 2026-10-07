import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, DETAIL_POLL_LIMIT, POLL_MS, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, Detail, DetailRequest, DetailResponse, Session, Tag } from '../src/protocol.ts';

const detail: Detail = { kind: 'amf0ScriptData', complete: true, truncated: false, fields: [], diagnostics: [] };
const replayDetail: Detail = { kind: 'avcVideo', complete: false, truncated: true, diagnostics: [], fields: [
  { id: '0', parentId: '', name: 'Parameter context', type: 'bounded replay', value: 'budget_exhausted', span: null },
  ...Object.entries({ nextTagId: '8192', selectedTagId: '9007199254740993', contextSegmentStartTagId: '0', contextCanContinue: 'true' }).map(([name, value], i) => ({ id: String(i + 1), parentId: '0', name, type: 'derived', value, span: null })),
] };
const ready = (request: DetailRequest, patch: Partial<DetailResponse> = {}): DetailResponse => ({ apiVersion: 1, ...request, state: 'ready', detail, error: null, ...patch });
const pending = (request: DetailRequest) => ready(request, { state: 'pending', detail: null });
function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function settle() { for (let i = 0; i < 20; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { assert.ok(ms === POLL_MS || ms === REPLY_TIMEOUT_MS); this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [task, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(task); task(); } }
}
function terminal(sessionId = 'scan-1'): Session {
  return { sessionId, state: 'finished', terminal: true, closed: false, progress: { sourceSize: '100', bytesScanned: '100', tagCount: '3', diagnosticCount: 0 }, scan: { complete: true, hasErrors: false }, failure: null };
}
function tag(id: string): Tag {
  const span = { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 8 };
  return { id, typeCode: 18, typeName: 'script', offset: span.byteOffset, span, headerSpan: span, payloadSpan: span, rawTypeFlags: 18, filter: false, dataSize: 1, availableDataSize: 1, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 12, expected: 12, valid: true } };
}
function setup(overrides: Partial<API> = {}) {
  const clock = new Clock();
  const api: API = {
    mediaCurves: async () => { throw Error('unexpected media curves'); },
    saveSnapshot: async () => { throw Error('unexpected save'); }, saveResult: async () => { throw Error('unexpected save confirmation'); },
    tagTrends: async () => { throw Error('unexpected trend query'); },
    filteredTags: async () => { throw Error('unexpected filtered tags'); },
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'sample.flv', session: terminal(), error: null }),
    openResult: async () => { throw Error('unexpected open recovery'); },
    status: async () => ({ apiVersion: 1, session: terminal(), error: null }),
    cancel: async () => ({ apiVersion: 1, session: { ...terminal(), state: 'canceled', scan: { complete: false, hasErrors: false } }, error: null }),
    close: async id => ({ apiVersion: 1, session: { ...terminal(id), closed: true }, error: null }),
    tags: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, total: '3', next: '3', limit: 100 }, tags: ['0', '1', '9007199254740993'].map(tag) } }),
    diagnostics: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, total: '0', next: '0', limit: 100 }, diagnostics: [] } }),
    startDetail: async request => ready(request), detailResult: async () => { throw Error('unexpected detail poll'); },
    startHex: async () => { throw Error('unexpected Hex request'); }, hexResult: async () => { throw Error('unexpected Hex query'); }, ...unexpectedSummary, ...overrides,
  };
  return { controller: new Controller(api, clock), clock };
}

test('details require a retained terminal Tag and preserve exact tag IDs', async () => {
  const requests: DetailRequest[] = [];
  const { controller, clock } = setup({
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'sample.flv', error: null, session: { ...terminal(), state: 'running', terminal: false, scan: null } }),
    startDetail: async request => { requests.push(request); return ready(request); },
  });
  await controller.open(); controller.selectTag('0'); assert.equal(requests.length, 0);
  await controller.cancel(); await settle();
  controller.selectTag('missing'); assert.equal(requests.length, 0);
  controller.selectTag('9007199254740993'); await settle();
  assert.deepEqual(requests, [{ sessionId: 'scan-1', requestId: '1', tagId: '9007199254740993' }]);
  assert.equal(controller.getSnapshot().session?.scan?.complete, false);
  assert.equal(controller.getSnapshot().detailState, 'ready'); assert.equal(clock.tasks.size, 0);
});

test('pending detail polling is serial, bounded, and manual retry keeps the same identity', async () => {
  const query = deferred<DetailResponse>(); const starts: DetailRequest[] = []; let queries = 0;
  const { controller, clock } = setup({ startDetail: async request => { starts.push(request); return starts.length === 1 ? pending(request) : ready(request); }, detailResult: () => { queries++; return query.promise; } });
  await controller.open(); await settle(); controller.selectTag('0'); await settle();
  clock.tick(); clock.tick(); controller.retryDetail(); assert.equal(queries, 1); assert.equal(starts.length, 1);
  query.resolve(pending(starts[0])); await settle();
  for (let i = 1; i < DETAIL_POLL_LIMIT; i++) { clock.tick(); await settle(); }
  assert.equal(queries, DETAIL_POLL_LIMIT); assert.equal(controller.getSnapshot().detailError, 'deadline_exceeded');
  assert.equal(clock.tasks.size, 0); clock.tick(); assert.equal(queries, DETAIL_POLL_LIMIT);
  controller.retryDetail(); await settle();
  assert.equal(controller.getSnapshot().detailState, 'ready'); assert.deepEqual(starts[0], starts[1]);
});

test('a never-settling Start reply is recovered by Get and a late Start cannot replace it', async () => {
  const started = deferred<DetailResponse>(); const requests: DetailRequest[] = []; let queries = 0;
  const { controller, clock } = setup({ startDetail: request => { requests.push(request); return started.promise; }, detailResult: async () => { queries++; return ready(requests[0]); } });
  await controller.open(); await settle(); controller.selectTag('0');
  clock.tick(REPLY_TIMEOUT_MS); await settle(); assert.equal(controller.getSnapshot().detailState, 'recovering');
  clock.tick(); await settle(); assert.equal(queries, 1); assert.equal(requests.length, 1);
  started.resolve(ready(requests[0], { state: 'failed', detail: null, error: { code: 'detail_failed', message: 'late' } })); await settle();
  assert.equal(controller.getSnapshot().detailState, 'ready'); assert.equal(controller.getSnapshot().detailError, null); assert.equal(clock.tasks.size, 0);
});

test('a lost Get callback stops at its deadline and retry resends only the original Start', async () => {
  const lost = deferred<DetailResponse>(); const requests: DetailRequest[] = []; let queries = 0;
  const { controller, clock } = setup({ startDetail: async request => { requests.push(request); return requests.length === 1 ? pending(request) : ready(request); }, detailResult: () => { queries++; return lost.promise; } });
  await controller.open(); await settle(); controller.selectTag('0'); await settle(); clock.tick();
  clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().detailError, 'transport_failed'); assert.equal(clock.tasks.size, 0);
  clock.tick(); assert.equal(queries, 1);
  controller.retryDetail(); controller.retryDetail(); await settle();
  assert.equal(requests.length, 2); assert.deepEqual(requests[0], requests[1]);
  lost.resolve(pending(requests[0])); await settle(); assert.equal(controller.getSnapshot().detailState, 'ready');
});

test('unknown Get identity requires explicit idempotent retry when Start never reached the host', async () => {
  const requests: DetailRequest[] = [];
  const { controller, clock } = setup({ startDetail: async request => { requests.push(request); if (requests.length === 1) throw Error('lost send'); return ready(request); }, detailResult: async request => ({ apiVersion: 1, ...request, tagId: '', state: 'failed', detail: null, error: { code: 'detail_not_found', message: 'unknown' } }) });
  await controller.open(); await settle(); controller.selectTag('0'); await settle(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().detailError, 'detail_not_found'); assert.equal(requests.length, 1); assert.equal(clock.tasks.size, 0);
  controller.retryDetail(); await settle(); assert.deepEqual(requests[0], requests[1]); assert.equal(controller.getSnapshot().detailState, 'ready');
});

test('rapid selection clears old details and coalesces outstanding Starts into only the latest Tag', async () => {
  const old = deferred<DetailResponse>(); const requests: DetailRequest[] = [];
  const { controller } = setup({ startDetail: request => { requests.push(request); return requests.length === 1 ? old.promise : Promise.resolve(ready(request)); } });
  await controller.open(); await settle(); controller.selectTag('0'); controller.selectTag('1'); controller.selectTag('9007199254740993');
  assert.equal(requests.length, 1); assert.equal(controller.getSnapshot().detail, null);
  assert.equal(controller.getSnapshot().selectedTag?.id, '9007199254740993');
  old.resolve(ready(requests[0])); await settle();
  assert.deepEqual(requests.map(request => [request.requestId, request.tagId]), [['1', '0'], ['3', '9007199254740993']]);
  assert.equal(controller.getSnapshot().detailState, 'ready');
  controller.selectTag('9007199254740993'); assert.equal(requests.length, 2);
});

test('a hung old selection releases its wait at five seconds without recovering the superseded job', async () => {
  const old = deferred<DetailResponse>(); const requests: DetailRequest[] = []; let queries = 0;
  const { controller, clock } = setup({ startDetail: request => { requests.push(request); return requests.length === 1 ? old.promise : Promise.resolve(ready(request)); }, detailResult: async () => { queries++; return ready(requests[0]); } });
  await controller.open(); await settle(); controller.selectTag('0'); controller.selectTag('1'); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(requests.length, 2); assert.equal(queries, 0); assert.equal(controller.getSnapshot().detailState, 'ready');
  old.resolve(ready(requests[0])); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '1'); assert.equal(clock.tasks.size, 0);
});

test('close stops pending detail waits, forbids new selection, and old responses cannot overwrite a new file', async () => {
  const old = deferred<DetailResponse>(), closing = deferred<{ apiVersion: number; session: Session; error: null }>();
  const requests: DetailRequest[] = []; let opens = 0;
  const { controller, clock } = setup({
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'sample.flv', session: terminal(`scan-${++opens}`), error: null }),
    startDetail: request => { requests.push(request); return requests.length === 1 ? old.promise : Promise.resolve(ready(request)); },
    close: () => closing.promise,
  });
  await controller.open(); await settle(); controller.selectTag('0'); const close = controller.close(); controller.selectTag('1');
  assert.equal(controller.getSnapshot().selectedTag, null); assert.equal(requests.length, 1);
  closing.resolve({ apiVersion: 1, session: { ...terminal(), closed: true }, error: null }); await close;
  await controller.open(); await settle(); controller.selectTag('1'); await settle();
  assert.deepEqual(requests[1], { sessionId: 'scan-2', requestId: '1', tagId: '1' });
  old.resolve(ready(requests[0])); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '1');
  assert.equal(controller.getSnapshot().detailState, 'ready'); assert.equal(clock.tasks.size, 0);
});

test('tab/page selection and disposal clear detail timers and ignore all late outcomes', async () => {
  const old = deferred<DetailResponse>(); const requests: DetailRequest[] = [];
  const { controller, clock } = setup({ startDetail: request => { requests.push(request); return old.promise; } });
  await controller.open(); await settle(); controller.selectTag('0'); controller.select('diagnostics'); await settle();
  assert.equal(controller.getSnapshot().selectedTag, null); assert.equal(clock.tasks.size, 0);
  controller.select('tags'); await settle(); controller.selectTag('1'); controller.dispose(); assert.equal(clock.tasks.size, 0);
  const disposed = controller.getSnapshot(); old.resolve(ready(requests[0])); await settle(); assert.equal(controller.getSnapshot(), disposed);
});

test('detail envelope identity mismatches never expose data or start endless confirmation', async () => {
  for (const patch of [{ requestId: '999' }, { tagId: '999' }, { sessionId: 'another' }, { state: 'pending' as const, detail }]) {
    const { controller, clock } = setup({ startDetail: async request => ready(request, patch) });
    await controller.open(); await settle(); controller.selectTag('0'); await settle();
    assert.equal(controller.getSnapshot().detail, null); assert.equal(controller.getSnapshot().detailError, 'invalid_response'); assert.equal(clock.tasks.size, 0);
    controller.dispose();
  }
});

test('an authoritative retryable failure starts a new read identity while semantic failures have no retry', async () => {
  for (const code of ['detail_failed', 'deadline_exceeded', 'canceled', 'detail_unsupported', 'detail_filtered', 'detail_truncated', 'source_changed', 'tag_not_found']) {
    const requests: DetailRequest[] = [];
    const { controller } = setup({ startDetail: async request => { requests.push(request); return requests.length === 1 ? ready(request, { state: 'failed', detail: null, error: { code, message: 'failed' } }) : ready(request); } });
    await controller.open(); await settle(); controller.selectTag('0'); await settle();
    const retryable = ['detail_failed', 'deadline_exceeded', 'canceled'].includes(code);
    assert.equal(controller.getSnapshot().detailRetry, retryable ? 'reload' : '');
    controller.retryDetail(); await settle();
    assert.equal(requests.length, retryable ? 2 : 1);
    if (retryable) {
      assert.equal(requests[1].requestId, '2'); assert.equal(requests[1].tagId, '0');
      assert.equal(controller.getSnapshot().detailState, 'ready');
    }
    controller.dispose();
  }
});

test('a superseded Get reply and detail deadline are isolated from concurrent page work', async () => {
  const old = deferred<DetailResponse>(); const requests: DetailRequest[] = [];
  const { controller, clock } = setup({ startDetail: async request => { requests.push(request); return requests.length === 1 ? pending(request) : ready(request); }, detailResult: () => old.promise });
  await controller.open(); await settle(); controller.selectTag('0'); await settle(); clock.tick();
  controller.selectTag('1'); assert.equal(controller.getSnapshot().detail, null);
  clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().selectedTag?.id, '1'); assert.equal(controller.getSnapshot().detailState, 'ready');
  old.resolve(ready(requests[0], { state: 'failed', detail: null, error: { code: 'source_changed', message: 'late' } })); await settle();
  assert.equal(controller.getSnapshot().detailError, null); assert.equal(controller.getSnapshot().page?.tab, 'tags'); assert.equal(clock.tasks.size, 0);
});
import { unexpectedSummary } from './summary-fixture.ts';

test('AVC context continuation is manual, serialized and advances the read identity', async () => {
  const requests: DetailRequest[] = [], next = deferred<DetailResponse>();
  const { controller, clock } = setup({ startDetail: async request => {
    requests.push(request);
    return requests.length === 1 ? ready(request, { detail: replayDetail }) : next.promise;
  } });
  await controller.open(); await settle(); controller.selectTag('9007199254740993'); await settle();
  clock.tick(); await settle();
  assert.equal(requests.length, 1, 'no automatic full-file loop');
  controller.continueAVCContext(); controller.continueAVCContext();
  assert.equal(requests.length, 2);
  assert.deepEqual(requests[1], { ...requests[0], requestId: '2' });
  next.resolve(ready(requests[1])); await settle();
  controller.continueAVCContext(); await settle();
  assert.equal(requests.length, 2, 'completed/non-AVC details cannot continue');
  assert.equal(controller.getSnapshot().detailState, 'ready');
  controller.dispose();
});

test('lost AVC continuation reply confirms the same request before permitting more work', async () => {
  const requests: DetailRequest[] = [], queries: string[] = [];
  const { controller, clock } = setup({
    startDetail: async request => { requests.push(request); if (requests.length === 2) throw Error('lost reply'); return ready(request, { detail: replayDetail }); },
    detailResult: async request => { queries.push(request.requestId); return ready({ ...request, tagId: '9007199254740993' }); },
  });
  await controller.open(); await settle(); controller.selectTag('9007199254740993'); await settle();
  controller.continueAVCContext(); await settle(); controller.continueAVCContext();
  clock.tick(); await settle();
  assert.deepEqual(queries, ['2']); assert.equal(requests.length, 2);
  assert.equal(controller.getSnapshot().detailState, 'ready');
  controller.dispose();
});
