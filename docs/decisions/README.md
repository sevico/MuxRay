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
| [ADR-010](./010-minimal-desktop-adapter.md) | Accepted | Add a single-slot desktop adapter with stale-request isolation, exact integer DTOs, bounded pages, and explicit host shutdown |
| [ADR-011](./011-minimal-wails-shell.md) | Accepted | Isolate the Wails host, native picker, and bounded React UI from the pure-Go core |
| [ADR-012](./012-recoverable-native-open.md) | Accepted | Supersede ADR-011's Native open surface with bounded request outcomes, fencing and timed confirmation |
| [ADR-013](./013-retained-document-details.md) | Accepted | Retain a document source independently of scans and expose bounded, recoverable AMF0/AAC desktop details |
| [ADR-014](./014-bounded-desktop-avc-context.md) | Accepted; lookup partly superseded by ADR-020 | Reuse AVC context rules with bounded desktop lookup, provenance and truthful syntax spans |
| [ADR-015](./015-bounded-desktop-hex.md) | Accepted | Add bounded source byte windows and field navigation through the retained document's shared worker |
| [ADR-016](./016-retained-desktop-summary.md) | Accepted | Add cancellable media-summary v2 with exact bounded projections, retained event pages and shared source ownership |
| [ADR-017](./017-bounded-tag-filtering.md) | Accepted | Query committed Tags by container type, inclusive raw DTS and structural state with bounded source cursors |
| [ADR-018](./018-bounded-tag-trends.md) | Accepted | Show bounded source-ordered container trends with exact per-track DTS deltas and explicit preceding-context limits |
| [ADR-019](./019-desktop-diagnostic-snapshot.md) | Accepted | Export bounded desktop diagnostic snapshots through a recoverable native save operation |
| [ADR-020](./020-indexed-desktop-avc-context.md) | Accepted | Index AVC sequence-header candidates during desktop scans for bounded, order-independent long-media context lookup |
| [ADR-021](./021-isolated-file-picker-prototype.md) | Accepted for opt-in prototype | Isolate Windows FLV file selection in a bounded child process and validate actual exit before publishing its result |
| [ADR-022](./022-bounded-media-curves.md) | Accepted | Collect bounded source-window bitrate and declared GOP curves during Summary analysis and page them without source I/O |
| [ADR-023](./023-deeper-codec-details.md) | Accepted | Add opt-in CLI v2 and bounded desktop H.264 VUI / MP3 frame, VBR and protected-header inspection |
| [ADR-024](./024-bounded-pps-slice-prefix.md) | Accepted | Add AVC detail v3 PPS/slice prefixes with bounded parameter replay and reproducible codec regression |
| [ADR-025](./025-avc-checkpoints-mp3-side-info.md) | Accepted | Resume bounded AVC context with compact checkpoints and add MP3 v3 side information |
| [ADR-026](./026-desktop-codec-evidence-snapshot-v2.md) | Accepted | Browse bounded MP3 frames, compare selected-Tag H.264 evidence and freeze retained curve pages in snapshot v2 |
