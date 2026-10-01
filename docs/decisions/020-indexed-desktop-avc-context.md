# ADR-020: Indexed AVC configuration candidates for desktop details

Status: Accepted

Date: 2026-10-01

Partially supersedes [ADR-014](014-bounded-desktop-avc-context.md): desktop
configuration discovery uses a bounded candidate index instead of an 8,192-Tag
backward search. Its usable-configuration policy, syntax projection, source
ownership and request protocol remain in force. Extends ADR-008/009 with an
optional committed-record observer; their existing scan entry points and
ADR-004's CLI behavior remain unchanged.

## Context

The 40-minute generated AVC/AAC corpus contains 172,504 Tags, with its AVC
sequence header near the beginning. Its final AVC NALU is Tag 172,497. The
ADR-014 desktop search stops after 8,192 preceding records, so it cannot recover
the declared NAL length width even though that configuration remains available
in the retained source. The resulting opaque payload and budget diagnostic are
truthful, but prevent useful inspection of ordinary long media.

Increasing the backward distance makes each selection read more unrelated
payload prefixes. Remembering only the last configuration selected by the user
would make results depend on navigation order and could miss an intervening
configuration. A Summary event list is independently bounded and may omit the
configuration needed by a selected Tag; running Summary must not be a
prerequisite for details.

## Decision

### Observe committed records without changing default scans

Add an opt-in, synchronous, generic Tag observer to the context-aware scanner
and borrowed-source session path. The document installs the observer. The FLV
scanner does not know AVC candidate rules or retain payload data, and existing
scan entry points install no observer. CLI scanning therefore continues to read
container structure only.

Call the observer after committing each immutable TagRecord and before starting
the next Tag. Pass the scan's cancellation context. The observer reads exactly
two payload bytes only for an unfiltered video Tag with a complete payload of
at least two bytes. Missing or invalid PreviousTagSize does not make an otherwise
complete payload ineligible. Audio, script, unsupported container types, filtered
Tags and incomplete payloads incur no observer payload reads.

A prefix identifies a candidate when CodecID is 7, FrameType is not 5, and
AVCPacketType is 0. This is a candidate declaration, not validation of avcC or
SPS/PPS. The observer does not parse syntax, retain payload bytes or cache a NAL
length width. Across the existing one-million-Tag hard limit, additional source
reads are at most 2,000,000 bytes.

An observer I/O error or cancellation stops the scan and returns its committed
prefix with the actual error. It is not a missing-configuration result or a
fabricated container diagnostic. The just-committed record can remain available
even when observing that record did not complete. Structural diagnostics already
established by the scanner remain attached to that prefix.

### Bounded candidate identity and coverage

`avccontext.CandidateIndex` has a usable zero value. It records candidate Tag IDs
in a bitset with at most 15,625 uint64 words, or 125,000 bytes, under
`flv.HardMaxTags == 1,000,000`. Allocate candidate storage lazily on the first
candidate. Retain no duplicate TagRecords, payloads, parsed configurations or
per-selection history.

Track a separate exclusive `covered` frontier: every Tag with ID below this
frontier was successfully observed in source order. A skipped, ineligible Tag
still advances coverage. Failed or canceled probes do not. Enforce contiguous
observation; an unobserved gap must never be represented by a zero candidate bit.

For selected Tag ID `s`, indexed predecessor lookup requires `s <= covered`.
Only IDs strictly less than `s` are relevant. Thus, after an observer failure on
the final committed Tag, that Tag can still use the fully observed preceding
range. Reject uncovered or invalid input with `ErrInvalidInput`; do not label
unknown coverage as `missing`. Partial scans, tag-limit stops and canceled scans
can use their covered committed prefix without requiring a complete file scan.

The scan worker alone builds the candidate index. Detail processing begins after
that worker finishes, so the index is then immutable and shares the document's
lifetime. It is not exposed as a mutable frontend object or shared across files.

### Resolve the nearest usable candidate for each selection

`ResolveIndexed` first applies the existing selected-packet framing rules. Only
ordinary AVC NALU packets need preceding context. Find candidate predecessors in
the bitset, strictly before the selected Tag, and visit them in descending source
order. In-memory word traversal is bounded by the hard Tag limit and observes
cancellation. Large logical Tag distances cause no intervening payload reads.

