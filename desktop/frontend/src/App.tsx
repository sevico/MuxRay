import { TimeSearchPanel } from './TimeSearchPanel';
import { CodecSearchPanel } from './CodecSearchPanel';
import { AVCSearchPanel } from './AVCSearchPanel';
import { MP3FramePanel, MP3DependencyPanel, KeyframeEvidencePanel } from './CodecDetailPanel';
import { useEffect, useState, useSyncExternalStore } from 'react';
import { TagTrendPanel } from './TagTrendPanel';
import { Controller, PAGE_SIZE } from './controller';
import type { Diagnostic, Session, Tag } from './protocol';
import type { ViewState } from './controller';
import { avcReplayProgress, detailRows, fieldValue } from './details';
import { byteSize, previousStart, progressPercent, unsigned } from './numbers';
import { selectionFromSpan } from './hex';
import type { HexSelection } from './hex';
import type { MP3DependencySource } from './codec-details';
import { HexPanel } from './HexPanel';
import { MediaCurvePanel } from './MediaCurvePanel';
import { SummaryPanel } from './SummaryPanel';
import { FilterPagination, TagFilterPanel } from './TagFilterPanel';
import { SnapshotStatus } from './SnapshotStatus';

const stateNames: Record<string, string> = {
  opening: '正在打开', running: '扫描中', canceling: '正在取消', finished: '扫描结束', canceled: '已取消', failed: '扫描失败',
};
const errors: Record<string, string> = {
  busy: '当前任务尚未释放，请稍后重试。', not_ready: '桌面或扫描结果尚未就绪。',
  source_not_found: '找不到所选文件。', source_permission_denied: '没有权限读取所选文件。',
  open_failed: '无法打开文件，请确认它是可读取的普通文件。', scan_failed: '扫描遇到读取或资源清理错误。',
  shutting_down: '应用正在退出。', dialog_failed: '文件选择窗口打开失败。',
  avc_search_failed: 'H.264 检索失败，可以重试当前批次。', avc_search_not_found: '尚未确认 H.264 检索是否已启动。', avc_search_unsupported: '当前宿主不支持 H.264 检索。',
  avc_search_cancel_unconfirmed: '尚未确认取消，请继续确认状态。', avc_search_wait_paused: '本批仍未完成，可继续确认状态或取消。',
  time_search_failed: '本批时间戳检索失败，可重试当前批次。', time_search_not_found: '尚未确认时间戳检索是否已启动。',
  time_search_cursor_expired: '继续位置已失效，请从头重新检索。', time_search_unsupported: '当前宿主不支持时间戳检索。',
  time_search_cancel_unconfirmed: '尚未确认时间戳检索取消，请继续确认状态。', time_search_wait_paused: '本批仍未完成，可继续确认状态或取消。',
  codec_search_failed: '本批编解码器检索失败，可重试当前批次。', codec_search_not_found: '尚未确认编解码器检索是否已启动。',
  codec_search_cursor_expired: '继续位置已失效，请从头重新检索。', codec_search_unsupported: '当前宿主不支持编解码器检索。',
  codec_search_cancel_unconfirmed: '尚未确认编解码器检索取消，请继续确认状态。', codec_search_wait_paused: '本批仍未完成，可继续确认状态或取消。',
  canceled: '任务已取消。', deadline_exceeded: '等待已超时。',
  invalid_argument: '请求参数无效。', session_closed: '此文件已关闭。', session_not_found: '此会话已失效。',
  incompatible_api: '界面与宿主版本不匹配，请重新构建。', invalid_response: '宿主返回了不匹配的结果。',
  transport_failed: '桌面通信失败，请重试或重新启动应用。',
  stale_request: '此请求已被较新的操作取代。', id_exhausted: '请求编号已用尽，请重新启动应用。',
  detail_not_found: '尚未确认详情请求，请重试当前 Tag。',
  detail_unsupported: '此 Tag 暂不支持详情；当前支持 AMF0 Script Data、AAC 音频、AVC 视频和 MP3 音频。',
  detail_filtered: '此 Tag 带有过滤或加密标记，暂不解析详情。',
  detail_truncated: '此 Tag 的 payload 不完整，无法取得详情。',
  source_changed: '文件内容已改变，请关闭后重新打开，以保证字段位置准确。',
  tag_not_found: '当前索引中没有这个 Tag。', detail_failed: '详情解析失败，请重试当前 Tag。',
  detail_superseded: '已切换到原始字节读取；可重新读取此 Tag 的详情。',
  hex_not_found: '尚未确认原始字节请求，请重试查询。', hex_failed: '原始字节读取失败，请重试。',
  hex_unsupported: '当前源不支持原始字节读取。',
  summary_not_found: '尚未确认媒体分析请求，请确认原请求的结果。', summary_failed: '媒体分析失败；未发布未完成的汇总，可重新分析。',
  summary_unsupported: '当前源不支持媒体分析。', summary_wait_paused: '媒体分析等待已暂停；任务可能仍在读取，请确认结果或取消。',
  summary_cancel_unconfirmed: '尚未确认取消命令生效；请再次确认取消，或关闭文件。',
  source_unavailable: '源文件身份已不可用，请重新打开媒体后再导出。',
  source_overwrite: '保存目标是当前媒体文件或它的别名，请选择其他 JSON 文件。',
  target_changed: '保存目标在确认后发生变化，请重新选择保存位置。',
  write_failed: '无法写入诊断报告，请检查保存位置和可用空间后重新导出。',
  snapshot_failed: '无法生成诊断快照，请重新查询当前结果后再导出。',
};
export function fault(code: string): string { return errors[code] ?? '请求失败，请重试。'; }

