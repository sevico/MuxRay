# Codec regression inputs and native acceptance

These tools preserve the H.264 VUI and MP3 depth regression outside a developer's
cache, together with PPS/slice-prefix v3 regression. They generate small synthetic
media locally, derive expectations from FFmpeg traces and source bytes, and
compare the actual CLI/native results.
Generated media are not stored in Git. No network samples or user media are used.

From the repository root, with Python 3, `jsonschema`, FFmpeg/ffprobe including
`libx264`, `libmp3lame` and `trace_headers` installed:

```sh
python3 -B -m unittest discover -s tools/codec-regression -p '*_test.py' -v
python3 -B tools/codec-regression/generate.py --output-dir "$PWD/.cache/codec-run/samples"
GOTOOLCHAIN=go1.27.0 go build -o "$PWD/.cache/codec-run/muxray" ./cmd/muxray
python3 -B tools/codec-regression/verify.py \
  --input-dir "$PWD/.cache/codec-run/samples" \
  --cli "$PWD/.cache/codec-run/muxray" --output-dir "$PWD/.cache/codec-run/cli"
python3 -B tools/codec-regression/verify_v3.py \
  --input-dir "$PWD/.cache/codec-run/samples" \
  --cli "$PWD/.cache/codec-run/muxray" --output-dir "$PWD/.cache/codec-run/cli-v3"
```

Each output directory must be new. `--ffmpeg` and `--ffprobe` accept alternative
executables. All command arguments, exit codes, stdout/stderr, encoder/tool
versions, sample sizes and hashes are recorded. Failures preserve evidence in
`result.json` and command logs. Hashes identify inputs within the run; they are
not fixed expected hashes across encoder versions. Encoding uses one thread and
deterministic test patterns, while source-byte and trace-derived expectations
accommodate valid encoding differences.

For real Linux desktop acceptance, first build
[`desktop/build-production.sh`](../../desktop/build-production.sh), then run with
the system Python containing the native smoke dependencies:

```sh
/usr/bin/python3 -B tools/codec-regression/native.py \
  --input-dir "$PWD/.cache/codec-run/samples" \
  --binary "$PWD/desktop/build/bin/MuxRay" --output-dir "$PWD/.cache/codec-run/native"
/usr/bin/python3 -B tools/codec-regression/native_v3.py \
  --input-dir "$PWD/.cache/codec-run/samples" \
  --binary "$PWD/desktop/build/bin/MuxRay" --output-dir "$PWD/.cache/codec-run/native-v3"
```

`native_v3.py` adds two separate rounds for PPS necessary fields and a B-frame
with multiple slices. It checks prefix values, contiguous source-bit links and
references into the earlier SPS/PPS Tag. `verify_v3.py` checks both generated
H.264 files against every supported trace field, including conditional-field
presence; missing context, truncated prefixes and invalid replacement configs
are separate container mutations. The scope ends at the bounded prefix.

`native.py` calls the existing
[`linux_smoke.py`](../desktop-native/linux_smoke.py) sequentially for six cases:
the original basic fixture, H.264 VUI/HRD, MP3 Xing, MP3 Info, truncated MP3, and
CRC match/mismatch. The basic case is included once. Each case owns its private
Xvfb/D-Bus session and memory-only GSettings environment. The helper retains real
GTK open/save, source-linked Hex, snapshot schema, unchanged source, FD release,
normal exit and process cleanup checks; the wrapper adds codec field assertions.
Application forced termination is a failure. Any teardown signals used for the
private Xvfb/D-Bus harness are recorded separately as `harnessCleanupSignals`.
It does not alter system accessibility settings. `--timeout-seconds` is a
per-case timeout (default 180, range 30–600). The Linux CI job runs the same entry
points and uploads JSON/log evidence even after failures.
The six-case v1/v2 gate stays independent. The default smoke's historical
two-byte IDR placeholder is replaced locally with a minimal complete ordinary
slice header through QP/deblocking fields; other Tags remain byte-for-byte unchanged.

## Evidence and scope

- `h264-sar-bt709-hrd.flv` requests SAR 4:3, BT.709 and CBR HRD. Every VUI field
  emitted by the installed FFmpeg trace is checked against the CLI, including
  the exact original source-bit segments around emulation-prevention bytes.
- Xing/Info inputs intentionally wrap an entire small MP3 stream into one FLV
  Tag. Independent header arithmetic supplies frame boundaries and metadata
  expectations. Declared stream totals remain distinct from physical frames.
