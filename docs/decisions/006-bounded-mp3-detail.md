# ADR-006: Add bounded MP3 detail without widening media-summary v1

- Status: Accepted
- Date: 2026-08-30

## Context

ADR-003 gives each codec detail an independent, closed public contract, and
ADR-005 defines `flv-media-summary` schemaVersion 1 as an AAC/AVC summary.
Adding MP3 fields to either existing v1 shape would make pinned validators
reject new output. Scanning every MPEG audio frame inside one FLV Tag would
also make an on-demand detail request proportional to compressed payload data
instead of the small syntax prefix needed to identify the stream.

FLV identifies ordinary MP3 audio with `SoundFormat=2`. It also names
`SoundFormat=14` as an 8 kHz MP3 variant reserved for internal use. MPEG audio
free-format headers use bitrate index zero and do not contain enough
information to derive bitrate or frame length from the four-byte header alone.

## Decision

- `flv-media-summary` schemaVersion 1 remains byte-for-byte and semantically
  frozen as an AAC/AVC contract. MP3 Tags continue to be counted as
  unsupported/unclassified there. Cross-Tag MP3 timing, bitrate, and
  configuration analysis requires a future media-summary v2 or a new document
  identity; this ADR does not change the default `summary` output.
- MP3 detail uses an independent closed contract with
  `schemaVersion: 1`, `documentType: "flv-mp3-tag-detail"`, and
  `detail.kind: "mp3Audio"`. Existing AMF0, AAC, and AVC v1 documents and
  validators are unchanged.
- Phase one accepts complete, unfiltered FLV Audio Tags with
  `SoundFormat=2`. The reserved `SoundFormat=14` variant is deferred and is not
  silently treated as ordinary MP3.
- Parsing is bounded to the FLV AudioTagHeader and the first four-byte MPEG
  audio frame header in SoundData. Remaining bytes are exposed only as source
  spans. The parser does not scan for another sync word, walk later frames,
  decode audio, verify CRC data, or prove that the declared frame body is
  present.
- The selected MPEG audio header must identify Layer III. Layer I, Layer II,
  reserved version/layer/sample-rate values, a bad bitrate index, or an invalid
  sync word produce a partial detail with stable diagnostics and recovery at
  the selected FLV payload end.
- Bitrate, sample rate, samples per frame, and frame length are derived only
  when their header fields are valid. Layer III frame length is
  `floor(144 * bitrate / sampleRate) + padding` for MPEG-1 and
  `floor(72 * bitrate / sampleRate) + padding` for MPEG-2/2.5.
- Bitrate index zero is valid free-format syntax. Phase one reports it
  explicitly, emits a warning, and omits bitrate and frame-length derivations;
  a complete detail means the fixed FLV and MPEG header syntax was parsed, not
  that a compressed frame boundary or body was validated.
- Generic audio detail dispatch reads only the first payload byte when it is
  safe to do so. It routes `SoundFormat=10` to AAC and `SoundFormat=2` to MP3.
  Empty, filtered, incomplete, unreadable, and other audio selections retain
  the pre-existing AAC error/partial-result behavior so adding MP3 does not
  rewrite older failure contracts.

## Consequences

- A common `muxray detail --tag` entry point can return MP3 without weakening
  the independently typed detail contracts.
- MP3 work and retained memory are constant with respect to the FLV audio
  payload size; payload bytes remain in the source.
- A valid header can expose useful codec facts even when the rest of the frame
  is truncated. Consumers must not interpret `complete: true` as proof that an
  entire MP3 frame is decodable.
- MP3 summary support, free-format boundary discovery, reserved FLV 8 kHz MP3,
  CRC verification, VBR headers, and multi-frame traversal remain explicit
  later increments.
