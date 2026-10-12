import type { MediaCurvePage, MediaCurveRequest } from './protocol.ts';
import { CURVE_PAGE_SIZE, curveTag, validateMediaCurveResponse } from './media-curves.ts';
import type { CurveMetric } from './media-curves.ts';
import type { API, Detail, DetailRequest, DetailResponse, DiagnosticPage, HexRequest, HexResponse, HexWindow, PageResponse, PickResponse, Session, SessionResponse, Tag, TagPage } from './protocol.ts';
import { unsigned } from './numbers.ts';
import { avcReplayProgress } from './details.ts';
import { mp3FrameSelection, mp3Dependency } from './codec-details.ts';
import type { MP3DependencySource } from './codec-details.ts';
import { avcSearchFilter, validateAVCSearch } from './avc-search.ts';
import type { AVCSearchFilter, AVCSearchPage, AVCSearchRequest, AVCSearchRow } from './protocol.ts';
import { HEX_WINDOW_BYTES, validateHexWindow, validateSelection } from './hex.ts';
import type { HexSelection } from './hex.ts';
import type { DetailQuery, SummaryKind, SummaryOverview, SummaryPage, SummaryPageRequest, SummaryResponse, SummaryEvent } from './protocol.ts';
import { summaryKind, validateSummaryPage, validateSummaryResponse } from './summary.ts';
import type { FilteredTagPage, TagFilter } from './protocol.ts';
import { FILTER_HISTORY_LIMIT, validateFilteredTagPage, validateTagFilter } from './tag-filter.ts';
import type { TagTrendPage } from './protocol.ts';
import { TREND_PAGE_SIZE, validateTagTrendPage } from './tag-trend-protocol.ts';
import type { SaveRequest, SaveResponse, SnapshotRequest, SnapshotTrend } from './protocol.ts';

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
  tagFilter: TagFilter | null;
  filteredPage: FilteredTagPage | null;
  filterError: string | null;
  filterPrevious: string | null;
  filterHistoryLimited: boolean;
  trendPage: TagTrendPage | null;
  trendLoading: boolean;
  trendError: string | null;
  trendStart: string;
  trendSelectedId: string | null;
  trendMetric: SnapshotTrend['metric'];
  trendType: SnapshotTrend['type'];
  trendGapThreshold: string;
  saveState: '' | 'saving' | 'recovering' | 'saved' | 'canceled' | 'failed';
  savePending: boolean;
  saveError: string | null;
  saveRetry: boolean;
  saveSourceName: string;
  saveSessionId: string | null;
  saveFileName: string;
  saveBytesWritten: string;
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
  avcSearchPage: AVCSearchPage | null;
  avcSearchState: '' | 'loading' | 'pending' | 'canceling' | 'recovering' | 'ready' | 'failed';
  avcSearchRunning: boolean;
  avcSearchCancelRequested: boolean;
  avcSearchError: string | null;
  avcSearchRetry: '' | 'confirm' | 'reload';
  avcSearchFilter: AVCSearchFilter;
  avcSearchStart: string;
  avcSearchPrevious: string | null;
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
  curvePage: MediaCurvePage | null;
  curveLoading: boolean;
  curveError: string | null;
  curveStart: string;
  curveSelectedIndex: string | null;
  curveMetric: CurveMetric;
}
export interface Scheduler { after(fn: () => void, ms: number): () => void }
const scheduler: Scheduler = {
  after(fn, ms) { const id = setTimeout(fn, ms); return () => clearTimeout(id); },
};

class RequestError extends Error {}
class ReplyUnconfirmed extends Error {}
type ReplySlot = 'open' | 'command' | 'status' | 'page' | 'detail' | 'hex' | 'summary' | 'summaryPage' | 'trend' | 'curve' | 'save' | 'avcSearch';
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
    const mp3Selection = mp3FrameSelection(response.detail);
    const dependency = request.mp3Dependency ? mp3Dependency(response.detail) : null;
    if ((mp3Selection && mp3Selection.selected !== (request.mp3FrameIndex ?? null)) ||
        (request.mp3FrameIndex !== undefined && !mp3Selection && !dependency)) throw new RequestError('invalid_response');
    if (request.mp3Dependency) {
      if (!dependency || dependency.tagId !== request.tagId || dependency.frameIndex !== (request.mp3FrameIndex ?? 0)) throw new RequestError('invalid_response');
    }
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

function validateSaveResult(response: SaveResponse, requestId: string): void {
  if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
  if (response.requestId !== requestId || typeof response.pending !== 'boolean' || typeof response.canceled !== 'boolean' ||
      typeof response.fileName !== 'string' || response.fileName.length > 512 || /[\\/\u0000]/.test(response.fileName) ||
      typeof response.bytesWritten !== 'string' || (response.pending && (response.canceled || response.error)) || (response.canceled && response.error)) throw new RequestError('invalid_response');
  if (response.error !== null && (!response.error || typeof response.error.code !== 'string' || !response.error.code || typeof response.error.message !== 'string')) throw new RequestError('invalid_response');
  let bytes: bigint;
  try { bytes = unsigned(response.bytesWritten); } catch { throw new RequestError('invalid_response'); }
  if (response.pending || response.canceled || response.error) {
    if (bytes !== 0n) throw new RequestError('invalid_response');
  } else if (bytes === 0n || bytes > 2097152n || !response.fileName) throw new RequestError('invalid_response');
}