- The truncated variant removes exactly 17 MP3 tail bytes and repairs the FLV
  envelope. Container parsing should succeed while deep MP3 inspection reports
  a partial result. It is excluded from decode-success checks.
- The CRC fixture contains two zero-filled structural frames: a known matching
  CRC and a one-bit corrupted stored CRC. Polynomial long division independently
  supplies expected CRCs. This is not a valid-decoded-audio sample. Naturally
  encoded VBRI and audio main-data validity are outside this suite.
- `h264-bframes-multislice.flv` is also generated and decoded successfully. Its
  trace must contain B slices and multiple slice headers in a picture packet.
  The v3 verifier compares all PPS/slice-prefix fields and their original bit
  mappings. The v1/v2 gate keeps its original inspection depth.

The stable generated `expectations.json` format has `schemaVersion: 1`, `sources`
(relative filename to size/hash), `flv`, `mp3`, `h264` and `h264BFrames`.
Both H.264 entries include `configurationTagId`, `configurationTagIds`, source
`tags`/`nals`, and ordered `traceBlocks`. Each block has `kind`, `tagId`,
`nalIndex`, `offset`, `bytes`, `type`, and `fields`. Each field retains its trace
name/value, encoded bits, `rbspBitStartIncludingNALHeader`, `bitLength`, and
`sourceBitSegments` (`byteOffset`, `bitOffset`, `bitLength`). Duplicate trace
field names remain separate ordered entries. No product parser generates this
oracle. The independent v3 validator consumes it without changing the
v1/v2 regression gate.

## MP3 side information and long AVC context

Generate the independent MPEG-1/2/2.5 mono/stereo corpus and inspect CLI v3:

```sh
/usr/bin/python3 -B tools/codec-regression/side_info_oracle.py \
  --output-dir "$PWD/.cache/codec-run/side-info-samples"
/usr/bin/python3 -B tools/codec-regression/verify_side_info.py \
  --input-dir "$PWD/.cache/codec-run/side-info-samples" \
  --cli "$PWD/.cache/codec-run/muxray" --output-dir "$PWD/.cache/codec-run/cli-side-info"
/usr/bin/python3 -B tools/codec-regression/native_context_side.py \
  --input-dir "$PWD/.cache/codec-run/samples" \
  --side-info-dir "$PWD/.cache/codec-run/side-info-samples" \
  --timeout-seconds 420 \
  --binary "$PWD/desktop/build/bin/MuxRay" --output-dir "$PWD/.cache/codec-run/native-context-side"
```

The corpus includes naturally encoded switched blocks and deliberate mutations
for truncated side information, invalid values, bad switched block type,
Tag-external history, impossible declared main-data length and CRC match/mismatch.
Unmodified generated audio is independently decoded with FFmpeg. The oracle
extracts fixed fields and source bits without using the product parser and
compares conditional presence, spans, dependency geometry and diagnostics.
The verifier also validates all three JSON versions and default/v1 equality.

The additional native entry point runs three MP3 cases and one long AVC case.
The AVC fixture retains real configuration and multi-slice B-frame payloads but
inserts many lightweight Tags to exercise three replay segments; it is a
context-replay fixture, not a claim of a natural long recording. Native snapshots
check monotonic completed boundaries, manual continuation, random-jump cache
reuse and original SPS/PPS source links. Every round retains the same isolated
GTK/WebKit, unchanged-source, export, FD-release and normal-exit checks.
The example allows 420 seconds for the long-context round's five GTK exports;
this harness deadline does not change the product's per-request parsing timeout.
The existing six-case gate accepts side-info display truncation only when the
explicit omitted-record count matches the retained frames and the first two
records are expanded. It still rejects unexplained truncation.

Use a fresh output directory per run. Generated media and local logs remain in
`.cache`; CI runs these same commands and uploads JSON/log evidence. Contract and
parser scope are defined in [ADR-025](../../docs/decisions/025-avc-checkpoints-mp3-side-info.md).

## MP3 frame browsing, AVC evidence and curve snapshots

After building both the CLI and production desktop, run the three additional
native rounds against the same generated inputs:

```sh
/usr/bin/python3 -B tools/codec-regression/native_browse_evidence.py \
  --input-dir "$PWD/.cache/codec-run/samples" \
  --side-info-dir "$PWD/.cache/codec-run/side-info-samples" \
  --binary "$PWD/desktop/build/bin/MuxRay" \
  --cli "$PWD/.cache/codec-run/muxray" \
  --output-dir "$PWD/.cache/codec-run/native-browse-evidence"
```

