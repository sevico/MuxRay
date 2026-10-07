# ADR-023: Bounded H.264 VUI and MP3 frame inspection

Status: Accepted

Date: 2026-10-07

Extends ADR-004/006's selected-Tag codec details. Existing default CLI v1
documents, media-summary v1/v2 and desktop snapshot v1 remain compatible.

## Decision

Add explicit CLI `detail --schema-version 2` for AVC and MP3. Version 1 remains
the default and keeps its original parsing depth and JSON. Other codec details
have no version-two contract and reject this request. Deep malformed syntax
still returns a partial JSON document with exit 2; input/I/O failures return 1,
invalid arguments return 64, and cancellation keeps the existing 130 behavior.

Desktop selected-Tag details enable the extensions through the existing bounded
field tree. They share existing cancellation, source-before/after verification,
single read worker, 512-field/64-diagnostic/256-KiB projection bounds and Hex
navigation. No new Wails method, full-file automatic read or new snapshot format
is introduced. A frozen snapshot may include already loaded deep fields in its
existing generic detail section.

### H.264 SPS VUI

Add an opt-in `avc.Options.ParseVUI` switch. Parse VUI from the already bounded
SPS RBSP buffer, retaining mapped source spans for each present field: aspect
ratio/SAR, overscan, video format/range/colour description, chroma location,
timing, NAL/VCL HRD, pic_struct and bitstream restrictions. Each HRD block has
at most 32 CPB entries. Validate SPS trailing bits on this path, even when the
SPS declares no VUI. Invalid counts never drive an unbounded allocation or loop.

Keep the original SPS phase-one completeness/bit counters separately from VUI
completeness. A selected detail can therefore expose a usable base SPS with an
incomplete VUI. Absent or unread fields are distinct from encoded zero/false.
Extended SAR zero terms mean unspecified; only non-zero terms form a ratio.
Timing and HRD are bitstream declarations, not measured frame rate/bitrate or
proof of playback cadence. Preserve raw integers rather than rounding to FPS.

The decoder configuration lookup and Summary retain their original options.
Only the final selected-Tag parse enables VUI. A broken VUI does not retroactively
invalidate an avcC's independently usable NAL length declaration. Ordinary slice
bodies stay opaque. This increment does not add deep PPS, slice reconstruction,
picture decoding, or IDR/GOP verification from decoded pictures.

RBSP coordinates and original EBSP coordinates remain distinct. A field crossing
an emulation-prevention byte must keep its multiple source segments; the desktop
must not invent a contiguous field span. Exact encoded spans, including the
whole parameter set, can still navigate to Hex.

### MP3 bounded frames, VBR metadata and protected headers

Add `mp3.Options.InspectFrames`, disabled by default. Walk complete MPEG Audio
Layer III frame boundaries only within the selected FLV SoundData, using each
validated header's frame length. Default retention is 64 frames, hard maximum
1024, with a fixed 1 MiB source-coverage budget. Do not search for sync patterns,
infer free-format length, cross Tag boundaries or retain compressed frame bodies.
Read only fixed headers and the bounded metadata/protection bytes required by
this inspection. A truncated/invalid header or body boundary stops the walk;
retain completed evidence and an explicit uninspected remainder.

Recognize Xing/Info in the first frame at the version/channel-specific fixed
location, and VBRI at first-frame offset 36. Parse bounded Xing flags and optional
frame count, byte count and quality, retaining the 100-byte TOC's source span;
parse the supported VBRI version
and its count/size declarations. Do not expand VBRI's variable seek table.
Declared stream totals are separate from observed Tag frame counts: a Xing/Info
metadata frame itself can make those counts differ without malformed media.

For CRC-protected Layer III frames, compare the stored CRC with CRC-16 over the
last 16 header bits and the version/channel-specific side-info bytes only.
Report protection absence, match, mismatch and unavailable/incomplete checks
explicitly. This does not validate Huffman/main-data contents, bit-reservoir
references, decoded audio or end-to-end media integrity. Frame-boundary
completeness likewise does not claim successful audio decoding.

### Validation and references

Retain old CLI goldens and closed v1 schemas. Add separate v2 schemas, complete
and partial examples, version-selection tests, and negative schema checks.
Exercise VUI conditional branches/HRD bounds, exact EPB mappings, absent/invalid
syntax and unchanged source reads; exercise MP3 multiple boundaries, version and
channel variants, free format, truncation, count/byte budgets, VBR declarations,
CRC corruption and source failures. Include cancellation/fuzz/race checks and
real production desktop detail → Hex → native export → close/exit.

H.264 field semantics follow ITU-T H.264 Annex E, rather than treating optional
declarations as measured output. For MPEG header/VBR conventions and protected
header coverage, cross-check primary implementations:

- [ITU-T H.264 (August 2024), Annex E](https://www.itu.int/rec/T-REC-H.264-202408-S/en).
- [FFmpeg MP3 demuxer](https://ffmpeg.org/doxygen/7.0/mp3dec_8c_source.html).
- [LAME VBR tag writer](https://android.googlesource.com/platform/external/lame/+/refs/heads/jumper-stable-alt/libmp3lame/VbrTag.c).
- [LAME CRC writer](https://android.googlesource.com/platform/external/lame/+/refs/heads/master/libmp3lame/bitstream.c).
- [FFmpeg Layer III decoder](https://ffmpeg.org/doxygen/8.1/mpegaudiodec__template_8c_source.html).
