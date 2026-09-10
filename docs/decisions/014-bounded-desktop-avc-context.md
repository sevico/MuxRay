# ADR-014: Bounded AVC context and desktop syntax details

Status: Accepted

Date: 2026-09-10

Supersedes: ADR-013's deferral of AVC desktop details. Its document ownership,
request recovery, resource limits and close protocol remain in force. ADR-004's
CLI selection policy and the existing CLI JSON contracts remain unchanged.

## Context

The desktop can retain a source and inspect AMF0/AAC after a complete or partial
scan. AVC parsers already exist, but an ordinary AVC NALU Tag omits the width of
its NAL length fields. The CLI obtains that width from a preceding usable
sequence header. A desktop selection needs the same meaning with a bounded
search, cancellation, source identity checks and an explanation of provenance.

AVC RBSP fields may cross removed emulation-prevention bytes. Their core
MappedSpan can contain several original source segments. The current desktop
field DTO has one contiguous Span; flattening those segments into one range
would falsely label intervening bytes as part of the field.

## Decision

### One owned operation, two bounded read phases

Extract the CLI context resolver into a shared pure-Go package. It receives a
selected Tag, a read-only Tag lookup, parser options and explicit search limits.
The CLI uses unrestricted search limits to preserve its existing behavior.
The desktop uses the retained document's committed index without copying the
whole index or diagnostics. No live scan traversal is introduced.

Context resolution and the selected Tag parse execute in the same document
worker and three-second cooperative deadline, between the same before/after
source-stat checks. A canceled or superseded selection cannot publish either
phase's result. Context reads are restricted to validated candidate payloads;
the syntax parser for the selected Tag remains restricted to that Tag's
payload. No context lookup reopens a path or creates a second worker.

Resolve context only for ordinary AVC NALU packets. Search backwards for the
nearest usable AVC sequence header. A configuration's FramingComplete and
declared length width of 1, 2 or 4 bytes determine usability; incomplete deeper
SPS semantics alone must not reject an otherwise usable declaration. This
preserves ADR-004's behavior, including passing over unusable candidates.

The desktop limits each lookup to 8,192 preceding index records, 32 sequence
header candidates and 1 MiB of aggregate source reads, including the selected
packet's framing probe. These are independent of each candidate's parser
budgets and of the selected Tag's parser budgets. The limit is checked before
admitting the next unit of work. Index and I/O failures remain errors rather
than masquerading as an absent sequence header. Scanned records exclude the
selected Tag; candidate count means actual configuration parses; byte count
includes all returned source bytes, including repeated probes and parser reads.

Resolution distinguishes not_needed, found, missing and budget_exhausted.
Only reaching the beginning of the available prefix without a usable header
means missing. Budget exhaustion preserves the selected framing and reports
AVC_CONTEXT_LOOKUP_LIMIT_REACHED; it never implies that the source contains no
configuration. Neither case guesses a four-byte NAL length width. The first
desktop increment has no cross-request context cache and no manual override.

For a found configuration, display the source Tag ID, the declared length
width and its original bit position, plus bounded search counters. Missing or
limited context leaves the core AVC_NAL_LENGTH_SIZE_UNAVAILABLE result visible
and the NAL payload opaque. Search counters and derived values have no invented
source span.

### Bounded AVC projection

Add avcVideo to the existing desktop Detail kind without adding binding
methods or changing the request/response envelope. Show:

- FLV FrameType and CodecID, AVCPacketType and signed CompositionTime;
- DTS and derived PTS when the framing needed to compute it is present;
- avcC declarations, parameter-set counts and original parameter-set ranges;
- bounded SPS/PPS summaries, including derived dimensions when available;
- NAL length prefixes, header fields, types, lengths and original ranges;
- existing syntax diagnostics and the desktop context-search diagnostic.

FLV's declared keyframe flag is not proof of an IDR or an independently decodable
frame. NAL type identifies header syntax, not successful slice decoding.
End-of-sequence and damaged packets retain the core parser's meaning.

Desktop AVC parser limits are 64 diagnostics, 64 avcC parameter-set entries,
256 NAL units, 64 KiB per parameter-set EBSP body (excluding the NAL header)
and 4,096 RBSP mapping entries per parse. In-band SPS/PPS entries are subject
to the NAL-unit limit rather than the avcC entry limit.
The view independently retains ADR-013's 512 fields, 64 diagnostics, 256-byte
UTF-8 previews and 256 KiB actual JSON bound. Syntax completeness and omitted
presentation remain separate; all budget or preview omissions are explicit.
Ordinary compressed NAL bodies are source ranges, never materialized for UI.

Keep the single-Span DTO in this increment. Original contiguous framing and
NAL/parameter-set ranges use exact source spans. Derived SPS/PPS summaries use
no span. Do not publish deep RBSP bit fields requiring multiple SourceSpans;
their future expansion requires a multi-segment selection contract. The whole
parameter-set range is explicitly a container range, not the span of a derived
dimension or other summary value. Existing AMF0/AAC source positions are intact.

### Recovery and compatibility

Use ADR-013's one active operation, latest pending selection, bounded result,
monotonic IDs, serial polling and manual confirmation rules. Context work is
part of that operation and cannot outlive document ownership. Close prevents
new reads immediately and waits for active I/O before releasing the source;
the existing host exit budget still applies to uninterruptible reads.

Regenerate Wails bindings from the real Go DTOs. Existing CLI schema versions,
goldens and diagnostics retain their meaning; the new search-limit diagnostic
belongs to the bounded desktop context path. Existing non-AVC unsupported
outcomes and AMF0/AAC behavior remain covered by regression tests.

## Validation

Test 1/2/4-byte lengths, absent/unusable/changing configurations, selection
across index blocks and complete partial-prefix Tags, negative CompositionTime,
NAL overruns, independent search/parser/view budgets, very large source offsets,
and SPS emulation-prevention mappings without fabricated contiguous spans.
Verify source changes, cancellation and blocked reads in both read phases.

Use real production Windows windows for sequence headers, NAL packets,
diagnostics, configuration provenance, partial indexes, repeated selection and
source-change recovery. Inspect a tail Tag in a physically written large file.
Use a separate, explicitly identified overlay build for blocked AVC detail Close;
its receipts must distinguish UI cancellation from actual source release.
Synthetic fixtures contain syntax exercises, not claims of playable video.

Hex, multi-segment field selection, MP3 desktop details, summary, slice decoding,
deeper SPS/PPS/VUI expansion and strong source snapshots remain outside this ADR.
