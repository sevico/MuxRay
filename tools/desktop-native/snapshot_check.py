"""Validate an exported desktop snapshot and compare its bounded byte evidence.

This reads only explicit local artifacts. --source is a small acceptance fixture
(at most 16 MiB), never an instruction to scan an arbitrary production corpus.
"""

import argparse
import hashlib
import json
from pathlib import Path

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[2]
MAX_SNAPSHOT = 2 << 20
MAX_FIXTURE = 16 << 20


def check_snapshot(report_path, source_path=None):
    if report_path.stat().st_size > MAX_SNAPSHOT:
        raise ValueError("Snapshot exceeds 2 MiB")
    raw = report_path.read_bytes()
    report = json.loads(raw.decode("utf-8"))
    schema = json.loads((ROOT / "docs/schemas/desktop-diagnostic-snapshot-v1.schema.json").read_text(encoding="utf-8"))
    Draft202012Validator(schema, format_checker=Draft202012Validator.FORMAT_CHECKER).validate(report)
    session_id = report["session"]["sessionId"]
    if report["selection"]["sessionId"] != session_id:
        raise ValueError("Selection belongs to a different session")
    for name in ("scanDiagnostics", "detail", "hex", "summary", "summaryPage", "trend"):
        section = report[name]
        if section is not None and section["sessionId"] != session_id:
            raise ValueError(f"{name} belongs to a different session")
    omitted = {item["section"] for item in report["omitted"]}
    for name in ("selectedTag", "detail", "hex", "summary", "summaryPage", "trend"):
        if (report[name] is None) != (name in omitted):
            raise ValueError(f"{name} omission does not match its presence")
    if report["selectedTag"] is not None and report["selectedTag"]["id"] != report["selection"]["selectedTagId"]:
        raise ValueError("Selected Tag identity mismatch")
    for name in ("detail", "hex", "summary"):
        if report[name] is not None and report[name]["requestId"] != report["selection"][name + "RequestId"]:
            raise ValueError(f"{name} request identity mismatch")
    if report["detail"] is not None and (report["selectedTag"] is None or report["detail"]["tagId"] != report["selectedTag"]["id"]):
        raise ValueError("Detail belongs to a different Tag")
    if report["hex"] is not None:
        section, window = report["hex"], report["hex"]["window"]
        offset, size = int(window["offset"]), int(window["sourceSize"])
        length = len(window["hex"]) // 2
        if (section["offset"] != window["offset"] or window["sourceSize"] != report["session"]["progress"]["sourceSize"]
                or offset > size or length != min(section["count"], size - offset)
                or window["eof"] != (offset + length == size)):
            raise ValueError("Hex window does not match its requested coverage")
    result = {"report": report_path.name, "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest(),
              "fileName": report["fileName"], "scanComplete": report["session"]["scan"]["complete"],
              "omitted": report["omitted"], "schemaValid": True}
    if source_path is not None:
        if source_path.stat().st_size > MAX_FIXTURE:
            raise ValueError("Use an acceptance fixture of at most 16 MiB")
        source = source_path.read_bytes()
        if report["fileName"] != source_path.name or int(report["session"]["progress"]["sourceSize"]) != len(source):
            raise ValueError("Source name or size differs from snapshot")
        tagged = []
        if report["selectedTag"] is not None:
            tagged.append((int(report["selectedTag"]["id"]), report["selectedTag"]))
        if report["trend"] is not None:
            tagged.extend((int(point["tagId"]), point) for point in report["trend"]["points"])
        offsets = fixture_tag_offsets(source, {tag_id for tag_id, _ in tagged})
        indexed = int(report["session"]["progress"]["tagCount"])
        for tag_id, tag in tagged:
            if tag_id >= indexed or offsets.get(tag_id) != int(tag["offset"]):
                raise ValueError(f"Tag {tag_id} does not match its source-order offset")
        if report["hex"] is not None:
            window = report["hex"]["window"]
            offset, data = int(window["offset"]), bytes.fromhex(window["hex"])
            if not 0 <= offset <= len(source) or data != source[offset:offset + len(data)]:
                raise ValueError("Hex bytes differ from source")
            if window["eof"] != (offset + len(data) == len(source)):
                raise ValueError("Hex EOF does not match its actual coverage")
            result["hex"] = {"offset": window["offset"], "bytes": len(data), "matchesSource": True}
        if report["selectedTag"] is not None:
            tag = report["selectedTag"]
            verify_tag(source, int(tag["offset"]), tag["typeCode"], int(tag["dts"]), tag["streamId"], tag["dataSize"], tag["availableDataSize"])
        if report["trend"] is not None:
            for point in report["trend"]["points"]:
                verify_tag(source, int(point["offset"]), point["typeCode"], int(point["dts"]), point["streamId"], point["dataSize"], point["availableDataSize"])
            result["trendPointsMatchingSource"] = len(report["trend"]["points"])
        result["sourceSHA256"] = hashlib.sha256(source).hexdigest()
    return result


def fixture_tag_offsets(source, wanted):
    """Walk the bounded fixture's headers; retain only requested identities.

    Payload and PreviousTagSize damage leave a final indexed header, while an
    incomplete header does not create a Tag. No payload is decoded here.
    """
    offsets = {}
    if len(source) < 9 or source[:3] != b"FLV":
        return offsets
    data_offset = int.from_bytes(source[5:9], "big")
    if data_offset < 9 or data_offset + 4 > len(source):
        return offsets
    offset, tag_id = data_offset + 4, 0
    while wanted and offset + 11 <= len(source):
        if tag_id in wanted:
            offsets[tag_id] = offset
            wanted.remove(tag_id)
        declared = int.from_bytes(source[offset + 1:offset + 4], "big")
        offset += 11 + declared + 4
        tag_id += 1
    return offsets


def verify_tag(source, offset, type_code, dts, stream_id, declared, available):
    if offset < 0 or offset + 11 > len(source):
        raise ValueError("Tag header is outside source")
    header = source[offset:offset + 11]
    actual = (header[0] & 31, int.from_bytes(header[4:7], "big") | header[7] << 24,
              int.from_bytes(header[8:11], "big"), int.from_bytes(header[1:4], "big"))
    if actual != (type_code, dts, stream_id, declared) or available != min(declared, len(source) - offset - 11):
        raise ValueError(f"Tag metadata differs from source at {offset}")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("report", type=Path)
    parser.add_argument("--source", type=Path)
    args = parser.parse_args()
    print(json.dumps(check_snapshot(args.report, args.source), ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
