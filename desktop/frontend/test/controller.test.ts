import { test } from 'node:test';
import assert from 'node:assert/strict';
import { Controller, PAGE_SIZE, POLL_MS, OPEN_REPLY_TIMEOUT_MS } from '../src/controller.ts';
import type { Scheduler } from '../src/controller.ts';
import type { API, DiagnosticPage, PageResponse, PickResponse, Session, SessionResponse, TagPage } from '../src/protocol.ts';

function deferred<T>() {
  let resolve!: (value: T) => void, reject!: (error: Error) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
async function settle() { for (let i = 0; i < 12; i++) await Promise.resolve(); }
class FakeClock implements Scheduler {
  tasks = new Set<() => void>();
  delays = new Map<() => void, number>();
  after(fn: () => void, ms: number) {
    assert.ok(ms === POLL_MS || ms === OPEN_REPLY_TIMEOUT_MS);
    this.tasks.add(fn); this.delays.set(fn, ms);
    return () => { this.tasks.delete(fn); this.delays.delete(fn); };
  }
  tick(ms = POLL_MS) {
    const tasks = [...this.tasks].filter(task => this.delays.get(task) === ms);
    for (const task of tasks) { this.tasks.delete(task); this.delays.delete(task); task(); }
  }
}
function snapshot(patch: Partial<Session> = {}): Session {
  return { sessionId: 'scan-1', state: 'running', terminal: false, closed: false,
    progress: { bytesScanned: '13', sourceSize: '10000', tagCount: '0', diagnosticCount: 0 }, scan: null, failure: null, ...patch };
}
const terminal = (patch: Partial<Session> = {}) => snapshot({ state: 'finished', terminal: true, scan: { complete: true, hasErrors: false }, ...patch });
const response = (session: Session): SessionResponse => ({ apiVersion: 1, error: null, session });
const tagPage = (sessionId = 'scan-1', start = '0'): PageResponse<TagPage> => ({ apiVersion: 1, error: null, page: { sessionId, bounds: { start, total: '0', next: start, limit: PAGE_SIZE }, tags: [] } });
const diagnosticPage = (): PageResponse<DiagnosticPage> => ({ apiVersion: 1, error: null, page: { sessionId: 'scan-1', bounds: { start: '0', total: '0', next: '0', limit: PAGE_SIZE }, diagnostics: [] } });
type FixturePick = Omit<PickResponse, 'requestId' | 'pending'> & Partial<Pick<PickResponse, 'requestId' | 'pending'>>;
function setup(override: Partial<Omit<API, 'open'>> & { open?: (requestId: string) => Promise<FixturePick> } = {}) {
  const clock = new FakeClock();
  const { open = async () => ({ ...response(snapshot()), fileName: 'sample.flv', canceled: false }), ...rest } = override;
  const api: API = {
    startAVCSearch: async () => { throw Error('unexpected AVC search'); }, avcSearchResult: async () => { throw Error('unexpected AVC search result'); }, cancelAVCSearch: async () => { throw Error('unexpected AVC search cancel'); },
    mediaCurves: async () => { throw Error('unexpected media curves'); },
    saveSnapshot: async () => { throw Error('unexpected save'); }, saveResult: async () => { throw Error('unexpected save confirmation'); },
    tagTrends: async () => { throw Error('unexpected trend query'); },
    filteredTags: async () => { throw Error('unexpected filtered tags'); },
    open: async requestId => ({ requestId, pending: false, ...await open(requestId) }),
    openResult: async requestId => ({ requestId, pending: false, ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    status: async () => response(terminal()), cancel: async () => response(snapshot({ state: 'canceling' })),
    close: async () => response(terminal({ closed: true })),
    tags: async () => tagPage(), diagnostics: async () => diagnosticPage(),
    startDetail: async () => { throw new Error('unexpected detail request'); }, detailResult: async () => { throw new Error('unexpected detail query'); },
    startHex: async () => { throw new Error('unexpected Hex request'); }, hexResult: async () => { throw new Error('unexpected Hex query'); }, ...unexpectedSummary, ...rest,
  };
  return { controller: new Controller(api, clock), clock, api };
}

test('open is serialized and dialog cancellation does not create a session', async () => {
  const opened = deferred<FixturePick>();
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
  assert.equal(polls, 1); assert.equal(clock.tasks.size, 1, 'only the current reply deadline remains');
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

test('a lost close reply is reconciled even when the last known scan was terminal', async () => {
  let closed = false, closes = 0, polls = 0, pages = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    close: async () => { closes++; closed = true; throw new Error('reply lost after close'); },
    status: async () => { polls++; return response(terminal({ closed })); },
    tags: async () => { pages++; assert.equal(closed, false, 'must reconcile status before requesting another page'); return tagPage(); },
  });
  await controller.open(); await settle();
  assert.equal(pages, 1);
  await controller.close();
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  assert.equal(controller.getSnapshot().page, null);
  assert.equal(pages, 1);
  assert.equal(clock.tasks.size, 1);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().fileName, '');
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(closes, 1); assert.equal(polls, 1); assert.equal(pages, 1);
  assert.equal(clock.tasks.size, 0);
});

test('reconciliation keeps an unclosed file and coalesces retries and selections while status is pending', async () => {
  const status = deferred<SessionResponse>(); let polls = 0, closes = 0, tags = 0, diagnostics = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    close: async () => { closes++; throw new Error('request never reached the host'); },
    status: () => { polls++; return status.promise; },
    tags: async () => { tags++; return tagPage(); },
    diagnostics: async () => { diagnostics++; return diagnosticPage(); },
  });
  await controller.open(); await settle(); await controller.close(); clock.tick();
  controller.retry(); controller.retry(); clock.tick();
  controller.select('tags', '100'); controller.select('diagnostics'); await settle();
  assert.equal(polls, 1); assert.equal(tags, 1); assert.equal(diagnostics, 0);
  assert.equal(controller.getSnapshot().page, null);
  assert.equal(clock.tasks.size, 1, 'retry does not add a poll while its reply deadline is active');
  status.resolve(response(terminal())); await settle();
  assert.equal(controller.getSnapshot().session?.closed, false);
  assert.equal(controller.getSnapshot().fileName, 'sample.flv');
  assert.equal(controller.getSnapshot().operation, '');
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(controller.getSnapshot().page?.tab, 'diagnostics');
  assert.equal(closes, 1); assert.equal(polls, 1); assert.equal(tags, 1); assert.equal(diagnostics, 1);
  assert.equal(clock.tasks.size, 0);
});

