import { test } from 'node:test';
import assert from 'node:assert/strict';
import { diagnosticResponse, pickResponse, sessionResponse, tagResponse } from '../src/normalize.ts';

// JSON.parse models plain IPC objects, not generated class instances. No
// generated constructors are called by Wails' JS method wrappers at runtime.
test('normalizes generated optional pointers and real JSON nulls', () => {
  for (const fields of ['', ',"session":null,"error":null']) {
    const value = sessionResponse(JSON.parse(`{"apiVersion":1${fields}}`));
    assert.equal(value.session, null); assert.equal(value.error, null);
  }
  const value = pickResponse(JSON.parse('{"apiVersion":1,"canceled":false,"fileName":"测试.flv","session":{"sessionId":"scan-1","state":"opening","terminal":false,"closed":false,"progress":null,"scan":null,"failure":null},"error":null}'));
  assert.equal(value.session?.progress, null); assert.equal(value.session?.scan, null);
  assert.equal(value.session?.failure, null); assert.equal(value.fileName, '测试.flv');
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
