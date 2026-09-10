import { test } from 'node:test';
import assert from 'node:assert/strict';
import { detailResponse, diagnosticResponse, pickResponse, sessionResponse, tagResponse } from '../src/normalize.ts';

// JSON.parse models plain IPC objects, not generated class instances. No
// generated constructors are called by Wails' JS method wrappers at runtime.
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