test('failed close reconciliation stops until retry, preserving the error across page selection', async () => {
  let polls = 0, closes = 0, pages = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    close: async () => { closes++; throw new Error('reply lost'); },
    status: async () => { if (++polls === 1) throw new Error('status unavailable'); return response(terminal({ closed: true })); },
    tags: async () => { pages++; return tagPage(); },
    diagnostics: async () => { pages++; return diagnosticPage(); },
  });
  await controller.open(); await settle(); await controller.close();
  clock.tick(); await settle();
  assert.equal(polls, 1); assert.equal(clock.tasks.size, 0);
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  controller.select('diagnostics'); await settle();
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  assert.equal(pages, 1);
  controller.retry(); controller.retry();
  assert.equal(clock.tasks.size, 1);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(polls, 2); assert.equal(closes, 1); assert.equal(pages, 1);
  assert.equal(clock.tasks.size, 0);
});

test('a lost close reply keeps the slot occupied until reconciliation observes cleanup terminal', async () => {
  let polls = 0, opens = 0;
  const { controller, clock } = setup({
    open: async () => { opens++; return { ...response(snapshot()), fileName: 'sample.flv', canceled: false }; },
    close: async () => { throw new Error('reply lost'); },
    status: async () => response(++polls === 1 ? snapshot({ closed: true, state: 'canceling' }) : terminal({ closed: true })),
  });
  await controller.open(); await controller.close(); await controller.open();
  assert.equal(opens, 1);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().operation, 'closing');
  assert.equal(controller.getSnapshot().session?.closed, true);
  assert.equal(controller.getSnapshot().error, null);
  await controller.open(); assert.equal(opens, 1);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(clock.tasks.size, 0);
});

