import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, PAGE_SIZE, POLL_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, DiagnosticPage, PageResponse, Session, SessionResponse, TagPage } from '../src/protocol.ts';

function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function settle() { for (let i = 0; i < 12; i++) await Promise.resolve(); }
class FakeClock implements Scheduler {
  tasks = new Set<() => void>();
  after(fn: () => void, ms: number) { assert.equal(ms, POLL_MS); this.tasks.add(fn); return () => { this.tasks.delete(fn); }; }
  tick() { const tasks = [...this.tasks]; this.tasks.clear(); for (const task of tasks) task(); }
}
function snapshot(patch: Partial<Session> = {}): Session {
  return { sessionId: 'scan-1', state: 'running', terminal: false, closed: false,
    progress: { bytesScanned: '13', sourceSize: '10000', tagCount: '0', diagnosticCount: 0 }, scan: null, failure: null, ...patch };
}
const terminal = (patch: Partial<Session> = {}) => snapshot({ state: 'finished', terminal: true, scan: { complete: true, hasErrors: false }, ...patch });
const response = (session: Session): SessionResponse => ({ apiVersion: 1, error: null, session });
const tagPage = (sessionId = 'scan-1', start = '0'): PageResponse<TagPage> => ({ apiVersion: 1, error: null, page: { sessionId, bounds: { start, total: '0', next: start, limit: PAGE_SIZE }, tags: [] } });
const diagnosticPage = (): PageResponse<DiagnosticPage> => ({ apiVersion: 1, error: null, page: { sessionId: 'scan-1', bounds: { start: '0', total: '0', next: '0', limit: PAGE_SIZE }, diagnostics: [] } });
function setup(override: Partial<API> = {}) {
  const clock = new FakeClock();
  const api: API = {
    open: async () => ({ ...response(snapshot()), fileName: 'sample.flv', canceled: false }),
    status: async () => response(terminal()), cancel: async () => response(snapshot({ state: 'canceling' })),
    close: async () => response(terminal({ closed: true })),
    tags: async () => tagPage(), diagnostics: async () => diagnosticPage(), ...override,
  };
  return { controller: new Controller(api, clock), clock, api };
}

test('open is serialized and dialog cancellation does not create a session', async () => {
  const opened = deferred<Awaited<ReturnType<API['open']>>>();
  let calls = 0;
  const { controller, clock } = setup({ open: () => { calls++; return opened.promise; } });
  const first = controller.open(); await controller.open();
  assert.equal(calls, 1);
  opened.resolve({ apiVersion: 1, error: null, session: null, fileName: '', canceled: true });
  await first;
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(clock.tasks.size, 0);
});

test('polling is sequential and stops at terminal status; no live index pages', async () => {
  const status = deferred<SessionResponse>(); let polls = 0, pages = 0;
  const { controller, clock } = setup({ status: () => { polls++; return status.promise; }, tags: async request => { pages++; assert.equal(request.count, 100); return tagPage(); } });
  await controller.open();
  assert.equal(pages, 0);
  clock.tick(); clock.tick(); clock.tick();
  assert.equal(polls, 1); assert.equal(clock.tasks.size, 0);
  status.resolve(response(terminal())); await settle();
  assert.equal(pages, 1); assert.equal(clock.tasks.size, 0);
  assert.equal(controller.getSnapshot().page?.tab, 'tags');
});

test('old poll cannot resurrect a closed session or overwrite a new file', async () => {
  const old = deferred<SessionResponse>(); let opens = 0;
  const { controller, clock } = setup({ status: () => old.promise, open: async () => ({ ...response(snapshot({ sessionId: `scan-${++opens}` })), fileName: `file-${opens}.flv`, canceled: false }) });
  await controller.open(); clock.tick(); await controller.close(); await controller.open();
  old.resolve(response(terminal())); await settle();
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
  assert.equal(controller.getSnapshot().session?.state, 'running');
  assert.equal(controller.getSnapshot().fileName, 'file-2.flv');
  assert.equal(clock.tasks.size, 1);
  controller.dispose();
});