The MP3 round opens the 15-frame `mpeg1-stereo.flv`, visits frames 0, 1 and 2,
saves `mp3-frame-2.json`, then selects frame 14 and saves `snapshot.json`. Each
snapshot must expand exactly its selected side-information record while keeping
all frame summaries. Every encoded field, conditional presence, source span,
CRC status and same-Tag dependency is checked against the independent source-bit
oracle. Selection retains the default 64-frame / 1 MiB inspection limits; it
does not inspect frames outside the checked range or rebuild the audio reservoir.

Two H.264 rounds inspect a real multi-slice B-frame Tag and an IDR Tag. Saved
FFmpeg traces and original NAL bytes supply the expected declaration comparison,
slice-type set and evidence spans. IDR is an observed NAL type, and these checks
do not assert complete pictures or independent decodability. Existing PPS/slice
prefix verification also runs on each selected Tag.
The native flow clicks each available declaration, NAL, IDR and slice source
button, then saves its accessible UI tree between the initial and final snapshots. Each stage must show
the expected byte offset, bit offset and bit length, and its Hex bytes must match
the original source. `--avc-evidence-only` reruns these two rounds when a prior
MP3 round already supplies the browsing evidence.

Every round first loads the media summary and its current curve page. The v2/v3
snapshot must retain the selected metric, summary request identity, complete
page bounds and values. These small fixtures fit one 256-Tag window; independent
FLV framing, media payload sizes and monotonic DTS values supply bitrate and
declared-GOP aggregates. The actual CLI v2 summary is a separate cross-check.
The one-Tag MP3 sample correctly has no covered bitrate interval. CLI output is
never used as the codec evidence oracle.

`--cli` is required; `--timeout-seconds` defaults to 240 per round (30–600).
Prefer a short output path such as `.cache/cb/n3`: GTK/D-Bus helpers also create
Unix sockets under the private evidence directories, which have path limits.
The wrapper preserves the existing native resource checks, source hashes,
source-linked Hex, schema validation, FD release, normal application exit and
isolated-harness cleanup. Failures retain their logs and snapshots in the new
output directory. Its no-GUI mutation tests are included in the existing
`python3 -B -m unittest discover -s tools/codec-regression -p '*_test.py'` command.

## H.264 batch search and MP3 dependency sources

After building the production desktop, generate separate inputs and run all
nine native rounds:

```sh
/usr/bin/python3 -B tools/codec-regression/search_history_oracle.py \
  --output-dir "$PWD/.cache/sh-run/samples"
/usr/bin/python3 -B tools/codec-regression/native_search_history.py \
  --input-dir "$PWD/.cache/sh-run/samples" \
  --binary "$PWD/desktop/build/bin/MuxRay" \
  --output-dir "$PWD/.cache/sh-run/native" --timeout-seconds 420
```

The five MP3 rounds compare a merged stream, split Tags, intervening video/script
Tags, an audio barrier and missing history. They verify the physical dependency
window against original source bytes, click every source range in Hex and open
the source Tag/frame. The four H.264 rounds cover pagination, unknown evidence,
observed IDR with partial coverage and missing configuration. They compare every
result row and each current-page v3 export with an independent byte oracle,
revisit the previous page and check source/Tag navigation. The search results describe the inspected range; they do
not assert independent decodability or reconstruct MP3 reservoir consumption.

The independent `linux-search-history` job in
[`desktop.yml`](../../.github/workflows/desktop.yml) builds its own production
binary and generates these samples afresh, without extending the existing Linux
codec job. It runs the helper unit tests and all nine cases, checks the binary's
SHA-256 before and after acceptance, and uploads JSON/log/hash evidence even on
failure. Each case has a 420-second limit; the 90-minute job budget includes all
nine case limits, dependency installation and the production build. The native
wrapper also checks binary/source identity and the usual unchanged-source, FD
release, normal-exit and isolated-harness cleanup assertions in every round.
Use a new, short output directory for each local run. Optional repeated
`--case` arguments rerun selected cases; CI deliberately omits them so all nine
rounds run.

## Long search and controlled native cancellation

The same generator also writes a repeated-segment structural sample exceeding
64 batches. Run it separately with the fixed production binary:

```sh
/usr/bin/python3 -B tools/codec-regression/native_search_history.py \
  --input-dir "$PWD/.cache/sh-run/samples" \
  --binary "$PWD/desktop/build/bin/MuxRay" \
  --output-dir "$PWD/.cache/sh-run/long" --long \
  --case avc-long-pagination --timeout-seconds 1800
```

