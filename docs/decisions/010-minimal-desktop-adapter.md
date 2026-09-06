# ADR-010: A bounded, framework-independent desktop adapter

Status: Accepted

## Context

ADR-009 provides owned background scans, but deliberately leaves application
concurrency limits and the Go/JavaScript boundary to an adapter. Passing its
`ScanResult`, context, or index directly to a desktop binding would expose an
unbounded transport surface and numeric offsets that JavaScript cannot always
represent exactly.

## Decision

Add pure-Go `internal/desktop`. `New(ctx)` creates an application owner;
`App.Bindings()` returns the only object intended for a future desktop binding.
The application lifecycle method `App.Shutdown(ctx)` is not on that object.
No Wails/runtime dependency, React UI, native picker, executable window, or
framework version change is introduced. ADR-001's reviewed framework choice
remains unchanged; real binding generation and window integration follow later.

### One owned slot and explicit identity

The adapter owns at most one slot, including opening, scanning, finished,
canceling, and closing work. `OpenFile` validates a bounded request, reserves the
slot with an opaque string ID, starts one opening goroutine, and immediately
returns an `opening` snapshot. That goroutine calls `session.Open`; the session
then owns the scan worker and file. There is no goroutine per poll, page, cancel,
or close, and no queue, watcher, event history, or retired-session list.

A second open returns `busy` until the user explicitly closes the old slot and
both its opening operation and any scan/source-close operation have ended.
Opening a new file never silently replaces an existing result. IDs are monotonic
within an App, never reused or wrapped, and required for every subsequent call.
An old ID cannot cancel, close, or query a newer file. These are local identity
tokens, not authentication or security capabilities.

A closed terminal slot remains as a single status tombstone until the next
open; repeated close and status queries for it work. On reuse the old ID becomes
`session_not_found`. Calls which captured an old result before concurrent
closure can finish; their replies carry the old ID so the client can discard
stale replies. No full index is exposed or cached by the adapter.

### Binding surface and transport

The binding object has six methods, using concrete JSON-friendly Go structs:

- `OpenFile(OpenRequest)` -> `SessionResponse`
- `GetSession(sessionID)` -> `SessionResponse`
- `CancelScan(sessionID)` -> `SessionResponse`
- `CloseFile(sessionID)` -> `SessionResponse`
- `GetTags(PageRequest)` -> `TagPageResponse`
- `GetDiagnostics(PageRequest)` -> `DiagnosticPageResponse`

Responses carry `apiVersion=1`, a concrete result or a structured request
`error`, and no Go error/interface/context/iterator. Runtime failures belong to
the session snapshot's `failure`, not to the envelope's request error. Messages
are fixed and do not echo paths or raw OS errors. No request path, display path,
timestamp, host data, or randomly generated identity enters the existing CLI
reports. The adapter response is a separate contract, not a reuse or revision
of a CLI report schema.

All 64-bit integers in responses, including offsets, sizes, counts, tag IDs,
signed DTS, and optional diagnostic values, are base-10 strings. Unsigned
strings are canonical `0` or a nonzero digit followed by digits. Page starts
accept that same canonical uint64 grammar, at most 20 characters, with checked
overflow. Existing safely bounded uint8/uint32 values remain JSON numbers.
Session IDs are opaque strings; clients must not parse them as counters.

Page requests require count 1..4,096 and a canonical start; start==total gives
an independent empty array, start>total is an argument error. Tags preserve
global IDs and use the core record's Span methods. Diagnostics also use bounded
paging: add `session.DiagnosticPage` so the adapter need not clone all retained
diagnostics to retrieve one page. Numeric pointers in returned diagnostics are
deep copies. The hard scan/diagnostic budgets of ADR-008/009 do not change.

The input path is nonempty, valid UTF-8, contains no NUL, and is limited to
32,768 bytes. Relative paths retain OS semantics; a future picker should pass
absolute paths. Scan options use shared FLV bounds and zero selects defaults.
This is an in-process trusted local adapter, not an HTTP service or sandbox for
untrusted web content. The future desktop host must separately secure its
assets, navigation, and binding exposure.

### Cancellation, closure, and shutdown

`GetSession` is nonblocking and exposes `opening` in addition to ADR-009's
states. Progress is null until a session exists; the final scan completeness
object is null until a scan has actually ended. Thus an open failure is not
misrepresented as FLV corruption. Structural diagnostics can coexist with a
`finished` state and a nil task failure, as in ADR-009.

Cancel requests propagate through a per-slot context, including during open.
Cancel keeps partial results available after the scan ends. Close immediately
marks the slot closed, disables new page access, requests cancellation, and
invokes session cleanup with an already canceled wait context. It never waits
for blocked open/read/close I/O. Opening work that returns after close/shutdown
must close its resulting session before relinquishing ownership.

`App.Shutdown(ctx)` permanently disables new opens, marks the current slot
closed, cancels the application lifetime, waits for the opening operation, and
then waits for session cleanup using the caller's context. Timeout does not
drop ownership or allow another open. Repeated shutdown can wait again, and
status queries can still observe the closing slot. Returned task errors include
expected cancellation and real cleanup failures; the host must distinguish
them. The inherited inability to interrupt arbitrary blocked I/O remains.

## Compatibility and scope

Existing scanner behavior, CLI commands, JSON schemas/goldens, exit codes, and
one-million-tag cap remain unchanged. No payload details, media summaries,
native dialogs, drag/drop, live partial-index queries, subscriptions, multiple
tabs, registry persistence, disk index, or export endpoint is added.

## Validation

Use gated openers and sessions to test single-slot admission, cancellation
during open, late-open cleanup, close/shutdown timeouts, repeat shutdown, stale
IDs, concurrent page/close/open, and resource ownership without sleep-based
timing. Include real-file integration through `session.Open`, bounded diagnostic
copy tests, exact JSON round trips beyond 2^53 and at uint64/int64 extremes,
invalid decimal/page/path/options inputs, and malformed-source outcomes.
Run full tests, build, vet, race, repeated lifecycle races, fuzz, and existing
contract regression checks. Pure-Go CI must not acquire a GUI/native dependency.
