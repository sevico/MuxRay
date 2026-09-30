// Synthetic save protocol only: no dialog, filesystem, Go exporter, or DOM runs.
import assert from 'node:assert/strict';
import test from 'node:test';
import { syntheticHost } from './browser-fixture.mjs';
import { validateTagTrendPage } from '../src/tag-trend-protocol.ts';

async function host(mode = 'snapshot-success') {
  globalThis.window = { location: { href: `http://127.0.0.1/?smoke=${mode}` } };
  (0, eval)(`(${syntheticHost.toString()})()`);
  const native = window.go.main.Native, api = window.go.desktop.Bindings;
  const opened = await native.OpenFLV('1');
  return { native, api, session: opened.session };
}
function request(h, requestId = '1') {
  return { requestId, snapshot: { sessionId: h.session.sessionId, selectedTagId: '257',
    detailRequestId: null, hexRequestId: null, summaryRequestId: null, summaryPage: null,
    trend: { start: '256', count: 256, type: 'video', metric: 'delta', gapThreshold: '2001' } } };
}
function check(response, requestId, state) {
  assert.deepEqual(Object.keys(response).sort(), ['apiVersion', 'bytesWritten', 'canceled', 'error', 'fileName', 'pending', 'requestId']);
  assert.equal(response.apiVersion, 1);
  assert.equal(response.requestId, requestId);
  assert.match(response.bytesWritten, /^(0|[1-9][0-9]*)$/);
  assert.equal(response.pending, state === 'pending');
  assert.equal(response.canceled, state === 'canceled');
  if (state === 'saved') {
    assert.equal(response.error, null); assert.ok(response.fileName.endsWith('.json'));
    assert.ok(BigInt(response.bytesWritten) > 0n);
  } else {
    assert.equal(response.fileName, ''); assert.equal(response.bytesWritten, '0');
    if (state === 'error') assert.ok(response.error?.code);
    else assert.equal(response.error, null);
  }
}
async function withHeldSave(action) {
  const original = globalThis.setTimeout, callbacks = [];
  globalThis.setTimeout = (callback, delay) => {
    assert.equal(delay, 6500); callbacks.push(callback); return 0;
  };
  try { await action(() => { assert.equal(callbacks.length, 1); callbacks.shift()(); }); }
  finally { globalThis.setTimeout = original; }
}

test('successful save freezes the view, returns exact bytes and has no source read side effects', async () => {
  const h = await host(), q = request(h);
  const trendRequest = { sessionId: h.session.sessionId, start: '256', count: 256 };
  const trend = await h.api.GetTagTrends(trendRequest);
  validateTagTrendPage(trend.page, trendRequest);
  assert.deepEqual([trend.page.bounds.total, trend.page.points.length], ['300', 44]);
  const before = await h.api.GetSession(h.session.sessionId), result = await h.native.SaveSnapshot(q);
  check(result, '1', 'saved');
  assert.deepEqual(await h.api.GetSession(h.session.sessionId), before);
  assert.deepEqual(window.smokeReads, []);
  assert.equal(window.smokeSaveCaptures.length, 1);
  assert.equal(window.smokeSavedSnapshots.length, 1);
  const saved = window.smokeSavedSnapshots[0], parsed = JSON.parse(saved.data);
  assert.deepEqual(parsed.selection, q.snapshot);
  assert.equal(parsed.session.progress.tagCount, '300');
  assert.equal(result.bytesWritten, String(new TextEncoder().encode(saved.data).length));
  q.snapshot.trend.start = '0';
  assert.equal(parsed.selection.trend.start, '256');
  assert.equal(window.smokeSaveCaptures[0].capture.selection.trend.start, '256');
});

test('Wails optional pointers omitted as undefined normalize to explicit null', async () => {
  const h = await host(), q = { requestId: '1', snapshot: { sessionId: h.session.sessionId } };
  check(await h.native.SaveSnapshot(q), '1', 'saved');
  const selection = JSON.parse(window.smokeSavedSnapshots[0].data).selection;
  for (const name of ['selectedTagId', 'detailRequestId', 'hexRequestId', 'summaryRequestId', 'summaryPage', 'trend']) assert.equal(selection[name], null);
});

test('dialog cancellation is retained and repeated same-ID calls do not capture or write twice', async () => {
  const h = await host('snapshot-cancel'), q = request(h);
  const result = await h.native.SaveSnapshot(q);
  check(result, '1', 'canceled');
  assert.deepEqual(await h.native.GetSaveResult('1'), result);
  assert.deepEqual(await h.native.SaveSnapshot(q), result);
  assert.equal(window.smokeSaveCaptures.length, 1);
  assert.deepEqual(window.smokeSavedSnapshots, []);
  assert.equal((await h.api.GetSession(h.session.sessionId)).session.closed, false);
});

