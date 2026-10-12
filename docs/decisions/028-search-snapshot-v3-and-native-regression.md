# ADR-028: Retained H.264 search snapshots and native regression

Status: Accepted

Date: 2026-10-12

Extends ADR-027. CLI schemas and diagnostic snapshot v1/v2 remain unchanged.

## Diagnostic snapshot v3

The desktop requests version 3. It keeps the v2 media-curve page and adds one
optional `avcSearch` selection containing `requestId`, `start` and `filter`.
The request names the current completed batch, never client-provided result
rows. IDs and cursors remain canonical decimal strings. Only v3 accepts this
selection; omitted/zero and explicit v1 retain the original v1 shape.

The backend copies the already retained page from the same document, checks
its request identity, start and filter, and exports `avcSearchPage` as a ready
response or explicit null with an omission reason. The page retains its actual
`[start,next)` coverage, indexed total, filter, read count, `scanComplete` and
`exhausted`, plus at most 64 rows with exact source spans and unknown reasons.
An empty completed batch is still a valid page. Exhausting a partial index does
not claim coverage of the complete file.

Only the current completed batch is exported. Pending, canceled, failed or
superseded work cannot supply a page. Detail/Hex navigation preserves the
completed search; a new search replaces it. Source-change and document-close
fences continue to apply. Capture performs no source Stat/ReadAt, starts no
analysis and does not walk earlier batches. The existing 2 MiB total limit and
save recovery semantics remain effective. Once captured, bytes are independent
of later selection changes or document close.

Scope explicitly describes NAL-header observations. A declaration difference
is not a corruption diagnosis; IDR observations do not prove complete pictures
or independent decoding. Unknown remains distinct from negative evidence.

## Regression ownership

The nine search/dependency native cases run in a separate Linux CI job with
their own production build, independently generated expectations and before/
after binary hashes. JSON and logs are retained on failure. Existing codec
jobs keep their own execution budgets.

Long-file native checks use a generated, source-auditable FLV corpus, check
forward cursors and the 64-entry back-history limit, and verify the exported
current page. Such repeated segments are structural fixtures, not business
recordings or proof of uninterrupted decodability.

Cancellation and close tests use a separately identified test-only Go overlay
that blocks the first actual search ReadAt until an identity-bound release.
Its extended task deadline is recorded, allowing the real UI cancellation to
arrive before a timeout. Receipts distinguish task entry, context cancellation,
read release and source closure. A production build contains neither the gate
nor the extended deadline. These controlled lifecycle results and production
long-file results are reported separately, with fixed binaries and source
hashes. Neither establishes Windows or macOS native acceptance.
