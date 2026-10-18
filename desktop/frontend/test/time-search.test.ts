import { test } from 'node:test';
import assert from 'node:assert/strict';
import { REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { PageResponse, SaveRequest, TagTrendPage, TimeSearchRequest, TimeSearchResponse } from '../src/protocol.ts';
import { timeSearchFilter, validateTimeSearch } from '../src/time-search.ts';
import { timeSearchResponse } from '../src/normalize.ts';
import { deferred, failed, pending, point, ready, setup, settle, trend } from './time-search-fixture.ts';

test('timestamp search is explicit, advances opaque cursors across empty batches, and finds a tail boundary regression', async () => {
  const { controller, calls } = setup(); await controller.open(); await settle(); assert.equal(calls.length, 0);
  controller.startTimeSearch(); await settle(); assert.equal(controller.getSnapshot().timeSearchPage!.rows.length, 0);
  controller.continueTimeSearch(); await settle(); assert.equal(controller.getSnapshot().timeSearchPage!.next, '8192');
  controller.continueTimeSearch(); await settle(); const p = controller.getSnapshot().timeSearchPage!;
  assert.deepEqual(calls.map(q => [q.requestId, q.cursor, q.filter]), [['1', '0', 'regressions'], ['2', '1', 'regressions'], ['3', '2', 'regressions']]);
  assert.equal(p.exhausted, true); assert.equal(p.rows[0].point.tagId, '8192'); assert.equal(p.rows[0].point.previousTagId, '8191'); assert.equal(p.rows[0].point.deltaMs, '-100');
  controller.continueTimeSearch(); await settle(); assert.equal(calls.length, 3); controller.dispose();
});

test('locating a hit waits for its trend window, preserves search evidence, and leaves the existing snapshot contract intact', async () => {
  const waiting = deferred<PageResponse<TagTrendPage>>(), saves: SaveRequest[] = []; let requested!: Parameters<typeof trend>[0];
  const { controller } = setup({ tagTrends: q => { requested = q; return waiting.promise; }, saveSnapshot: async q => { saves.push(q); return { apiVersion: 1, requestId: q.requestId, pending: false, canceled: false, error: null, fileName: 'report.json', bytesWritten: '1000' }; } });
  await controller.open(); await settle(); for (let i = 0; i < 3; i++) { if (i === 0) controller.startTimeSearch('regressions', '2000'); else controller.continueTimeSearch(); await settle(); }
  const p = controller.getSnapshot().timeSearchPage!, row = p.rows[0]; controller.locateTimeSearchRow(row); await settle();
  assert.equal(requested.start, '8192'); assert.equal(controller.getSnapshot().selectedTag, null); assert.equal(controller.getSnapshot().trendGapThreshold, '2000');
  waiting.resolve(trend(requested)); await settle(); assert.equal(controller.getSnapshot().trendSelectedId, '8192'); assert.equal(controller.getSnapshot().selectedTag!.id, '8192');
  controller.showTrendHex('8192', 'payload'); await settle(); assert.equal(controller.getSnapshot().timeSearchPage, p);
  await controller.saveSnapshot(); assert.equal(saves[0].snapshot.schemaVersion, 3); assert.equal('timeSearch' in saves[0].snapshot, false);
  assert.equal(saves[0].snapshot.trend?.start, '8192'); assert.equal(saves[0].snapshot.selectedTagId, '8192'); controller.dispose();
});

test('lost timestamp Start confirms the same ID and discards late source-change replies', async () => {
  const lost = deferred<TimeSearchResponse>(); let request!: TimeSearchRequest; let gets = 0;
  const { controller, clock } = setup({ startTimeSearch: q => { request = q; return lost.promise; }, timeSearchResult: async q => { gets++; assert.equal(q.requestId, request.requestId); return ready(request); } });
  await controller.open(); await settle(); controller.startTimeSearch(); clock.tick(REPLY_TIMEOUT_MS); await settle(); clock.tick(); await settle();
  assert.equal(gets, 1); assert.equal(controller.getSnapshot().timeSearchState, 'ready'); lost.resolve(failed(request, 'source_changed')); await settle();
  assert.equal(controller.getSnapshot().sourceChanged, false); controller.dispose();
});

test('cancel fences an in-flight batch, blocks other readers, and retries the same cursor with a fresh request', async () => {
  const lost = deferred<TimeSearchResponse>(), calls: TimeSearchRequest[] = []; let request!: TimeSearchRequest;
  const { controller, clock, reads, trends } = setup({ startTimeSearch: q => { request = q; calls.push(q); return calls.length === 1 ? lost.promise : Promise.resolve(ready(q)); }, cancelTimeSearch: async () => pending(request, 'canceling'), timeSearchResult: async () => failed(request) });
  await controller.open(); await settle(); controller.startTimeSearch(); controller.cancelTimeSearch(); await settle();
  controller.selectTag('0'); controller.jumpHex('13'); controller.loadTagTrends(); controller.startSummary(); controller.startAVCSearch(); controller.startCodecSearch(); controller.startTimeSearch();
  await settle(); assert.equal(calls.length, 1); assert.deepEqual(reads, []); assert.deepEqual(trends, []);
  lost.resolve(ready(request)); await settle(); assert.equal(controller.getSnapshot().timeSearchPage, null); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().timeSearchRunning, false); controller.retryTimeSearch(); await settle();
  assert.deepEqual(calls.map(q => [q.requestId, q.cursor]), [['1', '0'], ['2', '0']]); controller.dispose();
});

