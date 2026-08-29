# AMF0 diagnostic registry v1

This registry defines the machine-stable diagnostics emitted by the bounded AMF0 parser and the FLV ScriptData adapter. They appear in `flv-tag-detail-v1` reports.

Consumers branch on `code` and `severity`, never on `message`. New codes may be added to version 1, but an existing code is not reused for a different condition. Offsets are absolute positions in the original FLV source.

## Severity, completion, and recovery

- `warning` preserves syntactic framing and normally leaves `detail.complete=true`.
- `error` means the promised detail is incomplete, a required ScriptData root is missing, or diagnostics were truncated.
- An unrecoverable AMF0 condition stops parsing the selected payload without guessing an internal resynchronization point. `bytesConsumed` remains the actual parse cursor.
- `recoveryOffset`, when present, is the trusted end of the FLV Tag payload where the outer container can resume. AMF0 detail failure never changes the already-built FLV scan index.
- `expected` and `actual` are optional non-negative, code-specific integers. Signed Date values and IEEE-754 payloads are represented in syntax fields rather than coerced into these diagnostic members.

## Markers and scalar values

| Code | Severity | Condition | Action |
|---|---|---|---|
| `AMF0_MARKER_TRUNCATED` | error | A required value marker is unavailable | Stop detail |
| `AMF0_MARKER_UNKNOWN` | error | Marker is not defined by AMF0 | Stop detail |
| `AMF0_TYPE_UNSUPPORTED` | error | MovieClip, RecordSet, or AVMPlus has no implemented bounded grammar | Stop detail |
| `AMF0_OBJECT_END_UNEXPECTED` | error | Object-end marker appears as a standalone value | Stop detail |
| `AMF0_NUMBER_TRUNCATED` | error | Fewer than eight Number payload bytes remain | Stop detail |
| `AMF0_BOOLEAN_TRUNCATED` | error | Boolean byte is unavailable | Stop detail |
| `AMF0_BOOLEAN_NONCANONICAL` | warning | Boolean raw byte is greater than one | Preserve raw byte; interpret nonzero as true |
| `AMF0_STRING_LENGTH_TRUNCATED` | error | String/Long String/XML length field is incomplete | Stop detail |
| `AMF0_STRING_DATA_TRUNCATED` | error | Declared string bytes cross the payload boundary | Stop detail |
| `AMF0_STRING_LIMIT_EXCEEDED` | warning | One string exceeds the configured retained-length limit | Skip bytes inside the payload and mark value omitted |
| `AMF0_STRING_BUDGET_EXCEEDED` | warning | Retaining a string would exceed the cumulative budget | Skip bytes inside the payload and mark value omitted |
| `AMF0_UTF8_INVALID` | warning | String contains invalid UTF-8 | Preserve raw bytes in the internal Go string and anchor the first invalid byte; public JSON uses U+FFFD replacement |
| `AMF0_REFERENCE_TRUNCATED` | error | Reference index is incomplete | Stop detail |
| `AMF0_REFERENCE_OUT_OF_RANGE` | warning | Reference does not identify a preceding referenceable node | Preserve index and mark reference invalid |
| `AMF0_DATE_MILLISECONDS_TRUNCATED` | error | Date IEEE-754 payload is incomplete | Stop detail |
| `AMF0_DATE_TIMEZONE_TRUNCATED` | error | Date timezone field is incomplete | Stop detail |
| `AMF0_DATE_TIMEZONE_NONZERO` | warning | Reserved timezone field is nonzero | Preserve signed minutes |
| `AMF0_TYPED_CLASS_LENGTH_TRUNCATED` | error | Typed Object class-name length is incomplete | Stop detail |
| `AMF0_TYPED_CLASS_DATA_TRUNCATED` | error | Typed Object class name crosses the payload boundary | Stop detail |

## Containers and resource limits

| Code | Severity | Condition | Action |
|---|---|---|---|
| `AMF0_OBJECT_KEY_LENGTH_TRUNCATED` | error | Object/ECMA/Typed Object property-name length is incomplete | Stop detail |
| `AMF0_OBJECT_KEY_DATA_TRUNCATED` | error | Property name crosses the payload boundary | Stop detail |
| `AMF0_OBJECT_END_MISSING` | error | Property list reaches payload end without `00 00 09` | Stop detail |
| `AMF0_ARRAY_COUNT_TRUNCATED` | error | ECMA or Strict Array count is incomplete | Stop detail |
| `AMF0_ECMA_ARRAY_COUNT_MISMATCH` | warning | Advisory ECMA count differs from properties parsed through object end | Preserve declared and actual counts |
| `AMF0_DEPTH_LIMIT_EXCEEDED` | error | Next value exceeds configured nesting depth | Stop detail |
| `AMF0_NODE_LIMIT_EXCEEDED` | error | Next value exceeds flat-arena node budget | Stop detail |
| `AMF0_CONTAINER_ENTRY_LIMIT_EXCEEDED` | error | A container exceeds its entry budget | Stop detail |
| `AMF0_DIAGNOSTIC_LIMIT_REACHED` | error | Another diagnostic would exceed the configured list size | Replace the final retained slot and omit later diagnostics |

Only an empty property name followed by marker `0x09` is an object terminator. An empty name followed by any other supported marker is parsed as a real property.

## FLV ScriptData semantics

| Code | Severity | Condition | Action |
|---|---|---|---|
| `AMF0_SCRIPT_NAME_MISSING` | error | No first top-level value exists | Detail incomplete; recover at payload end |
| `AMF0_SCRIPT_NAME_NOT_STRING` | warning | First top-level value is not a String | Preserve roots; event name unavailable |
| `AMF0_SCRIPT_VALUE_MISSING` | error | No second top-level value exists | Detail incomplete; recover at payload end |

Additional top-level values are retained in source order. The parser does not silently discard them or assume that every ScriptData event is `onMetaData`.

## Deterministic order

Diagnostics are emitted in discovery order. Parser diagnostics precede ScriptData semantic diagnostics. If the shared limit is exhausted, the last retained slot becomes `AMF0_DIAGNOSTIC_LIMIT_REACHED`. If a fatal condition is discovered after warnings filled the budget, the slot immediately before that marker is reserved for the fatal cause and both entries carry the trusted recovery position; earlier diagnostics otherwise remain in discovery order.
