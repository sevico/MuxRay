import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, DETAIL_POLL_LIMIT, POLL_MS, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, DetailRequest, DetailResponse, HexRequest, HexResponse, Session, Tag } from '../src/protocol.ts';

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
const session = (id = 'scan-1', size = '10000', total = '205'): Session => ({ sessionId: id, state: 'finished', terminal: true, closed: false, scan: { complete: true, hasErrors: false }, failure: null, progress: { sourceSize: size, bytesScanned: size, tagCount: total, diagnosticCount: 0 } });
const detailReady = (request: DetailRequest): DetailResponse => ({ apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'aacAudio', complete: true, truncated: false, fields: [], diagnostics: [] } });
const ready = (request: HexRequest, size = '10000'): HexResponse => {
  const remain = BigInt(size) - BigInt(request.offset), length = remain < BigInt(request.count) ? Number(remain) : request.count;
  return { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: size, hex: 'ab'.repeat(length), eof: remain <= BigInt(request.count) } };
};
const pending = (request: HexRequest): HexResponse => ({ apiVersion: 1, ...request, state: 'pending', error: null, window: null });
const failed = (request: HexRequest, code: string): HexResponse => ({ apiVersion: 1, ...request, state: 'failed', error: { code, message: code }, window: null });
function tag(id: string): Tag { const span = { byteOffset: '13', bitOffset: 0, bitLength: 120 }; return { id, typeCode: 8, typeName: 'audio', offset: '13', span, headerSpan: span, payloadSpan: span, rawTypeFlags: 8, filter: false, dataSize: 4, availableDataSize: 4, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 15, expected: 15, valid: true } }; }
function setup(overrides: Partial<API> = {}, snapshot = session()) {
  const clock = new Clock();
  const api: API = {
    mediaCurves: async () => { throw Error('unexpected media curves'); },
    saveSnapshot: async () => { throw Error('unexpected save'); }, saveResult: async () => { throw Error('unexpected save confirmation'); },
    tagTrends: async () => { throw Error('unexpected trend query'); },
    filteredTags: async () => { throw Error('unexpected filtered tags'); },
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'bytes.flv', session: snapshot, error: null }),
    openResult: async () => { throw Error('unexpected open query'); }, status: async () => ({ apiVersion: 1, error: null, session: snapshot }),
    cancel: async () => ({ apiVersion: 1, error: null, session: snapshot }), close: async () => ({ apiVersion: 1, error: null, session: { ...snapshot, closed: true } }),
    tags: async request => { const total = BigInt(snapshot.progress!.tagCount), start = BigInt(request.start), count = Number(total - start > 100n ? 100n : total - start); return { apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, total: total.toString(), next: (start + BigInt(count)).toString(), limit: 100 }, tags: Array.from({ length: count }, (_, i) => tag((start + BigInt(i)).toString())) } }; },
    diagnostics: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, total: '0', next: '0', limit: 100 }, diagnostics: [] } }),
    startDetail: async request => detailReady(request), detailResult: async () => { throw Error('unexpected detail query'); },
    startHex: async request => ready(request, snapshot.progress!.sourceSize), hexResult: async () => { throw Error('unexpected Hex query'); }, ...unexpectedSummary, ...overrides,
  };
  return { controller: new Controller(api, clock), clock };
}

