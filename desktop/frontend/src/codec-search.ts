import type { CodecSearchFilter, CodecSearchKind, CodecSearchPosition, CodecSearchRequest, CodecSearchResponse, CodecSearchRow } from './protocol.ts';
import { unsigned } from './numbers.ts';
import { selectionFromSpan, validateSelection } from './hex.ts';

export function codecSearchKind(value: string): CodecSearchKind {
  if (value === 'mp3' || value === 'sei') return value;
  throw Error('Invalid codec search kind.');
}
export function codecSearchFilter(kind: CodecSearchKind, value: string): CodecSearchFilter {
  if (value === 'all' || (kind === 'mp3' && (value === 'parameters' || value === 'bitrate' || value === 'stops')) ||
      (kind === 'sei' && (value === 'bufferingPeriod' || value === 'pictureTiming' || value === 'unknown'))) return value;
  throw Error('Invalid codec search filter.');
}
export function codecRowID(row: CodecSearchRow): string {
  return `${row.tagId}:${row.frameIndex ?? '-'}:${row.nalIndex ?? '-'}:${row.messageIndex ?? '-'}`;
}
function decimal(value: string): bigint {
  if (typeof value !== 'string') throw Error('Invalid decimal wire value.');
  return unsigned(value);
}
function bounded(value: number, max: number): boolean { return Number.isInteger(value) && value >= 0 && value <= max; }
function compare(a: CodecSearchPosition, b: CodecSearchPosition): number {
  const left = decimal(a.tagId), right = decimal(b.tagId);
  return left < right ? -1 : left > right ? 1 : Math.sign(a.frameIndex - b.frameIndex);
}

