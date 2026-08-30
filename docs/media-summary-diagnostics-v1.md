# Media summary diagnostics v1

`muxray summary` keeps FLV scanner diagnostics in `scanDiagnostics` and emits
cross-tag/framing diagnostics in `analysisDiagnostics`. A diagnostic `code` is
the compatibility key; clients must not branch on `message`.

## Summary-owned codes

| Code | Severity | Meaning |
|---|---|---|
| `MEDIA_AUDIO_HEADER_TRUNCATED` | error | An Audio Tag has no AudioTagHeader byte. |
| `MEDIA_AAC_PACKET_TYPE_TRUNCATED` | error | An AAC Tag has no AACPacketType byte. |
| `MEDIA_AAC_PACKET_TYPE_UNKNOWN` | error | AACPacketType is neither sequence header nor raw. |
| `MEDIA_VIDEO_HEADER_TRUNCATED` | error | A Video Tag has no VideoTagHeader byte. |
| `MEDIA_AVC_FRAMING_TRUNCATED` | error | An AVC Tag lacks the complete packet type and signed composition time. |
| `MEDIA_AVC_PACKET_TYPE_UNKNOWN` | error | AVCPacketType is neither sequence header, NALU, nor end of sequence. |
| `MEDIA_AUDIO_CODEC_UNSUPPORTED` | warning | The Audio Tag is outside the AAC-only P2a summary scope. |
| `MEDIA_VIDEO_CODEC_UNSUPPORTED` | warning | The Video Tag is outside the AVC-only P2a summary scope. |
| `MEDIA_FILTERED_TAG_SKIPPED` | warning | A filtered/encrypted media Tag cannot be classified. |
| `MEDIA_INCOMPLETE_TAG_SKIPPED` | error | The indexed Tag payload is incomplete. |
| `MEDIA_CONFIGURATION_BYTE_LIMIT_REACHED` | error | The cumulative ASC/avcC parse-and-hash byte budget was exhausted. |
| `MEDIA_DIAGNOSTIC_LIMIT_REACHED` | error | Additional analysis diagnostics were omitted; a fatal diagnostic and recovery point are preserved when available. |

AAC/AVC parser diagnostics retain their existing `AAC_*` or `AVC_*` code and
gain the owning FLV `tagId`. For a summary-owned Tag error, `recoveryOffset` is
the exclusive end of the available Tag payload. For a configuration-byte
budget error, the offset is the skipped ASC/avcC data start and recovery is the
exclusive end of that complete Tag payload.

Warnings can still make `summary.complete` false when they identify a Tag that
could not be classified under the P2a codec scope. Entry-retention truncation
is represented by the corresponding `truncated` field rather than a diagnostic
because aggregate counts remain exact.
