# Sample manifests

This directory records the identity, provenance, handling status, coverage features, and minimum expected result for FLV test samples. The contract is [`sample-manifest-v1.schema.json`](./sample-manifest-v1.schema.json).

Small samples may be checked into `testdata/small/` only after rights and privacy review. Large, private, production-derived, or otherwise restricted media stays outside the repository; its manifest contains a basename, size, and SHA-256 but no local absolute path, credentials, or internal storage URL.

The [example manifest](./example-minimal-header-only.sample.json) illustrates the format and is explicitly excluded from corpus registration by its `example-` prefix. `metadata-only` does not by itself prevent registration: locally verified bytes can have a registered identity without being committed or cleared for redistribution.

## Current inventory (2026-09-05)

The directory contains 8 registered small-sample identities: 3 public external files, 3 generated media files, and 2 controlled damaged derivatives. The `largefile/` subdirectory contains 3 additional materialized container-stress fixtures (1/2/10 GiB, 100,000 tags each), not encoded media. All 11 hashes are distinct and all media remains local, outside version control.

The 3 external files have unreviewed redistribution rights and unreviewed privacy status. They do not count toward a cleared-real-corpus acceptance target. Identity registration, availability, and acceptance clearance are separate states; generated or derived samples never become real samples just because they are playable or damaged.

[`tools/validate_contracts.py`](../../tools/validate_contracts.py) recursively checks every registered manifest's schema, filename/ID, and hash uniqueness, but does not assert that media is present. [`tools/corpus.py`](../../tools/corpus.py) verifies local small-sample bytes and reports missing items explicitly; it does not recurse into `largefile/`. Use [`tools/largefile`](../../tools/largefile/main.go) for those stress fixtures. See the [validation record](../../docs/corpus-baseline-20260905.md) for provenance, results, limitations, and the unresolved cancellation outlier.

## Registration rules

- Use one `<id>.sample.json` file per sample. `id` is a stable lowercase slug and must not be recycled for different bytes.
- Hash the exact bytes with SHA-256. A changed hash is a new sample identity or a reviewed manifest revision; never silently update it.
- Use a basename in `file.name`, never a path. Do not record user names, machine names, signed URLs, tokens, customer names, or internal shares.
- Keep `originClass` (`real`, `generated`, or `derived`) independent from `validity` (`valid`, `damaged`, or `edge-case`). This supports the Draft 3 corpus targets without misclassifying generated damaged inputs.
- `rights.redistribution` applies to the media sample, not to MuxRay. MuxRay's project license remains undecided. `approved-for-repository` requires retained evidence; `unreviewed`, `testing-only`, and `prohibited` samples must remain metadata-only.
- A repository sample must also have privacy status `no-sensitive-data` or `sanitized`. Sanitization notes must not reproduce the sensitive values removed.
- `expected` is the minimum independently reviewable oracle. Put exhaustive field-by-field results in a Golden report later, but keep expected scan completion, header facts, tag count, and diagnostic code counts here.
- Diagnostic expectations use stable codes from [`docs/diagnostics-v1.md`](../../docs/diagnostics-v1.md). Do not match human messages.
- Keep `features` short and factual, using lowercase slugs such as `header-only`, `audio-tag`, `extended-timestamp`, `nonzero-previous-tag-size`, or `truncated-payload`.

## Storage meanings

| Value | Meaning |
|---|---|
| `repository` | Exact media bytes are committed in the test corpus after rights/privacy approval. |
| `metadata-only` | Bytes are deliberately absent; an authorized maintainer may hold them elsewhere. |
| `not-collected` | Candidate is described, but the exact hashed artifact is not currently available to the project. |

Manifests do not grant access to absent media. A storage transition to `repository` is a separate reviewed change.

## Validation checklist

Before treating a manifest as registered:

1. Validate it against the v1 schema.
2. Recompute `file.sizeBytes` and `file.sha256` from the exact artifact.
3. Run `muxray inspect` with the corpus-standard limits.
4. Confirm every `expected` fact and diagnostic count.
5. Record the actual rights/privacy review state and retain evidence outside the public manifest where necessary. Unreviewed artifacts may be identity-registered for local investigation but must not be counted as cleared acceptance samples.
6. Ensure the hash is unique across all registered manifests.