test('lost initial reply is recovered by query without reopening a dialog or repeating save', async () => {
  const h = await host('snapshot-lost-reply'), q = request(h);
  let settled = false;
  h.native.SaveSnapshot(q).then(() => { settled = true; });
  const recovered = await h.native.GetSaveResult('1');
  check(recovered, '1', 'saved');
  assert.equal(settled, false);
  assert.deepEqual(await h.native.SaveSnapshot(q), recovered);
  assert.equal(window.smokeSaveCaptures.length, 1);
  assert.equal(window.smokeSavedSnapshots.length, 1);
});

test('a lost recovery query can be retried with the same identity', async () => {
  const h = await host('snapshot-recovery-timeout');
  void h.native.SaveSnapshot(request(h));
  let querySettled = false;
  h.native.GetSaveResult('1').then(() => { querySettled = true; });
  const recovered = await h.native.GetSaveResult('1');
  check(recovered, '1', 'saved');
  assert.equal(querySettled, false);
  assert.deepEqual(window.smokeSaveQueries, ['1', '1']);
  assert.equal(window.smokeSaveCalls.length, 1);
  assert.equal(window.smokeSavedSnapshots.length, 1);
});

test('pending save survives source view closure and prevents newer dialog requests from superseding it', async () => {
  await withHeldSave(async release => {
    const h = await host('snapshot-delayed'), q = request(h);
    const pending = h.native.SaveSnapshot(q);
    check(await h.native.GetSaveResult('1'), '1', 'pending');
    assert.equal((await h.native.GetSaveResult('2')).error.code, 'busy');
    assert.equal((await h.native.SaveSnapshot(request(h, '2'))).error.code, 'busy');
    assert.equal((await h.native.OpenFLV('2')).error.code, 'busy');
    await h.api.CloseFile(h.session.sessionId);
    q.snapshot.selectedTagId = '8'; q.snapshot.trend.metric = 'size';
    release();
    const result = await pending;
    check(result, '1', 'saved');
    const saved = JSON.parse(window.smokeSavedSnapshots[0].data);
    assert.equal(saved.session.closed, false);
    assert.equal(saved.selection.selectedTagId, '257');
    assert.equal(saved.selection.trend.metric, 'delta');
    const reopened = await h.native.OpenFLV('2');
    assert.notEqual(reopened.session.sessionId, h.session.sessionId);
    assert.deepEqual(await h.native.GetSaveResult('1'), result);
    assert.equal(window.smokeSaveCaptures.length, 1);
  });
});

test('write failure preserves the session and ready Summary and never reports saved bytes', async () => {
  const h = await host('snapshot-write-failed');
  const summaryRequest = { sessionId: h.session.sessionId, requestId: 's1' };
  const summary = await h.api.StartSummary(summaryRequest), session = await h.api.GetSession(h.session.sessionId);
  const reads = structuredClone(window.smokeReads), failed = await h.native.SaveSnapshot(request(h));
  check(failed, '1', 'error');
  assert.equal(failed.error.code, 'write_failed');
  assert.deepEqual(await h.native.GetSaveResult('1'), failed);
  assert.deepEqual(await h.api.GetSession(h.session.sessionId), session);
  assert.deepEqual(await h.api.GetSummary(summaryRequest), summary);
  assert.deepEqual(window.smokeReads, reads);
  assert.deepEqual(window.smokeSavedSnapshots, []);
});

test('an unseen recovery ID seals the late save and stale requests cannot write', async () => {
  const h = await host();
  const sealed = await h.native.GetSaveResult('2');
  check(sealed, '2', 'canceled');
  assert.deepEqual(await h.native.SaveSnapshot(request(h, '2')), sealed);
  assert.equal((await h.native.SaveSnapshot(request(h, '1'))).error.code, 'stale_request');
  assert.equal((await h.native.GetSaveResult('1')).error.code, 'stale_request');
  assert.deepEqual(window.smokeSaveCaptures, []);
  assert.deepEqual(window.smokeSavedSnapshots, []);
});

test('canonical uint64 request IDs and detached query responses match the native recovery contract', async () => {
  const h = await host();
  for (const id of ['0', '01', '-1', '18446744073709551616', '', '1e2']) {
    assert.equal((await h.native.SaveSnapshot(request(h, id))).error.code, 'invalid_argument');
    assert.equal((await h.native.GetSaveResult(id)).error.code, 'invalid_argument');
  }
  const max = '18446744073709551615';
  const result = await h.native.SaveSnapshot(request(h, max));
  check(result, max, 'saved');
  result.fileName = 'mutated.json';
  assert.equal((await h.native.GetSaveResult(max)).fileName, 'browser-synthetic.muxray.json');
  assert.equal(window.smokeSavedSnapshots.length, 1);
});
