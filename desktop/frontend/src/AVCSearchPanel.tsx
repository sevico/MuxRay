import type { Controller, ViewState } from './controller';
import { keyframeRelations } from './codec-details';
import { avcSearchFilter } from './avc-search';

const reasons: Record<string, string> = { complete: 'NAL 头覆盖完整', filtered: '过滤载荷，编码未知', truncated_payload: '载荷截断',
  byte_limit: '读取预算耗尽', unsupported_packet: '未知 AVC 包类型', missing_configuration: '缺少可用 NAL 长度配置',
  nal_limit: 'NAL 头数量达到上限', invalid_framing: 'NAL 边界不完整', unclassified_header: '存在不能分类的 NAL 头' };

export function AVCSearchPanel({ view, controller, disabled, message }: { view: ViewState; controller: Controller; disabled: boolean; message: (code: string) => string }) {
  const page = view.avcSearchPage, running = view.avcSearchRunning, blocked = disabled || view.summaryRunning || view.codecSearchRunning;
  return <section className="summary-panel" aria-label="H.264 批量证据检索">
    <div className="detail-heading"><div><p className="eyebrow">按批检查 NAL 头</p><h2>H.264 批量证据检索</h2></div>
      <div className="actions">
        <label>检索范围 <select aria-label="H.264 证据筛选" disabled={blocked || running} value={view.avcSearchFilter} onChange={event => controller.startAVCSearch(avcSearchFilter(event.target.value))}>
          <option value="mismatches">声明与 IDR 差异</option><option value="unknown">IDR 证据未知</option><option value="all">全部 AVC / 未分类 Tag</option>
        </select></label>
        <button disabled={blocked || running} onClick={() => controller.startAVCSearch()}>{view.avcSearchState ? '从头检索 H.264' : '检索 H.264 证据'}</button>
        <button disabled={disabled || !running || view.avcSearchCancelRequested} onClick={() => controller.cancelAVCSearch()}>取消 H.264 检索</button>
      </div>
    </div>
    <p className="summary-note">按索引分批查找 FLV 声明与 IDR NAL 的关系；差异不直接判为损坏。此处不分析 slice 类型，GOP 仍按 FLV 声明统计。</p>
    {running && <p role="status" className="summary-note">{view.avcSearchCancelRequested ? '正在确认 H.264 检索取消…' : view.avcSearchState === 'recovering' ? '正在确认 H.264 检索结果…' : `正在从 Tag ${view.avcSearchStart} 检查本批证据…`}</p>}
    {view.avcSearchError && <div className="request-error" role="alert"><span>{message(view.avcSearchError)}</span>{view.avcSearchRetry && <button disabled={blocked} onClick={() => controller.retryAVCSearch()}>{view.avcSearchRetry === 'reload' ? '重试当前 H.264 批次' : '确认 H.264 检索状态'}</button>}</div>}
    {page && <>
      <p role="status" className="summary-note">H.264 检索完成：Tag [{page.start}, {page.next}) · 检查 {page.scanned} 条 · 本批命中 {page.rows.length} 条 · 读取 {page.bytesRead} byte。{page.exhausted ? '已到当前索引末尾。' : '还有 Tag 未检查，可继续下一批。'}{!page.scanComplete && '扫描索引不完整，本次检索不覆盖全文件。'}</p>
      <div className="summary-table-area"><table><thead><tr><th>Tag / 详情</th><th>DTS (ms)</th><th>声明与 IDR</th><th>覆盖说明</th><th>源位置</th></tr></thead><tbody>{page.rows.map(row => <tr key={row.tagId}>
        <td><button className="source-link" disabled={blocked || running} aria-label={`从 H.264 检索跳转 Tag ${row.tagId}`} onClick={() => controller.locateAVCSearchRow(row, 'tag')}>{row.tagId}</button></td><td>{row.dts}</td>
        <td>{keyframeRelations[row.declarationRelation as keyof typeof keyframeRelations]}<p>IDR：{row.observedIDR}</p></td><td>{reasons[row.reason] ?? row.reason}</td>
        <td>{row.span ? <button className="source-link" disabled={blocked || running} aria-label={`定位 H.264 Tag ${row.tagId} 证据`} title={`${row.span.byteOffset} : ${row.span.bitOffset} · ${row.span.bitLength} bit`} onClick={() => controller.locateAVCSearchRow(row, 'hex')}>{row.span.byteOffset} : {row.span.bitOffset}</button> : '—'}</td>
      </tr>)}</tbody></table>{!page.rows.length && <p className="summary-note">本批范围没有匹配证据。</p>}</div>
      <div className="pagination"><span>只保留当前批结果；最多回看 64 个批次。导出诊断 JSON 会包含当前已完成批次的证据和覆盖范围。</span><div>
        <button disabled={blocked || running || view.avcSearchPrevious === null} onClick={() => controller.pageAVCSearch('previous')}>上一批 H.264</button>
        <button disabled={blocked || running || page.exhausted} onClick={() => controller.pageAVCSearch('next')}>继续检索 H.264</button>
      </div></div>
    </>}
  </section>;
}
