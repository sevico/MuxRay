import { test } from 'node:test';
import assert from 'node:assert/strict';
import { detailRows, fieldValue } from '../src/details.ts';
import type { DetailField } from '../src/protocol.ts';

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
