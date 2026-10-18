import { useEffect, useMemo, useState } from 'react';
import type { Controller, ViewState } from './controller';
import type { TagTrendPage, TagTrendPoint } from './protocol';
import { unsigned } from './numbers';
import { TREND_GAP_DEFAULT, TREND_ISSUE_PAGE_SIZE, TREND_PLOT, trendContext, trendDomain, trendGap, trendHitWidth, trendIssueNames, trendIssues, trendType, trendTypeNames, trendValue, trendX, trendY } from './tag-trends';
import type { TrendMetric, TrendType } from './tag-trends';

const metricNames: Record<TrendMetric, string> = { dts: '原始 DTS', size: '载荷大小', delta: '同类型 DTS 差' };

export function TagTrendPanel({ view, controller, disabled, readDisabled, message }: {
  view: ViewState; controller: Controller; disabled: boolean; readDisabled: boolean; message: (code: string) => string;
}) {
  const [start, setStart] = useState('0');
  const metric = view.trendMetric;
  const type = view.trendType;
  const [gapDraft, setGapDraft] = useState(TREND_GAP_DEFAULT);
  const gap = view.trendGapThreshold;
  const [gapError, setGapError] = useState(false);
  const [issuePage, setIssuePage] = useState(0);
  const page = view.trendPage;
  useEffect(() => { setStart(view.trendStart); }, [view.trendStart]);
  useEffect(() => { setGapDraft(view.trendGapThreshold); }, [view.trendGapThreshold]);
  useEffect(() => { setIssuePage(0); }, [page, type, gap]);
  const points = useMemo(() => (page?.points ?? []).filter(point => type === 'all' || trendType(point) === type), [page, type]);
  const flagged = useMemo(() => points.map(point => ({ point, issues: trendIssues(point, BigInt(gap)) })).filter(item => item.issues.length), [points, gap]);
  const selected = page?.points.find(point => point.tagId === view.trendSelectedId);
  const issueStart = Math.min(issuePage * TREND_ISSUE_PAGE_SIZE, Math.max(0, Math.ceil(flagged.length / TREND_ISSUE_PAGE_SIZE) - 1) * TREND_ISSUE_PAGE_SIZE);
  const issueRows = flagged.slice(issueStart, issueStart + TREND_ISSUE_PAGE_SIZE);
  const busy = view.trendLoading;
  const sourcePartial = view.session?.scan && !view.session.scan.complete;
  const unknown = points.filter(point => point.context === 'budget_exhausted').length;
  return <section id="tag-trends" className="trend-panel" aria-label="Tag 趋势与异常定位" aria-busy={busy}>
    <div className="trend-heading"><div><p className="eyebrow">文件顺序 · 容器 TAG</p><h2>趋势观察与异常定位</h2></div><div className="panel-heading-links">{view.timeSearchPage && <a href="#time-search">返回时间戳检索 ↑</a>}<span className="trend-budget">每窗 256 Tag · 按需查看</span></div></div>
    <form className="trend-window-form" onSubmit={event => { event.preventDefault(); controller.loadTagTrends(start); }}>
      <label htmlFor="trend-start">窗口起始 Tag ID</label><input id="trend-start" inputMode="numeric" value={start} disabled={disabled} onChange={event => setStart(event.target.value)} />
      <button className="primary" disabled={disabled}>{page ? '查看指定窗口' : '查看趋势'}</button>
      {page && <div className="trend-window-buttons"><button type="button" disabled={disabled || busy || page.bounds.start === '0'} onClick={() => controller.pageTagTrends('first')}>趋势首页</button><button type="button" disabled={disabled || busy || page.bounds.start === '0'} onClick={() => controller.pageTagTrends('previous')}>上一趋势窗口</button><button type="button" disabled={disabled || busy || unsigned(page.bounds.next) >= unsigned(page.bounds.total)} onClick={() => controller.pageTagTrends('next')}>下一趋势窗口</button></div>}
    </form>
    {view.trendError && <div className="request-error" role="alert"><span>{message(view.trendError)}</span><button disabled={disabled || busy} onClick={() => controller.retryTagTrends()}>重试趋势查询</button></div>}
    {busy && <p role="status" className="trend-note">正在读取趋势窗口…</p>}
    {!page && !busy && !view.trendError && <p className="trend-note">查看 DTS 回退、重复、大间隔和结构问题，再定位到对应 Tag 与原始字节。</p>}
    {page && <>
      <p className="trend-coverage" role="status">趋势窗口 Tag [{page.bounds.start}, {page.bounds.next}) · {page.points.length} 条 / 已索引 {page.bounds.total}{sourcePartial ? ' · 部分索引，仅覆盖已保留 Tag' : ''}</p>
      <div className="trend-controls">
        <div className="trend-metrics" role="group" aria-label="趋势指标">{Object.entries(metricNames).map(([value, label]) => <button key={value} type="button" aria-pressed={metric === value} onClick={() => controller.setTrendOptions({ trendMetric: value as TrendMetric })}>{label}</button>)}</div>
        <label htmlFor="trend-type">图中类型<select id="trend-type" value={type} onChange={event => controller.setTrendOptions({ trendType: event.target.value as TrendType })}>{Object.entries(trendTypeNames).map(([value, label]) => <option key={value} value={value}>{label}</option>)}</select></label>
        <label htmlFor="trend-point">定位本窗口 Tag<select id="trend-point" value={points.some(point => point.tagId === view.trendSelectedId) ? view.trendSelectedId! : ''} disabled={readDisabled || busy || !points.length} onChange={event => { if (event.target.value) controller.selectTrendTag(event.target.value); }}><option value="">选择一个 Tag</option>{points.map(point => <option key={point.tagId} value={point.tagId}>Tag {point.tagId} · {trendTypeNames[trendType(point)]}</option>)}</select></label>
        <form onSubmit={event => { event.preventDefault(); try { trendGap(gapDraft); controller.setTrendOptions({ trendGapThreshold: gapDraft }); setGapError(false); } catch { setGapError(true); } }}>
          <label htmlFor="trend-gap">大间隔阈值 (ms)<input id="trend-gap" inputMode="numeric" value={gapDraft} onChange={event => setGapDraft(event.target.value)} /></label><button>应用阈值</button>
        </form>
      </div>
      {gapError && <p className="problem" role="alert">阈值请输入 1–4294967295 的十进制整数，不带前导零；仍使用 {gap} ms。</p>}
      <p className="trend-note">横轴为文件中的 Tag ID。DTS 差按相同类型及 Stream ID 的最近前项计算，包含配置与 filtered Tag；大间隔指正向差值 &gt; {gap} ms。时间提示不等于媒体损坏。</p>
      {points.length ? <TrendChart page={page} points={points} metric={metric} gap={BigInt(gap)} selectedId={view.trendSelectedId} disabled={readDisabled || busy} onSelect={id => controller.selectTrendTag(id)} /> : <p className="trend-placeholder">{page.points.length ? '本窗口没有所选类型的 Tag。' : '本窗口没有 Tag，已到当前索引末尾。'}</p>}
      <p className="trend-note">音频 <i className="trend-key audio" /> 视频 <i className="trend-key video" /> 脚本 <i className="trend-key script" /> 其他 <i className="trend-key other" /> · 橙色外圈为观察项。{metric === 'size' ? '实点与实线是实际载荷，空心点与虚线是声明大小；缺失字节不补齐。' : '同一类型和 Stream ID 内按源顺序连线。'}{metric === 'delta' && ' 无已知前驱的点不绘制差值。'}</p>
      <p className="trend-note">前驱回查范围 [{page.contextStart}, {page.bounds.start})，检查 {page.contextScanned} 条。{unknown ? `本窗口当前类型有 ${unknown} 个 Tag 的前驱因回查上限未知，未推断时间异常。` : '索引起点之前没有前驱时，时间差显示“—”。'} 类型选择只改变本窗口显示。</p>
      {selected && <div className="trend-selection" aria-label="当前趋势 Tag"><div><strong>Tag {selected.tagId} · {trendTypeNames[trendType(selected)]} · Stream ID {selected.streamId}</strong><p>DTS {selected.dts} ms · 差值 {selected.deltaMs ?? '—'} ms · 载荷 {selected.availableDataSize} / {selected.dataSize} byte · offset {selected.offset}{selected.filtered ? ' · filtered' : ''}</p><p>{trendContext(selected)}</p></div><div className="trend-selection-actions"><button disabled={readDisabled || busy} onClick={() => controller.selectTrendTag(selected.tagId)}>读取 Tag 详情</button><button disabled={readDisabled || busy} onClick={() => controller.showTrendHex(selected.tagId, 'tag')}>查看 Tag Hex</button><button disabled={readDisabled || busy} onClick={() => controller.showTrendHex(selected.tagId, 'payload')}>查看 Payload Hex</button>{view.selectedTag?.id === selected.tagId && <a href="#tag-detail">前往当前详情 ↓</a>}</div></div>}
      <div className="trend-issues-heading"><h3>本窗口异常与时间提示 <span>({flagged.length} 个 Tag)</span></h3><span>按源顺序 · 每页 {TREND_ISSUE_PAGE_SIZE} 项</span></div>
      {issueRows.length ? <div className="trend-issues-scroll"><table className="trend-issues"><thead><tr><th>Tag / 定位</th><th>观察项</th><th>DTS / 差值 (ms)</th><th>前驱依据</th><th>实际 / 声明载荷 (byte)</th><th>原始字节</th></tr></thead><tbody>{issueRows.map(({ point, issues }) => <tr key={point.tagId} className={view.trendSelectedId === point.tagId ? 'selected-tag' : ''}><td><button className="source-link" disabled={readDisabled || busy} aria-label={`从趋势异常定位 Tag ${point.tagId}`} onClick={() => controller.selectTrendTag(point.tagId)}>Tag {point.tagId}</button><span className="trend-row-type">{trendTypeNames[trendType(point)]} · Stream {point.streamId}</span></td><td>{issues.map(issue => <span className="trend-issue" key={issue}>{trendIssueNames[issue]}</span>)}</td><td>{point.dts} / {point.deltaMs ?? '—'}</td><td>{point.previousTagId === null ? '—' : `Tag ${point.previousTagId} · ${point.previousDts} ms`}</td><td>{point.availableDataSize} / {point.dataSize}</td><td><button className="source-link" disabled={readDisabled || busy} aria-label={`从趋势异常查看 Tag ${point.tagId} Hex`} onClick={() => controller.showTrendHex(point.tagId, 'tag')}>Tag Hex</button><button className="source-link" disabled={readDisabled || busy} aria-label={`从趋势异常查看 Tag ${point.tagId} Payload Hex`} onClick={() => controller.showTrendHex(point.tagId, 'payload')}>Payload Hex</button></td></tr>)}</tbody></table></div> : <p className="trend-note">本窗口所显示类型没有命中这些观察项；这不代表整文件或媒体语义正常。</p>}
      {flagged.length > TREND_ISSUE_PAGE_SIZE && <div className="pagination"><span>观察项 {issueStart + 1}–{Math.min(issueStart + TREND_ISSUE_PAGE_SIZE, flagged.length)} / {flagged.length}</span><div><button disabled={issueStart === 0} onClick={() => setIssuePage(Math.max(0, issueStart / TREND_ISSUE_PAGE_SIZE - 1))}>上一观察页</button><button disabled={issueStart + TREND_ISSUE_PAGE_SIZE >= flagged.length} onClick={() => setIssuePage(issueStart / TREND_ISSUE_PAGE_SIZE + 1)}>下一观察页</button></div></div>}
      <p className="trend-note">图和观察项仅覆盖当前窗口，沿用原始时间戳，不展开回绕。点击 Tag 会打开其普通索引与详情并退出 Tag 筛选；趋势窗口保留。<a href="#tag-trends">返回趋势窗口</a></p>
    </>}
  </section>;
}

