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
two-byte IDR placeholder is replaced locally with a minimal valid ten-bit slice
prefix; other Tags in that basic fixture remain byte-for-byte unchanged.

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