test('a lost cancel reply recovers partial results without repeating cancellation', async () => {
  let cancels = 0, pages = 0;
  const { controller, clock } = setup({
    cancel: async () => { cancels++; throw new Error('reply lost'); },
    status: async () => response(terminal({ state: 'canceled', scan: { complete: false, hasErrors: false }, failure: { code: 'canceled', message: 'Canceled.' } })),
    tags: async () => { pages++; return tagPage(); },
  });
  await controller.open(); await controller.cancel();
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  assert.equal(pages, 0);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session?.state, 'canceled');
  assert.equal(controller.getSnapshot().session?.scan?.complete, false);
  assert.equal(controller.getSnapshot().page?.tab, 'tags');
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(cancels, 1); assert.equal(pages, 1); assert.equal(clock.tasks.size, 0);
});

test('a stale poll cannot consume a pending close reconciliation', async () => {
  const old = deferred<SessionResponse>(); let polls = 0, pages = 0;
  const { controller, clock } = setup({
    status: () => ++polls === 1 ? old.promise : Promise.resolve(response(terminal({ closed: true }))),
    cancel: async () => response(terminal({ state: 'canceled', scan: { complete: false, hasErrors: false } })),
    close: async () => { throw new Error('reply lost'); },
    tags: async () => { pages++; return tagPage(); },
  });
  await controller.open(); clock.tick(); await controller.cancel(); await settle();
  assert.equal(pages, 1);
  await controller.close();
  assert.equal(clock.tasks.size, 1, 'an invalidated poll must not block command confirmation');
  old.resolve(response(snapshot())); await settle();
  assert.equal(controller.getSnapshot().session?.state, 'canceled');
  assert.equal(controller.getSnapshot().page, null);
  assert.equal(clock.tasks.size, 1);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(polls, 2); assert.equal(pages, 1);
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
  let pages = 0, polls = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    status: async () => { polls++; return response(terminal()); },
    tags: async () => ++pages === 1 ? tagPage('old-session') : tagPage(),
  });
  await controller.open(); await settle();
  assert.equal(controller.getSnapshot().page, null); assert.equal(controller.getSnapshot().error, 'invalid_response');
  controller.retry(); await settle();
  assert.equal(pages, 1, 'retry must refresh terminal status before requesting a page');
  clock.tick(); await settle();
  assert.equal(polls, 1); assert.equal(pages, 2);
  assert.equal(controller.getSnapshot().page?.tab, 'tags');
  assert.equal(clock.tasks.size, 0);
});

test('disposal stops timers and ignores pending replies', async () => {
  const status = deferred<SessionResponse>();
  const { controller, clock } = setup({ status: () => status.promise });
  await controller.open(); clock.tick(); const before = controller.getSnapshot(); controller.dispose();
  status.resolve(response(terminal())); await settle();
  assert.equal(controller.getSnapshot(), before); assert.equal(clock.tasks.size, 0);
});

test('lost open reply recovers the original completed session without another open', async () => {
  let opens = 0, queries = 0;
  const { controller, clock } = setup({
    open: async id => { opens++; assert.equal(id, '1'); throw new Error('reply lost after host open'); },
    openResult: async requestId => { queries++; return { requestId, pending: false, ...response(terminal()), fileName: 'recovered.flv', canceled: false }; },
  });
  await controller.open();
  assert.equal(controller.getSnapshot().operation, 'recovering');
  await controller.open();
  clock.tick(); await settle();
  assert.equal(opens, 1); assert.equal(queries, 1);
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-1');
  assert.equal(controller.getSnapshot().fileName, 'recovered.flv');
  assert.equal(controller.getSnapshot().page?.tab, 'tags');
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(clock.tasks.size, 0);
});

