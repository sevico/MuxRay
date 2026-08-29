# ADR-003: Generic internal detail dispatch with typed public contracts

- Status: Accepted
- Date: 2026-08-29

## Context

ADR-002 established one bounded, on-demand parsing boundary for ScriptData and
future codec details. The first public detail contract,
`flv-tag-detail-v1`, is deliberately closed and strongly typed for AMF0
ScriptData: its Tag is constrained to type 18 and its detail kind is
`amf0ScriptData`.

AAC reuses the same indexing and `ReaderAt` boundary, but its syntax tree and
conditional fields are unrelated to AMF0. Expanding the existing closed v1
Schema or replacing its concrete Go DTO with an untyped map would make pinned
v1 validators reject new output, weaken round-trip validation, and risk changing
the established AMF0 Golden encoding.

## Decision

- Core dispatch is generic: one selected `TagRecord` is routed to the matching
  bounded ScriptData or AAC parser without adding eager detail fields to
  `TagRecord` or the scan report.
- The generic result and report dispatch use an explicit, closed set of concrete
  Go types. Public detail values are not represented as `map[string]any`.
- AMF0 ScriptData retains `schemaVersion: 1`,
  `documentType: "flv-tag-detail"`, and
  `flv-tag-detail-v1.schema.json`. Its compact and pretty encodings remain
  byte-for-byte compatible.
- AAC uses an independent strong v1 contract:
  `schemaVersion: 1`, `documentType: "flv-aac-tag-detail"`, and
  `flv-aac-tag-detail-v1.schema.json`.
- Consumers select a detail validator by both `documentType` and
  `schemaVersion`; a shared CLI command does not imply a shared JSON shape.
- Fatal detail syntax errors stop at the selected payload boundary and expose a
  recovery offset. They never invalidate or mutate the completed outer FLV
  index.
- A future unified envelope or a new codec that cannot fit an existing contract
  requires a separately versioned Schema and, when it changes these compatibility
  rules, a new ADR. Existing v1 contracts are not silently broadened.

## Consequences

- `muxray detail` can use one internal parser/report dispatcher while returning
  either of two independently validated, strongly typed documents.
- Existing AMF0 consumers and Golden files do not change when AAC support is
  added.
- Consumers must inspect document identity before decoding the `detail` member.
- Some envelope fields are intentionally duplicated between the two v1 Schema
  files. This duplication is preferred to hidden cross-contract coupling or an
  unversioned shared shape.
- Adding another codec may add another typed contract; consolidation is deferred
  until a real common public model justifies a v2 migration.