function scanNote(session: Session): string {
  if (session.closed) return '正在释放当前文件；底层 I/O 结束后才能打开下一个文件。';
  if (session.failure && session.failure.code !== 'canceled') return fault(session.failure.code);
  if (!session.terminal) return '扫描期间仅更新进度；结束后按页读取结果。';
  if (!session.scan) return '文件未进入扫描，请关闭后重新选择。';
  if (!session.scan.complete) return '部分结果 · 索引未完整。已保留的 Tag 和诊断仍可查看。';
  if (session.scan.hasErrors) return '索引已结束，但发现结构错误；请查看诊断。';
  return '容器索引完整。这不代表所有音视频 payload 已通过语义验证。';
}

export function App({ controller }: { controller: Controller }) {
  const view = useSyncExternalStore(controller.subscribe, controller.getSnapshot);
  useEffect(() => () => controller.dispose(), [controller]);
  const [tagId, setTagId] = useState('');
  const { session, operation, page } = view;
  const progress = session?.progress;
  const scanReady = !!session?.terminal && !session.closed && session.scan !== null && !view.sourceChanged;
  const canPage = scanReady && !view.summaryRunning && !(view.avcSearchRunning || view.codecSearchRunning || view.timeSearchRunning);
  const bounds = page?.data.bounds;
  const percent = progress ? progressPercent(progress.bytesScanned, progress.sourceSize) : 0;
  const isClosing = operation === 'closing' || session?.closed;

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><span className="brand-mark" aria-hidden="true">M</span><div><h1>MuxRay <span>码流透镜</span></h1><p>FLV STRUCTURE INSPECTOR</p></div></div>
      <div className="actions">
        {session && <>
          <button onClick={() => void controller.saveSnapshot()} disabled={!scanReady || !!operation || view.savePending}>导出诊断 JSON</button>
          <button onClick={() => void controller.cancel()} disabled={!!operation || session.terminal || session.state === 'canceling'}>取消扫描</button>
          <button onClick={() => void controller.close()} disabled={!!operation}>关闭文件</button>
        </>}
        <button className="primary" onClick={() => void controller.open()} disabled={!!session || !!operation || view.savePending}>{operation === 'picking' ? '正在选择…' : operation === 'recovering' ? '正在确认打开结果…' : '打开 FLV 文件'}</button>
      </div>
    </header>

    <main>
      <SnapshotStatus view={view} controller={controller} message={fault} />
      {view.error && <div className="request-error" role="alert"><span>{fault(view.error)}</span>{!view.sourceChanged && <button onClick={() => controller.retry()}>重试查询</button>}</div>}
      {!session ? <section className="empty-state">
        <div className="file-symbol" aria-hidden="true">FLV</div>
        <h2>从一个文件开始，看清码流结构</h2>
        <p>选择本机 FLV 文件，查看 Tag 索引、时间戳和结构诊断。</p>
        <button className="primary" onClick={() => void controller.open()} disabled={!!operation || view.savePending}>选择 FLV 文件</button>
        <div className="empty-features"><span>只读分析</span><span>可取消扫描</span><span>按需分页</span></div>
        <p className="scope-note">按需查看 AMF0、AAC、AVC 与 MP3 字段 · 更多解析能力可通过 CLI 使用</p>
      </section> : <>
        <section className="scan-card" aria-label="扫描状态">
          <div className="scan-heading"><div><p className="eyebrow">当前文件</p><h2 title={view.fileName}>{view.fileName}</h2></div><span className={`status ${session.scan && !session.scan.complete ? 'partial' : ''}`}>{isClosing ? '正在关闭' : stateNames[session.state] ?? session.state}</span></div>
          <div className="metrics">
            <div><span>文件大小</span><strong title={progress?.sourceSize}>{progress ? byteSize(progress.sourceSize) : '—'}</strong></div>
            <div><span>已扫描</span><strong title={progress?.bytesScanned}>{progress ? byteSize(progress.bytesScanned) : '—'}</strong></div>
            <div><span>Tag 数</span><strong>{progress?.tagCount ?? '—'}</strong></div>
            <div><span>结构诊断</span><strong>{progress?.diagnosticCount ?? '—'}</strong></div>
          </div>
          <div className="progress-line"><progress max="100" value={percent} aria-label="扫描进度" /><span>{percent.toFixed(1)}%</span></div>
          <p className="scan-note" role="status">{scanNote(session)}</p>
          <p className="snapshot-scope">导出诊断 JSON 保存当前已载入的概览、事件页、Tag 详情、Hex、趋势、媒体曲线页与 H.264 检索页，不代表全文件分析。报告可能包含有限媒体字节和 AMF 字段文本。</p>
        </section>

        <CodecSearchPanel view={view} controller={controller} disabled={!scanReady || !!isClosing} message={fault} />
        <AVCSearchPanel view={view} controller={controller} disabled={!scanReady || !!isClosing} message={fault} />

        <SummaryPanel view={view} controller={controller} disabled={!scanReady || !!isClosing || (view.avcSearchRunning || view.codecSearchRunning || view.timeSearchRunning)} message={fault} />

        <MediaCurvePanel view={view} controller={controller} disabled={!scanReady || !!operation || (view.avcSearchRunning || view.codecSearchRunning || view.timeSearchRunning)} message={fault} />

        <TimeSearchPanel view={view} controller={controller} disabled={!scanReady || !!operation} message={fault} />

        <TagTrendPanel key={session.sessionId} view={view} controller={controller} disabled={!scanReady || !!operation || view.timeSearchRunning} readDisabled={!canPage || !!operation} message={fault} />

        <div className={`result-layout ${view.selectedTag ? 'has-detail' : ''}`}>
        <section className="results" aria-label="扫描结果">
          <div className="results-toolbar">
            <div className="tabs" role="tablist" aria-label="结果类型">
              <button role="tab" aria-selected={view.tab === 'tags'} onClick={() => controller.select('tags')} disabled={!canPage || isClosing}>Tag 索引</button>
              <button role="tab" aria-selected={view.tab === 'diagnostics'} onClick={() => controller.select('diagnostics')} disabled={!canPage || isClosing}>诊断 {progress ? `(${progress.diagnosticCount})` : ''}</button>
            </div>
            <span className="table-hint">每页最多 {PAGE_SIZE} 条 · 偏移单位 byte</span>
            {view.selectedTag && <a className="detail-jump" href="#tag-detail">当前详情 ↓</a>}
          </div>
          <form className="tag-jump-form" onSubmit={event => { event.preventDefault(); controller.jumpTag(tagId); }}>
            <label htmlFor="tag-id-input">Tag ID</label><input id="tag-id-input" inputMode="numeric" value={tagId} onChange={event => setTagId(event.target.value)} placeholder="精确 ID" disabled={!canPage || !!isClosing} />
            <button disabled={!canPage || !!isClosing}>跳转 Tag</button>{view.tagJumpError && <span className="problem" role="alert">{fault(view.tagJumpError)}</span>}
          </form>
          {view.tab === 'tags' && <TagFilterPanel key={session.sessionId} view={view} controller={controller} disabled={!canPage || !!isClosing} />}
          <div className="table-area" role="tabpanel" aria-busy={view.loading}>
            {view.loading ? <div className="result-placeholder" role="status">正在读取当前页…</div> : view.filteredPage ? <>
              {view.filteredPage.tags.length ? <TagTable rows={view.filteredPage.tags} selectedId={view.selectedTag?.id ?? null} disabled={!!isClosing || !canPage} onSelect={id => controller.selectTag(id)} onLocate={selection => controller.showHex(selection)} /> : <div className="result-placeholder">{view.filteredPage.exhausted ? '本次检查范围内没有匹配 Tag，已到当前索引末尾。' : '本次检查范围内没有匹配 Tag；仍有索引未检查。'}</div>}
            </> : page ? <>
              {page.tab === 'tags' ? <TagTable rows={page.data.tags} selectedId={view.selectedTag?.id ?? null} disabled={!!isClosing || !canPage} onSelect={id => controller.selectTag(id)} onLocate={selection => controller.showHex(selection)} /> : <DiagnosticTable rows={page.data.diagnostics} disabled={!!isClosing || !canPage} onLocate={selection => controller.showHex(selection)} />}
            </> : <div className="result-placeholder">{canPage ? '暂无可显示的页，请重试查询。' : '扫描结束后，完整或部分结果会显示在这里。'}</div>}
          </div>
          {view.tagFilter ? <FilterPagination view={view} controller={controller} disabled={!canPage || !!isClosing} /> : <div className="pagination"><span>{bounds ? `起点 ${bounds.start} · 本页 ${unsigned(bounds.next) - unsigned(bounds.start)} 条 · 共 ${bounds.total} 条` : '尚无结果页'}</span><div>
            <button disabled={!canPage || !bounds || view.loading || bounds.start === '0' || !!isClosing} onClick={() => controller.select(view.tab, '0')}>首页</button>
            <button disabled={!canPage || !bounds || view.loading || bounds.start === '0' || !!isClosing} onClick={() => controller.select(view.tab, previousStart(view.start, PAGE_SIZE))}>上一页</button>
            <button disabled={!canPage || !bounds || view.loading || unsigned(bounds.next) >= unsigned(bounds.total) || !!isClosing} onClick={() => bounds && controller.select(view.tab, bounds.next)}>下一页</button>
          </div></div>}
        </section>
        {view.selectedTag && <DetailPanel view={view} onRetry={() => controller.retryDetail()} onContinue={() => controller.continueAVCContext()} onSelectMP3={index => controller.selectMP3Frame(index)} onPageMP3={direction => controller.pageMP3Frames(direction)} onJumpMP3={text => controller.jumpMP3Frame(text)} onStepMP3={direction => controller.stepMP3Frame(direction)} onInspectMP3={() => controller.inspectMP3Dependency()} onLocateMP3={(source, destination) => controller.locateMP3Source(source, destination)} onLocate={selection => controller.showHex(selection)} />}
        </div>
        <HexPanel view={view} controller={controller} disabled={!canPage || !!isClosing} message={fault} />
      </>}
    </main>
    <footer><span><i aria-hidden="true" /> 源文件只读 · 不上传媒体</span><span>桌面预览版 / 单文件会话</span></footer>
  </div>;
}

