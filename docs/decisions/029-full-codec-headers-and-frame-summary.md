# ADR-029: Full ordinary H.264 headers and MP3 frame summary

Status: Accepted

Date: 2026-10-12

Extends ADR-023 through ADR-028. Parsing remains read-only and bounded.

## H.264 detail v4

Explicit `detail --schema-version 4` accepts AVC only. It retains v3 prefix
facts and adds separate PPS `full` and NAL `sliceHeaderFull` objects. Earlier
CLI versions and their completeness definitions remain unchanged. The desktop
uses the new depth within its existing generic bounded field tree; snapshot v3
requires no new envelope or client-supplied report data.

For ordinary NAL types 1 and 5, parsing ends exactly after `slice_header()`:
reference-list modifications, prediction weights, reference marking/MMCO,
CABAC initialization, QP/QS deltas, deblocking controls and conditional FMO
change cycle are included. Prefix completeness and full-header completeness
remain separate. RBSP source mapping distinguishes inspected prefix bytes from
full-header bytes, including emulation-prevention gaps. No fields acquire
invented contiguous source ranges across such gaps.

PPS parsing includes bounded FMO syntax for map types 0–6, optional transform
and scaling-list syntax, second chroma QP offset and trailing bits. Scaling
values follow syntax scan order; fallback matrices are not reconstructed.
SPS-dependent PPS facts are invalidated when the referenced SPS parameters
change. Missing/replaced context, unsupported extensions, invalid values,
truncation and resource limits preserve partial evidence with diagnostics.

A full slice header has at most 4,096 inspected bytes, with 256 KiB across a
selected packet; reference modification and MMCO lists have at most 64
operations, including their terminator. Explicit FMO maps retain at most 4,096
map units. Existing parameter-set, RBSP, 512 desktop field and serialized-size
bounds still apply. A complete parser result can have a truncated desktop view.
Context replay reads parameter sets, not slice bodies; bounded replay and
checkpoint policies from ADR-025 remain in effect and distinguish parse depth.

This is syntax inspection, not slice-data/CABAC/CAVLC decoding, reference-list
execution, decoded picture buffer reconstruction, SEI expansion or support for
partitioned/extension NALs. A complete header does not prove a decodable frame.

## MP3 summary v3

Explicit `summary --schema-version 3` retains v2 facts and adds `mp3Frames`.
It visits sequential frame boundaries across all indexed MP3 Tags, reads only
headers and skips bodies by validated frame length. It does not search for sync
after damage. Fixed-frame MPEG-1/2/2.5 Layer III is supported. Free-format,
unsupported layers, invalid/truncated headers and frames, unknown audio and
budget stops remain explicit; unclassified bytes are not silently counted.

Every complete frame contributes bytes, samples and exact rational duration.
Per-rate partitions preserve mixed-rate arithmetic. Parameter and bitrate
changes compare consecutive complete frames, including later frames in the
same Tag; discontinuities reset that comparison. Video interleaving does not
reset it. Event DTS belongs to the containing FLV Tag, not an invented frame
timestamp. Duration includes physical metadata/padding frames and does not
subtract encoder delay or end padding; it is not measured playback duration.

Analysis defaults to 1,000,000 attempted frame headers, with hard maximum
16,000,000. Event retention defaults to 256, hard maximum 65,536, independently
of analysis. Exhausting retained events does not stop counting; exhausting the
analysis budget produces a partial result and unresolved suffix bytes. CLI
flags are `--max-mp3-frames` and `--max-mp3-frame-events`, accepted only in v3.
The existing container scan limit and cooperative cancellation still apply.

The desktop uses these defaults, replaces first-header cards with full-frame
facts, and pages up to 256 retained events through the existing MP3 category.
The generic summary remains within 128 metrics and 24 values per event. Snapshot
v3 exports the current retained event page, not a newly computed full-file list.
Summary events can jump to physical source Hex even beyond the detail viewer's
separate 64-frame / 1 MiB inspection limit.

Summary boundary completeness does not check payloads, CRC, Huffman data or
reservoir reconstruction. Existing MP3 detail/side-info behavior is unchanged.

## Independent regression

Generated I/P/B streams use FFmpeg `trace_headers`; authored SP/SI, PPS/FMO and
scaling cases retain their own source-bit expectations. MP3 expectations use
independent raw-header arithmetic. Structural samples are not claimed to be
decodable media. CLI schemas, values, source ranges and budgets are checked
separately from real GTK/WebKit button, pagination, export and source-link
acceptance. Native helpers use private Xvfb/D-Bus and memory-only GSettings.
Linux evidence does not establish Windows/macOS native acceptance.

Primary references: [ITU-T H.264](https://www.itu.int/rec/T-REC-H.264),
[FFmpeg H.264 syntax](https://ffmpeg.org/doxygen/trunk/cbs__h264__syntax__template_8c_source.html),
[RFC 5219 section 3](https://www.rfc-editor.org/rfc/rfc5219.html#section-3),
[FFmpeg MPEG audio header arithmetic](https://ffmpeg.org/doxygen/trunk/mpegaudiodecheader_8c_source.html).
