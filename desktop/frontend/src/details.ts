import type { DetailField } from './protocol.ts';

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
