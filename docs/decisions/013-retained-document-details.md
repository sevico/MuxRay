# ADR-013: Retain a document source for bounded desktop details

Status: Accepted

Date: 2026-09-10

Supersedes: ADR-010's desktop source owner, cancellation propagation and
six-method binding restriction; ADR-011's deferral of desktop details.
ADR-009's existing `session.Open` ownership contract and ADR-012's native open
recovery protocol remain unchanged.

## Context

A scan indexes framing without retaining media payloads. Its existing session
closes the source on completion. Reading selected AMF0/AAC syntax afterwards
needs a longer-lived source, independent cancellation, bounded work and a
separate transport model. Reopening a path for every selection could combine
an old index with a replacement file. A frontend callback timeout cannot prove
that a source read has returned.

## Decision

### Document ownership and partial indexes

Add a pure-Go document owner. It opens one regular local file read-only and
retains that same handle until logical Close and completion of all admitted
reads. A new borrowed-source scan entry point in `session` does not close its
reader; its caller owns the reader until scan Wait returns. Existing
`session.Open` still owns and closes its file before publishing a terminal
scan result.

The document, scan and each active detail have separate cancellation contexts.
After admission, CancelScan stops scanning and retains the document for later
inspection. During opening, it cancels the opening slot's context instead. A
document created by that opener but published after cancellation is logically
closed and cleaned up; it is not retained for details. Its closed status is
propagated to the adapter, and the slot remains occupied until cleanup ends.
Scanning must reach a terminal state before paging or selecting details;
there are no live index reads. A complete payload
in the committed prefix is eligible even if other tags have structural errors
or the scan was canceled. A truncated payload or filtered tag returns an
explicit detail failure. Parser damage in one tag does not invalidate others.
The CLI's existing stricter whole-index admission policy is unchanged.

Close immediately prevents new pages/details, clears retained results and
cancels scan/detail work. Cleanup waits for all reads, closes the source exactly
once and retains close errors. The adapter distinguishes scan completion from
document cleanup: a closed slot cannot be reused until cleanup has returned.
Shutdown retains the existing three-second host wait and process-exit fallback.
An arbitrary blocked ReaderAt or Close cannot be forcibly interrupted; no
per-read goroutine or early resource release pretends otherwise.

Before and after detail parsing, compare the retained handle's identity, size
and modification time with its initial stat. An observed difference latches
`source_changed` for the document and discards the detail; the user must close
and reopen. A path replacement does not redirect the retained handle. This is
best-effort change detection, not an immutable snapshot: same-size changes with
restored timestamps and changes between checks may be undetectable. We do not
hash or copy entire media files on open.

### Bounded requests and recovery

Add `StartDetail({sessionId, requestId, tagId})` and
`GetDetail({sessionId, requestId})` to the desktop binding. Responses carry
`apiVersion=1`, `sessionId`, `requestId`, `tagId`, `state` (`pending`, `ready`,
or `failed`), a nullable typed detail and a nullable bounded fault. If GetDetail
fails before resolving a current admitted request, its tag ID is unknown and
`tagId` is the empty string. An admitted request's result preserves its exact
tag ID. This is an additive desktop API, with generated host/frontend bindings
shipped together; it does not revise any CLI schema. Session IDs are opaque
strings such as `scan-1`. Request IDs, tag IDs and all 64-bit coordinates use
canonical decimal strings.

Request IDs are positive uint64s increasing within a document. Repeating the
same ID and tag is idempotent; conflicting reuse and older IDs cannot replace a
new selection. Queries do not start reads. Keep at most one active detail
worker, one latest desired selection and one bounded result. A newer selection
cancels the active context and overwrites the desired selection; it starts
only after the old worker returns. There is no accumulated request queue.
Each active parse has a three-second cooperative deadline. A blocked read
continues to occupy its worker after that deadline.

The frontend independently limits each callback wait to five seconds and polls
pending results serially at 200 ms intervals after the previous reply. After
25 GetDetail replies still report pending, automatic polling pauses for manual
confirmation of the same identity; the initial Start reply is not counted.
This is a query-count limit, not a five-second overall completion guarantee.
A lost Start reply is reconciled with GetDetail. Failed confirmation also
stops automatic polling and exposes manual retry of the same identity.

New Tag selections fence old replies, cancel the scheduled poll timer and
replace the latest intent. An already outstanding callback continues to occupy
the single local wait until it replies or reaches its five-second deadline.
Page/tab changes, close, a different file and disposal clear local detail waits
immediately. None of these transitions claims that a host read ended merely
because JavaScript stopped waiting.

Once a host response confirms a terminal, retryable parse failure, an explicit
user retry may create a new increasing request ID to parse again. It must not
mistake a local polling timeout for a confirmed host failure. Unsupported,
filtered, truncated and changed-source outcomes explain the required action
instead of repeatedly reading the same ineligible payload.

### Separate parsing, transport and rendering limits

Add `internal/detailview` for AMF0 script data and AAC audio only. Its DTO is
independent of the CLI report: `kind`, `complete`, `truncated`, a flat parent-ID
field tree, and diagnostics. Fields have an ID, parent ID, name, type, readable
value and optional original byte/bit span. The view preserves codec header and
ASC bit fields and explicitly identifies AAC raw data as opaque; it does not
decode audio. Unsupported codecs return an explicit fault.

AMF0 parser budgets are 32 nesting levels, 4,096 nodes, 4,096 entries per
container, 64 KiB per retained string and 64 KiB of retained strings in total.
Both AMF0 and AAC retain at most 64 parser diagnostics. Existing core payload
boundaries and hard limits remain in force. Independently cap the view at 512
fields, 256 UTF-8 bytes per string preview, 64 diagnostics and 256 KiB of actual
JSON for the detail object. Account for JSON escaping, object overhead and
numeric representations. The response envelope adds only bounded identities
and fixed faults. Truncation is explicit, including string previews and budget
exhaustion. `complete` describes syntax success; `truncated` may also be true
for valid syntax with a shortened presentation. NaN/infinities and large
integers are textual values, never unsafe JavaScript numeric conversions.
The UI renders at most this bounded field list and only retains the current
selection; no full source payload or core parser graph crosses the binding.

## Consequences and validation

An open finished desktop document now keeps a file handle until Close. Source
close errors belong to document cleanup, independently of scan success.
Existing single-file admission, exact paging, native open recovery, immutable
indexes, CLI schemas and core parsers remain compatible.

Validate source lifetime, cancellation, partial tags, change detection,
idempotence, out-of-order requests, blocked read/close, stale replies, deadlines,
bounded projection and offsets above 2^53 with deterministic tests and race
detection. Regenerate real Wails bindings and exercise normal/damaged AMF0,
AAC configuration/raw packets, truncated tags, selection changes and file
closure in a production Windows window. Controlled overlays test blocked I/O
separately from production interaction; preserve that distinction in evidence.

AVC context resolution, MP3 desktop details, Hex, summary, export, multi-file
history and strong source snapshots are outside this decision.
