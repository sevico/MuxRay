import type { GetMediaCurves } from '../wailsjs/go/desktop/Bindings';
import type { MediaCurveResponse } from './protocol';
import type { desktop, main } from '../wailsjs/go/models';
import type { DetailKind, DetailResponse, DiagnosticPage, HexResponse, PageResponse, PickResponse, Session, SessionResponse, TagPage, SummaryResponse, SummaryPageResponse, FilteredTagPage, TagFilter, TagTrendPage, TagTrendPoint, SaveResponse } from './protocol';
import { validateTagFilter } from './tag-filter.ts';
import { validateHexWindow } from './hex.ts';
import { summaryKind, validateSummaryResponse, validateSummaryPage } from './summary.ts';
import type { AVCSearchResponse, AVCSearchRow } from './protocol';
import { avcSearchFilter } from './avc-search.ts';

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
  return { ...sessionResponse(value), requestId: value.requestId, pending: value.pending, canceled: value.canceled, fileName: value.fileName };
}
export function saveResponse(value: main.SaveResponse): SaveResponse {
  return { ...value, error: value.error ?? null };
}
export function tagResponse(value: desktop.TagPageResponse): PageResponse<TagPage> {
  return { apiVersion: value.apiVersion, error: value.error ?? null, page: value.page ? {
    ...value.page, tags: value.page.tags.map(tag => ({ ...tag, previousTagSize: { ...tag.previousTagSize, span: tag.previousTagSize.span ?? null } })),
  } : null };
}
export function filteredTagResponse(value: desktop.FilteredTagPageResponse): PageResponse<FilteredTagPage> {
  return { apiVersion: value.apiVersion, error: value.error ?? null, page: value.page ? {
    ...value.page, filter: normalizedFilter(value.page.filter),
    tags: value.page.tags.map(tag => ({ ...tag, previousTagSize: { ...tag.previousTagSize, span: tag.previousTagSize.span ?? null } })),
  } : null };
}
export function tagTrendResponse(value: desktop.TagTrendPageResponse): PageResponse<TagTrendPage> {
  return { apiVersion: value.apiVersion, error: value.error ?? null, page: value.page ? {
    ...value.page, points: value.page.points.map(point => ({ ...point,
      previousTagId: point.previousTagId ?? null, previousDts: point.previousDts ?? null, deltaMs: point.deltaMs ?? null,
      context: trendContext(point.context), previousTagSizeState: trendPreviousSize(point.previousTagSizeState),
    })),
  } : null };
}
function trendContext(value: string): TagTrendPoint['context'] {
  if (value === 'found' || value === 'start' || value === 'budget_exhausted') return value;
  throw Error('Invalid trend context.');
}
function trendPreviousSize(value: string): TagTrendPoint['previousTagSizeState'] {
  if (value === 'valid' || value === 'missing' || value === 'invalid') return value;
  throw Error('Invalid trend previous Tag size.');
}
function normalizedFilter(value: { type: string; structure: string; dtsMin?: string | null; dtsMax?: string | null }): TagFilter {
  const { type, structure } = value;
  if (type !== 'all' && type !== 'audio' && type !== 'video' && type !== 'script' && type !== 'other') throw Error('Invalid filter type.');
  if (structure !== 'all' && structure !== 'payload_incomplete' && structure !== 'previous_size_missing' && structure !== 'previous_size_invalid') throw Error('Invalid filter structure.');
  const filter: TagFilter = { type, structure, dtsMin: value.dtsMin ?? null, dtsMax: value.dtsMax ?? null };
  validateTagFilter(filter);
  return filter;
}
export function diagnosticResponse(value: desktop.DiagnosticPageResponse): PageResponse<DiagnosticPage> {
  return { apiVersion: value.apiVersion, error: value.error ?? null, page: value.page ? {
    ...value.page, diagnostics: value.page.diagnostics.map(item => ({ ...item, expected: item.expected ?? null, actual: item.actual ?? null })),
  } : null };
}
export function detailResponse(value: desktop.DetailResponse): DetailResponse {
  const state = value.state;
  if (state !== 'pending' && state !== 'ready' && state !== 'failed') throw new Error('Invalid detail response state.');
  return {
    ...value, state, error: value.error ?? null, detail: value.detail ? {
      ...value.detail, kind: detailKind(value.detail.kind),
      fields: value.detail.fields.map(field => ({ ...field, span: field.span ?? null })),
      diagnostics: value.detail.diagnostics.map(item => ({ ...item, expected: item.expected ?? null, actual: item.actual ?? null })),
    } : null,
  };
}
function detailKind(value: string): DetailKind {
  if (value === 'amf0ScriptData' || value === 'aacAudio' || value === 'avcVideo' || value === 'mp3Audio') return value;
  throw new Error('Invalid detail response kind.');
}
export function hexResponse(value: desktop.HexResponse): HexResponse {
  const state = value.state;
  if (state !== 'pending' && state !== 'ready' && state !== 'failed') throw new Error('Invalid Hex response state.');
  const window = value.window ?? null;
  if (window) validateHexWindow(window, value.count);
  return { ...value, state, window, error: value.error ?? null };
}
export function summaryResponse(value: desktop.SummaryResponse): SummaryResponse {
  const state = value.state;
  if (state !== 'pending' && state !== 'canceling' && state !== 'ready' && state !== 'failed') throw new Error('Invalid summary response state.');
  const result: SummaryResponse = { ...value, state, error: value.error ?? null, overview: value.overview ? {
    ...value.overview, metrics: value.overview.metrics.map(metric => ({ ...metric, value: metric.value ?? null })),
    lists: value.overview.lists.map(list => ({ ...list, kind: summaryKind(list.kind), observed: list.observed ?? null })),
  } : null };
  validateSummaryResponse(result);
  return result;
}
export function summaryPageResponse(value: desktop.SummaryPageResponse): SummaryPageResponse {
  const result: SummaryPageResponse = { ...value, kind: summaryKind(value.kind), error: value.error ?? null, page: value.page ? {
    ...value.page, events: value.page.events.map(event => ({ ...event, tagId: event.tagId ?? null, dts: event.dts ?? null,
      values: event.values.map(metric => ({ ...metric, value: metric.value ?? null })),
    })),
  } : null };
  if (result.page) validateSummaryPage(result.page, result.page.bounds.start, result.page.bounds.limit);
  return result;
}