This traverses every batch, rewinds the full 64-entry history, checks that older
history is unavailable, and returns forward to the end. It preserves per-page
UI evidence and exports the last page in v3. Repeated segments are a bounded
index/navigation fixture, not an uninterrupted recording or decode-success claim.

For a deterministic real-UI cancel/close test, build a **separate acceptance
executable** using a new directory outside the repository:

```sh
/usr/bin/python3 -B tools/desktop-native/avc_search_gate.py \
  --repository "$PWD" --source "$PWD/.cache/sh-run/samples/avc-declarations.flv" \
  --output-dir /tmp/muxray-avc-cancel-001
/usr/bin/python3 -B tools/codec-regression/native_search_lifecycle.py \
  --input-dir "$PWD/.cache/sh-run/samples" \
  --gate-dir /tmp/muxray-avc-cancel-001 --action cancel \
  --output-dir "$PWD/.cache/sh-run/cancel" --timeout-seconds 420
```

Use another fresh build/evidence directory and `--action close` for
the close/reopen case. The overlay gates the first actual search ReadAt, with a
60-second safety release, and extends the search deadline to 90 seconds. The
production source and executable are unchanged. Hash-bound build plans and
PID/run/token receipts establish actual read entry, `context canceled` before
release, retained source ownership while blocked, and successful recovery or
close/reopen after release. Deadline or safety expiration fails acceptance.
Both flows finish with source-preservation, FD-release and normal-exit checks.
These controlled results are distinct from production latency measurements;
all runs use private D-Bus/Xvfb and memory-only GSettings.

## Native MP3 all-frame summary

After building a fixed production executable, run the full-frame summary UI
round in a new, short evidence directory:

```sh
/usr/bin/python3 -B tools/codec-regression/native_full_summary.py \
  --binary "$PWD/desktop/build/bin/MuxRay" \
  --output-dir "$PWD/.cache/fn-mp3-001" --timeout-seconds 600
```

The wrapper authors 300 physical Layer III frames in one FLV Tag. A sampling
frequency/bitrate change occurs at frame 150; a channel-mode change at frame 270
lies beyond the 256 retained summary events. Its independent raw-header oracle
checks that all 300 frames still contribute to counts, changes and exact sample
seconds. It drives the real Analyze button and MP3 event tab, visits and exports
all three retained pages (100, 100 and 56 events), and clicks frame 255's source
button to compare the Hex bytes with the original file. Current-page snapshot
v3 exports preserve the generic summary and event shapes.

Each round also verifies unchanged source/binary hashes, source FD release,
normal application exit and isolated-process cleanup. The launcher uses private
D-Bus/Xvfb and memory-only GSettings. The authored frame bodies are opaque; this
proves frame-boundary summaries and source navigation, not audio decoding, CRC
validity or reservoir reconstruction. It deliberately distinguishes the 300
observed frames from the 256 retained events and the separate 64-frame detail
inspection limit.

## Native full H.264 headers

Generate the extended fixtures first, then drive a fixed production executable
through fourteen isolated native rounds. Use short, fresh evidence directories
so GTK's Unix-domain socket paths remain within the platform limit:

```sh
/usr/bin/python3 -B tools/codec-regression/full_codec_oracle.py \
  --output-dir "$PWD/.cache/fh-samples-001"
/usr/bin/python3 -B tools/codec-regression/native_full_h264.py \
  --input-dir "$PWD/.cache/fh-samples-001" \
  --binary "$PWD/desktop/build/bin/MuxRay" \
  --output-dir "$PWD/.cache/fh-native-001" --timeout-seconds 420
```

The rounds inspect one PPS scaling-matrix fixture, seven PPS FMO map types
(0 through 6), five ordinary I/P/B/SP/SI slice headers, and one FMO slice-group
change cycle. Authored codewords supply an independent oracle for every encoded
field shown in the PPS prefix/extended and slice prefix/full-header trees,
including nested reference-list operations, weights, MMCO and scaling lists.
Checks compare exact values, source bit spans, field hierarchy and the final
header bit position. The four legacy PPS base identifiers/flags remain derived
native text; CLI detail v4 separately checks their complete bit mappings.

Each round clicks an actual new tail-field source button (`secondChromaQpIndexOffset`,
`sliceBetaOffsetDiv2` or `sliceGroupChangeCycle`), preserves the UI and exported
Hex selection, then exports the final selected-Tag snapshot. The wrapper
independently rechecks all saved snapshot hashes, schemas and source bytes. It
also verifies fixed binary/source identities, source FD release, normal exit and
owned-process cleanup through the existing private D-Bus/Xvfb, memory-only
GSettings launcher. Syntax fixtures have opaque slice bodies: these rounds prove
header inspection and source navigation, not decoded pictures, reference-list
application or reconstructed DPB state.

