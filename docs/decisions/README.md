# Architecture decision records

Repository-wide architecture and compatibility decisions are recorded here. An accepted ADR is immutable except for typo or link fixes; changing a decision requires a new ADR that supersedes it.

| ADR | Status | Decision |
|---|---|---|
| [ADR-001](./001-initial-architecture.md) | Accepted | Initial core, process, data-model, scaling, and compliance boundaries |
| [ADR-002](./002-on-demand-tag-details.md) | Accepted | Keep scans compact and expose bounded syntax through a separate tag-detail contract |
| [ADR-003](./003-typed-detail-contracts.md) | Accepted | Use generic internal detail dispatch while preserving independent, strongly typed AMF0 and AAC v1 contracts |
