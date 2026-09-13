// Optional real-browser frontend smoke using the web-access CDP proxy.
// Synthetic host only: this is NOT a native Wails/picker acceptance test.
import assert from 'node:assert/strict';
import { createServer } from 'node:http';
import { readFile, readdir, mkdir } from 'node:fs/promises';
import { resolve, extname, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';

const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..');
const proxy = new URL(process.env.CDP_PROXY_URL ?? 'http://127.0.0.1:3456');
assert.ok(proxy.protocol === 'http:' && ['127.0.0.1', 'localhost'].includes(proxy.hostname), 'Use a local CDP proxy');
const csp = (await readFile(resolve(frontend, '../main.go'), 'utf8')).match(/const contentSecurityPolicy = "([^"]+)"/)[1];
const assets = new Map();
async function collect(directory, prefix = '') {
  for (const entry of await readdir(directory, { withFileTypes: true })) {
    const name = `${prefix}/${entry.name}`, path = resolve(directory, entry.name);
    if (entry.isDirectory()) await collect(path, name);
    else if (entry.isFile()) assets.set(name, await readFile(path));
  }
}
await collect(resolve(frontend, 'dist'));
const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css' };
const server = createServer((req, res) => {
  const path = new URL(req.url, 'http://localhost').pathname;
  const key = path === '/' ? '/index.html' : path;
  const body = assets.get(key);
  res.writeHead(body ? 200 : 404, { 'Content-Type': types[extname(key)] ?? 'application/octet-stream', 'Content-Security-Policy': csp, 'X-Content-Type-Options': 'nosniff' });
  res.end(body ?? 'Not found');
});
await new Promise((yes, no) => { server.once('error', no); server.listen(0, '127.0.0.1', yes); });
let target;
async function call(route, body) {
  const result = await fetch(new URL(route, proxy), { ...(body === undefined ? {} : { method: 'POST', body }), signal: AbortSignal.timeout(15000) });
  const value = await result.json();
  if (!result.ok || value.error) throw new Error(JSON.stringify(value));
  return value;
}
async function evaluate(code) { return (await call(`/eval?target=${target}`, code)).value; }
async function waitFor(expression) {
  for (let i = 0; i < 60; i++) {
    if (await evaluate(expression)) return;
    await new Promise(resolve => setTimeout(resolve, 50));
  }
  throw new Error(`Browser condition timed out: ${expression}`);
}
async function click(text) {
  await evaluate(`(() => { const matches = [...document.querySelectorAll('button')].filter(b => b.textContent.trim() === ${JSON.stringify(text)}); if(matches.length !== 1 || matches[0].disabled) throw Error('Button unavailable'); matches[0].click(); return true; })()`);
}

