// Browser fixture only: no Go parser or native I/O runs here.
export function syntheticHost() {
  let session, latestPick, latestDetail, latestHex, latestSummary, sequence = 0, dataset = 'finished', filterCalls = 0;
  window.smokeMode = new URL(window.location.href).searchParams.get('smoke') ?? 'finished';
  window.smokeReads = [];
  window.smokeSummaryCalls = [];
  window.smokeFilterCalls = [];
  const envelope = () => structuredClone({ apiVersion: 1, session, error: null });
  const span = { byteOffset: '9007199254740993', bitOffset: 0, bitLength: 88 };
  window.go = { main: { Native: { OpenFLV: async requestId => {
    if (window.smokeMode === 'picker-cancel') return latestPick = { apiVersion: 1, requestId, pending: false, canceled: true, session: null, error: null, fileName: '' };
    dataset = window.smokeMode;
    filterCalls = 0;
    session = { sessionId: `synthetic-${++sequence}`, state: window.smokeMode === 'running' ? 'running' : 'finished', terminal: window.smokeMode !== 'running', closed: false,
      progress: { sourceSize: '18446744073709551615', bytesScanned: '9223372036854775807', tagCount: String(tagTotal()), diagnosticCount: 2 },
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
      const start = Number(request.start), next = Math.min(start + request.count, tagTotal());
      return { apiVersion: 1, error: null, page: { sessionId: session.sessionId, bounds: { start: request.start, next: String(next), total: String(tagTotal()), limit: request.count }, tags: Array.from({ length: next - start }, (_, index) => tagAt(start + index)) } };
    },
    GetFilteredTags: async request => {
      assertRequest(request);
      window.smokeFilterCalls.push(structuredClone(request));
      filterCalls++;
      // Generated Wails request classes omit optional null pointers as undefined;
      // the real JSON bridge decodes those missing fields to Go nil.
      const filter = { ...request.filter, dtsMin: request.filter.dtsMin ?? null, dtsMax: request.filter.dtsMax ?? null };
      const tags = [], start = Number(request.start), total = tagTotal();
      if (start > total || !Number.isSafeInteger(start) || start < 0) throw Error('Synthetic filter cursor out of bounds');
      let next = start;
      while (next < total && next - start < 4096 && tags.length < request.count) {
        const tag = tagAt(next++), dts = BigInt(tag.dts);
        const kind = ['audio', 'video', 'script'].includes(tag.typeName) ? tag.typeName : 'other';
        const structure = { all: true, payload_incomplete: !tag.payloadComplete,
          previous_size_missing: !tag.previousTagSize.present,
          previous_size_invalid: tag.previousTagSize.present && !tag.previousTagSize.valid }[filter.structure];
        if ((filter.type === 'all' || kind === filter.type) && structure
          && (filter.dtsMin === null || dts >= BigInt(filter.dtsMin))
          && (filter.dtsMax === null || dts <= BigInt(filter.dtsMax))) tags.push(tag);
      }
      const response = { apiVersion: 1, error: null, page: { sessionId: session.sessionId,
        filter: structuredClone(filter), start: request.start, next: String(next), indexedTotal: String(total),
        limit: request.count, scanLimit: 4096, scanned: next - start, exhausted: next === total, tags } };
      if (dataset === 'filter-timeout' && filterCalls === 1) return new Promise(() => {});
      // The response is captured before waiting, so Close/Reopen produces an old session reply.
      if (dataset === 'filter-delayed') await new Promise(resolve => setTimeout(resolve, 1800));
      return response;
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
      latestSummary = { apiVersion: 1, ...request, state: window.smokeMode === 'summary-pending' || window.smokeMode === 'summary-cancel-lost' ? 'pending' : 'ready', processed: String(tagTotal()), total: String(tagTotal()), overview: summaryOverview(), error: null };
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
      if (dataset.startsWith('filter-')) {
        return { apiVersion: 1, sessionId: request.sessionId, requestId: request.requestId, kind: request.kind, error: null,
          page: { bounds: { start: request.start, next: String(next), total: info.retained, limit: 100 },
            events: filterSummaryEvents(request.kind).slice(Number(start), Number(next)) } };
      }
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
  function tagTotal() {
    return { 'filter-sparse': 8305, 'filter-multipage': 305, 'filter-ranges': 12,
      'filter-structures': 7, 'filter-truncated': 2, 'filter-empty': 0,
      'filter-delayed': 12, 'filter-timeout': 12 }[dataset] ?? 205;
  }
  function tagAt(id) {
    let typeCode = id >= 6 && id <= 9 ? 8 : 9, dts = '-9223372036854775808';
    if (dataset === 'filter-sparse') {
      typeCode = [4500, 5000, 8200].includes(id) ? 8 : id === 8304 ? 9 : 18;
      dts = String({ 4500: 10000, 5000: 12000, 8200: 11000, 8304: 11000 }[id] ?? 0);
    } else if (dataset === 'filter-multipage') {
      typeCode = id % 2 === 0 ? 8 : 9; dts = id % 3 ? '10000' : '12000';
    } else if (['filter-ranges', 'filter-delayed', 'filter-timeout'].includes(dataset)) {
      typeCode = [8, 9, 18, 31, 8, 9, 8, 9, 8, 31, 18, 9][id];
      dts = String([10000, 10000, 12000, 11000, 9999, 12001, 10000, 12000, 11000, 4294967295, 0, 10000][id]);
    } else if (dataset === 'filter-structures') {
      typeCode = [18, 8, 9, 31, 8, 9, 8][id]; dts = String(id * 10);
    } else if (dataset === 'filter-truncated') { typeCode = id ? 8 : 18; dts = id ? '12000' : '0'; }
    const truncated = dataset === 'filter-truncated' && id === 1;
    const missing = truncated || dataset === 'filter-structures' && id === 6;
    const invalid = dataset === 'filter-structures' && [1, 3].includes(id);
    const offset = String(9007199254740993n + BigInt(id));
    return { id: String(id), typeCode, typeName: { 8: 'audio', 9: 'video', 18: 'script' }[typeCode] ?? 'unknown',
      offset, span: { ...span, byteOffset: offset }, headerSpan: span, payloadSpan: span,
      rawTypeFlags: typeCode, filter: false, dataSize: truncated ? 10 : 64, availableDataSize: truncated ? 2 : 64,
      dts, streamId: 0, payloadComplete: !truncated,
      previousTagSize: { present: !missing, span: missing ? null : span, value: missing ? 0 : invalid ? 99 : 75, expected: truncated ? 21 : 75, valid: !missing && !invalid } };
  }
  function filterSummaryEvents(kind) {
    // Tag 6 is the one explicit MP3 detail supplied by this synthetic host.
    // Framing-only filter datasets must not inherit out-of-range Tag 105 events
    // or the separate Summary retention/large-integer fixture's event counts.
    if (kind !== 'mp3' || tagTotal() <= 6 || tagAt(6).typeCode !== 8) return [];
    const tag = tagAt(6);
    return [{ id: '0', tagId: tag.id, offset: tag.offset, bitOffset: 0, dts: tag.dts,
      title: 'mp3Header', severity: '', message: 'Synthetic fixed-header event; no Go parser runs here.',
      values: [{ section: '', name: 'bitrateKbps', value: '128', unit: 'kbps' }] }];
  }
  function filterSummaryOverview() {
    const metrics = [], metric = (section, name, value, unit = '') => metrics.push({ section, name, value, unit });
    const rows = Array.from({ length: tagTotal() }, (_, i) => tagAt(i));
    const audio = rows.filter(row => row.typeCode === 8), video = rows.filter(row => row.typeCode === 9);
    const mp3 = filterSummaryEvents('mp3');
    const counts = { audio: audio.length, video: video.length,
      script: rows.filter(row => row.typeCode === 18).length, other: rows.filter(row => ![8, 9, 18].includes(row.typeCode)).length };
    for (const [name, value] of Object.entries(counts)) metric('tags', name, String(value), 'tags');
    for (const [name, value] of Object.entries({ aacTagCount: 0, mp3TagCount: mp3.length, unknownTagCount: audio.length - mp3.length, switchCount: 0 })) metric('audioCodecs', name, String(value), 'count');
    for (const [track, tags] of [['audio', audio], ['video', video], ['mp3', mp3]]) {
      const values = tags.map(tag => Number(tag.dts));
      metric(track + 'Timing', 'minMs', values.length ? String(Math.min(...values)) : null, 'ms');
      metric(track + 'Timing', 'maxMs', values.length ? String(Math.max(...values)) : null, 'ms');
      metric(track + 'Timing', 'regressionCount', String(values.filter((value, index) => index > 0 && value < values[index - 1]).length), 'count');
      metric(track + 'Timing', 'duplicateCount', String(values.filter((value, index) => index > 0 && value === values[index - 1]).length), 'count');
      metric(track + 'Bitrate', 'averageBitsPerSecond', null, 'bps');
    }
    metric('videoPts', 'minMs', null, 'ms'); metric('videoPts', 'maxMs', null, 'ms');
    metric('keyframes', 'declaredCount', '0', 'tags'); metric('gop', 'averageDurationMs', null, 'ms');
    metric('aac', 'changeCount', '0', 'count'); metric('avc', 'changeCount', '0', 'count');
    metric('mp3Headers', 'parameterChangeCount', '0', 'count'); metric('mp3Headers', 'bitrateChangeCount', '0', 'count');
    metric('avSync', 'startExtremaSkewMs', null, 'ms'); metric('avSync', 'endExtremaSkewMs', null, 'ms');
    return { sourceSize: session.progress.sourceSize, indexedTags: String(tagTotal()), scanComplete: session.scan.complete,
      scanHasErrors: session.scan.hasErrors, complete: true, hasErrors: false, metrics,
      lists: ['keyframes', 'aac', 'avc', 'mp3', 'diagnostics'].map(kind => ({ kind,
        observed: String(filterSummaryEvents(kind).length), retained: String(filterSummaryEvents(kind).length), truncated: false })) };
  }
  function summaryOverview() {
    if (dataset.startsWith('filter-')) return filterSummaryOverview();
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
    return { sourceSize: session.progress.sourceSize, indexedTags: String(tagTotal()), scanComplete: false, scanHasErrors: true, complete: true, hasErrors: false, metrics,
      lists: ['keyframes', 'aac', 'avc', 'mp3', 'diagnostics'].map(kind => ({ kind, observed: kind === 'diagnostics' ? null : '2000', retained: kind === 'keyframes' ? '205' : kind === 'diagnostics' ? '256' : '100', truncated: true })),
    };
  }
  return true;
}