test('unseen open or canceled picker recovery releases admission with a fresh request ID', async () => {
  const ids: string[] = [];
  const { controller, clock } = setup({
    open: async id => { ids.push(id); throw new Error('delivery uncertain'); },
    openResult: async requestId => ({ requestId, pending: false, apiVersion: 1, error: null, session: null, canceled: true, fileName: '' }),
  });
  await controller.open(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().operation, '');
  assert.equal(controller.getSnapshot().error, null);
  await controller.open();
  assert.deepEqual(ids, ['1', '2']);
  controller.dispose();
});

test('failed open recovery stays blocked until retry and coalesces queries', async () => {
  const pending = deferred<PickResponse>(); let queries = 0, opens = 0;
  const { controller, clock } = setup({
    open: async () => { opens++; throw new Error('reply lost'); },
    openResult: async () => { if (++queries === 1) throw new Error('status unavailable'); return pending.promise; },
  });
  await controller.open(); clock.tick(); await settle();
  assert.equal(clock.tasks.size, 0);
  controller.select('tags');
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  await controller.open();
  assert.equal(opens, 1);
  controller.retry(); controller.retry(); clock.tick();
  controller.retry(); clock.tick();
  assert.equal(queries, 2);
  pending.resolve({ requestId: '1', pending: false, ...response(terminal()), fileName: 'recovered.flv', canceled: false });
  await settle();
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-1');
  assert.equal(controller.getSnapshot().error, null);
});

test('pending picker recovery polls sequentially until the original dialog completes', async () => {
  let queries = 0;
  const { controller, clock } = setup({
    open: async () => { throw new Error('reply lost while dialog remains open'); },
    openResult: async requestId => ++queries === 1
      ? { requestId, pending: true, apiVersion: 1, error: null, session: null, canceled: false, fileName: '' }
      : { requestId, pending: false, ...response(snapshot()), fileName: 'sample.flv', canceled: false },
  });
  await controller.open(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().operation, 'recovering');
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(clock.tasks.size, 1);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session?.state, 'running');
  assert.equal(controller.getSnapshot().error, null);
  controller.dispose();
});

test('recovery ignores mismatched request IDs and late replies after disposal', async () => {
  const late = deferred<PickResponse>(); let queries = 0;
  const { controller, clock } = setup({
    open: async () => { throw new Error('reply lost'); },
    openResult: async () => ++queries === 1
      ? { requestId: '999', pending: false, ...response(terminal()), fileName: 'wrong.flv', canceled: false }
      : late.promise,
  });
  await controller.open(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().error, 'invalid_response');
  assert.equal(clock.tasks.size, 0);
  controller.retry(); clock.tick(); controller.dispose();
  late.resolve({ requestId: '1', pending: false, ...response(terminal()), fileName: 'late.flv', canceled: false });
  await settle();
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(clock.tasks.size, 0);
});

test('recovered picker errors release admission and preserve the actual error', async () => {
  const { controller, clock } = setup({
    open: async () => { throw new Error('reply lost'); },
    openResult: async requestId => ({ requestId, pending: false, apiVersion: 1, error: { code: 'dialog_failed', message: 'Failed.' }, session: null, canceled: false, fileName: '' }),
  });
  await controller.open(); clock.tick(); await settle();
  assert.equal(controller.getSnapshot().operation, '');
  assert.equal(controller.getSnapshot().error, 'dialog_failed');
  assert.equal(clock.tasks.size, 0);
});

