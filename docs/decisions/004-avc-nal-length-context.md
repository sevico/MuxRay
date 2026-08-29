# ADR-004: Resolve AVC NAL length size explicitly

- Status: Accepted
- Date: 2026-08-29

## Context

FLV AVC sequence headers carry an AVCDecoderConfigurationRecord. Its
`lengthSizeMinusOne` field determines the byte width of every NAL-unit length
prefix in subsequent AVC NALU packets. An independently selected raw NALU Tag
does not repeat that value, so parsing it without stream context requires a
guess. Four-byte lengths are common, but treating them as a universal default
can invent invalid boundaries and misleading diagnostics.

AVC also adds a third public detail shape after AMF0 and AAC. Reusing either
existing closed v1 Schema would break the typed-contract rule in ADR-003.

## Decision

- AVC uses an independent strong v1 contract with
  `documentType: "flv-avc-tag-detail"` and
  `flv-avc-tag-detail-v1.schema.json`.
- Sequence-header Tags parse their own AVCDecoderConfigurationRecord and expose
  its declared NAL length size.
- For an AVC NALU Tag, `muxray detail` first uses an explicitly supplied
  `--avc-nal-length-size` value of 1, 2, or 4 bytes. Zero means automatic
  context lookup, not a four-byte default.
- Automatic lookup searches backward through the completed FLV index for the
  nearest usable AVC sequence header and takes its declared length size.
- If no usable sequence header is available, MuxRay preserves the selected
  payload span, emits `AVC_NAL_LENGTH_SIZE_UNAVAILABLE`, marks the detail
  incomplete, and exits with diagnostic status. It does not guess NAL
  boundaries.
- NAL-unit payload bytes remain source spans. Phase one validates and reports
  length-prefixed boundaries but does not decode slice data or retain complete
  access units in the report.

## Consequences

- A standalone raw AVC Tag can still be inspected deterministically when the
  caller supplies the length size.
- Normal CLI use can recover stream context from an earlier sequence header
  without adding eager codec state to every scan record.
- Seeking directly into a raw packet without configuration may return a useful
  partial document rather than fabricated NAL units.
- Consumers select the AVC validator using both `documentType` and
  `schemaVersion`; existing AMF0 and AAC compact and pretty encodings remain
  unchanged.
