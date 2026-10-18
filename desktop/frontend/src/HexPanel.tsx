import { memo, useEffect, useMemo, useRef, useState } from 'react';
import type { Controller, ViewState } from './controller';
import type { HexWindow } from './protocol';
import { hexRows, HEX_WINDOW_BYTES } from './hex';
import type { HexRow, HexSelection } from './hex';
import { unsigned } from './numbers';

const HexGrid = memo(function HexGrid({ rows, active, onSelect }: { rows: HexRow[]; active: string | null; onSelect: (offset: string) => void }) {
  return <div className="hex-grid-scroll" tabIndex={0} aria-label="原始字节表，可滚动">
    <table className="hex-grid"><thead><tr><th>byte 偏移</th>{Array.from({ length: 16 }, (_, i) => <th key={i}>+{i.toString(16).padStart(2, '0')}</th>)}<th>ASCII</th></tr></thead><tbody>
      {rows.map(row => <tr key={row.offset}><th scope="row">{row.offset}</th>{row.bytes.map(byte => <td key={byte.offset}>
        <button className={`hex-byte ${byte.mask ? 'range-selected' : ''} ${byte.point ? 'point-selected' : ''}`} aria-label={`查看 byte ${byte.offset} 的位`} aria-pressed={active === byte.offset} onClick={() => onSelect(byte.offset)}>{byte.hex}</button>
      </td>)}{row.bytes.length < 16 && <td colSpan={16 - row.bytes.length} />}
        <td className="hex-ascii"><code>{row.bytes.map(byte => <span key={byte.offset} className={byte.mask ? 'range-selected' : byte.point ? 'point-selected' : ''}>{byte.ascii}</span>)}</code></td>
      </tr>)}
    </tbody></table>
  </div>;
});

function selectionNote(selection: HexSelection | null, window: HexWindow | null): string {
  if (!selection) return '未选择字段范围。';
  const start = `${selection.offset} : ${selection.bitOffset}`;
  if (selection.bitLength === null) return `${selection.label} · ${start} · 仅标记位置，无已知范围。`;
  if (selection.bitLength === 0) return `${selection.label} · ${start} · 零长度范围，仅标记位置。`;
  const firstBit = unsigned(selection.offset) * 8n + BigInt(selection.bitOffset), end = firstBit + BigInt(selection.bitLength);
  let note = `${selection.label} · ${start} · ${selection.bitLength} bit；结束 ${end / 8n} : ${end % 8n}（不含）。`;
  if (window) {
    const windowStart = unsigned(window.offset) * 8n, windowEnd = windowStart + BigInt(window.hex.length * 4);
    if (firstBit < windowStart || end > windowEnd) note += ' 范围超出当前窗口，仅高亮已读取部分。';
    if (end > unsigned(window.sourceSize) * 8n) note += ' 声明范围超过文件末尾，缺失字节不会补零显示。';
  }
  return note;
}

