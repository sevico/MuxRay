import { test } from 'node:test';
import assert from 'node:assert/strict';
import { hexRows, selectionFromSpan, validateHexWindow, validateSelection } from '../src/hex.ts';
import type { HexSelection } from '../src/hex.ts';
import type { HexWindow } from '../src/protocol.ts';

const selection = (offset: string, bitOffset: number, bitLength: number | null): HexSelection => ({ offset, bitOffset, bitLength, label: '<script>字段</script>' });
test('Hex displays actual bytes and MSB-first cross-byte masks at exact large offsets', () => {
  const window = { offset: '9007199254740993', sourceSize: '9007199254740996', hex: '80ff3c', eof: true };
  validateHexWindow(window);
  const rows = hexRows(window, selection(window.offset, 6, 5));
  assert.deepEqual(rows.map(row => row.offset), ['9007199254740993']);
  assert.deepEqual(rows[0].bytes.map(byte => [byte.offset, byte.mask, byte.ascii, byte.bits]), [
    ['9007199254740993', 3, '.', '10000000'], ['9007199254740994', 224, '.', '11111111'], ['9007199254740995', 0, '<', '00111100'],
  ]);
});
test('Hex window clipping preserves the logical selection and never pads bytes', () => {
  const selected = selection('15', 4, 8), snapshot = structuredClone(selected);
  const first = hexRows({ offset: '0', sourceSize: '17', hex: '00'.repeat(16), eof: false }, selected);
  const next = hexRows({ offset: '16', sourceSize: '17', hex: 'fa', eof: true }, selected);
  assert.equal(first[0].bytes[15].mask, 15); assert.equal(next[0].bytes[0].mask, 240);
  assert.equal(next[0].bytes.length, 1); assert.deepEqual(selected, snapshot);
  assert.deepEqual(hexRows({ offset: '17', sourceSize: '17', hex: '', eof: true }, selection('17', 0, null)), []);
});
test('diagnostic and empty-span positions never claim an eight-bit range', () => {
  const window = { offset: '20', sourceSize: '22', hex: '001f', eof: true };
  for (const length of [null, 0]) {
    const bytes = hexRows(window, selection('21', 3, length))[0].bytes;
    assert.deepEqual(bytes.map(byte => byte.mask), [0, 0]); assert.deepEqual(bytes.map(byte => byte.point), [false, true]);
  }
  assert.deepEqual(selectionFromSpan({ byteOffset: '21', bitOffset: 3, bitLength: 0 }, 'empty'), { offset: '21', bitOffset: 3, bitLength: 0, label: 'empty' });
});
test('Hex validates transport size, lowercase encoding, EOF, actual extent and count', () => {
  const valid: HexWindow = { offset: '2', sourceSize: '4', hex: '00ff', eof: true };
  validateHexWindow(valid, 512);
  for (const patch of [{ hex: '00FF' }, { hex: '0' }, { hex: 'gg' }, { hex: '00'.repeat(513) }, { eof: false }, { offset: '5' }, { offset: '02' }, { sourceSize: '3' }]) assert.throws(() => validateHexWindow({ ...valid, ...patch }, 512));
  for (const count of [0, -1, 1.5, 4097]) assert.throws(() => validateHexWindow(valid, count));
  validateHexWindow({ offset: '18446744073709551615', sourceSize: '18446744073709551615', hex: '', eof: true });
  assert.throws(() => validateHexWindow({ offset: '18446744073709551615', sourceSize: '18446744073709551615', hex: '00', eof: true }));
});
test('selection ranges follow cursor End and ByteRange uint64 limits', () => {
  const max = '18446744073709551615';
  for (const item of [selection('01', 0, 1), selection('0', 8, 1), selection('0', 0, -1), selection('0', 0, 4294967296), selection(max, 7, 1), selection(max, 0, 1)]) assert.throws(() => validateSelection(item));
  validateSelection(selection(max, 7, 0)); validateSelection(selection(max, 7, null));
  validateSelection(selection('18446744073709551614', 7, 1));
});
