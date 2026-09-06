# Architecture decision records

Repository-wide architecture and compatibility decisions are recorded here. An accepted ADR is immutable except for typo or link fixes; changing a decision requires a new ADR that supersedes it.

| ADR | Status | Decision |
|---|---|---|
| [ADR-001](./001-initial-architecture.md) | Accepted | Initial core, process, data-model, scaling, and compliance boundaries |
| [ADR-002](./002-on-demand-tag-details.md) | Accepted | Keep scans compact and expose bounded syntax through a separate tag-detail contract |
| [ADR-003](./003-typed-detail-contracts.md) | Accepted | Use generic internal detail dispatch while preserving independent, strongly typed AMF0 and AAC v1 contracts |
| [ADR-004](./004-avc-nal-length-context.md) | Accepted | Resolve AVC NAL length prefixes from explicit or prior sequence-header context without guessing |
| [ADR-005](./005-bounded-media-summary.md) | Accepted | Derive bounded AAC/AVC timing, bitrate, keyframe/GOP, configuration, and A/V range summaries without timestamp guessing |
| [ADR-006](./006-bounded-mp3-detail.md) | Accepted | Add a bounded first-frame MP3 detail contract while keeping media-summary v1 frozen |
| [ADR-007](./007-mp3-media-summary-v2.md) | Accepted | Opt in to MP3 cross-tag summaries with explicit codec boundaries and a separate media-summary v2 contract |
| [ADR-008](./008-cancellable-chunked-index.md) | Accepted | Add cancellable chunked scans and bounded tag pages while preserving legacy scans and existing JSON contracts |
| [ADR-009](./009-background-scan-sessions.md) | Accepted | Add bounded progress snapshots and owned background scan sessions with post-scan paging and explicit cleanup |
