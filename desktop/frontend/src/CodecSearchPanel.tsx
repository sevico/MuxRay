import type { Controller, ViewState } from './controller';
import { codecRowID, codecSearchFilter, codecSearchKind } from './codec-search';

const kinds: Record<string, string> = { parameters: '参数变化', bitrate: '码率变化', parametersAndBitrate: '参数与码率变化', stop: '无法继续检查',
  bufferingPeriod: '缓冲周期', pictureTiming: '图像时序', recoveryPoint: '恢复点', userDataUnregistered: '用户数据', unknown: '其他 SEI', coverage: '覆盖缺口' };
const statuses: Record<string, string> = { complete: '完整', incomplete: '不完整', opaque: '保留未解释载荷', unknown: '未知',
  filteredAudio: '音频载荷被过滤', truncatedAudio: '音频载荷截断', missingAudioHeader: '缺少音频头', unsupportedAudio: '音频格式不支持',
  truncatedHeader: '帧头截断', malformedHeader: '帧头无效', unsupportedLayer: 'MPEG Layer 不支持', freeFormat: '自由格式长度未知', truncatedFrame: '帧正文截断' };
const contexts: Record<string, string> = { resolved: '上下文已确定', missing: '上下文缺失', ambiguous: '上下文不明确', not_applicable: '不适用' };

export function CodecSearchPanel({ view, controller, disabled, message }: { view: ViewState; controller: Controller; disabled: boolean; message: (code: string) => string }) {
  const page = view.codecSearchPage, running = view.codecSearchRunning, kind = view.codecSearchKind;
  const blocked = disabled || view.timeSearchRunning || view.summaryRunning || view.avcSearchRunning;
  return <section className="summary-panel" aria-label="编解码器批量检索">
    <div className="detail-heading"><div><p className="eyebrow">按批查找变化与来源</p><h2>MP3 / SEI 批量检索</h2></div><div className="actions">
      <label>类型 <select aria-label="编解码器检索类型" disabled={blocked || running} value={kind} onChange={event => controller.startCodecSearch(codecSearchKind(event.target.value))}>
        <option value="mp3">MP3 变化与停止</option><option value="sei">H.264 SEI 消息</option>
      </select></label>
      <label>范围 <select aria-label="编解码器检索筛选" disabled={blocked || running} value={view.codecSearchFilter} onChange={event => controller.startCodecSearch(kind, codecSearchFilter(kind, event.target.value))}>
        {kind === 'mp3' ? <><option value="all">全部变化与停止</option><option value="parameters">参数变化</option><option value="bitrate">码率变化</option><option value="stops">无法继续检查</option></> :
          <><option value="all">全部 SEI 消息</option><option value="bufferingPeriod">缓冲周期</option><option value="pictureTiming">图像时序</option><option value="unknown">未知或不完整证据</option></>}
      </select></label>
      <button disabled={blocked || running} onClick={() => controller.startCodecSearch()}>{view.codecSearchState ? '从头检索编解码器' : '检索编解码器证据'}</button>
      <button disabled={disabled || !running || view.codecSearchCancelRequested} onClick={() => controller.cancelCodecSearch()}>取消编解码器检索</button>
    </div></div>
    <p className="summary-note">{kind === 'mp3' ? '查找相邻可确认帧的参数、码率变化和无法继续检查的位置；正常 VBR 码率变化不代表损坏。停止记录不会打开帧详情。' :
      '按消息查看 SEI 证据与时序上下文来源；未知和覆盖缺口不直接判为损坏。同一 Tag 的消息可能分多批显示。'}</p>
    {running && <p role="status" className="summary-note">{view.codecSearchCancelRequested ? '正在确认编解码器检索取消…' : view.codecSearchState === 'recovering' ? '正在确认编解码器检索结果…' : '正在检查本批编解码器证据…'}</p>}
    {view.codecSearchError && <div className="request-error" role="alert"><span>{message(view.codecSearchError)}</span>{view.codecSearchRetry &&
      <button disabled={blocked} onClick={() => controller.retryCodecSearch()}>{view.codecSearchRetry === 'reload' ? '重试当前编解码器批次' : '确认编解码器检索状态'}</button>}</div>}
    {page && <>
      <p role="status" className="summary-note">编解码器检索完成：{page.kind} · Tag {page.start.tagId} / 帧 {page.start.frameIndex} → Tag {page.next.tagId} / 帧 {page.next.frameIndex} · 检查 {page.scannedTags} 个 Tag、{page.scannedFrames} 个帧头 · 本批 {page.rows.length} 条 · 待显示 {page.pending} 条 · 读取 {page.bytesRead} byte。{page.exhausted ? '已到当前索引末尾。' : '可继续下一批。'}{!page.scanComplete && '扫描索引不完整，本次检索不覆盖全文件。'}</p>
      <div className="summary-table-area"><table><thead><tr><th>Tag / 帧 / 消息</th><th>DTS (ms)</th><th>证据</th><th>参数与上下文</th><th>源位置</th></tr></thead><tbody>{page.rows.map(row => {
        const id = codecRowID(row);
        return <tr key={id} data-codec-row={id}>
          <td><button className="source-link" disabled={blocked || running} aria-label={`从编解码器检索跳转 ${id} Tag`} onClick={() => controller.locateCodecSearchRow(row, 'tag')}>Tag {row.tagId}</button>
            {row.frameIndex !== null && (row.complete && row.status === 'complete' ? <button className="source-link" disabled={blocked || running} aria-label={`从编解码器检索跳转 ${id} 帧`} onClick={() => controller.locateCodecSearchRow(row, 'frame')}>帧 {row.frameIndex}</button> : <p>停止于帧 {row.frameIndex}</p>)}
            {row.nalIndex !== null && <p>NAL {row.nalIndex} / 消息 {row.messageIndex}</p>}</td>
          <td>{row.dts}</td><td>{kinds[row.kind] ?? row.kind}<p>{statuses[row.status] ?? row.status}</p></td>
          <td>{row.contextState && <p>{contexts[row.contextState] ?? row.contextState}</p>}{row.values.map(value => <p key={value.name}>{value.name} = {value.value}</p>)}</td>
          <td>{([['hex', row.span, '证据'], ['sps', row.spsSpan, 'SPS'], ['evidence', row.evidenceSpan, '选择依据']] as const).map(([destination, span, label]) => span &&
            <p key={destination}><button className="source-link" disabled={blocked || running} aria-label={`定位编解码器 ${id} ${label}`} title={`${span.byteOffset} : ${span.bitOffset} · ${span.bitLength} bit`} onClick={() => controller.locateCodecSearchRow(row, destination)}>{label} {span.byteOffset} : {span.bitOffset}</button></p>)}</td>
        </tr>;
      })}</tbody></table>{!page.rows.length && <p className="summary-note">本批范围没有匹配证据。</p>}</div>
      <div className="pagination"><span>仅保留当前批结果；导出诊断 JSON 包含本批证据与覆盖范围。</span>
        <button disabled={blocked || running || page.exhausted} onClick={() => controller.continueCodecSearch()}>继续检索编解码器</button></div>
    </>}
  </section>;
}