function syntheticHost() {
  let session, latestPick, latestDetail, latestHex, sequence = 0;
  window.smokeMode = 'finished';
  window.smokeReads = [];
  const envelope = () => structuredClone({ apiVersion: 1, session, error: null });
  const span = { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 88 };
  window.go = { main: { Native: { OpenFLV: async requestId => {
    if (window.smokeMode === 'picker-cancel') return latestPick = { apiVersion: 1, requestId, pending: false, canceled: true, session: null, error: null, fileName: '' };
    session = { sessionId: `synthetic-${++sequence}`, state: window.smokeMode === 'running' ? 'running' : 'finished', terminal: window.smokeMode !== 'running', closed: false,
      progress: { sourceSize: '18446744073709551615', bytesScanned: '9223372036854775807', tagCount: '205', diagnosticCount: 2 },
      scan: window.smokeMode === 'running' ? null : { complete: false, hasErrors: true }, failure: null };
    return latestPick = { ...envelope(), requestId, pending: false, canceled: false, fileName: 'browser-synthetic <img onerror=alert(1)>.flv' };
  }, GetOpenResult: async requestId => {
    if (latestPick?.requestId !== requestId) throw Error('Unexpected synthetic open result request');
    return structuredClone({ ...latestPick, session: latestPick.canceled ? null : session });
  } } }, desktop: { Bindings: {
    GetSession: async () => envelope(),
    CancelScan: async () => { session.state = 'canceled'; session.terminal = true; session.scan = { complete: false, hasErrors: false }; session.failure = { code: 'canceled', message: 'Canceled.' }; return envelope(); },
    CloseFile: async () => { session.closed = true; session.terminal = true; return envelope(); },
    GetTags: async request => {
      assertRequest(request);
      const start = Number(request.start), next = Math.min(start + request.count, 205);
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: String(next), total: '205', limit: request.count }, tags: Array.from({ length: next - start }, (_, index) => ({
        id: String(start + index), typeCode: start + index >= 6 && start + index <= 9 ? 8 : 9, typeName: start + index >= 6 && start + index <= 9 ? 'audio' : 'video', offset: String(9007199254740993n + BigInt(start + index)), span: { ...span, byteOffset: String(9007199254740993n + BigInt(start + index)) }, headerSpan: span, payloadSpan: span,
        rawTypeFlags: start + index >= 6 && start + index <= 9 ? 8 : 9, filter: false, dataSize: 64, availableDataSize: 64, dts: '-9223372036854775808', streamId: 0, payloadComplete: true,
        previousTagSize: { present: true, span, value: 75, expected: 75, valid: true },
      })) } };
    },
    GetDiagnostics: async request => {
      assertRequest(request);
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: '0', next: '2', total: '2', limit: request.count }, diagnostics: [0, 1].map(i => ({ code: `SYNTHETIC_${i}`, severity: 'error', offset: '18446744073709551615', bitOffset: i, message: '<script>Not executable HTML</script>', expected: null, actual: '9007199254740993' })) } };
    },
    StartDetail: async request => {
      if (!session.terminal || session.closed || request.sessionId !== session.sessionId) throw Error('Unexpected synthetic detail request');
      window.smokeReads.push({ kind: 'detail', ...request });
      if (request.tagId === '9') {
        latestDetail = { apiVersion: 1, ...request, state: 'failed', error: { code: 'detail_unsupported', message: 'Synthetic SoundFormat=14 remains unsupported.' }, detail: null };
        return structuredClone(latestDetail);
      }
      const preview = {
        kind: request.tagId === '0' ? 'amf0ScriptData' : 'aacAudio', complete: request.tagId === '0', truncated: true,
        // The enclosing byte range ends at UINT64_MAX, without overflowing it.
        fields: [{ id: '0', parentId: '', name: 'synthetic payload', type: 'string', value: '<img onerror=alert(1)>字段值</img>', span: { byteOffset: '18446744073709551614', bitOffset: 3, bitLength: 5 } }],
        diagnostics: [{ code: 'SYNTHETIC_DETAIL', severity: 'warning', offset: '9007199254740993', bitOffset: 3, message: '<script>plain diagnostic</script>', expected: null, actual: null }],
      };
      if (['2', '3', '4', '5'].includes(request.tagId)) {
        const missingConfig = request.tagId === '3', contextBudget = request.tagId === '4', nalBudget = request.tagId === '5';
        preview.kind = 'avcVideo'; preview.complete = !missingConfig && !contextBudget && !nalBudget; preview.truncated = contextBudget || nalBudget;
        preview.fields = [
          { id: '0', parentId: '', name: 'AVC context', type: 'context', value: missingConfig ? 'missing' : contextBudget ? 'budget_exhausted' : 'found', span: null },
          { id: '1', parentId: '', name: 'compositionTime', type: 'int24', value: '-2', span: { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 24 } },
          { id: '2', parentId: '', name: 'PTS', type: 'int64', value: '-9223372036854775808', span: null },
          ...missingConfig || contextBudget ? [] : [{ id: '3', parentId: '', name: 'nalUnitType', type: 'uint5', value: '5', span: { byteOffset: '18446744073709551614', bitOffset: 3, bitLength: 5 } }],
        ];
        preview.diagnostics = missingConfig ? [{ code: 'AVC_NAL_LENGTH_SIZE_UNAVAILABLE', severity: 'error', offset: '9007199254740993', bitOffset: 0, message: '缺少前置 AVC 配置，保留包头；不猜测 NAL 长度。', expected: null, actual: null }]
          : contextBudget ? [{ code: 'AVC_CONTEXT_LOOKUP_LIMIT_REACHED', severity: 'error', offset: '9007199254740993', bitOffset: 0, message: '前置配置查找达到预算，不猜测 NAL 长度。', expected: null, actual: null }]
            : nalBudget ? [{ code: 'AVC_NAL_UNIT_LIMIT_REACHED', severity: 'error', offset: '18446744073709551615', bitOffset: 0, message: 'NAL 数量达到解析预算，保留已解析前缀。', expected: '128', actual: '129' }] : [];
      }
      if (['6', '7', '8'].includes(request.tagId)) {
        const freeFormat = request.tagId === '7', malformed = request.tagId === '8';
        preview.kind = 'mp3Audio'; preview.complete = !malformed; preview.truncated = false;
        preview.fields = [
          { id: '0', parentId: '', name: 'bitrateIndex', type: 'uint4', value: malformed ? '15 (invalid)' : freeFormat ? '0 (free-format)' : '9', span: { byteOffset: '9007199254740995', bitOffset: 0, bitLength: 4 } },
          { id: '1', parentId: '', name: 'bitrateMode', type: 'derived', value: malformed ? 'invalid' : freeFormat ? 'free' : 'indexed', span: null },
          ...(malformed || freeFormat ? [] : [
            { id: '2', parentId: '', name: 'bitrateKbps', type: 'derived', value: '128', span: null },
            { id: '3', parentId: '', name: 'frameLengthBytes', type: 'derived', value: '417', span: null },
          ]),
        ];
        preview.diagnostics = malformed ? [{ code: 'MP3_BITRATE_INDEX_INVALID', severity: 'error', offset: '9007199254740995', bitOffset: 0, message: 'Reserved bitrate index; retain fixed header fields.', expected: null, actual: '15' }]
          : freeFormat ? [{ code: 'MP3_FREE_FORMAT_UNRESOLVED', severity: 'warning', offset: '9007199254740995', bitOffset: 0, message: 'Free-format bitrate and frame length are unknown.', expected: null, actual: null }] : [];
      }
      if (request.tagId === '10') {
        preview.kind = 'amf0ScriptData'; preview.complete = true; preview.truncated = false;
        preview.fields = [
          { id: '0', parentId: '', name: 'cross-window', type: 'bytes', value: '1024 byte range', span: { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 8192 } },
          { id: '1', parentId: '', name: 'cross-byte', type: 'uint5', value: '0', span: { byteOffset: '9007199254740993', bitOffset: 6, bitLength: 5 } },
          { id: '2', parentId: '', name: 'derived', type: 'number', value: '3', span: null },
        ];
      }
      latestDetail = { apiVersion: 1, ...request, state: 'ready', error: null, detail: preview };
      return structuredClone(latestDetail);
    },
    GetDetail: async request => {
      if (request.sessionId !== latestDetail.sessionId || request.requestId !== latestDetail.requestId) throw Error('Unexpected synthetic detail query');
      return structuredClone(latestDetail);
    },
    StartHex: async request => {
      if (!session.terminal || session.closed || request.sessionId !== session.sessionId || request.count !== 512) throw Error('Unexpected synthetic Hex request');
      window.smokeReads.push({ kind: 'hex', ...request });
      if (window.smokeMode === 'source-changed') return { apiVersion: 1, ...request, state: 'failed', window: null, error: { code: 'source_changed', message: 'Synthetic change' } };
      const remain = BigInt(session.progress.sourceSize) - BigInt(request.offset), count = remain < 512n ? Number(remain) : 512;
      if (remain < 0n) throw Error('Hex offset beyond EOF');
      latestHex = { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: session.progress.sourceSize, hex: Array.from({ length: count }, (_, i) => (i % 256).toString(16).padStart(2, '0')).join(''), eof: remain <= 512n } };
      return structuredClone(latestHex);
    },
    GetHex: async request => {
      if (request.sessionId !== latestHex.sessionId || request.requestId !== latestHex.requestId) throw Error('Unexpected synthetic Hex query');
      return structuredClone(latestHex);
    },
  } } };
  function assertRequest(request) {
    if (!session.terminal || session.closed || request.sessionId !== session.sessionId || request.count !== 100) throw Error('Unexpected synthetic page request');
  }
  return true;
}

