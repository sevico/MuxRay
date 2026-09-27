import { test } from 'node:test';
import assert from 'node:assert/strict';
import type { TagTrendPoint } from '../src/protocol.ts';
import { TREND_PLOT, trendContext, trendDomain, trendGap, trendHitWidth, trendIssues, trendType, trendValue, trendX, trendY } from '../src/tag-trends.ts';

function point(values: Partial<TagTrendPoint> = {}): TagTrendPoint {
  return { tagId: '1', offset: '28', dts: '100', typeCode: 8, streamId: 0, dataSize: 4, availableDataSize: 4,
    payloadComplete: true, previousTagSizeState: 'valid', filtered: false, previousTagId: '0', previousDts: '50', deltaMs: '50', context: 'found', ...values };
}
test('trend observation threshold is exact, positive, canonical and strictly greater', () => {
  assert.equal(trendGap('4294967295'), 4294967295n);
  for (const value of ['0', '-1', '01', '1.5', '1e3', ' 1000', '4294967296', '']) assert.throws(() => trendGap(value));
  assert.deepEqual(trendIssues(point({ deltaMs: '1000' }), 1000n), []);
  assert.deepEqual(trendIssues(point({ deltaMs: '1001' }), 1000n), ['gap']);
  assert.deepEqual(trendIssues(point({ deltaMs: '0' }), 1000n), ['duplicate']);
  assert.deepEqual(trendIssues(point({ deltaMs: '-4294967295' }), 1000n), ['regression']);
});
test('unknown predecessor yields no invented timing observation while structure is preserved', () => {
  const missing = point({ deltaMs: null, previousTagId: null, previousDts: null, context: 'budget_exhausted', payloadComplete: false, availableDataSize: 2, previousTagSizeState: 'missing' });
  assert.deepEqual(trendIssues(missing, 1000n), ['payload', 'previous_missing']);
  assert.match(trendContext(missing), /4096/);
  assert.equal(trendValue(missing, 'delta'), null);
  assert.deepEqual(trendIssues(point({ previousTagSizeState: 'invalid' }), 1000n), ['previous_invalid']);
  assert.match(trendContext(point({ context: 'start', deltaMs: null })), /索引起点/);
});
test('trend charts retain huge Tag identities and normalize only relative geometry', () => {
  const start = '9007199254740993', next = '9007199254740996';
  assert.equal(trendX(start, start, next), TREND_PLOT.left);
  assert.equal(trendX('9007199254740995', start, next), TREND_PLOT.right);
  assert.equal(trendX('9007199254740994', start, next), (TREND_PLOT.left + TREND_PLOT.right) / 2);
  assert.equal(trendX(start, start, '9007199254740994'), (TREND_PLOT.left + TREND_PLOT.right) / 2);
  assert.throws(() => trendX('9007199254740992', start, next));
});
test('DTS difference plots include negative regression and leave unknown values absent', () => {
  const points = [point({ deltaMs: '-4294967295' }), point({ deltaMs: '4294967295' }), point({ deltaMs: null })];
  const domain = trendDomain(points, 'delta');
  assert.deepEqual(domain, { min: -4294967295n, max: 4294967295n });
  assert.equal(trendY(domain.min, domain), TREND_PLOT.bottom);
  assert.equal(trendY(domain.max, domain), TREND_PLOT.top);
  assert.equal(trendY(0n, domain), (TREND_PLOT.top + TREND_PLOT.bottom) / 2);
  assert.deepEqual(trendDomain([point({ deltaMs: null })], 'delta'), { min: 0n, max: 1n });
});
test('dense plot hit targets never overlap adjacent source columns', () => {
  const start = '9007199254740993';
  for (const count of [2, 44, 256, 512]) {
    const next = (BigInt(start) + BigInt(count)).toString();
    const gap = trendX((BigInt(start) + 1n).toString(), start, next) - trendX(start, start, next);
    assert.ok(trendHitWidth(start, next) < gap);
  }
  assert.equal(trendHitWidth(start, (BigInt(start) + 1n).toString()), 18);
  assert.throws(() => trendHitWidth('0', '513'));
});
test('payload chart keeps missing declared bytes separate from actual bytes', () => {
  const truncated = point({ dataSize: 10, availableDataSize: 2, payloadComplete: false });
  assert.equal(trendValue(truncated, 'size'), 2n);
  assert.deepEqual(trendDomain([truncated], 'size'), { min: 0n, max: 10n });
  assert.deepEqual(trendDomain([point({ dataSize: 0, availableDataSize: 0 })], 'size'), { min: 0n, max: 1n });
  assert.equal(trendType(point({ typeCode: 31 })), 'other');
  assert.equal(trendType(point({ typeCode: 9 })), 'video');
  assert.equal(trendType(point({ typeCode: 18 })), 'script');
});
