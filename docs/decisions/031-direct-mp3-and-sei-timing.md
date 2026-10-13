# ADR-031: Direct MP3 navigation and contextual SEI timing fields

Status: Accepted

Date: 2026-10-13

Extends ADR-030. Supersedes its exclusion of buffering-period and picture-timing
syntax and, only at the new timing depth, its parameter-replay SEI exclusion.
Sources remain read-only. No picture/audio decoding or HRD schedule is added.

## MP3 navigation

The desktop accepts an explicit, zero-based Tag-relative MP3 frame number as a
decimal uint32. It requests the containing multiple-of-64 page and that exact
frame through the existing detail protocol. Previous/next frame crosses page
boundaries when the boundary is known. Preview and page browsing remain available.

Direct navigation is not constant-time seeking. A request resumes from the
nearest retained checkpoint and advances at most 16 pages, then returns its
real cursor. Only an explicit continue advances another request. The original
64-frame / 1 MiB coverage / 64 KiB read bounds, 64 document checkpoints, source
identity checks, cancellation fences and shared serial worker are unchanged.
Input errors, a confirmed end before the target, and an unresolved boundary
have distinct feedback. The user can choose another target after a stop.

The 256 retained summary events do not limit direct frame selection. An unknown
or damaged boundary is never bypassed by searching for a sync word. No frame
timestamp or playable duration is inferred. MP3 CLI v1–v3 are unchanged.

## AVC detail v6

Explicit `detail --schema-version 6` extends v5 with `bufferingPeriod` and
`pictureTiming` on SEI messages. CLI v1–v5 and the default v1 remain frozen.
Desktop full detail enables the new depth; batch header searches do not.

Buffering-period parsing uses its explicit SPS ID and retains the NAL/VCL CPB
initial delays and offsets (at most 32 entries per HRD). Picture timing retains
CPB/DPB delays, pic_struct and at most three clock timestamps, including full
or partial time fields and signed time_offset. Conditional fields are absent
when not encoded; defaults are not assigned fictional source spans.

A timing context contains state (`resolved`, `missing`, `ambiguous`), its
selection evidence (`bufferingPeriod` or same-packet `slice`), SPS ID, original
SPS/evidence spans, and a fixed-size summary of the HRD syntax widths and VUI
timing declarations. Those declarations do not establish measured frame rate,
playback timing, decoder-buffer occupancy or a FLV DTS/PTS conversion.

No most-recent-ID or single-SPS fallback is used. A complete buffering-period
message activates its validated dependency. A same-packet ordinary slice may
identify the dependency for preceding picture-timing SEI; already-read bounded
payloads are deferred until packet inspection finishes, without extra reads.
Conflicting picture identities, ambiguous boundaries, incomplete coverage and
unavailable parameters remain unknown. This conservative packet association is
not a general access-unit parser. A SEI after VCL cannot borrow the preceding
picture's identity.

Timing-depth replay retains validated fixed-size SPS timing summaries and
activation evidence in compact checkpoints. It reads bounded SEI and ordinary
slice prefixes to maintain that evidence; it does not decode slice bodies.
The parse-depth flag participates in checkpoint identity. Configuration changes,
invalid parameter replacements, unsupported VCL and source/lifecycle barriers
invalidate affected timing evidence instead of reusing stale widths.
Legacy-depth replay retains its previous behavior.

Existing limits continue to apply: SEI 64 KiB / 64 messages per NAL and
256 KiB / 256 messages per parse; bounded source mapping, parameter replay,
slice-prefix reads and 512 desktop fields. Retained timing summaries contain
no RBSP bytes, dynamic maps or source handles. Delayed messages are discarded
at the end of the selected packet parse. Missing/ambiguous context produces an
explicit incomplete-analysis diagnostic, not a claim of media corruption.
Original source-bit mapping excludes emulation-prevention bytes; split fields
expose separate Hex targets. Snapshot v3 retains the new generic fields under
the existing request identity; its envelope does not change.

## Regression and performance

Independent authored syntax fixtures establish values, source bits, context
changes, truncation, ambiguity and old-version isolation. Negative schema and
oracle checks reject invented complete syntax. Linux native controls exercise
direct frame input, explicit continuation, page-boundary steps, timing source
links, export, source immutability and normal resource release.

Benchmarks separate parse/navigation cost from native UI orchestration. MP3
"cold" means an empty document checkpoint cache, not a cold OS page cache.
Already-open source files and prebuilt indexes exclude setup/scanning time.
Record source reads/bytes, continuation requests, elapsed time and Go allocation
counts/bytes. Allocation bytes are not RSS or peak resident memory. Capture the
toolchain and host with repeated measurements; use deterministic read/budget
checks, not universal wall-clock thresholds across machines.

Linux evidence does not establish Windows/macOS native acceptance. Authored
structural samples do not establish media decoding quality.

References: [ITU-T H.264 Annex D](https://www.itu.int/rec/T-REC-H.264),
[FFmpeg CBS H.264 syntax](https://ffmpeg.org/doxygen/trunk/cbs__h264__syntax__template_8c_source.html).
