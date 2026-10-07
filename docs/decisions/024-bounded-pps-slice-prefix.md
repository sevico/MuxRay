# ADR-024: Bounded PPS and slice-header prefix inspection

Status: Accepted

Date: 2026-10-07

## Decision

Extend selected AVC details with the PPS fields needed for a slice-header prefix,
and inspect ordinary non-IDR/IDR slice NAL units through `redundant_pic_cnt`.
This is not a full slice header, picture reconstruction, entropy decoding or a
verified GOP detector. Slice type, NAL type and FLV frame-type declarations stay
distinct. No decoding is needed to expose this useful diagnostic prefix.

Use explicit CLI `detail --schema-version 3` for AVC only. Versions 1 and 2,
including MP3 detail v2 and Summary v1/v2, retain their existing parse depth and
closed JSON contracts. Desktop selected-Tag details enable the new inspection
through the existing field tree and snapshot protocol; context lookup and parsing
remain inside the document's one read worker, deadline and source checks.

## Syntax and source evidence

Keep the original PPS base `Complete`, bit counters and fields unchanged. Add a
separate PPS prefix result through `redundant_pic_cnt_present_flag`, including
default reference counts, prediction flags, QP
declarations and deblocking flags. PPS extensions and trailing bits are outside
this prefix and must not be advertised as validated. Slice groups (FMO) are
explicitly unsupported; preserve the decoded count and diagnostic instead of
guessing subsequent syntax positions.

For NAL types 1 and 5, read `first_mb_in_slice`, `slice_type` and
`pic_parameter_set_id`, then resolve that PPS and its referenced SPS before
reading dependent bit widths and conditional fields: colour plane, frame number,
field flags, IDR ID, POC declarations and redundant picture count. Preserve
unread fields as absent, never guessed zero. Unsupported slice NAL extensions
remain explicitly outside the supported prefix scope.

Read slice bytes incrementally, at most 256 source bytes per slice and 64 KiB
cumulatively per NAL-sequence parse. Stop when the prefix is read; do not scan or
validate the compressed body. Source I/O failures remain errors, while truncated
syntax, missing parameters and exhausted budgets retain partial evidence with
diagnostics. Existing parameter-set byte/count and mapping limits still apply.
All encoded fields preserve exact RBSP-to-source mappings. The desktop may link
one contiguous mapped segment; a multi-segment field must not claim a single
contiguous source span. Derived fields do not acquire invented spans.

## Parameter context

NAL framing width and parameter-set activation are separate results. Preserve
the existing width resolver and configuration candidate index. Rebuild parameter
context from its source configuration through every preceding indexed Tag in
source order, with default limits of 8192 Tags and 1 MiB actual source reads.
Do not read ordinary slice bodies during replay. Selected-Tag NALs update local
parameter slots in source order; later NALs cannot retroactively affect earlier
slices, and parsing cannot mutate the caller's context.

Keep at most 32 SPS and 256 PPS slots. A new configuration replaces the old
configuration epoch. In-band updates replace their IDs; malformed updates must
invalidate old evidence instead of silently reusing it. EOS, codec changes and
uninspectable video ranges break continuity. Audio/Script Tags do not. Missing,
invalidated or budget-exhausted context is explicit and cannot publish stale
parameters; independently usable NAL framing can still be shown. Expose replay
coverage and counters so a long-distance limit is distinguishable from an absent
parameter set. This first implementation does not create a persistent activation
index or raise the scanner's existing maximum Tag count.

## Reproducible regression

Move generated-codec sample creation, independent CLI comparison and native
snapshot assertions from ignored local evidence into `tools/codec-regression/`.
Require new output directories, record tools/commands/hashes and preserve failed
runs. Media stays outside Git. Reuse the private D-Bus/Xvfb Linux smoke runner
with its memory-only settings and bounded process cleanup.

Validate VUI/HRD, MP3 Xing/Info/truncation/CRC fixtures plus B-frame and multi-slice
H.264. Compare PPS/slice fields and source mappings against FFmpeg
`trace_headers`; add explicit missing/switching/in-band/invalid parameter and
truncation tests. Preserve old v1/v2 goldens, add a separate v3 schema with
complete and partial fixtures, run race/vet/fuzz and real native exports.

Primary syntax references: [ITU-T H.264](https://www.itu.int/rec/T-REC-H.264-202408-S/en)
sections 7.3/7.4 and [FFmpeg coded-bitstream H.264 syntax](https://ffmpeg.org/doxygen/trunk/cbs__h264__syntax__template_8c_source.html).
