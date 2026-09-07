import { useEffect, useSyncExternalStore } from 'react';
import { Controller, PAGE_SIZE } from './controller';
import type { Diagnostic, Session, Tag } from './protocol';
import { byteSize, previousStart, progressPercent, unsigned } from './numbers';

const stateNames: Record<string, string> = {
  opening: '正在打开', running: '扫描中', canceling: '正在取消', finished: '扫描结束', canceled: '已取消', failed: '扫描失败',
};
const errors: Record<string, string> = {
  busy: '当前任务尚未释放，请稍后重试。', not_ready: '桌面或扫描结果尚未就绪。',
  source_not_found: '找不到所选文件。', source_permission_denied: '没有权限读取所选文件。',
  open_failed: '无法打开文件，请确认它是可读取的普通文件。', scan_failed: '扫描遇到读取或资源清理错误。',
  shutting_down: '应用正在退出。', dialog_failed: '文件选择窗口打开失败。',
  canceled: '任务已取消。', deadline_exceeded: '等待已超时。',
  invalid_argument: '请求参数无效。', session_closed: '此文件已关闭。', session_not_found: '此会话已失效。',
  incompatible_api: '界面与宿主版本不匹配，请重新构建。', invalid_response: '宿主返回了不匹配的结果。',
  transport_failed: '桌面通信失败，请重试或重新启动应用。',
};
function fault(code: string): string { return errors[code] ?? '请求失败，请重试。'; }

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
  const { session, operation, page } = view;
  const progress = session?.progress;
  const canPage = !!session?.terminal && !session.closed && session.scan !== null;
  const bounds = page?.data.bounds;
  const percent = progress ? progressPercent(progress.bytesScanned, progress.sourceSize) : 0;
  const isClosing = operation === 'closing' || session?.closed;

  return <div className="app-shell">
    <header className="topbar">
      <div className="brand"><span className="brand-mark" aria-hidden="true">M</span><div><h1>MuxRay <span>码流透镜</span></h1><p>FLV STRUCTURE INSPECTOR</p></div></div>
      <div className="actions">
        {session && <>
          <button onClick={() => void controller.cancel()} disabled={!!operation || session.terminal || session.state === 'canceling'}>取消扫描</button>
          <button onClick={() => void controller.close()} disabled={!!operation}>关闭文件</button>
        </>}
        <button className="primary" onClick={() => void controller.open()} disabled={!!session || !!operation}>{operation === 'picking' ? '正在选择…' : '打开 FLV 文件'}</button>
      </div>
    </header>

    <main>
      {view.error && <div className="request-error" role="alert"><span>{fault(view.error)}</span><button onClick={() => controller.retry()}>重试查询</button></div>}
      {!session ? <section className="empty-state">
        <div className="file-symbol" aria-hidden="true">FLV</div>
        <h2>从一个文件开始，看清码流结构</h2>
        <p>选择本机 FLV 文件，查看 Tag 索引、时间戳和结构诊断。</p>
        <button className="primary" onClick={() => void controller.open()} disabled={!!operation}>选择 FLV 文件</button>
        <div className="empty-features"><span>只读分析</span><span>可取消扫描</span><span>按需分页</span></div>
        <p className="scope-note">当前为最小桌面版 · 详情解析与媒体汇总仍可通过 CLI 使用</p>
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
        </section>

        <section className="results" aria-label="扫描结果">
          <div className="results-toolbar">
            <div className="tabs" role="tablist" aria-label="结果类型">
              <button role="tab" aria-selected={view.tab === 'tags'} onClick={() => controller.select('tags')} disabled={!canPage || isClosing}>Tag 索引</button>
              <button role="tab" aria-selected={view.tab === 'diagnostics'} onClick={() => controller.select('diagnostics')} disabled={!canPage || isClosing}>诊断 {progress ? `(${progress.diagnosticCount})` : ''}</button>
            </div>
            <span className="table-hint">每页最多 {PAGE_SIZE} 条 · 偏移单位 byte</span>
          </div>
          <div className="table-area" role="tabpanel" aria-busy={view.loading}>
            {view.loading ? <div className="result-placeholder" role="status">正在读取当前页…</div> : page ? <>
              {page.tab === 'tags' ? <TagTable rows={page.data.tags} /> : <DiagnosticTable rows={page.data.diagnostics} />}
            </> : <div className="result-placeholder">{canPage ? '暂无可显示的页，请重试查询。' : '扫描结束后，完整或部分结果会显示在这里。'}</div>}
          </div>
          <div className="pagination"><span>{bounds ? `起点 ${bounds.start} · 本页 ${unsigned(bounds.next) - unsigned(bounds.start)} 条 · 共 ${bounds.total} 条` : '尚无结果页'}</span><div>
            <button disabled={!bounds || view.loading || bounds.start === '0' || !!isClosing} onClick={() => controller.select(view.tab, '0')}>首页</button>
            <button disabled={!bounds || view.loading || bounds.start === '0' || !!isClosing} onClick={() => controller.select(view.tab, previousStart(view.start, PAGE_SIZE))}>上一页</button>
            <button disabled={!bounds || view.loading || unsigned(bounds.next) >= unsigned(bounds.total) || !!isClosing} onClick={() => bounds && controller.select(view.tab, bounds.next)}>下一页</button>
          </div></div>
        </section>
      </>}
    </main>
    <footer><span><i aria-hidden="true" /> 本地只读 · 不上传媒体</span><span>桌面预览版 / 单文件会话</span></footer>
  </div>;
}

function TagTable({ rows }: { rows: Tag[] }) {
  if (!rows.length) return <div className="result-placeholder">当前索引没有 Tag。</div>;
  return <table><thead><tr><th>ID</th><th>类型</th><th>偏移</th><th>DTS (ms)</th><th>Payload (byte)</th><th>Payload</th><th>PreviousTagSize</th></tr></thead><tbody>{rows.map(tag => <tr key={tag.id}>
    <td>{tag.id}</td><td><span className={`tag-type type-${tag.typeCode}`}>{tag.typeName}</span>{tag.filter && ' · filtered'}</td><td>{tag.offset}</td><td>{tag.dts}</td><td>{tag.availableDataSize} / {tag.dataSize}</td>
    <td className={tag.payloadComplete ? '' : 'problem'}>{tag.payloadComplete ? '完整' : '截断'}</td><td className={tag.previousTagSize.valid ? '' : 'problem'}>{!tag.previousTagSize.present ? '缺失' : tag.previousTagSize.valid ? '一致' : `${tag.previousTagSize.value} ≠ ${tag.previousTagSize.expected}`}</td>
  </tr>)}</tbody></table>;
}

function DiagnosticTable({ rows }: { rows: Diagnostic[] }) {
  if (!rows.length) return <div className="result-placeholder">没有保留的结构诊断。</div>;
  return <table className="diagnostics"><thead><tr><th>级别</th><th>代码 / 消息</th><th>偏移 : bit</th><th>期望 / 实际</th></tr></thead><tbody>{rows.map((item, index) => <tr key={`${item.code}-${item.offset}-${index}`}>
    <td><span className={`severity severity-${item.severity}`}>{item.severity}</span></td><td><code>{item.code}</code><p>{item.message}</p></td><td>{item.offset} : {item.bitOffset}</td><td>{item.expected ?? '—'} / {item.actual ?? '—'}</td>
  </tr>)}</tbody></table>;
}
