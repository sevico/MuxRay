// Reusable loopback-only host for an explicitly synthetic browser acceptance.
// Run: node test/browser-host.mjs [port]; then open its printed local URL.
// Optional ?smoke=summary-pending|summary-lost-start|summary-cancel-lost|
// summary-source-changed|summary-short-page selects a deterministic host case.
import { createServer } from 'node:http';
import { readFile, readdir } from 'node:fs/promises';
import { resolve, dirname, extname } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { syntheticHost } from './browser-fixture.mjs';

export async function createBrowserHost(port = 0) {
  const frontend = resolve(dirname(fileURLToPath(import.meta.url)), '..'), assets = new Map();
  const csp = (await readFile(resolve(frontend, '../main.go'), 'utf8')).match(/const contentSecurityPolicy = "([^"]+)"/)[1];
  async function collect(directory, prefix = '') {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const key = `${prefix}/${entry.name}`, path = resolve(directory, entry.name);
      if (entry.isDirectory()) await collect(path, key);
      else if (entry.isFile()) assets.set(key, await readFile(path));
    }
  }
  await collect(resolve(frontend, 'dist'));
  // A same-origin external script is compatible with the production CSP.
  assets.set('/synthetic-host.js', Buffer.from(`(${syntheticHost.toString()})();`));
  const index = assets.get('/index.html').toString('utf8').replace('<head>', '<head><script src="/synthetic-host.js"></script>');
  assets.set('/index.html', Buffer.from(index));
  const types = { '.html': 'text/html; charset=utf-8', '.js': 'text/javascript', '.css': 'text/css' };
  const server = createServer((request, response) => {
    const path = new URL(request.url, 'http://localhost').pathname, key = path === '/' ? '/index.html' : path, body = assets.get(key);
    response.writeHead(body ? 200 : 404, { 'Content-Type': types[extname(key)] ?? 'application/octet-stream', 'Content-Security-Policy': csp, 'X-Content-Type-Options': 'nosniff', 'Cache-Control': 'no-store' });
    response.end(body ?? 'Not found');
  });
  await new Promise((yes, no) => { server.once('error', no); server.listen(port, '127.0.0.1', yes); });
  return { server, url: `http://127.0.0.1:${server.address().port}/` };
}
if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  const port = process.argv[2] === undefined ? 0 : Number(process.argv[2]);
  if (!Number.isInteger(port) || port < 0 || port > 65535) throw Error('Port must be an integer from 0 to 65535.');
  const host = await createBrowserHost(port);
  console.log(JSON.stringify({ url: host.url, host: 'synthetic only; no native Wails or Go parser' }));
  const close = () => host.server.close(); process.once('SIGINT', close); process.once('SIGTERM', close);
}
