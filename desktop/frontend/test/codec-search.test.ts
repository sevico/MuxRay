import { test } from 'node:test';
import assert from 'node:assert/strict';
import { REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { CodecSearchRequest, CodecSearchResponse, DetailRequest, SaveRequest, SaveResponse } from '../src/protocol.ts';
import { codecSearchFilter, validateCodecSearch } from '../src/codec-search.ts';
import { codecSearchResponse } from '../src/normalize.ts';
import { deferred, failed, pending, ready, row, setup, settle } from './codec-search-fixture.ts';

test('codec search admits only explicit requests and follows opaque tokens across same-Tag SEI pages', async () => {
  const calls: CodecSearchRequest[] = [];
  const { controller } = setup({ startCodecSearch: async q => {
    calls.push(q); const first = calls.length === 1;
    const r = ready(q, { tagId: '0', frameIndex: 0 }, { tagId: first ? '0' : '1', frameIndex: 0 });
    Object.assign(r.page!, { scannedTags: first ? 1 : 0, bytesRead: first ? '900' : '0', pending: first ? 64 : 0,
      rows: Array.from({ length: 64 }, (_, i) => row('sei', { nalIndex: first ? 0 : 1, messageIndex: i })) });
    return r;
  } });
  await controller.open(); await settle(); assert.equal(calls.length, 0);
  controller.startCodecSearch('sei'); await settle();
  assert.equal(controller.getSnapshot().codecSearchPage?.pending, 64);
  controller.continueCodecSearch(); await settle();
  assert.deepEqual(calls.map(q => [q.requestId, q.cursor]), [['1', '0'], ['2', '1']]);
  assert.equal(controller.getSnapshot().codecSearchPage?.scannedTags, 0);
  assert.equal(controller.getSnapshot().codecSearchPage?.rows[0].nalIndex, 1);
  controller.startCodecSearch('mp3', 'parameters'); await settle();
  assert.equal(calls.at(-1)?.cursor, '0'); controller.dispose();
});

test('MP3 ready row selects the exact original frame and stale or stop rows never open frame detail', async () => {
  const details: DetailRequest[] = [];
  const { controller } = setup({ startDetail: async q => { details.push(q); return { apiVersion: 1, ...q, state: 'failed', detail: null, error: { code: 'detail_failed', message: '' } }; } });
  await controller.open(); await settle(); controller.startCodecSearch(); await settle();
  const first = controller.getSnapshot().codecSearchPage!.rows[0];
  controller.locateCodecSearchRow(first, 'frame'); await settle();
  assert.equal(controller.getSnapshot().selectedTag?.id, '0');
  assert.equal(details[0].mp3FrameIndex, 70); assert.equal(details[0].mp3PageStart, 64);
  controller.startCodecSearch('mp3', 'stops'); await settle();
  controller.locateCodecSearchRow(first, 'frame');
  controller.locateCodecSearchRow(controller.getSnapshot().codecSearchPage!.rows[0], 'frame'); await settle();
  assert.equal(details.length, 1);
  controller.locateCodecSearchRow(controller.getSnapshot().codecSearchPage!.rows[0], 'tag'); await settle();
  assert.equal(details.length, 2); assert.equal(details[1].mp3FrameIndex, undefined); controller.dispose();
});

test('SEI evidence, original SPS and selection source keep exact independent bit spans', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.startCodecSearch('sei'); await settle();
  const page = controller.getSnapshot().codecSearchPage!, item = page.rows[0];
  for (const [destination, span] of [['hex', item.span], ['sps', item.spsSpan], ['evidence', item.evidenceSpan]] as const) {
    controller.locateCodecSearchRow(item, destination); await settle();
    const selected = controller.getSnapshot().hexSelection!;
    assert.deepEqual([selected.offset, selected.bitOffset, selected.bitLength], [span!.byteOffset, span!.bitOffset, span!.bitLength]);
    assert.equal(controller.getSnapshot().codecSearchPage, page);
  }
  controller.dispose();
});