function TagTable({ rows, selectedId, disabled, onSelect, onLocate }: { rows: Tag[]; selectedId: string | null; disabled: boolean; onSelect: (id: string) => void; onLocate: (selection: HexSelection) => void }) {
  if (!rows.length) return <div className="result-placeholder">当前索引没有 Tag。</div>;
  return <table><thead><tr><th>ID / 详情</th><th>类型</th><th>偏移</th><th>DTS (ms)</th><th>Payload (byte)</th><th>Payload</th><th>PreviousTagSize</th></tr></thead><tbody>{rows.map(tag => <tr key={tag.id} className={selectedId === tag.id ? 'selected-tag' : ''}>
    <td><button className="tag-select" aria-label={`查看 Tag ${tag.id} 详情`} aria-pressed={selectedId === tag.id} disabled={disabled} onClick={() => onSelect(tag.id)}>{tag.id}</button></td><td><span className={`tag-type type-${tag.typeCode}`}>{tag.typeName}</span>{tag.filter && ' · filtered'}</td><td><button className="source-link" aria-label={`查看 Tag ${tag.id} 原始字节`} disabled={disabled} onClick={() => onLocate(selectionFromSpan(tag.span, `Tag ${tag.id} 范围`))}>{tag.offset}</button></td><td>{tag.dts}</td><td><button className="source-link" aria-label={`定位 Tag ${tag.id} payload`} disabled={disabled} onClick={() => onLocate(selectionFromSpan(tag.payloadSpan, `Tag ${tag.id} payload（实际 ${tag.availableDataSize} / 声明 ${tag.dataSize} byte）`))}>{tag.availableDataSize} / {tag.dataSize}</button></td>
    <td className={tag.payloadComplete ? '' : 'problem'}>{tag.payloadComplete ? '完整' : '截断'}</td><td className={tag.previousTagSize.valid ? '' : 'problem'}>{!tag.previousTagSize.present ? '缺失' : tag.previousTagSize.valid ? '一致' : `${tag.previousTagSize.value} ≠ ${tag.previousTagSize.expected}`}</td>
  </tr>)}</tbody></table>;
}

