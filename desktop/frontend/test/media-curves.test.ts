import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, MediaCurvePage, MediaCurveRequest, MediaCurveResponse, SaveRequest, SaveResponse, Session, Tag } from '../src/protocol.ts';
import { CURVE_PAGE_SIZE, curveDomain, curveTag, curveValue, curveY, validateMediaCurvePage } from '../src/media-curves.ts';
import { mediaCurveResponse } from '../src/normalize.ts';
import { overview } from './summary-fixture.ts';

const unknownRate = () => ({ coveredBytes: '0', coveredIntervalMs: '0', bitsPerSecond: null, tagId: null });
function curves(request: MediaCurveRequest, indexed = '33000'): MediaCurveResponse {
  const total = (BigInt(indexed) + 255n) / 256n, start = BigInt(request.start), count = request.count || CURVE_PAGE_SIZE;
  const next = start + BigInt(count) < total ? start + BigInt(count) : total;
  return { apiVersion: 1, sessionId: request.sessionId, requestId: request.requestId, error: null, page: {
    bounds: { start: request.start, limit: count, total: total.toString(), next: next.toString() }, windowTags: 256, indexedTags: indexed, scanComplete: false, complete: true,
    points: Array.from({ length: Number(next - start) }, (_, i) => {
      const index = start + BigInt(i), first = index * 256n, end = first + 256n < BigInt(indexed) ? first + 256n : BigInt(indexed);
      return { index: index.toString(), startTag: first.toString(), nextTag: end.toString(), audio: { coveredBytes: '9007199254740993', coveredIntervalMs: '8000', bitsPerSecond: '9007199254740993', tagId: first.toString() }, video: unknownRate(), mp3: unknownRate(),
        gop: { declaredCount: '1', closedCount: '1', totalDurationMs: '40', minDurationMs: '40', maxDurationMs: '40', averageDurationMs: '40', tagId: first.toString() }, unclassifiedTags: '0' };
    }),
  } };
}
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 30; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = REPLY_TIMEOUT_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
function setup(overrides: Partial<API> = {}, indexed = '33000') {
  const clock = new Clock(), requests: MediaCurveRequest[] = [], reads: { kind: string; id: string }[] = []; let opens = 0;
  const base: Session = { sessionId: 's', state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: true }, failure: null,
    progress: { sourceSize: '9007199254742000', bytesScanned: '20000', tagCount: indexed, diagnosticCount: 0 } };
  let session = base;
  const api: API = {
    startAVCSearch: async () => { throw Error('unexpected AVC search'); }, avcSearchResult: async () => { throw Error('unexpected AVC search result'); }, cancelAVCSearch: async () => { throw Error('unexpected AVC search cancel'); },
    open: async requestId => { session = { ...base, sessionId: `s${++opens}` }; return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'curves.flv', session, error: null }; },
    openResult: async () => { throw Error('unexpected open'); }, status: async () => ({ apiVersion: 1, session, error: null }), cancel: async () => ({ apiVersion: 1, session, error: null }),
    close: async () => ({ apiVersion: 1, session: { ...session, closed: true }, error: null }),
    tags: async request => {
      const start = BigInt(request.start), next = start + 100n < BigInt(indexed) ? start + 100n : BigInt(indexed);
      const tags: Tag[] = Array.from({ length: Number(next - start) }, (_, i) => {
        const id = (start + BigInt(i)).toString(), offset = (BigInt(id) * 20n + 13n).toString(), span = { byteOffset: offset, bitOffset: 0, bitLength: 120 };
        return { id, typeCode: 8, typeName: 'audio', offset, span, headerSpan: span, payloadSpan: span, rawTypeFlags: 8, filter: false, dataSize: 4, availableDataSize: 4, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 15, expected: 15, valid: true } };
      });
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: next.toString(), total: indexed, limit: 100 }, tags } };
    },
    diagnostics: async () => { throw Error('unexpected diagnostics'); }, filteredTags: async () => { throw Error('unexpected filter'); }, tagTrends: async () => { throw Error('unexpected trends'); },
    startDetail: async request => { reads.push({ kind: 'detail', id: request.tagId }); return { apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'aacAudio', complete: true, truncated: false, fields: [], diagnostics: [] } }; },
    detailResult: async () => { throw Error('unexpected detail'); },
    startHex: async request => { reads.push({ kind: 'hex', id: request.offset }); return { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: session.progress!.sourceSize, hex: 'aa'.repeat(512), eof: false } }; },
    hexResult: async () => { throw Error('unexpected Hex'); },
    startSummary: async request => ({ apiVersion: 1, ...request, state: 'ready', processed: indexed, total: indexed, overview: { ...overview(), indexedTags: indexed, lists: overview().lists.map(item => ({ ...item, retained: '0', observed: '0', truncated: false })) }, error: null }),
    summaryResult: async () => { throw Error('unexpected summary result'); }, cancelSummary: async () => { throw Error('unexpected cancel'); },
    summaryEvents: async request => ({ apiVersion: 1, ...request, page: { bounds: { start: '0', next: '0', total: '0', limit: 100 }, events: [] }, error: null }),
    mediaCurves: async request => { requests.push(request); return curves(request, indexed); },
    saveSnapshot: async () => { throw Error('unexpected save'); }, saveResult: async () => { throw Error('unexpected save result'); }, ...overrides,
  };
  return { controller: new Controller(api, clock), clock, requests, reads };
}
async function ready(controller: Controller) { await controller.open(); await settle(); controller.startSummary(); await settle(); }
const request = { sessionId: 's', requestId: '1', start: '0', count: 64 };

