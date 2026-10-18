# ADR-033: Bounded timestamp-observation search

Status: Accepted

Date: 2026-10-18

Extends ADR-018 with a search over committed Tag metadata. A user can discover a
time observation near the end of a long index without knowing its Tag ID or
manually visiting every 256-Tag trend window. Existing trend, detail, Hex, CLI
and snapshot contracts remain unchanged.

## Observations and comparison identity

Search compares raw container DTS in source order, separately for each
`(baseType, StreamID)` key. Configuration packets and filtered Tags participate
because their indexed timestamps remain container evidence. It does not decode
payloads, infer presentation timestamps, unwrap rollover or identify corruption.

Result modes are all observations, regressions, duplicates and gaps. A negative
signed delta is a regression, zero is a duplicate and a positive delta is a gap
only when it is strictly greater than the requested threshold. The threshold is
an integer from 1 through 4,294,967,295 ms. DTS coordinates are unsigned but their
subtraction is signed; a large backward jump is not silently repaired.

The search retains at most 512 predecessor entries. When a new key needs space,
the entry with the smallest last Tag ID is evicted. This deterministic least
recently encountered policy does not depend on map iteration order. Before any
eviction, a missing predecessor proves that this is the first occurrence in the
searched prefix. After the first eviction, every missing predecessor is
conservatively unknown: a bounded map cannot distinguish a new key from a
previously evicted key without another unbounded history.

Unknown predecessors are always visible under every result mode. They are
coverage limitations, not fabricated deltas and not proof of a clean interval.
An observed Tag becomes the retained predecessor for its key even when its
delta does not match the selected mode. Continuation preserves this state, so a
batch boundary does not reset a comparison.

## Work and retention limits

`StartTimeSearch`, `GetTimeSearch` and `CancelTimeSearch` use the document's
existing asynchronous serial worker, global request IDs, recoverable outcomes
and lifecycle fences. Each request inspects at most 4,096 committed index entries
and publishes at most 64 result rows. Hitting either limit yields an explicit
continuation; an empty result page can still have advanced through its inspected
range. The UI retains one page and never silently scans the entire file.

Only index metadata is read. The search performs no source `Stat` or `ReadAt`,
and does not claim to discover a new external file modification. A previously
latched source change rejects the operation, and document close or replacement
fences an obsolete request. Results state their actual inspected/indexed range
and scan completeness; reaching the end of a partial index is not equivalent to
checking the complete source file.

At most eight immutable continuation states are retained for a document. Their
opaque decimal cursor tokens use request identity, not client-supplied offsets,
predecessors or serialized map state. An expired token requires a restart. A
candidate page and its continuation state commit only when the current request
successfully publishes. Cancellation, failure, source-change rejection and
document close cannot commit partial progress. A successful restart discards
older states; merely starting or cancelling a restart does not erase them.

## Navigation and export boundary

A result opens the existing 256-Tag trend window containing its Tag and can
select the existing detail and Hex evidence. Returning from that evidence keeps
the retained search page. The ordinary trend reader retains its own 4,096-Tag
lookback limit: a sparse same-key predecessor found by the search may be outside
that window's context budget. In that case the trend can honestly report an
unknown predecessor even though the search page retained a known comparison.
Navigation must not inject or fabricate missing trend context to make the two
surfaces appear identical.

The existing diagnostic snapshot can export the selected trend/detail/Hex
evidence after navigation. This decision adds no snapshot section or version,
and the timestamp-search page itself is not exported. The UI explains that
boundary rather than implying that an exported snapshot preserves the search's
whole inspected range, filter or continuation state.

## Validation and stopping scope

Focused tests cover cross-batch comparisons, all result modes, strict gap
thresholds, signed extreme deltas, row and index limits, deterministic eviction,
unknown coverage under every filter, continuation retention, cancelled/failed
publication, lifecycle fences and metadata-only access. A bounded native flow
starts from Tag zero, explicitly continues to a synthetic tail regression,
opens its trend/detail/Hex evidence and saves a real existing-format snapshot.

The reproducible 10,000-Tag fixture places a regression at Tag 8,192, whose DTS
is 100 ms below Tag 8,191. The next Tag returns to the regular timeline and
therefore has a +140 ms delta; a threshold below 140 ms legitimately reports
that additional gap. These are constructed container observations with complete
FLV framing and small AAC packet payloads, not a real damaged recording or a
claim of playable or fully decoded AAC. This work does not reopen the separate
Windows selector, historical exit or Shell GDI investigations.