test('raw Tag location reads 512 bytes without starting detail and works beyond committed payloads', async () => {
  const starts: HexRequest[] = []; let details = 0;
  const { controller } = setup({ startHex: async request => { starts.push(request); return ready(request); }, startDetail: async request => { details++; return detailReady(request); } }, { ...session(), state: 'canceled', scan: { complete: false, hasErrors: false } });
  controller.jumpHex('13'); assert.equal(starts.length, 0);
  await controller.open(); await settle(); assert.equal(starts.length, 0);
  controller.showHex({ offset: '13', bitOffset: 0, bitLength: 8000000, label: 'declared large range' }); await settle();
  assert.equal(details, 0); assert.deepEqual(starts, [{ sessionId: 'scan-1', requestId: '1', offset: '13', count: 512 }]);
  assert.equal(controller.getSnapshot().hex?.hex.length, 1024);
  controller.jumpHex('9500'); await settle(); assert.equal(controller.getSnapshot().hex?.hex.length, 1000);
});
test('detail and Hex share increasing IDs; raw selection interrupts pending detail and permits explicit reload', async () => {
  const old = deferred<DetailResponse>(), details: DetailRequest[] = [], reads: HexRequest[] = [];
  const { controller, clock } = setup({ startDetail: request => { details.push(request); return details.length === 1 ? old.promise : Promise.resolve(detailReady(request)); }, startHex: async request => { reads.push(request); return ready(request); } });
  await controller.open(); await settle(); controller.selectTag('0'); controller.jumpHex('13'); await settle();
  assert.equal(details[0].requestId, '1'); assert.equal(reads[0].requestId, '2');
  assert.equal(controller.getSnapshot().detailError, 'detail_superseded'); assert.equal(controller.getSnapshot().detailRetry, 'reload');
  old.resolve(detailReady(details[0])); await settle(); assert.equal(controller.getSnapshot().detail, null);
  controller.retryDetail(); await settle(); assert.equal(details[1].requestId, '3'); assert.equal(controller.getSnapshot().hex, null);
  assert.equal(controller.getSnapshot().detailState, 'ready'); assert.equal(clock.tasks.size, 0);
});
test('completed fields remain visible when locating bytes in a previous AVC configuration', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.selectTag('105');
  controller.selectTag('0'); await settle(); const detail = controller.getSnapshot().detail;
  controller.showHex({ offset: '1', bitOffset: 6, bitLength: 2, label: 'earlier configuration' }); await settle();
  assert.equal(controller.getSnapshot().detail, detail); assert.equal(controller.getSnapshot().hex?.offset, '1');
  assert.equal(controller.getSnapshot().hexSelection?.bitOffset, 6);
});
test('a lost Hex Start is confirmed by Get using the same ID, with late Start ignored', async () => {
  const start = deferred<HexResponse>(); let request!: HexRequest; const queries: string[] = [];
  const { controller, clock } = setup({ startHex: value => { request = value; return start.promise; }, hexResult: async value => { queries.push(value.requestId); return ready(request); } });
  await controller.open(); await settle(); controller.jumpHex('24'); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().hexState, 'recovering'); clock.tick(); await settle();
  assert.deepEqual(queries, ['1']); assert.equal(controller.getSnapshot().hexState, 'ready');
  start.resolve(failed(request, 'hex_failed')); await settle(); assert.equal(controller.getSnapshot().hexState, 'ready'); assert.equal(clock.tasks.size, 0);
});
test('Hex pending polling stops at its limit and explicit confirmation keeps the ID', async () => {
  const starts: HexRequest[] = []; let queries = 0;
  const { controller, clock } = setup({ startHex: async request => { starts.push(request); return starts.length === 1 ? pending(request) : ready(request); }, hexResult: async () => { queries++; return pending(starts[0]); } });
  await controller.open(); await settle(); controller.jumpHex('0'); await settle();
  for (let i = 0; i < DETAIL_POLL_LIMIT; i++) { clock.tick(); await settle(); }
  assert.equal(queries, 25); assert.equal(clock.tasks.size, 0); assert.equal(controller.getSnapshot().hexRetry, 'confirm');
  controller.retryHex(); await settle(); assert.deepEqual(starts[0], starts[1]); assert.equal(controller.getSnapshot().hexState, 'ready');
});
test('lost Get stops after five seconds; confirmation and authoritative failure use appropriate IDs', async () => {
  const query = deferred<HexResponse>(); const starts: HexRequest[] = [];
  const { controller, clock } = setup({ startHex: async request => { starts.push(request); return starts.length === 1 ? pending(request) : starts.length === 2 ? failed(request, 'hex_failed') : ready(request); }, hexResult: () => query.promise });
  await controller.open(); await settle(); controller.jumpHex('1'); await settle(); clock.tick(); await settle(); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().hexRetry, 'confirm'); controller.retryHex(); await settle();
  assert.equal(controller.getSnapshot().hexRetry, 'reload'); controller.retryHex(); await settle();
  assert.deepEqual(starts.map(item => item.requestId), ['1', '1', '2']);
  query.resolve(pending(starts[0])); await settle(); assert.equal(controller.getSnapshot().hexState, 'ready');
});
test('rapid Hex jumps coalesce to the newest request and never confirm an obsolete one', async () => {
  const old = deferred<HexResponse>(), requests: HexRequest[] = [];
  const { controller, clock } = setup({ startHex: request => { requests.push(request); return requests.length === 1 ? old.promise : Promise.resolve(ready(request)); } });
  await controller.open(); await settle(); controller.jumpHex('1'); controller.jumpHex('2'); controller.jumpHex('3');
  assert.equal(requests.length, 1); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.deepEqual(requests.map(item => [item.requestId, item.offset]), [['1', '1'], ['3', '3']]);
  old.resolve(ready(requests[0])); await settle(); assert.equal(controller.getSnapshot().hex?.offset, '3');
});
test('Hex replies cannot overwrite a new Tag, page, closed file or disposed controller', async () => {
  for (const action of ['tag', 'page', 'close', 'dispose']) {
    const old = deferred<HexResponse>(); let request!: HexRequest;
    const { controller, clock } = setup({ startHex: value => { request = value; return old.promise; } });
    await controller.open(); await settle(); controller.jumpHex('13');
    if (action === 'tag') controller.selectTag('0'); else if (action === 'page') controller.select('tags', '100'); else if (action === 'close') await controller.close(); else controller.dispose();
    await settle(); old.resolve(ready(request)); await settle();
    assert.equal(controller.getSnapshot().hex, null); assert.equal(clock.tasks.size, 0);
    if (action === 'close') { await controller.open(); await settle(); assert.equal(controller.getSnapshot().hex, null); }
  }
});
test('Hex admission errors retain their real code with zero normalized count', async () => {
  for (const offset of ['', '13']) {
    const { controller } = setup({ startHex: async request => ({ ...failed(request, 'hex_unsupported'), offset, count: 0 }) });
    await controller.open(); await settle(); controller.jumpHex('13'); await settle(); assert.equal(controller.getSnapshot().hexError, 'hex_unsupported');
  }
});
test('Hex identity, bounds and malformed windows are rejected before display', async () => {
  const patches: ((request: HexRequest) => HexResponse)[] = [
    request => ({ ...ready(request), sessionId: 'old' }), request => ({ ...ready(request), requestId: '99' }),
    request => ({ ...ready(request), offset: '9' }), request => ({ ...ready(request), count: 4096 }),
    request => ({ ...ready(request), window: { offset: request.offset, sourceSize: '10000', hex: 'ab', eof: false } }),
    request => ({ ...failed(request, 'hex_unsupported'), offset: '99', count: 0 }),
  ];
  for (const patch of patches) {
    const { controller } = setup({ startHex: async request => patch(request) }); await controller.open(); await settle(); controller.jumpHex('13'); await settle();
    assert.equal(controller.getSnapshot().hex, null); assert.equal(controller.getSnapshot().hexError, 'invalid_response'); assert.equal(controller.getSnapshot().hexRetry, '');
  }
});
test('source change from either read kind clears both views and blocks reads until close/reopen', async () => {
  for (const kind of ['hex', 'detail']) {
    let reads = 0;
    const { controller } = setup({ startHex: async request => { reads++; return kind === 'hex' ? failed(request, 'source_changed') : ready(request); }, startDetail: async request => kind === 'detail' ? { ...detailReady(request), state: 'failed', detail: null, error: { code: 'source_changed', message: '' } } : detailReady(request) });
    await controller.open(); await settle();
    if (kind === 'hex') { controller.selectTag('0'); await settle(); controller.jumpHex('13'); } else { controller.jumpHex('13'); await settle(); controller.selectTag('0'); }
    await settle(); assert.equal(controller.getSnapshot().sourceChanged, true); assert.equal(controller.getSnapshot().detail, null); assert.equal(controller.getSnapshot().hex, null);
    const count = reads; controller.jumpHex('1'); controller.retry(); controller.selectTag('1'); await settle(); assert.equal(reads, count);
    await controller.close(); await controller.open(); await settle(); assert.equal(controller.getSnapshot().sourceChanged, false);
  }
});
test('exact Hex navigation preserves selection across windows and permits empty EOF', async () => {
  const size = '9007199254742000', { controller } = setup({}, session('scan-1', size));
  await controller.open(); await settle(); controller.showHex({ offset: '9007199254740993', bitOffset: 6, bitLength: 9000, label: 'large field' }); await settle();
  const selected = controller.getSnapshot().hexSelection; controller.pageHex(1); await settle();
  assert.equal(controller.getSnapshot().hex?.offset, '9007199254741505'); assert.equal(controller.getSnapshot().hexSelection, selected);
  controller.jumpHex(size); await settle(); assert.equal(controller.getSnapshot().hex?.hex, ''); assert.equal(controller.getSnapshot().hex?.eof, true);
  controller.jumpHex('9007199254742001'); assert.equal(controller.getSnapshot().hexError, 'invalid_argument');
});
test('Tag jump reads just one target page, selects the ID and rejects out-of-range IDs', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.jumpTag('105'); await settle();
  assert.equal(controller.getSnapshot().start, '100'); assert.equal(controller.getSnapshot().selectedTag?.id, '105'); assert.equal(controller.getSnapshot().detailState, 'ready');
  for (const value of ['205', '-1', '1.5', '0105']) { controller.jumpTag(value); assert.equal(controller.getSnapshot().tagJumpError, 'tag_not_found'); }
  const huge = setup({}, session('scan-1', '10000', '9007199254741000')); await huge.controller.open(); await settle(); huge.controller.jumpTag('9007199254740993'); await settle();
  assert.equal(huge.controller.getSnapshot().start, '9007199254740900'); assert.equal(huge.controller.getSnapshot().selectedTag?.id, '9007199254740993');
});
import { unexpectedSummary } from './summary-fixture.ts';
