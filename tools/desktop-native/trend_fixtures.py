"""Create deterministic ADR-018 trend fixtures in a new absolute directory.

Only FLV framing and small codec headers are provided, not playable media.
The manifest records source-order metadata and bounded same-track predecessors.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct

from hex_fixtures import HEADER, tag_header
from summary_fixtures import avcc, fixtures as summary_fixtures, video


CONTEXT_LIMIT = 4096
PAGE_LIMIT = 512


class Fixture:
    def __init__(self):
        self.data = bytearray(HEADER)
        self.tags = []
        self.windows = []

    def add(self, kind, dts, payload=b"", *, stream_id=0, filtered=False,
            previous=None, missing=False, declared=None):
        size = len(payload) if declared is None else declared
        state = "missing" if missing else "valid" if previous in (None, 11 + size) else "invalid"
        self.tags.append({"tagId": str(len(self.tags)), "offset": str(len(self.data)),
                          "typeCode": kind, "streamId": stream_id, "dts": str(dts),
                          "dataSize": size, "availableDataSize": len(payload),
                          "payloadComplete": size == len(payload), "filtered": filtered,
                          "previousTagSizeState": state})
        header = bytearray(tag_header(kind | (32 if filtered else 0), size, dts))
        header[8:11] = stream_id.to_bytes(3, "big")
        self.data.extend(header + payload)
        if not missing:
            self.data.extend(struct.pack(">I", 11 + size if previous is None else previous))

    def window(self, name, start, count=256):
        self.windows.append({"name": name, "request": {"start": str(start), "count": count},
                             "expected": trend_page(self.tags, start, count)})


def trend_page(rows, start, count):
    """Reference expectation; tests independently inspect bytes and predecessors."""
    if not 0 <= start <= len(rows) or not 1 <= count <= PAGE_LIMIT:
        raise ValueError("Invalid trend window")
    end = min(start + count, len(rows))
    points = rows[start:end]
    keys = {(row["typeCode"], row["streamId"]) for row in points}
    previous, context_start = {}, start
    while keys and context_start > 0 and start - context_start < CONTEXT_LIMIT:
        context_start -= 1
        row = rows[context_start]
        key = row["typeCode"], row["streamId"]
        if key in keys:
            previous[key] = row
            keys.remove(key)
    result = []
    for row in points:
        key = row["typeCode"], row["streamId"]
        before = previous.get(key)
        result.append({**row, "previousTagId": before["tagId"] if before else None,
                       "previousDts": before["dts"] if before else None,
                       "deltaMs": str(int(row["dts"]) - int(before["dts"])) if before else None,
                       "context": "found" if before else "start" if context_start == 0 else "budget_exhausted"})
        previous[key] = row
    return {"bounds": {"start": str(start), "next": str(end), "total": str(len(rows)), "limit": count},
            "contextStart": str(context_start), "contextScanned": start - context_start,
            "points": result}


def fixtures():
    mixed = Fixture()
    original, records, _ = summary_fixtures()["summary-mixed.flv"]
    for row in records:
        at = row["payloadOffset"]
        mixed.add(row["tagType"], row["dts"], original[at:at + row["payloadBytes"]])
    assert bytes(mixed.data) == original
    for index in range(20, 300):
        kind, dts = (8 if index % 2 == 0 else 9), index * 20
        payload = bytes.fromhex("af01aabb") if kind == 8 else video(length_size=2)
        options = {}
        if index in (254, 255, 256):
            dts = 5000
        elif index == 257:
            dts = 4900
        elif index == 258:
            dts = 7001
        elif index in (259, 261):
            kind, dts, payload = 8, 40 if index == 259 else 0, bytes.fromhex("af001210")
            options["stream_id"] = 1
        elif index == 260:
            dts = 4294967295
        elif index == 262:
            kind, dts, payload = 31, 0, b"\x01\x02"
            options["filtered"] = True
        elif index == 263:
            kind, dts, payload = 18, 0, b""
        elif index == 264:
            dts = 0
        mixed.add(kind, dts, payload, **options)
    mixed.window("first-256", 0)
    mixed.window("cross-window-and-streams", 256)
    mixed.window("small-lookback-stops-early", 20, 2)
    mixed.window("api-max-512", 0, 512)
    mixed.window("eof", 300)

    sparse = Fixture()
    for index in range(4354):
        kind, dts, payload = 18, 0, b""
        if index in (0, 4098, 4099):
            kind, dts = 8, {0: 0, 4098: 10, 4099: 20}[index]
            payload = bytes.fromhex("af001210" if index == 0 else "af01aabb")
        elif index in (1, 4097, 4100):
            kind, dts = 9, {1: 0, 4097: 100, 4100: 90}[index]
            payload = avcc(4) if index == 1 else video()
        elif index == 4101:
            kind = 31
        sparse.add(kind, dts, payload)
    sparse.window("budget-and-nearest-predecessors", 4097)
    sparse.window("exact-4096-lookback-found", 4097, 1)
    sparse.window("older-than-budget-unknown", 4098, 1)
    sparse.window("last-tag", 4353)
    sparse.window("eof", 4354)

    structures = Fixture()
    for index, kind in enumerate([18, 8, 31, 9, 8, 9, 8]):
        structures.add(kind, index * 10, b"\x01\x02", stream_id=16777215 if index == 4 else 0,
                       filtered=index == 2, previous=99 if index in (1, 3) else None,
                       missing=index == 6)
    structures.window("structural-metadata", 0)
    truncated = Fixture()
    truncated.add(18, 0)
    truncated.add(8, 12000, bytes.fromhex("af00"), declared=10, missing=True)
    truncated.window("partial-payload", 0)
    empty = Fixture()
    empty.window("empty-eof", 0)
    return {"trends-mixed.flv": mixed, "trends-sparse.flv": sparse,
            "trends-structures.flv": structures, "trends-truncated.flv": truncated,
            "trends-empty.flv": empty}


def write_fixtures(output):
    output.mkdir(parents=True, exist_ok=False)
    files = []
    for name, fixture in fixtures().items():
        data = bytes(fixture.data)
        with (output / name).open("xb") as stream:
            stream.write(data)
        files.append({"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                      "tagCount": len(fixture.tags), "tags": fixture.tags, "windows": fixture.windows})
    manifest = {"format": "muxray-desktop-trend-fixtures-v1", "contextLimit": CONTEXT_LIMIT,
                "pageLimit": PAGE_LIMIT, "uiPageSize": 256, "files": files,
                "scope": "Source-order FLV metadata and bounded same-(typeCode,streamId) DTS deltas; no decoding or playback claim"}
    with (output / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path)
    args = parser.parse_args()
    if not args.output_dir.is_absolute():
        parser.error("Use an absolute, new output directory.")
    manifest = write_fixtures(args.output_dir)
    print(json.dumps({"files": [{key: file[key] for key in ("name", "bytes", "sha256", "tagCount")}
                                for file in manifest["files"]]}, indent=2))


if __name__ == "__main__":
    main()
