import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, POLL_MS, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, PageRequest, PageResponse, Session, Tag, TagTrendPage, TagTrendPoint } from '../src/protocol.ts';
import { TREND_PAGE_SIZE, validateTagTrendPage } from '../src/tag-trend-protocol.ts';
import { tagTrendResponse } from '../src/normalize.ts';
import { summaryFailed, summaryPage, summaryPending, summaryReady } from './summary-fixture.ts';
import { EMPTY_TAG_FILTER } from '../src/tag-filter.ts';

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
const dts = (id: bigint) => id % 21n * 10n;
function point(id: bigint): TagTrendPoint {
  const previous = id >= 2n ? id - 2n : null;
  return { tagId: id.toString(), offset: (13n + id * 20n).toString(), dts: dts(id).toString(), typeCode: id % 2n === 0n ? 8 : 9, streamId: 0,
    dataSize: 4, availableDataSize: 4, payloadComplete: true, previousTagSizeState: 'valid', filtered: false,
    previousTagId: previous?.toString() ?? null, previousDts: previous === null ? null : dts(previous).toString(), deltaMs: previous === null ? null : (dts(id) - dts(previous)).toString(), context: previous === null ? 'start' : 'found' };
}
function trend(request: PageRequest, total = '530'): PageResponse<TagTrendPage> {
  const start = BigInt(request.start), length = BigInt(total) - start < BigInt(request.count) ? BigInt(total) - start : BigInt(request.count);
  const contextScanned = length === 0n ? 0 : Number(start > 2n ? 2n : start);
  return { apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: (start + length).toString(), total, limit: request.count }, contextStart: (start - BigInt(contextScanned)).toString(), contextScanned,
    points: Array.from({ length: Number(length) }, (_, i) => point(start + BigInt(i))) } };
}
function tag(id: string): Tag {
  const value = point(BigInt(id)), span = { byteOffset: value.offset, bitOffset: 0, bitLength: 120 };
  return { id, typeCode: value.typeCode, typeName: 'audio', offset: value.offset, span, headerSpan: { ...span, bitLength: 88 }, payloadSpan: { byteOffset: (BigInt(value.offset) + 11n).toString(), bitOffset: 0, bitLength: 32 },
    rawTypeFlags: value.typeCode, filter: false, dataSize: 4, availableDataSize: 4, dts: value.dts, streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 15, expected: 15, valid: true } };
}
function setup(overrides: Partial<API> = {}, total = '530') {
  const clock = new Clock(), requests: PageRequest[] = [], reads: { kind: string; id: string }[] = [];
  let opens = 0;
  const sourceSize = (BigInt(total) * 20n + 1000n).toString();
  const session = (id = `scan-${opens}`): Session => ({ sessionId: id, state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null, progress: { sourceSize, bytesScanned: sourceSize, tagCount: total, diagnosticCount: 0 } });
  const api: API = {
    mediaCurves: async () => { throw Error('unexpected media curves'); },
    saveSnapshot: async () => { throw Error('unexpected save'); }, saveResult: async () => { throw Error('unexpected save confirmation'); },
    open: async requestId => { opens++; return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'trends.flv', session: session(), error: null }; },
    openResult: async () => { throw Error('Unexpected open recovery'); }, status: async id => ({ apiVersion: 1, session: session(id), error: null }), cancel: async id => ({ apiVersion: 1, session: session(id), error: null }),
    close: async id => ({ apiVersion: 1, session: { ...session(id), closed: true }, error: null }),
    tags: async request => { const start = BigInt(request.start), length = Number(BigInt(total) - start < 100n ? BigInt(total) - start : 100n); return { apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: (start + BigInt(length)).toString(), total, limit: 100 }, tags: Array.from({ length }, (_, i) => tag((start + BigInt(i)).toString())) } }; },
    filteredTags: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, filter: request.filter, start: '0', next: '100', indexedTotal: total, limit: 100, scanLimit: 4096, scanned: 100, exhausted: false, tags: Array.from({ length: 100 }, (_, i) => tag(String(i))) } }),
    tagTrends: async request => { requests.push(request); return trend(request, total); },
    diagnostics: async request => ({ apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: '0', total: '0', limit: 100 }, diagnostics: [] } }),
    startDetail: async request => { reads.push({ kind: 'detail', id: request.tagId }); return { apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'aacAudio', complete: true, truncated: false, fields: [], diagnostics: [] } }; },
    detailResult: async () => { throw Error('Unexpected detail poll'); },
    startHex: async request => { reads.push({ kind: 'hex', id: request.offset }); return { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize, hex: 'aa'.repeat(512), eof: false } }; },
    hexResult: async () => { throw Error('Unexpected Hex poll'); },
    startSummary: async request => summaryReady(request), summaryResult: async request => summaryReady(request), cancelSummary: async request => summaryFailed(request, 'canceled'), summaryEvents: async request => summaryPage(request), ...overrides,
  };
  return { controller: new Controller(api, clock), clock, requests, reads };
}