test('lost cancellation that still reports pending preserves the guard and retries only cancellation', async () => {
  let request!: TimeSearchRequest; const calls: string[] = [];
  const { controller, clock } = setup({ startTimeSearch: async q => { request = q; return pending(q); }, cancelTimeSearch: async q => { calls.push(q.requestId); if (calls.length === 1) throw Error('lost'); return failed(request); }, timeSearchResult: async () => pending(request) });
  await controller.open(); await settle(); controller.startTimeSearch(); await settle(); controller.cancelTimeSearch(); await settle(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().timeSearchError, 'time_search_cancel_unconfirmed'); assert.equal(controller.getSnapshot().timeSearchRunning, true);
  controller.retryTimeSearch(); await settle(); assert.deepEqual(calls, ['1', '1']); assert.equal(controller.getSnapshot().timeSearchRunning, false); controller.dispose();
});

test('unadmitted work retries original identity; expired continuation requires explicit restart', async () => {
  for (const code of ['time_search_not_found', 'time_search_cursor_expired']) {
    const calls: TimeSearchRequest[] = [];
    const { controller } = setup({ startTimeSearch: async q => { calls.push(q); return calls.length === 1 ? failed(q, code) : ready(q); } });
    await controller.open(); await settle(); controller.startTimeSearch(); await settle(); controller.retryTimeSearch(); await settle();
    if (code.endsWith('not_found')) assert.deepEqual(calls.map(q => q.requestId), ['1', '1']);
    else { assert.equal(calls.length, 1); assert.equal(controller.getSnapshot().timeSearchRetry, ''); controller.startTimeSearch(); await settle(); assert.equal(calls[1].cursor, '0'); }
    controller.dispose();
  }
});

test('close/reopen, dispose and a newer locate intent fence delayed replies', async () => {
  for (const action of ['close', 'dispose']) {
    let request!: TimeSearchRequest; const lost = deferred<TimeSearchResponse>();
    const { controller, clock } = setup({ startTimeSearch: q => { request = q; return lost.promise; } });
    await controller.open(); await settle(); controller.startTimeSearch(); if (action === 'close') { await controller.close(); await controller.open(); } else controller.dispose();
    lost.resolve(ready(request)); await settle(); assert.equal(controller.getSnapshot().timeSearchPage, null); assert.equal(clock.tasks.size, 0); controller.dispose();
  }
  const lostTrend = deferred<PageResponse<TagTrendPage>>(); let old!: Parameters<typeof trend>[0];
  const { controller } = setup({ tagTrends: q => { if (!old) { old = q; return lostTrend.promise; } return Promise.resolve(trend(q)); } });
  await controller.open(); await settle(); for (let i = 0; i < 3; i++) { if (!i) controller.startTimeSearch(); else controller.continueTimeSearch(); await settle(); }
  controller.locateTimeSearchRow(controller.getSnapshot().timeSearchPage!.rows[0]); controller.loadTagTrends('0'); lostTrend.resolve(trend(old)); await settle();
  assert.equal(controller.getSnapshot().trendPage!.bounds.start, '0'); assert.equal(controller.getSnapshot().selectedTag, null); controller.dispose();
});

test('wrong session and false completion cannot invalidate source; matching source change clears all results', async () => {
  for (const wrong of [true, false]) {
    const { controller } = setup({ startTimeSearch: async q => ({ ...failed(q, 'source_changed'), sessionId: wrong ? 'old' : q.sessionId }) });
    await controller.open(); await settle(); controller.startTimeSearch(); await settle(); assert.equal(controller.getSnapshot().sourceChanged, !wrong); controller.dispose();
  }
  const { controller } = setup({ startTimeSearch: async q => { const r = ready(q); r.page!.scanComplete = true; return r; } });
  await controller.open(); await settle(); controller.startTimeSearch(); await settle(); assert.equal(controller.getSnapshot().timeSearchError, 'invalid_response'); controller.dispose();
});

test('a later explicit Tag selection cannot be replaced by a delayed search locate', async () => {
  for (const action of ['jump', 'select']) {
    const lost = deferred<PageResponse<TagTrendPage>>(); let request!: Parameters<typeof trend>[0];
    const { controller } = setup({ tagTrends: q => { request = q; return lost.promise; } });
    await controller.open(); await settle(); for (let i = 0; i < 3; i++) { if (!i) controller.startTimeSearch(); else controller.continueTimeSearch(); await settle(); }
    controller.locateTimeSearchRow(controller.getSnapshot().timeSearchPage!.rows[0]);
    if (action === 'jump') controller.jumpTag('20'); else controller.selectTag('20');
    await settle(); assert.equal(controller.getSnapshot().selectedTag?.id, '20'); lost.resolve(trend(request)); await settle();
    assert.equal(controller.getSnapshot().selectedTag?.id, '20'); assert.equal(controller.getSnapshot().trendSelectedId, null); controller.dispose();
  }
});

