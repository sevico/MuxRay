import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, POLL_MS, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, FilteredTagPage, FilteredTagRequest, PageResponse, Session, Tag, TagFilter } from '../src/protocol.ts';
import { EMPTY_TAG_FILTER, FILTER_HISTORY_LIMIT, validateFilteredTagPage, validateTagFilter } from '../src/tag-filter.ts';
import { filteredTagResponse } from '../src/normalize.ts';
import { summaryFailed, summaryPage, summaryReady } from './summary-fixture.ts';

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
function tag(id: string, typeCode = 8, dts = '0'): Tag {
  const span = { byteOffset: '13', bitOffset: 0, bitLength: 8 };
  return { id, typeCode, typeName: typeCode === 8 ? 'audio' : 'video', offset: '13', span, headerSpan: span, payloadSpan: span, rawTypeFlags: typeCode, filter: false, dataSize: 1, availableDataSize: 1, dts, streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 12, expected: 12, valid: true } };
}
const audio: TagFilter = { ...EMPTY_TAG_FILTER, type: 'audio' };
function filtered(request: FilteredTagRequest, total = '8305'): PageResponse<FilteredTagPage> {
  const start = BigInt(request.start), end = start + 4096n < BigInt(total) ? start + 4096n : BigInt(total);
  const tags = ['4500', '5000', '8200'].filter(id => BigInt(id) >= start && BigInt(id) < end).map(id => tag(id, request.filter.type === 'video' ? 9 : 8));
  return { apiVersion: 1, error: null, page: { sessionId: request.sessionId, filter: request.filter, start: request.start, next: end.toString(), indexedTotal: total, limit: request.count, scanLimit: 4096, scanned: Number(end - start), exhausted: end === BigInt(total), tags } };
}
function setup(overrides: Partial<API> = {}, total = '8305') {
  const clock = new Clock(), requests: FilteredTagRequest[] = [];
  let opens = 0;
  const session = (id = `scan-${opens}`): Session => ({ sessionId: id, state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null, progress: { sourceSize: '9007199254742000', bytesScanned: '10000', tagCount: total, diagnosticCount: 0 } });
  const api: API = {
    mediaCurves: async () => { throw Error('unexpected media curves'); },
    saveSnapshot: async () => { throw Error('unexpected save'); }, saveResult: async () => { throw Error('unexpected save confirmation'); },
    tagTrends: async () => { throw Error('unexpected trend query'); },
    open: async requestId => { opens++; return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'filtered.flv', session: session(), error: null }; },
    openResult: async () => { throw Error('Unexpected open recovery'); },
    status: async id => ({ apiVersion: 1, session: session(id), error: null }), cancel: async id => ({ apiVersion: 1, session: session(id), error: null }),
    close: async id => ({ apiVersion: 1, session: { ...session(id), closed: true }, error: null }),
    tags: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: (BigInt(request.start) + 100n).toString(), total, limit: 100 }, tags: [tag(request.start)] } }),
    filteredTags: async request => { requests.push(request); return filtered(request, total); },
    diagnostics: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: '0', total: '0', limit: 100 }, diagnostics: [] } }),
    startDetail: async request => ({ apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'aacAudio', complete: true, truncated: false, fields: [], diagnostics: [] } }),
    detailResult: async () => { throw Error('Unexpected detail poll'); },
    startHex: async request => ({ apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: session().progress!.sourceSize, hex: 'aa'.repeat(512), eof: false } }),
    hexResult: async () => { throw Error('Unexpected Hex poll'); },
    startSummary: async request => summaryReady(request), summaryResult: async request => summaryReady(request), cancelSummary: async request => summaryFailed(request, 'canceled'), summaryEvents: async request => summaryPage(request),
    ...overrides,
  };
  return { controller: new Controller(api, clock), clock, requests };
}

