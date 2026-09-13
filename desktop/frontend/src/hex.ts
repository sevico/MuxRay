import type { HexWindow, Span } from './protocol.ts';
import { unsigned } from './numbers.ts';

export const HEX_WINDOW_BYTES = 512;
export const HEX_MAX_BYTES = 4096;
// A diagnostic is a position, not a range. Zero-length spans are positions too.
export interface HexSelection { offset: string; bitOffset: number; bitLength: number | null; label: string }
export interface HexByte { offset: string; hex: string; ascii: string; mask: number; point: boolean; bits: string }
export interface HexRow { offset: string; bytes: HexByte[] }
export function selectionFromSpan(value: Span, label: string): HexSelection {
  return { offset: value.byteOffset, bitOffset: value.bitOffset, bitLength: value.bitLength, label };
}
export function validateSelection(value: HexSelection): void {
  const offset = unsigned(value.offset);
  if (!Number.isInteger(value.bitOffset) || value.bitOffset < 0 || value.bitOffset > 7 ||
      (value.bitLength !== null && (!Number.isInteger(value.bitLength) || value.bitLength < 0 || value.bitLength > 0xffffffff))) throw new Error('Invalid selection.');
  if (value.bitLength !== null) {
    const total = BigInt(value.bitOffset) + BigInt(value.bitLength), max = 18446744073709551615n;
    if (offset + total / 8n > max || (value.bitLength > 0 && offset + (total + 7n) / 8n > max)) throw new Error('Selection range overflows uint64.');
  }
}
export function validateHexWindow(value: HexWindow, count = HEX_MAX_BYTES): void {
  const start = unsigned(value.offset), size = unsigned(value.sourceSize);
  if (!Number.isInteger(count) || count < 1 || count > HEX_MAX_BYTES || typeof value.hex !== 'string' ||
      value.hex.length > count * 2 || !/^(?:[0-9a-f]{2})*$/.test(value.hex) || typeof value.eof !== 'boolean') throw new Error('Invalid Hex window.');
  const end = start + BigInt(value.hex.length / 2);
  if (start > size || end > size || value.eof !== (end === size) || (!value.eof && value.hex.length !== count * 2)) throw new Error('Inconsistent Hex bounds.');
}
export function hexRows(window: HexWindow, selection: HexSelection | null): HexRow[] {
  const base = unsigned(window.offset);
  if (selection) validateSelection(selection);
  const selectedStart = selection ? unsigned(selection.offset) * 8n + BigInt(selection.bitOffset) : 0n;
  const selectedEnd = selectedStart + BigInt(selection?.bitLength ?? 0);
  const rows: HexRow[] = [];
  for (let i = 0; i < window.hex.length / 2; i++) {
    const offset = base + BigInt(i), hex = window.hex.slice(i * 2, i * 2 + 2), value = parseInt(hex, 16);
    let mask = 0;
    if (selection && selection.bitLength !== null) for (let bit = 0; bit < 8; bit++) {
      const position = offset * 8n + BigInt(bit);
      if (position >= selectedStart && position < selectedEnd) mask |= 1 << (7 - bit);
    }
    if (i % 16 === 0) rows.push({ offset: offset.toString(), bytes: [] });
    rows[rows.length - 1].bytes.push({ offset: offset.toString(), hex, ascii: value >= 32 && value <= 126 ? String.fromCharCode(value) : '.', mask,
      point: !!selection && (selection.bitLength === null || selection.bitLength === 0) && offset.toString() === selection.offset,
      bits: value.toString(2).padStart(8, '0') });
  }
  return rows;
}
