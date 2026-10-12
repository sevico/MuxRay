import { test } from 'node:test';
import assert from 'node:assert/strict';
import { keyframeEvidence, mp3FrameSelection, mp3Dependency } from '../src/codec-details.ts';
import { dependencyDetail } from './mp3-dependency-fixture.ts';
import type { Detail } from '../src/protocol.ts';

function fixture(kind: Detail['kind'], name: string, type: string, values: Record<string, string>): Detail {
  return { kind, complete: true, truncated: false, diagnostics: [], fields: [
    { id: 'root', parentId: '', name, type, value: '', span: null },
    ...Object.entries(values).map(([name, value], i) => ({ id: String(i), parentId: 'root', name, type: 'derived', value, span: null })),
  ] };
}
const mp3 = () => fixture('mp3Audio', 'Frame inspection', 'bounded sequence', { selectedFrameIndex: 'preview', selectableFrameIndices: '0,1,2', checkedFrameCount: '4' });

test('MP3 selection uses explicit confirmed boundaries, including an unselectable partial suffix', () => {
  const detail = mp3();
  assert.deepEqual(mp3FrameSelection(detail), { selected: null, selectable: [0, 1, 2], checked: 4 });
  detail.fields[1].value = '2';
  assert.equal(mp3FrameSelection(detail)?.selected, 2);
  detail.fields[1].value = '3'; assert.equal(mp3FrameSelection(detail), null);
  detail.fields[1].value = 'preview'; detail.fields[2].value = '';
  assert.deepEqual(mp3FrameSelection(detail), { selected: null, selectable: [], checked: 4 });
});
test('MP3 selector rejects duplicated, noncanonical, out-of-budget and unrelated metadata', () => {
  for (const value of ['0,0', '1,0', '01', '64', '-1', '1.1', '0,']) {
    const detail = mp3(); detail.fields[2].value = value;
    assert.equal(mp3FrameSelection(detail), null, value);
  }
  const detail = mp3(); detail.fields[3].value = '65'; assert.equal(mp3FrameSelection(detail), null);
  detail.fields[3].value = '4'; detail.fields[2].parentId = 'unrelated'; assert.equal(mp3FrameSelection(detail), null);
  detail.fields[2].parentId = 'root'; detail.fields.push({ ...detail.fields[2], id: 'duplicate' }); assert.equal(mp3FrameSelection(detail), null);
});

const avc = () => fixture('avcVideo', 'Keyframe evidence', 'bounded NAL evidence', {
  declaredFrameType: '2', observedIDR: 'yes', declarationRelation: 'inter_with_idr',
  nalHeaderCoverageComplete: 'false', sliceTypeCoverageComplete: 'false', observedSliceTypes: 'B,I', inspectedNALHeaders: '2', retainedNALUnits: '3',
});
test('H264 preserves a positive IDR observation with partial coverage and multiple slice types', () => {
  const detail = avc();
  detail.fields.push({ id: 'source', parentId: 'root', name: 'evidenceIDRNALType', type: 'uint5', value: '5', span: { byteOffset: '9007199254740993', bitOffset: 3, bitLength: 5 } });
  const result = keyframeEvidence(detail)!;
  assert.equal(result.idr, 'yes'); assert.equal(result.nalComplete, false);
  assert.deepEqual(result.sliceTypes, ['B', 'I']); assert.equal(result.sources[0].span?.byteOffset, '9007199254740993');
  detail.fields.find(field => field.name === 'observedIDR')!.value = 'no';
  assert.equal(keyframeEvidence(detail), null, 'partial coverage cannot prove absence');
});
test('H264 rejects duplicate/invalid type sets and unsupported relation values', () => {
  for (const value of ['I,I', 'IDR', 'P, B']) {
    const detail = avc(); detail.fields.find(field => field.name === 'observedSliceTypes')!.value = value;
    assert.equal(keyframeEvidence(detail), null);
  }
  const detail = avc(); detail.fields.find(field => field.name === 'declarationRelation')!.value = 'decodable';
  assert.equal(keyframeEvidence(detail), null);
});

test('MP3 sources preserve physical ordering, precise spans and target identity', () => {
  const result = mp3Dependency(dependencyDetail())!;
  assert.equal(result.tagId, '1'); assert.equal(result.frameIndex, 0); assert.equal(result.requested, '20');
  assert.deepEqual(result.sources.map(source => [source.tagId, source.current, source.span.byteOffset, source.span.bitLength]), [['0', false, '25', 160], ['1', true, '60', 80]]);
});
test('MP3 source projection rejects inconsistent byte counts, targets, overlapping spans and duplicated metadata', () => {
  const changes: ((d: Detail) => void)[] = [
    d => d.fields.find(f => f.name === 'resolvedPreviousBytes')!.value = '19',
    d => d.fields.find(f => f.id === 'span-1')!.span!.byteOffset = '30',
    d => d.fields.find(f => f.id === 's-1-0')!.value = '2',
    d => d.fields.find(f => f.id === 's-0-2')!.value = 'true',
    d => d.fields.push({ ...d.fields.find(f => f.name === 'sourceCount')!, id: 'duplicate' }),
    d => d.fields.find(f => f.name === 'bytesRead')!.value = '1048577',
  ];
  for (const change of changes) { const d = dependencyDetail(); change(d); assert.equal(mp3Dependency(d), null); }
});
