import type { Controller, ViewState } from './controller';
import type { MediaCurvePoint } from './protocol';
import { unsigned } from './numbers';
import { curveDomain, curveMetricNames, curveTag, curveValue, curveY } from './media-curves';
import type { CurveMetric } from './media-curves';

export function MediaCurvePanel({ view, controller, disabled, message }: {
  view: ViewState; controller: Controller; disabled: boolean; message: (code: string) => string;
}) {
  const page = view.curvePage, metric = view.curveMetric, busy = view.curveLoading;
  const ready = !disabled && view.summaryState === 'ready';
  const selected = page?.points.find(point => point.index === view.curveSelectedIndex);
  const selectedTag = selected ? curveTag(selected, metric) : null;
  const unit = metric === 'gopAverage' || metric === 'gopMax' ? 'ms' : 'bps';
  const show = (value: string | null) => value ?? '未知';
  return <section className="curve-panel" aria-label="媒体码率与 GOP 曲线" aria-busy={busy}>
    <div className="summary-heading"><div><p className="eyebrow">MEDIA CURVES · 源 TAG 窗口</p><h2>媒体码率与 GOP 曲线</h2></div><button className="primary" disabled={!ready || busy} onClick={() => controller.loadMediaCurves()}>查看媒体曲线</button></div>
    <p className="summary-note">每 256 个源 Tag 聚合，非固定时间桶，也非解码帧。曲线覆盖全部已索引窗口，按需加载当前页；区间与 GOP 归到闭合它的 Tag 所在窗口，可能起于前窗。分页不重读源文件。</p>
    <p className="summary-note">音频码率包含 MP3；码率使用已覆盖字节与已知时长；无可用的已知时长时显示“未知”，不补零。GOP 依据封装中的关键帧声明，不保证 IDR。定位使用码率的首个正向闭合 Tag，或 GOP 最长时长的闭合 Tag。导出诊断 JSON v2 会保存当前已加载的曲线页和指标。</p>
    {!ready && !page && <p className="trend-note">请先完成媒体概览分析，再查看曲线。</p>}
    {view.curveError && <div className="request-error" role="alert"><span>{message(view.curveError)}</span><button disabled={!ready || busy} onClick={() => controller.retryMediaCurves()}>重试媒体曲线</button></div>}
    {busy && <p role="status">正在读取媒体曲线页…</p>}
    {page && <>
      <p className="trend-coverage" role="status">窗口 [{page.bounds.start}, {page.bounds.next}) / {page.bounds.total} · 已索引 {page.indexedTags} Tags · 本页 {page.points.length} 点</p>
      {(!page.scanComplete || !page.complete) && <p className="problem" role="status">{!page.scanComplete ? '部分索引：仅覆盖已保留的源 Tag。' : ''}{!page.complete ? '媒体分析不完整：曲线可能缺少未识别或未完成的区间。' : ''}不能据此判断整文件媒体正常。</p>}
      <div className="trend-controls"><div className="trend-metrics" role="group" aria-label="媒体曲线指标">{(Object.keys(curveMetricNames) as CurveMetric[]).map(value => <button key={value} aria-pressed={metric === value} onClick={() => controller.setCurveMetric(value)}>{curveMetricNames[value]}</button>)}</div></div>
      {page.points.length ? <CurveChart points={page.points} metric={metric} selected={view.curveSelectedIndex} disabled={!ready || busy} onSelect={index => controller.selectCurvePoint(index)} /> : <p className="trend-placeholder">当前页没有媒体窗口，已到已索引范围末尾。</p>}
      {selected && <div className="trend-selection" aria-label="当前媒体曲线窗口"><div><strong>窗口 {selected.index} · Tag [{selected.startTag}, {selected.nextTag})</strong><p>{curveMetricNames[metric]}：{curveValue(selected, metric)?.toString() ?? '未知'} {unit} · 归属 Tag {selectedTag ?? '未知'} · 未分类 {selected.unclassifiedTags} Tags</p>
        {metric === 'gopAverage' || metric === 'gopMax' ? <p>关键帧声明 {selected.gop.declaredCount} · 已闭合 GOP {selected.gop.closedCount} · 总时长 {selected.gop.totalDurationMs} ms · 最短 {show(selected.gop.minDurationMs)} / 最长 {show(selected.gop.maxDurationMs)} / 平均 {show(selected.gop.averageDurationMs)} ms</p> : <p>覆盖 {selected[metric].coveredBytes} byte / {selected[metric].coveredIntervalMs} ms</p>}
      </div><div className="trend-selection-actions"><button disabled={!ready || busy || selectedTag === null} onClick={() => controller.selectCurvePoint(selected.index)}>定位闭合 Tag</button><button disabled={!ready || busy || selectedTag === null || view.selectedTag?.id !== selectedTag} onClick={() => controller.showCurveHex(selected.index)}>查看曲线 Tag Hex</button>{selectedTag !== null && view.selectedTag?.id !== selectedTag && <span className="trend-note">先定位 Tag，取得实际字节范围后可查看 Hex。</span>}{selectedTag !== null && view.selectedTag?.id === selectedTag && <a href="#tag-detail">前往当前详情 ↓</a>}</div></div>}
      {page.points.length > 0 && <details className="curve-data"><summary>本页精确数据（{page.points.length} 个窗口）</summary><div className="curve-table-scroll"><table><thead><tr><th>窗口 / 源 Tag</th><th>音频 bps</th><th>视频 bps</th><th>MP3 bps</th><th>GOP 平均 / 最长 ms</th><th>未分类 Tags</th></tr></thead><tbody>{page.points.map(point => <tr key={point.index} className={selected?.index === point.index ? 'selected-tag' : ''}><td><button className="source-link" disabled={!ready || busy} onClick={() => controller.selectCurvePoint(point.index)} aria-label={`选择媒体曲线窗口 ${point.index}`}>窗口 {point.index}</button><span className="trend-row-type">[{point.startTag}, {point.nextTag})</span></td><td>{show(point.audio.bitsPerSecond)}</td><td>{show(point.video.bitsPerSecond)}</td><td>{show(point.mp3.bitsPerSecond)}</td><td>{show(point.gop.averageDurationMs)} / {show(point.gop.maxDurationMs)}</td><td>{point.unclassifiedTags}</td></tr>)}</tbody></table></div></details>}
      <div className="pagination"><span>每页最多 64 个窗口 · 横轴按源 Tag 窗口顺序</span><div><button disabled={!ready || busy || page.bounds.start === '0'} onClick={() => controller.pageMediaCurves('first')}>曲线首页</button><button disabled={!ready || busy || page.bounds.start === '0'} onClick={() => controller.pageMediaCurves('previous')}>上一曲线页</button><button disabled={!ready || busy || unsigned(page.bounds.next) >= unsigned(page.bounds.total)} onClick={() => controller.pageMediaCurves('next')}>下一曲线页</button></div></div>
    </>}
  </section>;
}
function CurveChart({ points, metric, selected, disabled, onSelect }: {
  points: MediaCurvePoint[]; metric: CurveMetric; selected: string | null; disabled: boolean; onSelect: (index: string) => void;
}) {
  const max = curveDomain(points, metric), x = (index: number) => points.length === 1 ? 555 : 146 + index * 820 / (points.length - 1);
  let drawing = false;
  const path = points.map((point, index) => {
    const value = curveValue(point, metric);
    if (value === null) { drawing = false; return ''; }
    const part = `${drawing ? 'L' : 'M'}${x(index)},${curveY(value, max)}`; drawing = true; return part;
  }).join(' ');
  const known = points.filter(point => curveValue(point, metric) !== null).length;
  if (known === 0) return <p className="trend-placeholder">本页 {curveMetricNames[metric]} 全部未知，没有可绘制的已知时长区间；可在数据表选择窗口。</p>;
  return <div className="curve-chart-scroll"><svg className="curve-chart" viewBox="0 0 1000 252" role="group" aria-label={`${curveMetricNames[metric]}曲线，回车或空格选择窗口`}>
    <title>{curveMetricNames[metric]} · 窗口 {points[0].index} 至 {points.at(-1)!.index} · {points.length - known} 点未知</title>
    {[max, max / 2n, 0n].map((tick, i) => <g key={i}><line className="trend-grid" x1="146" x2="966" y1={curveY(tick, max)} y2={curveY(tick, max)} /><text x="135" y={curveY(tick, max) + 4} textAnchor="end">{tick.toString()}</text></g>)}
    <text x="146" y="16">{metric === 'gopAverage' || metric === 'gopMax' ? 'ms' : 'bps'} · {known} 个已知点</text><path className="curve-line" d={path} />
    {points.map((point, index) => {
      const value = curveValue(point, metric);
      if (value === null) return null;
      const label = `窗口 ${point.index}，Tag ${point.startTag} 至 ${point.nextTag}，${curveMetricNames[metric]} ${value}`;
      return <g key={point.index} className={`curve-dot ${selected === point.index ? 'is-selected' : ''}`}><circle cx={x(index)} cy={curveY(value, max)} r="3.5" /><rect x={x(index) - 6} y="24" width="12" height="181" className="trend-hit" role="button" tabIndex={disabled ? -1 : 0} aria-disabled={disabled} aria-label={label} aria-pressed={selected === point.index} onClick={() => { if (!disabled) onSelect(point.index); }} onKeyDown={event => { if (!disabled && (event.key === 'Enter' || event.key === ' ')) { event.preventDefault(); onSelect(point.index); } }}><title>{label}</title></rect></g>;
    })}
    <text x="146" y="226">窗口 {points[0].index}</text><text x="966" y="226" textAnchor="end">窗口 {points.at(-1)!.index}</text><text x="146" y="245">未知点不绘制；精确值和未知窗口可在下方数据表选择。</text>
  </svg></div>;
}
