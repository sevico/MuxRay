import type { Detail } from '../src/protocol.ts';

export function dependencyDetail(tagId = '1', frameIndex = 0): Detail {
  return { kind: 'mp3Audio', complete: true, truncated: false, diagnostics: [], fields: [
    { id: 'dep', parentId: '', name: 'MP3 dependency sources', type: 'bounded cross-tag dependency', value: 'resolved', span: null },
    ...Object.entries({ dependencyState: 'resolved', dependencyReason: 'covered', targetTagId: tagId, targetFrameIndex: String(frameIndex),
      requestedPreviousBytes: '20', resolvedPreviousBytes: '20', missingPreviousBytes: '0', sourceCount: '2', scannedTags: '1', bytesRead: '80' })
      .map(([name, value], i) => ({ id: `dep-${i}`, parentId: 'dep', name, type: 'derived', value, span: null })),
    ...['0', tagId].flatMap((id, i) => [
      { id: `source-${i}`, parentId: 'dep', name: `Source ${i}`, type: 'physical main-data source', value: '', span: null },
      ...Object.entries({ sourceTagId: id, sourceFrameIndex: String(i ? frameIndex : 0), sourceCurrent: String(i === 1) }).map(([name, value], j) => ({ id: `s-${i}-${j}`, parentId: `source-${i}`, name, type: 'derived', value, span: null })),
      { id: `span-${i}`, parentId: `source-${i}`, name: 'sourceSpan', type: 'bytes', value: '', span: { byteOffset: i ? '60' : '25', bitOffset: 0, bitLength: i ? 80 : 160 } },
    ]),
  ] };
}