test('curves keep exact BigInt rates, nullable durations, partial coverage and empty pages', () => {
  const page = curves(request).page!; validateMediaCurvePage(page, request);
  assert.equal(curveValue(page.points[0], 'audio'), 9007199254740993n); assert.equal(curveValue(page.points[0], 'video'), null);
  assert.equal(curveTag(page.points[0], 'gopMax'), '0'); assert.equal(curveY(9007199254740993n, curveDomain(page.points, 'audio')), 30);
  const zero = { ...request, count: 0 }; validateMediaCurvePage(curves(zero).page!, zero);
  validateMediaCurvePage(curves(request, '0').page!, request);
  validateMediaCurvePage(curves({ ...request, start: '129' }).page!, { ...request, start: '129' });
  page.points[0].gop = { declaredCount: '1', closedCount: '0', totalDurationMs: '0', minDurationMs: null, maxDurationMs: null, averageDurationMs: null, tagId: null };
  validateMediaCurvePage(page, request); assert.equal(curveValue(page.points[0], 'gopAverage'), null);
});
test('curve normalization handles nullable pointers; validation rejects forged arithmetic, coordinates and numeric wire values', () => {
  const raw = JSON.parse(JSON.stringify(curves(request)));
  delete raw.page.points[0].video.bitsPerSecond; delete raw.page.points[0].video.tagId;
  const normalized = mediaCurveResponse(raw); validateMediaCurvePage(normalized.page!, request); assert.equal(normalized.page!.points[0].video.tagId, null);
  const mutations: ((p: MediaCurvePage) => void)[] = [
    p => { p.bounds.limit = 129; }, p => { p.bounds.next = '63'; }, p => { p.bounds.total = '3908'; }, p => { p.windowTags = 255; },
    p => { p.points[0].nextTag = '255'; }, p => { p.points.pop(); }, p => { p.points[0].index = '1'; },
    p => { p.points[0].audio.bitsPerSecond = '9007199254740992'; }, p => { p.points[0].audio.tagId = '256'; },
    p => { p.points[0].video.coveredBytes = '1'; }, p => { p.points[0].video.tagId = '0'; }, p => { p.points[0].unclassifiedTags = '257'; },
    p => { p.points[0].gop.averageDurationMs = '39'; }, p => { p.points[0].gop.minDurationMs = '41'; }, p => { p.points[0].gop.tagId = null; },
    p => { p.points[0].gop.closedCount = '0'; }, p => { p.points[0].gop.declaredCount = '257'; },
  ];
  for (const mutate of mutations) { const page = curves(request).page!; mutate(page); assert.throws(() => validateMediaCurvePage(page, request)); }
  for (const name of ['index', 'startTag', 'nextTag', 'unclassifiedTags']) {
    const value = JSON.parse(JSON.stringify(curves(request).page)); value.points[0][name] = 0; assert.throws(() => validateMediaCurvePage(value, request));
  }
  for (const name of ['coveredBytes', 'coveredIntervalMs', 'bitsPerSecond', 'tagId']) {
    const value = JSON.parse(JSON.stringify(curves(request).page)); value.points[0].audio[name] = 0; assert.throws(() => validateMediaCurvePage(value, request));
  }
});
test('curves load explicitly after summary ready, one page at a time, using the same summary identity', async () => {
  const { controller, requests, reads } = setup(); await controller.open(); await settle(); controller.loadMediaCurves(); assert.equal(requests.length, 0);
  controller.startSummary(); await settle(); assert.equal(requests.length, 0);
  controller.loadMediaCurves(); await settle(); assert.equal(controller.getSnapshot().curvePage?.points.length, 64); assert.equal(requests[0].requestId, '1');
  controller.pageMediaCurves('next'); await settle(); assert.equal(requests[1].start, '64');
  controller.pageMediaCurves('next'); await settle(); assert.equal(controller.getSnapshot().curvePage?.points.length, 1);
  controller.pageMediaCurves('next'); await settle(); assert.equal(requests.length, 3);
  controller.pageMediaCurves('previous'); await settle(); assert.equal(controller.getSnapshot().curveStart, '64');
  controller.pageMediaCurves('first'); await settle(); assert.equal(controller.getSnapshot().curveStart, '0'); assert.equal(reads.length, 0);
  controller.loadMediaCurves('130'); assert.equal(controller.getSnapshot().curveError, 'invalid_argument');
  controller.loadMediaCurves('129'); await settle(); assert.equal(controller.getSnapshot().curvePage?.points.length, 0); controller.dispose();
});
test('curve timeout stops until explicit retry and ignores late source-change replies', async () => {
  const old = deferred<MediaCurveResponse>(); let sent!: MediaCurveRequest, calls = 0;
  const { controller, clock } = setup({ mediaCurves: req => { sent = req; return ++calls === 1 ? old.promise : Promise.resolve(curves(req)); } });
  await ready(controller); controller.loadMediaCurves('64'); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().curveError, 'transport_failed'); assert.equal(clock.tasks.size, 0); assert.equal(calls, 1);
  controller.retryMediaCurves(); await settle(); const page = controller.getSnapshot().curvePage; assert.equal(page?.bounds.start, '64');
  old.resolve({ apiVersion: 1, ...sent, page: null, error: { code: 'source_changed', message: 'Late' } }); await settle();
  assert.equal(controller.getSnapshot().curvePage, page); assert.equal(controller.getSnapshot().sourceChanged, false); controller.dispose();
});
test('reanalysis, close/reopen, disposal and source change fence pending curve replies', async () => {
  for (const action of ['summary', 'close', 'dispose', 'source'] as const) {
    const old = deferred<MediaCurveResponse>(); let sent!: MediaCurveRequest;
    const { controller, clock } = setup({ mediaCurves: req => { sent = req; return old.promise; }, startHex: async req => ({ apiVersion: 1, ...req, state: 'failed', window: null, error: { code: 'source_changed', message: 'changed' } }) });
    await ready(controller); controller.loadMediaCurves();
    if (action === 'summary') { controller.startSummary(); await settle(); }
    if (action === 'close') { await controller.close(); await controller.open(); }
    if (action === 'dispose') controller.dispose();
    if (action === 'source') { controller.jumpHex('13'); await settle(); }
    old.resolve(curves(sent)); await settle(); assert.equal(controller.getSnapshot().curvePage, null); assert.equal(clock.tasks.size, 0);
    if (action === 'source') assert.equal(controller.getSnapshot().sourceChanged, true);
    if (action === 'summary') assert.equal(controller.getSnapshot().summaryState, 'ready');
    controller.dispose();
  }
});
test('malformed pages or stale identities cannot publish data or latch source change', async () => {
  for (const bad of ['session', 'identity', 'mixed', 'coverage', 'arithmetic', 'error', 'source']) {
    const { controller } = setup({ mediaCurves: async req => {
      const result = curves(req);
      if (bad === 'session') result.sessionId = 'other';
      if (bad === 'identity') result.requestId = '2';
      if (bad === 'coverage') result.page!.complete = false;
      if (bad === 'arithmetic') result.page!.points[0].audio.bitsPerSecond = '1';
      if (bad === 'mixed' || bad === 'source') result.error = { code: 'source_changed', message: 'changed' };
      if (bad === 'source') result.page = null;
      if (bad === 'error') { result.page = null; result.error = { code: '', message: '' }; }
      return result;
    } });
    await ready(controller); controller.loadMediaCurves(); await settle(); assert.equal(controller.getSnapshot().curvePage, null);
    assert.equal(controller.getSnapshot().sourceChanged, bad === 'source');
    if (bad !== 'source') assert.equal(controller.getSnapshot().curveError, 'invalid_response'); controller.dispose();
  }
});
test('point navigation gets real Tag metadata before Hex; unknown metrics never invent a Tag', async () => {
  const { controller, reads } = setup(); await ready(controller); controller.loadMediaCurves(); await settle(); const page = controller.getSnapshot().curvePage;
  controller.showCurveHex('1'); await settle(); assert.equal(reads.length, 0);
  controller.selectCurvePoint('1'); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '256'); assert.equal(controller.getSnapshot().curvePage, page);
  controller.showCurveHex('1'); await settle(); assert.equal(controller.getSnapshot().hexSelection?.offset, '5133'); assert.equal(controller.getSnapshot().hexSelection?.bitLength, 120);
  assert.deepEqual(reads, [{ kind: 'detail', id: '256' }, { kind: 'hex', id: '5133' }]);
  controller.setCurveMetric('video'); controller.selectCurvePoint('2'); controller.showCurveHex('2'); await settle(); assert.equal(reads.length, 2); assert.equal(controller.getSnapshot().curveSelectedIndex, '2');
  controller.startSummary(); await settle(); assert.equal(controller.getSnapshot().curvePage, null); assert.equal(controller.getSnapshot().curveSelectedIndex, null); controller.dispose();
});
test('rapid curve intents coalesce; an empty summary yields an empty page', async () => {
  const old = deferred<MediaCurveResponse>(); const requests: MediaCurveRequest[] = [];
  const { controller } = setup({ mediaCurves: req => { requests.push(req); return requests.length === 1 ? old.promise : Promise.resolve(curves(req)); } });
  await ready(controller); controller.loadMediaCurves(); controller.loadMediaCurves('64'); controller.loadMediaCurves('128'); await settle();
  assert.deepEqual(requests.map(item => item.start), ['0', '128']); assert.equal(controller.getSnapshot().curvePage?.bounds.start, '128');
  old.resolve(curves(requests[0])); await settle(); assert.equal(controller.getSnapshot().curvePage?.bounds.start, '128'); controller.dispose();
  const empty = setup({}, '0'); await ready(empty.controller); empty.controller.loadMediaCurves(); await settle(); assert.equal(empty.controller.getSnapshot().curvePage?.points.length, 0); empty.controller.dispose();
});
test('pending, canceled and failed reanalysis clear old curves and never admit curve paging', async () => {
  for (const code of ['canceled', 'summary_failed']) {
    let starts = 0;
    const { controller, requests } = setup({
      startSummary: async identity => ++starts === 1 ? { apiVersion: 1, ...identity, state: 'ready', processed: '33000', total: '33000', overview: { ...overview(), indexedTags: '33000', lists: overview().lists.map(item => ({ ...item, retained: '0', observed: '0', truncated: false })) }, error: null }
        : { apiVersion: 1, ...identity, state: code === 'canceled' ? 'pending' : 'failed', processed: '0', total: '33000', overview: null, error: code === 'canceled' ? null : { code, message: code } },
      cancelSummary: async identity => ({ apiVersion: 1, ...identity, state: 'failed', processed: '0', total: '33000', overview: null, error: { code, message: code } }),
    });
    await ready(controller); controller.loadMediaCurves(); await settle(); assert.ok(controller.getSnapshot().curvePage);
    controller.startSummary(); await settle(); controller.loadMediaCurves(); assert.equal(controller.getSnapshot().curvePage, null); assert.equal(requests.length, 1);
    if (code === 'canceled') { controller.cancelSummary(); await settle(); }
    assert.equal(controller.getSnapshot().summaryState, 'failed'); assert.equal(controller.getSnapshot().summaryError, code);
    controller.loadMediaCurves(); await settle(); assert.equal(requests.length, 1); controller.dispose();
  }
});


