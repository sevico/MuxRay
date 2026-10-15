import { CancelScan, CloseFile, GetDetail, GetDiagnostics, GetHex, GetSession, GetTags, GetFilteredTags, GetTagTrends, StartDetail, StartHex, StartSummary, GetSummary, CancelSummary, GetSummaryEvents, GetMediaCurves, StartAVCSearch, GetAVCSearch, CancelAVCSearch, StartCodecSearch, GetCodecSearch, CancelCodecSearch } from '../wailsjs/go/desktop/Bindings';
import { OpenFLV, GetOpenResult, SaveSnapshot, GetSaveResult } from '../wailsjs/go/main/Native';
import { desktop, main } from '../wailsjs/go/models';
import type { API } from './protocol';
import { detailResponse, diagnosticResponse, hexResponse, pickResponse, sessionResponse, tagResponse, filteredTagResponse, tagTrendResponse, summaryResponse, summaryPageResponse, mediaCurveResponse, saveResponse, avcSearchResponse, codecSearchResponse } from './normalize';

// Actual generated bindings, not a mock HTTP API. No arbitrary URL, filesystem,
// runtime lifecycle, or full-index endpoint is added to the UI.
export const api: API = {
  open: async requestId => pickResponse(await OpenFLV(requestId)),
  openResult: async requestId => pickResponse(await GetOpenResult(requestId)),
  saveSnapshot: async request => {
    const value = request.snapshot;
    const snapshot: Omit<desktop.SnapshotRequest, 'convertValues'> = { ...value,
      selectedTagId: value.selectedTagId ?? undefined, detailRequestId: value.detailRequestId ?? undefined,
      hexRequestId: value.hexRequestId ?? undefined, summaryRequestId: value.summaryRequestId ?? undefined,
      summaryPage: value.summaryPage ?? undefined, trend: value.trend ?? undefined,
    };
    const wire: Omit<main.SaveRequest, 'convertValues'> = { requestId: request.requestId, snapshot: new desktop.SnapshotRequest(snapshot) };
    return saveResponse(await SaveSnapshot(new main.SaveRequest(wire)));
  },
  saveResult: async requestId => saveResponse(await GetSaveResult(requestId)),
  status: async id => sessionResponse(await GetSession(id)),
  cancel: async id => sessionResponse(await CancelScan(id)),
  close: async id => sessionResponse(await CloseFile(id)),
  tags: async request => tagResponse(await GetTags(request)),
  tagTrends: async request => tagTrendResponse(await GetTagTrends(request)),
  filteredTags: async request => {
    const filter: desktop.TagFilter = { ...request.filter, dtsMin: request.filter.dtsMin ?? undefined, dtsMax: request.filter.dtsMax ?? undefined };
    return filteredTagResponse(await GetFilteredTags(new desktop.FilteredTagRequest({ ...request, filter })));
  },
  diagnostics: async request => diagnosticResponse(await GetDiagnostics(request)),
  startDetail: async request => detailResponse(await StartDetail({ ...request, mp3Dependency: request.mp3Dependency ?? false })),
  detailResult: async request => detailResponse(await GetDetail(request)),
  startHex: async request => hexResponse(await StartHex(request)),
  hexResult: async request => hexResponse(await GetHex(request)),
  startSummary: async request => summaryResponse(await StartSummary(request)),
  summaryResult: async request => summaryResponse(await GetSummary(request)),
  cancelSummary: async request => summaryResponse(await CancelSummary(request)),
  mediaCurves: async request => mediaCurveResponse(await GetMediaCurves(request)),
  summaryEvents: async request => summaryPageResponse(await GetSummaryEvents(request)),
  startCodecSearch: async request => codecSearchResponse(await StartCodecSearch(request)),
  codecSearchResult: async request => codecSearchResponse(await GetCodecSearch(request)),
  cancelCodecSearch: async request => codecSearchResponse(await CancelCodecSearch(request)),
  startAVCSearch: async request => avcSearchResponse(await StartAVCSearch(request)),
  avcSearchResult: async request => avcSearchResponse(await GetAVCSearch(request)),
  cancelAVCSearch: async request => avcSearchResponse(await CancelAVCSearch(request)),
};
