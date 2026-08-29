# Diagnostic registry v1

This registry defines the machine-stable diagnostic codes emitted by the initial FLV header/tag scanner. It is paired with scan-report `schemaVersion: 1`.

Consumers use `code` and `severity` for logic. `message` is deterministic English text for people and logs; it is not a localization key. Codes are never reused for a different condition. New codes may be added to v1, but removing a code or changing an existing code's condition, severity, anchor, or value semantics requires an explicit compatibility decision and normally a new report version.

## Severity and anchors

- `warning`: the observed bytes are non-canonical, unsupported at a deeper layer, or outside the base FLV expectation, but container framing can continue.
- `error`: framing cannot be completed safely, or a configured resource bound prevented a complete scan. `muxray inspect` still writes the report and exits with its diagnostics status.
- `offset` and `bitOffset` form a point anchor in the same coordinate system as `Span`. Bit offset zero is the most-significant bit. Initial container diagnostics currently use `bitOffset: 0`.
- For a present field, the anchor is its first byte. For a missing/truncated required structure, the anchor is the first unavailable byte or the beginning of the incomplete structure, as listed below.
- Optional `expected` and `actual` are non-negative integers whose meaning is code-specific. Their absence is intentional; clients must not synthesize zero.

## Header and initial PreviousTagSize

| Code | Severity | Anchor | `expected` / `actual` | Scanner action | Canonical message |
|---|---|---|---|---|---|
| `FLV_HEADER_TRUNCATED` | error | byte 0 | required header bytes / source bytes | Stop; `header.present=false` | `FLV header is shorter than 9 bytes` |
| `FLV_SIGNATURE_INVALID` | error | byte 0 | absent / absent | Stop after decoding the fixed header | `file does not begin with the FLV signature` |
| `FLV_VERSION_UNSUPPORTED` | warning | byte 3 | `1` / observed version | Continue with base-v1 framing | `FLV version is not the currently supported version 1` |
| `FLV_TYPE_FLAGS_RESERVED` | warning | byte 4 | `0` / non-zero reserved-bit mask | Continue | `reserved FLV type flag bits are non-zero` |
| `FLV_DATA_OFFSET_INVALID` | error | byte 5 | minimum fixed-header size (`9`) / declared offset | Stop | `FLV data offset points inside the fixed header` |
| `FLV_DATA_OFFSET_OUT_OF_BOUNDS` | error | byte 5 | maximum available offset (source size) / declared offset | Stop | `FLV data offset is beyond the end of the source` |
| `FLV_PREVIOUS_TAG_SIZE0_TRUNCATED` | error | declared data offset | required bytes (`4`) / remaining bytes | Stop | `initial PreviousTagSize is truncated` |
| `FLV_PREVIOUS_TAG_SIZE0_NONZERO` | warning | declared data offset | `0` / observed value | Continue | `initial PreviousTagSize should be zero` |

`header.span` covers the fixed nine bytes only. A valid `dataOffset` greater than nine is represented by `header.extraDataSize`; extension bytes are skipped without producing a diagnostic in the initial scanner.

## Tag framing

| Code | Severity | Anchor | `expected` / `actual` | Scanner action | Canonical message |
|---|---|---|---|---|---|
| `FLV_TAG_HEADER_TRUNCATED` | error | start of incomplete header | required bytes (`11`) / remaining bytes | Stop; do not add a partial tag record | `FLV tag header is truncated` |
| `FLV_TAG_RESERVED_BITS_NONZERO` | warning | tag header byte 0 | `0` / reserved-bit mask (`rawTypeFlags & 0xc0`) | Continue and index tag | `reserved FLV tag header bits are non-zero` |
| `FLV_TAG_FILTER_UNSUPPORTED` | warning | tag header byte 0 | absent / absent | Continue and index; do not claim decoded filtered/encrypted payload | `filtered/encrypted FLV tag payload is not decoded` |
| `FLV_TAG_TYPE_UNKNOWN` | warning | tag header byte 0 | absent / low five-bit type code | Continue and index as `typeName: "unknown"` | `unknown FLV tag type is indexed but not decoded` |
| `FLV_STREAM_ID_NONZERO` | warning | tag header byte 8 | `0` / observed 24-bit stream ID | Continue | `FLV stream ID should be zero` |
| `FLV_TAG_BODY_TRUNCATED` | error | EOF after available payload | declared `dataSize` / available payload bytes | Add one partial tag record, then stop | `FLV tag payload is shorter than DataSize` |
| `FLV_PREVIOUS_TAG_SIZE_TRUNCATED` | error | first byte after payload | required bytes (`4`) / remaining bytes | Add tag record, consume remaining trailer bytes, then stop | `PreviousTagSize following the tag is truncated` |
| `FLV_PREVIOUS_TAG_SIZE_MISMATCH` | warning | first byte of the four-byte value | `11 + dataSize` / observed value | Continue using DataSize-based framing | `PreviousTagSize does not match the preceding tag` |

Attaching the four-byte `PreviousTagSize` to the preceding tag is a report-model convention. It is excluded from that tag's `span` and exposed as `tag.previousTagSize.span` when present.

## Resource limits

| Code | Severity | Anchor | `expected` / `actual` | Scanner action | Canonical message |
|---|---|---|---|---|---|
| `FLV_TAG_LIMIT_EXCEEDED` | error | start of the next unindexed tag | configured maximum / attempted tag count | Stop before decoding the next tag | `configured tag limit reached` |
| `FLV_DIAGNOSTIC_LIMIT_REACHED` | error | anchor of the first omitted diagnostic | absent / absent | For limits of at least two, replace the last retained diagnostic with this marker; later diagnostics remain omitted | `diagnostic limit reached; additional diagnostics were omitted` |

The diagnostic limit marker preserves the configured maximum array length. Its presence means the report is not a complete list of observed problems, even if structural scanning later reaches EOF. Public scan options require a diagnostic limit of at least two so the first retained cause and this explicit truncation marker can coexist.

## Deterministic order

Diagnostics appear in discovery order. Header diagnostics precede tag diagnostics. Within a tag, the initial scanner reports reserved bits, filter support, unknown type, and stream ID in that order, followed later by payload or PreviousTagSize diagnostics. The diagnostic-limit marker occupies the retained slot of the first diagnostic it replaces.
