# ADR-008: Cancellable scans and a chunked, pageable tag index

Status: Accepted

## Context

ADR-001 bounded the flat tag index at one million records. Payloads are already
read on demand, but eagerly sizing a contiguous slice from source size penalizes
large files with few tags. Consumers also depend directly on that slice, and a
scan has no cancellation boundary. This blocks the next large-file and UI phase.

## Decision

Introduce `flv.ScanContext(context.Context, io.ReaderAt, uint64, Options)` and a
read-only `TagIndex` with `Len`, `Get`, `Page`, and `All`. The scanner builds blocks
of 1,024 records on demand, caps the final block at the remaining tag budget, and
does not flatten them. Records contain no payloads. The default and hard limits
remain 1,000,000 tags; this phase does not promise unbounded or disk-backed scans.

Keep `Scan` and its flat `ScanResult.Tags` for source compatibility. `Index()`
adapts legacy/manual results and exposes the chunked storage of new scans. All
production consumers use `Index()`. `Get`, iteration, and pages return record
copies. Page limits are 1 through 4,096; start equals total yields an empty page,
start beyond total is invalid. Page IDs and source spans remain absolute.
Returned indexes are immutable and support concurrent readers. A legacy slice
adapter requires its caller not to mutate the slice during access.

Cancellation is cooperative, checked before/after structural reads, between
tags, and before finishing. It returns the context error and a partial result
with `Complete=false`, the committed tag prefix, existing diagnostics, and the
logical cursor offset. An interrupted tag is not committed. Genuine truncated
source records retain existing behavior. Cancellation itself is not a corruption
diagnostic. An arbitrary in-flight `ReaderAt` cannot be interrupted; no per-read
goroutine is created. The same context reaches CLI detail reads, AVC context
search, summary analysis, and output writes.

The CLI handles Ctrl+C with exit code 130. Cancellation does not automatically
serialize the core's partial result. Output already written may be incomplete;
callers must discard stdout on exit 130. Existing exit codes remain unchanged.

Add `muxray tags --start <id> --count <n> <file.flv>` (default count 256). It
scans once and emits a bounded page with a separate, closed
`documentType=flv-tag-page`, `schemaVersion=1` contract. The page includes source,
scan status, `start/limit/total/next`, tag records, and bounded scan diagnostics.
Invalid page arguments use exit 64; structural scan failures still emit an
available page and use exit 2. No filtering, sorting, or renumbering is implied.

## Compatibility and scope

Existing scan, detail, and media-summary JSON contracts and goldens do not change.
`report.New` still explicitly materializes a document; streaming exports do not
copy the entire index. CLI commands use the new chunked scan path.

Incremental construction is not live publication: queries are supported only
after the scan returns (including cancellation or failure). Live snapshots,
progress subscriptions, disk indexes, GUI integration, removal of the tag cap,
and a registered real-world corpus are deferred. Synthetic sparse sources can
verify 64-bit offsets without claiming a real 10 GB corpus acceptance run.

## Validation

Cover block boundaries, page bounds/overflow/ownership, concurrent reads,
cancellation at read boundaries, partial-result recovery, sparse large offsets,
and flat/chunked consumer equivalence. Preserve existing goldens and add a page
schema/golden with negative validation. Run tests, vet, race, fuzz smoke tests,
and scan/page benchmarks.
