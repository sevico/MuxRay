// Synthetic host contract checks: no DOM, Wails runtime, parser, or GUI runs.
import assert from 'node:assert/strict';
import test from 'node:test';
import { syntheticHost } from './browser-fixture.mjs';
import { validateTagTrendPage } from '../src/tag-trend-protocol.ts';
import { validateSummaryResponse } from '../src/summary.ts';

async function host(mode = 'trends') {
  globalThis.window = { location: { href: `http://127.0.0.1/?smoke=${mode}` } };
  // Match browser-host.mjs injection: no outer module bindings are available.
  (0, eval)(`(${syntheticHost.toString()})()`);
  const opened = await window.go.main.Native.OpenFLV('open-1');
  return { api: window.go.desktop.Bindings, native: window.go.main.Native,
    sessionId: opened.session.sessionId, sourceSize: opened.session.progress.sourceSize };
}
async function page(h, start = '0', count = 256) {
  const request = { sessionId: h.sessionId, start, count };
  const response = await h.api.GetTagTrends(request);
  assert.equal(response.error, null);
  validateTagTrendPage(response.page, request, h.sourceSize);
  return response.page;
}

test('normal trends cross the 256 boundary with exact stream-specific DTS deltas', async () => {
  const h = await host(), first = await page(h), second = await page(h, first.bounds.next);
  assert.deepEqual([first.points.length, first.bounds.next, first.contextScanned], [256, '256', 0]);
  assert.deepEqual([second.points.length, second.bounds.next, second.contextStart, second.contextScanned], [44, '300', '0', 256]);
  const points = new Map(second.points.map(p => [p.tagId, p]));
  for (const [id, previous, delta] of [['256', '254', '0'], ['257', '255', '-100'], ['258', '256', '2001'],
    ['260', '258', '4294960294'], ['261', '259', '-40'], ['264', '260', '-4294967295']]) {
    assert.deepEqual([points.get(id).previousTagId, points.get(id).deltaMs], [previous, delta]);
  }
  assert.deepEqual([points.get('259').streamId, points.get('259').context], [1, 'start']);
  assert.equal(points.get('262').filtered, true);
  const short = await page(h, '20', 2);
  assert.deepEqual([short.contextStart, short.contextScanned], ['11', 9]);
});

test('4096-source-record budget retains exact-boundary predecessor and recovers within window', async () => {
  const h = await host('trends-sparse'), result = await page(h, '4097');
  assert.deepEqual([result.contextStart, result.contextScanned, result.points.length, result.bounds.next], ['1', 4096, 256, '4353']);
  const points = result.points;
  assert.deepEqual([points[0].previousTagId, points[0].deltaMs], ['1', '100']);
  assert.deepEqual([points[1].context, points[1].previousTagId, points[1].deltaMs], ['budget_exhausted', null, null]);
  assert.deepEqual([points[2].previousTagId, points[2].deltaMs], ['4098', '10']);
  assert.deepEqual([points[3].previousTagId, points[3].deltaMs], ['4097', '-10']);
  assert.equal(points[4].context, 'budget_exhausted');
  const last = await page(h, '4353');
  assert.deepEqual([last.points.length, last.bounds.next], [1, '4354']);
});

test('structures, partial payload, and EOF remain separate metadata facts', async () => {
  const h = await host('trends-structures'), result = await page(h);
  assert.deepEqual(result.points.filter(p => p.previousTagSizeState === 'invalid').map(p => p.tagId), ['1', '3']);
  assert.deepEqual(result.points.filter(p => p.previousTagSizeState === 'missing').map(p => p.tagId), ['6']);
  assert.equal(result.points[2].filtered, true);
  assert.equal(result.points[4].streamId, 16777215);
  const partial = await page(await host('trends-truncated'));
  assert.deepEqual([partial.points[1].dataSize, partial.points[1].availableDataSize, partial.points[1].payloadComplete, partial.points[1].previousTagSizeState], [10, 2, false, 'missing']);
  const empty = await page(await host('trends-empty'));
  assert.deepEqual([empty.points, empty.contextScanned, empty.contextStart, empty.bounds.next], [[], 0, '0', '0']);
  const terminal = await page(await host(), '300');
  assert.deepEqual([terminal.points, terminal.contextScanned, terminal.contextStart], [[], 0, '300']);
});

