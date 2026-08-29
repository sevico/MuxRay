# Public schemas

[`flv-scan-report-v1.schema.json`](./flv-scan-report-v1.schema.json) is the Draft 2020-12 contract for `muxray inspect` output with `schemaVersion: 1`.

## Deterministic encoding

For the same bytes, scanner options, and MuxRay build, repeated reports must be byte-identical within the same output mode:

- the default is one compact UTF-8 JSON object followed by one LF;
- `--pretty` uses two-space indentation and one trailing LF;
- object fields use the order shown in the schema/Go report DTO;
- tags are in source order and have contiguous, zero-based IDs;
- diagnostics are in scanner discovery order;
- HTML escaping is disabled, and numbers use base-10 JSON integer syntax;
- paths, base names, wall-clock timestamps, durations, host information, random IDs, and map-order-dependent values are absent.

`source.sizeBytes` is the only v1 source identity fact. A source hash may be added in a future compatible revision, but v1 does not compute or emit it.

JSON numbers used for offsets and sizes are unsigned 64-bit integers. Consumers must not silently round values through an IEEE-754-only representation. In particular, JavaScript clients should use a lossless JSON parser before accepting values above `2^53 - 1`.

## Field semantics

- A `span` is half-open in bit space: it starts at `(byteOffset, bitOffset)` and covers exactly `bitLength` bits. Bit offset zero is the most-significant bit of the first byte.
- `scan.bytesScanned` is the final sequential cursor, not a performance counter. `scan.complete` means the scanner reached source EOF after complete framing; warnings can coexist with `true`.
- `header.span` covers only the fixed nine-byte FLV header. `header.extraDataSize` records declared extension bytes between byte 9 and `dataOffset`.
- `header.previousTagSize0` is zero both when a zero value was read and when it was unavailable. `FLV_PREVIOUS_TAG_SIZE0_TRUNCATED` distinguishes the latter.
- A tag `span` covers its eleven-byte header plus the available payload, but not the following `PreviousTagSize` field. `payloadSpan`, `availableDataSize`, and `payloadComplete` make a truncated payload explicit.
- `previousTagSize.span` is present only when the complete four-byte value was read. `expected` remains available when the value is absent.
- Diagnostic `code` is the compatibility key. `message` is stable human-readable text, but clients must not branch on it.

The constraints between duplicated compact fields—such as `tag.offset == tag.span.byteOffset`, `availableDataSize <= dataSize`, and `scan.bytesScanned <= source.sizeBytes`—are semantic invariants tested by MuxRay even where JSON Schema cannot express them portably.
