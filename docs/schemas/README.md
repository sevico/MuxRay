# Public schemas

The public Draft 2020-12 contracts are:

- [`flv-scan-report-v1.schema.json`](./flv-scan-report-v1.schema.json) for `muxray inspect`;
- [`flv-tag-page-v1.schema.json`](./flv-tag-page-v1.schema.json) for bounded `muxray tags --start <id> --count <n>` pages;
- [`flv-tag-detail-v1.schema.json`](./flv-tag-detail-v1.schema.json) for one bounded AMF0 ScriptData `muxray detail --tag <id>` result;
- [`flv-aac-tag-detail-v1.schema.json`](./flv-aac-tag-detail-v1.schema.json) for one bounded FLV AAC `muxray detail --tag <id>` result.
- [`flv-avc-tag-detail-v1.schema.json`](./flv-avc-tag-detail-v1.schema.json) for one bounded FLV AVC/H.264 `muxray detail --tag <id>` result.
- [`flv-mp3-tag-detail-v1.schema.json`](./flv-mp3-tag-detail-v1.schema.json) for the bounded first MPEG Audio Layer III frame header in one FLV MP3 `muxray detail --tag <id>` result.
- [`flv-media-summary-v1.schema.json`](./flv-media-summary-v1.schema.json) for bounded cross-tag AAC/AVC `muxray summary` analysis.
- [`flv-media-summary-v2.schema.json`](./flv-media-summary-v2.schema.json) for opt-in AAC/AVC/MP3 `muxray summary --schema-version 2` analysis.
- [`desktop-diagnostic-snapshot-v1.schema.json`](./desktop-diagnostic-snapshot-v1.schema.json) for bounded desktop diagnostic snapshots saved through the native file dialog.

Scan and default detail use `schemaVersion: 1`; AVC/MP3 detail may select 2 or 3; media summary explicitly selects 1 or 2
and defaults to 1. These are independent contracts. The scan report
is selected by the `inspect` command. Detail consumers must select the validator
using both `documentType` and `schemaVersion`: AMF0 uses
`documentType: "flv-tag-detail"`, while AAC uses
`documentType: "flv-aac-tag-detail"`, and AVC uses
`documentType: "flv-avc-tag-detail"`; MP3 uses
`documentType: "flv-mp3-tag-detail"`. Adding members to a closed Schema or
emitting another detail shape under an existing document identity is not a
compatible extension.

The media summary is selected with `documentType: "flv-media-summary"` and its
own schemaVersion. Its independently bounded entry lists do not extend any
scan or detail v1 document. Media-summary v1 remains frozen to AAC/AVC: MP3
Tags remain unsupported/unclassified even though an independent MP3 detail
document now exists. V2 adds MP3 classification, header observations, and audio
codec counts under its own closed Schema. See [v2 semantics](../media-summary-v2.md).

Desktop snapshots use `documentType: "desktopDiagnosticSnapshot"` and
`schemaVersion: 1`. They preserve the selected, already completed bounded desktop
results, including explicit coverage and omissions, rather than extending a CLI
report. They include a source basename, creation timestamp and opaque session/
request identities, so repeated saves are not byte-identical. Source absolute
paths are absent; uint64 coordinates remain canonical decimal strings. The
encoded snapshot is at most 2 MiB, with at most 4096 actual Hex bytes, 512 trend
points and one requested Summary event page of at most 100 entries. Details and
source-validation limits are defined in [ADR-019](../decisions/019-desktop-diagnostic-snapshot.md).

## Opt-in AVC prefix detail v3

[`flv-avc-tag-detail-v3.schema.json`](./flv-avc-tag-detail-v3.schema.json) adds
`parameterContext`, PPS `prefix` and NAL `sliceHeaderPrefix` to the explicit AVC
v3 document only. V1/v2 stay frozen. PPS base completeness is unchanged; the
separate `prefixComplete` covers only the supported prefix, never full header or
picture decoding. Missing fields remain absent. The context records bounded
source-order replay and may be missing, invalidated or budget-exhausted while
NAL framing remains available. A referenced SPS/PPS span can point into an
earlier Tag. Slice RBSP mappings describe inspected bytes, not the opaque
compressed body. Limits, unsupported FMO and error semantics are defined in
[ADR-024](../decisions/024-bounded-pps-slice-prefix.md).

## Opt-in MP3 side-info detail v3

[`flv-mp3-tag-detail-v3.schema.json`](./flv-mp3-tag-detail-v3.schema.json) adds
per-frame `sideInfo` and `dependency`, retaining version-dependent fields and
exact source-bit Spans. Complete side information is not proof of audio
decodability. `before_tag` is missing history, not corruption; `availableBits`
counts only visible physical slots, not a reconstructed reservoir. Partial
syntax keeps decoded invalid values and omits unread fields. See
[ADR-025](../decisions/025-avc-checkpoints-mp3-side-info.md) for the bounds,
conditional fields and AVC v3 segmented counter semantics. V1/v2 remain frozen.

## Deterministic CLI encoding

