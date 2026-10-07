# ADR-022: Paginated media bitrate and declared GOP curves

Status: Accepted

Date: 2026-10-07

Extends ADR-016's retained Summary. Existing CLI schemas and Summary meanings,
ADR-018's container trends, and ADR-019's snapshot v1 schema remain unchanged.

## Context

Summary event lists retain only bounded prefixes. They cannot supply curves for
later media, even though analysis continues counting the entire indexed prefix.
Users need variation and source navigation without retaining every media event
or repeatedly analyzing the file for each page.

## Decision

Collect one compact point per 256 consecutive source Tags in the existing v2
analysis pass. All Tag types occupy source positions; this is neither a fixed
time bucket nor a decoded frame timeline. The scanner's 1,000,000 Tag hard limit
bounds retention to 3,907 points. A partial scan covers only its committed index.
An empty index yields no points. The final short window is retained.

`AnalyzeV2WithCurves` observes changes to the existing cumulative counters after
each successful Tag visit. It adds no source reads or retained payloads. The old
analysis entry points do not allocate a collector. Errors and cancellation do
not publish partial curves; successful classification of a partial or unsupported
index may publish points with explicit scan/analysis completeness flags.

Each point contains audio, video and MP3 covered bytes, covered interval and
integer bit/s (`bytes * 8000 / milliseconds`, rounded down), plus declared
keyframe count, closed GOP count, total/min/max/average closed duration and
unclassified audio/video Tag count. Audio includes MP3; MP3 is a subset, not an
additional contribution. Unclassified Tags do not include ordinary Script Tags.

Rate coverage and GOP barriers are exactly those of Summary v2. Equal DTS groups
close only on the next strictly greater DTS in the same continuous segment;
regressions and classification barriers discard uncovered pending groups. The
last group has no inferred duration. Audio codec/MP3 parameter barriers and AVC
GOP configuration/EOS barriers keep their existing meanings; configuration
changes do not introduce new bitrate barriers. GOP means FLV-declared keyframes,
not verified IDR pictures or decoded frames, and only positive closed durations
participate in the duration statistics.

Assign each newly covered interval or closed GOP to the window containing its
closing Tag, even if its bytes or starting keyframe belong to an earlier window.
Store the first positive interval's closing Tag for each rate and the longest
GOP's closing Tag (first on ties) for GOP navigation. A window's declared count
and closed count need not match. Unknown rates/durations/navigation Tags are
null in the wire response, never zero-duration estimates. Known zero bitrate is
still distinct from unknown. These aggregates describe coverage, not playback
time or instantaneous bitrate.

### Query, bounds and lifecycle

Add `GetMediaCurves({sessionId,requestId,start,count})`, where requestId identifies
the completed Summary, start is a point index, count 0 defaults to 64, and the
maximum is 128. The response has apiVersion, echoed sessionId/requestId, page and
error. A page contains bounds (`start`, `limit`, `total`, `next`), windowTags=256,
indexedTags, scanComplete, complete and points. Points contain index, startTag,
exclusive nextTag, the metrics above and navigation Tag IDs. Positions, counts,
rates and durations are canonical uint64 decimal strings; windowTags and the
bounded page limit are JSON numbers. The complete response is at most 256 KiB.
At start==total return an empty array; start>total is invalid.

Pages are detached value copies, built only from the retained completed Summary.
Queries perform neither Stat nor ReadAt, start no analysis, and do not consume
the shared read worker/request sequence. Subsequent Detail/Hex requests preserve
the curves. Reanalysis, cancellation, analysis failure, a latched source change,
closure or replacement invalidates them with the Summary. An in-memory query
does not itself detect source changes; actual reads keep before/after checks.

### UI and compatibility

Show one 64-point page at a time, with explicit previous/next/first controls,
source-window coverage, audio/video/MP3 bitrate and average/maximum GOP duration.
Do not automatically fetch all pages. Exact data remains strings/BigInt; only
bounded relative plot coordinates become floating point. Unknown points break
the path, and an all-unknown metric displays a placeholder.

Selecting a known point opens its representative closing Tag through existing
Tag navigation. Hex uses the returned Tag's actual source extent. Both GOP
metrics navigate to the longest GOP's closing Tag, not a fictitious average
frame. Keep the curve page through detail/Hex navigation. Use the existing
five-second IPC timeout, explicit retry and old-response fences for page intent,
Summary identity, session, source changes, closure and disposal.

The existing diagnostic snapshot v1 excludes curves and the UI says so. CLI
summary v1/v2 JSON and their counts remain unchanged. This decision does not add
decoded frame curves, time-axis resampling, zooming, or live-stream analysis.

## Validation

Verify identical old/new Summary results, progress and source read sequences;
window boundaries, DTS/configuration/classification barriers, retained event
prefix independence, maximum point count, unknown values and cancel publication.
Test detached pages, precise large IDs, partial coverage, no query I/O, lifecycle
fences and point-to-Tag/Hex navigation. Exercise generated bitrate/GOP variation,
timestamp regression and long-media pagination in the real Linux production UI.
