"""Create a deterministic ADR-033 long-index fixture in a new absolute directory.

Complete FLV framing and AAC packet headers are supplied, not playable media.
No product parser is imported. A separate fixed-field walk checks written bytes.
"""

import argparse
import hashlib
import json
from pathlib import Path


TAG_COUNT = 10_000
REGRESSION_TAG = 8192
HEADER = bytes.fromhex("464c5601040000000900000000")
CONFIG = bytes.fromhex("af001210")
RAW = bytes.fromhex("af01aabb")
NAME = "time-search-long.flv"


def timestamp(tag_id):
    return (tag_id - 1) * 20 - 100 if tag_id == REGRESSION_TAG else tag_id * 20


def make_data():
    data = bytearray(HEADER)
    for tag_id in range(TAG_COUNT):
        dts = timestamp(tag_id)
        payload = CONFIG if tag_id == 0 else RAW
        data.extend(b"\x08" + len(payload).to_bytes(3, "big"))
        data.extend((dts & 0xffffff).to_bytes(3, "big") + bytes([dts >> 24]))
        data.extend(b"\0\0\0" + payload)
        data.extend((11 + len(payload)).to_bytes(4, "big"))
    return bytes(data)


def inspect_written(path):
    """Read the serialized fields and derive comparisons independently of writer metadata."""
    data = path.read_bytes()
    if data[:13] != HEADER:
        raise ValueError("unexpected FLV header")
    offset, tag_id, previous = 13, 0, None
    selected, nonstandard = [], []
    while offset < len(data):
        header = data[offset:offset + 11]
        if len(header) != 11 or header[0] != 8 or header[8:11] != b"\0\0\0":
            raise ValueError(f"unexpected Tag header at {offset}")
        size = int.from_bytes(header[1:4], "big")
        dts = int.from_bytes(header[4:7], "big") | header[7] << 24
        end = offset + 11 + size
        payload = data[offset + 11:end]
        if size != 4 or payload != (CONFIG if tag_id == 0 else RAW):
            raise ValueError(f"unexpected AAC packet at Tag {tag_id}")
        if len(data[end:end + 4]) != 4 or int.from_bytes(data[end:end + 4], "big") != 11 + size:
            raise ValueError(f"invalid PreviousTagSize at Tag {tag_id}")
        delta = None if previous is None else dts - previous[1]
        row = {"tagId": str(tag_id), "offset": str(offset),
               "payloadOffset": str(offset + 11), "dataSize": size,
               "typeCode": 8, "streamId": 0, "dts": str(dts),
               "previousTagId": None if previous is None else str(previous[0]),
               "previousDts": None if previous is None else str(previous[1]),
               "deltaMs": None if delta is None else str(delta),
               "payloadHex": payload.hex()}
        if tag_id in (0, 4095, 4096, 8191, 8192, 8193, TAG_COUNT - 1):
            selected.append(row)
        if delta is not None and delta != 20:
            nonstandard.append(row)
        previous = tag_id, dts
        offset, tag_id = end + 4, tag_id + 1
    if tag_id != 10_000 or len(data) != 190_013:
        raise ValueError("unexpected fixture size or Tag count")
    observed = [(row["tagId"], row["deltaMs"]) for row in nonstandard]
    if observed != [("8192", "-100"), ("8193", "140")]:
        raise ValueError(f"unexpected nonstandard deltas: {observed}")
    return {"name": path.name, "bytes": len(data),
            "sha256": hashlib.sha256(data).hexdigest(), "tagCount": tag_id,
            "scanComplete": True, "tags": selected,
            "nonstandardDeltas": nonstandard}


def write_fixtures(output):
    output.mkdir(parents=True, exist_ok=False)
    path = output / NAME
    with path.open("xb") as target:
        target.write(make_data())
    facts = inspect_written(path)
    manifest = {"format": "muxray-time-search-fixtures-v1", "files": [facts],
                "search": {"start": "0", "batchTagLimit": 4096, "rowLimit": 64,
                           "gapThresholdMs": "1000", "mode": "all",
                           "expectedInspectedRanges": [["0", "4096"], ["4096", "8192"],
                                                       ["8192", "10000"]],
                           "expectedHitTagIds": ["8192"],
                           "gapThreshold100HitTagIds": ["8192", "8193"]},
                "scope": "Synthetic FLV framing and AAC packet headers; raw DTS observations only; no playback/decoding claim"}
    with (output / "fixtures.json").open("x", encoding="utf-8", newline="\n") as target:
        json.dump(manifest, target, indent=2)
        target.write("\n")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path)
    args = parser.parse_args()
    if not args.output_dir.is_absolute():
        parser.error("Use an absolute, new output directory.")
    manifest = write_fixtures(args.output_dir)
    print(json.dumps({"outputDir": str(args.output_dir),
                      "files": [{key: row[key] for key in ("name", "bytes", "sha256", "tagCount")}
                                for row in manifest["files"]]}, indent=2))


if __name__ == "__main__":
    main()