export function HexPanel({ view, controller, disabled, message }: { view: ViewState; controller: Controller; disabled: boolean; message: (code: string) => string }) {
  const [offset, setOffset] = useState('0');
  const [activeOffset, setActiveOffset] = useState<string | null>(null);
  const section = useRef<HTMLElement>(null);
  const window = view.hex;
  const rows = useMemo(() => window ? hexRows(window, view.hexSelection) : [], [window, view.hexSelection]);
  const activeByte = useMemo(() => {
    const bytes = rows.flatMap(row => row.bytes);
    return bytes.find(byte => byte.offset === activeOffset) ?? bytes.find(byte => byte.mask || byte.point) ?? bytes[0];
  }, [rows, activeOffset]);
  useEffect(() => { setOffset('0'); setActiveOffset(null); }, [view.session?.sessionId]);
  useEffect(() => { if (window) { setOffset(window.offset); setActiveOffset(null); } }, [window]);
  useEffect(() => { if (view.hexState === 'loading') section.current?.scrollIntoView({ block: 'start', behavior: 'smooth' }); }, [view.hexState, view.hexSelection]);
  const busy = ['loading', 'pending', 'recovering'].includes(view.hexState);
  const end = window ? (unsigned(window.offset) + BigInt(window.hex.length / 2)).toString() : null;
  return <section ref={section} id="hex-panel" className="hex-panel" aria-label="原始字节 Hex" aria-busy={busy}>
    <div className="hex-heading"><div><p className="eyebrow">只读 · 原始文件</p><h2>Hex / 字段定位</h2></div><div className="panel-heading-links">{view.timeSearchPage && <a href="#time-search">返回时间戳检索 ↑</a>}{view.trendPage && <a href="#tag-trends">返回趋势窗口 ↑</a>}{view.selectedTag && <a href="#tag-detail">返回 Tag {view.selectedTag.id} 详情 ↑</a>}<a href="#root">返回页首导出 ↑</a></div></div>
    <form className="hex-controls" onSubmit={event => { event.preventDefault(); controller.jumpHex(offset); }}>
      <label htmlFor="hex-offset-input">byte 偏移</label><input id="hex-offset-input" inputMode="numeric" value={offset} onChange={event => setOffset(event.target.value)} disabled={disabled} />
      <button disabled={disabled}>跳转偏移</button><span>每窗最多 {HEX_WINDOW_BYTES} byte · 每行 16 byte</span>
    </form>
    {view.hexError && <div className="request-error" role="alert"><span>{message(view.hexError)}</span>{view.hexRetry && <button onClick={() => controller.retryHex()} disabled={disabled}>{view.hexRetry === 'reload' ? '重新读取字节' : '重试字节查询'}</button>}</div>}
    {busy && <p className="detail-note" role="status">{view.hexState === 'recovering' ? '正在确认原始字节请求…' : '正在读取原始字节…'}</p>}
    <p className="hex-selection" aria-label="Hex 定位范围">{selectionNote(view.hexSelection, window)}</p>
    {window ? <>
      <div className="hex-window-heading"><p aria-label="Hex 实际窗口">实际窗口 [{window.offset}, {end}) · {window.hex.length / 2} byte / 文件 {window.sourceSize} byte{window.eof ? ' · 已到 EOF' : ''}</p>
        <div><button disabled={disabled || busy || window.offset === '0'} onClick={() => controller.pageHex(-1)}>前一窗口</button><button disabled={disabled || busy || window.eof} onClick={() => controller.pageHex(1)}>后一窗口</button></div>
      </div>
      {window.hex.length ? <>
        {activeByte && <div className="hex-bits" aria-label="选中字节的位"><span>byte {activeByte.offset} = {activeByte.hex} · bit 0 → 7（MSB 优先）</span><code>{[...activeByte.bits].map((bit, index) => {
          const selected = !!(activeByte.mask & (1 << (7 - index))), point = activeByte.point && view.hexSelection?.bitOffset === index;
          return <span key={index} className={selected ? 'range-selected' : point ? 'point-selected' : ''} aria-label={`bit ${index}${selected ? ' 范围内' : point ? ' 定位点' : ''}`}>{bit}</span>;
        })}</code></div>}
        <HexGrid rows={rows} active={activeByte?.offset ?? null} onSelect={setActiveOffset} />
      </> : <p className="detail-note">EOF：此位置没有字节。定位点不代表存在一个零值字节。</p>}
    </> : !busy && <p className="detail-note">点击 Tag 偏移、payload、字段位置或诊断位置，或输入文件偏移。仅读取当前窗口；不支持的编码和实际保留的截断字节也可查看。</p>}
    <p className="hex-legend">实色 = 范围内的实际字节/位；下划线 = 定位点。ASCII 中不可打印字节显示“.”；派生字段没有可定位的单一原始范围。</p>
  </section>;
}
