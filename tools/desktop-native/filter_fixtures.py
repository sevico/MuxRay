"""Generate deterministic ADR-017 Tag-filter fixtures using only stdlib.

Use a new absolute output directory. Samples describe FLV framing, not playable
media. Query expectations use global source-order Tag IDs and inclusive DTS.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct

from hex_fixtures import HEADER, tag, tag_header
from summary_fixtures import fixtures as summary_fixtures


class Fixture:
    def __init__(self):
        self.data = bytearray(HEADER)
        self.tags = []
        self.queries = []

    def add(self, kind, dts, payload=b"", *, previous=None, missing=False,
            declared=None):
        size = len(payload) if declared is None else declared
        self.tags.append({"id": len(self.tags), "offset": len(self.data),
                          "typeCode": kind, "dts": dts, "dataSize": size,
                          "availableDataSize": len(payload),
                          "payloadComplete": size == len(payload),
                          "previousPresent": not missing,
                          "previousValid": not missing and previous in (None, 11 + size)})
        self.data.extend(tag_header(kind, size, dts) + payload)
        if not missing:
            self.data.extend(struct.pack(">I", 11 + size if previous is None else previous))

    def query(self, name, ids, *, kind="all", minimum=None, maximum=None,
              structure="all", pages=None):
        self.queries.append({"name": name, "filter": {"type": kind,
                             "dtsMin": None if minimum is None else str(minimum),
                             "dtsMax": None if maximum is None else str(maximum),
                             "structure": structure}, "ids": ids,
                             **({"pages": pages} if pages is not None else {})})


def fixtures():
    mixed = Fixture()
    data, records, _ = summary_fixtures()["summary-mixed.flv"]
    for record in records:
        at = record["payloadOffset"]
        mixed.add(record["tagType"], record["dts"], data[at:at + record["payloadBytes"]])
    assert bytes(mixed.data) == data
    mixed.query("audio", [0, 2, 4, 6, 8, 9, 12, 13, 14, 15, 16, 17], kind="audio")
    mixed.query("video", [1, 3, 5, 7, 10, 11], kind="video")
    mixed.query("script", [18], kind="script")
    mixed.query("other", [19], kind="other")
    mixed.query("audio-duplicate-20", [4, 6], kind="audio", minimum=20, maximum=20)
    mixed.query("regression-range", [4, 6, 8, 9, 10], minimum=10, maximum=30)
    mixed.query("no-match", [], minimum=300, maximum=400)

    ranges = Fixture()
    for kind, dts in [(8, 10000), (9, 10000), (18, 12000), (31, 11000),
                      (8, 9999), (9, 12001), (8, 10000), (9, 12000),
                      (8, 11000), (31, 4294967295), (18, 0), (9, 10000)]:
        ranges.add(kind, dts)
    ranges.query("inclusive-regressing-range", [0, 1, 2, 3, 6, 7, 8, 11], minimum=10000, maximum=12000)
    ranges.query("video-range", [1, 7, 11], kind="video", minimum=10000, maximum=12000)
    ranges.query("equal-bound-duplicates", [0, 1, 6, 11], minimum=10000, maximum=10000)
    ranges.query("large-unsigned-flv-dts", [9], minimum=4294967295, maximum=4294967295)
    ranges.query("zero-dts", [10], minimum=0, maximum=0)
    ranges.query("empty-gap", [], minimum=13000, maximum=13000)

    structures = Fixture()
    for index, kind in enumerate([18, 8, 9, 31, 8, 9, 8]):
        structures.add(kind, index * 10, b"\x01\x02", previous=99 if index in (1, 3) else None,
                       missing=index == 6)
    structures.query("bad-previous-present-only", [1, 3], structure="previous_size_invalid")
    structures.query("bad-audio-previous", [1], kind="audio", structure="previous_size_invalid")
    structures.query("missing-previous", [6], structure="previous_size_missing")
    structures.query("all-payloads-complete", [], structure="payload_incomplete")

    truncated = Fixture()
    truncated.add(18, 0)
    truncated.add(8, 12000, bytes.fromhex("af00"), declared=10, missing=True)
    truncated.query("payload-incomplete", [1], structure="payload_incomplete")
    truncated.query("missing-previous", [1], structure="previous_size_missing")
    truncated.query("missing-is-not-invalid", [], structure="previous_size_invalid")
    truncated.query("combined-range-structure", [1], kind="audio", minimum=12000, maximum=12000, structure="payload_incomplete")

    sparse = Fixture()
    for index in range(8305):
        kind = 8 if index in (4500, 5000, 8200) else 9 if index == 8304 else 18
        sparse.add(kind, {4500: 10000, 5000: 12000, 8200: 11000, 8304: 11000}.get(index, 0))
    sparse.query("sparse-audio", [4500, 5000, 8200], kind="audio", pages=[
        {"start": "0", "next": "4096", "scanned": 4096, "ids": [], "exhausted": False},
        {"start": "4096", "next": "8192", "scanned": 4096, "ids": [4500, 5000], "exhausted": False},
        {"start": "8192", "next": "8305", "scanned": 113, "ids": [8200], "exhausted": True},
    ])
    sparse.query("no-other-across-windows", [], kind="other")

    multipage = Fixture()
    for index in range(305):
        multipage.add(8 if index % 2 == 0 else 9, 10000 if index % 3 else 12000)
    multipage.query("153-audio", list(range(0, 305, 2)), kind="audio", pages=[
        {"start": "0", "next": "199", "scanned": 199, "ids": list(range(0, 199, 2)), "exhausted": False},
        {"start": "199", "next": "305", "scanned": 106, "ids": list(range(200, 305, 2)), "exhausted": True},
    ])
    multipage.query("all-305", list(range(305)))
    return {"summary-mixed.flv": mixed, "filter-ranges.flv": ranges,
            "filter-structures.flv": structures, "filter-truncated.flv": truncated,
            "filter-sparse.flv": sparse, "filter-multipage.flv": multipage,
            "filter-empty.flv": Fixture()}


def write_fixtures(output):
    output.mkdir(parents=True, exist_ok=False)
    files = []
    for name, fixture in fixtures().items():
        data = bytes(fixture.data)
        with (output / name).open("xb") as stream:
            stream.write(data)
        files.append({"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                      "tagCount": len(fixture.tags), "tags": fixture.tags, "queries": fixture.queries})
    manifest = {"format": "muxray-desktop-filter-fixtures-v1", "scanLimit": 4096,
                "pageLimit": 100, "files": files,
                "scope": "Synthetic FLV framing with inclusive raw DTS and source-order global Tag IDs; no decoder or playback claims"}
    with (output / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    return manifest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    if not args.output_dir.is_absolute():
        parser.error("Use an absolute, new output directory.")
    manifest = write_fixtures(args.output_dir)
    print(json.dumps({"files": [{k: f[k] for k in ("name", "bytes", "sha256", "tagCount")}
                                for f in manifest["files"]]}, indent=2))


if __name__ == "__main__":
    main()
