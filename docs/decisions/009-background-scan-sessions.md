# ADR-009: Bounded progress and background scan sessions

Status: Accepted

## Context

ADR-008 made container scans cancellable and their final indexes pageable. A
desktop adapter still needs a nonblocking scan, bounded progress queries, and
an explicit owner for the input file and retained index. This ADR supersedes
only the deferral of progress and background sessions in ADR-008. Its rule
against querying an index while the scanner is constructing it still applies.

## Decision

### Progress without live index publication

Add `flv.ScanContextWithProgress` alongside unchanged `Scan` and `ScanContext`
entry points. Its optional synchronous callback receives value-only
`ScanProgress` counters: source size, logical bytes scanned, committed tag count,
and retained diagnostic count. No index, slice, source, path, timestamp, or
diagnostic pointer is published by the callback.

Publish an initial snapshot, one after the initial container structure is read,
one per 1,024 committed tags, and an exact final snapshot before returning,
including validation errors, cancellation, I/O failure, and structural damage.
Small scans can finish between polls. Intermediate progress is block-granular
and may lag construction; there is no wall-clock freshness guarantee while a
read is blocked. `BytesScanned` remains the logical cursor, not physical I/O,
work completed, or a prediction of remaining time. A damaged file can stop
before its source size, and skipped payloads can cause large progress jumps.

Callbacks execute on the scanning goroutine, must return promptly, and must not
panic. They are not asynchronous subscriptions. The scanner creates no timer,
queue, per-read goroutine, or per-tag clock measurement. Existing entry points
use a nil observer and do not acquire a progress lock.

### Application-layer session API

Add `internal/session`, separate from the pure container parser. `Open(ctx,
path, options)` opens and stats one regular local file synchronously, then
starts one scan worker and returns its session. Opening/stat errors are
synchronous; scan-option validation and all parser/I/O outcomes are returned
through `Wait`. The parent context controls the scan lifetime; nil means a
background context. There is no global registry, Wails binding, session ID,
transport, or new CLI command in this phase.

The session API is:

- `Snapshot()`: an independent, constant-size progress/state value; safe while
  the worker runs and after the session is closed.
- `Cancel()`: idempotent, nonblocking cooperative cancellation request.
- `Wait(ctx)`: wait for scanning **and source close** to finish; the wait
  context limits only this wait, not the scan. Nil waits without a deadline.
  Once finished, the recorded task error takes precedence over a canceled wait
  context. Multiple callers may wait independently.
- `Page(start, count)`: reuse ADR-008's bounded page API only after the worker
  finishes, including canceled or failed scans with a committed prefix.
- `Result()`: retrieve the final `flv.ScanResult` after the worker finishes.
  Its diagnostics, including numeric pointer values, are deep copies; its
  chunked index is immutable and shared. No flat tag copy is constructed.
- `Close(ctx)`: mark the session logically closed, request cancellation, drop
  its retained result as soon as scanning ends, and wait using the supplied
  context. It returns the same task/wait error as `Wait`, including an expected
  `context.Canceled` when its request cancels the scan. It is idempotent.

`Page` and `Result` return `ErrNotReady` while the worker is active and
`ErrClosed` once `Close` is requested. Their error describes access, not the
scan outcome: a final partial result remains accessible with a nil access
error until closure. Consult `Wait` and `ScanResult` for the actual outcome.
An operation that captured a finished result before concurrent closure may
finish successfully; already returned pages/results remain valid.

### State and outcome

States are `running`, `canceling`, `finished`, `canceled`, and `failed`.
`canceling` means a cancellation was requested, including by the parent
context; it does not guarantee the request beats scan completion. Terminal
state is derived from the scanner's returned error and the source close
error, not a late cancellation after the scanner returned:

| Outcome | Terminal state | `Wait` error |
|---|---|---|
| Scanner and source close return nil | `finished` | nil |
| Scanner returns cancellation/deadline, source close succeeds | `canceled` | context error |
| Other scan error, or any source close error | `failed` | underlying error(s) |

Structural errors are diagnostics, not Go errors: `finished` does **not** mean
the FLV is valid. The final snapshot's `Complete` and `HasErrors` copy the
scanner's values; callers must check them or inspect the final diagnostics.
These two fields describe the scan and are meaningful only in terminal states.
The retained diagnostic count is capped, not a count of all discovered issues.

Logical closure is an orthogonal `Closed` flag, not another scan outcome. It
means no new result/page access, not that a blocked read has finished. A
terminal state or a completed `Wait` confirms that source closure was attempted.
Closing failures remain visible in `Wait`; cancellation cannot hide them.

### Ownership and limits

The worker exclusively owns the opened file and attempts to close it exactly
once, on every normal scan return, before publishing a terminal result or
waking waiters. No caller closes the source concurrently with a read. The file
is not retained for future detail or summary requests; adding such operations
needs a separate source-lifetime decision.

`Close` releases the session's references to its result. Previously returned
immutable indexes or diagnostic/page copies can outlive it; memory remains
owned by those callers until they release it. If a wait times out while an
underlying read/close is blocked, the worker still owns cleanup and releases
resources when that call returns. No forced interruption or bounded shutdown
latency is promised for arbitrary I/O.

Each session has one worker, one mutex-protected scalar snapshot, one done
notification, and the existing bounded scan/index/diagnostics. Polling creates
no retained history or event backlog. Limits remain 1,000,000 tags, 1,024 records
per index block, 1..4,096 records per page, and existing diagnostic bounds.
There is no aggregate process limit: the application must bound concurrent
sessions and close sessions it no longer needs.

## Compatibility and scope

Existing CLI JSON schemas, diagnostics, goldens, exit codes, and synchronous
CLI behavior do not change. Session state/progress is a Go API, not a new
versioned wire contract. GUI integration, live partial-index queries, payload
analysis sessions, subscriptions, disk indexes, and lifting the tag cap remain
out of scope. The historical cancellation outlier remains an open observation;
this API is not evidence of a fix or a large-file acceptance claim.

## Validation

Compare observed scans with ordinary chunked/legacy scans across normal,
malformed, canceled, invalid-option, and I/O-failing inputs. Verify monotonic
bounded counters, exact final progress, update limits, and source-structure-only
reads. Use deterministic gated readers (not timing sleeps) to test active
queries, cancel/wait deadlines, parent cancellation, concurrent close/query,
partial pages, result ownership, and exactly-once close before terminal
publication. Exercise real-file opening/cleanup and close-error propagation.
Run full tests, vet, race, contract validation, fuzz smoke, and scan/progress
benchmarks; preserve existing schemas and goldens.
