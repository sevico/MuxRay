# ADR-005: Derive bounded cross-tag media summaries without timestamp guessing

- Status: Accepted
- Date: 2026-08-30

## Context

P2 requires recomputable AAC/AVC timing, keyframe, GOP, bitrate, configuration,
and basic A/V relationship facts. The existing scan and tag-detail v1 Schemas
are closed contracts. Eagerly parsing every media payload would also make work
and memory depend on compressed access-unit contents, while treating FLV's
32-bit millisecond timestamp as a monotonic duration would silently hide
resets, wrap candidates, and malformed ordering.

## Decision

- `muxray summary` emits an independent `flv-media-summary` schemaVersion 1
  document. Existing `inspect` and `detail` output remains unchanged.
- Phase P2a classifies AAC raw Tags and AVC NALU Tags as supported media
  samples. AAC/AVC sequence headers, AVC end-of-sequence, and command frames
  are excluded from media timing. Other audio/video codecs are counted but
  explicitly unclassified; MP3 is a later P2 increment.
- FLV DTS is retained as the raw unsigned 32-bit millisecond value decoded by
  the scanner. MuxRay does not automatically unwrap it. Per-track statistics
  use source order: a strictly smaller DTS is a regression/segment boundary,
  and an equal DTS is a duplicate timestamp group.
- AVC PTS is exactly `DTS + signedInt24(CompositionTime)` for AVC NALU Tags,
  giving a provable per-sample range of `-8388608..4303355902` milliseconds.
  Sequence headers and end-of-sequence Tags are not presentation samples.
- AVC NALU summary classification inspects only the five-byte FLV AVC framing
  prefix and timing fields. It remains classifiable without a preceding avcC
  because `summary` does not traverse the length-prefixed NAL payload, require
  a NAL length size, or guess one.
- `rangeMs` is `maxMs - minMs`. `forwardDeltaTotalMs` is the sum of strictly
  positive source-order deltas and never includes a regression edge; it is not
  a wall-clock elapsed duration. Neither value estimates the duration of the
  last sample.
- Bitrate uses the basis
  `flvMediaTagCoveredDataSizeOverForwardDtsIntervals`. Bytes sharing one DTS form a
  group. A group is covered only after the next strictly greater DTS in the
  same segment is observed; the final group of every segment is excluded
  because its duration is unknown. The published rate is
  `floor(covered DataSize bytes * 8000 / covered forward-DTS interval
  milliseconds)`.
- A keyframe is the FLV-declared `FrameType=1` on an AVC NALU Tag. This is not
  an IDR proof. GOP duration is the positive DTS delta between successive
  declared keyframes in one epoch. DTS regression, codec discontinuity, AVC
  end-of-sequence, or an actual change between two complete AVC configurations
  breaks the GOP epoch. Leaving AAC or AVC, or encountering a media Tag whose
  codec identity cannot be trusted because the Tag is filtered, incomplete, or
  lacks its codec header, clears that codec's configuration-comparison epoch.
  AVC end-of-sequence also clears its epoch, so a later configuration begins a
  new baseline. First, malformed, or budget-skipped AVC configuration events
  are conservative GOP uncertainty barriers but do not fabricate a
  configuration change. The first complete configuration after an unknown
  malformed/skipped configuration state is another recovery barrier, even when
  its fingerprint equals the last complete comparison baseline. A
  repeated-identical complete configuration outside such uncertainty is not a
  barrier.
- GOP aggregates are exact over declared keyframes: `count` is the declared
  keyframe count; `closedDurationCount` counts positive DTS deltas between
  successive declared keyframes in one epoch; and `nonPositiveDurationCount`
  counts non-positive deltas in one epoch. Because a DTS regression starts a
  new epoch before keyframe comparison, the last category is normally an
  equal-DTS pair. `averageDurationMs` is
  `floor(sum(positive closed durations) / closedDurationCount)` and is zero
  when no duration is closed.
- AAC and AVC configuration fingerprints are lowercase SHA-256 over the raw
  AudioSpecificConfig or AVCDecoderConfigurationRecord bytes, excluding FLV
  codec framing. Only complete configurations replace the comparison baseline.
  `changedFromPrevious` is present only when a complete current event was
  compared with the preceding complete configuration in the same codec epoch;
  absence means no comparison was made. `changeCount` counts changed
  comparisons across all source events, independent of retained-prefix
  truncation.
  Leaving a codec family clears its comparison baseline, so returning to AAC or
  AVC begins a new epoch rather than fabricating a cross-codec change.
  AAC reports syntax-level sampling and extension sampling frequencies
  separately. AVC dimensions are explicitly the first complete SPS summary;
  the report also states complete-SPS and distinct-dimension counts and does
  not claim slice-to-SPS activation.
- Basic A/V skew uses `videoPtsExtremaMinusAudioDtsExtrema` at numeric range
  start and end and reports whether the two sample-start ranges overlap. It is
  omitted after an audio or video DTS regression, whenever classification is
  incomplete, and does not model decoder priming, codec delay, or decoded
  sample duration.
- Ordinary media Tags require only a one- or five-byte framing read. Sequence
  header bytes parsed/hashed have a cumulative budget. Keyframe entries,
  configuration event entries, and diagnostics have independent hard limits.
  Retention truncation is explicit and does not make exact aggregate counters
  incomplete; malformed, unsupported, filtered, incomplete, or budget-skipped
  inputs do.

## Consequences

- Summary work is linear in indexed Tags plus bounded configuration bytes; raw
  access units remain in the source.
- Timestamp resets and reordering remain visible instead of being normalized
  into plausible-looking durations.
- Every bitrate field can be recomputed from the stated interval-coverage
  rule, but it is deliberately not a decoded elementary-stream bitrate or a
  container-overhead bitrate.
- P2a delivers AAC/AVC cross-tag analysis. MP3, timestamp-wrap policy, paged
  sample curves, active SPS resolution from slice/PPS references, and decoded
  lip-sync measurement require later contracts.
