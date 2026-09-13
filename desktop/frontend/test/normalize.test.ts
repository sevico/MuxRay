import { test } from 'node:test';
import assert from 'node:assert/strict';
import { detailResponse, diagnosticResponse, hexResponse, pickResponse, sessionResponse, tagResponse } from '../src/normalize.ts';

// JSON.parse models plain IPC objects, not generated class instances. No
// generated constructors are called by Wails' JS method wrappers at runtime.
test('normalizes Hex generated null pointers and rejects malformed or oversized byte windows', () => {
  for (const state of ['pending', 'failed']) {
    const value = hexResponse(JSON.parse(JSON.stringify({ apiVersion: 1, sessionId: 's', requestId: '1', offset: '', count: 0, state })));
    assert.equal(value.window, null); assert.equal(value.error, null);
  }
  const envelope = { apiVersion: 1, sessionId: 's', requestId: '9007199254740993', offset: '9007199254740993', count: 512, state: 'ready', window: { offset: '9007199254740993', sourceSize: '9007199254740995', hex: '00ff', eof: true } };
  const value = hexResponse(JSON.parse(JSON.stringify(envelope)));
  assert.equal(value.window?.offset, envelope.offset); assert.equal(value.window?.hex, '00ff'); assert.equal(value.error, null);
  for (const hex of ['f', 'FF', '00'.repeat(513)]) assert.throws(() => hexResponse(JSON.parse(JSON.stringify({ ...envelope, window: { ...envelope.window, hex } }))));
  assert.throws(() => hexResponse(JSON.parse('{"state":"unknown"}')));
});
test('normalizes generated optional pointers and real JSON nulls', () => {
  for (const fields of ['', ',"session":null,"error":null']) {
    const value = sessionResponse(JSON.parse(`{"apiVersion":1${fields}}`));
    assert.equal(value.session, null); assert.equal(value.error, null);
  }
  const value = pickResponse(JSON.parse('{"apiVersion":1,"requestId":"18446744073709551615","pending":false,"canceled":false,"fileName":"测试.flv","session":{"sessionId":"scan-1","state":"opening","terminal":false,"closed":false,"progress":null,"scan":null,"failure":null},"error":null}'));
  assert.equal(value.requestId, '18446744073709551615'); assert.equal(value.pending, false);
  assert.equal(value.session?.progress, null); assert.equal(value.session?.scan, null);
  assert.equal(value.session?.failure, null); assert.equal(value.fileName, '测试.flv');
});

test('normalizes detail nullable spans and diagnostics without losing large positions or preview text', () => {
  for (const value of [{ apiVersion: 1, state: 'pending' }, { apiVersion: 1, state: 'pending', detail: null, error: null }]) {
    const normalized = detailResponse(JSON.parse(JSON.stringify(value)));
    assert.equal(normalized.detail, null); assert.equal(normalized.error, null);
  }
  const value = detailResponse(JSON.parse('{"apiVersion":1,"sessionId":"scan-1","requestId":"18446744073709551615","tagId":"9007199254740993","state":"ready","detail":{"kind":"amf0ScriptData","complete":true,"truncated":true,"fields":[{"id":"0","parentId":"","name":"metadata","type":"string","value":"<script>值</script>","span":{"byteOffset":"18446744073709551615","bitOffset":3,"bitLength":5}},{"id":"1","parentId":"0","name":"derived","type":"number","value":"3"}],"diagnostics":[{"code":"TEST","severity":"warning","offset":"9007199254740993","bitOffset":0,"message":"preview"}]},"error":null}'));
  assert.equal(value.detail?.fields[0].span?.byteOffset, '18446744073709551615');
  assert.equal(value.detail?.fields[0].value, '<script>值</script>'); assert.equal(value.detail?.fields[1].span, null);
  assert.equal(value.detail?.diagnostics[0].expected, null); assert.equal(value.detail?.diagnostics[0].actual, null);
  assert.equal(value.detail?.complete, true); assert.equal(value.detail?.truncated, true);
  assert.throws(() => detailResponse(JSON.parse('{"state":"unknown"}')), /Invalid detail response state/);
});
test('normalizes nested page pointers without converting exact integers', () => {
  const tags = tagResponse(JSON.parse('{"apiVersion":1,"error":null,"page":{"sessionId":"scan-1","bounds":{"start":"0","limit":100,"total":"1","next":"1"},"tags":[{"id":"9007199254740993","offset":"18446744073709551615","dts":"-9223372036854775808","previousTagSize":{"present":false,"span":null,"value":0,"expected":11,"valid":false}}]}}'));
  assert.equal(tags.page?.tags[0].previousTagSize.span, null);
  assert.equal(tags.page?.tags[0].offset, '18446744073709551615');
  assert.equal(tags.page?.tags[0].dts, '-9223372036854775808');
  const diagnostics = diagnosticResponse(JSON.parse('{"apiVersion":1,"page":{"sessionId":"scan-1","bounds":{"start":"0","limit":100,"total":"1","next":"1"},"diagnostics":[{"code":"test","severity":"error","offset":"0","bitOffset":0,"message":"test","expected":"18446744073709551615","actual":null}]},"error":null}'));
  assert.equal(diagnostics.page?.diagnostics[0].actual, null);
  assert.equal(diagnostics.page?.diagnostics[0].expected, '18446744073709551615');
  assert.equal(tagResponse(JSON.parse('{"apiVersion":1,"page":null,"error":null}')).page, null);
  assert.equal(diagnosticResponse(JSON.parse('{"apiVersion":1,"page":null,"error":null}')).page, null);
});