test('lost codec Start confirms its identity without starting new work and ignores late replies', async () => {
  const lost = deferred<CodecSearchResponse>(); let request!: CodecSearchRequest; let gets = 0;
  const { controller, clock } = setup({ startCodecSearch: q => { request = q; return lost.promise; }, codecSearchResult: async q => { gets++; assert.equal(q.requestId, request.requestId); return ready(request); } });
  await controller.open(); await settle(); controller.startCodecSearch(); clock.tick(REPLY_TIMEOUT_MS); await settle(); clock.tick(); await settle();
  assert.equal(gets, 1); assert.equal(controller.getSnapshot().codecSearchState, 'ready');
  lost.resolve(failed(request, 'source_changed')); await settle(); assert.equal(controller.getSnapshot().sourceChanged, false); controller.dispose();
});

test('codec cancel fences active reply, blocks all shared readers and retries the original cursor', async () => {
  const lost = deferred<CodecSearchResponse>(), calls: CodecSearchRequest[] = []; let request!: CodecSearchRequest;
  const { controller, clock, reads } = setup({ startCodecSearch: q => { calls.push(q); request = q; return calls.length === 1 ? lost.promise : Promise.resolve(ready(q)); },
    cancelCodecSearch: async () => pending(request, 'canceling'), codecSearchResult: async () => failed(request) });
  await controller.open(); await settle(); controller.startCodecSearch(); controller.cancelCodecSearch(); await settle();
  controller.selectTag('0'); controller.jumpHex('13'); controller.startSummary(); controller.startAVCSearch(); controller.startCodecSearch('sei');
  assert.deepEqual(reads, []); assert.equal(calls.length, 1);
  lost.resolve(ready(request)); await settle(); assert.equal(controller.getSnapshot().codecSearchPage, null);
  clock.tick(); await settle(); assert.equal(controller.getSnapshot().codecSearchRunning, false);
  controller.retryCodecSearch(); await settle(); assert.deepEqual(calls.map(q => [q.requestId, q.cursor]), [['1', '0'], ['2', '0']]); controller.dispose();
});

test('lost cancellation with pending confirmation retains the shared-reader guard and resends only that cancellation', async () => {
  let request!: CodecSearchRequest; const calls: string[] = [];
  const { controller, clock } = setup({ startCodecSearch: async q => { request = q; return pending(q); }, cancelCodecSearch: async q => { calls.push(q.requestId); if (calls.length === 1) throw Error('lost'); return failed(request); }, codecSearchResult: async () => pending(request) });
  await controller.open(); await settle(); controller.startCodecSearch(); await settle(); controller.cancelCodecSearch(); await settle(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().codecSearchError, 'codec_search_cancel_unconfirmed'); assert.equal(controller.getSnapshot().codecSearchRunning, true);
  controller.retryCodecSearch(); await settle(); assert.deepEqual(calls, ['1', '1']); assert.equal(controller.getSnapshot().codecSearchRunning, false); controller.dispose();
});

test('unadmitted codec request confirms with the same ID, while expired cursors require a clean restart', async () => {
  for (const code of ['codec_search_not_found', 'codec_search_cursor_expired']) {
    let count = 0; const calls: CodecSearchRequest[] = [];
    const { controller } = setup({ startCodecSearch: async q => { calls.push(q); return ++count === 1 ? failed(q, code) : ready(q); } });
    await controller.open(); await settle(); controller.startCodecSearch(); await settle();
    controller.retryCodecSearch(); await settle();
    if (code === 'codec_search_not_found') assert.deepEqual(calls.map(q => q.requestId), ['1', '1']);
    else { assert.equal(calls.length, 1); assert.equal(controller.getSnapshot().codecSearchRetry, ''); controller.startCodecSearch(); await settle(); assert.deepEqual(calls.map(q => [q.requestId, q.cursor]), [['1', '0'], ['2', '0']]); }
    controller.dispose();
  }
});

