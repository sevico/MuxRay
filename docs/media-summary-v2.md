# Media summary v2

`muxray summary --schema-version 2` opts in to bounded AAC/AVC/MP3 analysis.
The default command and `--schema-version 1` keep the AAC/AVC v1 contract.
The document identity remains `flv-media-summary`; consumers select the
validator using both `documentType` and `schemaVersion`.

```text
go run ./cmd/muxray summary --schema-version 2 --pretty sample.flv
go run ./cmd/muxray summary --schema-version 2 --max-mp3-header-events 128 sample.flv
```

The [v2 Schema](schemas/flv-media-summary-v2.schema.json) and
[ADR-007](decisions/007-mp3-media-summary-v2.md) define this contract. V2 adds
`summary.audioCodecs` and `summary.mp3`, and includes accepted MP3 Tags in
aggregate audio timing and interval bitrate. Scan and codec-detail contracts
are unaffected.

## MP3 observation scope

Only complete, unfiltered `SoundFormat=2` Tags with a FLV audio-header byte are
identified as MP3. The analyzer reads that byte and at most four MPEG header
bytes. It does not inspect frame bodies, later frames, CRC data, or VBR headers.
`complete: true` means the promised classification and syntax observations
succeeded, not that the compressed audio is complete or decodable.

`mp3.track` has the ordinary track counters, DTS timeline, and optional bitrate.
`mp3.headers` records `eventCount`, `validCount`, `malformedCount`,
`freeFormatCount`, `parameterChangeCount`, `bitrateChangeCount`, a retained
source-order `events` prefix, and `truncated`.

Each event records `tagId`, the Tag-header `offset`, raw `dts`, and `complete`.
Complete events also report:

- `parameters`: named MPEG version and channel mode, sampling frequency,
  channel count, and samples per frame;
- `bitrateMode`: `indexed` or `free`; only indexed events have `bitrateKbps`;
- `parametersChangedFromPrevious` and `bitrateChangedFromPrevious`, both
  present only when compared with a preceding complete header in the same
  trusted MP3 epoch.

The parameter tuple compares version, sampling frequency, and channel mode.
Bitrate compares mode and indexed kb/s separately. Padding, CRC protection,
private, emphasis, copyright, and original flags do not trigger changes.
Observed bitrate changes are not proof of a VBR stream; identical first
headers are not proof of CBR. Malformed events omit parameter/bitrate facts;
use `detail --tag` for their partially decoded fields and source spans.

Tag counts and timeline `sampleCount` count FLV Tags. The first MPEG header's
`samplesPerFrame` is not multiplied by Tag count to invent decoded duration.

## Codec and interval boundaries

`audioCodecs` partitions all audio Tags into `aacTagCount`, `mp3TagCount`,
`otherTagCount`, and `unknownTagCount`. A filtered, incomplete, or empty audio
Tag has unknown identity. Reserved `SoundFormat=14` remains other/unsupported.
`switchCount` counts changes between consecutive trustworthy SoundFormat
values in audio source order, including unsupported codecs. Unknown identities
clear this comparison baseline; intervening video/script Tags do not.

MP3 header comparisons restart after a codec switch, unknown audio identity,
malformed MPEG header, or backward MP3 DTS. Returning to AAC similarly starts
a fresh AAC configuration-comparison epoch. A switch does not reset video GOPs.

Timelines retain raw uint32 DTS, source-order comparisons, and numeric extrema
over all accepted Tags. They do not unwrap timestamps or infer final duration.
The interval bitrate includes FLV Tag DataSize bytes (including audio framing,
excluding the eleven-byte Tag header and four-byte PreviousTagSize) only when
the next strictly greater DTS covers that group in the same segment:

```text
averageBitsPerSecond = floor(coveredBytes * 8000 / coveredIntervalMs)
```

Equal-DTS groups combine bytes. Regression, codec switch, unknown/unclassified
audio, and MP3 parameter change discard the pending group, retaining previously
committed totals. The last group of every segment remains uncovered. A bitrate
change alone is not a boundary. For example, 5-byte MP3 Tags at 0 and 26 ms
cover 5 bytes over 26 ms (1,538 bit/s of FLV media DataSize); an AAC Tag at 52 ms
does not give the MP3 Tag at 26 ms another inferred interval.

Free-format can therefore have a measured interval bitrate even though its
first header has no derivable `bitrateKbps`. These fields have different bases.
`avSync` keeps the v1 complete/non-regressing range guards and is additionally
omitted when audio `switchCount` is nonzero. It is a numeric range comparison,
not decoded lip-sync.

## Budgets, diagnostics, and exits

`--max-mp3-header-events` is v2-only, including when explicitly set to zero.
Zero selects 256 retained events; the hard maximum is 65,536. Retention does
not stop parsing headers or counting changes, and `truncated: true` alone does
not make a summary incomplete. MP3 fixed headers do not consume the cumulative
AAC/AVC configuration-byte budget. The existing global analysis-diagnostic
budget still applies to all codecs.

V2 reuses the [summary-owned diagnostics](media-summary-diagnostics-v1.md),
with `MEDIA_AUDIO_CODEC_UNSUPPORTED` now meaning outside AAC/MP3. MPEG header
diagnostics use the existing [MP3 registry](mp3-diagnostics-v1.md), adding the
owning `tagId`. In this path they are:

| Codes | Severity | Summary effect |
|---|---|---|
| `MP3_FRAME_HEADER_TRUNCATED`, `MP3_SYNC_INVALID`, `MP3_VERSION_RESERVED`, `MP3_LAYER_RESERVED`, `MP3_LAYER_NOT_III`, `MP3_BITRATE_INDEX_INVALID`, `MP3_SAMPLING_FREQUENCY_RESERVED` | error | MP3 and aggregate audio count the Tag as unclassified; recover at its payload end. |
| `MP3_FREE_FORMAT_UNRESOLVED`, `MP3_EMPHASIS_RESERVED` | warning | Can coexist with a complete event/summary; no recovery offset. |
| `MEDIA_DIAGNOSTIC_LIMIT_REACHED` | error | Diagnostic output was truncated; exact media/header counters continue. |

Missing FLV audio-header bytes use `MEDIA_AUDIO_HEADER_TRUNCATED`; filtered
and incomplete Tags are diagnosed before codec parsing. Error/warning severity,
bit offsets, and recovery semantics stay consistent with the existing parsers.

Exit codes remain 0 for complete analysis without errors, 2 for diagnostic or
incomplete results (JSON is still written), 1 for operational failures, and 64
for usage/limit errors. Free-format warnings alone return 0.

Producer tests verify arithmetic invariants that JSON Schema cannot express
portably: codec totals partition all audio Tags; MP3 header events equal MP3
Tag count; valid plus malformed equals event count; and retained-prefix limits
do not alter counts, comparisons, or covered bytes.
