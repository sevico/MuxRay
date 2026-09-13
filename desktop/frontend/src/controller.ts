import type { API, Detail, DetailRequest, DetailResponse, DiagnosticPage, HexRequest, HexResponse, HexWindow, PageResponse, PickResponse, Session, SessionResponse, Tag, TagPage } from './protocol.ts';
import { unsigned } from './numbers.ts';
import { HEX_WINDOW_BYTES, validateHexWindow, validateSelection } from './hex.ts';
import type { HexSelection } from './hex.ts';
import type { DetailQuery, SummaryKind, SummaryOverview, SummaryPage, SummaryPageRequest, SummaryResponse, SummaryEvent } from './protocol.ts';
import { summaryKind, validateSummaryPage, validateSummaryResponse } from './summary.ts';

export const PAGE_SIZE = 100;
export const POLL_MS = 200;
export const REPLY_TIMEOUT_MS = 5000;
export const OPEN_REPLY_TIMEOUT_MS = REPLY_TIMEOUT_MS;
export const DETAIL_POLL_LIMIT = 25;
export const SUMMARY_POLL_LIMIT = 600;
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
  selectedTag: Tag | null;
  detail: Detail | null;
  detailState: '' | 'loading' | 'pending' | 'recovering' | 'ready' | 'failed';
  detailError: string | null;
  detailRetry: '' | 'confirm' | 'reload';
  hex: HexWindow | null;
  hexSelection: HexSelection | null;
  hexState: '' | 'loading' | 'pending' | 'recovering' | 'ready' | 'failed';
  hexError: string | null;
  hexRetry: '' | 'confirm' | 'reload';
  tagJumpError: string | null;
  sourceChanged: boolean;
  summary: SummaryOverview | null;
  summaryState: '' | 'loading' | 'pending' | 'canceling' | 'recovering' | 'ready' | 'failed';
  summaryRunning: boolean;
  summaryCancelRequested: boolean;
  summaryProcessed: string;
  summaryTotal: string;
  summaryError: string | null;
  summaryRetry: '' | 'confirm' | 'reload';
  summaryKind: SummaryKind;
  summaryStart: string;
  summaryPage: SummaryPage | null;
  summaryPageLoading: boolean;
  summaryPageError: string | null;
  summaryPrevious: string | null;
}
export interface Scheduler { after(fn: () => void, ms: number): () => void }
const scheduler: Scheduler = {
  after(fn, ms) { const id = setTimeout(fn, ms); return () => clearTimeout(id); },
};

class RequestError extends Error {}
class ReplyUnconfirmed extends Error {}
type ReplySlot = 'open' | 'command' | 'status' | 'page' | 'detail' | 'hex' | 'summary' | 'summaryPage';
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
function detailResult(response: DetailResponse, request: DetailRequest): DetailResponse {
  if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
  if (response.sessionId !== request.sessionId || response.requestId !== request.requestId ||
      (response.tagId !== request.tagId && !(response.state === 'failed' && response.error && response.tagId === ''))) throw new RequestError('invalid_response');
  if (response.state === 'failed') {
    if (!response.error || response.detail) throw new RequestError('invalid_response');
  } else if (response.state === 'pending') {
    if (response.error || response.detail) throw new RequestError('invalid_response');
  } else if (response.state === 'ready') {
    if (response.error || !response.detail) throw new RequestError('invalid_response');
  } else throw new RequestError('invalid_response');
  return response;
}

function hexResult(response: HexResponse, request: HexRequest): HexResponse {
  if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
  const initialFailure = response.state === 'failed' && response.error && response.count === 0 && (response.offset === '' || response.offset === request.offset);
  if (response.sessionId !== request.sessionId || response.requestId !== request.requestId ||
      (!initialFailure && (response.offset !== request.offset || response.count !== request.count))) throw new RequestError('invalid_response');
  if (response.state === 'failed') {
    if (!response.error || response.window) throw new RequestError('invalid_response');
  } else if (response.state === 'pending') {
    if (response.error || response.window) throw new RequestError('invalid_response');
  } else if (response.state === 'ready') {
    if (response.error || !response.window || response.window.offset !== request.offset) throw new RequestError('invalid_response');
    try { validateHexWindow(response.window, request.count); } catch { throw new RequestError('invalid_response'); }
  } else throw new RequestError('invalid_response');
  return response;
}

