// Host plumbing checks only. No DOM, native Wails, or Go parser is exercised.
import assert from 'node:assert/strict';
import test from 'node:test';
import { syntheticHost } from './browser-fixture.mjs';
import { validateFilteredTagPage } from '../src/tag-filter.ts';
import { validateSummaryResponse, validateSummaryPage } from '../src/summary.ts';

async function host(mode) {
  globalThis.window = { location: { href: `http://127.0.0.1/?smoke=${mode}` } };
  syntheticHost();
  const opened = await window.go.main.Native.OpenFLV('1');
  return { api: window.go.desktop.Bindings, sessionId: opened.session.sessionId };
}
const all = { type: 'all', dtsMin: null, dtsMax: null, structure: 'all' };
const ids = response => response.page.tags.map(tag => Number(tag.id));
async function query(h, filter, start = '0') {
  const request = { sessionId: h.sessionId, start, count: 100, filter: { ...all, ...filter } };
  const response = await h.api.GetFilteredTags(request);
  validateFilteredTagPage(response.page, request);
  return response;
}

test('sparse fixture advances past empty windows and preserves global IDs', async () => {
  const h = await host('filter-sparse');
  const first = await query(h, { type: 'audio' });
  assert.deepEqual([ids(first), first.page.next, first.page.scanned, first.page.exhausted], [[], '4096', 4096, false]);
  const second = await query(h, { type: 'audio' }, first.page.next);
  assert.deepEqual([ids(second), second.page.next, second.page.exhausted], [[4500, 5000], '8192', false]);
  const final = await query(h, { type: 'audio' }, second.page.next);
  assert.deepEqual([ids(final), final.page.next, final.page.scanned, final.page.exhausted], [[8200], '8305', 113, true]);
  assert.equal(final.page.tags[0].offset, '9007199254749193');
});

test('multipage cursor follows examined records, not hit counts', async () => {
  const h = await host('filter-multipage');
  const first = await query(h, { type: 'audio' });
  assert.deepEqual([first.page.tags.length, first.page.next, first.page.scanned], [100, '199', 199]);
  const second = await query(h, { type: 'audio' }, first.page.next);
  assert.deepEqual([second.page.tags.length, second.page.next, second.page.exhausted], [53, '305', true]);
  assert.deepEqual(ids(first).concat(ids(second)), Array.from({ length: 153 }, (_, i) => i * 2));
});

test('generated Wails requests may omit unbounded optional DTS fields', async () => {
  const h = await host('filter-sparse');
  const response = await h.api.GetFilteredTags({ sessionId: h.sessionId, start: '4096', count: 100,
    filter: { type: 'audio', structure: 'all' } });
  assert.deepEqual(ids(response), [4500, 5000]);
  assert.deepEqual(response.page.filter, { type: 'audio', structure: 'all', dtsMin: null, dtsMax: null });
});

test('ranges use inclusive raw DTS with repeated nonmonotonic values', async () => {
  const h = await host('filter-ranges');
  assert.deepEqual(ids(await query(h, { dtsMin: '10000', dtsMax: '12000' })), [0, 1, 2, 3, 6, 7, 8, 11]);
  assert.deepEqual(ids(await query(h, { type: 'video', dtsMin: '10000', dtsMax: '12000' })), [1, 7, 11]);
  assert.deepEqual(ids(await query(h, { dtsMin: '4294967295', dtsMax: '4294967295' })), [9]);
  assert.deepEqual(ids(await query(h, { type: 'other' })), [3, 9]);
});

test('missing previous-size is distinct from present-but-invalid', async () => {
  let h = await host('filter-structures');
  assert.deepEqual(ids(await query(h, { structure: 'previous_size_invalid' })), [1, 3]);
  assert.deepEqual(ids(await query(h, { structure: 'previous_size_missing' })), [6]);
  h = await host('filter-truncated');
  assert.deepEqual(ids(await query(h, { structure: 'payload_incomplete' })), [1]);
  assert.deepEqual(ids(await query(h, { structure: 'previous_size_missing' })), [1]);
  assert.deepEqual(ids(await query(h, { structure: 'previous_size_invalid' })), []);
});

test('empty index ends without fabricated matches', async () => {
  const h = await host('filter-empty'), response = await query(h, {});
  assert.deepEqual([ids(response), response.page.start, response.page.next, response.page.indexedTotal, response.page.scanned, response.page.exhausted], [[], '0', '0', '0', 0, true]);
});

test('filter Summary progress, type counts, and event coordinates use their opened dataset', async () => {
  for (const [mode, total, counts] of [
    ['filter-ranges', 12, [4, 4, 2, 2]], ['filter-multipage', 305, [153, 152, 0, 0]],
    ['filter-sparse', 8305, [3, 1, 8301, 0]], ['filter-structures', 7, [3, 2, 1, 1]],
    ['filter-truncated', 2, [1, 0, 1, 0]], ['filter-empty', 0, [0, 0, 0, 0]],
    ['filter-delayed', 12, [4, 4, 2, 2]], ['filter-timeout', 12, [4, 4, 2, 2]],
  ]) {
    const h = await host(mode), request = { sessionId: h.sessionId, requestId: '1' };
    const response = await h.api.StartSummary(request);
    validateSummaryResponse(response);
    assert.deepEqual([response.processed, response.total, response.overview.indexedTags], Array(3).fill(String(total)), mode);
    assert.deepEqual(response.overview.metrics.filter(metric => metric.section === 'tags').map(metric => Number(metric.value)), counts, mode);
    for (const list of response.overview.lists) {
      const events = await h.api.GetSummaryEvents({ ...request, kind: list.kind, start: '0', count: 100 });
      validateSummaryPage(events.page, '0', 100);
      assert.equal(events.page.events.length, Number(list.retained), mode);
      for (const event of events.page.events) {
        assert.ok(BigInt(event.tagId) < BigInt(total), mode);
        assert.ok(BigInt(event.dts) >= 0n && BigInt(event.dts) <= 4294967295n, mode);
      }
    }
  }
});

test('legacy Summary integer and retention fixtures remain separate from filtering', async () => {
  const h = await host('summary-short-page'), request = { sessionId: h.sessionId, requestId: '1' };
  const response = await h.api.StartSummary(request);
  validateSummaryResponse(response);
  assert.deepEqual([response.processed, response.total, response.overview.indexedTags], ['205', '205', '205']);
  assert.equal(response.overview.lists.find(list => list.kind === 'keyframes').retained, '205');
  const page = await h.api.GetSummaryEvents({ ...request, kind: 'keyframes', start: '0', count: 100 });
  validateSummaryPage(page.page, '0', 100);
  assert.equal(page.page.events.length, 37);
  assert.equal(page.page.events[0].tagId, '105');
  assert.equal(page.page.events[0].dts, '-9223372036854775808');
});
