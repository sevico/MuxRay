// The nullable transport model is explicit: Wails' generated Go pointer types
// use optional fields even though encoding/json emits null. normalize.ts checks
// compatibility with generated methods; controller.ts handles normalized nulls.
export interface Fault { code: string; message: string }
export interface Progress {
  sourceSize: string; bytesScanned: string; tagCount: string; diagnosticCount: number;
}
export interface Session {
  sessionId: string; state: string; terminal: boolean; closed: boolean;
  progress: Progress | null;
  scan: { complete: boolean; hasErrors: boolean } | null;
  failure: Fault | null;
}
export interface SessionResponse { apiVersion: number; session: Session | null; error: Fault | null }
export interface PickResponse extends SessionResponse { requestId: string; pending: boolean; canceled: boolean; fileName: string }
export interface PageRequest { sessionId: string; start: string; count: number }
export interface Bounds { start: string; limit: number; total: string; next: string }
export interface Span { byteOffset: string; bitOffset: number; bitLength: number }
export interface Tag {
  id: string; typeCode: number; typeName: string; offset: string;
  span: Span; headerSpan: Span; payloadSpan: Span;
  rawTypeFlags: number; filter: boolean; dataSize: number; availableDataSize: number;
  dts: string; streamId: number; payloadComplete: boolean;
  previousTagSize: { present: boolean; span: Span | null; value: number; expected: number; valid: boolean };
}
export interface Diagnostic {
  code: string; severity: string; offset: string; bitOffset: number; message: string;
  expected: string | null; actual: string | null;
}
export interface TagPage { sessionId: string; bounds: Bounds; tags: Tag[] }
export interface TagFilter {
  type: 'all' | 'audio' | 'video' | 'script' | 'other';
  dtsMin: string | null; dtsMax: string | null;
  structure: 'all' | 'payload_incomplete' | 'previous_size_missing' | 'previous_size_invalid';
}
export interface FilteredTagRequest extends PageRequest { filter: TagFilter }
export interface FilteredTagPage {
  sessionId: string; filter: TagFilter; start: string; next: string; indexedTotal: string;
  limit: number; scanLimit: number; scanned: number; exhausted: boolean; tags: Tag[];
}
export interface TagTrendPoint {
  tagId: string; offset: string; dts: string; typeCode: number; streamId: number;
  dataSize: number; availableDataSize: number; payloadComplete: boolean;
  previousTagSizeState: 'valid' | 'missing' | 'invalid'; filtered: boolean;
  previousTagId: string | null; previousDts: string | null; deltaMs: string | null;
  context: 'found' | 'start' | 'budget_exhausted';
}
export interface TagTrendPage {
  sessionId: string; bounds: Bounds; contextStart: string; contextScanned: number; points: TagTrendPoint[];
}
export interface DiagnosticPage { sessionId: string; bounds: Bounds; diagnostics: Diagnostic[] }
export interface PageResponse<P> { apiVersion: number; page: P | null; error: Fault | null }
export interface DetailRequest { sessionId: string; requestId: string; tagId: string; mp3FrameIndex?: number; mp3Dependency?: boolean }
export interface DetailQuery { sessionId: string; requestId: string }
export type AVCSearchFilter = 'mismatches' | 'unknown' | 'all';
export interface AVCSearchRequest extends DetailQuery { start: string; filter: AVCSearchFilter }
export interface AVCSearchRow {
  tagId: string; dts: string; offset: string; declaredFrameType: number | null;
  observedIDR: 'yes' | 'no' | 'unknown'; declarationRelation: string; reason: string; span: Span | null;
}
export interface AVCSearchPage {
  start: string; next: string; total: string; scanned: number; bytesRead: string;
  exhausted: boolean; scanComplete: boolean; filter: AVCSearchFilter; rows: AVCSearchRow[];
}
export interface AVCSearchResponse extends DetailQuery {
  apiVersion: number; state: 'pending' | 'canceling' | 'ready' | 'failed'; page: AVCSearchPage | null; error: Fault | null;
}
export interface DetailField { id: string; parentId: string; name: string; type: string; value: string; span: Span | null }
export type DetailKind = 'amf0ScriptData' | 'aacAudio' | 'avcVideo' | 'mp3Audio';
export interface Detail { kind: DetailKind; complete: boolean; truncated: boolean; fields: DetailField[]; diagnostics: Diagnostic[] }
export interface DetailResponse {
  apiVersion: number; sessionId: string; requestId: string; tagId: string;
  state: 'pending' | 'ready' | 'failed'; detail: Detail | null; error: Fault | null;
}
export interface HexRequest { sessionId: string; requestId: string; offset: string; count: number }
export interface HexWindow { offset: string; sourceSize: string; hex: string; eof: boolean }
export interface HexResponse {
  apiVersion: number; sessionId: string; requestId: string; offset: string; count: number;
  state: 'pending' | 'ready' | 'failed'; window: HexWindow | null; error: Fault | null;
}
export type SummaryKind = 'keyframes' | 'aac' | 'avc' | 'mp3' | 'diagnostics';
export interface SummaryMetric { section: string; name: string; value: string | null; unit: string }
export interface SummaryList { kind: SummaryKind; observed: string | null; retained: string; truncated: boolean }
export interface SummaryOverview {
  sourceSize: string; indexedTags: string; scanComplete: boolean; scanHasErrors: boolean;
  complete: boolean; hasErrors: boolean; metrics: SummaryMetric[]; lists: SummaryList[];
}
export interface SummaryEvent {
  id: string; tagId: string | null; offset: string; bitOffset: number; dts: string | null;
  title: string; severity: string; message: string; values: SummaryMetric[];
}
export interface SummaryResponse {
  apiVersion: number; sessionId: string; requestId: string; state: 'pending' | 'canceling' | 'ready' | 'failed';
  processed: string; total: string; overview: SummaryOverview | null; error: Fault | null;
}
export interface SummaryPageRequest extends DetailQuery { kind: SummaryKind; start: string; count: number }
export interface SummaryPage { bounds: Bounds; events: SummaryEvent[] }
export interface SummaryPageResponse {
  apiVersion: number; sessionId: string; requestId: string; kind: SummaryKind;
  page: SummaryPage | null; error: Fault | null;
}
export interface MediaCurveRate {
  coveredBytes: string; coveredIntervalMs: string; bitsPerSecond: string | null; tagId: string | null;
}
export interface MediaCurveGOP {
  declaredCount: string; closedCount: string; totalDurationMs: string;
  minDurationMs: string | null; maxDurationMs: string | null; averageDurationMs: string | null; tagId: string | null;
}
export interface MediaCurvePoint {
  index: string; startTag: string; nextTag: string;
  audio: MediaCurveRate; video: MediaCurveRate; mp3: MediaCurveRate; gop: MediaCurveGOP; unclassifiedTags: string;
}
export interface MediaCurveRequest extends DetailQuery { start: string; count: number }
export interface MediaCurvePage {
  bounds: Bounds; windowTags: number; indexedTags: string; scanComplete: boolean; complete: boolean; points: MediaCurvePoint[];
}
export interface MediaCurveResponse extends DetailQuery { apiVersion: number; page: MediaCurvePage | null; error: Fault | null }
export interface SnapshotTrend {
  start: string; count: number; type: 'all' | 'audio' | 'video' | 'script' | 'other';
  metric: 'dts' | 'delta' | 'size'; gapThreshold: string;
}
export interface SnapshotRequest {
  schemaVersion?: 1 | 2 | 3;
  mediaCurve?: { start: string; count: number; metric: 'audio' | 'video' | 'mp3' | 'gopAverage' | 'gopMax' };
  avcSearch?: { requestId: string; start: string; filter: AVCSearchFilter };
  sessionId: string; selectedTagId: string | null; detailRequestId: string | null;
  hexRequestId: string | null; summaryRequestId: string | null;
  summaryPage: { kind: SummaryKind; start: string; count: number } | null;
  trend: SnapshotTrend | null;
}
export interface SaveRequest { requestId: string; snapshot: SnapshotRequest }
export interface SaveResponse {
  apiVersion: number; requestId: string; pending: boolean; canceled: boolean;
  fileName: string; bytesWritten: string; error: Fault | null;
}
export interface API {
  open(requestId: string): Promise<PickResponse>;
  openResult(requestId: string): Promise<PickResponse>;
  status(id: string): Promise<SessionResponse>;
  cancel(id: string): Promise<SessionResponse>;
  close(id: string): Promise<SessionResponse>;
  tags(request: PageRequest): Promise<PageResponse<TagPage>>;
  filteredTags(request: FilteredTagRequest): Promise<PageResponse<FilteredTagPage>>;
  tagTrends(request: PageRequest): Promise<PageResponse<TagTrendPage>>;
  diagnostics(request: PageRequest): Promise<PageResponse<DiagnosticPage>>;
  startDetail(request: DetailRequest): Promise<DetailResponse>;
  detailResult(request: DetailQuery): Promise<DetailResponse>;
  startHex(request: HexRequest): Promise<HexResponse>;
  hexResult(request: DetailQuery): Promise<HexResponse>;
  startSummary(request: DetailQuery): Promise<SummaryResponse>;
  summaryResult(request: DetailQuery): Promise<SummaryResponse>;
  cancelSummary(request: DetailQuery): Promise<SummaryResponse>;
  summaryEvents(request: SummaryPageRequest): Promise<SummaryPageResponse>;
  mediaCurves(request: MediaCurveRequest): Promise<MediaCurveResponse>;
  startAVCSearch(request: AVCSearchRequest): Promise<AVCSearchResponse>;
  avcSearchResult(request: DetailQuery): Promise<AVCSearchResponse>;
  cancelAVCSearch(request: DetailQuery): Promise<AVCSearchResponse>;
  saveSnapshot(request: SaveRequest): Promise<SaveResponse>;
  saveResult(requestId: string): Promise<SaveResponse>;
}
