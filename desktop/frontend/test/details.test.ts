import { test } from 'node:test';
import assert from 'node:assert/strict';
import { avcReplayProgress, detailRows, fieldValue } from '../src/details.ts';
import type { Detail, DetailField } from '../src/protocol.ts';

function field(id: string, parentId = ''): DetailField { return { id, parentId, name: '<script>metadata</script>', type: 'string', value: '值'.repeat(20), span: { byteOffset: '18446744073709551615', bitOffset: 3, bitLength: 5 } }; }
test('field hierarchy keeps exact transport text and positions while bounding display depth', () => {
  const fields = Array.from({ length: 12 }, (_, index) => field(String(index), index ? String(index - 1) : ''));
  const rows = detailRows(fields);
  assert.deepEqual(rows.map(row => row.depth), [0, 1, 2, 3, 4, 5, 6, 7, 8, 8, 8, 8]);
  assert.equal(rows[0].field, fields[0]); assert.equal(rows[0].field.span?.byteOffset, '18446744073709551615');
  assert.equal(rows[0].field.value, '值'.repeat(20));
});
test('missing parents and cyclic field references cannot recurse or reorder fields', () => {
  const fields = [field('a', 'b'), field('b', 'a'), field('c', 'missing'), field('d', 'd')];
  assert.deepEqual(detailRows(fields).map(row => [row.field.id, row.depth]), [['a', 1], ['b', 1], ['c', 0], ['d', 0]]);
});
test('empty UTF-8 field values remain distinct from container placeholders', () => {
  assert.equal(fieldValue({ ...field('0'), type: 'utf8', value: '' }), '""');
  assert.equal(fieldValue({ ...field('0'), type: 'object', value: '' }), '—');
  assert.equal(fieldValue({ ...field('0'), type: 'utf8', value: '0' }), '0');
  assert.equal(fieldValue({ ...field('0'), type: 'utf8', value: '<img>值</img>' }), '<img>值</img>');
});

test('AVC continuation requires consistent progress and exact direct child fields', () => {
  const d: Detail = { kind: 'avcVideo', complete: false, truncated: true, diagnostics: [], fields: [
    { id: '0', parentId: '', name: 'Parameter context', type: 'bounded replay', value: 'budget_exhausted', span: null },
    ...Object.entries({ nextTagId: '9007199254741000', selectedTagId: '9007199254742000', contextSegmentStartTagId: '9007199254740000', contextCanContinue: 'true' }).map(([name, value], i) => ({ id: String(i + 1), parentId: '0', name, type: 'derived', value, span: null })),
  ] };
  assert.deepEqual(avcReplayProgress(d), { next: '9007199254741000', target: '9007199254742000', canContinue: true });
  d.fields[3].value = d.fields[1].value;
  assert.equal(avcReplayProgress(d)?.canContinue, false, 'no progress cannot start another read');
  d.fields[1].parentId = 'unrelated';
  assert.equal(avcReplayProgress(d), null);
  d.fields[1].parentId = '0'; d.fields[1].value = '01';
  assert.equal(avcReplayProgress(d), null, 'noncanonical coordinates rejected');
});