function DetailPanel({ view, onRetry, onContinue, onSelectMP3, onPageMP3, onJumpMP3, onStepMP3, onInspectMP3, onLocateMP3, onLocate }: { view: ViewState; onRetry: () => void; onContinue: () => void; onSelectMP3: (index: number | null) => void; onPageMP3: (direction: -1 | 1 | 'continue') => void; onJumpMP3: (text: string) => void; onStepMP3: (direction: -1 | 1) => void; onInspectMP3: () => void; onLocateMP3: (source: MP3DependencySource, destination: 'tag' | 'hex') => void; onLocate: (selection: HexSelection) => void }) {
  const detail = view.detail;
  const kind = detail?.kind === 'amf0ScriptData' ? 'AMF0 Script Data' : detail?.kind === 'aacAudio' ? 'AAC Audio' : detail?.kind === 'avcVideo' ? 'AVC Video' : detail?.kind === 'mp3Audio' ? 'MP3 Audio' : 'Tag 详情';
  const busy = ['loading', 'pending', 'recovering'].includes(view.detailState);
  const replay = avcReplayProgress(detail);
  return <section id="tag-detail" className="detail-panel" aria-label={`Tag ${view.selectedTag!.id} 详情`} aria-busy={busy}>
    <div className="detail-heading"><div><p className="eyebrow">按需解析 · TAG {view.selectedTag!.id}</p><h2>{kind}</h2></div>
      {view.trendPage && <a className="trend-return" href="#tag-trends">返回趋势 ↑</a>}
      {detail && <div className="detail-badges"><span className={`status ${detail.complete ? '' : 'partial'}`}>{detail.complete ? '语法解析完成' : '部分解析'}</span>{detail.truncated && <span className="status partial">内容截断或展示省略</span>}</div>}
    </div>
    {busy && <p className="detail-note" role="status">{view.detailState === 'recovering' ? '正在确认详情请求…' : '正在读取当前 Tag 的详情…'}</p>}
    {view.detailError && <div className="request-error" role="alert"><span>{fault(view.detailError)}</span>{view.detailRetry && <button onClick={onRetry}>{view.detailRetry === 'reload' ? '重新读取详情' : '重试详情查询'}</button>}</div>}
    {detail && <>
      <p className="detail-note">字段位置使用原文件的 byte 偏移和 bit 起点；位长包含字段占用的位数。{detail.kind === 'aacAudio' && ' AAC raw packet 仅显示封装信息与数据范围，不解码音频。'}</p>
      {detail.kind === 'avcVideo' && <p className="detail-note">AVC 展开 SPS VUI、PPS 扩展及普通 slice 完整头部，并解析 SEI 恢复点、UUID 用户数据、缓冲周期与图像时序字段。时序解析显示所用 SPS 及选择依据；上下文缺失或含糊时保留未知。未知 SEI 保留类型与源范围。前缀与完整头部分别标明覆盖；不解码图像或推算播放时间线。派生值及跨段字段的单一原始位置显示“—”。</p>}
      {replay && <div className="detail-note" role="status">参数上下文已检查到 Tag {replay.next} 之前，目标为 Tag {replay.target}。{replay.canContinue ? <button disabled={busy || view.detailState !== 'ready' || view.summaryRunning} onClick={onContinue}>继续准备参数</button> : '当前段无法继续推进；请检查源数据或解析预算限制。'}</div>}
      {detail.kind === 'mp3Audio' && <>
        <p className="detail-note">MP3 在当前 Tag 内检查帧边界、VBR 声明、side-info 和头部 CRC，并显示主数据回指范围。范围覆盖不代表 bit-reservoir 或音频解码有效；可按需回溯前序 Tag，定位主数据依赖来源。</p>
        <p className="detail-note">free-format 的码率和帧长未知；派生字段不标注单一原始位置，以“—”显示。</p>
      </>}
      <MP3FramePanel key={`${view.session?.sessionId}-${view.selectedTag?.id}`} detail={detail} jumpError={view.mp3JumpError} disabled={busy || view.detailState !== 'ready' || view.summaryRunning || (view.avcSearchRunning || view.codecSearchRunning || view.timeSearchRunning) || view.sourceChanged} onSelect={onSelectMP3} onPage={onPageMP3} onJump={onJumpMP3} onStep={onStepMP3} />
      <MP3DependencyPanel detail={detail} disabled={busy || view.detailState !== 'ready' || view.summaryRunning || (view.avcSearchRunning || view.codecSearchRunning || view.timeSearchRunning) || view.sourceChanged} onInspect={onInspectMP3} onLocate={onLocateMP3} />
      <KeyframeEvidencePanel detail={detail} disabled={busy || view.detailState !== 'ready' || view.summaryRunning || (view.avcSearchRunning || view.codecSearchRunning || view.timeSearchRunning) || view.sourceChanged} onLocate={onLocate} />
      {!detail.complete && <p className="detail-note problem">保留可解析的字段；请结合下方详情诊断检查未完成部分。</p>}
      {detail.truncated && <p className="detail-note">部分内容因截断或展示上限而省略；显示的值是有限预览。</p>}
      <div className="detail-table-area"><table className="detail-fields"><thead><tr><th>字段</th><th>类型</th><th>值 / 预览</th><th>byte : bit</th><th>位长</th></tr></thead><tbody>{detailRows(detail.fields).map(({ field, depth }) => <tr key={field.id}>
        <td><span className={`field-name depth-${depth}`}>{depth > 0 && <span aria-hidden="true">└ </span>}{field.name}</span></td><td>{field.type}</td><td className="field-value">{fieldValue(field)}</td><td>{field.span ? <button className="source-link" aria-label={`定位字段 ${field.name}`} onClick={() => onLocate(selectionFromSpan(field.span!, field.name))}>{field.span.byteOffset} : {field.span.bitOffset}</button> : '—'}</td><td>{field.span?.bitLength ?? '—'}</td>
      </tr>)}</tbody></table>{!detail.fields.length && <p className="detail-note">没有可展示的字段。</p>}</div>
      <div className="detail-diagnostics"><h3>详情诊断 <span>({detail.diagnostics.length})</span></h3>{detail.diagnostics.length ? <div className="detail-table-area"><DiagnosticTable rows={detail.diagnostics} onLocate={onLocate} /></div> : <p className="detail-note">没有保留的详情诊断。</p>}</div>
    </>}
  </section>;
}

function DiagnosticTable({ rows, onLocate, disabled = false }: { rows: Diagnostic[]; onLocate: (selection: HexSelection) => void; disabled?: boolean }) {
  if (!rows.length) return <div className="result-placeholder">没有保留的结构诊断。</div>;
  return <table className="diagnostics"><thead><tr><th>级别</th><th>代码 / 消息</th><th>偏移 : bit</th><th>期望 / 实际</th></tr></thead><tbody>{rows.map((item, index) => <tr key={`${item.code}-${item.offset}-${index}`}>
    <td><span className={`severity severity-${item.severity}`}>{item.severity}</span></td><td><code>{item.code}</code><p>{item.message}</p></td><td><button className="source-link" disabled={disabled} aria-label={`定位诊断 ${item.code}`} onClick={() => onLocate({ offset: item.offset, bitOffset: item.bitOffset, bitLength: null, label: `诊断 ${item.code}` })}>{item.offset} : {item.bitOffset}</button></td><td>{item.expected ?? '—'} / {item.actual ?? '—'}</td>
  </tr>)}</tbody></table>;
}
