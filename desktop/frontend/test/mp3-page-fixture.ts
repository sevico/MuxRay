import type { Detail } from '../src/protocol.ts';

export function mp3PageDetail(start = 0, selected?: number, state = 'ready', progress = start + 64): Detail {
  const detail: Detail = { kind: 'mp3Audio', complete: false, truncated: false, diagnostics: [], fields: [] };
  const group = (id: string, name: string, type: string, values: Record<string, string>) => {
    detail.fields.push({ id, parentId: '', name, type, value: '', span: null });
    detail.fields.push(...Object.entries(values).map(([name, value], i) => ({ id: `${id}-${i}`, parentId: id, name, type: 'derived', value, span: null })));
  };
  const actual = state === 'ready' ? start : Math.max(0, progress - 64);
  group('page', 'MP3 frame page', 'bounded frame page', {
    pageState: state, pageStopReason: state === 'stopped' ? 'invalid_frame' : 'page_limit', requestedPageStart: String(start), pageStartIndex: String(actual), pageSize: '64', nextPageStart: String(actual + 64),
    nextFrameIndex: String(progress), pageFrames: '64', pageBytesCovered: '26688', pageBytesRead: '256', pageReadBudget: '65536',
    replayPages: '1', replayPageBudget: '16', checkpointCount: '2', hasNext: 'true', canContinue: String(state === 'preparing'),
    requestedFrameIndex: selected === undefined ? 'preview' : String(selected),
  });
  if (state === 'ready') group('frames', 'Frame inspection', 'bounded sequence', {
    selectedFrameIndex: selected === undefined ? 'preview' : String(selected),
    selectableFrameIndices: start < 1024 ? Array.from({ length: 64 }, (_, i) => start + i).join(',') : '',
    selectableFrameOffsets: Array.from({ length: 64 }, (_, i) => i).join(','), checkedFrameCount: '64',
  });
  return detail;
}
