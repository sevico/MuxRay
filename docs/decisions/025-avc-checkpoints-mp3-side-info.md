# ADR-025: Resumable AVC parameter context and MP3 side information

Status: Accepted

Date: 2026-10-07

## AVC context

Retain the existing candidate index for NAL framing and replay parameter sets in
source order. Each desktop request still has an 8,192-Tag / 1 MiB replay budget
and the document's shared three-second cooperative deadline. A budget-limited
result exposes its exclusive completed boundary and an explicit “继续准备参数”
action. Each click creates a new read request; lost-response confirmation keeps
that request ID. No automatic unbounded background replay is started.

Keep at most eight immutable checkpoints per document. A checkpoint contains
the framing origin, normalized options, invalidation state, next Tag ID, and
only the fixed SPS/PPS scalars and original NAL spans consumed by slice parsing.
It retains no payload, RBSP mapping or VUI tree. Reachable storage is checked
against a 512 KiB per-checkpoint bound (4 MiB total). The nearest matching
checkpoint at or before the selected Tag can serve forward or backward jumps.

Only fully processed Tags advance the checkpoint. Incomplete configuration
reads do not replace the last committed parameter state. Bad replacement
configurations, video barriers and EOS retain the existing invalidation rules.
The document commits only after cancellation, current-request and source-state
checks pass, and clears checkpoints on source invalidation or close. A segment
that cannot finish even one Tag has no continuation action. Limits therefore
remain effective for a single oversized Tag as well as a long recording.

Desktop `startTagId` remains the original configuration; `contextSegmentStartTagId`
is the current segment start, `nextTagId` its completed exclusive boundary, and
`checkpointCount` the cache count after successful publication. Counters describe
this request's actual work, which may include an unfinished Tag.

CLI AVC detail v3 uses the candidate index and automatically attempts at most
128 bounded segments, honoring cancellation. Existing v3 JSON members retain
their types; work counters accumulate across segments and may count a retried
Tag twice. They are not unique Tag distances. Source identity, size and mtime
are checked after scanning and before emitting v3 JSON. This detects ordinary
file changes, not in-place changes that preserve all metadata. Versions 1 and 2
keep their previous parsing and JSON behavior.

## MP3 side information

Opt-in `mp3.Options.InspectSideInfo` also enables frame traversal. Desktop
selected details enable it; CLI uses `detail --schema-version 3` and the separate
closed `flv-mp3-tag-detail-v3.schema.json` contract. MP3 v1/v2 and Summary v1/v2
retain their previous depth and output.

Parse fixed Layer III side information for MPEG-1/2/2.5 mono and stereo:
`main_data_begin`, private bits, MPEG-1 SCFSI, granule/channel lengths, gains,
scalefactor compression, switching/block flags, table selections, subblock gains,
encoded region counts and final flags. Conditional fields remain absent when not
encoded. Each decoded scalar retains its original absolute source-bit Span;
truncated fields remain absent. Structural checks include `big_values <= 288`
and a nonzero switched `block_type`. Protected frames reuse the already-read
CRC prefix. Inspection reads at most 32 side-info bytes per frame and never reads
the main-data body to derive these fields.

`dependency` describes physical main-data slots, not an unused bit reservoir or
decodability. `knownPreviousBytes` counts the compatible uninterrupted slot
prefix in this Tag, including bytes that earlier audio may have consumed.
`none` means zero backpointer; `within_tag` means the backward slot window lies
in that prefix; `before_tag` means it needs bytes before the Tag; `unknown` means
syntax, frame boundary, CRC or continuity prevents that inference. Missing
history before the Tag is not a corruption diagnostic. Parameter changes and
bad frames reset known history; no cross-Tag reservoir is reconstructed. An
`unknown` result may retain geometric counts derived from already-read fields
(for example when the declared bit total is impossible); those counts are not
a completeness or decodability conclusion.

`part23Bits` is the sum of declared `part2_3_length` values. `availableBits` is
only the visible suffix of the backward slot window plus the current main-data
area. A `before_tag` result does not establish that its leading audio bits are
available. Emit `MP3_MAIN_DATA_LENGTH_INVALID` only when declared bits exceed
even the full backward distance plus current frame slots. This geometric check
does not validate scalefactors, Huffman data or remaining reservoir capacity.

The existing limits remain: default 64 retained frames (hard maximum 1,024),
1 MiB source coverage, bounded diagnostics and first-frame VBR metadata. Desktop
keeps its 512-field budget, presents frame/CRC/VBR/dependency summaries first,
and expands at most two side-info records. Additional records produce an
explicit `sideInfoOmittedFrames` field and `Truncated=true`. CLI v3 retains
side-info for all inspected frames within its frame budget.

## Verification

Use generated, independently decoded MPEG version/channel samples and a
separate source-bit oracle. Compare all values, conditional presence, original
Spans, dependency facts, diagnostics and v1/v2 compatibility. Mutations cover
truncation, bad values, invalid window type, impossible main-data length and CRC
mismatch. Linux acceptance extends the existing six codec and two PPS/slice
rounds with three MP3 cases and one long AVC case. The latter exports three
replay stages and two random-jump results through the real GTK/WebKit build.

The fixed layout and backward-window interpretation were cross-checked against
[FFmpeg 8.1 `mp_decode_layer3`](https://ffmpeg.org/doxygen/8.1/mpegaudiodec__template_8c_source.html).
This work does not add audio decoding, full slice headers, picture reconstruction,
or Windows/macOS native acceptance.
