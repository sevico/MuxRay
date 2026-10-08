# ADR-026: Desktop codec browsing, bounded evidence and snapshot v2

Status: Accepted

Date: 2026-10-08

Extends the desktop presentation in ADR-025 and the export format in ADR-019.
Existing CLI contracts and desktop snapshot v1 remain unchanged.

## MP3 frame selection

Keep the desktop's 64-frame / 1 MiB source-coverage limits and its shared detail
worker. An optional `DetailRequest.mp3FrameIndex` selects a zero-based frame
within that bounded inspection. Only retained complete frame boundaries with a
side-info record are selectable; truncated boundaries, unknown lengths and
frames outside the budget are not guessed or searched for.

Each selected-frame request inspects the normal Tag prefix so preceding
main-data-slot history remains available. The selected frame is not treated as
the beginning of a new stream. Request identity includes the optional frame
index; confirmation reuses the same identity. Cancellation, source checks,
supersession and close follow the existing detail lifecycle.

Retain all inspected frame/CRC/dependency summaries and first-frame VBR facts,
then expand only the selected frame's side information. Without an explicit
selection, retain the first-two-frame preview. The existing 512-field and
256 KiB detail budgets remain effective; `sideInfoOmittedFrames` and the display
truncation flag make omitted records explicit. Selection state and available
indices live in `Detail.Fields`, without changing the response envelope. The
UI exposes direct selection, previous/next, last selectable frame and preview.

This changes presentation, not MP3 decoding depth. Dependency remains physical
same-Tag slot coverage, never reconstructed reservoir state or proof of valid
audio. A selectable complete boundary may still contain diagnosed side info.

## Selected-Tag H.264 evidence

Compare the selected Tag's FLV FrameType declaration with already parsed NAL
headers and `slice_type` fields. This calculation performs no additional source
reads, does not mutate parser results and adds no corruption diagnostics.

Report IDR observation as yes/no/unknown. Observing a usable type-5 NAL is a
positive observation even when another part is unavailable. A negative requires
complete usable NAL-header coverage. Truncated framing, unavailable headers,
reserved types and unsupported extension semantics preserve uncertainty.

Report observed slice types as a set in P/B/I/SP/SI order, normalizing the two
encoded forms of each type. NAL-header coverage and slice-type coverage are
separate facts. Data partitions or an unread `slice_type` keep the latter
partial. A known type may be reported even when the later slice prefix cannot
finish. Neither an I slice nor a collection of slices establishes an IDR picture,
an access-unit count or independent decodability.

Retain the FLV declaration's four-bit source location, representative five-bit
NAL type locations and the first encoded `slice_type` of each observed type.
Only a contiguous original source mapping becomes a single clickable Span;
derived sets/relations do not invent one. Comparison is confined to the selected
Tag. Summary and curve GOP accounting continues to use FLV keyframe declarations.

## Diagnostic snapshot v2

Add an explicit `SnapshotRequest.schemaVersion`. Omitted/zero or 1 preserves the
original v1 JSON shape and rejects media-curve selection. Version 2 has its own
closed schema and is selected by the desktop UI.

An optional `mediaCurve` selection contains a canonical decimal `start`, a
`count` from 1 through 64, and one of `audio`, `video`, `mp3`, `gopAverage`,
`gopMax`. `summaryRequestId` identifies the completed retained analysis. The
request carries no client-provided curve data. The backend copies that bounded
page using the existing in-memory query and verifies its requested bounds.

The v2 document always contains `mediaCurvePage`: a response with matching
session/request identities and a page, or null accompanied by one explicit
omission reason. Scope records retained-page semantics and that these values are
covered payload rates and declared GOP durations, not transport throughput or
decodability. Unknown values remain null; indexed coverage and analysis
completeness remain separate. Existing detail fields freeze the MP3 frame
selection and H.264 evidence without a second copy of their trees.

Capture keeps the same slot fence and existing source-change latch checks. It
does not restat/reread the source, start analysis, or traverse the full index.
The report includes at most the requested 64 curve points, and the existing
2 MiB total, 4 KiB Hex, 512 trend-point and 100-event-page bounds remain.
Frozen bytes survive later selection changes and document close. Save recovery
and protection against overwriting the source are unchanged.

## Verification boundary

Go tests cover selected-frame history and source positions, evidence coverage,
request identity, retained-only capture, v1 isolation, omissions and byte limits.
Snapshot v2 has full/minimal goldens and schema negative cases. Independent
source-bit and FFmpeg-derived codec oracles remain separate from product output.

`tools/codec-regression/native_browse_evidence.py` uses the private D-Bus/Xvfb
smoke harness for MP3 third/last-frame selection, B multi-slice and IDR evidence,
current curve export and snapshot v2. It checks original source bits, independent
curve aggregation and CLI Summary values, plus Hex, source preservation, file
descriptor release and normal exit. Actual acceptance results belong in dated
evidence records.
Windows picker-process and macOS native acceptance remain separate work.