test('recognizes every supported detail kind and rejects unexpected kinds', () => {
  const envelope = (kind: string) => JSON.parse(JSON.stringify({ apiVersion: 1, sessionId: 'scan-1', requestId: '1', tagId: '2', state: 'ready', error: null, detail: {
    kind, complete: true, truncated: false, diagnostics: [], fields: [
      { id: '0', parentId: '', name: 'nalUnitType', type: 'uint5', value: '5', span: { byteOffset: '18446744073709551615', bitOffset: 3, bitLength: 5 } },
      { id: '1', parentId: '', name: 'pts', type: 'int64', value: '-9223372036854775808', span: null },
    ],
  } }));
  for (const kind of ['amf0ScriptData', 'aacAudio', 'avcVideo', 'mp3Audio']) {
    const result = detailResponse(envelope(kind));
    assert.equal(result.detail?.kind, kind);
    assert.equal(result.detail?.fields[0].span?.byteOffset, '18446744073709551615');
    assert.equal(result.detail?.fields[1].value, '-9223372036854775808');
    assert.equal(result.detail?.fields[1].span, null, 'derived values must not acquire a fabricated source position');
  }
  for (const kind of ['avc', 'mp3', '', 'futureCodec']) assert.throws(() => detailResponse(envelope(kind)), /Invalid detail response kind/);
});

test('MP3 free-format preserves complete header syntax and does not invent unknown bitrate or length', () => {
  const value = detailResponse(JSON.parse(JSON.stringify({ apiVersion: 1, sessionId: 'scan-1', requestId: '4', tagId: '9007199254740993', state: 'ready', error: null, detail: {
    kind: 'mp3Audio', complete: true, truncated: false,
    fields: [
      { id: '0', parentId: '', name: 'bitrateIndex', type: 'uint4', value: '0 (free-format)', span: { byteOffset: '9007199254740995', bitOffset: 0, bitLength: 4 } },
      { id: '1', parentId: '', name: 'bitrateMode', type: 'derived', value: 'free', span: null },
      { id: '2', parentId: '', name: 'samplingFrequencyHz', type: 'derived', value: '44100' },
    ],
    diagnostics: [{ code: 'MP3_FREE_FORMAT_UNRESOLVED', severity: 'warning', offset: '9007199254740995', bitOffset: 0, message: 'First fixed header only; bitrate and frame length remain unknown.', expected: null }],
  } })));
  assert.equal(value.detail?.kind, 'mp3Audio');
  assert.equal(value.detail?.complete, true, 'a warning must not turn complete free-format header syntax into a failed parse');
  assert.equal(value.detail?.truncated, false);
  assert.equal(value.detail?.fields[0].span?.byteOffset, '9007199254740995');
  assert.equal(value.detail?.fields[1].value, 'free');
  assert.equal(value.detail?.fields[2].value, '44100');
  assert.equal(value.detail?.fields.some(field => field.name === 'bitrateKbps' || field.name === 'frameLengthBytes'), false);
  assert.equal(value.detail?.fields[1].span, null);
  assert.equal(value.detail?.fields[2].span, null);
  assert.equal(value.detail?.diagnostics[0].severity, 'warning');
  assert.equal(value.detail?.diagnostics[0].offset, '9007199254740995');
  assert.equal(value.detail?.diagnostics[0].expected, null);
  assert.equal(value.detail?.diagnostics[0].actual, null);
});