// Framework-independent controller: one poll, one page, one user command at a
// time. Old replies cannot overwrite a close/cancel/new-session transition.
export class Controller {
  private api: API;
  private clock: Scheduler;
  private listeners = new Set<() => void>();
  private state: ViewState = { fileName: '', session: null, operation: '', error: null, tab: 'tags', start: '0', page: null, loading: false, selectedTag: null, detail: null, detailState: '', detailError: null, detailRetry: '', hex: null, hexSelection: null, hexState: '', hexError: null, hexRetry: '', tagJumpError: null, sourceChanged: false, summary: null, summaryState: '', summaryRunning: false, summaryCancelRequested: false, summaryProcessed: '0', summaryTotal: '0', summaryError: null, summaryRetry: '', summaryKind: 'keyframes', summaryStart: '0', summaryPage: null, summaryPageLoading: false, summaryPageError: null, summaryPrevious: null };
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
  private replyStops = new Map<ReplySlot, () => void>();
  private nextReadID = 0n;
  private detailRequest: DetailRequest | null = null;
  private detailPhase: 'start' | 'get' = 'start';
  private detailBusy = false;
  private detailPaused = false;
  private detailPolls = 0;
  private stopDetailTimer: (() => void) | null = null;
  private jumpTagId: string | null = null;
  private hexRequest: HexRequest | null = null;
  private hexPhase: 'start' | 'get' = 'start';
  private hexBusy = false;
  private hexPaused = false;
  private hexPolls = 0;
  private stopHexTimer: (() => void) | null = null;
  private summaryRequest: DetailQuery | null = null;
  private summaryPhase: 'start' | 'get' | 'cancel' = 'start';
  private summaryBusy = false;
  private summaryPaused = false;
  private summaryPolls = 0;
  private stopSummaryTimer: (() => void) | null = null;
  private summaryPageVersion = 0;
  private summaryPageBusy = false;
  private summaryHistory: string[] = [];

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
      this.clearSummary();
      this.clearDetail();
      this.pageVersion++;
      this.replyStops.get('page')?.();
      this.refreshStatus = false;
      this.jumpTagId = null;
      this.update({ session: null, fileName: '', operation: '', page: null, loading: false, start: '0', sourceChanged: false, error: null });
    } else {
      if (session.closed) { this.clearSummary(); this.clearDetail(); }
      this.update({ session, operation: session.closed ? 'closing' : '' });
      this.ensureDetail();
      this.ensureHex();
      this.ensureSummary();
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
      const result = await this.awaitReply('open', () => this.api.open(requestId));
      if (this.disposed) return;
      this.acceptOpen(result, requestId);
    } catch (error) {
      if (!(error instanceof ReplyUnconfirmed)) this.update({ error: errorCode(error) });
    }
    finally {
      if (this.openingID) this.update({ operation: 'recovering' });
      this.schedule(); this.ensurePage();
    }
  }
  private async awaitReply<T>(slot: ReplySlot, call: () => Promise<T>): Promise<T> {
    let stopDeadline = () => {};
    let rejectWait: (error: ReplyUnconfirmed) => void = () => {};
    const deadline = new Promise<never>((_, reject) => {
      rejectWait = reject;
      stopDeadline = this.clock.after(() => reject(new ReplyUnconfirmed()), REPLY_TIMEOUT_MS);
    });
    const stopWait = () => { stopDeadline(); rejectWait(new ReplyUnconfirmed()); };
    this.replyStops.set(slot, stopWait);
    try {
      // A lost callback must not retain a local request slot indefinitely.
      // The host call may still finish; only its local wait is abandoned.
      return await Promise.race([call(), deadline]);
    } finally {
      stopDeadline();
      if (this.replyStops.get(slot) === stopWait) this.replyStops.delete(slot);
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
      this.clearSummary();
      this.clearDetail();
      this.nextReadID = 0n;
      this.statusVersion++;
      this.pageVersion++;
      this.pollFailed = false;
      this.refreshStatus = false;
      this.update({ fileName: result.fileName, tab: 'tags', start: '0', page: null, sourceChanged: false });
      this.accept(session);
    }
  }
  async cancel(): Promise<void> { await this.command('canceling'); }
  async close(): Promise<void> { await this.command('closing'); }
  private async command(operation: 'canceling' | 'closing'): Promise<void> {
    const session = this.state.session;
    if (this.disposed || !session || this.state.operation || (operation === 'canceling' && session.terminal)) return;
    const version = ++this.statusVersion;
    this.stopTimer?.(); this.stopTimer = null;
    this.replyStops.get('status')?.();
    this.pollFailed = false;
    this.refreshStatus = false;
    if (operation === 'closing') {
      this.clearSummary();
      this.clearDetail();
      this.pageVersion++;
      this.replyStops.get('page')?.();
    }
    this.update({ operation, error: null, ...(operation === 'closing' ? { page: null, loading: false } : {}) });
    try {
      const result = await this.awaitReply('command', () => operation === 'closing' ? this.api.close(session.sessionId) : this.api.cancel(session.sessionId));
      if (!this.disposed && version === this.statusVersion && this.state.session?.sessionId === session.sessionId) {
        const current = sessionResult(result, session.sessionId);
        this.refreshStatus = false;
        this.accept(current);
      }
    } catch (error) {
      if (!this.disposed && version === this.statusVersion && this.state.session?.sessionId === session.sessionId) {
        this.refreshStatus = true;
        // Keep commands disabled until a query confirms what actually happened.
        // Neither a timeout nor retry is permission to repeat a host command.
        this.update({ error: errorCode(error) });
      }
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
    if (this.state.operation && !this.refreshStatus && !(this.state.operation === 'closing' && this.state.session.closed)) return;
    this.stopTimer = this.clock.after(() => { this.stopTimer = null; void this.poll(); }, POLL_MS);
  }
  private async recoverOpen(): Promise<void> {
    const requestId = this.openingID;
    if (this.disposed || this.polling || !requestId) return;
    this.polling = true;
    try {
      const result = await this.awaitReply('open', () => this.api.openResult(requestId));
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
      const result = await this.awaitReply('status', () => this.api.status(session.sessionId));
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
      if (!this.disposed && version === this.statusVersion && this.state.session?.sessionId === session.sessionId) { this.pollFailed = true; this.update({ error: errorCode(error) }); }
    } finally { this.polling = false; this.schedule(); }
  }
  select(tab: Tab, start = '0'): void {
    if (this.state.summaryRunning) return;
    try { unsigned(start); } catch { this.update({ error: 'invalid_argument' }); return; }
    this.pageVersion++;
    this.jumpTagId = null;
    this.clearDetail();
    this.update({ tab, start, page: null, loading: false, error: this.openingID || this.refreshStatus || this.pollFailed ? this.state.error : null });
    this.ensurePage();
  }
  private ready(): boolean {
    const session = this.state.session;
    return !!session?.terminal && !session.closed && session.scan !== null && this.state.operation !== 'closing' && !this.state.sourceChanged;
  }
  private ensurePage(): void {
    if (this.disposed || this.paging || this.refreshStatus || this.pollFailed || !this.ready() || this.state.page || this.state.summaryRunning) return;
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
        ? { tab, data: pageResult(await this.awaitReply('page', () => this.api.tags(request)), session.sessionId, start) }
        : { tab, data: pageResult(await this.awaitReply('page', () => this.api.diagnostics(request)), session.sessionId, start) };
      if (!this.disposed && version === this.pageVersion && this.state.session?.sessionId === session.sessionId && this.ready()) {
        this.update({ page: result });
        if (this.jumpTagId && result.tab === 'tags') {
          const id = this.jumpTagId; this.jumpTagId = null;
          this.selectTag(id);
        }
      }
    } catch (error) {
      if (!this.disposed && version === this.pageVersion && this.state.session?.sessionId === session.sessionId) this.update({ error: errorCode(error) });
    } finally {
      this.paging = false;
      if (version === this.pageVersion) this.update({ loading: false });
      else this.ensurePage(); // Coalesce rapid selections into one latest page.
    }
  }
  retry(): void {
    if (this.disposed || this.replyStops.has('command') || this.state.operation === 'picking' || this.state.sourceChanged) return;
    this.update({ error: null });
    this.pollFailed = false;
    if (this.state.session) this.refreshStatus = true;
    this.schedule();
    this.ensurePage();
  }
  selectTag(tagId: string): void {
    if (this.disposed || this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed || this.state.page?.tab !== 'tags') return;
    const tag = this.state.page.data.tags.find(item => item.id === tagId);
    if (!tag || this.state.selectedTag?.id === tagId) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ detailError: 'id_exhausted' }); return; }
    this.clearHex();
    this.stopDetailTimer?.(); this.stopDetailTimer = null;
    this.detailRequest = { sessionId: this.state.session!.sessionId, requestId: (++this.nextReadID).toString(), tagId };
    this.detailPhase = 'start'; this.detailPaused = false; this.detailPolls = 0;
    this.update({ selectedTag: tag, detail: null, detailState: 'loading', detailError: null, detailRetry: '' });
    // Keep one IPC wait. If an older reply is in flight, coalesce selections
    // into this latest intent; its finite deadline releases the local slot.
    this.ensureDetail();
  }
  private clearDetail(): void {
    this.clearHex();
    this.detailRequest = null;
    this.detailPaused = false;
    this.stopDetailTimer?.(); this.stopDetailTimer = null;
    this.replyStops.get('detail')?.();
    this.update({ selectedTag: null, detail: null, detailState: '', detailError: null, detailRetry: '' });
  }
  private ensureDetail(): void {
    if (this.disposed || this.state.summaryRunning || this.detailBusy || this.detailPaused || this.stopDetailTimer || !this.detailRequest || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.detailPhase === 'start') void this.loadDetail();
    else this.stopDetailTimer = this.clock.after(() => { this.stopDetailTimer = null; void this.loadDetail(); }, POLL_MS);
  }
  private currentDetail(request: DetailRequest): boolean {
    return !this.disposed && this.detailRequest === request && this.state.session?.sessionId === request.sessionId && this.ready();
  }
  private async loadDetail(): Promise<void> {
    const request = this.detailRequest;
    if (!request || this.detailBusy || this.disposed || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const phase = this.detailPhase;
    this.detailBusy = true;
    try {
      const raw = await this.awaitReply('detail', () => phase === 'start' ? this.api.startDetail(request) : this.api.detailResult({ sessionId: request.sessionId, requestId: request.requestId }));
      if (!this.currentDetail(request)) return;
      const response = detailResult(raw, request);
      if (response.state === 'ready') {
        this.detailPaused = true;
        this.update({ detail: response.detail, detailState: 'ready', detailError: null, detailRetry: '' });
      } else if (response.state === 'failed') {
        this.detailPaused = true;
        const code = response.error!.code;
        if (code === 'source_changed') { this.invalidateSource(); return; }
        this.update({ detailState: 'failed', detailError: code, detailRetry: code === 'detail_not_found' ? 'confirm' : ['detail_failed', 'deadline_exceeded', 'canceled'].includes(code) ? 'reload' : '' });
      } else {
        this.detailPhase = 'get';
        if (phase === 'get' && ++this.detailPolls >= DETAIL_POLL_LIMIT) {
          this.detailPaused = true;
          this.update({ detailState: 'failed', detailError: 'deadline_exceeded', detailRetry: 'confirm' });
        } else this.update({ detailState: 'pending', detailError: null });
      }
    } catch (error) {
      if (!this.currentDetail(request)) return;
      if (phase === 'start' && !(error instanceof RequestError)) {
        // A lost Start reply is uncertain: query that identity, never create
        // another job automatically. Failed confirmation waits for the user.
        this.detailPhase = 'get';
        this.update({ detailState: 'recovering', detailError: null });
      } else {
        this.detailPaused = true;
        this.update({ detailState: 'failed', detailError: errorCode(error), detailRetry: error instanceof RequestError ? '' : 'confirm' });
      }
    } finally {
      this.detailBusy = false;
      this.ensureDetail();
    }
  }
  retryDetail(): void {
    if (this.disposed || this.state.summaryRunning || this.detailBusy || !this.detailPaused || !this.detailRequest || !this.state.detailRetry || !this.ready() || this.refreshStatus || this.pollFailed) return;
    // Unconfirmed work keeps its identity. An explicitly failed read is cached
    // by the host, so only a deliberate new read advances the request number.
    if (this.state.detailRetry === 'reload') {
      if (this.nextReadID === 18446744073709551615n) { this.update({ detailError: 'id_exhausted', detailRetry: '' }); return; }
      this.clearHex();
      this.detailRequest = { ...this.detailRequest, requestId: (++this.nextReadID).toString() };
    }
    this.detailPhase = 'start'; this.detailPaused = false; this.detailPolls = 0;
    this.update({ detailState: 'loading', detailError: null, detailRetry: '' });
    this.ensureDetail();
  }
  jumpTag(text: string): void {
    if (this.disposed || this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    try {
      const id = unsigned(text), total = unsigned(this.state.session!.progress!.tagCount);
      if (id >= total) throw new Error('Out of range.');
      this.select('tags', (id / BigInt(PAGE_SIZE) * BigInt(PAGE_SIZE)).toString());
      this.jumpTagId = id.toString();
      this.update({ tagJumpError: null });
    } catch { this.update({ tagJumpError: 'tag_not_found' }); }
  }
  showHex(selection: HexSelection): void {
    if (this.disposed || this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    try {
      validateSelection(selection);
      if (unsigned(selection.offset) > unsigned(this.state.session!.progress!.sourceSize)) throw new Error('Out of range.');
    } catch { this.update({ hexError: 'invalid_argument' }); return; }
    this.requestHex(selection.offset, selection);
  }
  jumpHex(offset: string): void { this.showHex({ offset, bitOffset: 0, bitLength: null, label: '指定位置' }); }
  pageHex(direction: -1 | 1): void {
    const window = this.state.hex;
    if (!window || this.disposed || !this.ready()) return;
    const next = unsigned(window.offset) + BigInt(direction * HEX_WINDOW_BYTES), size = unsigned(window.sourceSize);
    this.requestHex((next < 0n ? 0n : next > size ? size : next).toString(), this.state.hexSelection);
  }
  private pauseDetailForHex(): void {
    if (!this.detailRequest) return;
    // Change local identity to reject older replies. An already completed field
    // table remains visible alongside its bytes; unfinished detail is retryable.
    this.detailRequest = { ...this.detailRequest };
    this.detailPaused = true;
    this.stopDetailTimer?.(); this.stopDetailTimer = null;
    this.replyStops.get('detail')?.();
    if (this.state.detailState !== 'ready') this.update({ detailState: 'failed', detailError: 'detail_superseded', detailRetry: 'reload' });
  }
  private requestHex(offset: string, selection: HexSelection | null): void {
    if (this.disposed || this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ hexError: 'id_exhausted' }); return; }
    this.pauseDetailForHex();
    this.stopHexTimer?.(); this.stopHexTimer = null;
    this.hexRequest = { sessionId: this.state.session!.sessionId, requestId: (++this.nextReadID).toString(), offset, count: HEX_WINDOW_BYTES };
    this.hexPhase = 'start'; this.hexPaused = false; this.hexPolls = 0;
    this.update({ hex: null, hexSelection: selection, hexState: 'loading', hexError: null, hexRetry: '' });
    this.ensureHex();
  }
  private clearHex(): void {
    this.hexRequest = null; this.hexPaused = false;
    this.stopHexTimer?.(); this.stopHexTimer = null;
    this.replyStops.get('hex')?.();
    this.update({ hex: null, hexSelection: null, hexState: '', hexError: null, hexRetry: '', tagJumpError: null });
  }
  private invalidateSource(): void {
    this.clearSummary();
    this.clearDetail();
    this.pageVersion++; this.jumpTagId = null;
    this.replyStops.get('page')?.();
    this.update({ sourceChanged: true, error: 'source_changed', page: null, loading: false });
  }
  private ensureHex(): void {
    if (this.disposed || this.state.summaryRunning || this.hexBusy || this.hexPaused || this.stopHexTimer || !this.hexRequest || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.hexPhase === 'start') void this.loadHex();
    else this.stopHexTimer = this.clock.after(() => { this.stopHexTimer = null; void this.loadHex(); }, POLL_MS);
  }
  private currentHex(request: HexRequest): boolean {
    return !this.disposed && this.hexRequest === request && this.state.session?.sessionId === request.sessionId && this.ready();
  }
  private async loadHex(): Promise<void> {
    const request = this.hexRequest;
    if (!request || this.hexBusy || this.disposed || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const phase = this.hexPhase;
    this.hexBusy = true;
    try {
      const raw = await this.awaitReply('hex', () => phase === 'start' ? this.api.startHex(request) : this.api.hexResult({ sessionId: request.sessionId, requestId: request.requestId }));
      if (!this.currentHex(request)) return;
      const response = hexResult(raw, request);
      if (response.state === 'ready') {
        this.hexPaused = true;
        this.update({ hex: response.window, hexState: 'ready', hexError: null, hexRetry: '' });
      } else if (response.state === 'failed') {
        this.hexPaused = true;
        const code = response.error!.code;
        if (code === 'source_changed') { this.invalidateSource(); return; }
        this.update({ hexState: 'failed', hexError: code, hexRetry: code === 'hex_not_found' ? 'confirm' : ['hex_failed', 'deadline_exceeded', 'canceled'].includes(code) ? 'reload' : '' });
      } else {
        this.hexPhase = 'get';
        if (phase === 'get' && ++this.hexPolls >= DETAIL_POLL_LIMIT) {
          this.hexPaused = true;
          this.update({ hexState: 'failed', hexError: 'deadline_exceeded', hexRetry: 'confirm' });
        } else this.update({ hexState: 'pending', hexError: null });
      }
    } catch (error) {
      if (!this.currentHex(request)) return;
      if (phase === 'start' && !(error instanceof RequestError)) {
        this.hexPhase = 'get'; this.update({ hexState: 'recovering', hexError: null });
      } else {
        this.hexPaused = true;
        this.update({ hexState: 'failed', hexError: errorCode(error), hexRetry: error instanceof RequestError ? '' : 'confirm' });
      }
    } finally { this.hexBusy = false; this.ensureHex(); }
  }
  retryHex(): void {
    if (this.disposed || this.state.summaryRunning || this.hexBusy || !this.hexPaused || !this.hexRequest || !this.state.hexRetry || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.state.hexRetry === 'reload') {
      if (this.nextReadID === 18446744073709551615n) { this.update({ hexError: 'id_exhausted' }); return; }
      this.hexRequest = { ...this.hexRequest, requestId: (++this.nextReadID).toString() };
    }
    this.hexPhase = 'start'; this.hexPaused = false; this.hexPolls = 0;
    this.update({ hexState: 'loading', hexError: null, hexRetry: '' });
    this.ensureHex();
  }
  startSummary(): void {
    if (this.disposed || this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ summaryError: 'id_exhausted' }); return; }
    this.clearSummary();
    this.clearDetail();
    this.pageVersion++; this.jumpTagId = null;
    this.replyStops.get('page')?.();
    this.summaryRequest = { sessionId: this.state.session!.sessionId, requestId: (++this.nextReadID).toString() };
    this.summaryPhase = 'start'; this.summaryPaused = false; this.summaryPolls = 0;
    this.update({ summaryState: 'loading', summaryRunning: true, summaryTotal: this.state.session!.progress!.tagCount, loading: false });
    this.ensureSummary();
  }
  cancelSummary(): void {
    if (this.disposed || !this.summaryRequest || !this.state.summaryRunning || this.state.summaryCancelRequested || !this.ready()) return;
    // Fence any Start/Get reply before sending the cancellation. A local timeout
    // never implies that the host released the read worker.
    this.summaryRequest = { ...this.summaryRequest };
    this.stopSummaryTimer?.(); this.stopSummaryTimer = null;
    this.replyStops.get('summary')?.();
    this.summaryPhase = 'cancel'; this.summaryPaused = false; this.summaryPolls = 0;
    this.update({ summaryState: 'canceling', summaryCancelRequested: true, summaryError: null, summaryRetry: '' });
    this.ensureSummary();
  }
  retrySummary(): void {
    if (this.disposed || this.summaryBusy || !this.summaryPaused || !this.summaryRequest || !this.state.summaryRetry || !this.ready()) return;
    if (this.state.summaryRetry === 'reload') { this.startSummary(); return; }
    this.summaryRequest = { ...this.summaryRequest };
    this.summaryPhase = this.state.summaryCancelRequested ? 'cancel' : this.state.summaryError === 'summary_not_found' ? 'start' : 'get';
    this.summaryPaused = false; this.summaryPolls = 0;
    this.update({ summaryState: this.state.summaryCancelRequested ? 'canceling' : 'recovering', summaryError: null, summaryRetry: '' });
    this.ensureSummary();
  }
  private clearSummary(): void {
    this.summaryRequest = null; this.summaryPaused = false; this.summaryPolls = 0;
    this.stopSummaryTimer?.(); this.stopSummaryTimer = null;
    this.replyStops.get('summary')?.(); this.replyStops.get('summaryPage')?.();
    this.summaryPageVersion++; this.summaryHistory = [];
    this.update({ summary: null, summaryState: '', summaryRunning: false, summaryCancelRequested: false, summaryProcessed: '0', summaryTotal: '0', summaryError: null, summaryRetry: '', summaryKind: 'keyframes', summaryStart: '0', summaryPage: null, summaryPageLoading: false, summaryPageError: null, summaryPrevious: null });
  }
  private ensureSummary(): void {
    if (this.disposed || this.summaryBusy || this.summaryPaused || this.stopSummaryTimer || !this.summaryRequest || !this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.summaryPhase !== 'get') void this.loadSummary();
    else this.stopSummaryTimer = this.clock.after(() => { this.stopSummaryTimer = null; void this.loadSummary(); }, POLL_MS);
  }
  private currentSummary(request: DetailQuery): boolean {
    return !this.disposed && this.summaryRequest === request && this.state.session?.sessionId === request.sessionId && this.ready();
  }
  private async loadSummary(): Promise<void> {
    const request = this.summaryRequest;
    if (!request || this.summaryBusy || !this.ready() || this.disposed) return;
    const phase = this.summaryPhase;
    this.summaryBusy = true;
    try {
      const response: SummaryResponse = await this.awaitReply('summary', () => phase === 'start' ? this.api.startSummary(request) : phase === 'cancel' ? this.api.cancelSummary(request) : this.api.summaryResult(request));
      if (!this.currentSummary(request)) return;
      if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
      if (response.sessionId !== request.sessionId || response.requestId !== request.requestId) throw new RequestError('invalid_response');
      try { validateSummaryResponse(response); } catch { throw new RequestError('invalid_response'); }
      this.update({ summaryProcessed: response.processed, summaryTotal: response.total });
      if (response.state === 'ready') {
        this.summaryPaused = true;
        this.update({ summary: response.overview, summaryState: 'ready', summaryRunning: false, summaryCancelRequested: false, summaryError: null, summaryRetry: '' });
        this.selectSummaryKind(this.state.summaryKind);
        this.ensurePage();
      } else if (response.state === 'failed') {
        const code = response.error!.code;
        if (code === 'source_changed') { this.invalidateSource(); return; }
        this.summaryPaused = true;
        // Not-found cannot prove a lost Start/Cancel command was never admitted.
        const uncertain = code === 'summary_not_found';
        this.update({ summaryState: 'failed', summaryRunning: uncertain, summaryCancelRequested: uncertain && this.state.summaryCancelRequested, summaryError: code,
          summaryRetry: uncertain ? 'confirm' : ['summary_failed', 'deadline_exceeded', 'canceled'].includes(code) ? 'reload' : '' });
        if (!uncertain) this.ensurePage();
      } else {
        this.summaryPhase = 'get';
        if (this.state.summaryCancelRequested && response.state === 'pending') {
          // A pending reply after an uncertain cancellation is not consent to
          // resume. Keep reads blocked and allow the same cancellation again.
          this.summaryPaused = true;
          this.update({ summaryState: 'canceling', summaryError: 'summary_cancel_unconfirmed', summaryRetry: 'confirm' });
        } else if (phase === 'get' && ++this.summaryPolls >= SUMMARY_POLL_LIMIT) {
          this.summaryPaused = true;
          this.update({ summaryState: response.state, summaryError: 'summary_wait_paused', summaryRetry: 'confirm' });
        } else this.update({ summaryState: response.state, summaryCancelRequested: response.state === 'canceling' || this.state.summaryCancelRequested, summaryError: null, summaryRetry: '' });
      }
    } catch (error) {
      if (!this.currentSummary(request)) return;
      if ((phase === 'start' || phase === 'cancel') && !(error instanceof RequestError)) {
        this.summaryPhase = 'get';
        this.update({ summaryState: this.state.summaryCancelRequested ? 'canceling' : 'recovering', summaryError: null });
      } else {
        this.summaryPaused = true;
        this.update({ summaryState: this.state.summaryCancelRequested ? 'canceling' : 'failed', summaryError: errorCode(error), summaryRetry: 'confirm' });
      }
    } finally { this.summaryBusy = false; this.ensureSummary(); }
  }
  selectSummaryKind(kind: SummaryKind): void {
    if (this.disposed || this.state.summaryState !== 'ready' || !this.ready()) return;
    try { summaryKind(kind); } catch { return; }
    this.summaryHistory = [];
    this.requestSummaryPage(kind, '0');
  }
  pageSummary(direction: 'first' | 'previous' | 'next'): void {
    const page = this.state.summaryPage;
    if (!page || this.state.summaryPageLoading || this.state.summaryState !== 'ready' || !this.ready()) return;
    let start: string;
    if (direction === 'first') { this.summaryHistory = []; start = '0'; }
    else if (direction === 'previous') { const previous = this.summaryHistory.pop(); if (previous === undefined) return; start = previous; }
    else { if (unsigned(page.bounds.next) >= unsigned(page.bounds.total)) return; this.summaryHistory.push(page.bounds.start); start = page.bounds.next; }
    this.requestSummaryPage(this.state.summaryKind, start);
  }
  retrySummaryPage(): void {
    if (!this.state.summaryPageError || this.state.summaryPageLoading || this.state.summaryState !== 'ready' || !this.ready()) return;
    this.requestSummaryPage(this.state.summaryKind, this.state.summaryStart);
  }
  private requestSummaryPage(kind: SummaryKind, start: string): void {
    this.summaryPageVersion++;
    this.replyStops.get('summaryPage')?.();
    this.update({ summaryKind: kind, summaryStart: start, summaryPage: null, summaryPageLoading: true, summaryPageError: null, summaryPrevious: this.summaryHistory.at(-1) ?? null });
    this.ensureSummaryPage();
  }
  private ensureSummaryPage(): void {
    if (this.disposed || this.summaryPageBusy || !this.state.summaryPageLoading || !this.summaryRequest || this.state.summaryState !== 'ready' || !this.ready()) return;
    void this.loadSummaryPage();
  }
  private async loadSummaryPage(): Promise<void> {
    const identity = this.summaryRequest!;
    const request: SummaryPageRequest = { ...identity, kind: this.state.summaryKind, start: this.state.summaryStart, count: PAGE_SIZE };
    const version = this.summaryPageVersion;
    this.summaryPageBusy = true;
    try {
      const response = await this.awaitReply('summaryPage', () => this.api.summaryEvents(request));
      if (!this.currentSummary(identity) || version !== this.summaryPageVersion) return;
      if (response.sessionId !== request.sessionId || response.requestId !== request.requestId || response.kind !== request.kind) throw new RequestError('invalid_response');
      check(response);
      if (!response.page) throw new RequestError('invalid_response');
      try {
        validateSummaryPage(response.page, request.start, request.count);
        if (response.page.bounds.total !== this.state.summary!.lists.find(list => list.kind === request.kind)?.retained) throw Error('Invalid retained count.');
      } catch { throw new RequestError('invalid_response'); }
      this.update({ summaryPage: response.page });
    } catch (error) {
      if (!this.currentSummary(identity) || version !== this.summaryPageVersion) return;
      const code = errorCode(error);
      if (code === 'source_changed') { this.invalidateSource(); return; }
      this.update({ summaryPageError: code });
    } finally {
      this.summaryPageBusy = false;
      if (version === this.summaryPageVersion) this.update({ summaryPageLoading: false });
      else this.ensureSummaryPage();
    }
  }
  locateSummaryEvent(event: SummaryEvent, target: 'tag' | 'hex'): void {
    if (this.state.summaryState !== 'ready' || !this.state.summaryPage?.events.includes(event) || !this.ready()) return;
    if (target === 'tag') { if (event.tagId !== null) this.jumpTag(event.tagId); }
    else this.showHex({ offset: event.offset, bitOffset: event.bitOffset, bitLength: null, label: `媒体概览 · ${event.title}` });
  }
  dispose(): void {
    this.disposed = true;
    this.statusVersion++; this.pageVersion++;
    this.stopTimer?.(); this.stopTimer = null;
    this.detailRequest = null;
    this.stopDetailTimer?.(); this.stopDetailTimer = null;
    this.hexRequest = null;
    this.stopHexTimer?.(); this.stopHexTimer = null;
    this.summaryRequest = null;
    this.summaryPageVersion++;
    this.stopSummaryTimer?.(); this.stopSummaryTimer = null;
    for (const stop of this.replyStops.values()) stop();
    this.replyStops.clear();
    this.listeners.clear();
    // Resource ownership belongs to the host, not an unreliable unload event.
  }
}
