import type { API, DetailQuery, SummaryOverview, SummaryPageRequest, SummaryPageResponse, SummaryResponse } from '../src/protocol.ts';
import { SUMMARY_KINDS } from '../src/summary.ts';

export const unexpectedSummary: Pick<API, 'startSummary' | 'summaryResult' | 'cancelSummary' | 'summaryEvents'> = {
  startSummary: async () => { throw Error('unexpected summary start'); }, summaryResult: async () => { throw Error('unexpected summary query'); },
  cancelSummary: async () => { throw Error('unexpected summary cancellation'); }, summaryEvents: async () => { throw Error('unexpected summary page'); },
};
export function overview(): SummaryOverview {
  return { sourceSize: '9007199254742000', indexedTags: '205', scanComplete: false, scanHasErrors: true, complete: true, hasErrors: false,
    metrics: [{ section: 'avSync', name: 'startExtremaSkewMs', value: '-9007199254740993', unit: 'ms' }, { section: 'audioBitrate', name: 'averageBitsPerSecond', value: null, unit: 'bps' }],
    lists: SUMMARY_KINDS.map(kind => ({ kind, observed: kind === 'diagnostics' ? null : '300', retained: kind === 'keyframes' ? '205' : kind === 'diagnostics' ? '256' : '100', truncated: true })),
  };
}
export const summaryReady = (request: DetailQuery): SummaryResponse => ({ apiVersion: 1, ...request, state: 'ready', processed: '205', total: '205', overview: overview(), error: null });
export const summaryPending = (request: DetailQuery, state: 'pending' | 'canceling' = 'pending'): SummaryResponse => ({ apiVersion: 1, ...request, state, processed: '100', total: '205', overview: null, error: null });
export const summaryFailed = (request: DetailQuery, code: string): SummaryResponse => ({ apiVersion: 1, ...request, state: 'failed', processed: '0', total: '205', overview: null, error: { code, message: code } });
export function summaryPage(request: SummaryPageRequest, pageSize = 100): SummaryPageResponse {
  const total = overview().lists.find(item => item.kind === request.kind)!.retained;
  const start = BigInt(request.start), next = start + BigInt(pageSize) < BigInt(total) ? start + BigInt(pageSize) : BigInt(total);
  return { apiVersion: 1, ...request, error: null, page: { bounds: { start: request.start, next: next.toString(), total, limit: request.count }, events: Array.from({ length: Number(next - start) }, (_, index) => ({
    id: (start + BigInt(index)).toString(), tagId: request.kind === 'diagnostics' ? null : '105', offset: '9007199254740993', bitOffset: 3, dts: '-9223372036854775808', title: 'aacConfiguration', severity: '', message: '<script>plain text</script>', values: [],
  })) } };
}