test('512-point API bound and invalid window requests are enforced', async () => {
  const h = await host('trends-sparse'), result = await page(h, '4097', 512);
  assert.equal(result.points.length, 257);
  assert.equal((await page(h, '0', 512)).points.length, 512);
  for (const [start, count] of [['0', 0], ['0', 513], ['4355', 256], ['01', 256], ['-1', 256]]) {
    const response = await h.api.GetTagTrends({ sessionId: h.sessionId, start, count });
    assert.deepEqual([response.page, response.error.code], [null, 'invalid_argument']);
  }
});

test('huge virtual coordinates remain exact through trend, Tag page, detail, and Summary', async () => {
  const h = await host('trends-huge'), start = '9007199254740993', result = await page(h, start);
  assert.equal(result.points[0].tagId, start);
  assert.equal(result.points[1].tagId, '9007199254740994');
  assert.equal(result.bounds.next, '9007199254741249');
  assert.equal(result.points[0].offset, '909727124728840293');
  const tags = await h.api.GetTags({ sessionId: h.sessionId, start, count: 100 });
  assert.deepEqual([tags.page.tags[0].id, tags.page.tags[0].offset], [start, result.points[0].offset]);
  const detail = await h.api.StartDetail({ sessionId: h.sessionId, requestId: 'd1', tagId: start });
  assert.equal(detail.detail.fields[0].span.byteOffset, String(BigInt(result.points[0].offset) + 11n));
  const summary = await h.api.StartSummary({ sessionId: h.sessionId, requestId: 's1' });
  validateSummaryResponse(summary, h.sessionId, 's1');
  const total = summary.overview.metrics.filter(m => m.section === 'tags').reduce((sum, m) => sum + BigInt(m.value), 0n);
  assert.equal(String(total), result.bounds.total);
});

test('all trend datasets keep Summary totals and source type counts consistent', async () => {
  const expectedCounts = { trends: [153, 143, 2, 2], 'trends-sparse': [3, 3, 4347, 1],
    'trends-structures': [3, 2, 1, 1], 'trends-truncated': [1, 0, 1, 0], 'trends-empty': [0, 0, 0, 0] };
  for (const mode of ['trends', 'trends-sparse', 'trends-structures', 'trends-truncated', 'trends-empty']) {
    const h = await host(mode), result = await page(h), summary = await h.api.StartSummary({ sessionId: h.sessionId, requestId: 'summary-1' });
    validateSummaryResponse(summary, h.sessionId, 'summary-1');
    assert.equal(summary.processed, result.bounds.total);
    assert.equal(summary.total, result.bounds.total);
    assert.equal(summary.overview.indexedTags, result.bounds.total);
    const counts = summary.overview.metrics.filter(m => m.section === 'tags');
    assert.deepEqual(['audio', 'video', 'script', 'other'].map(name => Number(counts.find(m => m.name === name).value)), expectedCounts[mode]);
    assert.equal(String(counts.reduce((sum, metric) => sum + BigInt(metric.value), 0n)), result.bounds.total);
    assert.ok(summary.overview.lists.every(list => list.retained === '0'));
  }
});

test('lost first trend response allows a separate retry to complete', async () => {
  const h = await host('trends-timeout');
  const first = h.api.GetTagTrends({ sessionId: h.sessionId, start: '0', count: 256 });
  let settled = false;
  first.then(() => { settled = true; }, () => { settled = true; });
  const retry = await page(h);
  assert.equal(retry.points.length, 256);
  assert.equal(settled, false);
  assert.equal(window.smokeTrendCalls.length, 2);
});

test('delayed trend response preserves old session after close and reopen', async () => {
  const h = await host('trends-delayed');
  const request = { sessionId: h.sessionId, start: '256', count: 256 };
  const pending = h.api.GetTagTrends(request);
  await h.api.CloseFile(h.sessionId);
  const reopened = await h.native.OpenFLV('open-2');
  const old = await pending;
  validateTagTrendPage(old.page, request, h.sourceSize);
  assert.equal(old.page.sessionId, h.sessionId);
  assert.notEqual(old.page.sessionId, reopened.session.sessionId);
});
