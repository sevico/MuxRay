import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { Children, isValidElement } from 'react';
import type { ComponentProps, ReactElement, ReactNode } from 'react';
import * as jsxRuntime from 'react/jsx-runtime';
import { renderToStaticMarkup } from 'react-dom/server';
import ts from 'typescript';
import * as codec from '../src/codec-details.ts';
import * as hex from '../src/hex.ts';
import type { MP3FramePanel as FramePanel } from '../src/CodecDetailPanel.tsx';
import { mp3PageDetail } from './mp3-page-fixture.ts';

// Node's type stripping does not compile TSX. Compile only this component and
// resolve its imports to the actual tested helpers, without a browser or DOM.
const compiled = ts.transpileModule(readFileSync(new URL('../src/CodecDetailPanel.tsx', import.meta.url), 'utf8'), {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022, jsx: ts.JsxEmit.ReactJSX },
}).outputText;
const componentExports = {} as { MP3FramePanel: typeof FramePanel };
new Function('require', 'exports', compiled)((name: string) => {
  if (name === 'react/jsx-runtime') return jsxRuntime;
  if (name === './codec-details') return codec;
  if (name === './hex') return hex;
  throw new Error(`Unexpected component import ${name}`);
}, componentExports);
const { MP3FramePanel } = componentExports;
type PanelProps = ComponentProps<typeof FramePanel>;
function panel(patch: Partial<PanelProps> = {}) {
  return MP3FramePanel({ detail: mp3PageDetail(0, 63), disabled: false, jumpError: null,
    onSelect() {}, onPage() {}, onJump() {}, onStep() {}, ...patch });
}
function elements(node: ReactNode): ReactElement<Record<string, unknown>>[] {
  return Children.toArray(node).flatMap(child => isValidElement<Record<string, unknown>>(child)
    ? [child, ...elements(child.props.children as ReactNode)] : []);
}
function control(node: ReactNode, text: string) {
  const result = elements(node).filter(element => element.type === 'button' && element.props.children === text);
  assert.equal(result.length, 1, text); return result[0];
}

test('MP3 panel submits literal input, exposes validation and preserves its exact accessible controls', () => {
  const jumps: string[] = [], steps: number[] = [];
  const node = panel({ onJump: text => jumps.push(text), onStep: direction => steps.push(direction), jumpError: '请输入有效帧号。' });
  const markup = renderToStaticMarkup(node);
  assert.match(markup, /aria-label="MP3 帧号"/); assert.match(markup, /aria-invalid="true"/);
  assert.match(markup, /role="alert">请输入有效帧号。/);
  const form = elements(node).find(element => element.type === 'form')!;
  let prevented = false;
  (form.props.onSubmit as (event: unknown) => void)({ preventDefault() { prevented = true; }, currentTarget: { elements: { namedItem(name: string) {
    assert.equal(name, 'mp3-frame-index'); return { value: '01' };
  } } } });
  assert.equal(prevented, true); assert.deepEqual(jumps, ['01'], 'validation receives the original input');
  assert.equal(control(node, '下一 MP3 帧').props.disabled, false);
  (control(node, '下一 MP3 帧').props.onClick as () => void)(); assert.deepEqual(steps, [1]);
  const previous = panel({ detail: mp3PageDetail(64, 64), onStep: direction => steps.push(direction) });
  assert.equal(control(previous, '上一 MP3 帧').props.disabled, false);
  (control(previous, '上一 MP3 帧').props.onClick as () => void)(); assert.deepEqual(steps, [1, -1]);
});

test('MP3 remote preparing and stopped views retain the frame target and a way to correct it', () => {
  const preparing = panel({ detail: mp3PageDetail(2560, 2600, 'preparing', 1024) });
  assert.match(renderToStaticMarkup(preparing), /正在定位 MP3 帧 2600/);
  assert.equal(control(preparing, '继续定位 MP3 帧页').props.disabled, false);
  assert.equal(control(preparing, '跳转 MP3 帧').props.disabled, false);
  assert.equal(elements(preparing).some(element => element.type === 'select'), false);
  const detail = mp3PageDetail(2560, 2600, 'stopped', 64);
  detail.fields.find(f => f.name === 'pageStopReason')!.value = 'end';
  const stopped = panel({ detail });
  assert.match(renderToStaticMarkup(stopped), /MP3 帧 2600 超出当前 Tag 已确认帧范围/);
  assert.equal(elements(stopped).some(element => element.type === 'button' && element.props.children === '继续定位 MP3 帧页'), false);
  assert.equal(control(stopped, '跳转 MP3 帧').props.disabled, false);
  detail.fields.find(f => f.name === 'pageStopReason')!.value = 'invalid_frame';
  assert.match(renderToStaticMarkup(panel({ detail })), /目标帧或后续边界无法确认/);
  assert.doesNotMatch(renderToStaticMarkup(panel({ detail })), /超出当前 Tag/);
});

test('MP3 input and all navigation controls disable together while a detail request is active', () => {
  const node = panel({ disabled: true });
  const controls = elements(node).filter(element => ['input', 'select', 'button'].includes(String(element.type)));
  assert.ok(controls.length >= 8);
  for (const element of controls) assert.equal(element.props.disabled, true, String(element.props.children ?? element.props['aria-label']));
  assert.equal(control(panel({ detail: mp3PageDetail(0, 0) }), '上一 MP3 帧').props.disabled, true);
});
