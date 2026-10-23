import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import ts from 'typescript';
import type { desktop } from '../wailsjs/go/models';

// Exercise the actual generated constructor: a structurally compatible spread
// may type-check even when stale Wails output silently drops a new selection.
const compiled = ts.transpileModule(readFileSync(new URL('../wailsjs/go/models.ts', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText;
const generated = {} as { desktop: { SnapshotRequest: typeof desktop.SnapshotRequest } };
new Function('exports', compiled)(generated);

test('generated Wails snapshot constructor retains codec v4 selection alongside older optional sections', () => {
  const selection = { sessionId: 'session', schemaVersion: 4, selectedTagId: '9007199254740993',
    codecSearch: { requestId: '18446744073709551615', cursor: '9007199254740993', kind: 'sei', filter: 'pictureTiming' },
    avcSearch: { requestId: '10', start: '256', filter: 'unknown' }, mediaCurve: { start: '64', count: 64, metric: 'gopMax' } };
  const roundTrip = JSON.parse(JSON.stringify(new generated.desktop.SnapshotRequest(selection)));
  assert.deepEqual(roundTrip.codecSearch, selection.codecSearch); assert.deepEqual(roundTrip.avcSearch, selection.avcSearch);
  assert.deepEqual(roundTrip.mediaCurve, selection.mediaCurve); assert.equal(roundTrip.schemaVersion, 4);
  const old = JSON.parse(JSON.stringify(new generated.desktop.SnapshotRequest({ sessionId: 'session', schemaVersion: 3 })));
  assert.equal(old.codecSearch, undefined); assert.equal(old.schemaVersion, 3);
});


test('generated Wails constructor preserves exact snapshot v5 timestamp selection with codec evidence', () => {
  const selection = { sessionId: 'session', schemaVersion: 5,
    timeSearch: { requestId: '18446744073709551615', start: '9007199254740993', filter: 'regressions', gapThreshold: '4294967295' },
    codecSearch: { requestId: '10', cursor: '9', kind: 'mp3', filter: 'all' } };
  const roundTrip = JSON.parse(JSON.stringify(new generated.desktop.SnapshotRequest(selection)));
  assert.deepEqual(roundTrip.timeSearch, selection.timeSearch); assert.deepEqual(roundTrip.codecSearch, selection.codecSearch);
  assert.equal(roundTrip.schemaVersion, 5);
  for (const version of [1, 2, 3, 4]) {
    const old = JSON.parse(JSON.stringify(new generated.desktop.SnapshotRequest({ sessionId: 'session', schemaVersion: version })));
    assert.equal(old.timeSearch, undefined); assert.equal(old.schemaVersion, version);
  }
});
