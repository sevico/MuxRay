# ADR-032: MP3 changes and bounded SEI message searches

Status: Accepted

Date: 2026-10-15

Extends ADR-027 and ADR-031 with separate searches. The original AVC NAL-header
search, CLI schemas and MP3 summary's retained 256-event prefix remain unchanged.

## MP3 changes and unresolved boundaries

The desktop searches validated frame boundaries for parameter changes, bitrate
changes, stop reasons, or their union. Normal frames are not result rows. VBR
bitrate changes are observations, not corruption. Parameter comparison includes
MPEG version, sample rate, channel mode/count and samples per frame; DTS belongs
to the source Tag, not an inferred per-frame timestamp.

A batch visits at most 256 Tags, attempts 1,024 frame headers, returns 64 rows,
and has a 64 KiB source-read ceiling. Current framing reads require at most
4,352 bytes per batch. A large Tag can continue at a validated internal boundary,
including changes after the summary's retained prefix. The server cursor retains
the preceding validated header, so a batch boundary does not erase a comparison.
Budget exhaustion yields a continuation, not a media stop reason.

Damage, truncation, unsupported Layer/free-format and unresolved audio boundaries
remain explicit. No sync-word rescan bypasses a stop. Normal audio-codec switches
and DTS rollback reset comparison; video/script interleaving does not. Unknown
or filtered audio is an unresolved audio boundary, not a claim that it is MP3.
Only complete frame rows can select exact MP3 frame detail. Stop rows provide
Tag and physical-source navigation without inventing a decodable frame.

## SEI messages and timing provenance

A separate batch search selects all messages, buffering-period, picture-timing,
or unresolved/incomplete evidence. It uses the existing bounded full-header and
SEI parsers to maintain compact SPS/PPS and timing activation context in source
order. There is no newest-SPS fallback. Source/configuration, EOS, unknown video
and incomplete coverage invalidate affected context.

Each batch visits at most 256 Tags, reads at most 1 MiB and returns at most 64
rows. Per-NAL/per-parse limits remain 64/256 messages and 64/256 KiB SEI payload
reads. One parsed Tag retains at most 257 rows, including one aggregate coverage
gap. Remaining rows drain on later batches without rereading that Tag; therefore
start/next Tag can be equal and a successful page can have zero source reads.
A coverage gap appears under every filter: an uninspected suffix cannot establish
absence of that message type. An opaque, fully bounded non-timing message does
not by itself imply missing timing context.

If a Tag cannot finish within the remaining page budget, its context/position
are rolled back and it is retried with a full budget on explicit continuation.
If the Tag alone exceeds the full budget, emit a coverage gap, invalidate its
context and advance. This prevents an endless continuation loop.

Rows retain message identity, status, original physical source envelope, and
explicit SPS/selection source when available. The envelope can contain inserted
emulation-prevention bytes; it is not a contiguous logical RBSP bit field.
These observations do not reconstruct an HRD/DPB schedule or establish decoding.

## Ownership, cancellation and export

Both searches share the document's serial reader, global request IDs, deadline,
source identity checks and lifecycle fences. Client continuation values are
opaque decimal tokens, never guessed file offsets or caller-supplied contexts.
Only a successful current request commits its candidate state. Cancellation,
I/O failure, source change and close cannot publish a partial page or cursor.

A document retains at most eight immutable continuation states. An expired token
requires restarting from source order. Restart clears older states. UI retains
one result page and offers explicit continuation/restart, cancellation and
transport-error reconciliation. It does not automatically scan the whole file.
Detail and Hex navigation preserve the ready page. Large coordinates and tokens
remain decimal strings; frame/message indices are bounded integers.

Snapshot v4 adds only the selected current codec-search page and its exact
request/filter/token identity. Export copies retained memory and performs no
source reads. Unavailable sections are null with omission reasons. v1–v3 remain
unchanged and reject the new selection. The existing 2 MiB snapshot ceiling
continues to apply.

## Regression tooling

The independent side-info oracle extracts only the bytes covering each field,
checking original bit offsets, encoded bits and integer value. Comparison with
the prior whole-source algorithm measures tool cost separately from product
parsing and native UI latency; elapsed values are local baselines, not CI limits.

A failed native close remains failed. A separate read-only process takes two
resource observations before a bounded UI traversal. Its eight-second timeout
can terminate only that observer. Receipts distinguish the original action,
observer completeness/errors/termination, and later application cleanup. No
close action is replayed and uncertain observations cannot turn a failure into
acceptance. Tests use private displays and in-memory GSettings.