// Positions describe coverage, never a client-generated continuation token.
// SEI can drain another 64 messages from one Tag without reading any bytes.
export function validateCodecSearch(response: CodecSearchResponse, request: CodecSearchRequest, total: string, sourceSize: string, expectedStart?: CodecSearchPosition | null): void {
  if (response.apiVersion !== 1 || response.sessionId !== request.sessionId || response.requestId !== request.requestId) throw Error('Invalid codec search identity.');
  if (response.state === 'failed') {
    if (!response.error || response.page) throw Error('Invalid codec search failure.');
    return;
  }
  if (response.error) throw Error('Invalid codec search error.');
  if (response.state === 'pending' || response.state === 'canceling') {
    if (response.page) throw Error('Invalid pending codec search.');
    return;
  }
  if (response.state !== 'ready' || !response.page) throw Error('Invalid codec search state.');
  const p = response.page, end = decimal(total), size = decimal(sourceSize);
  codecSearchKind(p.kind); codecSearchFilter(p.kind, p.filter); decimal(p.cursor); decimal(p.nextCursor);
  if (p.kind !== request.kind || p.filter !== request.filter || p.cursor !== request.cursor || p.total !== total ||
      !bounded(p.scannedTags, 256) || !bounded(p.scannedFrames, p.kind === 'mp3' ? 1024 : 0) || !bounded(p.pending, p.kind === 'sei' ? 257 : 0) ||
      decimal(p.bytesRead) > (p.kind === 'mp3' ? 65536n : 1048576n) || typeof p.exhausted !== 'boolean' || typeof p.scanComplete !== 'boolean' ||
      p.nextCursor !== (p.exhausted ? '0' : request.requestId) || !Array.isArray(p.rows) || p.rows.length > 64) throw Error('Invalid codec search bounds.');
  for (const pos of [p.start, p.next]) {
    const tag = decimal(pos.tagId);
    if (tag > end || !bounded(pos.frameIndex, 4294967295) || ((p.kind === 'sei' || tag === end) && pos.frameIndex !== 0)) throw Error('Invalid codec search position.');
  }
  if (compare(p.start, p.next) > 0 || (expectedStart && compare(p.start, expectedStart) !== 0) ||
      (request.cursor === '0' && (p.start.tagId !== '0' || p.start.frameIndex !== 0)) ||
      p.exhausted !== (p.next.tagId === total && p.next.frameIndex === 0 && p.pending === 0) ||
      (!p.exhausted && compare(p.start, p.next) === 0 && (p.kind === 'mp3' || (p.rows.length === 0 && p.scannedTags === 0)))) throw Error('Invalid codec search progress.');
  const tagAdvance = decimal(p.next.tagId) - decimal(p.start.tagId);
  if (BigInt(p.scannedTags) > tagAdvance + 1n || tagAdvance > BigInt(p.scannedTags) + (p.kind === 'sei' ? 1n : 0n) ||
      (p.kind === 'mp3' && p.rows.length > p.scannedFrames + p.scannedTags)) throw Error('Invalid codec search coverage.');
  const seen = new Set<string>(); let previous = decimal(p.start.tagId), previousRow: CodecSearchRow | null = null;
  for (const row of p.rows) {
    const id = decimal(row.tagId), offset = decimal(row.offset), key = codecRowID(row);
    if (decimal(row.dts) > 4294967295n ||
        id < previous || id >= end || id > decimal(p.next.tagId) || offset > size || seen.has(key) || typeof row.complete !== 'boolean' ||
        ![row.kind, row.status, row.contextState].every(text => typeof text === 'string' && text.length <= 128) ||
        !Array.isArray(row.values) || row.values.length > 12 || new Set(row.values.map(v => v.name)).size !== row.values.length ||
        !row.values.every(v => typeof v.name === 'string' && v.name.length > 0 && v.name.length <= 64 && typeof v.value === 'string' && v.value.length <= 256)) throw Error('Invalid codec search row.');
    for (const index of [row.frameIndex, row.nalIndex, row.messageIndex]) if (index !== null && !bounded(index, 4294967295)) throw Error('Invalid codec evidence index.');
    if (p.kind === 'mp3') {
      if (row.frameIndex === null || row.nalIndex !== null || row.messageIndex !== null || row.spsSpan || row.evidenceSpan || row.contextState !== '' ||
          row.complete !== (row.status === 'complete') || row.complete === (row.kind === 'stop') ||
          (row.complete && !row.span) || (row.span && row.span.byteOffset !== row.offset) || (!row.complete && !['truncatedHeader', 'malformedHeader', 'unsupportedLayer', 'freeFormat', 'truncatedFrame', 'filteredAudio', 'truncatedAudio', 'missingAudioHeader', 'unsupportedAudio'].includes(row.status)) ||
          !['parameters', 'bitrate', 'parametersAndBitrate', 'stop'].includes(row.kind) ||
          (p.filter === 'stops' && row.kind !== 'stop') || (p.filter === 'parameters' && !['parameters', 'parametersAndBitrate'].includes(row.kind)) ||
          (p.filter === 'bitrate' && !['bitrate', 'parametersAndBitrate'].includes(row.kind)) ||
          compare({ tagId: row.tagId, frameIndex: row.frameIndex }, p.start) < 0 || compare({ tagId: row.tagId, frameIndex: row.frameIndex }, p.next) >= 0 ||
          (previousRow && compare({ tagId: previousRow.tagId, frameIndex: previousRow.frameIndex! }, { tagId: row.tagId, frameIndex: row.frameIndex }) >= 0)) throw Error('Invalid MP3 search evidence.');
    } else {
      if (row.frameIndex !== null || (row.tagId === p.next.tagId && p.pending === 0) ||
          !['complete', 'incomplete', 'unknown', 'opaque'].includes(row.status) || !['resolved', 'missing', 'ambiguous', 'not_applicable'].includes(row.contextState) ||
          (row.kind === 'coverage' ? (row.complete || row.status !== 'unknown' || row.nalIndex !== null || row.messageIndex !== null) :
            (row.nalIndex === null || row.messageIndex === null || row.messageIndex >= 64 || row.complete !== ['complete', 'opaque'].includes(row.status) ||
              !['bufferingPeriod', 'pictureTiming', 'recoveryPoint', 'userDataUnregistered', 'unknown'].includes(row.kind) ||
              (row.contextState === 'resolved' && (!row.spsSpan || !row.evidenceSpan)) || (['missing', 'ambiguous'].includes(row.contextState) && row.complete))) ||
          (row.kind !== 'coverage' && p.filter !== 'all' && p.filter !== 'unknown' && row.kind !== p.filter) ||
          (p.filter === 'unknown' && row.kind !== 'coverage' && row.complete && !['missing', 'ambiguous'].includes(row.contextState))) throw Error('Invalid SEI search evidence.');
      if (previousRow?.tagId === row.tagId && (previousRow.kind === 'coverage' || (row.kind !== 'coverage' && previousRow.nalIndex !== null &&
          (row.nalIndex! < previousRow.nalIndex || (row.nalIndex === previousRow.nalIndex && row.messageIndex! <= previousRow.messageIndex!))))) throw Error('Invalid SEI evidence order.');
    }
    for (const span of [row.span, row.spsSpan, row.evidenceSpan]) {
      if (!span) continue;
      validateSelection(selectionFromSpan(span, 'Codec evidence'));
      if (span.bitLength === 0 || decimal(span.byteOffset) * 8n + BigInt(span.bitOffset + span.bitLength) > size * 8n) throw Error('Invalid codec evidence source.');
    }
    if (row.span && decimal(row.span.byteOffset) < offset) throw Error('Invalid codec evidence offset.');
    seen.add(key); previous = id; previousRow = row;
  }
}
