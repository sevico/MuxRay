import { useEffect, useState } from 'react';
import type { Controller, ViewState } from './controller';
import type { TagFilter } from './protocol';
import { EMPTY_TAG_FILTER, FILTER_HISTORY_LIMIT, sameTagFilter } from './tag-filter';

const typeLabels: Record<TagFilter['type'], string> = { all: '全部类型', audio: '音频', video: '视频', script: '脚本', other: '其他' };
const structureLabels: Record<TagFilter['structure'], string> = { all: '全部结构状态', payload_incomplete: 'Payload 截断', previous_size_missing: 'PreviousTagSize 缺失', previous_size_invalid: 'PreviousTagSize 不一致' };

export function TagFilterPanel({ view, controller, disabled }: { view: ViewState; controller: Controller; disabled: boolean }) {
  const [draft, setDraft] = useState<TagFilter>(view.tagFilter ?? { ...EMPTY_TAG_FILTER });
  useEffect(() => { setDraft(view.tagFilter ?? { ...EMPTY_TAG_FILTER }); }, [view.tagFilter]);
  const applied = view.tagFilter;
  const changed = !sameTagFilter(draft, applied ?? EMPTY_TAG_FILTER);
  return <section className="tag-filter" aria-label="Tag 筛选">
    <form onSubmit={event => { event.preventDefault(); controller.applyTagFilter(draft); }}>
      <label htmlFor="tag-filter-type">Tag 类型<select id="tag-filter-type" value={draft.type} disabled={disabled} onChange={event => setDraft({ ...draft, type: event.target.value as TagFilter['type'] })}>
        {Object.entries(typeLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <label htmlFor="tag-filter-dts-min">DTS 起点 (ms)<input id="tag-filter-dts-min" inputMode="numeric" placeholder="不限" value={draft.dtsMin ?? ''} disabled={disabled} onChange={event => setDraft({ ...draft, dtsMin: event.target.value === '' ? null : event.target.value })} /></label>
      <label htmlFor="tag-filter-dts-max">DTS 终点 (ms)<input id="tag-filter-dts-max" inputMode="numeric" placeholder="不限" value={draft.dtsMax ?? ''} disabled={disabled} onChange={event => setDraft({ ...draft, dtsMax: event.target.value === '' ? null : event.target.value })} /></label>
      <label htmlFor="tag-filter-structure">结构状态<select id="tag-filter-structure" value={draft.structure} disabled={disabled} onChange={event => setDraft({ ...draft, structure: event.target.value as TagFilter['structure'] })}>
        {Object.entries(structureLabels).map(([value, label]) => <option key={value} value={value}>{label}</option>)}
      </select></label>
      <div className="tag-filter-actions"><button className="primary" disabled={disabled}>应用筛选</button><button type="button" disabled={disabled || (!applied && !changed && !view.filterError)} onClick={() => { setDraft({ ...EMPTY_TAG_FILTER }); controller.resetTagFilter(); }}>清除筛选</button></div>
    </form>
    {view.filterError && <p className="problem" role="alert">DTS 请输入 0–4294967295 的十进制整数，不带前导零；起点不能大于终点。留空表示不限。</p>}
    <p className="filter-note">{applied ? `已应用：${typeLabels[applied.type]} · DTS ${applied.dtsMin ?? '不限'} 至 ${applied.dtsMax ?? '不限'} ms · ${structureLabels[applied.structure]}` : '当前显示全部 Tag。'}{changed && ' · 表单有尚未应用的修改。'}</p>
    <p className="filter-note">DTS 区间包含两端，按原始时间戳匹配；结果保留文件顺序。跳转 Tag ID 或概览中的 Tag 会退出筛选。</p>
  </section>;
}

export function FilterPagination({ view, controller, disabled }: { view: ViewState; controller: Controller; disabled: boolean }) {
  const page = view.filteredPage;
  return <div className="filter-pagination">
    <div className="pagination"><span>{page ? `当前检查 Tag [${page.start}, ${page.next}) · 本页命中 ${page.tags.length} · 已索引 ${page.indexedTotal}` : '正在准备筛选结果'}</span><div>
      <button disabled={disabled || view.loading || !page || page.start === '0'} onClick={() => controller.pageFilteredTags('first')}>筛选首页</button>
      <button disabled={disabled || view.loading || !page || view.filterPrevious === null} onClick={() => controller.pageFilteredTags('previous')}>上一结果页</button>
      <button disabled={disabled || view.loading || !page || page.exhausted} onClick={() => controller.pageFilteredTags('next')}>{page && page.tags.length === 0 && !page.exhausted ? '继续查找' : '下一结果页'}</button>
    </div></div>
    {page && <p className="filter-note" role="status">{page.exhausted ? '已检查到当前索引末尾。' : '仍有索引未检查，请继续查找。'} 本页命中数不代表全部匹配数。{view.session?.scan && !view.session.scan.complete && ' 部分索引：范围仅覆盖已保留的 Tag。'}</p>}
    {view.filterHistoryLimited && <p className="filter-note">仅保留最近 {FILTER_HISTORY_LIMIT} 页的返回位置；可通过“筛选首页”重新查找。</p>}
  </div>;
}
