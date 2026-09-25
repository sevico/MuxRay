# ADR-017: Bounded desktop Tag filtering and DTS location

Status: Accepted

Date: 2026-09-25

Extends ADR-008's immutable index and ADR-010's bounded desktop transport.
ADR-013's source ownership and ADR-016's retained Summary remain in force.
Existing CLI schemas and unfiltered Tag pagination are unchanged.

## Context

The desktop can navigate from Summary events to a Tag, its detail and Hex,
but arbitrary Tag location requires a known ID or sequential page browsing.
Summary event prefixes do not cover every occurrence in a long file. Users
need to find Tags by container type, raw timestamp and structural state using
the already committed index, including its prefix after an incomplete scan.

## Decision

### Predicates and ordering

Add a separate, stateless filtered query over the terminal scan's immutable
index. Combine these predicates with AND:

- Type: all, audio, video, script or other (any other base Tag type).
- Optional inclusive lower/upper raw FLV DTS bounds, in milliseconds.
- Structure: all, incomplete payload, missing PreviousTagSize, or present but
  inconsistent PreviousTagSize. Missing and inconsistent are distinct states.

DTS bounds are canonical decimal strings representing 0 through 4294967295;
null means no bound, and a lower bound above its upper bound is invalid.
The UI may trim user input before submitting its canonical value. Values are
container DTS, not PTS, elapsed playback time or decoded frame times.

Preserve source order and global Tag IDs. DTS may duplicate or regress and is
not automatically unwrapped. Do not binary-search the index by DTS or stop
when one Tag exceeds the upper timestamp bound: later Tags can match again.
Do not infer codec, keyframe or media-bearing status from container metadata.

### Bounded query and transport

Expose GetFilteredTags with sessionId, start, count and filter. Start is an
absolute source-index cursor encoded as a canonical decimal string. Count is
1 through 100 matched Tags. Each call visits at most 4096 index records and
stops when the match count, visit budget or indexed EOF is reached. Work and
allocation remain bounded even when matches are sparse or absent. No payload
reads, source rescans, whole-index copies or retained match lists are needed.

Return a detached page containing sessionId, filter, start, next, indexedTotal,
limit, scanLimit, scanned, exhausted and tags. Cursor fields and indexedTotal
are exact decimal strings; bounded limits and scanned are numbers. Next is
the exclusive end of records examined, including nonmatching records, and
scanned equals next minus start. Exhausted means next equals indexedTotal.
The page reports at most 100 Tags within the 256 KiB response budget.

Start equal to indexedTotal yields an empty exhausted page. A cursor beyond
indexedTotal is invalid. An empty non-exhausted page is valid and must permit
continuation. IndexedTotal is the size of the committed index, never the
number of matching Tags. There is no guessed global match count or extra
full-index traversal to compute one.

Queries are available after scanning reaches a terminal state, including a
canceled or damaged prefix. Close and a latched source change disable new
queries. A metadata query calls neither Stat nor ReadAt and only honors an
already detected source-change latch. It does not claim an immutable byte
snapshot; existing Detail/Hex source checks apply
when subsequently opening source bytes. The query does not join or replace
the shared source-reading worker, and leaves a ready Summary intact.

### Desktop interaction and recovery

Offer explicit Apply and Reset controls with labeled type, DTS and structural
filters. Distinguish editable fields from the applied query. Show the covered
source-index range, current-page matches and incomplete-index status. Empty
pages before indexed EOF offer Continue searching; they do not assert that
the entire file has no matches. First and Previous navigation use actual
source cursors, with bounded cursor history rather than count subtraction.

Preserve a filtered result page when opening one of its Tags or Hex locations.
Keep exact Tag-ID navigation available through the existing unfiltered index;
clearly show the active mode after switching. Query results use the existing
five-second IPC timeout and explicit retry. Condition changes, session
replacement, close, observed source change and controller disposal fence late
responses. Retrying the same stateless query is safe and starts no background
job. Do not automatically scan the entire index in a hidden frontend loop.

## Validation and scope

Test sparse matches across index blocks, empty non-final pages, inclusive
endpoints, repeated/regressed timestamps, distinct structural conditions,
invalid bounds/cursors, detached replies and fixed visit/count/JSON limits.
Verify partial-index access, no payload I/O, closed/source-changed rejection,
late replies and continued Summary/Detail/Hex navigation.

Browser smoke uses a clearly identified synthetic host. Windows native
acceptance separately uses the compiled production host and deterministic
fixture files. Record observed coverage and build identity. Supplement the
previous Summary acceptance with cancellation completion followed by a fresh
analysis using its separately identified controlled-I/O fixture.

Sorting, codec/PTS/keyframe predicates, frame plots, global match counts,
live-scan queries, export and increased index limits are outside this batch.
