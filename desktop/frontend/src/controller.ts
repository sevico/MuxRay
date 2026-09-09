import type { API, DiagnosticPage, PageResponse, PickResponse, Session, SessionResponse, TagPage } from './protocol.ts';
import { unsigned } from './numbers.ts';

export const PAGE_SIZE = 100;
export const POLL_MS = 200;
export const OPEN_REPLY_TIMEOUT_MS = 5000;
export type Tab = 'tags' | 'diagnostics';
export type VisiblePage = { tab: 'tags'; data: TagPage } | { tab: 'diagnostics'; data: DiagnosticPage };
export interface ViewState {
  fileName: string;
  session: Session | null;
  operation: '' | 'picking' | 'recovering' | 'canceling' | 'closing';
  error: string | null;
  tab: Tab;
  start: string;
  page: VisiblePage | null;
  loading: boolean;
}
export interface Scheduler { after(fn: () => void, ms: number): () => void }
const scheduler: Scheduler = {
  after(fn, ms) { const id = setTimeout(fn, ms); return () => clearTimeout(id); },
};

class RequestError extends Error {}
class OpenUnconfirmed extends Error {}
function check(response: { apiVersion: number; error: { code: string } | null }): void {
  if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
  if (response.error) throw new RequestError(response.error.code);
}
function sessionResult(response: SessionResponse, id?: string): Session {
  check(response);
  if (!response.session || (id !== undefined && response.session.sessionId !== id)) throw new RequestError('invalid_response');
  return response.session;
}
function pageResult<P extends { sessionId: string; bounds: { start: string } }>(response: PageResponse<P>, id: string, start: string): P {
  check(response);
  if (!response.page || response.page.sessionId !== id || response.page.bounds.start !== start) throw new RequestError('invalid_response');
  return response.page;
}
function errorCode(error: unknown): string {
  return error instanceof RequestError ? error.message : 'transport_failed';
}

// Framework-independent controller: one poll, one page, one user command at a
// time. Old replies cannot overwrite a close/cancel/new-session transition.
export class Controller {
  private api: API;
  private clock: Scheduler;
  private listeners = new Set<() => void>();
  private state: ViewState = { fileName: '', session: null, operation: '', error: null, tab: 'tags', start: '0', page: null, loading: false };
  private disposed = false;
  private statusVersion = 0;
  private pageVersion = 0;
  private polling = false;
  private paging = false;
  private pollFailed = false;
  // A command may reach the host even if its reply is lost. Reconcile its
  // outcome before reading pages, including for a previously terminal scan.
  private refreshStatus = false;
  private nextOpenID = 0n;
  private openingID: string | null = null;
  private stopTimer: (() => void) | null = null;
  private stopOpenReply: (() => void) | null = null;

