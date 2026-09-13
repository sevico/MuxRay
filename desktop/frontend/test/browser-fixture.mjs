// Browser fixture only: no Go parser or native I/O runs here.
export function syntheticHost() {
  let session, latestPick, latestDetail, latestHex, latestSummary, sequence = 0;
  window.smokeMode = new URL(window.location.href).searchParams.get('smoke') ?? 'finished';
  window.smokeReads = [];
  window.smokeSummaryCalls = [];
  const envelope = () => structuredClone({ apiVersion: 1, session, error: null });
  const span = { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 88 };
  window.go = { main: { Native: { OpenFLV: async requestId => {
    if (window.smokeMode === 'picker-cancel') return latestPick = { apiVersion: 1, requestId, pending: false, canceled: true, session: null, error: null, fileName: '' };
    session = { sessionId: `synthetic-${++sequence}`, state: window.smokeMode === 'running' ? 'running' : 'finished', terminal: window.smokeMode !== 'running', closed: false,
      progress: { sourceSize: '18446744073709551615', bytesScanned: '9223372036854775807', tagCount: '205', diagnosticCount: 2 },
      scan: window.smokeMode === 'running' ? null : { complete: false, hasErrors: true }, failure: null };
    return latestPick = { ...envelope(), requestId, pending: false, canceled: false, fileName: 'browser-synthetic <img onerror=alert(1)>.flv' };
  }, GetOpenResult: async requestId => {
    if (latestPick?.requestId !== requestId) throw Error('Unexpected synthetic open result request');
    return structuredClone({ ...latestPick, session: latestPick.canceled ? null : session });
  } } }, desktop: { Bindings: {
    GetSession: async () => envelope(),
    CancelScan: async () => { session.state = 'canceled'; session.terminal = true; session.scan = { complete: false, hasErrors: false }; session.failure = { code: 'canceled', message: 'Canceled.' }; return envelope(); },
    CloseFile: async () => { session.closed = true; session.terminal = true; return envelope(); },
    GetTags: async request => {
      assertRequest(request);
      const start = Number(request.start), next = Math.min(start + request.count, 205);
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: String(next), total: '205', limit: request.count }, tags: Array.from({ length: next - start }, (_, index) => ({
        id: String(start + index), typeCode: start + index >= 6 && start + index <= 9 ? 8 : 9, typeName: start + index >= 6 && start + index <= 9 ? 'audio' : 'video', offset: String(9007199254740993n + BigInt(start + index)), span: { ...span, byteOffset: String(9007199254740993n + BigInt(start + index)) }, headerSpan: span, payloadSpan: span,
        rawTypeFlags: start + index >= 6 && start + index <= 9 ? 8 : 9, filter: false, dataSize: 64, availableDataSize: 64, dts: '-9223372036854775808', streamId: 0, payloadComplete: true,
        previousTagSize: { present: true, span, value: 75, expected: 75, valid: true },
      })) } };
    },
    GetDiagnostics: async request => {
      assertRequest(request);
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: '0', next: '2', total: '2', limit: request.count }, diagnostics: [0, 1].map(i => ({ code: `SYNTHETIC_${i}`, severity: 'error', offset: '18446744073709551615', bitOffset: i, message: '<script>Not executable HTML</script>', expected: null, actual: '9007199254740993' })) } };
    },
    StartDetail: async request => {
      if (!session.terminal || session.closed || request.sessionId !== session.sessionId) throw Error('Unexpected synthetic detail request');
      window.smokeReads.push({ kind: 'detail', ...request });
      if (request.tagId === '9') {
        latestDetail = { apiVersion: 1, ...request, state: 'failed', error: { code: 'detail_unsupported', message: 'Synthetic SoundFormat=14 remains unsupported.' }, detail: null };
        return structuredClone(latestDetail);
      }
      const preview = {
        kind: request.tagId === '0' ? 'amf0ScriptData' : 'aacAudio', complete: request.tagId === '0', truncated: true,
        // The enclosing byte range ends at UINT64_MAX, without overflowing it.
        fields: [{ id: '0', parentId: '', name: 'synthetic payload', type: 'string', value: '<img onerror=alert(1)>字段值</img>', span: { byteOffset: '18446744073709551614', bitOffset: 3, bitLength: 5 } }],
        diagnostics: [{ code: 'SYNTHETIC_DETAIL', severity: 'warning', offset: '9007199254740993', bitOffset: 3, message: '<script>plain diagnostic</script>', expected: null, actual: null }],
      };
      if (['2', '3', '4', '5'].includes(request.tagId)) {
        const missingConfig = request.tagId === '3', contextBudget = request.tagId === '4', nalBudget = request.tagId === '5';
        preview.kind = 'avcVideo'; preview.complete = !missingConfig && !contextBudget && !nalBudget; preview.truncated = contextBudget || nalBudget;
        preview.fields = [
          { id: '0', parentId: '', name: 'AVC context', type: 'context', value: missingConfig ? 'missing' : contextBudget ? 'budget_exhausted' : 'found', span: null },
          { id: '1', parentId: '', name: 'compositionTime', type: 'int24', value: '-2', span: { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 24 } },
          { id: '2', parentId: '', name: 'PTS', type: 'int64', value: '-9223372036854775808', span: null },
          ...missingConfig || contextBudget ? [] : [{ id: '3', parentId: '', name: 'nalUnitType', type: 'uint5', value: '5', span: { byteOffset: '18446744073709551614', bitOffset: 3, bitLength: 5 } }],
        ];
        preview.diagnostics = missingConfig ? [{ code: 'AVC_NAL_LENGTH_SIZE_UNAVAILABLE', severity: 'error', offset: '9007199254740993', bitOffset: 0, message: '缺少前置 AVC 配置，保留包头；不猜测 NAL 长度。', expected: null, actual: null }]
          : contextBudget ? [{ code: 'AVC_CONTEXT_LOOKUP_LIMIT_REACHED', severity: 'error', offset: '9007199254740993', bitOffset: 0, message: '前置配置查找达到预算，不猜测 NAL 长度。', expected: null, actual: null }]
            : nalBudget ? [{ code: 'AVC_NAL_UNIT_LIMIT_REACHED', severity: 'error', offset: '18446744073709551615', bitOffset: 0, message: 'NAL 数量达到解析预算，保留已解析前缀。', expected: '128', actual: '129' }] : [];
      }
      if (['6', '7', '8'].includes(request.tagId)) {
        const freeFormat = request.tagId === '7', malformed = request.tagId === '8';
        preview.kind = 'mp3Audio'; preview.complete = !malformed; preview.truncated = false;
        preview.fields = [
          { id: '0', parentId: '', name: 'bitrateIndex', type: 'uint4', value: malformed ? '15 (invalid)' : freeFormat ? '0 (free-format)' : '9', span: { byteOffset: '9007199254740995', bitOffset: 0, bitLength: 4 } },
          { id: '1', parentId: '', name: 'bitrateMode', type: 'derived', value: malformed ? 'invalid' : freeFormat ? 'free' : 'indexed', span: null },
          ...(malformed || freeFormat ? [] : [
            { id: '2', parentId: '', name: 'bitrateKbps', type: 'derived', value: '128', span: null },
            { id: '3', parentId: '', name: 'frameLengthBytes', type: 'derived', value: '417', span: null },
          ]),
        ];
        preview.diagnostics = malformed ? [{ code: 'MP3_BITRATE_INDEX_INVALID', severity: 'error', offset: '9007199254740995', bitOffset: 0, message: 'Reserved bitrate index; retain fixed header fields.', expected: null, actual: '15' }]
          : freeFormat ? [{ code: 'MP3_FREE_FORMAT_UNRESOLVED', severity: 'warning', offset: '9007199254740995', bitOffset: 0, message: 'Free-format bitrate and frame length are unknown.', expected: null, actual: null }] : [];
      }
      if (request.tagId === '10') {
        preview.kind = 'amf0ScriptData'; preview.complete = true; preview.truncated = false;
        preview.fields = [
          { id: '0', parentId: '', name: 'cross-window', type: 'bytes', value: '1024 byte range', span: { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 8192 } },
          { id: '1', parentId: '', name: 'cross-byte', type: 'uint5', value: '0', span: { byteOffset: '9007199254740993', bitOffset: 6, bitLength: 5 } },
          { id: '2', parentId: '', name: 'derived', type: 'number', value: '3', span: null },
        ];
      }
      latestDetail = { apiVersion: 1, ...request, state: 'ready', error: null, detail: preview };
      return structuredClone(latestDetail);
    },
    GetDetail: async request => {
      if (request.sessionId !== latestDetail.sessionId || request.requestId !== latestDetail.requestId) throw Error('Unexpected synthetic detail query');
      return structuredClone(latestDetail);
    },
    StartHex: async request => {
      if (!session.terminal || session.closed || request.sessionId !== session.sessionId || request.count !== 512) throw Error('Unexpected synthetic Hex request');
      window.smokeReads.push({ kind: 'hex', ...request });
      if (window.smokeMode === 'source-changed') return { apiVersion: 1, ...request, state: 'failed', window: null, error: { code: 'source_changed', message: 'Synthetic change' } };
      const remain = BigInt(session.progress.sourceSize) - BigInt(request.offset), count = remain < 512n ? Number(remain) : 512;
      if (remain < 0n) throw Error('Hex offset beyond EOF');
      latestHex = { apiVersion: 1, ...request, state: 'ready', error: null, window: { offset: request.offset, sourceSize: session.progress.sourceSize, hex: Array.from({ length: count }, (_, i) => (i % 256).toString(16).padStart(2, '0')).join(''), eof: remain <= 512n } };
      return structuredClone(latestHex);
    },
    GetHex: async request => {
      if (request.sessionId !== latestHex.sessionId || request.requestId !== latestHex.requestId) throw Error('Unexpected synthetic Hex query');
      return structuredClone(latestHex);
    },
    StartSummary: async request => {
      if (!session.terminal || session.closed || request.sessionId !== session.sessionId) throw Error('Unexpected synthetic summary start');
      window.smokeReads.push({ kind: 'summary', ...request }); window.smokeSummaryCalls.push({ operation: 'start', ...request });
      latestSummary = { apiVersion: 1, ...request, state: window.smokeMode === 'summary-pending' || window.smokeMode === 'summary-cancel-lost' ? 'pending' : 'ready', processed: '205', total: '205', overview: summaryOverview(), error: null };
      if (latestSummary.state === 'pending') { latestSummary.processed = '100'; latestSummary.overview = null; }
      if (window.smokeMode === 'summary-source-changed') { latestSummary.state = 'failed'; latestSummary.overview = null; latestSummary.error = { code: 'source_changed', message: 'Synthetic source change' }; }
      if (window.smokeMode === 'summary-lost-start') return new Promise(() => {});
      return structuredClone(latestSummary);
    },
    GetSummary: async request => { assertSummary(request); window.smokeSummaryCalls.push({ operation: 'get', ...request }); return structuredClone(latestSummary); },
    CancelSummary: async request => {
      assertSummary(request); window.smokeSummaryCalls.push({ operation: 'cancel', ...request });
      if (window.smokeMode === 'summary-cancel-lost' && window.smokeSummaryCalls.filter(call => call.operation === 'cancel').length === 1) return new Promise(() => {});
      latestSummary.state = 'failed'; latestSummary.overview = null; latestSummary.error = { code: 'canceled', message: 'Synthetic cancellation' };
      return structuredClone(latestSummary);
    },
    GetSummaryEvents: async request => {
      assertSummary(request); window.smokeSummaryCalls.push({ operation: 'page', ...request });
      if (latestSummary.state !== 'ready' || request.count !== 100) throw Error('Unexpected synthetic summary page');
      const info = latestSummary.overview.lists.find(list => list.kind === request.kind);
      if (!info) throw Error('Unknown synthetic summary kind');
      const start = BigInt(request.start), total = BigInt(info.retained), limit = window.smokeMode === 'summary-short-page' ? 37n : 100n;
      const next = start + limit < total ? start + limit : total;
      if (start > total) throw Error('Synthetic page out of bounds');
      return { apiVersion: 1, sessionId: request.sessionId, requestId: request.requestId, kind: request.kind, error: null,
        page: { bounds: { start: request.start, next: String(next), total: info.retained, limit: 100 }, events: Array.from({ length: Number(next - start) }, (_, index) => ({
          id: String(start + BigInt(index)), tagId: request.kind === 'diagnostics' ? null : '105', offset: '9007199254740993', bitOffset: 3, dts: '-9223372036854775808',
          title: request.kind === 'diagnostics' ? 'SYNTHETIC_SUMMARY' : { keyframes: 'declaredKeyframe', aac: 'aacConfiguration', avc: 'avcConfiguration', mp3: 'mp3Header' }[request.kind],
          severity: request.kind === 'diagnostics' ? 'warning' : '', message: '<script>summary plain text</script>', values: [
            { section: '', name: 'bitrateKbps', value: null, unit: 'kbps' }, { section: '', name: 'fingerprintSHA256', value: 'a'.repeat(64), unit: '' },
          ],
        })) } };
    },
  } } };
  function assertRequest(request) {
    if (!session.terminal || session.closed || request.sessionId !== session.sessionId || request.count !== 100) throw Error('Unexpected synthetic page request');
  }
  function assertSummary(request) {
    if (session.closed || request.sessionId !== latestSummary?.sessionId || request.requestId !== latestSummary?.requestId) throw Error('Unexpected synthetic summary identity');
  }
  function summaryOverview() {
    const metrics = [], metric = (section, name, value, unit = '') => metrics.push({ section, name, value, unit });
    for (const [name, value] of [['audio', '100'], ['video', '100'], ['script', '4'], ['other', '1']]) metric('tags', name, value, 'tags');
    for (const [name, value] of [['aacTagCount', '50'], ['mp3TagCount', '50'], ['switchCount', '2']]) metric('audioCodecs', name, value, 'count');
    for (const track of ['audio', 'video', 'mp3', 'videoPts']) {
      const section = track === 'videoPts' ? track : track + 'Timing';
      metric(section, 'minMs', '-9223372036854775808', 'ms'); metric(section, 'maxMs', '9007199254740993', 'ms'); metric(section, 'regressionCount', '2', 'count'); metric(section, 'duplicateCount', '3', 'count');
      if (track !== 'videoPts') metric(track + 'Bitrate', 'averageBitsPerSecond', track === 'mp3' ? null : '9007199254740993', 'bps');
    }
    metric('keyframes', 'declaredCount', '2000', 'tags'); metric('gop', 'averageDurationMs', null, 'ms');
    metric('aac', 'changeCount', '2', 'count'); metric('avc', 'changeCount', '3', 'count'); metric('mp3Headers', 'parameterChangeCount', '4', 'count'); metric('mp3Headers', 'bitrateChangeCount', '5', 'count');
    metric('avSync', 'startExtremaSkewMs', '-9007199254740993', 'ms'); metric('avSync', 'endExtremaSkewMs', '9007199254740993', 'ms');
    return { sourceSize: session.progress.sourceSize, indexedTags: '205', scanComplete: false, scanHasErrors: true, complete: true, hasErrors: false, metrics,
      lists: ['keyframes', 'aac', 'avc', 'mp3', 'diagnostics'].map(kind => ({ kind, observed: kind === 'diagnostics' ? null : '2000', retained: kind === 'keyframes' ? '205' : kind === 'diagnostics' ? '256' : '100', truncated: true })),
    };
  }
  return true;
}