test('matching source change invalidates all evidence; wrong identity and wrong scan-complete claims are rejected first', async () => {
  for (const mismatch of [true, false]) {
    const { controller } = setup({ startCodecSearch: async q => ({ ...failed(q, 'source_changed'), sessionId: mismatch ? 'old' : q.sessionId }) });
    await controller.open(); await settle(); controller.startCodecSearch(); await settle();
    assert.equal(controller.getSnapshot().sourceChanged, !mismatch); assert.equal(controller.getSnapshot().codecSearchPage, null); controller.dispose();
  }
  const { controller } = setup({ startCodecSearch: async q => { const r = ready(q); r.page!.scanComplete = true; return r; } });
  await controller.open(); await settle(); controller.startCodecSearch(); await settle(); assert.equal(controller.getSnapshot().codecSearchError, 'invalid_response'); controller.dispose();
});

test('close or dispose discards delayed codec responses and every timer', async () => {
  for (const close of [true, false]) {
    const lost = deferred<CodecSearchResponse>(); let request!: CodecSearchRequest;
    const { controller, clock } = setup({ startCodecSearch: q => { request = q; return lost.promise; } });
    await controller.open(); await settle(); controller.startCodecSearch(); if (close) await controller.close(); else controller.dispose();
    lost.resolve(ready(request)); await settle(); assert.equal(controller.getSnapshot().codecSearchPage, null); assert.equal(clock.tasks.size, 0);
  }
});

test('codec validation rejects forged cursor/coverage/budget/identity/source values and accepts null stop spans', () => {
  const q: CodecSearchRequest = { sessionId: 's', requestId: '9007199254740993', cursor: '0', kind: 'mp3', filter: 'all' };
  validateCodecSearch(ready(q), q, '600', '10000');
  const changes: ((r: CodecSearchResponse) => void)[] = [r => r.page!.nextCursor = '2', r => r.page!.cursor = '1', r => r.page!.scannedFrames = 1025,
    r => r.page!.bytesRead = '65537', r => r.page!.scannedTags = 257, r => r.page!.pending = 1, r => r.page!.rows.push(r.page!.rows[0]),
    r => r.page!.start.frameIndex = 1, r => r.page!.next.tagId = '601', r => r.page!.rows[0].frameIndex = 4294967296,
    r => r.page!.rows[0].span!.byteOffset = '10000', r => r.page!.rows[0].dts = '9223372036854775808', r => r.page!.rows[0].complete = false,
    r => r.page!.rows[0].spsSpan = r.page!.rows[0].span, r => r.page!.exhausted = true, r => r.page!.kind = 'sei'];
  for (const mutate of changes) { const r = ready(q); mutate(r); assert.throws(() => validateCodecSearch(r, q, '600', '10000')); }
  const stop = ready(q); stop.page!.rows = [row('mp3', { kind: 'stop', complete: false, status: 'missingAudioHeader', span: null })];
  validateCodecSearch(stop, q, '600', '10000');
  for (const key of ['offset', 'dts', 'tagId']) { const r = JSON.parse(JSON.stringify(ready(q))); r.page.rows[0][key] = 0; assert.throws(() => validateCodecSearch(r, q, '600', '10000')); }
  assert.throws(() => codecSearchFilter('mp3', 'pictureTiming')); assert.throws(() => codecSearchFilter('sei', 'bitrate'));
});

