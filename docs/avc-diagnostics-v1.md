# AVC diagnostics v1

These codes are the compatibility keys emitted by
`documentType: "flv-avc-tag-detail"`. Consumers must branch on `code`, not on
the human-readable `message`.

## FLV AVC framing

| Code | Meaning |
|---|---|
| `AVC_FLV_VIDEO_HEADER_TRUNCATED` | The FLV VideoTagHeader byte is missing. |
| `AVC_FLV_FRAME_TYPE_RESERVED` | The four-bit FrameType value is reserved. |
| `AVC_FLV_FRAME_TYPE_UNEXPECTED` | The FrameType is defined for another FLV video codec and is unexpected with AVC. |
| `AVC_FLV_PACKET_TYPE_TRUNCATED` | The AVCPacketType byte is missing. |
| `AVC_FLV_COMPOSITION_TIME_TRUNCATED` | The signed 24-bit CompositionTime field is incomplete. |
| `AVC_FLV_COMPOSITION_TIME_NON_ZERO` | A sequence-header or end-of-sequence packet has a non-zero CompositionTime. |
| `AVC_FLV_PACKET_TYPE_UNKNOWN` | AVCPacketType is not sequence header, NALU, or end of sequence. |
| `AVC_NAL_LENGTH_SIZE_UNAVAILABLE` | A NALU packet has no explicit or preceding sequence-header length-size context. |
| `AVC_FLV_END_OF_SEQUENCE_PAYLOAD_UNEXPECTED` | An end-of-sequence packet contains trailing payload bytes. |

## AVCDecoderConfigurationRecord and parameter sets

| Code | Meaning |
|---|---|
| `AVC_CONFIGURATION_RECORD_HEADER_TRUNCATED` | The fixed AVCDecoderConfigurationRecord header is truncated. |
| `AVC_CONFIGURATION_VERSION_UNEXPECTED` | `configurationVersion` is not the expected value 1. |
| `AVC_RESERVED_BITS_UNEXPECTED` | An avcC reserved-bit field does not contain all one bits. |
| `AVC_LENGTH_SIZE_RESERVED` | `lengthSizeMinusOne` uses the reserved value 2. |
| `AVC_CONFIGURATION_BIT_DEPTH_INVALID` | A high-profile avcC bit-depth declaration exceeds the supported H.264 range. |
| `AVC_PARAMETER_SET_COUNT_TRUNCATED` | A required SPS, PPS, or SPS-extension count is missing. |
| `AVC_PARAMETER_SET_LENGTH_TRUNCATED` | A parameter-set length field is truncated. |
| `AVC_PARAMETER_SET_DATA_TRUNCATED` | A declared parameter-set payload exceeds the bounded avcC payload. |
| `AVC_PARAMETER_SET_LIMIT_REACHED` | The configured parameter-set count budget was exhausted. |
| `AVC_PARAMETER_SET_TOO_LARGE` | A parameter set exceeds the configured byte budget. |
| `AVC_PARAMETER_SET_NAL_TYPE_UNEXPECTED` | An avcC array entry has a NAL-unit type different from that array's nominal SPS/PPS type. Permitted declarative SEI is accepted without a warning; reserved or otherwise mismatched entries are retained with this warning. |
| `AVC_HIGH_PROFILE_EXTENSION_TRUNCATED` | Profile-conditional avcC extension syntax is truncated. |

## Length-prefixed NAL units

| Code | Meaning |
|---|---|
| `AVC_NAL_LENGTH_TRUNCATED` | A NAL-unit length prefix is truncated. |
| `AVC_NAL_PAYLOAD_TRUNCATED` | A declared NAL-unit payload exceeds its bounded packet. |
| `AVC_NAL_UNIT_LIMIT_REACHED` | The configured NAL-unit count budget was exhausted. |
| `AVC_NAL_UNIT_ZERO_LENGTH` | A length prefix declares a zero-byte NAL unit. |
| `AVC_NAL_HEADER_TRUNCATED` | The one-byte H.264 NAL header is missing. |
| `AVC_FORBIDDEN_ZERO_BIT_SET` | `forbidden_zero_bit` is one. |

## RBSP, SPS, and PPS summaries

