import type { Controller, ViewState } from './controller';
import type { SummaryKind, SummaryMetric, SummaryOverview } from './protocol';
import { progressPercent, unsigned } from './numbers';

export const summaryListNames: Record<SummaryKind, string> = { keyframes: '声明关键帧', aac: 'AAC 配置', avc: 'AVC 配置', mp3: 'MP3 帧头', diagnostics: '汇总诊断' };
const sectionNames: Record<string, string> = {
  tags: 'Tag 分布', audioCodecs: '音频编码', audio: '音频载荷', video: '视频载荷', mp3: 'MP3 载荷',
  audioTiming: '音频 DTS', videoTiming: '视频 DTS', mp3Timing: 'MP3 DTS', videoPts: '视频 PTS',
  audioBitrate: '音频区间码率', videoBitrate: '视频区间码率', mp3Bitrate: 'MP3 区间码率',
  keyframes: '声明关键帧', gop: 'GOP', aac: 'AAC 配置', avc: 'AVC 配置', mp3Headers: 'MP3 各 Tag 首帧头', mp3Frames: 'MP3 全帧检查', composition: '合成时间偏移', avSync: '音视频时间范围',
};
const metricNames: Record<string, string> = {
  completeFrameCount: '完整帧边界数', attemptedHeaderCount: '已检查帧头数', frameBytes: '完整帧字节', unresolvedBytes: '未确认字节',
  completeTagCount: '边界检查完成 Tag', partialTagCount: '边界检查不完整 Tag', truncatedHeaderCount: '截断帧头', truncatedFrameCount: '截断帧',
  unsupportedLayerCount: '不支持的 MPEG 层', budgetStoppedTagCount: '预算停止 Tag', maxFrames: '帧头检查上限', scanComplete: '索引完整',
  durationSeconds: '完整帧采样时长（精确分数）', frameIndex: 'Tag 内帧序号', frameStatus: '帧边界状态', sourceBitLength: '源范围长度',
  total: '总数', audio: '音频', video: '视频', script: '脚本', other: '其他', dataSizeBytes: '声明载荷字节',
  aacTagCount: 'AAC Tag', mp3TagCount: 'MP3 Tag', otherTagCount: '其他编码 Tag', unknownTagCount: '编码未知 Tag', switchCount: '编码切换',
  tagCount: 'Tag 数', indexedDataSizeBytes: '索引载荷字节', mediaTagCount: '媒体 Tag 数', mediaDataSizeBytes: '媒体载荷字节', excludedTagCount: '排除 Tag 数', unclassifiedTagCount: '未分类 Tag 数',
  sampleCount: '样本数', firstMs: '首个时间戳', lastMs: '最后时间戳', minMs: '最小值', maxMs: '最大值', rangeMs: '时间范围', forwardDeltaTotalMs: '正向间隔累计', regressionCount: '时间戳回退', duplicateCount: '重复时间戳', largestForwardGapMs: '最大正向间隔',
  basis: '计算口径', coveredBytes: '覆盖字节', coveredIntervalMs: '覆盖区间', averageBitsPerSecond: '平均区间码率',
  declaredCount: '声明关键帧数', count: '数量', closedDurationCount: '已闭合时长数', nonPositiveDurationCount: '非正时长数', minDurationMs: '最短时长', maxDurationMs: '最长时长', averageDurationMs: '平均时长',
  eventCount: '观测事件数', analyzedCount: '已分析数', skippedByBudgetCount: '预算限制跳过数', changeCount: '配置变化数',
  validCount: '有效帧头', malformedCount: '损坏帧头', malformedHeaderCount: '损坏帧头', freeFormatCount: '自由格式', parameterChangeCount: '参数变化数', bitrateChangeCount: '码率变化数',
  negativeCount: '负偏移数', zeroCount: '零偏移数', positiveCount: '正偏移数', startExtremaSkewMs: '起点极值偏差', endExtremaSkewMs: '终点极值偏差', rangesOverlap: '时间范围重叠', overlapMs: '重叠时长',
  pts: 'PTS', gopDurationMs: 'GOP 时长', complete: '解析完成', fingerprintSHA256: '配置 SHA-256', changedFromPrevious: '相对前项变化',
  audioObjectType: '音频对象类型', samplingFrequencyHz: '采样率', extensionSamplingFrequencyHz: '扩展采样率', channelConfiguration: '声道配置', channels: '声道数',
  profileIDC: 'Profile IDC', levelIDC: 'Level IDC', lengthSize: 'NAL 长度字段字节数', completeSPSCount: '完整 SPS 数', distinctSPSDimensions: '不同 SPS 尺寸数', displayWidth: '显示宽度', displayHeight: '显示高度',
  version: 'MPEG 版本', channelMode: '声道模式', samplesPerFrame: '每帧采样数', bitrateMode: '码率类型', bitrateKbps: '帧头码率', parametersChangedFromPrevious: '参数相对前项变化', bitrateChangedFromPrevious: '码率相对前项变化', expected: '期望值', actual: '实际值', recoveryOffset: '恢复偏移',
};
const eventNames: Record<string, string> = { declaredKeyframe: '声明关键帧', aacConfiguration: 'AAC 配置', avcConfiguration: 'AVC 配置', mp3Header: 'MP3 首帧头', mp3Frame: 'MP3 帧边界' };
const units: Record<string, string> = { tags: 'Tag', count: '项', byte: 'byte', ms: 'ms', bps: 'bit/s', Hz: 'Hz', kbps: 'kbit/s', px: 'px' };
export function summaryMetricValue(metric: SummaryMetric): string {
  if (metric.value === null) return '—';
  const value = metric.value === 'true' ? '是' : metric.value === 'false' ? '否' : metric.value;
  return metric.unit ? `${value} ${units[metric.unit] ?? metric.unit}` : value;
}
function value(overview: SummaryOverview, section: string, name: string): string {
  const metric = overview.metrics.find(item => item.section === section && item.name === name);
  return metric?.value ?? '—';
}
function MetricTable({ metrics }: { metrics: SummaryMetric[] }) {
  return <dl className="summary-facts">{metrics.map((metric, index) => <div key={`${metric.section}-${metric.name}-${index}`}><dt>{metricNames[metric.name] ?? metric.name}</dt><dd>{summaryMetricValue(metric)}</dd></div>)}</dl>;
}
export function SummaryPanel({ view, controller, disabled, message }: { view: ViewState; controller: Controller; disabled: boolean; message: (code: string) => string }) {
  const overview = view.summary, page = view.summaryPage;
  const list = overview?.lists.find(item => item.kind === view.summaryKind);
  const percent = progressPercent(view.summaryProcessed, view.summaryTotal);
  const sections = overview ? [...new Set(overview.metrics.map(item => item.section))] : [];
  return <section className="summary-panel" aria-label="媒体概览" aria-busy={view.summaryRunning}>
    <div className="summary-heading"><div><p className="eyebrow">MEDIA SUMMARY</p><h2>媒体概览</h2></div><div className="actions">
      <button className="primary" disabled={disabled || view.summaryRunning} onClick={() => controller.startSummary()}>{overview || view.summaryState ? '重新分析媒体' : '分析媒体'}</button>
      {view.summaryRunning && <button disabled={disabled || view.summaryCancelRequested} onClick={() => controller.cancelSummary()}>{view.summaryCancelRequested ? '正在确认取消…' : '取消媒体分析'}</button>}
    </div></div>
    {!overview && !view.summaryState && <p className="summary-note">按需汇总已索引的 Tag，查看编码、时间戳、区间码率和配置变化，再定位到详情与原始字节。</p>}
    {view.summaryRunning && <>
      <div className="progress-line"><progress max="100" value={percent} aria-label="媒体分析进度" /><span>{view.summaryProcessed} / {view.summaryTotal} Tag</span></div>
      <p className="summary-note" role="status">{view.summaryCancelRequested ? '取消尚未确认完成；正在等待底层读取结束。' : view.summaryState === 'recovering' ? '正在确认原媒体分析请求…' : '正在分析媒体；完成或取消后可继续读取 Tag 与 Hex。'}关闭文件仍可用。</p>
    </>}
    {view.summaryError && <div className="request-error" role="alert"><span>{message(view.summaryError)}</span>{view.summaryRetry && <button disabled={disabled} onClick={() => controller.retrySummary()}>{view.summaryRetry === 'reload' ? '重试媒体分析' : view.summaryCancelRequested ? '确认取消媒体分析' : '确认媒体分析结果'}</button>}</div>}
    {overview && <>
      <div className="summary-coverage" role="status"><span className={`status ${overview.scanComplete ? '' : 'partial'}`}>{overview.scanComplete ? '索引完整' : '部分索引'}</span><span className={`status ${overview.complete ? '' : 'partial'}`}>{overview.complete ? '覆盖范围内分析完成' : '覆盖范围内部分分析'}</span><span>覆盖 {overview.indexedTags} Tag · 原文件 {overview.sourceSize} byte</span>{(overview.scanHasErrors || overview.hasErrors) && <span className="problem">{overview.scanHasErrors ? '存在结构错误' : ''}{overview.scanHasErrors && overview.hasErrors ? ' · ' : ''}{overview.hasErrors ? '存在媒体分析错误' : ''}</span>}</div>
      {!overview.scanComplete && <p className="summary-note">这些数值只覆盖已提交的索引前缀，不代表整个文件。</p>}
      <div className="summary-cards">
        <div><span>音频 / 视频 / 脚本 / 其他 Tag</span><strong>{['audio', 'video', 'script', 'other'].map(name => value(overview, 'tags', name)).join(' / ')}</strong></div>
        <div><span>AAC / MP3 Tag · 编码切换</span><strong>{value(overview, 'audioCodecs', 'aacTagCount')} / {value(overview, 'audioCodecs', 'mp3TagCount')} · {value(overview, 'audioCodecs', 'switchCount')}</strong></div>
        <div><span>声明关键帧 · 平均 GOP 时长</span><strong>{value(overview, 'keyframes', 'declaredCount')} · {value(overview, 'gop', 'averageDurationMs')} ms</strong></div>
        <div><span>AAC / AVC 配置变化</span><strong>{value(overview, 'aac', 'changeCount')} / {value(overview, 'avc', 'changeCount')}</strong></div>
        <div><span>MP3 完整帧 / 未确认字节</span><strong>{value(overview, 'mp3Frames', 'completeFrameCount')} / {value(overview, 'mp3Frames', 'unresolvedBytes')}</strong></div>
        <div><span>MP3 逐帧参数 / 声明码率变化</span><strong>{value(overview, 'mp3Frames', 'parameterChangeCount')} / {value(overview, 'mp3Frames', 'bitrateChangeCount')}</strong></div>
        <div><span>音视频起点 / 终点极值偏差</span><strong>{value(overview, 'avSync', 'startExtremaSkewMs')} / {value(overview, 'avSync', 'endExtremaSkewMs')} ms</strong></div>
      </div>
      <div className="summary-table-area"><table className="summary-timing"><thead><tr><th>时间轴</th><th>最小 / 最大 (ms)</th><th>回退 / 重复</th><th>平均区间码率 (bit/s)</th></tr></thead><tbody>{['audio', 'video', 'mp3', 'videoPts'].map(track => {
        const timing = track === 'videoPts' ? track : track + 'Timing';
        return <tr key={track}><td>{sectionNames[timing]}</td><td>{value(overview, timing, 'minMs')} / {value(overview, timing, 'maxMs')}</td><td>{value(overview, timing, 'regressionCount')} / {value(overview, timing, 'duplicateCount')}</td><td>{track === 'videoPts' ? '—' : value(overview, track + 'Bitrate', 'averageBitsPerSecond')}</td></tr>;
      })}</tbody></table></div>
      {overview.metrics.some(metric => metric.section === 'mp3Frames') && <p className="summary-note">MP3 按各 Tag 内连续帧边界逐帧检查，事件中的 DTS 属于所在 Tag。完整帧采样时长不扣除编码延迟或填充，不代表解码后播放时长；自由格式、截断和预算外字节保留未知，事件省略后仍继续汇总。</p>}
      <p className="summary-note">关键帧依据封装声明；区间码率只计算可归属时间区间的媒体载荷。音视频偏差是时间范围的数值比较，不代表播放同步质量。未知值显示“—”。</p>
      <details className="summary-all-metrics"><summary>全部指标与计算覆盖范围（{overview.metrics.length} 项）</summary><div className="summary-metric-groups">{sections.map(section => <section key={section}><h3>{sectionNames[section] ?? section}</h3><MetricTable metrics={overview.metrics.filter(metric => metric.section === section)} /></section>)}</div></details>
      <div className="summary-events"><h3>事件与定位</h3><div className="tabs" role="tablist" aria-label="媒体事件类型">{overview.lists.map(item => <button key={item.kind} role="tab" aria-selected={view.summaryKind === item.kind} disabled={disabled} onClick={() => controller.selectSummaryKind(item.kind)}>{summaryListNames[item.kind]} ({item.retained})</button>)}</div>
        <p className="summary-note">观测总数 {list?.observed ?? '未知（已省略）'} · 实际保留 {list?.retained ?? '—'}{list?.truncated ? ' · 事件受保留上限限制，部分事件已省略' : ''} · 每页最多 100 条</p>
        {view.summaryPageError && <div className="request-error" role="alert"><span>{message(view.summaryPageError)}</span><button disabled={disabled} onClick={() => controller.retrySummaryPage()}>重试媒体事件页</button></div>}
        <div className="summary-table-area" aria-busy={view.summaryPageLoading}>{view.summaryPageLoading ? <p className="summary-note" role="status">正在读取媒体事件页…</p> : page?.events.length ? <table><thead><tr><th>事件 / 值</th><th>Tag / 详情</th><th>原始位置 : bit</th><th>DTS (ms)</th></tr></thead><tbody>{page.events.map(event => <tr key={event.id}><td><strong>{eventNames[event.title] ?? event.title}</strong>{event.severity && <span className={`severity severity-${event.severity}`}>{event.severity}</span>}{event.message && <p>{event.message}</p>}{event.values.length > 0 && <details><summary>事件参数</summary><MetricTable metrics={event.values} /></details>}</td><td>{event.tagId === null ? '—' : <button className="source-link" disabled={disabled} aria-label={`从媒体事件跳转 Tag ${event.tagId}`} onClick={() => controller.locateSummaryEvent(event, 'tag')}>{event.tagId}</button>}</td><td><button className="source-link" disabled={disabled} aria-label={`定位媒体事件 ${event.id} 原始字节`} onClick={() => controller.locateSummaryEvent(event, 'hex')}>{event.offset} : {event.bitOffset}</button></td><td>{event.dts ?? '—'}</td></tr>)}</tbody></table> : <p className="summary-note">当前类别没有保留的事件。</p>}</div>
        <div className="pagination"><span>{page ? `起点 ${page.bounds.start} · 本页 ${page.events.length} 条 · 保留 ${page.bounds.total} 条` : '尚无媒体事件页'}</span><div>
          <button disabled={disabled || !page || view.summaryPageLoading || page.bounds.start === '0'} onClick={() => controller.pageSummary('first')}>媒体事件首页</button>
          <button disabled={disabled || !page || view.summaryPageLoading || view.summaryPrevious === null} onClick={() => controller.pageSummary('previous')}>媒体事件上一页</button>
          <button disabled={disabled || !page || view.summaryPageLoading || unsigned(page.bounds.next) >= unsigned(page.bounds.total)} onClick={() => controller.pageSummary('next')}>媒体事件下一页</button>
        </div></div>
      </div>
    </>}
  </section>;
}
