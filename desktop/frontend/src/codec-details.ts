import type { Detail, DetailField, Span } from './protocol.ts';
import { unsigned } from './numbers.ts';
import { selectionFromSpan, validateSelection } from './hex.ts';

function childValue(fields: DetailField[], parent: string, name: string): string | null {
  const found = fields.filter(field => field.parentId === parent && field.name === name);
  return found.length === 1 ? found[0].value : null;
}

export interface MP3FrameSelection { selected: number | null; selectable: number[]; checked: number }

export interface MP3FramePage {
  state: 'ready' | 'preparing' | 'stopped'; requested: number; start: number; next: number | null;
  progress: number; frames: number; bytesCovered: number; bytesRead: number;
  replayPages: number; checkpoints: number; canContinue: boolean; selected: number | null;
}

export function mp3FramePage(detail: Detail | null): MP3FramePage | null {
  if (detail?.kind !== 'mp3Audio') return null;
  const roots = detail.fields.filter(field => field.name === 'MP3 frame page' && field.type === 'bounded frame page');
  if (roots.length !== 1) return null;
  const value = (name: string) => childValue(detail.fields, roots[0].id, name);
  const state = value('pageState'), next = value('nextPageStart'), selected = value('requestedFrameIndex');
  const hasNext = value('hasNext'), canContinue = value('canContinue');
  if (!['ready', 'preparing', 'stopped'].includes(state ?? '') || next === null || selected === null ||
      !['true', 'false'].includes(hasNext ?? '') || !['true', 'false'].includes(canContinue ?? '') ||
      value('pageSize') !== '64' || value('pageReadBudget') !== '65536' || value('replayPageBudget') !== '16') return null;
  const number = (name: string, max: bigint) => {
    const text = value(name);
    if (text === null) throw Error('Missing page field.');
    const n = unsigned(text);
    if (n > max) throw Error('Page budget exceeded.');
    return Number(n);
  };
  try {
    const requested = number('requestedPageStart', 4294967295n), start = number('pageStartIndex', 4294967295n);
    const frames = number('pageFrames', 64n), progress = number('nextFrameIndex', 4294967295n);
    const nextIndex = next === 'none' ? null : Number(unsigned(next)), selectedIndex = selected === 'preview' ? null : Number(unsigned(selected));
    if (requested % 64 || start % 64 || start > requested || (state === 'ready' && start !== requested) ||
        (hasNext === 'true') !== (nextIndex !== null) || (nextIndex !== null && (nextIndex !== start + 64 || nextIndex > 4294967295 || frames !== 64)) ||
        (canContinue === 'true') !== (state === 'preparing') || (state === 'preparing' && (progress <= 0 || progress > requested)) ||
        (selectedIndex !== null && (selectedIndex < requested || selectedIndex >= requested + 64 || selectedIndex > 4294967295))) return null;
    return { state: state as MP3FramePage['state'], requested, start, next: nextIndex, progress, frames,
      bytesCovered: number('pageBytesCovered', 1048576n), bytesRead: number('pageBytesRead', 65536n),
      replayPages: number('replayPages', 16n), checkpoints: number('checkpointCount', 64n), canContinue: canContinue === 'true', selected: selectedIndex };
  } catch { return null; }
}