| Code | Meaning |
|---|---|
| `AVC_EMULATION_PREVENTION_INVALID` | EBSP contains an invalid emulation-prevention byte sequence. |
| `AVC_RBSP_MAPPING_LIMIT_REACHED` | The bounded RBSP-to-source mapping budget was exhausted. |
| `AVC_RBSP_SYNTAX_TRUNCATED` | Required phase-one SPS or PPS RBSP syntax is truncated. |
| `AVC_EXP_GOLOMB_OVERFLOW` | An Exp-Golomb code exceeds the supported integer range. |
| `AVC_SPS_ID_OUT_OF_RANGE` | `seq_parameter_set_id` exceeds the H.264 range. |
| `AVC_PPS_ID_OUT_OF_RANGE` | `pic_parameter_set_id` exceeds the H.264 range. |
| `AVC_PPS_REFERENCED_SPS_ID_OUT_OF_RANGE` | The PPS references an out-of-range SPS identifier. |
| `AVC_SPS_RESERVED_BITS_UNEXPECTED` | The SPS reserved-zero bits are non-zero. |
| `AVC_SPS_CHROMA_FORMAT_INVALID` | `chroma_format_idc` is outside its defined range. |
| `AVC_SPS_BIT_DEPTH_INVALID` | An SPS luma or chroma bit-depth value exceeds the supported H.264 range. |
| `AVC_SPS_LOG2_MAX_FRAME_NUM_INVALID` | `log2_max_frame_num_minus4` exceeds its defined range. |
| `AVC_SPS_LOG2_MAX_PIC_ORDER_CNT_INVALID` | `log2_max_pic_order_cnt_lsb_minus4` exceeds its defined range. |
| `AVC_SPS_PIC_ORDER_CNT_TYPE_INVALID` | `pic_order_cnt_type` is outside its defined range. |
| `AVC_SPS_REFERENCE_CYCLE_TOO_LARGE` | The declared picture-order reference cycle exceeds the phase-one bounded budget. |
| `AVC_SPS_CROP_INVALID` | Cropping offsets exceed the coded frame dimensions. |
| `AVC_DIAGNOSTIC_LIMIT_REACHED` | Additional diagnostics were omitted after the configured limit. |

## SEI message inspection (opt-in detail v5 / desktop)

| Code | Meaning |
|---|---|
| `AVC_SEI_BYTE_LIMIT_REACHED` | SEI source bytes reached the per-NAL 64 KiB or cumulative 256 KiB limit. |
| `AVC_SEI_MESSAGE_LIMIT_REACHED` | SEI messages reached the per-NAL 64 or cumulative 256 limit. |
| `AVC_SEI_HEADER_TRUNCATED` | A payload type/size continuation lacks its terminating byte. |
| `AVC_SEI_PAYLOAD_TRUNCATED` | Declared payload size exceeds inspected RBSP bytes. |
| `AVC_SEI_SYNTAX_INVALID` | Supported SEI syntax, payload alignment, range or NAL reference constraint is invalid. |
| `AVC_SEI_TRAILING_BITS_INVALID` | Required SEI RBSP trailing bits are missing or invalid. |

Unknown payload types retain their source range and are not errors merely because
their syntax is unsupported. Truncation and resource budgets preserve observed
fields; neither recovery-point declarations nor a complete message guarantees
decoder recovery. Shared RBSP mapping/EPB diagnostics also apply.

## Severity and recovery

- `warning` records a non-canonical or unexpected value whose bounded syntax
  can still be classified. Warnings may coexist with `detail.complete: true`.
- `error` makes `detail.complete` false. The report retains all independently
  complete framing and syntax fields parsed before the failure.
- Every error in an FLV AVC detail document has `recoveryOffset` equal to the
  exclusive end of the selected Tag payload. Warnings do not have a recovery
  offset.
- Diagnostics are emitted in deterministic discovery order: FLV framing first,
  followed by avcC or NAL-unit/RBSP diagnostics.

## Syntax references

- [Adobe Flash Video File Format Specification 10.1](https://veovera.org/docs/legacy/video-file-format-v10-1-spec.pdf) defines VideoTagHeader, AVCPacketType, and signed 24-bit CompositionTime framing.
- [ISO/IEC 14496-15:2024](https://www.iso.org/standard/89118.html) defines AVCDecoderConfigurationRecord and its NAL length-size field.
- [ITU-T H.264](https://www.itu.int/rec/t-rec-h.264) defines NAL, RBSP, SPS, and PPS syntax.
