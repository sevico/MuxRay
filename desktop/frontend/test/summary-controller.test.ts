import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, POLL_MS, REPLY_TIMEOUT_MS, SUMMARY_POLL_LIMIT } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, DetailQuery, SummaryResponse, SummaryPageResponse, Session, Tag } from '../src/protocol.ts';
import { summaryReady, summaryPending, summaryFailed, summaryPage } from './summary-fixture.ts';

function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(yes => { resolve = yes; }); return { promise, resolve }; }
async function settle() { for (let i = 0; i < 24; i++) await Promise.resolve(); }
class Clock implements Scheduler {
  tasks = new Map<() => void, number>();
  after(fn: () => void, ms: number) { this.tasks.set(fn, ms); return () => { this.tasks.delete(fn); }; }
  tick(ms = POLL_MS) { for (const [fn, delay] of [...this.tasks]) if (delay === ms) { this.tasks.delete(fn); fn(); } }
}
function setup(overrides: Partial<API> = {}) {
  const clock = new Clock(), reads: { kind: string; requestId: string }[] = [];
  const session: Session = { sessionId: 'scan-1', state: 'canceled', terminal: true, closed: false, scan: { complete: false, hasErrors: false }, failure: null, progress: { sourceSize: '9007199254742000', bytesScanned: '10000', tagCount: '205', diagnosticCount: 0 } };
  const span = { byteOffset: '13', bitOffset: 0, bitLength: 120 };
  const tag = (id: string): Tag => ({ id, typeCode: 8, typeName: 'audio', offset: '13', span, headerSpan: span, payloadSpan: span, rawTypeFlags: 8, filter: false, dataSize: 4, availableDataSize: 4, dts: '0', streamId: 0, payloadComplete: true, previousTagSize: { present: true, span, value: 15, expected: 15, valid: true } });
  const api: API = {
    filteredTags: async () => { throw Error('unexpected filtered tags'); },
    open: async requestId => ({ apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'summary.flv', session, error: null }),
    openResult: async () => { throw Error('unexpected open'); }, status: async () => ({ apiVersion: 1, session, error: null }), cancel: async () => ({ apiVersion: 1, session, error: null }),
    close: async () => ({ apiVersion: 1, session: { ...session, closed: true }, error: null }),
    tags: async request => ({ apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: request.start === '0' ? '100' : '200', total: '205', limit: 100 }, tags: [tag(request.start === '0' ? '0' : '105')] } }),
    diagnostics: async request => ({ apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: '0', total: '0', limit: 100 }, diagnostics: [] } }),
    startDetail: async request => { reads.push({ kind: 'detail', ...request }); return { apiVersion: 1, ...request, state: 'ready', error: null, detail: { kind: 'aacAudio', complete: true, truncated: false, fields: [], diagnostics: [] } }; },
    detailResult: async () => { throw Error('unexpected detail'); },
    startHex: async request => { reads.push({ kind: 'hex', ...request }); return { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: session.progress!.sourceSize, hex: 'aa'.repeat(512), eof: false } }; },
    hexResult: async () => { throw Error('unexpected Hex'); },
    startSummary: async request => { reads.push({ kind: 'summary', ...request }); return summaryReady(request); }, summaryResult: async request => summaryReady(request),
    cancelSummary: async request => summaryFailed(request, 'canceled'), summaryEvents: async request => summaryPage(request), ...overrides,
  };
  return { controller: new Controller(api, clock), clock, reads };
}
test('summary is explicit, shares read IDs, and retains its ready result through Tag and Hex navigation', async () => {
  const { controller, reads } = setup(); await controller.open(); await settle(); assert.equal(reads.length, 0);
  controller.selectTag('0'); await settle(); controller.startSummary(); await settle();
  const summary = controller.getSnapshot().summary, event = controller.getSnapshot().summaryPage!.events[0];
  assert.equal(summary?.scanComplete, false); assert.equal(summary?.complete, true);
  controller.locateSummaryEvent(event, 'tag'); await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '105');
  controller.locateSummaryEvent(event, 'hex'); await settle(); assert.equal(controller.getSnapshot().hex?.offset, '9007199254740993');
  assert.equal(controller.getSnapshot().hexSelection?.bitOffset, 3); assert.equal(controller.getSnapshot().hexSelection?.bitLength, null);
  assert.equal(controller.getSnapshot().summary, summary);
  assert.deepEqual(reads.map(item => [item.kind, item.requestId]), [['detail', '1'], ['summary', '2'], ['detail', '3'], ['hex', '4']]);
});
test('long pending summary blocks detail, Hex and page intents and uses the 600-reply limit', async () => {
  let queries = 0; const starts: DetailQuery[] = [];
  const { controller, clock, reads } = setup({ startSummary: async request => { starts.push(request); return summaryPending(request); }, summaryResult: async request => { queries++; return summaryPending(request); } });
  await controller.open(); await settle(); controller.startSummary(); await settle();
  controller.selectTag('0'); controller.jumpHex('13'); controller.select('diagnostics'); controller.jumpTag('105'); controller.startSummary();
  assert.equal(reads.length, 0); assert.equal(controller.getSnapshot().tab, 'tags'); assert.equal(starts.length, 1);
  for (let i = 0; i < SUMMARY_POLL_LIMIT; i++) { clock.tick(); await settle(); }
  assert.equal(queries, 600); assert.equal(clock.tasks.size, 0); assert.equal(controller.getSnapshot().summaryRunning, true); assert.equal(controller.getSnapshot().summaryRetry, 'confirm');
  controller.retrySummary(); clock.tick(); await settle(); assert.equal(starts.length, 1); assert.equal(queries, 601);
});
test('lost Start reply uses the same Get identity and ignores the late reply', async () => {
  const lost = deferred<SummaryResponse>(); let request!: DetailQuery; const queries: string[] = [];
  const { controller, clock } = setup({ startSummary: value => { request = value; return lost.promise; }, summaryResult: async value => { queries.push(value.requestId); return summaryReady(value); } });
  await controller.open(); await settle(); controller.startSummary(); clock.tick(REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().summaryState, 'recovering'); clock.tick(); await settle();
  assert.deepEqual(queries, ['1']); assert.equal(controller.getSnapshot().summaryState, 'ready');
  lost.resolve(summaryFailed(request, 'summary_failed')); await settle(); assert.equal(controller.getSnapshot().summaryState, 'ready');
});
test('cancel fences an in-flight Start and remains blocked until an authoritative terminal result', async () => {
  const old = deferred<SummaryResponse>(); let request!: DetailQuery; let canceled = 0;
  const { controller, clock } = setup({ startSummary: value => { request = value; return old.promise; }, cancelSummary: async value => { canceled++; return summaryPending(value, 'canceling'); }, summaryResult: async value => summaryFailed(value, 'canceled') });
  await controller.open(); await settle(); controller.startSummary(); controller.cancelSummary(); await settle();
  assert.equal(canceled, 1); assert.equal(controller.getSnapshot().summaryState, 'canceling'); assert.equal(controller.getSnapshot().summaryRunning, true);
  old.resolve(summaryReady(request)); await settle(); assert.equal(controller.getSnapshot().summary, null);
  clock.tick(); await settle(); assert.equal(controller.getSnapshot().summaryRunning, false); assert.equal(controller.getSnapshot().summaryRetry, 'reload');
});
test('a lost cancellation is confirmed with Get and pending does not silently resume analysis', async () => {
  const lost = deferred<SummaryResponse>(); const cancellations: string[] = [];
  const { controller, clock } = setup({ startSummary: async request => summaryPending(request), cancelSummary: request => { cancellations.push(request.requestId); return cancellations.length === 1 ? lost.promise : Promise.resolve(summaryFailed(request, 'canceled')); }, summaryResult: async request => summaryPending(request) });
  await controller.open(); await settle(); controller.startSummary(); await settle(); controller.cancelSummary(); clock.tick(REPLY_TIMEOUT_MS); await settle(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().summaryCancelRequested, true); assert.equal(controller.getSnapshot().summaryRunning, true); assert.equal(controller.getSnapshot().summaryError, 'summary_cancel_unconfirmed'); assert.equal(clock.tasks.size, 0);
  controller.retrySummary(); await settle(); assert.deepEqual(cancellations, ['1', '1']); assert.equal(controller.getSnapshot().summaryError, 'canceled'); assert.equal(controller.getSnapshot().summary, null);
});
test('authoritative failure reload uses a new shared identity, while lost Get confirmation keeps the old identity', async () => {
  const lost = deferred<SummaryResponse>(); const starts: string[] = []; let gets = 0;
  const { controller, clock } = setup({ startSummary: async request => { starts.push(request.requestId); return starts.length === 1 ? summaryPending(request) : summaryReady(request); }, summaryResult: request => ++gets === 1 ? lost.promise : Promise.resolve(summaryFailed(request, 'summary_failed')) });
  await controller.open(); await settle(); controller.startSummary(); await settle(); clock.tick(); await settle(); clock.tick(REPLY_TIMEOUT_MS); await settle();
  controller.retrySummary(); clock.tick(); await settle(); assert.equal(controller.getSnapshot().summaryRetry, 'reload'); controller.retrySummary(); await settle(); assert.deepEqual(starts, ['1', '2']);
});
test('event paging uses real next positions and history for short bounded pages', async () => {
  const { controller } = setup({ summaryEvents: async request => summaryPage(request, 37) }); await controller.open(); await settle(); controller.startSummary(); await settle();
  controller.pageSummary('next'); await settle(); assert.equal(controller.getSnapshot().summaryStart, '37');
  controller.pageSummary('next'); await settle(); assert.equal(controller.getSnapshot().summaryStart, '74');
  controller.pageSummary('previous'); await settle(); assert.equal(controller.getSnapshot().summaryStart, '37');
  controller.selectSummaryKind('diagnostics'); await settle(); assert.equal(controller.getSnapshot().summaryStart, '0'); assert.equal(controller.getSnapshot().summaryPrevious, null);
  assert.equal(controller.getSnapshot().summary?.lists.find(list => list.kind === 'diagnostics')?.observed, null);
});
test('only the latest category page updates UI, and an old page cannot navigate after replacement', async () => {
  const old = deferred<SummaryPageResponse>(); let first!: SummaryPageResponse;
  const { controller } = setup({ summaryEvents: async request => { if (request.kind === 'keyframes') { first = summaryPage(request); return old.promise; } return summaryPage(request); } });
  await controller.open(); await settle(); controller.startSummary(); await settle(); controller.selectSummaryKind('mp3'); await settle();
  assert.equal(controller.getSnapshot().summaryKind, 'mp3'); old.resolve(first); await settle(); assert.equal(controller.getSnapshot().summaryPage?.bounds.total, '100');
  controller.locateSummaryEvent(first.page!.events[0], 'tag'); await settle(); assert.equal(controller.getSnapshot().selectedTag, null);
});
test('source changes from summary, event pages, or later Hex clear the retained overview and block reuse', async () => {
  for (const kind of ['summary', 'page', 'hex']) {
    const { controller } = setup({ startSummary: async request => kind === 'summary' ? summaryFailed(request, 'source_changed') : summaryReady(request), summaryEvents: async request => kind === 'page' ? { ...summaryPage(request), page: null, error: { code: 'source_changed', message: '' } } : summaryPage(request), startHex: async request => ({ apiVersion: 1, ...request, state: 'failed', error: { code: 'source_changed', message: '' }, window: null }) });
    await controller.open(); await settle(); controller.startSummary(); await settle(); if (kind === 'hex') { controller.jumpHex('13'); await settle(); }
    assert.equal(controller.getSnapshot().sourceChanged, true); assert.equal(controller.getSnapshot().summary, null); assert.equal(controller.getSnapshot().summaryPage, null);
    controller.startSummary(); await settle(); assert.equal(controller.getSnapshot().summaryState, '');
    await controller.close(); await controller.open(); await settle(); assert.equal(controller.getSnapshot().sourceChanged, false);
  }
});
test('close and dispose fence late summary replies and stop all timers', async () => {
  for (const action of ['close', 'dispose']) {
    const old = deferred<SummaryResponse>(); let request!: DetailQuery;
    const { controller, clock } = setup({ startSummary: value => { request = value; return old.promise; } });
    await controller.open(); await settle(); controller.startSummary(); if (action === 'close') await controller.close(); else controller.dispose();
    await settle(); old.resolve(summaryReady(request)); await settle(); assert.equal(controller.getSnapshot().summary, null); assert.equal(clock.tasks.size, 0);
  }
});
test('mismatched summary identity stays unconfirmed and does not publish an overview', async () => {
  const { controller } = setup({ startSummary: async request => ({ ...summaryReady(request), requestId: '999' }) }); await controller.open(); await settle(); controller.startSummary(); await settle();
  assert.equal(controller.getSnapshot().summary, null); assert.equal(controller.getSnapshot().summaryRunning, true); assert.equal(controller.getSnapshot().summaryError, 'invalid_response');
});
test('malformed event page cannot publish mismatched retained bounds', async () => {
  const { controller } = setup({ summaryEvents: async request => { const result = summaryPage(request); result.page!.bounds.total = '999'; return result; } });
  await controller.open(); await settle(); controller.startSummary(); await settle(); assert.equal(controller.getSnapshot().summaryPage, null); assert.equal(controller.getSnapshot().summaryPageError, 'invalid_response');
});
test('mismatched page source_changed error cannot invalidate the active session', async () => {
  const { controller } = setup({ summaryEvents: async request => ({ ...summaryPage(request), sessionId: 'old-session', page: null, error: { code: 'source_changed', message: '' } }) });
  await controller.open(); await settle(); controller.startSummary(); await settle();
  assert.equal(controller.getSnapshot().sourceChanged, false); assert.notEqual(controller.getSnapshot().summary, null); assert.equal(controller.getSnapshot().summaryPageError, 'invalid_response');
});
