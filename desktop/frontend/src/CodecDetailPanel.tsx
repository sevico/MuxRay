import type { Detail } from './protocol';
import type { HexSelection } from './hex';
import { selectionFromSpan } from './hex';
import { keyframeEvidence, keyframeRelations, mp3FrameSelection } from './codec-details';

export function MP3FramePanel({ detail, disabled, onSelect }: { detail: Detail; disabled: boolean; onSelect: (index: number | null) => void }) {
  const frames = mp3FrameSelection(detail);
  if (!frames) return null;
  const position = frames.selected === null ? -1 : frames.selectable.indexOf(frames.selected);
  return <section className="codec-evidence" aria-label="MP3 逐帧详情">
    <h3>MP3 逐帧详情</h3>
    <div className="codec-frame-controls">
      <label>选择帧 <select aria-label="MP3 帧选择" disabled={disabled} value={frames.selected ?? ''} onChange={event => onSelect(event.target.value === '' ? null : Number(event.target.value))}>
        <option value="">前两帧预览</option>
        {frames.selectable.map(index => <option key={index} value={index}>帧 {index}</option>)}
      </select></label>
      <button disabled={disabled || position <= 0} onClick={() => onSelect(frames.selectable[position - 1])}>上一 MP3 帧</button>
      <button disabled={disabled || position + 1 >= frames.selectable.length} onClick={() => onSelect(frames.selectable[position + 1])}>下一 MP3 帧</button>
      <button disabled={disabled || !frames.selectable.length || frames.selected === frames.selectable.at(-1)} onClick={() => onSelect(frames.selectable.at(-1)!)}>末个可选 MP3 帧</button>
    </div>
    <p className="detail-note" role="status">{frames.selected === null ? '当前显示前两帧预览' : `当前展开 MP3 帧 ${frames.selected}`} · 已检查 {frames.checked} 个帧头，{frames.selectable.length} 帧可选（索引从 0 开始）。</p>
    <p className="detail-note">仅选择当前预算内已确认边界的帧；依赖分析保留本 Tag 的前序历史。截断、未知帧长和预算外范围仍保留在详情中。</p>
  </section>;
}

export function KeyframeEvidencePanel({ detail, disabled, onLocate }: { detail: Detail; disabled: boolean; onLocate: (selection: HexSelection) => void }) {
  const evidence = keyframeEvidence(detail);
  if (!evidence) return null;
  const declaration = evidence.declared.value === '1' ? '关键帧' : evidence.declared.value === '2' ? '非关键帧' : `类型 ${evidence.declared.value}`;
  return <section className="codec-evidence" aria-label="H.264 关键帧声明与 NAL 证据">
    <h3>H.264 关键帧声明与 NAL 证据</h3>
    <div className="codec-facts"><span>FLV 声明：<strong>{declaration}</strong></span><span>IDR NAL：<strong>{evidence.idr === 'yes' ? '已观察到' : evidence.idr === 'no' ? '未观察到' : '未知'}</strong></span><span>slice 类型集合：<strong>{evidence.sliceTypes.join(' / ') || (evidence.sliceComplete ? '无已观察 slice' : '未知')}</strong></span></div>
    <p role="status">{keyframeRelations[evidence.relation]}</p>
    <p className="detail-note">已检查 NAL 头 {evidence.inspectedHeaders} / {evidence.retainedNALs}（保留项）；NAL 头覆盖{evidence.nalComplete ? '完整' : '不完整'}，slice 类型覆盖{evidence.sliceComplete ? '完整' : '不完整'}。这是所选 Tag 的观察；I slice 不等于 IDR，多 slice 也不代表多个完整帧。</p>
    <div className="codec-frame-controls">
      {evidence.declared.span && <button disabled={disabled} onClick={() => onLocate(selectionFromSpan(evidence.declared.span!, 'FLV frameType'))}>定位 FLV 帧类型</button>}
      {evidence.sources.map(field => <button key={field.id} disabled={disabled} onClick={() => onLocate(selectionFromSpan(field.span!, field.name))}>{field.name === 'evidenceNALType' ? '定位 NAL 头证据' : field.name === 'evidenceIDRNALType' ? '定位 IDR NAL' : `定位 ${field.name.slice('evidenceSliceType'.length)} slice`}</button>)}
    </div>
    <p className="detail-note">声明与观察的差异不直接判为损坏；这些证据不保证可独立解码，媒体概览的 GOP 仍按 FLV 声明统计。</p>
  </section>;
}
