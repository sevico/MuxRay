import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { Children, isValidElement } from 'react';
import type { ReactElement, ReactNode } from 'react';
import * as jsxRuntime from 'react/jsx-runtime';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import * as helpers from '../src/time-search.ts';
import * as trends from '../src/tag-trends.ts';
import type { TimeSearchPanel as Panel } from '../src/TimeSearchPanel.tsx';
import { ready, setup, settle } from './time-search-fixture.ts';

const compiled = ts.transpileModule(readFileSync(new URL('../src/TimeSearchPanel.tsx', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText;
const componentExports = {} as { TimeSearchPanel: typeof Panel };
new Function('require', 'exports', compiled)((name: string) => {
  if (name === 'react/jsx-runtime') return jsxRuntime;
  if (name === './time-search') return helpers;
  if (name === './tag-trends') return trends;
  throw new Error(`Unexpected component import ${name}`);
}, componentExports);
const { TimeSearchPanel } = componentExports;
function elements(node: ReactNode): ReactElement<Record<string, unknown>>[] {
  return Children.toArray(node).flatMap(child => isValidElement<Record<string, unknown>>(child) ? [child, ...elements(child.props.children as ReactNode)] : []);
}
function accessible(node: ReactNode, name: string) {
  const result = elements(node).filter(element => element.props['aria-label'] === name || (element.type === 'button' && element.props.children === name));
  assert.equal(result.length, 1, name); return result[0];
}

test('timestamp panel shows bounded partial coverage, explicit continuation and snapshot omission', async () => {
  const { controller } = setup(); await controller.open(); await settle(); controller.startTimeSearch(); await settle();
  const options: unknown[] = []; let starts = 0, continued = 0;
  controller.setTimeSearchOptions = q => options.push(q); controller.startTimeSearch = () => { starts++; }; controller.continueTimeSearch = () => { continued++; };
  const node = TimeSearchPanel({ view: controller.getSnapshot(), controller, disabled: false, message: x => x });
  const markup = renderToStaticMarkup(node);
  assert.match(markup, /连续已检范围 \[0, 4096\)/); assert.match(markup, /尚未到索引末尾/); assert.match(markup, /仅本批范围未命中/);
  assert.match(markup, /部分索引，本次检索不覆盖全文件/); assert.match(markup, /不包含此检索页/); assert.match(markup, /时间观察不等于媒体损坏/);
  (accessible(node, '时间戳检索筛选').props.onChange as (e: unknown) => void)({ target: { value: 'gaps' } });
  (accessible(node, '时间戳检索大间隔阈值').props.onChange as (e: unknown) => void)({ target: { value: '9000' } });
  (accessible(node, '从头检索时间戳').props.onClick as () => void)(); (accessible(node, '继续下一批时间戳').props.onClick as () => void)();
  assert.deepEqual(options, [{ timeSearchFilter: 'gaps' }, { timeSearchGapThreshold: '9000' }]); assert.equal(starts, 1); assert.equal(continued, 1); controller.dispose();
});

test('timestamp rows expose exact predecessor evidence and one action to load trend and detail', async () => {
  const { controller } = setup(); await controller.open(); await settle(); for (let i = 0; i < 3; i++) { if (!i) controller.startTimeSearch(); else controller.continueTimeSearch(); await settle(); }
  const selected: unknown[] = []; controller.locateTimeSearchRow = row => selected.push(row);
  const view = controller.getSnapshot(), node = TimeSearchPanel({ view, controller, disabled: false, message: x => x }), markup = renderToStaticMarkup(node);
  assert.match(markup, /Tag 8191 · 163820 ms/); assert.match(markup, /163720 \/ -100/); assert.match(markup, /已到当前索引末尾/);
  (accessible(node, '从时间戳检索定位 Tag 8192').props.onClick as () => void)(); assert.equal(selected[0], view.timeSearchPage!.rows[0]);
  assert.equal(accessible(node, '继续下一批时间戳').props.disabled, true); controller.dispose();
});

test('unknown context remains visible under regression filter and other workers disable timestamp actions', async () => {
  const { controller } = setup({ startTimeSearch: async q => { const r = ready(q, '0', '4096'); r.page!.unknownPredecessors = 1;
    r.page!.rows = [{ kind: 'unknown', point: { tagId: '513', offset: '10273', dts: '0', typeCode: 9, streamId: 513, dataSize: 0, availableDataSize: 0, payloadComplete: true, filtered: false, previousTagSizeState: 'valid', previousTagId: null, previousDts: null, deltaMs: null, context: 'budget_exhausted' } }]; return r; } });
  await controller.open(); await settle(); controller.startTimeSearch(); await settle(); const view = controller.getSnapshot();
  const markup = renderToStaticMarkup(TimeSearchPanel({ view, controller, disabled: false, message: x => x }));
  assert.match(markup, /前驱未知/); assert.match(markup, /未知项不受筛选隐藏/); assert.match(markup, /上下文上限，无法比较/);
  for (const worker of ['summaryRunning', 'avcSearchRunning', 'codecSearchRunning'] as const) {
    const blocked = TimeSearchPanel({ view: { ...view, [worker]: true }, controller, disabled: false, message: x => x });
    for (const e of elements(blocked).filter(e => ['select', 'input', 'button'].includes(String(e.type)))) assert.equal(e.props.disabled, true);
  }
  controller.dispose();
});
