"""Create new, deterministic AVC desktop fixtures without external media.

The optional large fixture physically writes 64 MiB to 4096 MiB of filler;
it does not seek or create a sparse file. No fixture promises playable video.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct


HEADER = b"FLV\x01\x01\0\0\0\x09\0\0\0\0"
# Existing core regression configurations, including emulation-prevention bytes.
BASELINE = bytes.fromhex("0142c00bffe100166742c00bd902c4ec0440000003004000000c83c50a9201000568cb83cb20")
HIGH = bytes.fromhex("0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800")


def tag_header(kind: int, size: int, dts: int = 0) -> bytes:
    return (bytes([kind]) + size.to_bytes(3, "big")
            + (dts & 0xFFFFFF).to_bytes(3, "big") + bytes([dts >> 24]) + b"\0\0\0")


def tag(kind: int, payload: bytes, dts: int = 0) -> bytes:
    return tag_header(kind, len(payload), dts) + payload + struct.pack(">I", 11 + len(payload))


def configuration(width: int, high: bool = False) -> bytes:
    data = bytearray(HIGH if high else BASELINE)
    data[4] = 0xFC | (width - 1)
    return b"\x17\0\0\0\0" + data


def nalu(width: int, units: list[bytes], cts: int = 0) -> bytes:
    return (b"\x17\x01" + (cts & 0xFFFFFF).to_bytes(3, "big")
            + b"".join(len(unit).to_bytes(width, "big") + unit for unit in units))


def small_fixtures() -> dict[str, tuple[bytes, list[dict]]]:
    examples = [
        (9, nalu(1, [b"\x65\x88"], -12), 5, "missing context; CTS -12, PTS -7"),
        (9, configuration(1), 0, "baseline avcC, NAL width 1; SPS has EPB"),
        (9, nalu(1, [b"\x65\x88", b"\x06\x80"], -12), 40, "width 1 from Tag 1; IDR and SEI; PTS 28"),
        (9, configuration(2, True), 0, "high profile avcC, NAL width 2"),
        (9, nalu(2, [b"\x41\x9a", b"\x06\x80"], -100), 80, "width 2 from Tag 3; PTS -20"),
        (9, b"\x17\0\0\0\0\x01", 0, "truncated avcC; not a usable length declaration"),
        (9, nalu(2, [b"\x65\x88"]), 100, "skips unusable Tag 5; width 2 from Tag 3"),
        (9, configuration(4), 0, "switch to width 4"),
        (9, nalu(4, [b"\x65\x88", b"\x41\x9a"], 12), 120, "width 4 from Tag 7; PTS 132"),
        (9, b"\x17\x01\0\0\0\0\0\0\x14\x65\x88", 160, "NAL declares 20 bytes, only 2 remain"),
        (9, b"\x17\x02\0\0\0", 0, "end of sequence"),
        (9, b"\x12\0", 0, "non-AVC video; unsupported"),
        (9, nalu(4, [b"\x65"] * 300), 200, "NAL parser and field view budgets"),
    ]
    data = bytearray(HEADER)
    records = []
    for kind, payload, dts, purpose in examples:
        records.append({"id": len(records), "offset": len(data), "payloadOffset": len(data) + 11,
                        "payloadBytes": len(payload), "dts": dts, "purpose": purpose})
        data.extend(tag(kind, payload, dts))
    truncated = bytes(data) + tag_header(9, 10) + b"\x17\x01"
    truncated_records = records + [{"id": len(records), "offset": len(data),
                                   "payloadOffset": len(data) + 11, "declaredBytes": 10,
                                   "availableBytes": 2, "purpose": "incomplete container payload"}]
    budget = bytearray(HEADER)
    budget_records = []
    for _ in range(33):
        budget_records.append({"id": len(budget_records), "offset": len(budget), "purpose": "unusable avcC candidate"})
        budget.extend(tag(9, b"\x17\0\0\0\0\x01"))
    budget_records.append({"id": 33, "offset": len(budget), "purpose": "32-candidate lookup limit; not missing"})
    budget.extend(tag(9, nalu(4, [b"\x65\x88"]), 40))
    return {"avc-details.flv": (bytes(data), records), "avc-change.flv": (bytes(data), records),
            "avc-truncated.flv": (truncated, truncated_records),
            "avc-budget.flv": (bytes(budget), budget_records)}


def large_fixture(path: Path, size_mib: int) -> dict:
    digest = hashlib.sha256()
    offset, tag_id = 0, 0
    with path.open("xb") as stream:
        def write(data: bytes) -> None:
            nonlocal offset
            stream.write(data)
            digest.update(data)
            offset += len(data)

        write(HEADER)
        write(tag(9, configuration(4)))
        tag_id += 1
        remaining = size_mib << 20
        block = b"\0" * (1 << 20)
        while remaining:
            payload_size = min(0xFFFFFF, remaining)
            write(tag_header(18, payload_size))
            to_write = payload_size
            while to_write:
                count = min(len(block), to_write)
                write(block[:count])
                to_write -= count
            write(struct.pack(">I", 11 + payload_size))
            tag_id += 1
            remaining -= payload_size
        tail_offset = offset
        write(tag(9, nalu(4, [b"\x65\x88", b"\x06\x80"], -12), 1000))
        stream.flush()
    return {"name": path.name, "bytes": offset, "sha256": digest.hexdigest(),
            "physicallyWrittenFillerBytes": size_mib << 20, "tags": tag_id + 1,
            "tailTag": {"id": tag_id, "offset": tail_offset, "dts": 1000, "pts": 988,
                        "contextTagId": 0, "nalLengthSize": 4}}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path)
    parser.add_argument("--large-mib", type=int, default=0)
    args = parser.parse_args()
    if not args.output_dir.is_absolute():
        parser.error("Use an absolute, new output directory.")
    if args.large_mib and not 64 <= args.large_mib <= 4096:
        parser.error("--large-mib must be 0 or between 64 and 4096.")
    args.output_dir.mkdir(parents=True, exist_ok=False)
    files = []
    for name, (data, records) in small_fixtures().items():
        with (args.output_dir / name).open("xb") as stream:
            stream.write(data)
        files.append({"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), "tags": records})
    if args.large_mib:
        files.append(large_fixture(args.output_dir / "avc-large.flv", args.large_mib))
    manifest = {"format": "muxray-desktop-avc-fixtures-v1", "files": files}
    with (args.output_dir / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(json.dumps({"files": [{"name": f["name"], "bytes": f["bytes"], "sha256": f["sha256"]} for f in files]}, indent=2))


if __name__ == "__main__":
    main()
