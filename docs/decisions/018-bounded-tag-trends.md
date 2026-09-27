# ADR-018: Bounded source-ordered Tag trends

Status: Accepted

Date: 2026-09-27

Extends ADR-008's immutable index and ADR-017's metadata-only queries.
ADR-013's source lifetime and ADR-016's retained Summary remain unchanged.
Existing CLI schemas, Summary meanings and filtered Tag pagination are unchanged.

## Context

Summary exposes aggregate timing and limited event prefixes; filtering locates
matching Tags but does not show their surrounding variation. Plotting those
prefixes as a full-file media timeline would imply coverage that is absent.
The committed index already contains exact source order, raw DTS and container
payload sizes. These support bounded diagnostic views without parsing media.

## Decision

### Container points and preceding context

Show a continuous source-index window, including every Tag type. Keep source
order and absolute IDs/offsets. DTS is raw FLV uint32 milliseconds; do not sort,
unwrap, normalize to playback time or assume one Tag is one decoded frame.
Show declared DataSize and actual AvailableDataSize separately.

A track key is `(base Tag type code, StreamID)`. Its previous Tag is the closest
earlier source record with that key, including configuration, command, filtered
and damaged Tags. This is container adjacency, not supported-media sample
classification, codec continuity, GOP or decoded cadence. The FLV Filter flag
is returned as `filtered`; it is independent of ADR-017's query conditions.

Before processing the visible window, search backward from start for preceding
records of only the distinct keys found in that window (at most 512 keys).
Inspect at most 4096 preceding records; stop earlier when every key is found
or source index 0 is reached. The closest predecessor wins, even when its DTS
is greater than the visible point's DTS. Then process visible records in order,
updating each key's predecessor after each point.

Each point has one of these context states:

- `found`: previousTagId, previousDts and deltaMs are present; deltaMs equals
  current DTS minus preceding DTS, including zero and negative values.
- `start`: no predecessor exists between source index 0 and this point; all
  three previous/delta fields are null.
- `budget_exhausted`: the lookback stopped at 4096 without finding that key and
  without reaching index 0; all three fields are null, not zero or a guess.

Once a key appears within the window, later same-key points have `found`
context. Negative deltas range down to -4294967295; no unsigned subtraction
or timestamp wrapping is allowed. Unknown context is distinct from a first
Tag proven by reaching source index 0. An empty EOF window scans no context.

### Bounded API and lifetime

Add `GetTagTrends(PageRequest)` with sessionId, canonical uint64 decimal start
and count in 1..512. The frontend defaults to 256 visible points. The response
is `{apiVersion:1,page:TagTrendPage|null,error:Fault|null}`.

A page contains sessionId, the existing PageBounds (`start`, `limit`, `total`,
`next`), contextStart, contextScanned and points. `next` is the end of the
continuous visible window, `total` is the committed index length, and
`contextStart = start - contextScanned`. At EOF (`start == total`), points is
an empty array, contextScanned is zero, and contextStart/start/next equal total.
Beyond EOF is invalid. Context rows are not returned as extra visible points.

Each point contains tagId, offset, dts, typeCode, streamId, dataSize,
availableDataSize, payloadComplete, previousTagSizeState, filtered,
previousTagId, previousDts, deltaMs and context. PreviousTagSize state is
`valid`, `missing`, or `invalid` (present but inconsistent).

IDs, offsets, DTS and signed deltas are exact canonical decimal strings,
nullable only for missing preceding values. Bounded byte counts, type/stream
codes, limits and contextScanned are JSON numbers. Return detached points and
keep the complete 512-point response within 256 KiB. Each query visits at most
512 visible plus 4096 context records, with no full-index copy, hidden scan to
EOF, or per-file cache of all matches/trend points.

Queries run only on terminal immutable indexes, including a canceled or
damaged scan's committed prefix. They call neither Stat nor ReadAt, do not
join the shared source-read worker or change read request IDs, and preserve a
ready Summary. The document fences closure and an already detected source-
change latch. This query does not itself detect source changes; subsequent
Detail/Hex reads retain their before/after checks. Partial index coverage must
remain visible, independently of whether this bounded query succeeded.

### Desktop interaction and interpretation

Use source Tag order on the horizontal axis. Let users navigate bounded
windows and inspect exact point values, open the selected Tag's detail, and
locate its source bytes in Hex. Preserve query identity and window selection
through related views; distinguish trend windows from filtered match pages.

Use the existing five-second IPC wait with explicit retry. Fence stale replies
on window changes, close, source change, replacement sessions and disposal.
The query is stateless; retrying the same window does not start background work.
Do not automatically load every window or connect unknown preceding context.

The frontend may highlight repeats, regressions, truncated payloads and large
positive gaps. The configurable positive-gap threshold defaults to 1000 ms;
only deltaMs strictly greater than the threshold is a large gap. This is a
local display rule, not a new parser diagnostic or proof of corrupted media.
Changing the threshold does not require a backend read. Never label these
container differences as decoded frame duration, bitrate, GOP or lip sync.

## Validation and scope

Test the 512+4096 visit bound, predecessor choice across page/block boundaries,
mixed types and StreamIDs, repeated/regressing/uint32-boundary DTS, missing
context, EOF, partial indexes, invalid requests, detached replies and the
maximum response JSON size. Verify no source I/O, ready Summary retention,
metadata access while a read worker is blocked, and close/source-latch fencing.
Frontend checks cover exact arithmetic, threshold boundaries, late replies,
window navigation and Tag/Hex interaction. Browser checks use a synthetic
host; Windows native acceptance separately exercises the compiled production
host with deterministic fixture files and records observed coverage.

Full-file downsampling, media-bearing sample classification, PTS, bitrate/GOP
curves, timestamp unwrapping, decoding, export and increased index limits are
outside this batch. Such features require their own data coverage and boundary
semantics; Summary's limited event prefixes are not a substitute.
