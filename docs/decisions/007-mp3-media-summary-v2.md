# ADR-007: Add MP3 cross-tag analysis through media-summary v2

- Status: Accepted
- Date: 2026-09-05
- Extends: ADR-005 and ADR-006; their v1 contracts remain in force.

## Context

MP3 detail now identifies a first MPEG Audio Layer III header, but summary v1
intentionally classifies MP3 as unsupported. The next P2 increment needs MP3
timing, interval bitrate, header-parameter observations, and explicit audio
codec boundaries without widening a closed v1 document or scanning compressed
frame bodies.

## Decision

- `muxray summary --schema-version 2` emits `documentType: "flv-media-summary"`
  with `schemaVersion: 2`. The flag defaults to 1; explicit 1 and the existing
  command retain v1 output and diagnostics. Unsupported versions are usage
  errors checked before opening the input. Core and report APIs provide
  separate v2 result/document types and explicit v2 entry points.
- V2 adds `audioCodecs` and `mp3` to the summary. Existing AAC/AVC observation
  definitions are reused. Audio media totals now include AAC raw Tags and MP3
  Tags whose first fixed Layer III header passes the phase-one syntax checks.
  Counts and timelines measure FLV Tags, not decoded frames or audio samples.
- `audioCodecs` partitions audio Tags into `aacTagCount`, `mp3TagCount`,
  `otherTagCount`, and `unknownTagCount`. Only complete, unfiltered Tags with
  an audio-header byte have a trusted codec identity. `SoundFormat=14` remains
  other/unsupported. `switchCount` counts changes between consecutive trusted
  SoundFormat values in audio source order, including other codecs. Unknown
  identities clear the comparison baseline; video and script Tags do not.
- `mp3.track` uses the existing track shape for the trusted SoundFormat=2
  subset. Malformed/truncated MPEG headers count as unclassified in both this
  subset and aggregate audio. Their diagnostics preserve absolute byte/bit
  locations, selected Tag IDs, and payload-end recovery offsets. Filtered,
  incomplete, or empty audio Tags belong only to the unknown-codec total.
- Each trusted MP3 Tag contributes a `mp3.headers` event with Tag ID, Tag
  offset, DTS, and `complete`. Only complete events expose `parameters`,
  `bitrateMode`, and (for indexed bitrate) `bitrateKbps`. The parameter tuple
  is MPEG version, sampling frequency, and channel mode; channel count and
  samples per frame are derived facts. Padding, CRC protection, private bits,
  emphasis, copyright, and original bits are not parameter-change signals.
- `parameterChangeCount` compares that tuple; `bitrateChangeCount` separately
  compares bitrate mode and indexed bitrate. The corresponding event booleans
  are absent when no comparison was made. A codec switch, unknown audio
  identity, malformed MP3 header, or backward MP3 DTS clears the MP3 comparison
  baseline. First complete headers after those barriers establish a baseline.
  A changed bitrate does not establish that the stream is VBR, and unchanged
  observed headers do not establish CBR.
- Free-format is a valid header observation with the existing warning and no
  declared bitrate. It contributes to Tag timing and interval bitrate, which
  depend on container bytes and DTS rather than a frame-length estimate.
  `freeFormatCount` counts only complete free-format header observations.
- Audio and MP3 timelines retain the v1 source-order/numeric-extrema rules
  over their accepted Tags, including comparisons between accepted Tags on
  either side of a codec boundary. Bitrate coverage is stricter: a sound-format
  switch, unknown/unclassified audio Tag, or MP3 parameter change discards the
  pending audio and MP3 timestamp groups without discarding committed totals.
  Backward DTS likewise starts a new bitrate segment. No interval closes
  across these boundaries; the final group in every segment stays uncovered.
  Indexed bitrate changes alone do not interrupt coverage. DataSize includes
  FLV audio framing, excludes the Tag header and PreviousTagSize, and uses
  `floor(coveredBytes * 8000 / coveredIntervalMs)` as in v1.
- A/V range comparison uses all accepted audio Tags and the v1 completeness
  and regression guards. V2 additionally omits it when `switchCount > 0`.
  Neither version estimates decoder delay, final-frame duration, or lip-sync.
- MP3 work reads at most the one-byte FLV header plus the four-byte first MPEG
  header per Tag; body bytes are not read or retained. Header events retain
  only a source-order prefix bounded by `--max-mp3-header-events` (default 256,
  hard maximum 65,536). Truncation is explicit and does not stop aggregate or
  change counting. Existing global diagnostic and AAC/AVC configuration
  budgets still apply. MP3 fixed headers do not consume the AAC/AVC byte budget.
- V2 has its own closed JSON Schema and goldens. Old v1 Schemas/goldens are
  unchanged. Tests must prove version isolation, exact bitrate boundaries,
  change-counter independence from retention, malformed/free-format recovery,
  bounded reads, deterministic output, cancellation, and CLI exit behavior.

## Consequences

MP3 joins AAC/AVC in an opt-in, recomputable summary while existing consumers
can keep their v1 contracts. Header observations remain deliberately narrower
than multi-frame, VBR-header, CRC, or decoded-duration analysis. Paged indexes,
scanner cancellation, corpus registration, and desktop integration are later
increments and are not implied complete by this change.
