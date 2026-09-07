import { test } from 'node:test';
import assert from 'node:assert/strict';
import { byteSize, previousStart, progressPercent, unsigned } from '../src/numbers.ts';

test('canonical uint64 input, including beyond JavaScript safe integer', () => {
  assert.equal(unsigned('9007199254740993'), 9007199254740993n);
  assert.equal(unsigned('18446744073709551615'), 18446744073709551615n);
  for (const value of ['', '01', '+1', '-1', ' 1', '1.0', '1e3', '١', '18446744073709551616', '1'.repeat(100)]) {
    assert.throws(() => unsigned(value), value);
  }
});
test('page arithmetic never rounds offsets', () => {
  assert.equal(previousStart('9007199254740993', 100), '9007199254740893');
  assert.equal(previousStart('0', 100), '0');
  assert.equal(previousStart('99', 100), '0');
});
test('bounded percentage and human sizes do not coerce raw offsets to number', () => {
  assert.equal(progressPercent('9223372036854775807', '18446744073709551615'), 49.99);
  assert.equal(progressPercent('18446744073709551615', '18446744073709551615'), 100);
  assert.equal(progressPercent('100', '99'), 100);
  assert.equal(progressPercent('0', '0'), 0);
  assert.equal(byteSize('10737418240'), '10.0 GiB');
  assert.equal(byteSize('18446744073709551615'), '15.9 EiB');
  assert.equal(byteSize('13'), '13 B');
});
