# MP3 diagnostics v1

These codes are the compatibility keys emitted by
`documentType: "flv-mp3-tag-detail"`. Consumers must branch on `code`, not on
the human-readable `message`.

## Phase-one scope

The parser accepts a complete, unfiltered FLV Audio Tag with `SoundFormat=2`
and inspects only its one-byte AudioTagHeader plus the first four bytes of
SoundData. Those four bytes are interpreted as one MPEG Audio Layer III fixed
header. It does not search for sync, traverse later frames, validate frame-body
availability, verify CRC data, parse VBR metadata, or decode audio.

The reserved FLV `SoundFormat=14` 8 kHz MP3 variant is outside v1. Selection
failures such as a nil source, non-Audio or filtered Tag, physically incomplete
Tag, unsupported SoundFormat, invalid options, and source read errors are Go
errors and do not produce a detail document.

The FLV framing is defined by the
[Adobe Flash Video File Format Specification v10.1](https://veovera.org/docs/legacy/video-file-format-v10-1-spec.pdf).
The phase-one bitrate, sampling-frequency, samples-per-frame, and frame-length
derivations follow the MPEG Audio header tables and Layer III calculations also
implemented by
[FFmpeg's MPEG audio header decoder](https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/mpegaudiodecheader.c).

## Stable codes

| Code | Severity | Meaning |
|---|---|---|
| `MP3_FLV_AUDIO_HEADER_TRUNCATED` | error | The complete FLV Tag has an empty payload, so its one-byte AudioTagHeader is absent. |
| `MP3_FRAME_HEADER_TRUNCATED` | error | SoundData contains fewer than the four bytes required for the fixed MPEG Audio header. |
| `MP3_SYNC_INVALID` | error | The eleven-bit MPEG Audio sync word is not all ones. The parser does not scan ahead for another candidate. |
| `MP3_VERSION_RESERVED` | error | MPEG Audio version ID has the reserved encoded value `1`. |
| `MP3_LAYER_RESERVED` | error | The layer field has the reserved encoded value `0`. |
| `MP3_LAYER_NOT_III` | error | The header identifies Layer I or Layer II, which is not valid MP3 SoundData for this contract. |
| `MP3_BITRATE_INDEX_INVALID` | error | Bitrate index `15` is forbidden. |
| `MP3_SAMPLING_FREQUENCY_RESERVED` | error | Sampling-frequency index `3` is reserved. |
| `MP3_FREE_FORMAT_UNRESOLVED` | warning | Bitrate index `0` selects free format; one header cannot derive bitrate or frame length, so both fields are omitted. |
| `MP3_EMPHASIS_RESERVED` | warning | The emphasis field has reserved encoded value `2`; all other independently valid fields remain available. |
| `MP3_DIAGNOSTIC_LIMIT_REACHED` | error | The configured diagnostic budget was exhausted and additional diagnostics were omitted while a fatal condition and recovery position were retained. |

## Completeness and derived fields

- `detail.complete` and `firstFrameHeader.complete` have the same value whenever
  a four-byte header is present. An error makes both false; warnings can coexist
  with true.
- Indexed Layer III bitrate values are derived from the version-specific MPEG
  table. Sampling frequency is derived only for a non-reserved version and
  frequency index. Samples per frame are 1152 for MPEG-1 Layer III and 576 for
  MPEG-2/2.5 Layer III.
- `frameLengthBytes` is published only when both indexed bitrate and sampling
  frequency are available. It uses `floor(144000 * kbps / Hz) + padding` for
  MPEG-1 and `floor(72000 * kbps / Hz) + padding` for MPEG-2/2.5.
- `crcProtected` reports the inverse of the MPEG protection bit. Phase one does
  not read or verify the following CRC word.
- A complete free-format header intentionally has no `bitrateKbps` or
  `frameLengthBytes`. Complete means that the bounded fixed syntax is valid,
  not that the end of a compressed frame is known.

## Offsets and recovery

- `offset` and `bitOffset` identify the first invalid or unavailable source bit
  in absolute coordinates of the original FLV file.
- Every emitted error has `recoveryOffset` equal to the exclusive end of the
  selected FLV Tag payload. Malformed MP3 syntax therefore cannot consume the
  following Tag.
- Warnings have no `recoveryOffset` because parsing remains complete.
- `bitsConsumed` counts inspected syntax, not recovery: it is `0` when the FLV
  header is absent, `8` when SoundData has no complete fixed header, and `40`
  after a four-byte MPEG header is decoded.

## Opt-in side-info diagnostics (detail v3 and desktop)

These additions are emitted only by the deeper parser; v1/v2 output remains
unchanged. All three are errors with recovery at the selected Tag payload end.

| Code | Meaning |
|---|---|
| `MP3_SIDE_INFO_TRUNCATED` | The version/channel-dependent fixed side information is incomplete; unread fields stay absent. |
| `MP3_SIDE_INFO_VALUE_INVALID` | A decoded side-info constraint is invalid, such as `big_values > 288` or switched `block_type = 0`. |
| `MP3_MAIN_DATA_LENGTH_INVALID` | Declared part2_3 bits exceed the full backpointer distance plus current physical main-data slots. |

A `before_tag` dependency is not an error: earlier history was not inspected.
These checks do not validate Huffman data, scalefactors or reservoir usage.
See [ADR-025](decisions/025-avc-checkpoints-mp3-side-info.md).
