# ADR-019: Bounded desktop diagnostic snapshots and recoverable native saving

Status: Accepted

Date: 2026-09-29

Extends ADR-012's native request recovery and ADR-018's diagnostic navigation.
Adds bounded completed-result retention to ADR-013/015; their existing read
request, cancellation and recovery protocols remain unchanged.

## Context

The desktop can inspect trends, select observations, and inspect syntax and
original bytes. Closing the view loses that investigation. The desktop retains
bounded projections of analysis results, not the complete CLI Summary v2 model;
exporting those projections as a CLI document would misstate its contract.

## Decision

### Independent, bounded evidence document

Introduce `documentType: desktopDiagnosticSnapshot`, `schemaVersion: 1`, with
an independent JSON Schema. Include source basename, creation time, producer,
session scan coverage and the exact requested view selection. Do not export a
source absolute path or claim content-hash identity.

Capture only committed in-memory facts from one current terminal scan:

- The first at most 100 retained scan diagnostics, with page bounds and retained
  counts. These are neither all observed errors nor necessarily the full list.
- The selected indexed Tag, with original IDs, offsets and byte/bit spans.
- Matching completed detail and Hex results, when available. Hex contains only
  the already read window, at most 4096 actual bytes, with no padding.
- A completed Summary overview and at most the requested 100-event page of one
  retained event kind. Other kinds/pages are not implicitly enumerated.
- At most 512 source-ordered trend points, their bounded preceding context,
  display metric/type and gap threshold, plus corresponding local observations.

All 64-bit coordinates and counts remain decimal strings. Timestamp regression,
duplicates and large gaps retain ADR-018's local observation meaning. Unknown
context is not zero; a large gap means strictly greater than the selected
threshold. Filtering observations does not change the trend window's coverage.

Each unavailable or unrequested optional section has an explicit omission
reason. A partial scan, truncated retained diagnostics/events and bounded
context remain visible. The total encoded document, including final newline,
must be no larger than 2 MiB; reject an oversized document rather than silently
truncating fields. No source payload read, full index copy or new analysis is
started for export. Source validation describes the existing change latch;
export is not a fresh revalidation of every byte on disk.

The source owner retains one completed detail and one completed Hex for this
purpose, bounded by the existing detail and 4096-byte limits. A new same-kind
request invalidates that kind's retained snapshot; starting a new Summary,
closing the document or detecting source changes clears both. The existing
latest-read `GetDetail`/`GetHex` semantics are unchanged. A snapshot only includes
the retained result if its request ID and selected Tag identity match.

### Freeze before native destination selection

`App.CaptureSnapshot` is a Go-only operation, not a JavaScript binding. It
serializes the selected bounded facts while fencing session replacement and
closure, returning owned JSON bytes plus host-only source path/handle identity.
Requests select facts by IDs; they do not submit arbitrary report contents.

The native surface adds `SaveSnapshot({requestId,snapshot})` and
`GetSaveResult(requestId)`. It does not expose caller-chosen destination paths.
The native save dialog suggests `<source basename>.muxray.json`; only absolute
`.json` destinations are accepted. Export may contain AMF field text and raw
media bytes; the UI describes that scope before saving.

Closing a source view does not revoke a snapshot already frozen for saving.
Its result remains labelled with the original source, and the save state
survives view closure. Browsing can continue; another save or native open is
blocked while a save is unresolved. Shutdown stops admission and prevents a
commit that has not yet begun. A commit already completed remains successful.

### Recoverable, bounded native outcomes

Save request IDs are canonical positive uint64 decimal strings, monotonically
increasing over the native host lifetime. Keep only the latest small outcome:
pending, canceled, success (destination basename and exact bytes written), or
a path-free failure. Repeating an admitted ID returns its outcome and never
opens another dialog or writes again, even if the supplied selection differs.

Querying a previously unseen higher ID seals it as canceled. A delayed original
invocation therefore cannot later create an unexpected file. Older IDs are
stale; an in-progress dialog/write prevents another request from superseding
its recovery result. Opening and saving share the native operation gate.

The frontend waits at most five seconds per IPC reply, then queries the same
save ID. Pending outcomes use serial polling; communication failure offers
explicit retry of the query. An unconfirmed result is not called a save failure
or success. Retrying confirmation never repeats the write. Disposal and view
transitions cannot attach the old result to a new analysis.

### Destination integrity

Protect both the source pathname and its original retained file identity.
Reject the source itself, hard-link aliases and symbolic-link destinations.
Write to a randomly named exclusive temporary file in the destination directory,
check write, sync and close errors, then publish through rename/replace without
first deleting the destination. Any failure before publication leaves the
existing destination intact and cleans up the owned temporary file.

Before publication, recheck destination source identity and whether an existing
target's identity, size or modification time changed. Native overwrite prompts
govern ordinary replacement. Publication is the success point; a lost response
does not undo a completed save. Windows uses Go's `MoveFileEx` replacement path.
This is not a guarantee against arbitrary hostile directory races, same-size
same-time modifications, power loss, or filesystem operations that block.
Cooperative shutdown and IPC timeouts do not forcibly terminate filesystem I/O.

## Validation

Test snapshot coverage, large decimal coordinates, matching retained results,
omissions and size budgets; validate the schema against committed examples and
actual native exports. Test cancellation, failed capture, failed publication,
source/alias protection, destination changes, repeated requests, unseen-query
sealing, save-after-source-close, and shutdown before publication. Test the UI's
reply timeout and recovery without a second write, with state kept across close.
Record real Windows save-dialog evidence separately from synthetic host tests.

## Consequences

Users can preserve an investigation without re-reading the media. JSON is the
first export format; this is not a re-openable project, full-media archive,
complete CLI report, or full-file timeline. Existing CLI schemas are unchanged.
The host retains only one small save outcome after releasing the frozen bytes.
