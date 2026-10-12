# ADR-027: Bounded H.264 evidence search and MP3 dependency sources

Status: Accepted

Date: 2026-10-12

Extends ADR-026 with explicit desktop reads. Existing CLI schemas, snapshot
schemas and Summary/GOP definitions remain unchanged.

## H.264 batches

`StartAVCSearch` accepts a shared read request ID, zero-based index `start`, and
`mismatches`, `unknown` or `all` filter. Each request examines at most 256 Tags,
retains at most 64 rows and reads at most 1 MiB. Each AVC media Tag inspects at
most 256 NAL headers. NAL bodies, slice syntax and parameter-set replay are not
read by this search. avcC framing is checked using lengths, skipping SPS/PPS
bodies. The existing scan's two-byte video probe also records configuration and
framing-barrier positions in a second bounded bitset (at most 125,000 bytes).
Arbitrary batch starts recover framing from the nearest such event without
walking all preceding Tags or reusing a configuration across a barrier.

Rows preserve `yes/no/unknown` IDR observations. A usable type-5 header provides
positive evidence even with partial coverage; a negative requires complete,
classifiable NAL-header coverage. Filtered or unclassifiable video can appear
as unknown without being asserted to be AVC. Missing configurations, framing
damage and bounded coverage have explicit reasons. Relations are observations,
not corruption diagnoses, picture counts or guarantees of independent decoding.

Only a successfully completed batch publishes `[start,next)` and rows. A row
limit consumes its final Tag before advancing the cursor. `scanComplete`
distinguishes a partial source index from a complete file scan; `exhausted`
means the end of that index. Continue uses `next`; retry after cancellation
restarts the uncommitted batch. The UI retains only one page and up to 64 prior
start positions. Tag navigation and exact-bit Hex links retain the completed
page. Search rows are not a new snapshot section; users can navigate to and
export an existing selected-Tag detail.

## MP3 physical sources

`DetailRequest.mp3Dependency=true` explicitly requests source mapping for the
selected frame; an absent frame selector means frame zero. Existing detail
requests keep their same-Tag behavior. Request identity binds the opt-in flag
and frame selector. The resolver parses the selected Tag once and reuses that
bounded parse for presentation, then visits preceding Tags backwards as needed.

Limits are 512 preceding Tags, 64 preceding audio Tags, 64 frames per Tag,
256 frames in total, 1 MiB source coverage, 1 MiB actual reads and 64 source
ranges. Coverage charges whole SoundData ranges, including uninspected tails.
A preceding audio Tag must be parsed from its beginning to its actual end;
unparsed tails cannot be skipped to guess the immediately preceding frame.
Video/script interleaving does not break audio adjacency. Codec, MPEG version,
sampling frequency, channel or stream changes; invalid headers, side-info or
CRC; insufficient history; and exhausted budgets preserve unknown with a reason.

Sources describe the declared `main_data_begin` suffix and current frame's
main-data slots, ordered by physical source position. Each range has a Tag ID,
frame index and exact original byte span. Unknown results may retain only the
already established suffix, with requested/resolved/missing byte counts. These
are physical slots, potentially including consumed or ancillary data. Neither
reservoir consumption nor Huffman data is reconstructed or validated.

The bounded `MP3 dependency sources` detail-field group is projected before
ordinary frame summaries, so required provenance survives the 512-field limit.
It is exported through existing snapshot detail fields; omitted regular fields
remain marked by existing truncation metadata. Buttons open the source frame
or highlight the exact source range in Hex.

## Ownership and verification

Both features use the existing single document reader and three-second task
deadline. They respect source identity checks before/after reads, generation
fences, timeout recovery and document close. Canceling a blocked H.264 batch
does not release ownership until the actual read returns. Get operations do
not start work; a lost start is confirmed by the same identity. Source changes
clear retained evidence. MP3 requests can be superseded by another detail/Hex
selection or canceled through document close.

Core, document, transport and frontend tests cover coverage semantics, budgets,
pagination, source spans, identity, cancellation and late replies. Independent
Python byte oracles compare one MP3 stream packed as merged, split and
interleaved Tags, and inspect encoded H.264 fixtures plus structural mutations.
Linux native acceptance uses private D-Bus/Xvfb with memory-only GSettings and
real Wails bindings. Results and binary hashes belong in the dated acceptance
record; they do not establish Windows or macOS native acceptance.