test('snapshot v2 freezes the visible curve page and metric without issuing another curve query', async () => {
  const saving = deferred<SaveResponse>(), saves: SaveRequest[] = [];
  const { controller, requests } = setup({ saveSnapshot: request => { saves.push(request); return saving.promise; } });
  await ready(controller); controller.loadMediaCurves('64'); await settle(); controller.setCurveMetric('gopMax');
  const saved = controller.saveSnapshot();
  assert.equal(saves[0].snapshot.schemaVersion, 2);
  assert.deepEqual(saves[0].snapshot.mediaCurve, { start: '64', count: 64, metric: 'gopMax' });
  assert.equal(saves[0].snapshot.summaryRequestId, '1'); assert.equal(requests.length, 1);
  assert.equal(Object.isFrozen(saves[0].snapshot.mediaCurve), true);
  controller.setCurveMetric('video'); controller.pageMediaCurves('next'); await settle();
  assert.deepEqual(saves[0].snapshot.mediaCurve, { start: '64', count: 64, metric: 'gopMax' });
  saving.resolve({ apiVersion: 1, requestId: saves[0].requestId, pending: false, canceled: false, fileName: 'curves.json', bytesWritten: '1000', error: null });
  await saved; controller.dispose();
});
test('snapshot v2 omits curve selection while a replacement page is pending or summary was replaced', async () => {
  const pending = deferred<MediaCurveResponse>(), saves: SaveRequest[] = [];
  let calls = 0;
  const { controller } = setup({ mediaCurves: request => ++calls === 1 ? Promise.resolve(curves(request)) : pending.promise,
    saveSnapshot: async request => { saves.push(request); return { apiVersion: 1, requestId: request.requestId, pending: false, canceled: false, fileName: 'page.json', bytesWritten: '1000', error: null }; } });
  await ready(controller); controller.loadMediaCurves(); await settle(); controller.pageMediaCurves('next');
  await controller.saveSnapshot(); assert.equal(saves[0].snapshot.mediaCurve, undefined);
  controller.startSummary(); await settle(); await controller.saveSnapshot();
  assert.equal(saves[1].snapshot.mediaCurve, undefined); controller.dispose();
});