For each candidate, retrieve and validate its indexed record, re-read its
framing prefix and invoke the existing bounded AVC parser. Use a declaration only
when `ConfigPresent`, `FramingComplete` and a length width of 1, 2 or 4 hold.
Unusable candidates may be skipped within the existing budgets. Damaged deeper
SPS semantics alone do not reject an otherwise usable framing declaration. A
candidate bit never proves that configuration is usable, and no result guesses
a four-byte width.

Every selection repeats this validation against the retained source. Selecting
the tail first, returning to an earlier Tag or crossing configuration changes
therefore uses the same nearest-usable policy. Results do not depend on a prior
selection, a Summary request or retained Summary event counts.

Keep the limits of 32 actual configuration parses and 1 MiB of context source
reads, including the selected framing probe, repeated probes and parser reads.
Context lookup and selected-Tag projection share the existing three-second
cooperative detail deadline. Candidate-parser, selected-parser and presentation
budgets remain independent. Exhausting a work budget retains
`budget_exhausted` and `AVC_CONTEXT_LOOKUP_LIMIT_REACHED`; reaching the beginning
of a fully covered candidate range without a usable declaration means `missing`.
I/O, lookup and cancellation errors remain errors.

### Provenance, lifecycle and compatibility

Resolution records `LookupMethod` as `candidate_index` or `backward_tags`. The
desktop exposes it as a derived `lookupMethod` field through its existing generic
detail fields. `scannedTags` counts actual indexed records visited, excluding the
selected Tag; it does not measure the logical distance to the source
configuration. With indexed lookup, unrelated skipped records contribute zero.
`candidates` continues to count actual configuration parses, and `bytesRead`
continues to count context source bytes returned for this request. These derived
fields have no source Span. The source Tag ID, declared width and original
length-size bit Span retain ADR-014's meanings.

There is no new binding method, DTO envelope or JSON schema version. The shared
backward resolver remains available to the CLI with its existing policy and
output contracts. Detail snapshots include the new derived field through the
existing bounded generic representation.

Before/after source identity checks, stale-request fencing and the single active
read worker remain unchanged. Canceling a scan leaves covered committed Tags
available to independently canceled detail requests. Closing the document stops
admission, cancels active work and waits for actual I/O completion before releasing
the source and candidate index. A timeout cannot forcibly interrupt a blocked
ReaderAt. Observable source changes invalidate detail results; the existing stat
checks still do not constitute a byte snapshot or detect every same-stat rewrite.

## Validation

Cover distant configurations beyond 8,192 records, tail-first and reordered
selections, changing 1/2/4-byte declarations, unusable intervening candidates,
usable framing with damaged SPS, and candidate/byte/deadline exhaustion. Verify
strict predecessor behavior at IDs 0, 63, 64 and 65, the hard-limit boundary,
empty candidate sets, sparse bitsets and uncovered or out-of-order observations.

Verify observer read bounds and filtering, full payloads with partial trailers,
incomplete payloads, prefix I/O failures, cancellation during a probe, committed
partial-prefix access, source changes, supersession and blocked Close. Default
scanner/CLI tests must continue to establish their structure-only reads and
unchanged contracts. Confirm the candidate index is released with the document.

For production Windows acceptance, open the 40-minute corpus and select the
final AVC NALU directly, without first running Summary or visiting a configuration
Tag. Check the resolved source declaration, NAL boundaries and source Hex,
repeat selections in another order, export the bounded diagnostic snapshot,
then close the file and application. Record actual acceptance results separately
from this decision and from historical ADR-014/corpus observations.

## Consequences

Long-media AVC details can locate their preceding configuration without a
distance-dependent source scan. Opening a desktop document now performs bounded
two-byte video probes, so a payload I/O failure can stop this opt-in scan after
its structural record has committed. Candidate memory and total probe bytes
have fixed bounds, while source latency and blocked I/O remain cooperative.

The index improves configuration discovery only. It does not add slice decoding,
deep SPS/PPS semantics, source snapshots, manual width overrides or a persistent
configuration cache. Real long recordings remain a separate corpus requirement
from the generated 40-minute sample.
