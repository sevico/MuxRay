import type { Detail } from './protocol';
import type { HexSelection } from './hex';
import { selectionFromSpan } from './hex';
import { keyframeEvidence, keyframeRelations, mp3FramePage, mp3FrameSelection, mp3Dependency } from './codec-details';
import type { MP3DependencySource } from './codec-details';

const dependencyReasons: Record<string, string> = {
  no_backward_reference: '不依赖前序槽位', within_tag: '来源均在当前 Tag 内', across_tags: '已定位跨 Tag 来源', start_of_file: '已到文件起点，历史不足',
  parameters_changed: 'MPEG 版本、采样率或声道配置改变', stream_changed: 'FLV 音频流标识改变', audio_codec_changed: '音频编码改变',
  invalid_frame: '帧头或边界不可确认', crc_invalid: 'CRC 未通过', invalid_side_info: 'side-info 无效或不完整', invalid_main_data_length: '主数据声明位数超出窗口',
  unparsed_audio_suffix: '前序音频 Tag 尾部未解析', unsupported_tag: '遇到无法分类的 Tag', filtered_audio: '音频载荷经过过滤', truncated_audio: '音频载荷截断',
  audio_header_missing: '缺少音频头', target_unavailable: '目标帧不可用', tag_budget: '前序 Tag 数达到上限', audio_tag_budget: '前序音频 Tag 数达到上限',
  frame_budget: '帧检查数量达到上限', coverage_budget: '源范围检查预算耗尽', read_budget: '读取字节预算耗尽', source_limit: '来源条目数达到上限',
};

export function MP3FramePanel({ detail, disabled, onSelect, onPage }: { detail: Detail; disabled: boolean; onSelect: (index: number | null) => void; onPage: (direction: -1 | 1 | 'continue') => void }) {
  const frames = mp3FrameSelection(detail), page = mp3FramePage(detail);
  if (!frames && !page) return null;
  const position = !frames || frames.selected === null ? -1 : frames.selectable.indexOf(frames.selected);
  return <section className="codec-evidence" aria-label="MP3 逐帧详情">
    <h3>MP3 逐帧详情</h3>
    {page && <>
      <div className="codec-frame-controls">
        <button disabled={disabled || page.state === 'preparing' || page.start === 0} onClick={() => onPage(-1)}>上一 MP3 帧页</button>
        <button disabled={disabled || page.state !== 'ready' || page.next === null} onClick={() => onPage(1)}>下一 MP3 帧页</button>
        {page.canContinue && <button disabled={disabled} onClick={() => onPage('continue')}>继续定位 MP3 帧页</button>}
      </div>
      <p className="detail-note" role="status">{page.state === 'preparing' ? `正在定位 MP3 帧页 ${page.requested}，已检查到帧 ${page.progress} 之前。请继续定位。` : page.state === 'stopped' ? `MP3 帧页 ${page.requested} 定位停止，已检查到帧 ${page.progress} 之前。请查看详情诊断。` : `MP3 帧页 ${page.start} · 本页 ${page.frames} 个帧头 · 覆盖 ${page.bytesCovered} byte。`}</p>
    </>}
    {frames && <>
    <div className="codec-frame-controls">
      <label>选择帧 <select key={page?.start ?? 0} aria-label="MP3 帧选择" disabled={disabled} value={frames.selected ?? ''} onChange={event => onSelect(event.target.value === '' ? null : Number(event.target.value))}>
        <option value="">{page && page.start > 0 ? '本页前两帧预览' : '前两帧预览'}</option>
        {frames.selectable.map(index => <option key={index} value={index}>帧 {index}</option>)}
      </select></label>
      <button disabled={disabled || position <= 0} onClick={() => onSelect(frames.selectable[position - 1])}>上一 MP3 帧</button>
      <button disabled={disabled || position + 1 >= frames.selectable.length} onClick={() => onSelect(frames.selectable[position + 1])}>下一 MP3 帧</button>
      <button disabled={disabled || !frames.selectable.length || frames.selected === frames.selectable.at(-1)} onClick={() => onSelect(frames.selectable.at(-1)!)}>末个可选 MP3 帧</button>
    </div>
    <p className="detail-note" role="status">{frames.selected === null ? (page && page.start > 0 ? '当前显示本页前两帧预览' : '当前显示前两帧预览') : `当前展开 MP3 帧 ${frames.selected}`} · 已检查 {frames.checked} 个帧头，{frames.selectable.length} 帧可选（索引从 0 开始）。</p>
    </>}
    <p className="detail-note">帧号保持 Tag 内原始序号；每页检查最多 64 帧，预览只展开前两帧。依赖分析保留跨页历史。截断、未知帧长和预算停止处不会猜测后续边界。</p>
  </section>;
}

export function MP3DependencyPanel({ detail, disabled, onInspect, onLocate }: { detail: Detail; disabled: boolean; onInspect: () => void; onLocate: (source: MP3DependencySource, destination: 'tag' | 'hex') => void }) {
  const frames = mp3FrameSelection(detail), dependency = mp3Dependency(detail), page = mp3FramePage(detail);
  if (!frames && !dependency) return null;
  return <section className="codec-evidence" aria-label="MP3 跨 Tag 依赖来源">
    <div className="detail-heading"><h3>MP3 跨 Tag 依赖来源</h3><button disabled={disabled || !frames?.selectable.includes(frames.selected ?? page?.start ?? 0)} onClick={onInspect}>定位 MP3 依赖来源</button></div>
    <p className="detail-note">按当前帧的 main_data_begin 查找前序 MP3 主数据槽；预览状态使用本页首帧。只说明物理范围覆盖，不判断剩余 reservoir 容量或音频能否解码。</p>
    {dependency && <>
      <p role="status">MP3 依赖定位完成：{dependency.state} · Tag {dependency.tagId} 帧 {dependency.frameIndex} · 回指 {dependency.requested} byte，已定位 {dependency.resolved} byte，未知 {dependency.missing} byte。</p>
      <p className="detail-note">{dependencyReasons[dependency.reason] ?? dependency.reason} · 检查前序 Tag {dependency.scannedTags} 条，读取 {dependency.bytesRead} byte。视频交错不视为音频中断；损坏、配置切换和预算限制保留未知。</p>
      <div className="detail-table-area"><table><thead><tr><th>来源 Tag / 帧</th><th>范围类型</th><th>起点 byte</th><th>字节数</th></tr></thead><tbody>{dependency.sources.map(source => <tr key={`${source.tagId}-${source.frameIndex}`}>
        <td><button className="source-link" disabled={disabled} aria-label={`跳转 MP3 来源 Tag ${source.tagId} 帧 ${source.frameIndex}`} onClick={() => onLocate(source, 'tag')}>Tag {source.tagId} / 帧 {source.frameIndex}</button></td>
        <td>{source.current ? '当前帧主数据槽' : '声明回溯窗口'}</td><td><button className="source-link" disabled={disabled} aria-label={`定位 MP3 来源 Tag ${source.tagId} 帧 ${source.frameIndex}`} onClick={() => onLocate(source, 'hex')}>{source.span.byteOffset}</button></td><td>{source.span.bitLength / 8}</td>
      </tr>)}</tbody></table>{!dependency.sources.length && <p className="detail-note">没有可确认的来源范围。</p>}</div>
    </>}
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
