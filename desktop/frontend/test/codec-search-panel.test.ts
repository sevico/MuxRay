import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { Children, isValidElement } from 'react';
import type { ReactElement, ReactNode } from 'react';
import * as jsxRuntime from 'react/jsx-runtime';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import * as helpers from '../src/codec-search.ts';
import type { CodecSearchPanel as Panel } from '../src/CodecSearchPanel.tsx';
import { ready, row, setup, settle } from './codec-search-fixture.ts';

const compiled = ts.transpileModule(readFileSync(new URL('../src/CodecSearchPanel.tsx', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText;
const componentExports = {} as { CodecSearchPanel: typeof Panel };
new Function('require', 'exports', compiled)((name: string) => {
  if (name === 'react/jsx-runtime') return jsxRuntime;
  if (name === './codec-search') return helpers;
  throw new Error(`Unexpected component import ${name}`);
}, componentExports);
const { CodecSearchPanel } = componentExports;
function elements(node: ReactNode): ReactElement<Record<string, unknown>>[] {
  return Children.toArray(node).flatMap(child => isValidElement<Record<string, unknown>>(child)
    ? [child, ...elements(child.props.children as ReactNode)] : []);
}
function accessible(node: ReactNode, name: string) {
  const result = elements(node).filter(element => element.props['aria-label'] === name || (element.type === 'button' && element.props.children === name));
  assert.equal(result.length, 1, name); return result[0];
}

test('codec panel exposes stable filters and explicit restart/next actions without an invented previous page', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.startCodecSearch(); await settle();
  const starts: unknown[] = []; let continued = 0;
  controller.startCodecSearch = (...args) => { starts.push(args); }; controller.continueCodecSearch = () => { continued++; };
  const node = CodecSearchPanel({ view: controller.getSnapshot(), controller, disabled: false, message: x => x });
  const markup = renderToStaticMarkup(node);
  assert.match(markup, /aria-label="编解码器批量检索"/); assert.match(markup, /扫描索引不完整/);
  assert.doesNotMatch(markup, /上一批|全部 MP3 帧/);
  (accessible(node, '编解码器检索类型').props.onChange as (e: unknown) => void)({ target: { value: 'sei' } });
  (accessible(node, '编解码器检索筛选').props.onChange as (e: unknown) => void)({ target: { value: 'stops' } });
  (accessible(node, '从头检索编解码器').props.onClick as () => void)();
  (accessible(node, '继续检索编解码器').props.onClick as () => void)();
  assert.deepEqual(starts, [['sei'], ['mp3', 'stops'], []]); assert.equal(continued, 1); controller.dispose();
});

test('codec panel keeps full frame actions distinct from stops and maps each source to its own row', async () => {
  const { controller } = setup({ startCodecSearch: async q => { const r = ready(q); r.page!.rows.push(row('mp3', { frameIndex: 71, kind: 'stop', complete: false, status: 'freeFormat', span: null })); return r; } });
  await controller.open(); await settle(); controller.startCodecSearch(); await settle();
  const calls: unknown[] = []; controller.locateCodecSearchRow = (...args) => { calls.push(args); };
  const page = controller.getSnapshot().codecSearchPage!, node = CodecSearchPanel({ view: controller.getSnapshot(), controller, disabled: false, message: x => x });
  assert.match(renderToStaticMarkup(node), /停止于帧 71/); assert.match(renderToStaticMarkup(node), /自由格式长度未知/);
  (accessible(node, '从编解码器检索跳转 0:70:-:- 帧').props.onClick as () => void)();
  (accessible(node, '从编解码器检索跳转 0:71:-:- Tag').props.onClick as () => void)();
  assert.equal(elements(node).some(e => e.props['aria-label'] === '从编解码器检索跳转 0:71:-:- 帧'), false);
  assert.deepEqual(calls, [[page.rows[0], 'frame'], [page.rows[1], 'tag']]); controller.dispose();
});

test('SEI source links and no-read continuation are visible; shared reader ownership disables all new actions', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.startCodecSearch('sei'); await settle();
  const view = controller.getSnapshot(), calls: unknown[] = []; controller.locateCodecSearchRow = (...args) => { calls.push(args); };
  const node = CodecSearchPanel({ view, controller, disabled: false, message: x => x });
  for (const [label, action] of [['证据', 'hex'], ['SPS', 'sps'], ['选择依据', 'evidence']]) {
    (accessible(node, `定位编解码器 0:-:0:0 ${label}`).props.onClick as () => void)();
    assert.equal((calls.at(-1) as unknown[])[1], action);
  }
  const draining = CodecSearchPanel({ view: { ...view, codecSearchPage: { ...view.codecSearchPage!, scannedTags: 0, bytesRead: '0', pending: 193 } }, controller, disabled: false, message: x => x });
  assert.match(renderToStaticMarkup(draining), /检查 0 个 Tag/); assert.match(renderToStaticMarkup(draining), /待显示 193 条/); assert.match(renderToStaticMarkup(draining), /读取 0 byte/);
  for (const owner of ['summaryRunning', 'avcSearchRunning'] as const) {
    const blocked = CodecSearchPanel({ view: { ...view, [owner]: true }, controller, disabled: false, message: x => x });
    for (const e of elements(blocked).filter(e => ['select', 'button'].includes(String(e.type)))) assert.equal(e.props.disabled, true);
  }
  controller.dispose();
});
