import type { Controller, ViewState } from './controller';

export function SnapshotStatus({ view, controller, message }: { view: ViewState; controller: Controller; message: (code: string) => string }) {
  if (!view.saveState) return null;
  const failed = view.saveError !== null;
  return <section className={`snapshot-status${failed ? ' has-error' : ''}`} aria-label="诊断快照保存状态" role={failed ? 'alert' : 'status'}>
    <div><strong>诊断快照 · {view.saveSourceName || '当前文件'}</strong>
      {view.savePending ? <p>{view.saveState === 'recovering' ? '正在确认原保存请求的结果。' : '正在等待保存完成。'} 浏览变化不会更新这份快照；关闭文件不会撤销已开始的保存。</p> :
        view.saveState === 'saved' ? <p>已保存 {view.saveFileName} · {view.saveBytesWritten} byte</p> :
        view.saveState === 'canceled' ? <p>已取消保存。</p> : <p>保存失败。</p>}
      {view.saveError && <p>{message(view.saveError)}{view.savePending && ' 保存状态尚未确定，请确认原请求；不会重复弹出保存窗口。'}</p>}
    </div>
    {view.saveRetry && <button onClick={() => controller.retrySave()}>确认保存结果</button>}
  </section>;
}
