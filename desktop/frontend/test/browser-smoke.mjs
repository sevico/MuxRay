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
  let session, sequence = 0;
  window.smokeMode = 'finished';
  const envelope = () => structuredClone({ apiVersion: 1, session, error: null });
  const span = { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 88 };
  window.go = { main: { Native: { OpenFLV: async () => {
    if (window.smokeMode === 'picker-cancel') return { apiVersion: 1, canceled: true, session: null, error: null, fileName: '' };
    session = { sessionId: `synthetic-${++sequence}`, state: window.smokeMode === 'running' ? 'running' : 'finished', terminal: window.smokeMode !== 'running', closed: false,
      progress: { sourceSize: '18446744073709551615', bytesScanned: '9223372036854775807', tagCount: '205', diagnosticCount: 2 },
      scan: window.smokeMode === 'running' ? null : { complete: false, hasErrors: true }, failure: null };
    return { ...envelope(), canceled: false, fileName: 'browser-synthetic <img onerror=alert(1)>.flv' };
  } } }, desktop: { Bindings: {
    GetSession: async () => envelope(),
    CancelScan: async () => { session.state = 'canceled'; session.terminal = true; session.scan = { complete: false, hasErrors: false }; session.failure = { code: 'canceled', message: 'Canceled.' }; return envelope(); },
    CloseFile: async () => { session.closed = true; session.terminal = true; return envelope(); },
    GetTags: async request => {
      assertRequest(request);
      const start = Number(request.start), next = Math.min(start + request.count, 205);
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: String(next), total: '205', limit: request.count }, tags: Array.from({ length: next - start }, (_, index) => ({
        id: String(start + index), typeCode: 9, typeName: 'video', offset: String(9007199254740993n + BigInt(start + index)), span, headerSpan: span, payloadSpan: span,
        rawTypeFlags: 9, filter: false, dataSize: 64, availableDataSize: 64, dts: '-9223372036854775808', streamId: 0, payloadComplete: true,
        previousTagSize: { present: true, span, value: 75, expected: 75, valid: true },
      })) } };
    },
    GetDiagnostics: async request => {
      assertRequest(request);
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: '0', next: '2', total: '2', limit: request.count }, diagnostics: [0, 1].map(i => ({ code: `SYNTHETIC_${i}`, severity: 'error', offset: '18446744073709551615', bitOffset: i, message: '<script>Not executable HTML</script>', expected: null, actual: '9007199254740993' })) } };
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
  console.log(JSON.stringify({ result: 'passed', host: 'synthetic only; native Wails not verified', checks: ['empty state', '100/100/5 pagination', 'exact large integers', 'escaped diagnostic text', 'partial results', 'cancel', 'close', 'picker cancellation'], screenshot }, null, 2));
} finally {
  try { if (target) await call(`/close?target=${target}`); }
  finally { server.close(); }
}
