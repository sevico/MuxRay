# ADR-012: Recover native opens through bounded request outcomes

Status: Accepted

Date: 2026-09-10

Supersedes: ADR-011's Native binding surface and open-response handling only.
Its module isolation, single-session UI and host lifecycle decisions remain.

## Context

The native picker can admit a file while its JavaScript response is lost. The
UI then lacks the session ID needed to close or inspect that file, while the
host's occupied slot rejects a second open. A Wails callback may also never
settle, so handling only rejected promises does not bound this failure.

## Decision

Expose `Native.OpenFLV(requestId)` and `Native.GetOpenResult(requestId)` beside
the unchanged six-method desktop adapter. Request IDs are canonical positive
uint64 decimal strings, increasing within the window controller's lifetime.
The native host retains only the latest request and its pending/completed
outcome. Repeating that ID returns its current result without opening another
dialog; older IDs are rejected. No full selected path is retained in this record.

Result confirmation never opens a dialog or file. If the request has not
arrived, confirmation seals it as canceled before returning, so a late original
invocation cannot admit an orphan session. It does not supersede a different
pending picker. Session snapshots are refreshed from the adapter, including
logical close and terminal cleanup, rather than replaying an obsolete snapshot.

The UI waits at most five seconds for each open or confirmation callback. An
open timeout starts sequential confirmation at the existing 200 ms interval;
it neither cancels nor repeats the picker. A failed/timed-out confirmation
stops polling and exposes manual retry of the same ID. Late replies cannot
replace a recovered or newer session. UI disposal cancels its local waits;
resource ownership remains with the host.

## Consequences

The generated Native binding and its embedded frontend change together. The
six-method adapter's v1 contract, CLI schemas and core scan lifecycle remain
unchanged. The record is bounded to one outcome and does not provide session
history, persistent request IDs or recovery across application restarts.

The local timeout bounds UI waiting; it does not interrupt native I/O. Result
confirmation can mutate the request record to fence an unseen command, although
it never opens a source. A canceled selection and a sealed undelivered open both
release the UI after confirmation without pretending that a scan was admitted.

## Validation

Regression tests cover lost and unsettled open/confirmation replies, pending
pickers, duplicate/stale requests, unseen-request fencing, current closed
snapshots, retry coalescing, late results and disposal. Generate actual Wails
bindings and validate the production window's timeout-confirmation path with
a real picker. The detailed results and controlled-I/O acceptance boundary are
recorded in [the Windows acceptance report](../desktop-native-20260910.md).
