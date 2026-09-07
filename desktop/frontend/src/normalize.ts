import type { desktop, main } from '../wailsjs/go/models';
import type { DiagnosticPage, PageResponse, PickResponse, Session, SessionResponse, TagPage } from './protocol';

// Wails v2.14 emits optional TS properties for Go pointers. Normalize both
// undefined (generated declarations) and null (actual JSON) without unsafe
// type assertions, and keep the rest of the generated contract type-checked.
function session(value: desktop.SessionSnapshot | null | undefined): Session | null {
  return value ? { ...value, progress: value.progress ?? null, scan: value.scan ?? null, failure: value.failure ?? null } : null;
}
export function sessionResponse(value: desktop.SessionResponse): SessionResponse {
  return { apiVersion: value.apiVersion, session: session(value.session), error: value.error ?? null };
}
export function pickResponse(value: main.PickResponse): PickResponse {
  return { ...sessionResponse(value), canceled: value.canceled, fileName: value.fileName };
}
export function tagResponse(value: desktop.TagPageResponse): PageResponse<TagPage> {
  return { apiVersion: value.apiVersion, error: value.error ?? null, page: value.page ? {
    ...value.page, tags: value.page.tags.map(tag => ({ ...tag, previousTagSize: { ...tag.previousTagSize, span: tag.previousTagSize.span ?? null } })),
  } : null };
}
export function diagnosticResponse(value: desktop.DiagnosticPageResponse): PageResponse<DiagnosticPage> {
  return { apiVersion: value.apiVersion, error: value.error ?? null, page: value.page ? {
    ...value.page, diagnostics: value.page.diagnostics.map(item => ({ ...item, expected: item.expected ?? null, actual: item.actual ?? null })),
  } : null };
}