  constructor(api: API, clock: Scheduler = scheduler) { this.api = api; this.clock = clock; }
  getSnapshot = (): ViewState => this.state;
  subscribe = (listener: () => void): (() => void) => {
    this.listeners.add(listener);
    return () => { this.listeners.delete(listener); };
  };
  private update(patch: Partial<ViewState>): void {
    if (this.disposed) return;
    this.state = { ...this.state, ...patch };
    for (const listener of this.listeners) listener();
  }
  private accept(session: Session): void {
    if (session.closed && session.terminal) {
      this.pageVersion++;
      this.refreshStatus = false;
      this.update({ session: null, fileName: '', operation: '', page: null, loading: false, start: '0' });
    } else {
      this.update({ session, operation: session.closed ? 'closing' : '' });
    }
  }
  async open(): Promise<void> {
    if (this.disposed || this.state.session || this.state.operation) return;
    if (this.nextOpenID === 18446744073709551615n) { this.update({ error: 'id_exhausted' }); return; }
    const requestId = (++this.nextOpenID).toString();
    this.openingID = requestId;
    this.pollFailed = false;
    this.update({ operation: 'picking', error: null });
    try {
      // Wails may never settle a call whose callback is lost. A timeout only
      // starts result confirmation; it never repeats or cancels the picker.
      const result = await this.awaitOpenReply(() => this.api.open(requestId));
      if (this.disposed) return;
      this.acceptOpen(result, requestId);
    } catch (error) {
      if (!(error instanceof OpenUnconfirmed)) this.update({ error: errorCode(error) });
    }
    finally {
      if (this.openingID) this.update({ operation: 'recovering' });
      this.schedule(); this.ensurePage();
    }
  }
  private async awaitOpenReply(call: () => Promise<PickResponse>): Promise<PickResponse> {
    let stopDeadline = () => {};
    let rejectWait: (error: OpenUnconfirmed) => void = () => {};
    const deadline = new Promise<never>((_, reject) => {
      rejectWait = reject;
      stopDeadline = this.clock.after(() => reject(new OpenUnconfirmed()), OPEN_REPLY_TIMEOUT_MS);
    });
    const stopWait = () => { stopDeadline(); rejectWait(new OpenUnconfirmed()); };
    this.stopOpenReply = stopWait;
    try {
      // Confirmation callbacks can also be lost. Racing each wait lets retry
      // make another query while preventing a late reply from changing state.
      return await Promise.race([call(), deadline]);
    } finally {
      stopDeadline();
      if (this.stopOpenReply === stopWait) this.stopOpenReply = null;
    }
  }
  private acceptOpen(result: PickResponse, requestId: string): void {
    if (result.apiVersion !== 1) throw new RequestError('incompatible_api');
    if (result.requestId !== requestId || typeof result.pending !== 'boolean' || typeof result.canceled !== 'boolean' || (result.session && (result.canceled || result.error))) throw new RequestError('invalid_response');
    if (result.pending) {
      if (result.session || result.error || result.canceled) throw new RequestError('invalid_response');
      this.update({ operation: 'recovering' });
      return;
    }
    // A completed error is authoritative; malformed/mismatched envelopes are
    // still uncertain and must not release admission to a second open.
    const session = result.error || result.canceled ? null : sessionResult(result);
    this.openingID = null;
    this.update({ operation: '', error: null });
    check(result);
    if (session) {
      this.statusVersion++;
      this.pageVersion++;
      this.pollFailed = false;
      this.refreshStatus = false;
      this.update({ fileName: result.fileName, tab: 'tags', start: '0', page: null });
      this.accept(session);
    }
  }
  async cancel(): Promise<void> { await this.command('canceling'); }
  async close(): Promise<void> { await this.command('closing'); }
  private async command(operation: 'canceling' | 'closing'): Promise<void> {
    const session = this.state.session;
    if (this.disposed || !session || this.state.operation || (operation === 'canceling' && session.terminal)) return;
    this.statusVersion++;
    this.stopTimer?.(); this.stopTimer = null;
    this.pollFailed = false;
    if (operation === 'closing') this.pageVersion++;
    this.update({ operation, error: null, ...(operation === 'closing' ? { page: null, loading: false } : {}) });
    try {
      const result = await (operation === 'closing' ? this.api.close(session.sessionId) : this.api.cancel(session.sessionId));
      if (!this.disposed && this.state.session?.sessionId === session.sessionId) {
        const current = sessionResult(result, session.sessionId);
        this.refreshStatus = false;
        this.accept(current);
      }
    } catch (error) {
      this.refreshStatus = true;
      this.update({ error: errorCode(error), operation: '' });
    }
    finally { this.schedule(); this.ensurePage(); }
  }
  private schedule(): void {
    if (this.disposed || this.polling || this.stopTimer || this.pollFailed) return;
    if (this.openingID) {
      if (this.state.operation === 'recovering') this.stopTimer = this.clock.after(() => { this.stopTimer = null; void this.recoverOpen(); }, POLL_MS);
      return;
    }
    if (!this.state.session || (this.state.session.terminal && !this.refreshStatus)) return;
    if (this.state.operation && !(this.state.operation === 'closing' && this.state.session.closed)) return;
    this.stopTimer = this.clock.after(() => { this.stopTimer = null; void this.poll(); }, POLL_MS);
  }
  private async recoverOpen(): Promise<void> {
    const requestId = this.openingID;
    if (this.disposed || this.polling || !requestId) return;
    this.polling = true;
    try {
      const result = await this.awaitOpenReply(() => this.api.openResult(requestId));
      if (!this.disposed && this.openingID === requestId) {
        this.acceptOpen(result, requestId);
        this.ensurePage();
      }
    } catch (error) {
      if (!this.disposed && this.openingID === requestId) this.pollFailed = true;
      this.update({ error: errorCode(error) });
    } finally { this.polling = false; this.schedule(); }
  }
  private async poll(): Promise<void> {
    const session = this.state.session;
    if (this.disposed || this.polling || !session || (session.terminal && !this.refreshStatus)) return;
    this.polling = true;
    const version = this.statusVersion;
    try {
      const result = await this.api.status(session.sessionId);
      if (!this.disposed && version === this.statusVersion && this.state.session?.sessionId === session.sessionId) {
        const current = sessionResult(result, session.sessionId);
        if (this.refreshStatus) {
          this.refreshStatus = false;
          this.update({ error: null });
        }
        this.accept(current);
        this.ensurePage();
      }
    } catch (error) {
      if (version === this.statusVersion) { this.pollFailed = true; this.update({ error: errorCode(error) }); }
    } finally { this.polling = false; this.schedule(); }
  }
  select(tab: Tab, start = '0'): void {
    try { unsigned(start); } catch { this.update({ error: 'invalid_argument' }); return; }
    this.pageVersion++;
    this.update({ tab, start, page: null, loading: false, error: this.openingID || this.refreshStatus || this.pollFailed ? this.state.error : null });
    this.ensurePage();
  }
  private ready(): boolean {
    const session = this.state.session;
    return !!session?.terminal && !session.closed && session.scan !== null && this.state.operation !== 'closing';
  }
  private ensurePage(): void {
    if (this.disposed || this.paging || this.refreshStatus || this.pollFailed || !this.ready() || this.state.page) return;
    void this.loadPage();
  }
  private async loadPage(): Promise<void> {
    const session = this.state.session!;
    const { tab, start } = this.state;
    const version = this.pageVersion;
    this.paging = true;
    this.update({ loading: true });
    try {
      const request = { sessionId: session.sessionId, start, count: PAGE_SIZE };
      const result: VisiblePage = tab === 'tags'
        ? { tab, data: pageResult(await this.api.tags(request), session.sessionId, start) }
        : { tab, data: pageResult(await this.api.diagnostics(request), session.sessionId, start) };
      if (!this.disposed && version === this.pageVersion && this.ready()) this.update({ page: result });
    } catch (error) {
      if (version === this.pageVersion) this.update({ error: errorCode(error) });
    } finally {
      this.paging = false;
      if (version === this.pageVersion) this.update({ loading: false });
      else this.ensurePage(); // Coalesce rapid selections into one latest page.
    }
  }
  retry(): void {
    if (this.disposed) return;
    this.update({ error: null });
    this.pollFailed = false;
    if (this.state.session) this.refreshStatus = true;
    this.schedule();
    this.ensurePage();
  }
  dispose(): void {
    this.disposed = true;
    this.statusVersion++; this.pageVersion++;
    this.stopTimer?.(); this.stopTimer = null;
    this.stopOpenReply?.(); this.stopOpenReply = null;
    this.listeners.clear();
    // Resource ownership belongs to the host, not an unreliable unload event.
  }
}