## Independent full-codec CLI regression

Build a fixed CLI and generate fresh inputs before checking detail v4 and
summary v3. The generator uses the same FFmpeg prerequisites as the baseline
codec suite. Keep each run in a new directory to preserve failures and hashes:

```sh
mkdir -p "$PWD/.cache/fc-run-001"
GOTOOLCHAIN=go1.27.0 go build -o "$PWD/.cache/fc-run-001/muxray" ./cmd/muxray
python3 -B tools/codec-regression/full_codec_oracle.py \
  --output-dir "$PWD/.cache/fc-run-001/samples"
python3 -B tools/codec-regression/verify_full_codec.py \
  --input-dir "$PWD/.cache/fc-run-001/samples" \
  --cli "$PWD/.cache/fc-run-001/muxray" \
  --output-dir "$PWD/.cache/fc-run-001/cli"
```

The H.264 checks cover two encoded FFmpeg streams and twelve authored syntax
fixtures: I/P/B/SP/SI headers, FMO map types 0–6 and PPS scaling lists. FFmpeg
`trace_headers` and authored codewords independently establish values, exact
source-bit mappings and the final header bit position. The checker also verifies
separate prefix/full RBSP read scopes and rejects stale PPS context after an SPS
dimension change and an 8-bit PPS initial QP outside its valid range. Authored
slice bodies are opaque; those fixtures establish syntax parsing, not decoding.

The MP3 checks derive physical frame boundaries from raw MPEG header bytes.
Merged, split and video-interleaved Tags must produce the same complete-frame
counts and exact sample-duration fractions. Later-frame parameter changes,
truncated bodies, free-format and invalid headers exercise partial results.
Separate low frame-attempt and retained-event limits verify that bounded event
storage does not stop aggregation. Twelve summary invocations compare counters,
event status/order, source spans and rates with the independent oracle.

Saved results include input, executable, schema and report hashes. These new
contract checks complement the baseline v1/v2/v3 codec suite and the native UI
rounds above; run the baseline separately when checking old export contracts.

## MP3 frame pages and AVC SEI v5

`paged_codec_oracle.py` authors five MP3 and thirteen SEI structural inputs with
independent raw-byte/bit expectations. It requires no downloaded media or
encoder. MP3 includes a 3,000-frame Tag exceeding 1 MiB, later parameter changes,
physical dependency windows, split/video-interleaved Tags and an AAC barrier.
SEI covers recovery points, UUID data/preview, unknown and extended type/size,
emulation prevention, truncation, syntax ranges and per-NAL/packet budgets.

```sh
python3 -B tools/codec-regression/paged_codec_oracle.py \
  --output-dir "$PWD/.cache/ps-samples"
GOTOOLCHAIN=go1.27.0 go build -trimpath -o "$PWD/.cache/ps-cli" ./cmd/muxray
python3 -B tools/codec-regression/verify_paged_codec.py \
  --input-dir "$PWD/.cache/ps-samples" --cli "$PWD/.cache/ps-cli" \
  --output-dir "$PWD/.cache/ps-cli-check"
/usr/bin/python3 -B tools/codec-regression/native_paged_codec.py \
  --input-dir "$PWD/.cache/ps-samples" --binary "$PWD/desktop/build/bin/MuxRay" \
  --output-dir "$PWD/.cache/ps-native" --timeout-seconds 600
```

The CLI checker validates opt-in AVC v5 schemas, values, source mapping and
legacy versions. MP3 CLI versions 1–3 keep their existing contract; pagination
is checked through document tests and real desktop snapshots. Eight native
rounds check summary frame 255 → page 192, 37 further page transitions → frame
2600 on page 2560, same-Tag/cross-Tag dependency source frames and Hex, SEI
recovery/UUID source segments and truncated/budget-limited reports. Each page
transition retains UI evidence; final snapshots are verified against the
independent source expectations. `--case` can select one native case for a
focused rerun. Failed output directories are preserved; use a fresh, short
absolute output path for each run because D-Bus socket paths have a size limit.

All native rounds use the shared isolated Linux helper and verify source/binary
hashes, exported snapshots, source descriptor release and normal process exit.
They do not validate audio/video decoding or Windows/macOS native behavior.