// Only backend-confirmed boundaries can be selected. Keep this separate from
// the human-readable frame summaries, which can also include partial frames.
export function mp3FrameSelection(detail: Detail | null): MP3FrameSelection | null {
  if (detail?.kind !== 'mp3Audio') return null;
  const roots = detail.fields.filter(field => field.name === 'Frame inspection' && field.type === 'bounded sequence');
  if (roots.length !== 1) return null;
  const root = roots[0].id;
  const selected = childValue(detail.fields, root, 'selectedFrameIndex');
  const choices = childValue(detail.fields, root, 'selectableFrameIndices');
  const checked = childValue(detail.fields, root, 'checkedFrameCount');
  if (selected === null || choices === null || checked === null || !/^(0|[1-9][0-9]*)$/.test(checked)) return null;
  const count = Number(checked);
  if (!Number.isSafeInteger(count) || count > 64) return null;
  const page = mp3FramePage(detail);
  if (detail.fields.some(field => field.name === 'MP3 frame page' && field.type === 'bounded frame page') && !page) return null;
  const first = page?.start ?? 0;
  // A full page of large absolute indices would exceed the 256-byte field
  // preview bound. Page-relative offsets preserve all 64 choices losslessly.
  const offsets = page ? childValue(detail.fields, root, 'selectableFrameOffsets') : null;
  const text = offsets ?? choices;
  const indices = text === '' ? [] : text.split(',');
  if (indices.length > count || indices.some(value => !/^(0|[1-9][0-9]*)$/.test(value))) return null;
  const selectable = indices.map(value => Number(value) + (offsets === null ? 0 : first));
  if (selectable.some((value, i) => value < first || value >= first + count || (i > 0 && value <= selectable[i - 1]))) return null;
  if (selected === 'preview') return { selected: null, selectable, checked: count };
  if (!/^(0|[1-9][0-9]*)$/.test(selected) || !selectable.includes(Number(selected))) return null;
  return { selected: Number(selected), selectable, checked: count };
}

export interface MP3DependencySource { tagId: string; frameIndex: number; current: boolean; span: Span }
export interface MP3Dependency {
  state: 'resolved' | 'unknown'; reason: string; tagId: string; frameIndex: number;
  requested: string; resolved: string; missing: string; scannedTags: string; bytesRead: string;
  sources: MP3DependencySource[];
}
export function mp3Dependency(detail: Detail | null): MP3Dependency | null {
  if (detail?.kind !== 'mp3Audio') return null;
  const roots = detail.fields.filter(field => field.name === 'MP3 dependency sources' && field.type === 'bounded cross-tag dependency');
  if (roots.length !== 1) return null;
  const root = roots[0], value = (name: string) => childValue(detail.fields, root.id, name);
  const state = value('dependencyState'), reason = value('dependencyReason'), tagId = value('targetTagId'), frameIndex = value('targetFrameIndex');
  const requested = value('requestedPreviousBytes'), resolved = value('resolvedPreviousBytes'), missing = value('missingPreviousBytes');
  const scannedTags = value('scannedTags'), bytesRead = value('bytesRead'), sourceCount = value('sourceCount');
  if ((state !== 'resolved' && state !== 'unknown') || root.value !== state || reason === null || reason.length > 128 || tagId === null || frameIndex === null ||
      requested === null || resolved === null || missing === null || scannedTags === null || bytesRead === null || sourceCount === null) return null;
  try {
    unsigned(tagId);
    if (unsigned(frameIndex) > 4294967295n || unsigned(scannedTags) > 512n || unsigned(bytesRead) > 1048576n || unsigned(sourceCount) > 64n) return null;
    for (const n of [requested, resolved, missing]) if (n !== 'unknown' && unsigned(n) > 511n) return null;
    if (requested !== 'unknown' && resolved !== 'unknown' && missing !== 'unknown' && unsigned(requested) !== unsigned(resolved) + unsigned(missing)) return null;
    if (state === 'resolved' && (requested === 'unknown' || resolved === 'unknown' || missing !== '0')) return null;
    const sources: MP3DependencySource[] = [];
    let previousEnd = 0n, previousBytes = 0n, currents = 0;
    for (const container of detail.fields.filter(field => field.parentId === root.id && field.type === 'physical main-data source')) {
      const get = (name: string) => childValue(detail.fields, container.id, name);
      const id = get('sourceTagId'), frame = get('sourceFrameIndex'), current = get('sourceCurrent');
      const spans = detail.fields.filter(field => field.parentId === container.id && field.name === 'sourceSpan');
      if (id === null || frame === null || (current !== 'true' && current !== 'false') || spans.length !== 1 || !spans[0].span) return null;
      const span = spans[0].span, offset = unsigned(span.byteOffset);
      validateSelection(selectionFromSpan(span, 'MP3 dependency'));
      if (unsigned(id) > unsigned(tagId) || unsigned(frame) > 4294967295n || (id === tagId && unsigned(frame) > unsigned(frameIndex)) ||
          span.bitOffset !== 0 || span.bitLength <= 0 || span.bitLength % 8 !== 0 || offset < previousEnd) return null;
      if (current === 'true') { currents++; if (id !== tagId || frame !== frameIndex) return null; }
      else { if (currents || (id === tagId && frame === frameIndex)) return null; previousBytes += BigInt(span.bitLength / 8); }
      previousEnd = offset + BigInt(span.bitLength / 8);
      sources.push({ tagId: id, frameIndex: Number(frame), current: current === 'true', span });
    }
    if (sources.length !== Number(sourceCount) || currents > 1 || (resolved !== 'unknown' && previousBytes !== unsigned(resolved))) return null;
    return { state, reason, tagId, frameIndex: Number(frameIndex), requested, resolved, missing, scannedTags, bytesRead, sources };
  } catch { return null; }
}