test('an unsettled open promise times out into confirmation and its late reply cannot overwrite a newer file', async () => {
  const lost = deferred<FixturePick>(); let opens = 0;
  const { controller, clock } = setup({
    open: async () => ++opens === 1 ? lost.promise : { ...response(terminal({ sessionId: 'scan-2' })), fileName: 'new.flv', canceled: false },
    openResult: async requestId => ({ requestId, pending: false, ...response(terminal()), fileName: 'recovered.flv', canceled: false }),
    tags: async request => tagPage(request.sessionId),
  });
  const opening = controller.open();
  clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle(); await opening;
  assert.equal(controller.getSnapshot().operation, 'recovering');
  assert.equal(controller.getSnapshot().error, null);
  clock.tick(); await settle();
  assert.equal(controller.getSnapshot().fileName, 'recovered.flv');
  await controller.close(); await controller.open(); await settle();
  lost.resolve({ ...response(terminal()), fileName: 'late.flv', canceled: false });
  await settle();
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
  assert.equal(controller.getSnapshot().fileName, 'new.flv');
  assert.equal(opens, 2);
  assert.equal(clock.tasks.size, 0);
});

test('an unsettled confirmation times out, retries the same request, and ignores its late reply after a new open', async () => {
  const lost = deferred<PickResponse>(); let opens = 0;
  const queriedIDs: string[] = [];
  const { controller, clock } = setup({
    open: async () => {
      if (++opens === 1) throw new Error('open reply lost');
      return { ...response(terminal({ sessionId: 'scan-2' })), fileName: 'new.flv', canceled: false };
    },
    openResult: async requestId => {
      queriedIDs.push(requestId);
      return queriedIDs.length === 1 ? lost.promise
        : { requestId, pending: false, ...response(terminal()), fileName: 'recovered.flv', canceled: false };
    },
    tags: async request => tagPage(request.sessionId),
  });
  await controller.open(); clock.tick(); await settle();
  clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().operation, 'recovering');
  assert.equal(controller.getSnapshot().session, null);
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  assert.equal(clock.tasks.size, 0);
  await controller.open();
  assert.equal(opens, 1, 'an uncertain open must retain admission');
  controller.retry(); controller.retry(); clock.tick(); await settle();
  assert.deepEqual(queriedIDs, ['1', '1']);
  assert.equal(controller.getSnapshot().fileName, 'recovered.flv');
  assert.equal(controller.getSnapshot().error, null);
  assert.equal(controller.getSnapshot().page?.tab, 'tags');
  await controller.close(); await controller.open(); await settle();
  const beforeLateReply = controller.getSnapshot();
  lost.resolve({ requestId: '1', pending: false, ...response(terminal()), fileName: 'late.flv', canceled: false });
  await settle();
  assert.equal(controller.getSnapshot(), beforeLateReply);
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
  assert.equal(controller.getSnapshot().fileName, 'new.flv');
  assert.equal(opens, 2);
  assert.equal(clock.tasks.size, 0);
});

test('disposal cancels both open and confirmation deadlines without accepting late replies', async () => {
  for (const recovering of [false, true]) {
    const late = deferred<PickResponse>();
    const { controller, clock } = setup({
      open: async () => {
        if (recovering) throw new Error('open reply lost');
        return late.promise;
      },
      openResult: () => late.promise,
    });
    const opening = controller.open();
    if (recovering) { await opening; clock.tick(); await settle(); }
    const before = controller.getSnapshot();
    assert.equal(clock.tasks.size, 1);
    controller.dispose();
    assert.equal(clock.tasks.size, 0, 'dispose must remove the active reply deadline');
    await opening;
    late.resolve({ requestId: '1', pending: false, ...response(terminal()), fileName: 'late.flv', canceled: false });
    await settle();
    assert.equal(controller.getSnapshot(), before);
    assert.equal(clock.tasks.size, 0);
  }
});