test('filter range is inclusive uint32 text and rejects noncanonical or reversed input', () => {
  for (const filter of [EMPTY_TAG_FILTER, { ...audio, dtsMin: '0', dtsMax: '4294967295' }, { ...audio, dtsMin: '42', dtsMax: '42' }]) assert.doesNotThrow(() => validateTagFilter(filter));
  for (const text of ['01', '+1', '-1', '1.0', '1e3', ' 1', '', '4294967296']) assert.throws(() => validateTagFilter({ ...audio, dtsMin: text }));
  assert.throws(() => validateTagFilter({ ...audio, dtsMin: '2', dtsMax: '1' }));
});
test('filtered normalization preserves exact coordinates and maps nullable generated fields', () => {
  const request = { sessionId: 's', start: '9007199254740993', count: 100, filter: audio };
  const response = filtered(request, '9007199254740994');
  response.page!.tags = [tag(request.start)];
  const wire = JSON.parse(JSON.stringify(response)); delete wire.page.filter.dtsMin; delete wire.page.filter.dtsMax; delete wire.page.tags[0].previousTagSize.span;
  const normalized = filteredTagResponse(wire);
  assert.equal(normalized.page!.start, request.start); assert.equal(normalized.page!.filter.dtsMin, null); assert.equal(normalized.page!.tags[0].previousTagSize.span, null);
  validateFilteredTagPage(normalized.page!, request);
  assert.equal(filteredTagResponse(JSON.parse('{"apiVersion":1}')).page, null);
});
test('filtered protocol rejects forged coverage, type, coordinates and out-of-order matches', () => {
  const request = { sessionId: 's', start: '4096', count: 100, filter: audio };
  for (const mutate of [
    (p: FilteredTagPage) => { p.next = '4097'; }, (p: FilteredTagPage) => { p.scanned = 1; },
    (p: FilteredTagPage) => { p.scanLimit = 9999; }, (p: FilteredTagPage) => { p.exhausted = true; },
    (p: FilteredTagPage) => { p.filter = { ...audio, type: 'video' }; }, (p: FilteredTagPage) => { p.tags.reverse(); },
    (p: FilteredTagPage) => { p.tags[0].id = '8192'; }, (p: FilteredTagPage) => { p.tags[0].typeCode = 9; },
    (p: FilteredTagPage) => { p.tags[0].dts = '4294967296'; },
  ]) { const page = filtered(request).page!; mutate(page); assert.throws(() => validateFilteredTagPage(page, request)); }
});
test('sparse filter requires explicit continuation and uses scan cursors for forward/back navigation', async () => {
  const { controller, requests } = setup(); await controller.open(); await settle(); controller.applyTagFilter(audio); await settle();
  assert.equal(requests.length, 1); assert.equal(controller.getSnapshot().filteredPage?.next, '4096'); assert.equal(controller.getSnapshot().filteredPage?.exhausted, false); assert.deepEqual(controller.getSnapshot().filteredPage?.tags, []);
  assert.equal(controller.getSnapshot().session?.scan?.complete, false);
  controller.pageFilteredTags('next'); await settle(); assert.equal(controller.getSnapshot().filteredPage?.start, '4096'); assert.deepEqual(controller.getSnapshot().filteredPage?.tags.map(t => t.id), ['4500', '5000']);
  controller.pageFilteredTags('next'); await settle(); assert.equal(controller.getSnapshot().filteredPage?.start, '8192'); assert.equal(controller.getSnapshot().filteredPage?.exhausted, true);
  controller.pageFilteredTags('next'); await settle(); assert.equal(requests.length, 3);
  controller.pageFilteredTags('previous'); await settle(); assert.equal(controller.getSnapshot().filteredPage?.start, '4096');
  controller.pageFilteredTags('first'); await settle(); assert.equal(controller.getSnapshot().filterPrevious, null); assert.equal(controller.getSnapshot().start, '0'); controller.dispose();
});
test('invalid application preserves previous results and makes no query', async () => {
  const { controller, requests } = setup(); await controller.open(); await settle(); controller.applyTagFilter(audio); await settle();
  const page = controller.getSnapshot().filteredPage;
  controller.applyTagFilter({ ...audio, dtsMin: '5', dtsMax: '4' }); await settle();
  assert.equal(controller.getSnapshot().filterError, 'invalid_filter'); assert.equal(controller.getSnapshot().filteredPage, page); assert.equal(requests.length, 1); controller.dispose();
});
test('rapid filter changes coalesce, fence old replies and preserve latest applied criteria', async () => {
  const old = deferred<PageResponse<FilteredTagPage>>(); const requests: FilteredTagRequest[] = [];
  const { controller } = setup({ filteredTags: request => { requests.push(request); return requests.length === 1 ? old.promise : Promise.resolve(filtered(request)); } });
  await controller.open(); await settle(); controller.applyTagFilter(audio);
  controller.applyTagFilter({ ...audio, dtsMin: '1' }); controller.applyTagFilter({ ...audio, type: 'video' });
  assert.equal(requests.length, 1); old.resolve(filtered(requests[0])); await settle();
  assert.equal(requests.length, 2); assert.equal(requests[1].filter.type, 'video'); assert.equal(controller.getSnapshot().filteredPage?.filter.type, 'video'); assert.equal(controller.getSnapshot().error, null); controller.dispose();
});
test('late filter reply cannot overwrite close, a new session, reset or source-change latch', async () => {
  for (const action of ['close', 'reset', 'source'] as const) {
    const old = deferred<PageResponse<FilteredTagPage>>(); let sent!: FilteredTagRequest;
    const { controller } = setup({ filteredTags: request => { sent = request; return old.promise; }, startHex: async request => ({ apiVersion: 1, ...request, state: 'failed', window: null, error: { code: 'source_changed', message: 'Changed' } }) });
    await controller.open(); await settle(); controller.applyTagFilter(audio);
    if (action === 'close') { await controller.close(); await controller.open(); }
    if (action === 'reset') controller.resetTagFilter();
    if (action === 'source') { controller.jumpHex('13'); await settle(); }
    old.resolve(filtered(sent)); await settle();
    assert.equal(controller.getSnapshot().tagFilter, null); assert.equal(controller.getSnapshot().filteredPage, null);
    if (action === 'close') assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
    if (action === 'reset') assert.equal(controller.getSnapshot().page?.tab, 'tags');
    if (action === 'source') { assert.equal(controller.getSnapshot().sourceChanged, true); assert.equal(controller.getSnapshot().page, null); }
    controller.dispose();
  }
});
test('lost filter reply is retryable with same cursor and late callback stays fenced', async () => {
  const old = deferred<PageResponse<FilteredTagPage>>(); let first!: FilteredTagRequest; let calls = 0;
  const { controller, clock } = setup({ filteredTags: request => { if (++calls === 1) { first = request; return old.promise; } return Promise.resolve(filtered(request)); } });
  await controller.open(); await settle(); controller.applyTagFilter(audio); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().error, 'transport_failed'); controller.retry(); clock.tick(); await settle();
  assert.equal(calls, 2); assert.equal(controller.getSnapshot().filteredPage?.start, '0');
  const accepted = controller.getSnapshot().filteredPage; old.resolve({ ...filtered(first), error: { code: 'source_changed', message: 'Late error' } }); await settle();
  assert.equal(controller.getSnapshot().filteredPage, accepted); assert.equal(controller.getSnapshot().sourceChanged, false); controller.dispose();
});
test('filtered result detail and Hex retain Summary; exact Tag navigation exits the filter', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.startSummary(); await settle(); const summary = controller.getSnapshot().summary;
  controller.applyTagFilter(audio); await settle(); controller.pageFilteredTags('next'); await settle(); const page = controller.getSnapshot().filteredPage;
  controller.selectTag('4500'); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '4500'); assert.equal(controller.getSnapshot().detailState, 'ready');
  controller.jumpHex('13'); await settle(); assert.equal(controller.getSnapshot().hexState, 'ready'); assert.equal(controller.getSnapshot().filteredPage, page); assert.equal(controller.getSnapshot().summary, summary);
  controller.jumpTag('4500'); await settle(); assert.equal(controller.getSnapshot().tagFilter, null); assert.equal(controller.getSnapshot().selectedTag?.id, '4500'); assert.equal(controller.getSnapshot().summary, summary); controller.dispose();
});
test('filter history has a fixed memory bound and first resets limited navigation', async () => {
  const { controller } = setup({ filteredTags: async request => { const value = filtered(request, '2000000'); value.page!.tags = []; return value; } }, '2000000');
  await controller.open(); await settle(); controller.applyTagFilter(audio); await settle();
  for (let i = 0; i < FILTER_HISTORY_LIMIT + 3; i++) { controller.pageFilteredTags('next'); await settle(); }
  assert.equal(controller.getSnapshot().filterHistoryLimited, true);
  for (let i = 0; i < FILTER_HISTORY_LIMIT; i++) { controller.pageFilteredTags('previous'); await settle(); }
  assert.equal(controller.getSnapshot().filterPrevious, null); assert.equal(controller.getSnapshot().start, String(3 * 4096));
  controller.pageFilteredTags('first'); await settle(); assert.equal(controller.getSnapshot().start, '0'); assert.equal(controller.getSnapshot().filterHistoryLimited, false); controller.dispose();
});
