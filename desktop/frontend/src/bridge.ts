import { CancelScan, CloseFile, GetDetail, GetDiagnostics, GetHex, GetSession, GetTags, StartDetail, StartHex, StartSummary, GetSummary, CancelSummary, GetSummaryEvents } from '../wailsjs/go/desktop/Bindings';
import { OpenFLV, GetOpenResult } from '../wailsjs/go/main/Native';
import type { API } from './protocol';
import { detailResponse, diagnosticResponse, hexResponse, pickResponse, sessionResponse, tagResponse, summaryResponse, summaryPageResponse } from './normalize';

// Actual generated bindings, not a mock HTTP API. No arbitrary URL, filesystem,
// runtime lifecycle, or full-index endpoint is added to the UI.
export const api: API = {
  open: async requestId => pickResponse(await OpenFLV(requestId)),
  openResult: async requestId => pickResponse(await GetOpenResult(requestId)),
  status: async id => sessionResponse(await GetSession(id)),
  cancel: async id => sessionResponse(await CancelScan(id)),
  close: async id => sessionResponse(await CloseFile(id)),
  tags: async request => tagResponse(await GetTags(request)),
  diagnostics: async request => diagnosticResponse(await GetDiagnostics(request)),
  startDetail: async request => detailResponse(await StartDetail(request)),
  detailResult: async request => detailResponse(await GetDetail(request)),
  startHex: async request => hexResponse(await StartHex(request)),
  hexResult: async request => hexResponse(await GetHex(request)),
  startSummary: async request => summaryResponse(await StartSummary(request)),
  summaryResult: async request => summaryResponse(await GetSummary(request)),
  cancelSummary: async request => summaryResponse(await CancelSummary(request)),
  summaryEvents: async request => summaryPageResponse(await GetSummaryEvents(request)),
};