function TrendChart({ page, points, metric, gap, selectedId, disabled, onSelect }: {
  page: TagTrendPage; points: TagTrendPoint[]; metric: TrendMetric; gap: bigint; selectedId: string | null; disabled: boolean; onSelect: (id: string) => void;
}) {
  const domain = trendDomain(points, metric);
  const hitWidth = trendHitWidth(page.bounds.start, page.bounds.next);
  const groups = new Map<string, TagTrendPoint[]>();
  for (const point of points) { const key = `${point.typeCode}:${point.streamId}`; const group = groups.get(key) ?? []; group.push(point); groups.set(key, group); }
  const x = (point: TagTrendPoint) => trendX(point.tagId, page.bounds.start, page.bounds.next);
  const path = (group: TagTrendPoint[], declared = false) => {
    let drawing = false;
    return group.map(point => {
      const value = declared ? BigInt(point.dataSize) : trendValue(point, metric);
      if (value === null) { drawing = false; return ''; }
      const part = `${drawing ? 'L' : 'M'}${x(point).toFixed(3)},${trendY(value, domain).toFixed(3)}`;
      drawing = true; return part;
    }).join(' ');
  };
  return <div className="trend-chart-scroll"><svg className="trend-chart" viewBox={`0 0 ${TREND_PLOT.width} ${TREND_PLOT.height}`} role="group" aria-label={`${metricNames[metric]}趋势图，点击或按回车选择 Tag`}>
    <title>{metricNames[metric]} · 源顺序 Tag [{page.bounds.start}, {page.bounds.next})</title>
    {[domain.max, (domain.max + domain.min) / 2n, domain.min].map((tick, index) => <g key={index}><line className="trend-grid" x1={TREND_PLOT.left} x2={TREND_PLOT.right} y1={trendY(tick, domain)} y2={trendY(tick, domain)} /><text x={TREND_PLOT.left - 12} y={trendY(tick, domain) + 4} textAnchor="end">{tick.toString()}</text></g>)}
    <text x={TREND_PLOT.left} y="14">{metric === 'size' ? 'byte' : 'ms'}</text><text x={TREND_PLOT.left} y="224">{page.bounds.start}</text><text x={TREND_PLOT.right} y="224" textAnchor="end">{(unsigned(page.bounds.next) - 1n).toString()}</text><text x="535" y="241" textAnchor="middle">Tag ID · 文件顺序</text>
    {[...groups].map(([key, group]) => <g key={key} className={`trend-series ${trendType(group[0])}`}><path d={path(group)} />{metric === 'size' && <path className="declared" d={path(group, true)} />}</g>)}
    {points.map(point => {
      const value = trendValue(point, metric);
      if (value === null) return null;
      const issues = trendIssues(point, gap), chosen = selectedId === point.tagId;
      const label = `趋势点 Tag ${point.tagId}，${trendTypeNames[trendType(point)]}，${metricNames[metric]} ${value}，${issues.map(issue => trendIssueNames[issue]).join('、') || '无观察项'}`;
      return <g key={point.tagId} className={`trend-dot ${trendType(point)}${issues.length ? ' has-issue' : ''}${chosen ? ' is-selected' : ''}`}>
        {metric === 'size' && <circle className="trend-declared-point" cx={x(point)} cy={trendY(BigInt(point.dataSize), domain)} r="4" aria-hidden="true" />}
        <circle cx={x(point)} cy={trendY(value, domain)} r={chosen ? 6 : 3.5} />
        <rect className="trend-hit" x={x(point) - hitWidth / 2} y={trendY(value, domain) - 9} width={hitWidth} height="18" role="button" aria-label={label} aria-pressed={chosen} aria-disabled={disabled} tabIndex={disabled ? -1 : 0} onClick={() => { if (!disabled) onSelect(point.tagId); }} onKeyDown={event => { if (!disabled && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); onSelect(point.tagId); } }}><title>{label} · offset {point.offset}</title></rect>
      </g>;
    })}
  </svg></div>;
}
