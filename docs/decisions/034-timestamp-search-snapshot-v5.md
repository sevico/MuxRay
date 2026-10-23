# ADR-034: Retained timestamp-search evidence in desktop snapshot v5

Status: Accepted

Date: 2026-10-24

Extends [ADR-033](033-bounded-timestamp-observation-search.md) and the desktop
snapshot contract with the current completed timestamp-search page. A saved
result can now explain the selected mode, threshold, observed comparisons and
inspected range alongside its existing trend, detail and Hex evidence.

## Selection and retained evidence

Snapshot v5 accepts `selection.timeSearch` with `requestId`, `start`, `filter`
and `gapThreshold`. The numeric fields are canonical unsigned decimal strings;
request identity is nonzero and the gap threshold remains 1–4,294,967,295 ms.
The modes remain `all`, `regressions`, `duplicates` and `gaps`.

These fields identify an already retained result of the same document. They do
not supply rows, authorize another search, or reconstruct client-authored parser
state. `Document.SnapshotTimeSearch` returns a detached copy under the document
lock, honors close and known-source-change fences, and does not call `Stat` or
`ReadAt`. The one ready page can survive detail/Hex navigation. Admitting another
timestamp search replaces that page; an obsolete request cannot export it as its
own result. Pending, canceling, failed, missing, superseded or mismatched results
produce explicit omission reasons instead of exporting incomplete work.

`timeSearchPage` has the existing response envelope (`apiVersion`, `sessionId`,
`requestId`, `state`, `page`, `error`). A present page is ready with a null error.
Its page retains `start`, `next`, `total`, `scanned`, `filter`, `gapThreshold`,
`exhausted`, `scanComplete`, `unknownPredecessors` and `rows`. Neither the opaque
`cursor` / `nextCursor` nor the private predecessor map is serialized. A snapshot
is a report of evidence, not a portable session that can resume source access.

## Coverage and interpretation

The search always starts at Tag zero and its server-owned continuations advance
through a contiguous prefix. `inspectedStart` is therefore `"0"` and
`inspectedEnd` equals `next`; both ranges use an exclusive upper bound. The
current saved rows describe only `[start, next)`. Earlier pages have been
inspected but their rows are not retained or exported. An empty page still
records its actual progress. `unknownPredecessors` is a count for the current
page, not a cumulative count over the whole inspected prefix.

`exhausted` means the retained index has been reached; it does not turn a partial
scan into a complete file inspection. `scanComplete` preserves that independent
distinction. Comparisons use raw container DTS and source order for the same
`(baseType, StreamID)` key, including signed backward differences and unknown
predecessors after the bounded history budget is exhausted. Unknowns remain
visible in every filter and have no fabricated predecessor or delta.

The added scope fields are:

- `timeSearch`: `requested_retained_current_page_only`
- `timeSearchInterpretation`:
  `raw_container_dts_observation_not_corruption_or_playback_proof`

These observations do not establish corruption, playback failure, decoded frame
timing, timestamp rollover correction or the absence of anomalies elsewhere.

## Capture bounds and validation

Capture retains the existing 2 MiB total JSON limit and the search's limit of
64 rows from at most 4,096 inspected index records per batch. It matches request
identity, selected start/mode/threshold, current indexed total and scan
completeness. It checks monotonic page bounds, resource limits, ordering, filter
membership, signed delta arithmetic, unknown-predecessor relationships and
current-page counts before serializing.

For each of at most 64 rows, capture checks the current Tag and, when present,
its predecessor against immutable index metadata (at most 128 single-record
lookups). Tag identity, type, stream, DTS, offsets, sizes and framing must match.
This validates the retained server-produced evidence without traversing the
inspected prefix, rebuilding predecessor history, probing the source or starting
a worker. The known source-change latch is checked again before frozen bytes are
returned. No new source freshness guarantee is introduced by exporting.

Versions 1–4 keep their previous JSON shapes and reject a v5 selection. Version
5 can export media curves, AVC search, codec search and timestamp search together
when each requested retained section is available. An unavailable v5 page is an
explicit null with an omission reason. Existing export/save recovery and source
overwrite protections are unchanged.

## Validation

Go coverage includes detached pointer/slice copies, absence of source I/O,
pending/canceled/superseded requests, known source change and close, malicious
retained identities and malformed rows, strict threshold arithmetic, extreme
signed deltas across the 8,192-Tag boundary, continuation, empty pages, partial
indexes, unknown predecessors, legacy-version isolation and simultaneous codec
and timestamp pages. Real backend-generated minimal and full v5 goldens freeze
the wire format; JSON Schema and the independent snapshot validator cover the
same format for external consumers.

The bounded native acceptance follows the existing synthetic long-file flow:
start at zero, explicitly continue to the tail regression, open its evidence,
save a v5 snapshot through the native dialog, and validate both its schema and
source relationships. This acceptance does not expand into unrelated Windows
resource investigations or claim that a synthetic fixture is playable media.