for (const command of ['cancel', 'close'] as const) {
  for (const delivered of [false, true]) {
    test(`an unsettled ${command} reply confirms whether the command ${delivered ? 'executed' : 'was not delivered'} without repeating it`, async () => {
      const late = deferred<SessionResponse>();
      let commands = 0, polls = 0, opens = 0;
      const initial = command === 'close' ? terminal() : snapshot();
      const confirmed = !delivered ? initial : command === 'close'
        ? terminal({ closed: true })
        : terminal({ state: 'canceled', scan: { complete: false, hasErrors: false } });
      const { controller, clock } = setup({
        open: async () => ({ ...response(++opens === 1 ? initial : snapshot({ sessionId: `scan-${opens}` })), fileName: `file-${opens}.flv`, canceled: false }),
        [command]: () => { commands++; return late.promise; },
        status: async () => { polls++; return response(confirmed); },
      });
      await controller.open();
      const commandWait = controller[command]();
      clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle();
      assert.equal(controller.getSnapshot().error, 'transport_failed');
      await commandWait;
      await controller[command]();
      assert.equal(commands, 1, 'uncertain commands must be confirmed before accepting another command');
      clock.tick(); await settle();
      assert.equal(polls, 1); assert.equal(commands, 1);
      assert.equal(controller.getSnapshot().error, null);
      assert.equal(controller.getSnapshot().operation, '');
      assert.equal(controller.getSnapshot().session?.state ?? null, command === 'close' && delivered ? null : confirmed.state);
      if (command === 'cancel' && delivered) assert.equal(controller.getSnapshot().page?.tab, 'tags');
      if (delivered) {
        if (command === 'cancel') await controller.close();
        await controller.open();
        assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
      }
      const before = controller.getSnapshot();
      late.resolve(response(terminal({ closed: command === 'close', state: 'canceled' })));
      await settle();
      assert.equal(controller.getSnapshot(), before, 'late command replies are obsolete after confirmation');
      controller.dispose();
    });
  }
}

test('an unsettled status query times out, coalesces retries, and cannot overwrite a new session', async () => {
  const late = deferred<SessionResponse>(); let polls = 0, opens = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(snapshot({ sessionId: `scan-${++opens}` })), fileName: `file-${opens}.flv`, canceled: false }),
    status: async id => ++polls === 1 ? late.promise : response(terminal({ sessionId: id })),
    tags: async request => tagPage(request.sessionId),
  });
  await controller.open(); clock.tick(); await settle();
  clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  assert.equal(clock.tasks.size, 0);
  controller.retry(); controller.retry(); clock.tick(); await settle();
  assert.equal(polls, 2); assert.equal(controller.getSnapshot().session?.terminal, true);
  assert.equal(controller.getSnapshot().error, null);
  await controller.close(); await controller.open();
  const before = controller.getSnapshot();
  late.reject(new Error('obsolete status failure'));
  await settle();
  assert.equal(controller.getSnapshot(), before);
  assert.equal(controller.getSnapshot().session?.sessionId, 'scan-2');
  controller.dispose();
});

for (const tab of ['tags', 'diagnostics'] as const) {
  test(`an unsettled ${tab} page times out and retries after one status confirmation`, async () => {
    const late = deferred<never>(); let pages = 0, polls = 0;
    const { controller, clock } = setup({
      open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
      status: async () => { polls++; return response(terminal()); },
      [tab]: () => ++pages === 1 ? late.promise : Promise.resolve(tab === 'tags' ? tagPage() : diagnosticPage()),
    });
    await controller.open(); await settle();
    if (tab === 'diagnostics') { controller.select(tab); await settle(); }
    clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle();
    assert.equal(controller.getSnapshot().error, 'transport_failed');
    assert.equal(controller.getSnapshot().loading, false);
    assert.equal(controller.getSnapshot().page, null);
    assert.equal(clock.tasks.size, 0);
    controller.retry(); controller.retry();
    assert.equal(pages, 1);
    clock.tick(); await settle();
    assert.equal(polls, 1); assert.equal(pages, 2);
    assert.equal(controller.getSnapshot().page?.tab, tab);
    assert.equal(controller.getSnapshot().error, null);
    const before = controller.getSnapshot();
    late.reject(new Error('obsolete page failure')); await settle();
    assert.equal(controller.getSnapshot(), before);
    controller.dispose();
  });
}