Tag pages use `documentType: "flv-tag-page"`, `schemaVersion: 1`; their bounded
`tags` array retains global IDs and spans. Pages do not extend scan-report v1.
See [index and cancellation semantics](../tag-index-v1.md). Cancelled CLI work
exits 130 and may leave incomplete stdout, which callers must discard.

For the same bytes, scanner/detail/summary options, schema version, and MuxRay build, repeated CLI reports must be byte-identical within the same output mode:

- the default is one compact UTF-8 JSON object followed by one LF;
- `--pretty` uses two-space indentation and one trailing LF;
- object fields use the order shown in the schema/Go report DTO;
- tags are in source order and have contiguous, global zero-based IDs; a page starts at `page.start`, not necessarily zero;
- diagnostics are in scanner discovery order;
- AMF0 roots, flat arena nodes, and properties are in parser discovery/source order;
- AAC framing diagnostics precede AudioSpecificConfig diagnostics, and AAC fields remain in parser/DTO order;
- AVC framing diagnostics precede avcC or NAL-unit diagnostics; parameter sets and NAL units remain in source order;
- MP3 framing diagnostics precede first-frame-header diagnostics; fixed header fields remain in source-bit order;
- media-summary keyframes, configuration events, and v2 MP3 header events remain in source order; their `truncated` flags make retained-prefix limits explicit;
- HTML escaping is disabled, and integer fields use base-10 JSON syntax;
- AMF0 Number and Date milliseconds preserve their exact 64-bit IEEE-754 payload as lowercase hexadecimal `bits`; finite values also emit a JSON number (including `-0`), while NaN and infinities emit only their class and bits;
- paths, base names, wall-clock timestamps, durations, host information, random IDs, and map-order-dependent values are absent.

`source.sizeBytes` is the only v1 source identity fact. A source hash may be added in a future compatible revision, but v1 does not compute or emit it.

JSON numbers used for offsets and sizes are unsigned 64-bit integers. Consumers must not silently round values through an IEEE-754-only representation. In particular, JavaScript clients should use a lossless JSON parser before accepting values above `2^53 - 1`.

The separate [desktop adapter](../desktop-adapter-v1.md) uses `apiVersion: 1`
DTOs with decimal strings for 64-bit values. It does not send these CLI report
documents through a desktop binding or change the CLI schemas above. Its
Go/JSON shape tests are separate from CLI Schema validation. The Wails desktop
module commits generated bindings and checks repeatable generation; see the
[desktop development guide](../../desktop/README.md). Saved desktop snapshots
have their own versioned Schema and validation, independent of the IPC API version.

## Field semantics

