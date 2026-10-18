import type { Controller, ViewState } from './controller';
import { timeSearchFilter } from './time-search';
import { trendType, trendTypeNames } from './tag-trends';

const names = { regression: 'DTS 回退', duplicate: 'DTS 重复', gap: 'DTS 大间隔', unknown: '前驱未知' };

export function TimeSearchPanel({ view, controller, disabled, message }: { view: ViewState; controller: Controller; disabled: boolean; message: (code: string) => string }) {
  const page = view.timeSearchPage, running = view.timeSearchRunning;
  const blocked = disabled || view.summaryRunning || view.avcSearchRunning || view.codecSearchRunning;
  return <section id="time-search" className="summary-panel" aria-label="长文件时间戳观察检索" aria-busy={running}>
    <div className="detail-heading"><div><p className="eyebrow">无需预先知道 Tag 位置</p><h2>时间戳观察检索</h2></div><div className="actions">
      <label>观察项 <select aria-label="时间戳检索筛选" disabled={blocked || running} value={view.timeSearchFilter} onChange={event => controller.setTimeSearchOptions({ timeSearchFilter: timeSearchFilter(event.target.value) })}>
        <option value="regressions">DTS 回退</option><option value="duplicates">DTS 重复</option><option value="gaps">DTS 大间隔</option><option value="all">全部时间观察</option>
      </select></label>
      <label>大间隔阈值 (ms)<input aria-label="时间戳检索大间隔阈值" inputMode="numeric" maxLength={10} disabled={blocked || running} value={view.timeSearchGapThreshold} onChange={event => controller.setTimeSearchOptions({ timeSearchGapThreshold: event.target.value })} /></label>
      <button disabled={blocked || running} onClick={() => controller.startTimeSearch()}>从头检索时间戳</button>
      <button disabled={disabled || !running || view.timeSearchCancelRequested} onClick={() => controller.cancelTimeSearch()}>取消时间戳检索</button>
    </div></div>
    <p className="summary-note">每批最多检查 4096 个已索引 Tag、显示 64 条结果，只保留当前批。按同类型、同 Stream ID 的最近前项比较原始 DTS，不展开回绕；时间观察不等于媒体损坏。大间隔严格大于阈值。</p>
    {running && <p role="status" className="summary-note">{view.timeSearchCancelRequested ? '正在确认时间戳检索取消…' : view.timeSearchState === 'recovering' ? '正在确认本批时间戳结果…' : '正在检查本批时间戳…'}</p>}
    {view.timeSearchError && <div className="request-error" role="alert"><span>{message(view.timeSearchError)}</span>{view.timeSearchRetry && <button disabled={blocked} onClick={() => controller.retryTimeSearch()}>{view.timeSearchRetry === 'reload' ? '重试当前时间戳批次' : '确认时间戳检索状态'}</button>}</div>}
    {page && <>
      <p role="status" className="summary-note">本批检查 Tag [{page.start}, {page.next}) · {page.scanned} 个 Tag · 本批 {page.rows.length} 条 · 连续已检范围 [0, {page.next}) / 已索引 {page.total}。{page.exhausted ? '已到当前索引末尾。' : '尚未到索引末尾，可继续下一批。'}{!page.scanComplete && '部分索引，本次检索不覆盖全文件。'}</p>
      {page.unknownPredecessors > 0 && <p className="summary-note">本批 {page.unknownPredecessors} 个前驱因 512 条轨道上下文上限未知；未推断时间异常，未知项不受筛选隐藏。</p>}
      <div className="summary-table-area"><table><thead><tr><th>Tag / 定位</th><th>观察项</th><th>类型 / Stream ID</th><th>DTS / 差值 (ms)</th><th>前驱依据</th></tr></thead><tbody>{page.rows.map(row => <tr key={row.point.tagId}>
        <td><button className="source-link" aria-label={`从时间戳检索定位 Tag ${row.point.tagId}`} disabled={blocked || running || view.trendLoading} onClick={() => controller.locateTimeSearchRow(row)}>Tag {row.point.tagId} · 趋势与详情</button></td>
        <td>{names[row.kind]}</td><td>{trendTypeNames[trendType(row.point)]} / {row.point.streamId}</td><td>{row.point.dts} / {row.point.deltaMs ?? '—'}</td>
        <td>{row.point.context === 'found' ? `Tag ${row.point.previousTagId} · ${row.point.previousDts} ms` : '上下文上限，无法比较'}</td>
      </tr>)}</tbody></table>{!page.rows.length && <p className="summary-note">仅本批范围未命中所选观察项，不代表全文件正常。</p>}</div>
      <div className="pagination"><span>本批阈值 {page.gapThreshold} ms；继续使用本批筛选与阈值。</span><button disabled={blocked || running || page.exhausted} onClick={() => controller.continueTimeSearch()}>继续下一批时间戳</button></div>
      {view.trendPage && <p className="summary-note"><a href="#tag-trends">前往已加载趋势 ↓</a>{view.selectedTag && <> · <a href="#tag-detail">前往当前 Tag {view.selectedTag.id} 详情 ↓</a></>}</p>}
    </>}
    <p className="summary-note">命中后加载所在 256 Tag 趋势窗口并读取详情，可继续查看 Hex。窗口独立回查最多 4096 个 Tag，稀疏轨道可能显示前驱未知。诊断 JSON 导出已加载的趋势、详情和 Hex，不包含此检索页。</p>
  </section>;
}
