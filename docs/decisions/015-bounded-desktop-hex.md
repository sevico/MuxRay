# ADR-015: Bounded desktop source windows and syntax navigation

Status: Accepted

Date: 2026-09-11

Supersedes: ADR-013's deferral of Hex and its detail-only request namespace.
Its source ownership, cooperative deadlines, recovery and close protocol remain
in force. Existing CLI contracts and parser admission rules remain unchanged.

## Context

AMF0, AAC, AVC and MP3 desktop details expose exact source spans, but the user
cannot inspect those bytes in the application. Unsupported and truncated Tags
also need a raw inspection path that does not depend on a successful parse.
AVC provenance may refer to a preceding configuration Tag rather than the
selected payload. A field can describe megabytes while the source is gigabytes.

## Decision

### Source ownership and scheduling

Add a Hex operation to the retained document's existing single worker. Detail
and Hex share one active operation, one replaceable pending intent and one
monotonically increasing uint64 request-ID namespace per document. A new valid
request cancels/supersedes the previous operation, including across kinds. An
older ID is stale; a repeated current ID must match the kind and normalized
parameters. Invalid requests do not advance the ledger. Each kind retains at
most one bounded result, and close clears both. No second goroutine bypasses a
blocked ReaderAt. The three-second deadline remains cooperative.

Hex is available after an actual scan reaches a terminal state, including
incomplete, failed and canceled scans with a retained source. Its addressable
range is the original file size, independent of the retained index prefix or
selected Tag. Reading bytes beyond a committed prefix says nothing about their
validity as FLV structure. It never reopens a path. The existing before/after
identity, size, mode and mtime checks apply, and observable source change remains
latched until close/reopen. This is not a strong content snapshot.

### Bounded transport

Expose StartHex(sessionId, requestId, offset, count) and
GetHex(sessionId, requestId) using apiVersion 1. All IDs and uint64 coordinates
are canonical decimal strings; count is uint32, zero selects 512 bytes, and the
hard maximum is 4,096 bytes. Compare normalized counts for idempotent retries.
GetHex is an in-memory query and never starts I/O. Replies carry pending, ready
or failed state and a nullable window or bounded fault. A ready window contains
offset, original sourceSize, lower-case hexadecimal bytes and eof. Hex strings
contain at most 8,192 characters; no paths, raw OS errors or whole-file buffers
are returned.

Validate offset <= original source size and signed ReaderAt bounds before
conversion or allocation. Read at most the smaller of count and remaining
bytes. Offset exactly at EOF returns an empty ready window. Do not zero-pad a
short read or publish a partial buffer after an I/O error or detected source
change. A field's declared range and the actual available bytes remain distinct.

The frontend shares its request counter across detail and Hex. It keeps finite
five-second callback waits, serialized 200 ms result polls and a maximum of 25
pending query replies before pausing for explicit confirmation. Lost Start
replies are reconciled by Get with the same ID. A confirmation or repeat of an
unknown Start preserves that identity; retrying a completed retryable failure
uses a new ID. Local wait expiry is not host cancellation. New selection,
page/tab changes, close and disposal fence old replies and clear obsolete Hex.

### Navigation and presentation

Show a read-only window with 16 bytes per row, hex and printable ASCII, previous
and next windows, and exact offset entry. A field with a source Span can locate
and highlight its actual bit range (bit zero is the most significant bit).
Source range semantics are those of cursor.Span and ByteRange, including empty
spans, cross-byte ranges and overflow checks. Frontend arithmetic uses BigInt
and equivalent checked semantics; file coordinates never pass through Number.
Only the intersection with the current window is rendered, and large spans do
not determine the requested allocation size.

Derived fields with no Span have no source-location action. A diagnostic's
offset/bitOffset denotes a point, not an invented length. Zero-length and EOF
locations remain visible as positions even when there is nothing to highlight.
Provide raw Tag/header/payload access independently of detail success, and
permit AVC provenance links to preceding Tags. A decimal Tag ID jump fetches
only the containing bounded page and selects the target; it does not fetch or
copy the whole index.

### Validation and scope

Test byte/bit boundaries, maximal coordinates, EOF and truncated sources,
request idempotency and cross-kind fencing, lost replies, pending saturation,
source-change latching and blocked-read close ownership. Native Windows
acceptance exercises four codec families, raw unsupported/truncated data,
cross-page Tag and byte navigation, a real large source, and controlled Hex I/O
closure. Synthetic >2^53 coordinates verify transport/arithmetic; they do not
claim a physical file of that size was opened.

Hex editing, whole-file search, export, media summary and streaming are outside
this batch. Summary remains a separate future task because whole-index analysis
needs its own budgets, progress semantics, exact DTOs and event pagination.