test('closing releases an unsettled page so a new file can load immediately', async () => {
  const old = deferred<PageResponse<TagPage>>(); let opens = 0, pages = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal({ sessionId: `scan-${++opens}` })), fileName: `file-${opens}.flv`, canceled: false }),
    tags: async request => ++pages === 1 ? old.promise : tagPage(request.sessionId),
  });
  await controller.open(); await controller.close(); await controller.open(); await settle();
  assert.equal(pages, 2);
  assert.equal(controller.getSnapshot().page?.data.sessionId, 'scan-2');
  assert.equal(controller.getSnapshot().loading, false);
  assert.equal(clock.tasks.size, 0);
  const before = controller.getSnapshot();
  old.resolve(tagPage()); await settle();
  assert.equal(controller.getSnapshot(), before);
});

test('a stale page timeout serves the latest selection without adding an obsolete error', async () => {
  const old = deferred<PageResponse<TagPage>>(); let pages = 0;
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    tags: async request => ++pages === 1 ? old.promise : tagPage(request.sessionId, request.start),
  });
  await controller.open(); controller.select('tags', '100'); controller.select('tags', '200');
  clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle();
  assert.equal(pages, 2);
  assert.equal(controller.getSnapshot().page?.data.bounds.start, '200');
  assert.equal(controller.getSnapshot().error, null);
  old.resolve(tagPage()); await settle();
  assert.equal(controller.getSnapshot().page?.data.bounds.start, '200');
});

test('status and page deadlines are independent and disposal clears every pending wait', async () => {
  const status = deferred<SessionResponse>(), page = deferred<PageResponse<TagPage>>();
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    status: () => status.promise,
    tags: () => page.promise,
  });
  await controller.open(); controller.retry(); clock.tick(); await settle();
  assert.equal(clock.tasks.size, 2, 'the status and page waits each require their own deadline');
  const before = controller.getSnapshot();
  controller.dispose();
  assert.equal(clock.tasks.size, 0);
  await settle();
  status.resolve(response(terminal())); page.resolve(tagPage()); await settle();
  assert.equal(controller.getSnapshot(), before);
});

test('a completed status query leaves the concurrent page deadline armed', async () => {
  const status = deferred<SessionResponse>(), page = deferred<PageResponse<TagPage>>();
  const { controller, clock } = setup({
    open: async () => ({ ...response(terminal()), fileName: 'sample.flv', canceled: false }),
    status: () => status.promise,
    tags: () => page.promise,
  });
  await controller.open(); controller.retry(); clock.tick(); await settle();
  assert.equal(clock.tasks.size, 2);
  status.resolve(response(terminal())); await settle();
  assert.equal(clock.tasks.size, 1);
  assert.equal(controller.getSnapshot().loading, true);
  clock.tick(OPEN_REPLY_TIMEOUT_MS); await settle();
  assert.equal(controller.getSnapshot().loading, false);
  assert.equal(controller.getSnapshot().error, 'transport_failed');
  assert.equal(clock.tasks.size, 0);
  controller.dispose();
});

for (const command of ['cancel', 'close'] as const) {
  test(`disposing an unsettled ${command} releases the local command wait and its deadline`, async () => {
    const late = deferred<SessionResponse>();
    const { controller, clock } = setup({ [command]: () => late.promise });
    await controller.open(); const waiting = controller[command]();
    assert.equal(clock.tasks.size, 1);
    const before = controller.getSnapshot(); controller.dispose();
    assert.equal(clock.tasks.size, 0);
    await waiting;
    late.resolve(response(terminal({ closed: command === 'close' }))); await settle();
    assert.equal(controller.getSnapshot(), before);
  });
}
import { unexpectedSummary } from './summary-fixture.ts';
