# ADR-001: Initial architecture and engineering boundaries

- Status: Accepted
- Date: 2026-08-29
- Baseline: `PROJECT_HANDOFF.md`, Draft 3

## Context

MuxRay is starting with a narrow, testable FLV scanning core. The first implementation batch covers bounded byte/bit readers, the shared source-range model, FLV Header/Tag/PreviousTagSize scanning, deterministic JSON, diagnostics, and unit/golden/fuzz tests. AMF0, codec details, FFmpeg-assisted features, Wails UI, and network inputs follow only after this data model is stable.

The design must remain usable with large and damaged inputs, expose exact source locations to future Hex and syntax views, and avoid making an optional desktop or media dependency part of the parser's correctness boundary.

## Decision

### Core and application layers

- The parser, index, diagnostics, analysis, and report model are pure Go.
- A pure-Go `muxray` CLI is the first application and exercises the same core APIs that later applications will use.
- The desktop application will be added later with Wails v2.14.0 and React/TypeScript. Wails bindings are an adapter over stable, paginated Go APIs; parsing rules do not live in the binding or frontend layers. A framework-version change requires its own review.
- The initial build remains compatible with `CGO_ENABLED=0`. Direct linking to libavcodec, libavformat, or other native codec libraries is outside the initial architecture.

### FFmpeg boundary

- FFmpeg and ffprobe are optional subprocess tools for preview, remuxing, assisted extraction, sample generation, and independent cross-checking.
- The core scanner works when they are absent. Their output is not the sole authority for FLV/AMF/codec field locations or MuxRay diagnostics.
- Future invocation uses an argument vector and a cancellable process context, not a shell command string. Time, stderr, output-size, and child-process cleanup limits are mandatory.
- Whether an FFmpeg build can be bundled is a separate distribution decision. This ADR does not approve any FFmpeg binary for redistribution.

### Source locations

Every component uses one source-range representation:

```go
type Span struct {
    ByteOffset uint64
    BitOffset  uint8
    BitLength  uint32
}
```

`ByteOffset` identifies the first source byte. `BitOffset` is zero for the most-significant bit and advances toward the least-significant bit. `BitLength` is the number of covered bits; zero-length spans are allowed as insertion/end-of-input locations. Core records expose spans through this shared model and canonical span methods. Reports, exports, selections, future syntax trees, and Hex highlighting reuse those methods rather than implementing separate offset calculations. Point diagnostics use the same byte/bit coordinate system through their `offset` and `bitOffset` anchor.

### Large-file behavior

- Sequential scanning builds compact, flat records with stable integer IDs.
- Bulk consumers use bounded pagination. A Wails binding must not send an unbounded tag list or syntax tree through IPC.
- Deep AMF/codec syntax is parsed on demand and stored only in a bounded cache. File bytes are fetched through bounded sequential reads or `io.ReaderAt`; loading an entire input is not the default.
- Lengths, counts, recursion, allocation, output, and external-process resources have explicit limits. Until a paged/chunked index replaces the contiguous slice, the initial scanner defaults to and hard-limits the in-memory index at 1,000,000 tags; diagnostics default to 1,024 entries with an allowed range of 2–65,536 so a truncation marker can coexist with the first cause. One allocating `ByteReader.ReadBytes` call is capped at 16 MiB; larger or cumulative reads require chunking and a caller-owned budget. Crossing a limit produces a stable diagnostic or command error rather than unbounded work.

### Public scan report

- The first public report contract is [`flv-scan-report-v1.schema.json`](../schemas/flv-scan-report-v1.schema.json) with `schemaVersion: 1`.
- Reports contain source size but no source path, file name, current time, elapsed duration, host data, or other run-specific metadata. The first version does not compute a source hash.
- Arrays have defined stable ordering and JSON is encoded deterministically as specified beside the schema. Diagnostic codes come from the [v1 diagnostic registry](../diagnostics-v1.md).
- Additive optional fields may be introduced compatibly. Removing a field, changing its meaning or JSON type, or changing established ordering requires a new schema version.

### Reverse engineering and compliance

- MuxRay has no clean-room separation requirement. Static analysis, decompilation, dynamic debugging, behavioral comparison, and reconstructed algorithms may directly inform implementation and compatibility work.
- That permission is an engineering-scope decision, not a grant of copyright, patent, trademark, privacy, or redistribution rights.
- Original binaries, manuals, assets, third-party libraries, and restricted samples are not added to a public repository or release without separate authorization. Third-party notices and FFmpeg/sample distribution obligations are reviewed independently before release.
- The MuxRay project license is **not yet decided**. This ADR neither selects MIT/Apache-2.0 nor authorizes a public distribution.

## Consequences

- The scanner and CLI can be tested on all target operating systems without a native toolchain or GUI runtime.
- Future GUI and export features share exact byte/bit locations and stable integer identities with the CLI.
- Large-input work is designed around indexes, pages, and lazy detail rather than retrofitted after an eager object graph exists.
- FFmpeg can improve workflows without making its version-dependent interpretation the core truth.
- Reverse engineering can proceed without personnel isolation, while release and reuse still require a separate rights review.
- The initial batch deliberately does not imply that AMF0, H.264/AAC/MP3 details, Wails, live RTMP/HTTP-FLV, or a product license are complete.