export function mediaCurveResponse(value: Awaited<ReturnType<typeof GetMediaCurves>>): MediaCurveResponse {
  return { ...value, error: value.error ?? null, page: value.page ? {
    ...value.page, points: value.page.points.map(point => ({ ...point,
      audio: { ...point.audio, bitsPerSecond: point.audio.bitsPerSecond ?? null, tagId: point.audio.tagId ?? null },
      video: { ...point.video, bitsPerSecond: point.video.bitsPerSecond ?? null, tagId: point.video.tagId ?? null },
      mp3: { ...point.mp3, bitsPerSecond: point.mp3.bitsPerSecond ?? null, tagId: point.mp3.tagId ?? null },
      gop: { ...point.gop, minDurationMs: point.gop.minDurationMs ?? null, maxDurationMs: point.gop.maxDurationMs ?? null,
        averageDurationMs: point.gop.averageDurationMs ?? null, tagId: point.gop.tagId ?? null },
    })),
  } : null };
}

export function avcSearchResponse(value: desktop.AVCSearchResponse): AVCSearchResponse {
  const state = value.state;
  if (state !== 'pending' && state !== 'canceling' && state !== 'ready' && state !== 'failed') throw Error('Invalid AVC search state.');
  return { ...value, state, error: value.error ?? null, page: value.page ? { ...value.page, filter: avcSearchFilter(value.page.filter),
    rows: value.page.rows.map(row => ({ ...row, declaredFrameType: row.declaredFrameType ?? null, observedIDR: observedIDR(row.observedIDR), span: row.span ?? null })),
  } : null };
}
function observedIDR(value: string): AVCSearchRow['observedIDR'] {
  if (value === 'yes' || value === 'no' || value === 'unknown') return value;
  throw Error('Invalid IDR observation.');
}