test('SEI validation accepts saved rows without rereading, but rejects false unknown matches and over-budget pending', () => {
  const q: CodecSearchRequest = { sessionId: 's', requestId: '3', cursor: '1', kind: 'sei', filter: 'all' };
  const r = ready(q, { tagId: '1', frameIndex: 0 }, { tagId: '1', frameIndex: 0 });
  Object.assign(r.page!, { scannedTags: 0, bytesRead: '0', pending: 193 });
  validateCodecSearch(r, q, '600', '10000', { tagId: '1', frameIndex: 0 });
  for (const mutate of [(x: CodecSearchResponse) => x.page!.pending = 258, (x: CodecSearchResponse) => x.page!.rows = [],
    (x: CodecSearchResponse) => x.page!.scannedFrames = 1, (x: CodecSearchResponse) => x.page!.rows[0].frameIndex = 0]) {
    const x = structuredClone(r); mutate(x); assert.throws(() => validateCodecSearch(x, q, '600', '10000'));
  }
  const unknown = { ...q, filter: 'unknown' as const }, x = ready(unknown); x.page!.rows = [row('sei', { kind: 'unknown', complete: true, status: 'opaque', contextState: 'not_applicable' })];
  assert.throws(() => validateCodecSearch(x, unknown, '600', '10000'));
  assert.throws(() => validateCodecSearch(r, q, '600', '10000', { tagId: '2', frameIndex: 0 }));
});

function saved(requestId: string): SaveResponse { return { apiVersion: 1, requestId, pending: false, canceled: false, fileName: 'evidence.json', bytesWritten: '1024', error: null }; }
test('snapshot uses v4 only for current ready codec identity and freezes it through source navigation, changes and close', async () => {
  const saves: SaveRequest[] = []; const saving = deferred<SaveResponse>();
  const { controller } = setup({ saveSnapshot: q => { saves.push(q); return saves.length === 2 ? saving.promise : Promise.resolve(saved(q.requestId)); } });
  await controller.open(); await settle(); await controller.saveSnapshot(); assert.equal(saves[0].snapshot.schemaVersion, 3); assert.equal(saves[0].snapshot.codecSearch, undefined);
  controller.startCodecSearch('sei', 'pictureTiming'); await settle(); controller.locateCodecSearchRow(controller.getSnapshot().codecSearchPage!.rows[0], 'sps'); await settle();
  const waiting = controller.saveSnapshot();
  assert.equal(saves[1].snapshot.schemaVersion, 4); assert.deepEqual(saves[1].snapshot.codecSearch, { requestId: '1', cursor: '0', kind: 'sei', filter: 'pictureTiming' });
  assert.equal(Object.isFrozen(saves[1].snapshot.codecSearch), true);
  controller.startCodecSearch('mp3'); await settle(); await controller.close();
  assert.deepEqual(saves[1].snapshot.codecSearch, { requestId: '1', cursor: '0', kind: 'sei', filter: 'pictureTiming' });
  saving.resolve(saved(saves[1].requestId)); await waiting; controller.dispose();
});

test('pending, canceling and failed codec work never export a stale ready page or select snapshot v4', async () => {
  const saves: SaveRequest[] = []; let request!: CodecSearchRequest;
  const { controller, clock } = setup({ startCodecSearch: async q => { request = q; return pending(q); }, cancelCodecSearch: async () => pending(request, 'canceling'), codecSearchResult: async () => failed(request),
    saveSnapshot: async q => { saves.push(q); return saved(q.requestId); } });
  await controller.open(); await settle(); controller.startCodecSearch(); await settle(); await controller.saveSnapshot();
  controller.cancelCodecSearch(); await settle(); await controller.saveSnapshot(); clock.tick(); await settle(); await controller.saveSnapshot();
  assert.equal(saves.length, 3); assert.ok(saves.every(q => q.snapshot.schemaVersion === 3 && q.snapshot.codecSearch === undefined)); controller.dispose();
});

