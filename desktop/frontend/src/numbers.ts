const maxUint64 = 18446744073709551615n;

export function unsigned(text: string): bigint {
  if (!/^(0|[1-9][0-9]{0,19})$/.test(text)) throw new Error('Invalid decimal integer');
  const result = BigInt(text);
  if (result > maxUint64) throw new Error('Integer out of range');
  return result;
}

export function previousStart(start: string, count: number): string {
  const value = unsigned(start) - BigInt(count);
  return (value < 0n ? 0n : value).toString();
}

export function progressPercent(scanned: string, total: string): number {
  const size = unsigned(total);
  if (size === 0n) return 0;
  const value = unsigned(scanned) * 10000n / size;
  return Number(value > 10000n ? 10000n : value) / 100;
}

export function byteSize(value: string): string {
  const bytes = unsigned(value);
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB', 'EiB'];
  let divisor = 1n, index = 0;
  while (bytes / divisor >= 1024n && index < units.length - 1) { divisor *= 1024n; index++; }
  if (index === 0) return `${bytes} B`;
  const scaled = bytes * 10n / divisor;
  return `${scaled / 10n}.${scaled % 10n} ${units[index]}`;
}