test('cancel invalidates an outstanding poll and immediate partial results are pageable', async () => {
  const old = deferred<SessionResponse>();
  const { controller, clock } = setup({ status: () => old.promise, cancel: async () => response(terminal({ state: 'canceled', scan: { complete: false, hasErrors: false }, failure: { code: 'canceled', message: 'Canceled.' } })) });
  await controller.open(); clock.tick(); await controller.cancel(); await settle();
  old.resolve(response(snapshot())); await settle();
  assert.equal(controller.getSnapshot().session?.state, 'canceled');
  assert.equal(controller.getSnapshot().page?.tab, 'tags');
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(clock.tasks.size, 0);
});

test('close remains occupied until the backend reports cleanup terminal', async () => {
  const { controller, clock } = setup({ close: async () => response(snapshot({ closed: true, state: 'canceling' })), status: async () => response(terminal({ closed: true })) });
  await controller.open(); await controller.close(); await controller.open();
  assert.equal(controller.getSnapshot().operation, 'closing');
  assert.equal(controller.getSnapshot().session?.closed, true);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().operation, '');
});

test('coalesced page selection keeps only the latest view and one request in flight', async () => {
  const page = deferred<PageResponse<TagPage>>(); let tagCalls = 0, diagnosticCalls = 0;
  const { controller } = setup({ open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }), tags: () => { tagCalls++; return page.promise; }, diagnostics: async () => { diagnosticCalls++; return diagnosticPage(); } });
  await controller.open(); controller.select('tags', '100'); controller.select('diagnostics');
  assert.equal(tagCalls, 1); assert.equal(diagnosticCalls, 0);
  page.resolve(tagPage()); await settle();
  assert.equal(diagnosticCalls, 1); assert.equal(controller.getSnapshot().page?.tab, 'diagnostics');
});

test('an old page finishing after close is discarded', async () => {
  const page = deferred<PageResponse<TagPage>>();
  const { controller } = setup({ open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }), tags: () => page.promise });
  await controller.open(); await controller.close(); page.resolve(tagPage()); await settle();
  assert.equal(controller.getSnapshot().page, null); assert.equal(controller.getSnapshot().loading, false);
});

test('transport failure stops polling until an explicit retry', async () => {
  let polls = 0;
  const { controller, clock } = setup({ status: async () => { if (++polls === 1) throw new Error('private implementation data'); return response(terminal()); } });
  await controller.open(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().error, 'transport_failed'); assert.equal(clock.tasks.size, 0);
  controller.retry(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().error, null); assert.equal(controller.getSnapshot().session?.terminal, true);
});

test('task failure is separate from request failure; no pages for open failure', async () => {
  let pages = 0;
  const { controller, clock } = setup({ status: async () => response(snapshot({ state: 'failed', terminal: true, progress: null, failure: { code: 'source_not_found', message: 'Not found.' } })), tags: async () => { pages++; return tagPage(); } });
  await controller.open(); clock.tick(); await settle();
  assert.equal(pages, 0); assert.equal(controller.getSnapshot().error, null);
  assert.equal(controller.getSnapshot().session?.failure?.code, 'source_not_found');
});

test('mismatched response identity and API version are not displayed', async () => {
  for (const wrong of [response(terminal({ sessionId: 'scan-other' })), { ...response(terminal()), apiVersion: 2 }]) {
    const { controller, clock } = setup({ status: async () => wrong });
    await controller.open(); clock.tick(); await settle();
    assert.ok(controller.getSnapshot().error); assert.equal(controller.getSnapshot().session?.state, 'running');
    assert.equal(clock.tasks.size, 0);
  }
});

test('page identity is checked and failed page requests can be retried', async () => {
  let pages = 0;
  const { controller } = setup({ open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }), tags: async () => ++pages === 1 ? tagPage('old-session') : tagPage() });
  await controller.open(); await settle();
  assert.equal(controller.getSnapshot().page, null); assert.equal(controller.getSnapshot().error, 'invalid_response');
  controller.retry(); await settle(); assert.equal(controller.getSnapshot().page?.tab, 'tags');
});

test('disposal stops timers and ignores pending replies', async () => {
  const status = deferred<SessionResponse>();
  const { controller, clock } = setup({ status: () => status.promise });
  await controller.open(); clock.tick(); const before = controller.getSnapshot(); controller.dispose();
  status.resolve(response(terminal())); await settle();
  assert.equal(controller.getSnapshot(), before); assert.equal(clock.tasks.size, 0);
});
