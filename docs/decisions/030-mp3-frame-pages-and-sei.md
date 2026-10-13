# ADR-030: MP3 frame pages, summary links and H.264 SEI evidence

Status: Accepted

Date: 2026-10-13

Extends ADR-025 through ADR-029. Source files remain read-only. Parsing and
presentation have separate finite budgets; a partial result preserves evidence
without claiming a decodable audio frame or picture.

## MP3 desktop pages

The optional desktop `DetailRequest.mp3PageStart` is a uint32 multiple of 64.
`mp3FrameIndex`, when present, is the original Tag-relative frame index in that
page. An omitted page retains the original first-64-frame behavior. CLI MP3
schemas 1–3 and their defaults remain unchanged.

Each page locates at most 64 sequential frames, covers at most 1 MiB and reads
at most 64 KiB of headers/side information. It never searches for a new sync
word after an unresolved boundary or joins frame bodies across Tags. The page
keeps original frame indices and source spans, expands only the selected
side-info (or the first two frames in preview) and reports omitted display
content. Choices on a late page use bounded `selectableFrameOffsets` plus the
explicit page start; a long CSV of absolute indices would exceed the existing
256-byte generic field value limit. Frame labels and source navigation always
use the original indices.
The native frame selector is recreated when the page start changes so that
WebKitGTK exposes the replacement options through its accessibility tree.

An opaque checkpoint retains the next frame/offset, physical main-data history
and at most 64 compact source slots, with no open reader or decoded audio.
The document caches at most 64 checkpoints across Tags. A request starts from
the nearest usable preceding checkpoint and advances at most 16 pages; if the
target has not been reached, it returns `preparing` and a real progress cursor.
Only an explicit continuation creates the next request. Unresolved boundaries
return `stopped`, not an invented page. Page readiness does not mean that the
whole Tag or the file has been checked.

Checkpoints publish only with the current request after cancellation and source
identity checks. Close/source changes clear them. Request idempotence includes
the page, frame and dependency intent; the frontend verifies those identities
before showing a result. The existing serial worker, three-second cooperative
read deadline and lost-response confirmation behavior remain in force.

Dependency resolution can reuse the selected page and compact same-Tag suffix,
then walk previous Tags within its independent 512-Tag / 64-audio-Tag / 256-frame
and 1 MiB read/coverage bounds. The frame budget includes the selected page;
reused cached slots incur no new read. Previous-Tag paging reserves a complete
64-frame page before advancing, so an insufficient remaining page budget can
conservatively stop before the numerical frame limit. At most 64 physical
sources are retained. It must
locate a previous Tag's complete suffix from validated boundaries; damage,
codec/parameter changes, missing history and budget exhaustion remain unknown.
Source links open the originating frame page or exact physical Hex range.
This describes byte provenance, not bit-reservoir occupancy or decoding.

Media-summary v3 counts and retained events remain independent. A retained
complete MP3 frame event opens its exact page and side-info. Stop/legacy events
still open their Tag. The desktop retains at most 256 MP3 frame summary events, while
frame pages can continue through later boundaries. Event DTS still belongs to
the Tag; no per-frame timestamp or playback duration is invented.

## SEI detail v5

`detail --schema-version 5` accepts AVC only and extends the full ordinary-header
v4 report with optional `sei` on NAL entries, including tolerated SEI entries
inside avcC. CLI v1–v4 remain frozen. Desktop selected full AVC details opt in;
parameter replay and batch evidence search do not inspect SEI bodies.

The parser reads payload type/size continuation bytes, multiple messages and
RBSP trailing bits. It interprets `recovery_point` and
`user_data_unregistered`: recovery count/flags, a 16-byte UUID and at most 64
bytes of application data as hexadecimal. Unknown types retain mapped payload
ranges and explicitly set `syntaxSupported=false`. Recovery declarations do
not reconstruct DPB state or prove independent decoding. Buffering-period,
picture-timing/HRD interpretation, registered user data and picture decoding
are outside this first version.

SEI has independent hard limits of 64 KiB source bytes and 64 messages per NAL,
with 256 KiB and 256 messages per selected parse. Existing cumulative RBSP
mapping, diagnostic and desktop 512-field/serialized-size limits still apply.
Actual inspected RBSP/EBSP coverage remains distinct from declared NAL length.
Truncation, invalid syntax/EPB/trailing bits and budget stops preserve partial
fields. `framingComplete` describes message boundaries and exact trailing bits;
`complete` additionally validates supported payload syntax. Neither means an
unknown payload was interpreted. A UUID data preview can be truncated while
its message syntax is complete.

RBSP source spans exclude emulation-prevention bytes. Desktop fields with one
contiguous span can open Hex directly; discontiguous fields expose separately
clickable `.source[i]` children. Data previews are binary hex, not executable
content. Snapshot v3 stores these generic detail fields under the retained
request identity; no new snapshot envelope is necessary.

## Regression and platform boundaries

Authored structural fixtures and independent bit/byte expectations cover MP3
pages beyond frame 64 and 1 MiB, summary frame 255, cross-page/cross-Tag physical
sources, SEI continuation headers, recovery/UUID/unknown messages, EPB, partial
payloads and separate budgets. Negative controls reject wrong values, spans,
re-numbered frames, omissions and invented coverage. These authored inputs
establish parser behavior, not real media decoding quality.

Linux native rounds exercise actual GTK/WebKit controls, exports, source links
and close/exit resource release under private Xvfb/D-Bus and memory-only
GSettings. They do not establish Windows or macOS native acceptance.

Syntax references: [ITU-T H.264 Annex D](https://www.itu.int/rec/T-REC-H.264),
[FFmpeg H.264 SEI syntax](https://ffmpeg.org/doxygen/trunk/cbs__h264__syntax__template_8c_source.html),
[FFmpeg MPEG audio header arithmetic](https://ffmpeg.org/doxygen/trunk/mpegaudiodecheader_8c_source.html).
