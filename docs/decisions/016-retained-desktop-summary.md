# ADR-016: Retained desktop media summary v2

Status: Accepted

Date: 2026-09-13

Supersedes: ADR-015's deferral of media summary. ADR-013's source ownership,
ADR-014's AVC bounds, and ADR-015's shared read scheduling remain in force.
The CLI media-summary v1/v2 contracts and default version remain unchanged.

## Context

The desktop can inspect individual Tags and source bytes, but diagnosing a
file's timing, codec changes and keyframe distribution still requires the CLI.
Whole-index analysis can outlast an individual detail read. Sending the core
report directly would lose integer precision in JavaScript and retain too much
event data. A completed overview must remain available while inspecting an
event's Tag and bytes.

## Decision

### Analysis and ownership

Explicitly start media-summary v2 after scanning reaches a terminal state.
Analyze the immutable, committed index with the existing AnalyzeV2 core, on the
document's retained ReaderAt. Do not reopen the path, rescan, copy all Tags or
start a second read worker. A canceled scan can supply its committed prefix;
report scan completeness separately from analysis completion.

Summary joins Detail and Hex in the same monotonically increasing request-ID
namespace, one active worker and one replaceable pending intent. A valid new
read supersedes active/pending analysis. A ready summary has its own retained
result ID and survives subsequent Detail/Hex navigation; a new Summary replaces
it. Get and event pagination read memory only. Close and observed source change
clear the retained summary. Existing before/after source checks are retained;
they do not guarantee a content snapshot against undetectable in-place writes.

Use a cooperative 120-second Summary deadline, measured when work starts; keep
the three-second Detail/Hex deadline. Cancellation never releases a source while
an actual read is blocked. Pending cancellation can finish immediately; active
cancellation remains canceling until the worker returns. Discard an aggregate
on read failure, cancellation or deadline, because a failed Tag may have updated
internal counters. Do not display that partial aggregate as a completed result.

AnalyzeV2WithProgress is an additive Go API. Its callback reports processed and
total Tags initially, periodically after successful Tag visits, and at the end.
AnalyzeV2 remains a compatibility wrapper. Progress never includes a failed Tag
and does not change the CLI report or its exit semantics.

### Projection and bounded transport

Add internal/summaryview to project the core report into detached scalar DTOs.
All counts, coordinates, timestamps and rates are decimal strings, or null when
unavailable. Event IDs identify retained list positions; Tag IDs and source
positions remain absolute. No source index, parser tree or parameter-set bytes
are retained by the projection. CLI and desktop comparisons use the same
analysis options, not the CLI's larger defaults.

Desktop budgets are 1,024 keyframes, 128 configuration events per AAC/AVC list,
16 MiB configuration bytes, 256 MP3 header events and 256 diagnostics. AAC keeps
64 parser diagnostics. AVC uses the existing bounded desktop AVC parser options.
Aggregate counters continue after event retention limits. Lists expose observed
and retained counts and truncation separately. When diagnostic collection has
truncated and the core has no omitted total, observed is null, never a guessed
count. A retention limit alone does not imply a damaged source.

Limit projection text to 256 UTF-8 bytes, overview metrics to 128, values per
event to 24, overview JSON to 64 KiB, each event to 16 KiB, retained report JSON
to 8 MiB, and any transport reply to 256 KiB. Event pages contain at most 100
items and shorten further for the byte budget; next is the actual continuation
position. At EOF return an empty page; beyond EOF reject the query. Consumers
must follow next rather than add the requested count.

Expose StartSummary, GetSummary and CancelSummary with sessionId/requestId and
pending/canceling/ready/failed replies, processed/total, nullable overview and
bounded fault. GetSummaryEvents adds kind/start/count and bounded page metadata.
These are desktop apiVersion 1 DTOs, not a new CLI schema. Current repeated
requests are idempotent; stale admissions cannot start work. A terminal ready
or failed cancellation is an idempotent query of that result.

### Recovery and presentation

Use explicit Analyze/Reanalyze and Cancel controls, with Close always available.
While analysis or its outcome is unresolved, disable competing reads. Use
five-second IPC waits, serialized 200 ms queries and at most 600 pending query
replies before pausing for explicit confirmation. Lost Start or Cancel replies
are reconciled with Get and the original ID; a local timeout neither cancels
the worker nor starts another analysis. A completed retry uses a fresh ID.
Fence delayed responses on close, source change, new session and disposal.

Present compact timing/bitrate/codec cards, expandable exact metrics and bounded
keyframe, AAC, AVC, MP3 and diagnostic lists. Event Tag links use existing bounded
Tag navigation. Diagnostic source links use point locations in Hex. Clearly
label retained prefixes, missing timing coverage, declared keyframes rather
than verified IDR, MP3 first-header scope, and range-based A/V skew rather than
decoded lip sync. Derived fields do not acquire invented source spans.

## Validation and scope

Compare deterministic mixed-codec, timing, retention and damaged fixtures with
the same-budget CLI core. Test progress, exact integers, page count and byte
limits, copied replies, cross-kind retention, cancellation, lost replies,
source-change latching and real blocked-read Close ownership. Browser checks
exercise the UI against a synthetic host; native Windows acceptance separately
exercises the compiled production host and a clearly identified I/O-gated build.

Frame plots, export, full MP3 frames/VBR, deep H.264 semantics, decoding and
streaming remain outside this batch.
