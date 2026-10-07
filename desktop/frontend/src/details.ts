import type { Detail, DetailField } from './protocol.ts';

// Replay status is projected in the existing bounded field tree; exact Tag
// coordinates remain strings. Only a complete, consistent progress group can
// enable another read, never an arbitrary similarly named child field.
export function avcReplayProgress(detail: Detail | null): { next: string; target: string; canContinue: boolean } | null {
  if (detail?.kind !== 'avcVideo') return null;
  const root = detail.fields.find(f => f.name === 'Parameter context' && f.type === 'bounded replay');
  if (!root || root.value !== 'budget_exhausted') return null;
  const fields = detail.fields.filter(f => f.parentId === root.id);
  const value = (name: string) => fields.filter(f => f.name === name).length === 1 ? fields.find(f => f.name === name)!.value : '';
  const next = value('nextTagId'), target = value('selectedTagId'), start = value('contextSegmentStartTagId');
  if (![next, target, start].every(v => /^(0|[1-9][0-9]*)$/.test(v))) return null;
  if (BigInt(start) > BigInt(next) || BigInt(next) >= BigInt(target) || BigInt(target) > 18446744073709551615n) return null;
  return { next, target, canContinue: value('contextCanContinue') === 'true' && BigInt(next) > BigInt(start) };
}

export function fieldValue(field: DetailField): string {
  if (field.value !== '') return field.value;
  return field.type === 'utf8' ? '""' : '—';
}

// Flattened transport fields keep their source order. Resolve indentation
// without recursive rendering, including missing parents or malformed cycles.
export function detailRows(fields: DetailField[]): { field: DetailField; depth: number }[] {
  const parents = new Map(fields.map(field => [field.id, field.parentId]));
  return fields.map(field => {
    const seen = new Set([field.id]);
    let parent = field.parentId, depth = 0;
    while (parent && parents.has(parent) && !seen.has(parent) && depth < 8) {
      seen.add(parent); depth++; parent = parents.get(parent)!;
    }
    return { field, depth };
  });
}
