import { CancelScan, CloseFile, GetDiagnostics, GetSession, GetTags } from '../wailsjs/go/desktop/Bindings';
import { OpenFLV } from '../wailsjs/go/main/Native';
import type { API } from './protocol';
import { diagnosticResponse, pickResponse, sessionResponse, tagResponse } from './normalize';

// Actual generated bindings, not a mock HTTP API. No arbitrary URL, filesystem,
// runtime lifecycle, or full-index endpoint is added to the UI.
export const api: API = {
  open: async () => pickResponse(await OpenFLV()),
  status: async id => sessionResponse(await GetSession(id)),
  cancel: async id => sessionResponse(await CancelScan(id)),
  close: async id => sessionResponse(await CloseFile(id)),
  tags: async request => tagResponse(await GetTags(request)),
  diagnostics: async request => diagnosticResponse(await GetDiagnostics(request)),
};