test('trend protocol retains precise uint64 positions and signed raw-DTS regressions', () => {
  const request = { sessionId: 's', start: '9007199254740993', count: 2 };
  const value = trend(request, '9007199254740995').page!;
  validateTagTrendPage(value, request); assert.equal(value.points[0].tagId, request.start); assert.ok(BigInt(value.points[0].offset) > BigInt(Number.MAX_SAFE_INTEGER));
  const rollback = trend({ ...request, start: '20', count: 3 }, '23').page!;
  validateTagTrendPage(rollback, { ...request, start: '20', count: 3 }); assert.equal(rollback.points[1].deltaMs, '-190');
  rollback.points[1].dts = '0'; rollback.points[1].previousDts = '4294967295'; rollback.points[1].deltaMs = '-4294967295';
  validateTagTrendPage(rollback, { ...request, start: '20', count: 3 });
});
test('trend normalization restores nullable generated fields and rejects unknown enums', () => {
  const request = { sessionId: 's', start: '0', count: 2 }, raw = JSON.parse(JSON.stringify(trend(request)));
  for (const point of raw.page.points) { delete point.previousTagId; delete point.previousDts; delete point.deltaMs; }
  const value = tagTrendResponse(raw); validateTagTrendPage(value.page!, request);
  assert.equal(value.page!.points[0].deltaMs, null); assert.equal(tagTrendResponse(JSON.parse('{"apiVersion":1}')).page, null);
  raw.page.points[0].context = 'unknown'; assert.throws(() => tagTrendResponse(raw)); raw.page.points[0].context = 'start'; raw.page.points[0].previousTagSizeState = 'unknown'; assert.throws(() => tagTrendResponse(raw));
});
test('trend protocol rejects forged windows, track predecessors and numeric values', () => {
  const request = { sessionId: 's', start: '10', count: 3 };
  for (const mutate of [
    (p: TagTrendPage) => { p.bounds.limit = 513; }, (p: TagTrendPage) => { p.bounds.next = '12'; }, (p: TagTrendPage) => { p.contextStart = '9'; },
    (p: TagTrendPage) => { p.contextScanned = 4097; }, (p: TagTrendPage) => { p.points[1].tagId = '10'; }, (p: TagTrendPage) => { p.points[1].offset = p.points[0].offset; },
    (p: TagTrendPage) => { p.points[0].dts = '4294967296'; }, (p: TagTrendPage) => { p.points[0].dataSize = 0x1000000; }, (p: TagTrendPage) => { p.points[0].streamId = -1; },
    (p: TagTrendPage) => { p.points[0].availableDataSize = 5; }, (p: TagTrendPage) => { p.points[0].payloadComplete = false; },
    (p: TagTrendPage) => { p.points[0].previousTagId = '7'; }, (p: TagTrendPage) => { p.points[0].deltaMs = '-0'; },
    (p: TagTrendPage) => { p.points[2].previousTagId = '11'; }, (p: TagTrendPage) => { p.points[2].previousDts = '0'; },
    (p: TagTrendPage) => { p.points[0].context = 'start'; }, (p: TagTrendPage) => { p.points[0].previousDts = null; },
  ]) { const value = trend(request).page!; mutate(value); assert.throws(() => validateTagTrendPage(value, request)); }
  assert.throws(() => validateTagTrendPage(trend(request).page!, request, '100'));
  for (const field of ['tagId', 'offset', 'previousTagId', 'dts']) {
    const value = JSON.parse(JSON.stringify(trend(request).page)); value.points[0][field] = Number(value.points[0][field]);
    assert.throws(() => validateTagTrendPage(value, request));
  }
  for (const field of ['start', 'next', 'total']) {
    const value = JSON.parse(JSON.stringify(trend(request).page)); value.bounds[field] = Number(value.bounds[field]);
    assert.throws(() => validateTagTrendPage(value, request));
  }
  const value = JSON.parse(JSON.stringify(trend(request).page)); value.contextStart = Number(value.contextStart);
  assert.throws(() => validateTagTrendPage(value, request));
});
test('missing trend context distinguishes index beginning from exhausted lookback and empty EOF', () => {
  const request = { sessionId: 's', start: '5000', count: 3 }, value = trend(request, '5003').page!;
  value.contextStart = '904'; value.contextScanned = 4096;
  for (const point of value.points.slice(0, 2)) { point.context = 'budget_exhausted'; point.previousTagId = null; point.previousDts = null; point.deltaMs = null; }
  validateTagTrendPage(value, request);
  value.contextScanned--; value.contextStart = '905'; assert.throws(() => validateTagTrendPage(value, request));
  const eofRequest = { ...request, start: '5003' }; validateTagTrendPage(trend(eofRequest, '5003').page!, eofRequest);
});
test('trends load explicitly, page continuous windows and accept partial index coverage', async () => {
  const { controller, requests } = setup(); await controller.open(); await settle(); assert.equal(requests.length, 0);
  controller.loadTagTrends(); await settle(); assert.equal(requests[0].count, TREND_PAGE_SIZE); assert.equal(controller.getSnapshot().trendPage?.points.length, 256); assert.equal(controller.getSnapshot().session?.scan?.complete, false);
  controller.pageTagTrends('next'); await settle(); assert.equal(controller.getSnapshot().trendPage?.bounds.start, '256');
  controller.pageTagTrends('next'); await settle(); assert.equal(controller.getSnapshot().trendPage?.points.length, 18);
  controller.pageTagTrends('next'); await settle(); assert.equal(requests.length, 3);
  controller.pageTagTrends('previous'); await settle(); assert.equal(controller.getSnapshot().trendStart, '256');
  controller.pageTagTrends('first'); await settle(); assert.equal(controller.getSnapshot().trendStart, '0');
  controller.loadTagTrends('531'); assert.equal(controller.getSnapshot().trendError, 'invalid_argument'); assert.equal(requests.length, 5);
  controller.loadTagTrends('530'); await settle(); assert.equal(controller.getSnapshot().trendPage?.points.length, 0); controller.dispose();
});
test('rapid trend window intents coalesce independently of Tag pagination and fence older data', async () => {
  const old = deferred<PageResponse<TagTrendPage>>(), calls: PageRequest[] = [];
  const { controller } = setup({ tagTrends: request => { calls.push(request); return calls.length === 1 ? old.promise : Promise.resolve(trend(request)); } });
  await controller.open(); await settle(); controller.loadTagTrends(); controller.loadTagTrends('256'); controller.loadTagTrends('512');
  controller.select('tags', '100'); await settle(); assert.equal(controller.getSnapshot().page?.data.bounds.start, '100'); assert.equal(calls.length, 1);
  old.resolve(trend(calls[0])); await settle(); assert.deepEqual(calls.map(c => c.start), ['0', '512']); assert.equal(controller.getSnapshot().trendPage?.bounds.start, '512'); controller.dispose();
});
test('trend timeout stops until retry, keeps the same window and ignores its late callback', async () => {
  const old = deferred<PageResponse<TagTrendPage>>(); let first!: PageRequest; let calls = 0;
  const { controller, clock } = setup({ tagTrends: request => { if (++calls === 1) { first = request; return old.promise; } return Promise.resolve(trend(request)); } });
  await controller.open(); await settle(); controller.loadTagTrends('256'); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().trendError, 'transport_failed'); assert.equal(controller.getSnapshot().trendLoading, false); assert.equal(calls, 1);
  controller.retryTagTrends(); await settle(); const page = controller.getSnapshot().trendPage; assert.equal(calls, 2); assert.equal(page?.bounds.start, '256');
  old.resolve({ ...trend(first), error: { code: 'source_changed', message: 'Late' } }); await settle(); assert.equal(controller.getSnapshot().trendPage, page); assert.equal(controller.getSnapshot().sourceChanged, false); controller.dispose();
});
test('close, reopen, disposal and source-change invalidate trend waits and late responses', async () => {
  for (const action of ['close', 'dispose', 'source'] as const) {
    const old = deferred<PageResponse<TagTrendPage>>(); let request!: PageRequest;
    const { controller, clock } = setup({ tagTrends: sent => { request = sent; return old.promise; }, startHex: async sent => ({ apiVersion: 1, ...sent, state: 'failed', window: null, error: { code: 'source_changed', message: 'Changed' } }) });
    await controller.open(); await settle(); controller.loadTagTrends();
    if (action === 'close') { await controller.close(); await controller.open(); }
    if (action === 'dispose') controller.dispose();
    if (action === 'source') { controller.jumpHex('13'); await settle(); }
    old.resolve(trend(request)); await settle(); assert.equal(controller.getSnapshot().trendPage, null);
    if (action === 'close') assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
    if (action === 'source') { assert.equal(controller.getSnapshot().sourceChanged, true); assert.equal(controller.getSnapshot().trendLoading, false); }
    assert.equal(clock.tasks.size, 0); controller.dispose();
  }
});
test('malformed trend replies never publish points and only a valid active source-change error latches', async () => {
  for (const bad of ['session', 'count', 'total', 'mixed-error', 'source'] as const) {
    const { controller } = setup({ tagTrends: async request => {
      const response = trend(request);
      if (bad === 'session') response.page!.sessionId = 'other';
      if (bad === 'count') response.page!.points.pop();
      if (bad === 'total') response.page!.bounds.total = '531';
      if (bad === 'source' || bad === 'mixed-error') response.error = { code: 'source_changed', message: 'Changed' };
      if (bad === 'source') response.page = null;
      return response;
    } });
    await controller.open(); await settle(); controller.loadTagTrends(); await settle(); assert.equal(controller.getSnapshot().trendPage, null);
    assert.equal(controller.getSnapshot().sourceChanged, bad === 'source'); if (bad !== 'source') assert.equal(controller.getSnapshot().trendError, 'invalid_response'); controller.dispose();
  }
});
test('trend queries preserve filter, details and Summary; selecting a point exits filter and preserves its trend window', async () => {
  const { controller, reads } = setup(); await controller.open(); await settle(); controller.startSummary(); await settle(); const summary = controller.getSnapshot().summary;
  controller.applyTagFilter(EMPTY_TAG_FILTER); await settle(); controller.selectTag('0'); await settle(); const detail = controller.getSnapshot().detail, filtered = controller.getSnapshot().filteredPage;
  controller.loadTagTrends(); await settle(); assert.equal(controller.getSnapshot().detail, detail); assert.equal(controller.getSnapshot().filteredPage, filtered); assert.equal(controller.getSnapshot().summary, summary);
  const trend = controller.getSnapshot().trendPage; controller.selectTrendTag('105'); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '105'); assert.equal(controller.getSnapshot().trendSelectedId, '105'); assert.equal(controller.getSnapshot().tagFilter, null); assert.equal(controller.getSnapshot().trendPage, trend); assert.equal(controller.getSnapshot().summary, summary);
  controller.pageTagTrends('next'); await settle(); assert.equal(controller.getSnapshot().trendSelectedId, null); assert.equal(controller.getSnapshot().selectedTag?.id, '105'); assert.deepEqual(reads.map(r => r.id), ['0', '105']); controller.dispose();
});
test('trend Hex uses actual truncated payload range directly and only preserves the same Tag detail', async () => {
  const { controller, reads } = setup({ tagTrends: async request => { const response = trend(request); response.page!.points[1].dataSize = 20; response.page!.points[1].availableDataSize = 2; response.page!.points[1].payloadComplete = false; return response; } });
  await controller.open(); await settle(); controller.loadTagTrends(); await settle(); controller.showTrendHex('1', 'payload'); await settle();
  assert.equal(controller.getSnapshot().hexSelection?.offset, '44'); assert.equal(controller.getSnapshot().hexSelection?.bitLength, 16); assert.equal(controller.getSnapshot().selectedTag, null); assert.deepEqual(reads.map(r => r.kind), ['hex']);
  controller.selectTrendTag('1'); await settle(); const detail = controller.getSnapshot().detail; controller.showTrendHex('1', 'tag'); await settle();
  assert.equal(controller.getSnapshot().detail, detail); assert.equal(controller.getSnapshot().hexSelection?.offset, '33'); assert.equal(controller.getSnapshot().hexSelection?.bitLength, 104);
  controller.showTrendHex('2', 'payload'); await settle(); assert.equal(controller.getSnapshot().detail, null); assert.equal(controller.getSnapshot().trendSelectedId, '2'); controller.dispose();
});
test('metadata trends remain available during Summary without taking a source request identity', async () => {
  const { controller, reads, requests } = setup({ startSummary: async request => summaryPending(request) });
  await controller.open(); await settle(); controller.startSummary(); await settle(); controller.loadTagTrends(); await settle();
  assert.equal(requests.length, 1); assert.equal(controller.getSnapshot().summaryRunning, true); assert.equal(controller.getSnapshot().trendPage?.points.length, 256);
  controller.selectTrendTag('1'); controller.showTrendHex('1', 'tag'); await settle(); assert.equal(reads.length, 0); assert.equal(controller.getSnapshot().trendSelectedId, null); controller.dispose();
});
test('large trend Tag selection and Hex navigation preserve exact decimal identities end to end', async () => {
  const id = '9007199254740993', total = '9007199254741250';
  const { controller, requests, reads } = setup({}, total);
  await controller.open(); await settle(); controller.loadTagTrends(id); await settle();
  assert.equal(requests[0].start, id); assert.equal(controller.getSnapshot().trendPage?.points[0].tagId, id);
  controller.selectTrendTag(id); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, id); assert.equal(reads[0].id, id);
  controller.showTrendHex(id, 'payload'); await settle(); assert.equal(controller.getSnapshot().hexSelection?.offset, (BigInt(id) * 20n + 24n).toString());
  assert.equal(controller.getSnapshot().hexSelection?.bitLength, 32); assert.equal(controller.getSnapshot().trendSelectedId, id); controller.dispose();
});
test('a trend Hex intent supersedes pending Tag jump detail without discarding the arriving index page', async () => {
  const old = deferred<Awaited<ReturnType<API['tags']>>>(); let calls = 0; let delayed!: Awaited<ReturnType<API['tags']>>;
  const { controller, reads } = setup({ tags: request => {
    const start = BigInt(request.start), page = { apiVersion: 1, error: null, page: { sessionId: request.sessionId, bounds: { start: request.start, next: (start + 100n).toString(), total: '530', limit: 100 }, tags: Array.from({ length: 100 }, (_, i) => tag((start + BigInt(i)).toString())) } };
    if (++calls === 1) return Promise.resolve(page);
    delayed = page; return old.promise;
  } });
  await controller.open(); await settle(); controller.loadTagTrends(); await settle();
  controller.selectTrendTag('105'); controller.showTrendHex('1', 'payload'); await settle();
  const hex = controller.getSnapshot().hex; assert.equal(hex?.offset, '44'); assert.equal(controller.getSnapshot().trendSelectedId, '1');
  old.resolve(delayed); await settle();
  assert.equal(controller.getSnapshot().page?.data.bounds.start, '100'); assert.equal(controller.getSnapshot().selectedTag, null);
  assert.equal(controller.getSnapshot().hex, hex); assert.equal(controller.getSnapshot().trendSelectedId, '1'); assert.deepEqual(reads.map(item => item.kind), ['hex']); controller.dispose();
});
test('a newer trend Tag intent fences an earlier Hex reply and retains its own selected point', async () => {
  const old = deferred<Awaited<ReturnType<API['startHex']>>>(); let delayed!: Awaited<ReturnType<API['startHex']>>;
  const { controller, reads } = setup({ startHex: request => {
    delayed = { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: '11600', hex: 'aa'.repeat(512), eof: false } };
    return old.promise;
  } });
  await controller.open(); await settle(); controller.loadTagTrends(); await settle();
  controller.showTrendHex('1', 'payload'); controller.selectTrendTag('105'); await settle();
  assert.equal(controller.getSnapshot().selectedTag?.id, '105'); assert.equal(controller.getSnapshot().detailState, 'ready');
  old.resolve(delayed); await settle();
  assert.equal(controller.getSnapshot().hex, null); assert.equal(controller.getSnapshot().trendSelectedId, '105'); assert.deepEqual(reads.map(item => item.kind), ['detail']); controller.dispose();
});