- A `span` is half-open in bit space: it starts at `(byteOffset, bitOffset)` and covers exactly `bitLength` bits. Bit offset zero is the most-significant bit of the first byte.
- `scan.bytesScanned` is the final sequential cursor, not a performance counter. `scan.complete` means the scanner reached source EOF after complete framing; warnings can coexist with `true`.
- `header.span` covers only the fixed nine-byte FLV header. `header.extraDataSize` records declared extension bytes between byte 9 and `dataOffset`.
- `header.previousTagSize0` is zero both when a zero value was read and when it was unavailable. `FLV_PREVIOUS_TAG_SIZE0_TRUNCATED` distinguishes the latter.
- A tag `span` covers its eleven-byte header plus the available payload, but not the following `PreviousTagSize` field. `payloadSpan`, `availableDataSize`, and `payloadComplete` make a truncated payload explicit.
- `previousTagSize.span` is present only when the complete four-byte value was read. `expected` remains available when the value is absent.
- Diagnostic `code` is the compatibility key. `message` is stable human-readable text, but clients must not branch on it.
- Invalid UTF-8 is retained byte-for-byte only inside the Go parser result. JSON encoding replaces each invalid sequence with U+FFFD and `AMF0_UTF8_INVALID` points to the first invalid source byte.
- `detail.complete` means the selected ScriptData payload was fully parsed and has its required event/value roots. A warning can coexist with `true`; any parser/semantic error makes it `false`.
- `node.complete` means that node's complete AMF0 grammar was consumed. On malformed input, the marker, consumed node span, relationships, and any independently complete syntax fields may remain while undecoded scalar members are absent.
- In an AAC document, `detail.complete` means the complete selected FLV AAC payload was classified and, for a sequence header, its supported AudioSpecificConfig syntax completed. Raw AAC access-unit bytes are described by `rawDataSpan` and are not decoded.
- AAC `bitsConsumed` counts syntax actually inspected. `audioSpecificConfig.syntaxSpan` covers those bits and `trailingSpan` preserves the remaining bounded payload without treating it as parsed syntax.
- `packetType: sequenceHeader` requires `audioSpecificConfig`; `packetType: raw` requires `rawDataSpan`. Unknown or truncated packet types are incomplete and expose neither member.
- Every AAC detail error has `recoveryOffset` equal to the exclusive end of the selected FLV Tag payload. Warnings have no recovery offset and can coexist with `detail.complete: true`.
- In an AVC document, `detail.complete` means FLV AVC framing was classified and the selected sequence header, NALU packet, or end-of-sequence packet satisfied its bounded phase-one syntax contract.
- AVC NALU packet boundaries require a one-, two-, or four-byte NAL length size. The CLI resolves it from an explicit option or the nearest usable preceding sequence header; missing context is reported and never replaced by a guessed four-byte width.
- AVC parameter-set and NAL-unit payloads are represented by source spans. Their bytes are not copied into JSON and slice-layer H.264 syntax is outside the phase-one contract.
- Every AVC detail error has `recoveryOffset` equal to the exclusive end of the selected FLV Tag payload. Warnings have no recovery offset and can coexist with `detail.complete: true`.
- For `complete: false` AVC objects, the Schema validates the permitted partial data shape and branch dependencies; it is not a byte-for-byte proof of every parser prefix. Producer tests separately validate the actual emitted truncation prefixes.
- In an MP3 document, parsing is bounded to the one-byte FLV AudioTagHeader and the first four-byte MPEG Audio header. `audioDataSpan` covers all SoundData and `unparsedDataSpan` covers bytes after the inspected fixed header; neither causes frame bodies to be copied into JSON.
- MP3 `detail.complete: true` means the FLV header and one MPEG Layer III fixed header satisfy phase-one syntax checks. It does not prove that a complete compressed frame, CRC, VBR header, or later frame exists. `bitsConsumed` is therefore exactly 40 for any decoded frame header, independent of Tag payload size.
- `bitrateMode: indexed` publishes table-derived bitrate and, when sampling frequency is also valid, frame length. `bitrateMode: free` is valid and complete with `MP3_FREE_FORMAT_UNRESOLVED`, but intentionally omits `bitrateKbps` and `frameLengthBytes` because one header cannot determine them.
- Every MP3 detail error has `recoveryOffset` equal to the exclusive end of the selected FLV Tag payload. `MP3_FREE_FORMAT_UNRESOLVED` and `MP3_EMPHASIS_RESERVED` are warnings without a recovery offset and can coexist with `detail.complete: true`.
- In media-summary v1, only AAC raw and AVC NALU Tags are supported media observations; v2 also includes MP3 Tags with a complete first Layer III header. Track `timing` uses raw unsigned 32-bit FLV DTS without automatic unwrapping, and `sampleCount` counts Tags. `videoPts` adds signed 24-bit CompositionTime exactly, so each value is in `-8388608..4303355902`. `rangeMs` is numeric `max-min`, while `forwardDeltaTotalMs` sums only strictly positive source-order deltas, never crosses a regression edge, and is not a wall-clock elapsed duration.
- Media bitrate uses `flvMediaTagCoveredDataSizeOverForwardDtsIntervals`: a same-DTS byte group is included only when a later, greater DTS closes its interval in the same segment. `coveredIntervalMs` sums only those intervals; segment-tail bytes are excluded because their duration is unknown.
- V2 audio and MP3 bitrate additionally break coverage on sound-format switches, unknown/unclassified audio, and MP3 parameter changes. Timelines still describe all accepted Tags in source order. Header-declared `bitrateKbps` and DTS-covered interval bitrate have distinct meanings; free-format can have only the latter.
- AVC `pts` is `dts + signed compositionTime`. Declared keyframes come from FLV `FrameType=1`; they are not proof of an IDR NAL. GOP durations never cross DTS regressions, codec/end-of-sequence boundaries, or an actual complete AVC configuration change.
- Configuration fingerprints cover only raw ASC/avcC bytes. `changedFromPrevious` appears only when the current complete configuration was compared with a preceding complete configuration in the same codec epoch; absence means no comparison was made. `changeCount` counts changed comparisons across all source events even when the retained event prefix is truncated. Malformed configurations do not replace the baseline. Leaving AAC/AVC, or losing a trustworthy codec identity at an unusable media Tag, clears that codec's baseline; AVC end-of-sequence also clears it. First, malformed, and budget-skipped AVC configurations conservatively break GOP continuity; the first complete AVC configuration after malformed/skipped uncertainty is also a recovery barrier, while a repeated identical complete configuration in a known state is not. AVC dimensions are the first complete SPS summary and do not claim that slices reference that SPS.
- `avSync` uses signed video-PTS-extrema-minus-audio-DTS-extrema values and reports whether the sample-start ranges overlap. It is present only in a complete summary with audio DTS, video DTS, and video PTS timelines and no DTS regression on either track. When `rangesOverlap` is false, `overlapMs` is zero. It does not model decoder delay or decoded sample duration.
- V2 additionally omits `avSync` when `audioCodecs.switchCount` is nonzero. MP3 malformed events omit parameter and bitrate facts; complete events compare MPEG version, sample rate, and channel mode separately from bitrate mode/indexed value. Unknown identities, codec switches, malformed headers, and backward MP3 DTS clear the comparison baseline.
- `summary.complete: true` excludes error-severity entries from both `scanDiagnostics` and `analysisDiagnostics`; warnings may still coexist with a complete summary when they do not make classification incomplete.

The constraints between duplicated compact fields—such as `tag.offset == tag.span.byteOffset`, `availableDataSize <= dataSize`, and `scan.bytesScanned <= source.sizeBytes`—are semantic invariants tested by MuxRay even where JSON Schema cannot express them portably.