try {
  target = (await call(`/new?url=${encodeURIComponent(`http://127.0.0.1:${server.address().port}/`)}`)).targetId;
  await waitFor("document.querySelector('h1')?.textContent.includes('MuxRay')");
  console.log('Initial DOM:', await evaluate("[...document.querySelectorAll('button')].map(b => b.textContent)"));
  await evaluate(`(${syntheticHost.toString()})()`);
  await click('打开 FLV 文件');
  await waitFor("document.querySelectorAll('tbody tr').length === 100");
  assert.equal(await evaluate("document.querySelector('tbody tr td:nth-child(3)').textContent"), '9007199254740993');
  assert.equal(await evaluate("document.querySelector('tbody tr td:nth-child(4)').textContent"), '-9223372036854775808');
  assert.equal(await evaluate("document.querySelector('h2 img') === null"), true);
  await click('下一页');
  await waitFor("document.querySelector('tbody tr td')?.textContent === '100'");
  let screenshot = null;
  if (process.env.MUXRAY_SCREENSHOT === '1') {
    screenshot = resolve(frontend, '../../.cache/desktop-ui-smoke.png');
    await mkdir(dirname(screenshot), { recursive: true });
    await call(`/screenshot?target=${target}&file=${encodeURIComponent(screenshot)}`);
  }
  assert.equal(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true, 'No window-wide horizontal overflow');
  await click('下一页');
  await waitFor("document.querySelectorAll('tbody tr').length === 5");
  assert.equal(await evaluate("[...document.querySelectorAll('button')].find(b=>b.textContent==='下一页').disabled"), true);
  await click('诊断 (2)');
  await waitFor("document.querySelectorAll('tbody tr').length === 2");
  assert.equal(await evaluate("document.querySelector('tbody p').textContent"), '<script>Not executable HTML</script>');
  await click('Tag 索引');
  await waitFor("document.querySelector('tbody tr td')?.textContent === '0'");
  await click('0');
  await waitFor("document.querySelector('.detail-heading h2')?.textContent === 'AMF0 Script Data'");
  assert.equal(await evaluate("document.querySelector('.field-value').textContent"), '<img onerror=alert(1)>字段值</img>');
  assert.equal(await evaluate("document.querySelector('.detail-panel img') === null"), true);
  assert.equal(await evaluate("document.querySelector('.detail-fields td:nth-child(4)').textContent"), '18446744073709551614 : 3');
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent.includes('语法解析完成') && document.querySelector('.detail-badges').textContent.includes('展示省略')"), true);
  await click('1');
  await waitFor("document.querySelector('.detail-heading h2')?.textContent === 'AAC Audio'");
  assert.equal(await evaluate("document.querySelector('.detail-panel').textContent.includes('不解码音频')"), true);
  assert.equal(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true, 'Detail layout keeps horizontal scrolling inside its panes');
  await click('2');
  await waitFor("document.querySelector('.detail-heading h2')?.textContent === 'AVC Video'");
  assert.equal(await evaluate("document.querySelector('.detail-panel').textContent.includes('不解码 slice')"), true);
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent"), '语法解析完成');
  assert.deepEqual(await evaluate("(() => { const row = [...document.querySelectorAll('.detail-fields tbody tr')].find(row => row.cells[0].textContent === 'PTS'); return [row.cells[2].textContent, row.cells[3].textContent, row.cells[4].textContent]; })()"), ['-9223372036854775808', '—', '—']);
  assert.equal(await evaluate("[...document.querySelectorAll('.detail-fields tbody tr')].find(row => row.cells[0].textContent === 'nalUnitType').cells[3].textContent"), '18446744073709551614 : 3');
  await click('3');
  await waitFor("document.querySelector('.detail-diagnostics code')?.textContent === 'AVC_NAL_LENGTH_SIZE_UNAVAILABLE'");
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent"), '部分解析');
  assert.equal(await evaluate("document.querySelector('.detail-fields').textContent.includes('nalUnitType')"), false, 'Missing config does not show old NAL fields');
  assert.equal(await evaluate("document.querySelector('.detail-panel').getAttribute('aria-label')"), 'Tag 3 详情');
  await click('4');
  await waitFor("document.querySelector('.detail-diagnostics code')?.textContent === 'AVC_CONTEXT_LOOKUP_LIMIT_REACHED'");
  assert.equal(await evaluate("document.querySelector('.detail-fields').textContent.includes('budget_exhausted')"), true);
  assert.equal(await evaluate("document.querySelector('.detail-fields').textContent.includes('nalUnitType')"), false, 'Budget exhaustion does not invent NAL boundaries');
  await click('5');
  await waitFor("document.querySelector('.detail-diagnostics code')?.textContent === 'AVC_NAL_UNIT_LIMIT_REACHED'");
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent.includes('部分解析') && document.querySelector('.detail-badges').textContent.includes('展示省略')"), true);
  assert.equal(await evaluate("document.querySelector('.detail-diagnostics td:last-child').textContent"), '128 / 129');
  assert.equal(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true, 'AVC fields stay inside the details pane');
  await click('6');
  await waitFor("document.querySelector('.detail-heading h2')?.textContent === 'MP3 Audio'");
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent"), '语法解析完成');
  assert.equal(await evaluate("document.querySelector('.detail-panel').textContent.includes('首个 MPEG Audio Layer III 固定帧头') && document.querySelector('.detail-panel').textContent.includes('不保证整帧完整') && document.querySelector('.detail-panel').textContent.includes('不校验 CRC') && document.querySelector('.detail-panel').textContent.includes('不分析 VBR')"), true);
  assert.deepEqual(await evaluate("(() => { const row = [...document.querySelectorAll('.detail-fields tbody tr')].find(row => row.cells[0].textContent === 'frameLengthBytes'); return [row.cells[2].textContent, row.cells[3].textContent, row.cells[4].textContent]; })()"), ['417', '—', '—']);
  assert.equal(await evaluate("document.querySelector('.detail-fields td:nth-child(4)').textContent"), '9007199254740995 : 0');
  await click('7');
  await waitFor("document.querySelector('.detail-diagnostics code')?.textContent === 'MP3_FREE_FORMAT_UNRESOLVED'");
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent"), '语法解析完成', 'Free-format warning keeps fixed-header syntax complete');
  assert.equal(await evaluate("[...document.querySelectorAll('.detail-fields tbody tr')].some(row => ['bitrateKbps', 'frameLengthBytes'].includes(row.cells[0].textContent))"), false, 'Unknown free-format values stay absent instead of retaining indexed values');
  assert.equal(await evaluate("[...document.querySelectorAll('.detail-fields tbody tr')].find(row => row.cells[0].textContent === 'bitrateMode').cells[2].textContent"), 'free');
  assert.equal(await evaluate("document.querySelector('.detail-panel').textContent.includes('free-format 的码率和帧长未知')"), true);
  await click('8');
  await waitFor("document.querySelector('.detail-diagnostics code')?.textContent === 'MP3_BITRATE_INDEX_INVALID'");
  assert.equal(await evaluate("document.querySelector('.detail-badges').textContent"), '部分解析');
  assert.equal(await evaluate("document.querySelector('.detail-fields').textContent.includes('frameLengthBytes')"), false, 'Malformed header does not keep old derived values');
  assert.equal(await evaluate('document.documentElement.scrollWidth <= window.innerWidth'), true, 'MP3 fields stay inside the details pane');
  await click('9');
  await waitFor("document.querySelector('.detail-panel [role=alert]')?.textContent.includes('当前支持 AMF0 Script Data、AAC 音频、AVC 视频和 MP3 音频')");
  assert.equal(await evaluate("document.querySelector('.detail-fields') === null"), true, 'Unsupported audio clears the preceding MP3 detail');
  const beforeRaw = await evaluate('window.smokeReads.length');
  await evaluate("document.querySelector('[aria-label=\"查看 Tag 9 原始字节\"]').click()");
  await waitFor("document.querySelector('.hex-grid tbody tr') !== null");
  assert.deepEqual(await evaluate(`window.smokeReads.slice(${beforeRaw}).map(r => [r.kind,r.offset,r.count])`), [['hex', '9007199254741002', 512]], 'Raw Tag action does not start a detail job');
  assert.equal(await evaluate("document.querySelectorAll('.hex-grid tbody tr').length"), 32);
  await evaluate("document.querySelector('[aria-label=\"查看 Tag 10 详情\"]').click()");
  await waitFor("document.querySelector('[aria-label=\"定位字段 cross-window\"]') !== null");
  assert.equal(await evaluate("document.querySelector('.hex-grid') === null"), true, 'New Tag clears old byte window');
  assert.equal(await evaluate("[...document.querySelectorAll('.detail-fields tr')].find(r => r.textContent.includes('derived')).querySelector('button') === null"), true);
  await evaluate("document.querySelector('[aria-label=\"定位字段 cross-window\"]').click()");
  await waitFor("document.querySelector('.hex-grid tbody th')?.textContent === '9007199254740993'");
  assert.equal(await evaluate("document.querySelector('.hex-selection').textContent.includes('超出当前窗口')"), true);
  assert.equal(await evaluate("document.querySelectorAll('.hex-byte.range-selected').length"), 512);
  await click('后一窗口');
  await waitFor("document.querySelector('.hex-grid tbody th')?.textContent === '9007199254741505'");
  assert.equal(await evaluate("document.querySelector('.hex-selection').textContent.includes('9007199254740993')"), true, 'Paging retains original field selection');
  await evaluate("document.querySelector('[aria-label=\"定位字段 cross-byte\"]').click()");
  await waitFor("document.querySelectorAll('.hex-byte.range-selected').length === 2");
  assert.deepEqual(await evaluate("[...document.querySelectorAll('.hex-bits .range-selected')].map(n=>n.getAttribute('aria-label'))"), ['bit 6 范围内', 'bit 7 范围内']);
  await evaluate("document.querySelector('[aria-label=\"定位诊断 SYNTHETIC_DETAIL\"]').click()");
  await waitFor("document.querySelector('.hex-panel')?.getAttribute('aria-busy') === 'false' && document.querySelector('.hex-grid tbody tr') !== null && document.querySelector('.hex-selection').textContent.includes('仅标记位置') && document.querySelectorAll('.hex-byte.point-selected').length === 1");
  assert.equal(await evaluate("document.querySelectorAll('.hex-byte.range-selected').length"), 0);
  assert.equal(await evaluate("document.querySelectorAll('.hex-byte.point-selected').length"), 1);
  assert.equal(await evaluate("document.querySelector('.hex-panel img') === null && document.documentElement.scrollWidth <= window.innerWidth"), true);
  await evaluate("(() => { const input=document.querySelector('#hex-offset-input'); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'18446744073709551615'); input.dispatchEvent(new Event('input',{bubbles:true})); })()");
  await click('跳转偏移');
  await waitFor("document.querySelector('[aria-label=\"Hex 实际窗口\"]')?.textContent.includes('0 byte')");
  assert.equal(await evaluate("document.querySelector('.hex-grid') === null && document.querySelector('.hex-panel').textContent.includes('此位置没有字节')"), true);
  await evaluate("(() => { const input=document.querySelector('#tag-id-input'); Object.getOwnPropertyDescriptor(HTMLInputElement.prototype,'value').set.call(input,'105'); input.dispatchEvent(new Event('input',{bubbles:true})); })()");
  await click('跳转 Tag');
  await waitFor("document.querySelector('.selected-tag .tag-select')?.textContent === '105'");
  assert.equal(await evaluate("document.querySelector('.results tbody tr td')?.textContent"), '100');
  await evaluate("window.smokeMode='source-changed'; document.querySelector('[aria-label=\"查看 Tag 105 原始字节\"]').click()");
  await waitFor("document.querySelector('main > [role=alert]')?.textContent.includes('文件内容已改变')");
  assert.equal(await evaluate("document.querySelector('.detail-fields') === null && document.querySelector('.hex-grid') === null && document.querySelector('#hex-offset-input').disabled"), true, 'Source change invalidates both views');
  await click('关闭文件');
  await waitFor("document.querySelector('.empty-state') !== null");
  await evaluate("window.smokeMode = 'running'");
  await click('打开 FLV 文件');
  await waitFor("document.querySelector('.status')?.textContent === '扫描中'");
  assert.equal(await evaluate("document.querySelector('[role=tab]').disabled"), true);
  await click('取消扫描');
  await waitFor("document.querySelector('.status')?.textContent === '已取消' && document.querySelectorAll('tbody tr').length === 100");
  await click('关闭文件');
  await waitFor("document.querySelector('.empty-state') !== null");
  await evaluate("window.smokeMode = 'picker-cancel'");
  await click('打开 FLV 文件');
  await waitFor("document.querySelector('.topbar .primary')?.disabled === false");
  assert.equal(await evaluate("document.querySelector('.empty-state') !== null && document.querySelector('[role=alert]') === null"), true);
  console.log(JSON.stringify({ result: 'passed', host: 'synthetic only; native Wails not verified', checks: ['empty state', '100/100/5 pagination', 'exact large integers', 'escaped diagnostic text', 'partial results', 'AMF0/AAC/AVC/MP3 detail panes', 'AVC derived values without fabricated spans', 'AVC missing configuration', 'AVC context lookup budget', 'AVC bounded prefix diagnostics', 'MP3 fixed-header scope', 'MP3 free-format unknown derived values', 'MP3 malformed header and unsupported audio clear stale values', 'escaped field previews', 'exact byte/bit positions', '512-byte Hex windows', 'raw unsupported Tag without detail parse', 'cross-window and cross-byte selection', 'diagnostic point without range', 'empty EOF', 'Tag ID jump', 'source change invalidates both views', 'cancel', 'close', 'picker cancellation'], screenshot }, null, 2));
} finally {
  try { if (target) await call(`/close?target=${target}`); }
  finally { server.close(); }
}