// Framework-independent controller: one poll, one page, one user command at a
// time. Old replies cannot overwrite a close/cancel/new-session transition.
export class Controller {
  private api: API;
  private clock: Scheduler;
  private listeners = new Set<() => void>();
  private state: ViewState = { fileName: '', session: null, operation: '', error: null, tab: 'tags', start: '0', page: null, loading: false, tagFilter: null, filteredPage: null, filterError: null, filterPrevious: null, filterHistoryLimited: false, trendPage: null, trendLoading: false, trendError: null, trendStart: '0', trendSelectedId: null, trendMetric: 'dts', trendType: 'all', trendGapThreshold: '1000', saveState: '', savePending: false, saveError: null, saveRetry: false, saveSourceName: '', saveSessionId: null, saveFileName: '', saveBytesWritten: '0', selectedTag: null, detail: null, detailState: '', detailError: null, detailRetry: '', hex: null, hexSelection: null, hexState: '', hexError: null, hexRetry: '', tagJumpError: null, sourceChanged: false, avcSearchPage: null, avcSearchState: '', avcSearchRunning: false, avcSearchCancelRequested: false, avcSearchError: null, avcSearchRetry: '', avcSearchFilter: 'mismatches', avcSearchStart: '0', avcSearchPrevious: null, summary: null, summaryState: '', summaryRunning: false, summaryCancelRequested: false, summaryProcessed: '0', summaryTotal: '0', summaryError: null, summaryRetry: '', summaryKind: 'keyframes', summaryStart: '0', summaryPage: null, summaryPageLoading: false, summaryPageError: null, summaryPrevious: null, curvePage: null, curveLoading: false, curveError: null, curveStart: '0', curveSelectedIndex: null, curveMetric: 'audio' };
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
  private avcSearchRequest: AVCSearchRequest | null = null;
  private avcSearchPhase: 'start' | 'get' | 'cancel' = 'start';
  private avcSearchBusy = false;
  private avcSearchPaused = false;
  private avcSearchPolls = 0;
  private stopAVCSearchTimer: (() => void) | null = null;
  private avcSearchHistory: string[] = [];
  private detailRequest: DetailRequest | null = null;
  private detailPhase: 'start' | 'get' = 'start';
  private detailBusy = false;
  private detailPaused = false;
  private detailPolls = 0;
  private stopDetailTimer: (() => void) | null = null;
  private jumpTagId: string | null = null;
  private jumpMP3Frame: number | undefined;
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
  private filterHistory: string[] = [];
  private trendVersion = 0;
  private trendBusy = false;
  private trendWanted = false;
  private curveVersion = 0;
  private curveBusy = false;
  private nextSaveID = 0n;
  private saveRequest: SaveRequest | null = null;
  private saveBusy = false;
  private savePaused = false;
  private stopSaveTimer: (() => void) | null = null;

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
      this.clearTrend();
      this.clearFilter();
      this.clearAVCSearch();
      this.clearSummary();
      this.clearDetail();
      this.pageVersion++;
      this.replyStops.get('page')?.();
      this.refreshStatus = false;
      this.jumpTagId = null;
      this.update({ session: null, fileName: '', operation: '', page: null, loading: false, start: '0', sourceChanged: false, error: null });
    } else {
      if (session.closed) { this.clearTrend(); this.clearFilter(); this.clearAVCSearch(); this.clearSummary(); this.clearDetail(); }
      this.update({ session, operation: session.closed ? 'closing' : '' });
      this.ensureDetail();
      this.ensureHex();
      this.ensureSummary();
      this.ensureAVCSearch();
      this.ensureTrend();
    }
  }
  async open(): Promise<void> {
    if (this.disposed || this.state.session || this.state.operation || this.state.savePending) return;
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
      this.clearCompletedSave();
      this.update({ trendMetric: 'dts', trendType: 'all', trendGapThreshold: '1000' });
      this.clearTrend();
      this.clearFilter();
      this.clearAVCSearch();
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
  private clearCompletedSave(): void {
    if (this.state.savePending) return;
    this.saveRequest = null; this.savePaused = false;
    this.stopSaveTimer?.(); this.stopSaveTimer = null;
    this.update({ saveState: '', saveError: null, saveRetry: false, saveSourceName: '', saveSessionId: null, saveFileName: '', saveBytesWritten: '0' });
  }
  private freezeSnapshot(): SnapshotRequest {
    const summaryPage = this.state.summaryPage;
    const trendPage = this.state.trendPage;
    const curvePage = !this.state.curveLoading && !this.state.curveError ? this.state.curvePage : null;
    return Object.freeze({
      schemaVersion: 2,
      ...(curvePage ? { mediaCurve: Object.freeze({ start: curvePage.bounds.start, count: curvePage.bounds.limit, metric: this.state.curveMetric }) } : {}),
      sessionId: this.state.session!.sessionId, selectedTagId: this.state.selectedTag?.id ?? null,
      detailRequestId: this.detailRequest?.requestId ?? null, hexRequestId: this.hexRequest?.requestId ?? null,
      summaryRequestId: this.summaryRequest?.requestId ?? null,
      summaryPage: summaryPage ? Object.freeze({ kind: this.state.summaryKind, start: summaryPage.bounds.start, count: summaryPage.bounds.limit }) : null,
      trend: trendPage ? Object.freeze({ start: trendPage.bounds.start, count: trendPage.bounds.limit,
        type: this.state.trendType, metric: this.state.trendMetric, gapThreshold: this.state.trendGapThreshold }) : null,
    });
  }
  async saveSnapshot(): Promise<void> {
    if (this.disposed || this.state.savePending || this.saveBusy || this.state.operation || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.nextSaveID === 18446744073709551615n) { this.update({ saveState: 'failed', saveError: 'id_exhausted' }); return; }
    const request: SaveRequest = Object.freeze({ requestId: (++this.nextSaveID).toString(), snapshot: this.freezeSnapshot() });
    this.saveRequest = request; this.savePaused = false;
    this.update({ saveState: 'saving', savePending: true, saveError: null, saveRetry: false,
      saveSourceName: this.state.fileName, saveSessionId: request.snapshot.sessionId, saveFileName: '', saveBytesWritten: '0' });
    await this.readSave(true);
  }
  private currentSave(request: SaveRequest): boolean {
    // Saving owns a frozen snapshot, independent of the current document. Close
    // cannot release the native dialog gate or reinterpret this receipt.
    return !this.disposed && this.saveRequest === request && this.state.savePending;
  }
  private acceptSave(response: SaveResponse, request: SaveRequest): void {
    validateSaveResult(response, request.requestId);
    if (response.pending) { this.update({ saveState: 'saving', saveError: null, saveRetry: false }); return; }
    this.savePaused = false;
    this.update({ savePending: false, saveState: response.error ? 'failed' : response.canceled ? 'canceled' : 'saved',
      saveError: response.error?.code ?? null, saveRetry: false, saveFileName: response.fileName, saveBytesWritten: response.bytesWritten });
    if (response.error?.code === 'source_changed' && this.state.session?.sessionId === request.snapshot.sessionId) this.invalidateSource();
  }
  private async readSave(initial: boolean): Promise<void> {
    const request = this.saveRequest;
    if (!request || this.saveBusy || !this.currentSave(request)) return;
    this.saveBusy = true;
    try {
      const response = await this.awaitReply('save', () => initial ? this.api.saveSnapshot(request) : this.api.saveResult(request.requestId));
      if (this.currentSave(request)) this.acceptSave(response, request);
    } catch (error) {
      if (!this.currentSave(request)) return;
      if (initial) {
        // Transport failure or a malformed receipt cannot prove whether saving
        // happened. Confirm the same ID; never repeat a dialog or write.
        this.update({ saveState: 'recovering', saveError: error instanceof ReplyUnconfirmed ? null : errorCode(error), saveRetry: false });
      } else {
        this.savePaused = true;
        this.update({ saveState: 'recovering', saveError: errorCode(error), saveRetry: true });
      }
    } finally { this.saveBusy = false; this.scheduleSave(); }
  }
  private scheduleSave(): void {
    if (this.disposed || !this.state.savePending || this.saveBusy || this.savePaused || this.stopSaveTimer) return;
    this.stopSaveTimer = this.clock.after(() => { this.stopSaveTimer = null; void this.readSave(false); }, POLL_MS);
  }
  retrySave(): void {
    if (this.disposed || !this.state.savePending || !this.state.saveRetry || this.saveBusy) return;
    this.savePaused = false;
    this.update({ saveState: 'recovering', saveError: null, saveRetry: false });
    this.scheduleSave();
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
      this.clearTrend();
      this.clearFilter();
      this.clearAVCSearch();
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
    if (this.state.summaryRunning || this.state.avcSearchRunning) return;
    try { unsigned(start); } catch { this.update({ error: 'invalid_argument' }); return; }
    this.clearFilter();
    this.pageVersion++;
    this.jumpTagId = null;
    this.clearDetail();
    this.update({ tab, start, page: null, loading: false, error: this.openingID || this.refreshStatus || this.pollFailed ? this.state.error : null });
    this.ensurePage();
  }
  private clearFilter(): void {
    this.filterHistory = [];
    this.update({ tagFilter: null, filteredPage: null, filterPrevious: null, filterHistoryLimited: false, filterError: null });
  }
  applyTagFilter(filter: TagFilter): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    try { validateTagFilter(filter); } catch { this.update({ filterError: 'invalid_filter' }); return; }
    this.filterHistory = [];
    this.pageVersion++; this.jumpTagId = null;
    this.clearDetail();
    this.update({ tab: 'tags', start: '0', page: null, filteredPage: null, tagFilter: { ...filter }, loading: false,
      filterError: null, filterPrevious: null, filterHistoryLimited: false, error: null });
    this.ensurePage();
  }
  resetTagFilter(): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    this.select('tags');
  }
  pageFilteredTags(direction: 'first' | 'previous' | 'next'): void {
    const page = this.state.filteredPage;
    if (this.disposed || !page || !this.state.tagFilter || this.state.loading || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    let start: string;
    let limited = this.state.filterHistoryLimited;
    if (direction === 'first') { start = '0'; this.filterHistory = []; limited = false; }
    else if (direction === 'previous') {
      const previous = this.filterHistory.pop();
      if (previous === undefined) return;
      start = previous;
    } else {
      if (page.exhausted) return;
      this.filterHistory.push(page.start);
      if (this.filterHistory.length > FILTER_HISTORY_LIMIT) { this.filterHistory.shift(); limited = true; }
      start = page.next;
    }
    this.pageVersion++; this.jumpTagId = null;
    this.clearDetail();
    this.update({ start, filteredPage: null, loading: false, error: null,
      filterPrevious: this.filterHistory.at(-1) ?? null, filterHistoryLimited: limited });
    this.ensurePage();
  }
  private ready(): boolean {
    const session = this.state.session;
    return !!session?.terminal && !session.closed && session.scan !== null && this.state.operation !== 'closing' && !this.state.sourceChanged;
  }
  private clearTrend(): void {
    this.trendVersion++; this.trendWanted = false;
    this.replyStops.get('trend')?.();
    this.update({ trendPage: null, trendLoading: false, trendError: null, trendStart: '0', trendSelectedId: null });
  }
  setTrendOptions(options: Partial<Pick<ViewState, 'trendMetric' | 'trendType' | 'trendGapThreshold'>>): void {
    if (this.disposed || !this.state.session) return;
    if (options.trendMetric !== undefined && !['dts', 'delta', 'size'].includes(options.trendMetric)) throw Error('Invalid trend metric.');
    if (options.trendType !== undefined && !['all', 'audio', 'video', 'script', 'other'].includes(options.trendType)) throw Error('Invalid trend type.');
    if (options.trendGapThreshold !== undefined) {
      const gap = unsigned(options.trendGapThreshold);
      if (gap < 1n || gap > 4294967295n) throw Error('Invalid trend threshold.');
    }
    this.update(options);
  }
  loadTagTrends(start = '0'): void {
    if (this.disposed || !this.ready() || this.refreshStatus || this.pollFailed) return;
    try {
      if (unsigned(start) > unsigned(this.state.session!.progress!.tagCount)) throw Error('Out of index.');
    } catch { this.update({ trendError: 'invalid_argument' }); return; }
    this.trendVersion++; this.trendWanted = true;
    this.update({ trendStart: start, trendPage: null, trendLoading: true, trendError: null, trendSelectedId: null });
    this.ensureTrend();
  }
  pageTagTrends(direction: 'first' | 'previous' | 'next'): void {
    const page = this.state.trendPage;
    if (!page || this.state.trendLoading) return;
    const start = unsigned(page.bounds.start);
    if (direction === 'next') {
      if (unsigned(page.bounds.next) >= unsigned(page.bounds.total)) return;
      this.loadTagTrends(page.bounds.next);
    } else if (direction === 'first') this.loadTagTrends('0');
    else this.loadTagTrends((start > BigInt(TREND_PAGE_SIZE) ? start - BigInt(TREND_PAGE_SIZE) : 0n).toString());
  }
  retryTagTrends(): void {
    if (!this.state.trendError || this.trendBusy) return;
    this.loadTagTrends(this.state.trendStart);
  }
  private ensureTrend(): void {
    if (this.disposed || this.trendBusy || !this.trendWanted || !this.ready() || this.refreshStatus || this.pollFailed) return;
    void this.readTrend();
  }
  private async readTrend(): Promise<void> {
    const session = this.state.session!, version = this.trendVersion;
    const request = { sessionId: session.sessionId, start: this.state.trendStart, count: TREND_PAGE_SIZE };
    this.trendBusy = true; this.trendWanted = false;
    try {
      const response = await this.awaitReply('trend', () => this.api.tagTrends(request));
      if (this.disposed || version !== this.trendVersion || this.state.session?.sessionId !== session.sessionId || !this.ready()) return;
      if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
      if (response.error && response.page) throw new RequestError('invalid_response');
      check(response);
      if (!response.page) throw new RequestError('invalid_response');
      try {
        validateTagTrendPage(response.page, request, session.progress?.sourceSize);
        if (response.page.bounds.total !== session.progress?.tagCount) throw Error('Invalid index coverage.');
      } catch { throw new RequestError('invalid_response'); }
      this.update({ trendPage: response.page });
    } catch (error) {
      if (this.disposed || version !== this.trendVersion || this.state.session?.sessionId !== session.sessionId || !this.ready()) return;
      const code = errorCode(error);
      if (code === 'source_changed') this.invalidateSource();
      else this.update({ trendError: code });
    } finally {
      this.trendBusy = false;
      if (version === this.trendVersion) this.update({ trendLoading: false });
      this.ensureTrend();
    }
  }
  selectTrendTag(id: string): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed || !this.state.trendPage?.points.some(point => point.tagId === id)) return;
    this.update({ trendSelectedId: id });
    this.jumpTag(id);
  }
  showTrendHex(id: string, target: 'tag' | 'payload'): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const point = this.state.trendPage?.points.find(item => item.tagId === id);
    if (!point || (target !== 'tag' && target !== 'payload')) return;
    if (this.state.selectedTag?.id !== id) this.clearDetail();
    this.update({ trendSelectedId: id });
    this.showHex({ offset: (unsigned(point.offset) + (target === 'payload' ? 11n : 0n)).toString(), bitOffset: 0,
      bitLength: (point.availableDataSize + (target === 'tag' ? 11 : 0)) * 8,
      label: target === 'tag' ? `Tag ${id} 范围` : `Tag ${id} payload（实际 ${point.availableDataSize} / 声明 ${point.dataSize} byte）` });
  }
  private ensurePage(): void {
    if (this.disposed || this.paging || this.refreshStatus || this.pollFailed || !this.ready() || this.state.page || this.state.filteredPage || this.state.summaryRunning || this.state.avcSearchRunning) return;
    void this.loadPage();
  }
  private async loadPage(): Promise<void> {
    const session = this.state.session!;
    const { tab, start, tagFilter } = this.state;
    const version = this.pageVersion;
    this.paging = true;
    this.update({ loading: true });
    try {
      const request = { sessionId: session.sessionId, start, count: PAGE_SIZE };
      if (tab === 'tags' && tagFilter) {
        const filteredRequest = { ...request, filter: tagFilter };
        const response = await this.awaitReply('page', () => this.api.filteredTags(filteredRequest));
        if (this.disposed || version !== this.pageVersion || this.state.session?.sessionId !== session.sessionId || !this.ready()) return;
        check(response);
        if (!response.page) throw new RequestError('invalid_response');
        try {
          validateFilteredTagPage(response.page, filteredRequest);
          if (response.page.indexedTotal !== session.progress?.tagCount) throw Error('Invalid index coverage.');
        } catch { throw new RequestError('invalid_response'); }
        this.update({ filteredPage: response.page });
        return;
      }
      const result: VisiblePage = tab === 'tags'
        ? { tab, data: pageResult(await this.awaitReply('page', () => this.api.tags(request)), session.sessionId, start) }
        : { tab, data: pageResult(await this.awaitReply('page', () => this.api.diagnostics(request)), session.sessionId, start) };
      if (!this.disposed && version === this.pageVersion && this.state.session?.sessionId === session.sessionId && this.ready()) {
        this.update({ page: result });
        if (this.jumpTagId && result.tab === 'tags') {
          const id = this.jumpTagId, frame = this.jumpMP3Frame; this.jumpTagId = null; this.jumpMP3Frame = undefined;
          this.selectTag(id, frame);
        }
      }
    } catch (error) {
      if (!this.disposed && version === this.pageVersion && this.state.session?.sessionId === session.sessionId) {
        if (errorCode(error) === 'source_changed') this.invalidateSource();
        else this.update({ error: errorCode(error) });
      }
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
  selectTag(tagId: string, mp3FrameIndex?: number): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const tags = this.state.filteredPage?.tags ?? (this.state.page?.tab === 'tags' ? this.state.page.data.tags : []);
    const tag = tags.find(item => item.id === tagId);
    if (!tag || this.state.selectedTag?.id === tagId) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ detailError: 'id_exhausted' }); return; }
    this.clearHex();
    this.stopDetailTimer?.(); this.stopDetailTimer = null;
    this.detailRequest = { sessionId: this.state.session!.sessionId, requestId: (++this.nextReadID).toString(), tagId, ...(mp3FrameIndex === undefined ? {} : { mp3FrameIndex }) };
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
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || this.detailBusy || this.detailPaused || this.stopDetailTimer || !this.detailRequest || !this.ready() || this.refreshStatus || this.pollFailed) return;
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
  selectMP3Frame(index: number | null): void {
    const frames = mp3FrameSelection(this.state.detail);
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || this.detailBusy || !this.detailPaused || !this.detailRequest ||
        this.state.detailState !== 'ready' || !frames || frames.selected === index ||
        !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (index !== null && (!Number.isInteger(index) || !frames.selectable.includes(index))) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ detailError: 'id_exhausted' }); return; }
    this.clearHex();
    const { sessionId, tagId } = this.detailRequest;
    this.detailRequest = { sessionId, tagId, requestId: (++this.nextReadID).toString(), ...(index === null ? {} : { mp3FrameIndex: index }) };
    this.detailPhase = 'start'; this.detailPaused = false; this.detailPolls = 0;
    this.update({ detailState: 'loading', detailError: null, detailRetry: '' });
    this.ensureDetail();
  }
  inspectMP3Dependency(): void {
    const frames = mp3FrameSelection(this.state.detail);
    if (this.disposed || this.state.summaryRunning || this.state.avcSearchRunning || this.detailBusy || !this.detailPaused ||
        !this.detailRequest || this.state.detailState !== 'ready' || !frames || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const index = frames.selected ?? 0;
    if (!frames.selectable.includes(index)) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ detailError: 'id_exhausted' }); return; }
    this.clearHex();
    this.detailRequest = { ...this.detailRequest, requestId: (++this.nextReadID).toString(), mp3FrameIndex: index, mp3Dependency: true };
    this.detailPhase = 'start'; this.detailPaused = false; this.detailPolls = 0;
    this.update({ detailState: 'loading', detailError: null, detailRetry: '' });
    this.ensureDetail();
  }
  locateMP3Source(source: MP3DependencySource, destination: 'tag' | 'hex'): void {
    const result = mp3Dependency(this.state.detail);
    if (this.state.detailState !== 'ready' || !result?.sources.some(item => item.tagId === source.tagId && item.frameIndex === source.frameIndex &&
        item.current === source.current && item.span.byteOffset === source.span.byteOffset && item.span.bitLength === source.span.bitLength) ||
        this.state.summaryRunning || this.state.avcSearchRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (destination === 'tag') this.jumpTag(source.tagId, source.frameIndex);
    else this.showHex({ offset: source.span.byteOffset, bitOffset: 0, bitLength: source.span.bitLength, label: `MP3 来源 Tag ${source.tagId} 帧 ${source.frameIndex}` });
  }
  continueAVCContext(): void {
    const progress = avcReplayProgress(this.state.detail);
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || this.detailBusy || !this.detailPaused || !this.detailRequest ||
        this.state.detailState !== 'ready' || !progress?.canContinue || progress.target !== this.detailRequest.tagId ||
        !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ detailError: 'id_exhausted' }); return; }
    this.clearHex();
    this.detailRequest = { ...this.detailRequest, requestId: (++this.nextReadID).toString() };
    this.detailPhase = 'start'; this.detailPaused = false; this.detailPolls = 0;
    this.update({ detailState: 'loading', detailError: null, detailRetry: '' });
    this.ensureDetail();
  }
  retryDetail(): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || this.detailBusy || !this.detailPaused || !this.detailRequest || !this.state.detailRetry || !this.ready() || this.refreshStatus || this.pollFailed) return;
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
  jumpTag(text: string, mp3FrameIndex?: number): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    try {
      const id = unsigned(text), total = unsigned(this.state.session!.progress!.tagCount);
      if (id >= total) throw new Error('Out of range.');
      if (mp3FrameIndex !== undefined && (!Number.isInteger(mp3FrameIndex) || mp3FrameIndex < 0 || mp3FrameIndex >= 64)) throw new Error('Invalid frame.');
      this.select('tags', (id / BigInt(PAGE_SIZE) * BigInt(PAGE_SIZE)).toString());
      this.jumpTagId = id.toString();
      this.jumpMP3Frame = mp3FrameIndex;
      this.update({ tagJumpError: null });
    } catch { this.update({ tagJumpError: 'tag_not_found' }); }
  }
  showHex(selection: HexSelection): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
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
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.nextReadID === 18446744073709551615n) { this.update({ hexError: 'id_exhausted' }); return; }
    // A direct byte selection supersedes a Tag jump whose index page is still
    // arriving. Keep that page usable, but do not open its obsolete detail and
    // clear the newer Hex selection when the delayed page finishes.
    this.jumpTagId = null;
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
    this.clearTrend();
    this.clearFilter();
    this.clearAVCSearch();
    this.clearSummary();
    this.clearDetail();
    this.pageVersion++; this.jumpTagId = null;
    this.replyStops.get('page')?.();
    this.update({ sourceChanged: true, error: 'source_changed', page: null, loading: false });
  }
  private ensureHex(): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || this.hexBusy || this.hexPaused || this.stopHexTimer || !this.hexRequest || !this.ready() || this.refreshStatus || this.pollFailed) return;
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
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || this.hexBusy || !this.hexPaused || !this.hexRequest || !this.state.hexRetry || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.state.hexRetry === 'reload') {
      if (this.nextReadID === 18446744073709551615n) { this.update({ hexError: 'id_exhausted' }); return; }
      this.hexRequest = { ...this.hexRequest, requestId: (++this.nextReadID).toString() };
    }
    this.hexPhase = 'start'; this.hexPaused = false; this.hexPolls = 0;
    this.update({ hexState: 'loading', hexError: null, hexRetry: '' });
    this.ensureHex();
  }
  startAVCSearch(filter: AVCSearchFilter = this.state.avcSearchFilter): void {
    if (this.disposed || this.state.summaryRunning || this.state.avcSearchRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    try { avcSearchFilter(filter); } catch { return; }
    this.avcSearchHistory = [];
    this.requestAVCSearch('0', filter);
  }
  pageAVCSearch(direction: 'previous' | 'next'): void {
    const page = this.state.avcSearchPage;
    if (!page || this.state.avcSearchRunning || this.state.summaryRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    let start: string;
    if (direction === 'previous') {
      const previous = this.avcSearchHistory.pop(); if (previous === undefined) return; start = previous;
    } else {
      if (page.exhausted) return;
      this.avcSearchHistory.push(page.start);
      if (this.avcSearchHistory.length > 64) this.avcSearchHistory.shift();
      start = page.next;
    }
    this.requestAVCSearch(start, page.filter);
  }
  private requestAVCSearch(start: string, filter: AVCSearchFilter): void {
    if (this.nextReadID === 18446744073709551615n) { this.update({ avcSearchError: 'id_exhausted' }); return; }
    this.clearDetail();
    this.pageVersion++; this.jumpTagId = null;
    this.replyStops.get('page')?.();
    this.avcSearchRequest = { sessionId: this.state.session!.sessionId, requestId: (++this.nextReadID).toString(), start, filter };
    this.avcSearchPhase = 'start'; this.avcSearchPaused = false; this.avcSearchPolls = 0;
    this.update({ avcSearchPage: null, avcSearchState: 'loading', avcSearchRunning: true, avcSearchCancelRequested: false,
      avcSearchError: null, avcSearchRetry: '', avcSearchStart: start, avcSearchFilter: filter, avcSearchPrevious: this.avcSearchHistory.at(-1) ?? null, loading: false });
    this.ensureAVCSearch();
  }
  cancelAVCSearch(): void {
    if (this.disposed || !this.avcSearchRequest || !this.state.avcSearchRunning || this.state.avcSearchCancelRequested || !this.ready()) return;
    this.avcSearchRequest = { ...this.avcSearchRequest };
    this.stopAVCSearchTimer?.(); this.stopAVCSearchTimer = null;
    this.replyStops.get('avcSearch')?.();
    this.avcSearchPhase = 'cancel'; this.avcSearchPaused = false; this.avcSearchPolls = 0;
    this.update({ avcSearchState: 'canceling', avcSearchCancelRequested: true, avcSearchError: null, avcSearchRetry: '' });
    this.ensureAVCSearch();
  }
  retryAVCSearch(): void {
    if (this.disposed || this.avcSearchBusy || !this.avcSearchPaused || !this.avcSearchRequest || !this.state.avcSearchRetry || !this.ready() || this.state.summaryRunning || this.refreshStatus || this.pollFailed) return;
    if (this.state.avcSearchRetry === 'reload') { this.requestAVCSearch(this.avcSearchRequest.start, this.avcSearchRequest.filter); return; }
    this.avcSearchRequest = { ...this.avcSearchRequest };
    this.avcSearchPhase = this.state.avcSearchCancelRequested ? 'cancel' : this.state.avcSearchError === 'avc_search_not_found' ? 'start' : 'get';
    this.avcSearchPaused = false; this.avcSearchPolls = 0;
    this.update({ avcSearchState: this.state.avcSearchCancelRequested ? 'canceling' : 'recovering', avcSearchError: null, avcSearchRetry: '' });
    this.ensureAVCSearch();
  }
  private clearAVCSearch(): void {
    this.avcSearchRequest = null; this.avcSearchPaused = false; this.avcSearchPolls = 0; this.avcSearchHistory = [];
    this.stopAVCSearchTimer?.(); this.stopAVCSearchTimer = null;
    this.replyStops.get('avcSearch')?.();
    this.update({ avcSearchPage: null, avcSearchState: '', avcSearchRunning: false, avcSearchCancelRequested: false,
      avcSearchError: null, avcSearchRetry: '', avcSearchStart: '0', avcSearchFilter: 'mismatches', avcSearchPrevious: null });
  }
  private ensureAVCSearch(): void {
    if (this.disposed || this.avcSearchBusy || this.avcSearchPaused || this.stopAVCSearchTimer || !this.avcSearchRequest || !this.state.avcSearchRunning || !this.ready() || this.refreshStatus || this.pollFailed) return;
    if (this.avcSearchPhase !== 'get') void this.loadAVCSearch();
    else this.stopAVCSearchTimer = this.clock.after(() => { this.stopAVCSearchTimer = null; void this.loadAVCSearch(); }, POLL_MS);
  }
  private async loadAVCSearch(): Promise<void> {
    const request = this.avcSearchRequest;
    if (!request || this.avcSearchBusy || !this.ready() || this.disposed) return;
    const current = () => !this.disposed && this.avcSearchRequest === request && this.state.session?.sessionId === request.sessionId && this.ready();
    const phase = this.avcSearchPhase;
    this.avcSearchBusy = true;
    try {
      const response = await this.awaitReply('avcSearch', () => phase === 'start' ? this.api.startAVCSearch(request) : phase === 'cancel' ? this.api.cancelAVCSearch(request) : this.api.avcSearchResult(request));
      if (!current()) return;
      try { validateAVCSearch(response, request, this.state.session!.progress!.tagCount, this.state.session!.progress!.sourceSize); } catch { throw new RequestError('invalid_response'); }
      if (response.page && response.page.scanComplete !== this.state.session!.scan!.complete) throw new RequestError('invalid_response');
      if (response.state === 'ready') {
        this.avcSearchPaused = true;
        this.update({ avcSearchPage: response.page, avcSearchState: 'ready', avcSearchRunning: false, avcSearchCancelRequested: false, avcSearchError: null, avcSearchRetry: '' });
        this.ensurePage();
      } else if (response.state === 'failed') {
        const code = response.error!.code;
        if (code === 'source_changed') { this.invalidateSource(); return; }
        this.avcSearchPaused = true;
        const uncertain = code === 'avc_search_not_found';
        this.update({ avcSearchState: 'failed', avcSearchRunning: uncertain, avcSearchCancelRequested: uncertain && this.state.avcSearchCancelRequested,
          avcSearchError: code, avcSearchRetry: uncertain ? 'confirm' : ['avc_search_failed', 'deadline_exceeded', 'canceled'].includes(code) ? 'reload' : '' });
        if (!uncertain) this.ensurePage();
      } else {
        this.avcSearchPhase = 'get';
        if (this.state.avcSearchCancelRequested && response.state === 'pending') {
          this.avcSearchPaused = true;
          this.update({ avcSearchState: 'canceling', avcSearchError: 'avc_search_cancel_unconfirmed', avcSearchRetry: 'confirm' });
        } else if (phase === 'get' && ++this.avcSearchPolls >= DETAIL_POLL_LIMIT) {
          this.avcSearchPaused = true;
          this.update({ avcSearchState: response.state, avcSearchError: 'avc_search_wait_paused', avcSearchRetry: 'confirm' });
        } else this.update({ avcSearchState: response.state, avcSearchCancelRequested: response.state === 'canceling' || this.state.avcSearchCancelRequested, avcSearchError: null, avcSearchRetry: '' });
      }
    } catch (error) {
      if (!current()) return;
      if ((phase === 'start' || phase === 'cancel') && !(error instanceof RequestError)) {
        this.avcSearchPhase = 'get';
        this.update({ avcSearchState: this.state.avcSearchCancelRequested ? 'canceling' : 'recovering', avcSearchError: null });
      } else {
        this.avcSearchPaused = true;
        this.update({ avcSearchState: this.state.avcSearchCancelRequested ? 'canceling' : 'failed', avcSearchError: errorCode(error), avcSearchRetry: 'confirm' });
      }
    } finally { this.avcSearchBusy = false; this.ensureAVCSearch(); }
  }
  locateAVCSearchRow(row: AVCSearchRow, destination: 'tag' | 'hex'): void {
    if (this.state.avcSearchState !== 'ready' || !this.state.avcSearchPage?.rows.includes(row) || !this.ready()) return;
    if (destination === 'tag') this.jumpTag(row.tagId);
    else if (row.span) this.showHex({ offset: row.span.byteOffset, bitOffset: row.span.bitOffset, bitLength: row.span.bitLength, label: `H.264 Tag ${row.tagId} 证据` });
  }
  startSummary(): void {
    if (this.disposed || (this.state.summaryRunning || this.state.avcSearchRunning) || !this.ready() || this.refreshStatus || this.pollFailed) return;
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
    this.clearCurves();
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
  private clearCurves(): void {
    this.curveVersion++;
    this.replyStops.get('curve')?.();
    this.update({ curvePage: null, curveLoading: false, curveError: null, curveStart: '0', curveSelectedIndex: null, curveMetric: 'audio' });
  }
  loadMediaCurves(start = '0'): void {
    if (this.disposed || !this.ready() || this.refreshStatus || this.pollFailed || this.state.summaryState !== 'ready' || !this.state.summary) return;
    try { if (unsigned(start) > (unsigned(this.state.summary.indexedTags) + 255n) / 256n) throw Error('Out of curve range.'); }
    catch { this.update({ curveError: 'invalid_argument' }); return; }
    this.curveVersion++;
    this.replyStops.get('curve')?.();
    this.update({ curveStart: start, curvePage: null, curveLoading: true, curveError: null, curveSelectedIndex: null });
    this.ensureCurves();
  }
  pageMediaCurves(direction: 'first' | 'previous' | 'next'): void {
    const page = this.state.curvePage;
    if (!page || this.state.curveLoading) return;
    const start = unsigned(page.bounds.start);
    if (direction === 'next') {
      if (unsigned(page.bounds.next) >= unsigned(page.bounds.total)) return;
      this.loadMediaCurves(page.bounds.next);
    } else this.loadMediaCurves(direction === 'first' ? '0' : (start > BigInt(CURVE_PAGE_SIZE) ? start - BigInt(CURVE_PAGE_SIZE) : 0n).toString());
  }
  retryMediaCurves(): void {
    if (!this.state.curveError || this.curveBusy) return;
    this.loadMediaCurves(this.state.curveStart);
  }
  private ensureCurves(): void {
    if (this.disposed || this.curveBusy || !this.state.curveLoading || !this.summaryRequest || this.state.summaryState !== 'ready' || !this.ready() || this.refreshStatus || this.pollFailed) return;
    void this.readCurves();
  }
  private async readCurves(): Promise<void> {
    const identity = this.summaryRequest!, version = this.curveVersion;
    const request: MediaCurveRequest = { ...identity, start: this.state.curveStart, count: CURVE_PAGE_SIZE };
    this.curveBusy = true;
    try {
      const response = await this.awaitReply('curve', () => this.api.mediaCurves(request));
      if (!this.currentSummary(identity) || version !== this.curveVersion || this.state.summaryState !== 'ready') return;
      if (response.apiVersion !== 1) throw new RequestError('incompatible_api');
      try { validateMediaCurveResponse(response, request, this.state.summary!); } catch { throw new RequestError('invalid_response'); }
      check(response);
      this.update({ curvePage: response.page });
    } catch (error) {
      if (!this.currentSummary(identity) || version !== this.curveVersion || this.state.summaryState !== 'ready') return;
      const code = errorCode(error);
      if (code === 'source_changed') this.invalidateSource();
      else this.update({ curveError: code });
    } finally {
      this.curveBusy = false;
      if (version === this.curveVersion) this.update({ curveLoading: false });
      else this.ensureCurves();
    }
  }
  setCurveMetric(metric: CurveMetric): void {
    if (!['audio', 'video', 'mp3', 'gopAverage', 'gopMax'].includes(metric) || this.disposed) return;
    this.update({ curveMetric: metric });
  }
  selectCurvePoint(index: string): void {
    if (this.disposed || this.state.summaryState !== 'ready' || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const point = this.state.curvePage?.points.find(item => item.index === index);
    if (!point) return;
    this.update({ curveSelectedIndex: index });
    const id = curveTag(point, this.state.curveMetric);
    if (id !== null) this.jumpTag(id);
  }
  showCurveHex(index: string): void {
    if (this.disposed || this.state.summaryState !== 'ready' || !this.ready() || this.refreshStatus || this.pollFailed) return;
    const point = this.state.curvePage?.points.find(item => item.index === index), selected = this.state.selectedTag;
    if (!point || !selected || curveTag(point, this.state.curveMetric) !== selected.id) return;
    this.update({ curveSelectedIndex: index });
    this.showHex({ offset: selected.offset, bitOffset: 0, bitLength: (11 + selected.availableDataSize) * 8, label: `媒体曲线 · Tag ${selected.id}` });
  }
  dispose(): void {
    this.disposed = true;
    this.statusVersion++; this.pageVersion++;
    this.trendVersion++; this.trendWanted = false;
    this.saveRequest = null;
    this.stopSaveTimer?.(); this.stopSaveTimer = null;
    this.stopTimer?.(); this.stopTimer = null;
    this.detailRequest = null;
    this.stopDetailTimer?.(); this.stopDetailTimer = null;
    this.hexRequest = null;
    this.stopHexTimer?.(); this.stopHexTimer = null;
    this.avcSearchRequest = null;
    this.stopAVCSearchTimer?.(); this.stopAVCSearchTimer = null;
    this.summaryRequest = null;
    this.summaryPageVersion++; this.curveVersion++;
    this.stopSummaryTimer?.(); this.stopSummaryTimer = null;
    for (const stop of this.replyStops.values()) stop();
    this.replyStops.clear();
    this.listeners.clear();
    // Resource ownership belongs to the host, not an unreliable unload event.
  }
}
