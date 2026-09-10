import { CancelScan, CloseFile, GetDetail, GetDiagnostics, GetSession, GetTags, StartDetail } from '../wailsjs/go/desktop/Bindings';
import { OpenFLV, GetOpenResult } from '../wailsjs/go/main/Native';
import type { API } from './protocol';
import { detailResponse, diagnosticResponse, pickResponse, sessionResponse, tagResponse } from './normalize';

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
};
