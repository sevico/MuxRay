import type { Detail, DetailField } from './protocol.ts';

function childValue(fields: DetailField[], parent: string, name: string): string | null {
  const found = fields.filter(field => field.parentId === parent && field.name === name);
  return found.length === 1 ? found[0].value : null;
}

export interface MP3FrameSelection { selected: number | null; selectable: number[]; checked: number }

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
  const indices = choices === '' ? [] : choices.split(',');
  if (indices.length > count || indices.some(value => !/^(0|[1-9][0-9]*)$/.test(value))) return null;
  const selectable = indices.map(Number);
  if (selectable.some((value, i) => value >= count || (i > 0 && value <= selectable[i - 1]))) return null;
  if (selected === 'preview') return { selected: null, selectable, checked: count };
  if (!/^(0|[1-9][0-9]*)$/.test(selected) || !selectable.includes(Number(selected))) return null;
  return { selected: Number(selected), selectable, checked: count };
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
