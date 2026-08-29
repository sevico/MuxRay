# ADR-002: On-demand tag details and a separate public contract

- Status: Accepted
- Date: 2026-08-29

## Context

MuxRay must index up to one million FLV tags without retaining payloads or building a recursive object graph for every tag. ScriptData can contain deeply nested AMF0 values and large strings; later AAC and AVC configuration records will have the same distinction between a compact tag summary and a detailed syntax view.

The existing `flv-scan-report-v1` contract is deliberately closed with `additionalProperties: false`. Adding detail fields to that document would cause existing version-1 validators to reject output while eager detail parsing would also make default scan memory depend on payload content.

## Decision

- `flv.Scan` remains a container-only pass and `TagRecord` remains a compact payload index.
- Deep syntax is parsed on demand from `io.ReaderAt` inside the selected tag's canonical `PayloadSpan`.
- AMF0 values use a bounded flat arena with integer node relationships, not a recursive pointer graph.
- The CLI exposes selected details through `muxray detail --tag <id> <file.flv>`.
- Tag details use the independent `flv-tag-detail-v1` JSON Schema. `flv-scan-report-v1` output remains byte-for-byte unchanged.
- A detail parser stops at the selected tag boundary after an unrecoverable syntax error. The outer FLV index remains valid and later tags remain addressable.
- Payload size, nesting depth, node count, container entries, retained string bytes, and diagnostics all have explicit hard limits.
- Future AAC, AVC, and other codec syntax details should reuse this boundary instead of expanding every scan record.

## Consequences

- Inspecting one detail requires a completed tag index and an additional bounded `ReaderAt` pass over the selected payload.
- Consumers select the scan-report or tag-detail schema by command/document type rather than assuming that one response contains every syntax field.
- Batch detail export, caching, and pagination require explicit budgets and are deferred until a session/index layer exists.
- The default million-tag scan keeps its existing memory and throughput characteristics.