export const keyframeRelations = {
  keyframe_with_idr: '标记为关键帧，已观察到 IDR NAL',
  keyframe_without_idr: '标记为关键帧，未观察到 IDR NAL',
  inter_with_idr: '标记为非关键帧，已观察到 IDR NAL',
  inter_without_idr: '标记为非关键帧，未观察到 IDR NAL',
  undetermined: '检查范围不足，暂不能确定对照结果',
  other_declaration: '该 FLV 帧类型不参与关键帧对照',
};
export interface KeyframeEvidence {
  declared: DetailField; idr: 'yes' | 'no' | 'unknown'; relation: keyof typeof keyframeRelations;
  nalComplete: boolean; sliceComplete: boolean; sliceTypes: string[];
  inspectedHeaders: string; retainedNALs: string; sources: DetailField[];
}
export function keyframeEvidence(detail: Detail | null): KeyframeEvidence | null {
  if (detail?.kind !== 'avcVideo') return null;
  const roots = detail.fields.filter(field => field.name === 'Keyframe evidence' && field.type === 'bounded NAL evidence');
  if (roots.length !== 1) return null;
  const root = roots[0].id, fields = detail.fields.filter(field => field.parentId === root);
  const value = (name: string) => childValue(fields, root, name);
  const declared = fields.filter(field => field.name === 'declaredFrameType');
  const idr = value('observedIDR'), relation = value('declarationRelation');
  const nal = value('nalHeaderCoverageComplete'), slice = value('sliceTypeCoverageComplete');
  const types = value('observedSliceTypes'), inspected = value('inspectedNALHeaders'), retained = value('retainedNALUnits');
  if (declared.length !== 1 || !/^(0|[1-9]|1[0-5])$/.test(declared[0].value) ||
      (idr !== 'yes' && idr !== 'no' && idr !== 'unknown') || !relation || !Object.hasOwn(keyframeRelations, relation) ||
      (nal !== 'true' && nal !== 'false') || (slice !== 'true' && slice !== 'false') ||
      types === null || inspected === null || retained === null || !/^(0|[1-9][0-9]*)$/.test(inspected) || !/^(0|[1-9][0-9]*)$/.test(retained)) return null;
  const sliceTypes = types === '' ? [] : types.split(',');
  if (sliceTypes.length > 5 || new Set(sliceTypes).size !== sliceTypes.length || sliceTypes.some(type => !['P', 'B', 'I', 'SP', 'SI'].includes(type)) ||
      BigInt(inspected) > BigInt(retained) || BigInt(retained) > 256n || (idr === 'no' && nal !== 'true')) return null;
  return { declared: declared[0], idr, relation: relation as keyof typeof keyframeRelations, nalComplete: nal === 'true', sliceComplete: slice === 'true',
    sliceTypes, inspectedHeaders: inspected, retainedNALs: retained,
    sources: fields.filter(field => /^evidence(NALType|IDRNALType|SliceType(P|B|I|SP|SI))$/.test(field.name) && field.span !== null) };
}
