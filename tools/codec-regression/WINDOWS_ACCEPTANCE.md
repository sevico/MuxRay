# Windows codec export recheck

`windows_codec_check.py` checks actual Windows exports against the existing
independent structural fixtures. It operates only on supplied local files. It
does not launch or operate an application, save a product snapshot, invent UI
evidence, or establish native process/resource acceptance.

Generate a fresh fixture directory with `codec_search_oracle.py --output-dir`.
That command refuses an existing directory. The six FLV samples are authored
syntax inputs, not recordings or decoder-success evidence. The original-source
hashes in `expectations.json` are checked before and after verification.

The helper reuses `codec_search_verify.page_rows`,
`desktop-native/snapshot_check.py` (including snapshot v4),
`verify_paged_codec.verify_mp3_native_page`, and the pure
`linux_smoke.check_mp3_navigation_stage` function. Importing the last module
does not run its Linux worker, import pyatspi/Xlib, or operate any UI.
Dependencies are the existing `tools/requirements-schema.txt` Python packages.

## Actual window sequence

Use a fixed, identified Windows build. For each source, open through the real
file chooser and wait for scanning to finish. The codec panel is labelled
“MP3 / SEI 批量检索”. Changing its kind or filter starts a new first batch;
do not press the restart button again unintentionally. Each “继续检索编解码器”
click advances exactly one batch. Export every completed batch through the
real save dialog, preserving the original JSON and a fresh complete UI state.
Do not replace a failed action by editing evidence or replay an uncertain click.

| Source | Kind / filters in order | Expected rows in successive pages | Additional observations |
| --- | --- | --- | --- |
| `mp3-late-change.flv` | MP3 / 参数变化 (`parameters`) | 0, 0, 1 | 1 indexed Tag; 1,024 / 1,024 / 952 inspected headers. Last row is Tag 0, frame 2600: jointStereo/2 channels → singleChannel/1 channel; 128 kbps and 44,100 Hz unchanged. |
| `mp3-bitrate-stops.flv` | MP3 / 全部、码率、停止 (`all`, `bitrate`, `stops`) | 7; 2; 5 | 6 Tags; 128→160→128 kbps. Stops are truncatedHeader, malformedHeader, unsupportedLayer, freeFormat, truncatedFrame. Stop rows must not offer frame detail. |
| `sei-pagination.flv` | SEI / 全部 (`all`) | 64, 64, 2 | 2 Tags; pending 66, 2, 0. Pages 1 and 2 read 0 source bytes and inspect 0 Tags. Before the sequence, optionally export a separate first page and restart; the new first request must be newer with identical rows. |
| `sei-cross-256.flv` | SEI / 全部、缓冲周期、图像时序 (`all`, `bufferingPeriod`, `pictureTiming`) | 1, 1; 1, 0; 0, 1 | 259 Tags; each filter scans 256 then 3 Tags. Tag 258 pictureTiming is complete/resolved, SPS 0 selected by bufferingPeriod. Empty first/last pages still require exports. |
| `sei-missing.flv` | SEI / 未知或不完整 (`unknown`) | 1 | 2 Tags; Tag 1 pictureTiming remains incomplete/missing, with timing_context_missing. No SPS/activation source button is invented. |
| `sei-budget.flv` | SEI / 全部、未知或不完整、图像时序 (`all`, `unknown`, `pictureTiming`) | 64, 1; 1; 1 | 2 Tags; the coverage row is incomplete/unknown with sei_message_limit and survives every filter. Unknown payloads alone are not missing timing context. |

For MP3 late change, click the complete row's frame button. Preserve the two
preparing exports and the final ready export: progress 1024 → 2048 → 2624,
replay pages 16 → 16 → 9, final page start 2560 and selected frame 2600.
Explicitly continue each preparing step. The final independent check covers
the original 64 frame headers and 32 selected side-info fields and mappings.
Do this before exploring other frame pages so the expected checkpoint history
is unambiguous. Search counting does not populate detail-page checkpoints.

After the last search page, click these source buttons and export each actual
Hex result. The retained search page must survive navigation. Offsets below
are decimal bytes; every target has bit offset 0.

| Source / target row identity | Button(s): byte offset, bit length |
| --- | --- |
| mp3-late-change / `0:2600:-:-` | 证据: 1084225, 3336 |
| mp3-bitrate-stops / `5:0:-:-` | 证据: 1491, 96 |
| sei-pagination / `1:-:2:1` | 证据: 492, 24 |
| sei-cross-256 / `258:-:0:0` | 证据: 4220, 80; SPS: 37, 184; 选择依据: 95, 24 |
| sei-missing / `1:-:0:0` | 证据: 93, 80 |
| sei-budget / `1:-:-:-` | 证据: 83, 1648 |

Source buttons have accessible names `定位编解码器 <identity> 证据/SPS/选择依据`.
The Tag and exact-frame controls are `从编解码器检索跳转 <identity> Tag/帧`.
The raw UI file should include the exact current coverage status and every
row/control name. Truncated UI evidence fails that check; it must not be
expanded from expectations or substituted with generated HTML.

## Read-only commands

Run from the repository root. Use absolute paths in the actual command. Repeat
`--snapshot` for all pages of one filter from zero, in original action order.
The final `--ui` belongs to the last supplied snapshot. Recheck each page as it
is exported if UI evidence is required for every page. Omit `--ui` only for an
explicitly export-only check; its result will say `checked: false`.

```powershell
python -B tools/codec-regression/windows_codec_check.py pages --input-dir C:\evidence\codec-fixtures --case sei-pagination --filter all --snapshot C:\evidence\page0.json --snapshot C:\evidence\page1.json --snapshot C:\evidence\page2.json --ui C:\evidence\page2-ui.txt --output C:\evidence\page2-check.json
python -B tools/codec-regression/windows_codec_check.py frame --input-dir C:\evidence\codec-fixtures --case mp3-late-change --snapshot C:\evidence\prepare0.json --snapshot C:\evidence\prepare1.json --snapshot C:\evidence\frame2600.json --ui C:\evidence\frame2600-ui.txt --output C:\evidence\frame-check.json
python -B tools/codec-regression/windows_codec_check.py source --input-dir C:\evidence\codec-fixtures --case sei-cross-256 --destination sps --snapshot C:\evidence\source-sps.json --ui C:\evidence\source-sps-ui.txt --output C:\evidence\source-sps-check.json
```

The helper validates schema, source hashes/bytes, exact oracle row values and
spans, sequence/cursor identities, coverage, budgets, zero-read pending pages,
and retained source targets. Raw UI checks cover named text only; their receipts
state this scope. It cannot prove native clicks, visual layout or process exit.
Output receipts use exclusive creation, including failures; choose a new path
for a new attempt. Synthetic unit tests are clearly separate from real exports:

```powershell
python -B -m unittest discover -s tools/codec-regression -p windows_codec_check_test.py -v
```

An overall native acceptance also requires separate evidence of the fixed
binary and process identity, actual open/save/source actions, source hashes
unchanged, media close releasing the source, parent exit code 0, and no owned
process left running. Do not infer those facts from this checker's `passed` flag.
Picker prototypes and default production builds need distinct build identities.
