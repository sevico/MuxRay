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
export interface DiagnosticPage { sessionId: string; bounds: Bounds; diagnostics: Diagnostic[] }
export interface PageResponse<P> { apiVersion: number; page: P | null; error: Fault | null }
export interface API {
  open(requestId: string): Promise<PickResponse>;
  openResult(requestId: string): Promise<PickResponse>;
  status(id: string): Promise<SessionResponse>;
  cancel(id: string): Promise<SessionResponse>;
  close(id: string): Promise<SessionResponse>;
  tags(request: PageRequest): Promise<PageResponse<TagPage>>;
  diagnostics(request: PageRequest): Promise<PageResponse<DiagnosticPage>>;
}