test('normalizer preserves exact tokens and converts only nullable generated fields', () => {
  const q: CodecSearchRequest = { sessionId: 's', requestId: '9007199254740993', cursor: '0', kind: 'mp3', filter: 'all' };
  const wire = JSON.parse(JSON.stringify(ready(q)));
  delete wire.error; for (const key of ['nalIndex', 'messageIndex', 'spsSpan', 'evidenceSpan']) delete wire.page.rows[0][key];
  const value = codecSearchResponse(wire);
  assert.equal(value.page!.nextCursor, '9007199254740993'); assert.equal(value.page!.rows[0].frameIndex, 70);
  assert.equal(value.page!.rows[0].nalIndex, null); assert.equal(value.page!.rows[0].spsSpan, null); assert.equal(value.error, null);
  assert.throws(() => codecSearchResponse({ ...wire, state: 'wrong' }));
});

test('codec row validation rejects reordered frames/messages, premature coverage and contradictory SEI context', () => {
  for (const kind of ['mp3', 'sei'] as const) {
    const q: CodecSearchRequest = { sessionId: 's', requestId: '1', cursor: '0', kind, filter: 'all' }, r = ready(q);
    r.page!.rows.push(row(kind, kind === 'mp3' ? { frameIndex: 69 } : { nalIndex: 0, messageIndex: 1 }));
    if (kind === 'sei') r.page!.rows.reverse();
    assert.throws(() => validateCodecSearch(r, q, '600', '10000'));
  }
  const q: CodecSearchRequest = { sessionId: 's', requestId: '1', cursor: '0', kind: 'sei', filter: 'all' };
  const changes: ((r: CodecSearchResponse) => void)[] = [r => r.page!.rows[0].messageIndex = 64, r => r.page!.rows[0].spsSpan = null,
    r => r.page!.rows[0].contextState = 'missing', r => r.page!.rows[0].status = 'incomplete',
    r => r.page!.rows.unshift(row('sei', { kind: 'coverage', complete: false, status: 'unknown', contextState: 'not_applicable', nalIndex: null, messageIndex: null })),
    r => r.page!.rows[0].values.push(r.page!.rows[0].values[0]), r => r.page!.rows[0].dts = '4294967296'];
  for (const mutate of changes) { const r = ready(q); mutate(r); assert.throws(() => validateCodecSearch(r, q, '600', '10000')); }
});

test('completed failures in AVC/summary cannot be restarted while codec work owns the shared reader', async () => {
  let oldCalls = 0;
  const { controller } = setup({ startAVCSearch: async q => { oldCalls++; return { apiVersion: 1, ...q, state: 'failed', page: null, error: { code: 'avc_search_failed', message: '' } }; },
    startSummary: async q => { oldCalls++; return { apiVersion: 1, ...q, state: 'failed', overview: null, error: { code: 'summary_failed', message: '' }, processed: '0', total: '600' }; },
    startCodecSearch: async q => pending(q) });
  await controller.open(); await settle(); controller.startAVCSearch(); await settle(); controller.startSummary(); await settle();
  assert.equal(oldCalls, 2); controller.startCodecSearch(); await settle();
  controller.retryAVCSearch(); controller.retrySummary(); await settle(); assert.equal(oldCalls, 2); assert.equal(controller.getSnapshot().codecSearchRunning, true); controller.dispose();
});

test('snapshot v4 retains the existing ready AVC search selection', async () => {
  const saves: SaveRequest[] = [];
  const { controller } = setup({ startAVCSearch: async q => ({ apiVersion: 1, ...q, state: 'ready', error: null, page: { start: '0', next: '1', total: '600', scanned: 1, bytesRead: '5', exhausted: false, scanComplete: false, filter: q.filter, rows: [] } }),
    saveSnapshot: async q => { saves.push(q); return saved(q.requestId); } });
  await controller.open(); await settle(); controller.startAVCSearch(); await settle(); controller.startCodecSearch(); await settle(); await controller.saveSnapshot();
  assert.equal(saves[0].snapshot.schemaVersion, 4); assert.deepEqual(saves[0].snapshot.avcSearch, { requestId: '1', start: '0', filter: 'mismatches' });
  assert.deepEqual(saves[0].snapshot.codecSearch, { requestId: '2', cursor: '0', kind: 'mp3', filter: 'all' }); controller.dispose();
});