test('timestamp continuation and completed codec retries respect the other worker ownership', async () => {
  let retries = 0;
  const one = setup({ startCodecSearch: async q => { retries++; return { apiVersion: 1, ...q, state: 'failed', page: null, error: { code: 'codec_search_failed', message: '' } }; }, startTimeSearch: async q => pending(q) });
  await one.controller.open(); await settle(); one.controller.startCodecSearch(); await settle(); one.controller.startTimeSearch(); await settle(); one.controller.retryCodecSearch(); await settle();
  assert.equal(retries, 1); one.controller.dispose();
  let starts = 0;
  const two = setup({ startTimeSearch: async q => { starts++; return ready(q); }, startCodecSearch: async q => ({ apiVersion: 1, ...q, state: 'pending', error: null, page: null }) });
  await two.controller.open(); await settle(); two.controller.startTimeSearch(); await settle(); two.controller.startCodecSearch(); await settle(); two.controller.continueTimeSearch(); await settle();
  assert.equal(starts, 1); two.controller.dispose();
});

test('validator preserves large exact coordinates and accepts carried predecessors older than 4096 Tags', () => {
  const q: TimeSearchRequest = { sessionId: 's', requestId: '9007199254740993', cursor: '800', filter: 'regressions', gapThreshold: '1000' };
  const start = '9007199254740993', next = '9007199254740994', r = ready(q, start, next, next), p = point(start);
  Object.assign(p, { dts: '0', previousTagId: '1', previousDts: '4294967295', deltaMs: '-4294967295' }); r.page!.rows = [{ kind: 'regression', point: p }];
  validateTimeSearch(r, q, next, '18446744073709551615', start); assert.equal(r.page!.rows[0].point.tagId, start);
  assert.throws(() => validateTimeSearch(r, q, next, '18446744073709551615', '0'));
});

test('validator rejects forged identities, bounds, counts, cursor tokens, comparisons, unknowns and filter mismatches', () => {
  const q: TimeSearchRequest = { sessionId: 's', requestId: '9', cursor: '8', filter: 'regressions', gapThreshold: '1000' };
  const valid = () => ready(q, '8192', '10000'); validateTimeSearch(valid(), q, '10000', '201000', '8192');
  const changes: ((r: TimeSearchResponse) => void)[] = [r => r.requestId = '8', r => r.page!.start = '8193', r => r.page!.scanned = 4097, r => r.page!.nextCursor = '9',
    r => r.page!.cursor = '1', r => r.page!.rows.push(r.page!.rows[0]), r => r.page!.unknownPredecessors = 1, r => r.page!.exhausted = false,
    r => r.page!.rows[0].point.previousTagId = '8192', r => r.page!.rows[0].point.deltaMs = '-0', r => r.page!.rows[0].point.dts = '4294967296',
    r => r.page!.rows[0].point.offset = '201000', r => r.page!.rows[0].point.availableDataSize = 6, r => r.page!.rows[0].kind = 'gap',
    r => r.page!.gapThreshold = '0', r => r.page!.filter = 'all'];
  for (const mutate of changes) { const r = valid(); mutate(r); assert.throws(() => validateTimeSearch(r, q, '10000', '201000', '8192')); }
  const unknown = valid(); unknown.page!.rows[0].kind = 'unknown'; unknown.page!.unknownPredecessors = 1;
  Object.assign(unknown.page!.rows[0].point, { context: 'budget_exhausted', previousTagId: null, previousDts: null, deltaMs: null });
  validateTimeSearch(unknown, q, '10000', '201000', '8192');
  for (const field of ['tagId', 'offset', 'previousTagId', 'dts']) { const r = JSON.parse(JSON.stringify(valid())); r.page.rows[0].point[field] = 0; assert.throws(() => validateTimeSearch(r, q, '10000', '201000')); }
  assert.throws(() => timeSearchFilter('mismatches'));
});

test('normalizer restores nullable unknown context without coercing decimal tokens or accepting enum drift', () => {
  const q: TimeSearchRequest = { sessionId: 's', requestId: '9007199254740993', cursor: '0', filter: 'regressions', gapThreshold: '1000' }, raw = JSON.parse(JSON.stringify(ready(q)));
  raw.page.rows = [{ kind: 'unknown', point: { ...point('513'), context: 'budget_exhausted' } }]; raw.page.unknownPredecessors = 1;
  delete raw.error; for (const key of ['previousTagId', 'previousDts', 'deltaMs']) delete raw.page.rows[0].point[key];
  const normalized = timeSearchResponse(raw); validateTimeSearch(normalized, q, '10000', '201000');
  assert.equal(normalized.page!.nextCursor, q.requestId); assert.equal(normalized.page!.rows[0].point.deltaMs, null);
  assert.throws(() => timeSearchResponse({ ...raw, state: 'bad' })); raw.page.rows[0].kind = 'bad'; assert.throws(() => timeSearchResponse(raw));
});
