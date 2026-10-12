"""Validate an exported desktop snapshot and compare its bounded byte evidence.

This reads only explicit local artifacts. Source checks default to 16 MiB;
larger local acceptance media requires --max-source-bytes (at most 1 GiB).
Source bytes are read in bounded chunks, never loaded as one media object.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import time

from jsonschema import Draft202012Validator

ROOT = Path(__file__).resolve().parents[2]
MAX_SNAPSHOT = 2 << 20
MAX_FIXTURE = 16 << 20
MAX_SOURCE = 1 << 30
MAX_TAG_VISITS = 1_000_000
MAX_SOURCE_SECONDS = 120


class BoundedSource:
    """Seekable byte slices for the oracle, plus streaming identity validation."""

    def __init__(self, path, limit):
        if not MAX_FIXTURE <= limit <= MAX_SOURCE:
            raise ValueError("Source byte limit must be between 16 MiB and 1 GiB")
        self.path, self.limit = path, limit
        self.stream = None

    def __enter__(self):
        self.stream = self.path.open("rb")
        try:
            self.before = os.fstat(self.stream.fileno())
            if self.before.st_size > self.limit:
                raise ValueError("Source exceeds byte limit; explicitly raise --max-source-bytes for larger acceptance media")
        except BaseException:
            self.stream.close()
            raise
        self.deadline = time.monotonic() + MAX_SOURCE_SECONDS
        return self

    def __exit__(self, *args):
        self.stream.close()

    def __len__(self):
        return self.before.st_size

    def check_deadline(self):
        if time.monotonic() >= self.deadline:
            raise ValueError("Source validation exceeded its cooperative time budget")

    def __getitem__(self, key):
        if not isinstance(key, slice):
            raise TypeError("Use bounded source slices")
        start, end, step = key.indices(len(self))
        if step != 1 or end - start > 1 << 20:
            raise ValueError("Source read exceeds the 1 MiB buffer budget")
        self.check_deadline()
        self.stream.seek(start)
        data = self.stream.read(max(0, end - start))
        self.check_deadline()
        if len(data) != max(0, end - start):
            raise ValueError("Source changed or returned a short read")
        return data

    def digest(self):
        self.stream.seek(0)
        digest, total = hashlib.sha256(), 0
        while True:
            self.check_deadline()
            block = self.stream.read(1 << 20)
            if not block:
                break
            total += len(block)
            if total > self.limit:
                raise ValueError("Source grew beyond its byte limit")
            digest.update(block)
        after = os.fstat(self.stream.fileno())
        identity = lambda stat: (stat.st_dev, stat.st_ino, stat.st_size, stat.st_mtime_ns)
        if total != len(self) or identity(after) != identity(self.before) or identity(self.path.stat()) != identity(self.before):
            raise ValueError("Source identity changed during validation")
        self.check_deadline()
        return digest.hexdigest()


def check_snapshot(report_path, source_path=None, max_source_bytes=MAX_FIXTURE):
    if report_path.stat().st_size > MAX_SNAPSHOT:
        raise ValueError("Snapshot exceeds 2 MiB")
    raw = report_path.read_bytes()
    report = json.loads(raw.decode("utf-8"))
    version = report.get("schemaVersion")
    if type(version) is not int or version not in (1, 2, 3):
        raise ValueError("Unsupported snapshot version")
    schema = json.loads((ROOT / f"docs/schemas/desktop-diagnostic-snapshot-v{version}.schema.json").read_text(encoding="utf-8"))
    Draft202012Validator(schema, format_checker=Draft202012Validator.FORMAT_CHECKER).validate(report)
    session_id = report["session"]["sessionId"]
    if report["selection"]["sessionId"] != session_id:
        raise ValueError("Selection belongs to a different session")
    sections = (("mediaCurvePage",) if version >= 2 else ()) + (("avcSearchPage",) if version >= 3 else ())
    for name in ("scanDiagnostics", "detail", "hex", "summary", "summaryPage", "trend") + sections:
        section = report[name]
        if section is not None and section["sessionId"] != session_id:
            raise ValueError(f"{name} belongs to a different session")
    omitted = {item["section"] for item in report["omitted"]}
    for name in ("selectedTag", "detail", "hex", "summary", "summaryPage", "trend") + sections:
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
    if version >= 2 and report["mediaCurvePage"] is not None:
        check_curve_page(report)
    if version >= 3 and report["avcSearchPage"] is not None:
        check_avc_search_page(report)
    result = {"report": report_path.name, "bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest(),
              "fileName": report["fileName"], "scanComplete": report["session"]["scan"]["complete"],
              "omitted": report["omitted"], "schemaValid": True}
    if source_path is not None:
        result.update(check_source(report, source_path, max_source_bytes))
    return result


def check_avc_search_page(report):
    response, query = report["avcSearchPage"], report["selection"].get("avcSearch")
    if not query or response["requestId"] != query["requestId"] or response["state"] != "ready" or response["error"] is not None:
        raise ValueError("AVC search request identity or ready state mismatch")
    page = response["page"]
    start, end, total = (int(page[key]) for key in ("start", "next", "total"))
    if (page["start"] != query["start"] or page["filter"] != query["filter"] or
            total != int(report["session"]["progress"]["tagCount"]) or
            page["scanComplete"] != report["session"]["scan"]["complete"] or
            not 0 <= start <= end <= total or (end == start and end != total) or
            page["scanned"] != end - start or end - start > 256 or
            page["exhausted"] != (end == total) or int(page["bytesRead"]) > 1048576 or len(page["rows"]) > 64):
        raise ValueError("AVC search coverage or read budget mismatch")
    previous = start - 1
    for row in page["rows"]:
        tag = int(row["tagId"])
        if not previous < tag < end:
            raise ValueError("AVC search rows repeat, reorder or exceed coverage")
        previous = tag
        declared, observed = row["declaredFrameType"], row["observedIDR"]
        relation = ("undetermined" if declared is None else "other_declaration" if declared not in (1, 2) else "undetermined" if observed == "unknown" else
                    ("keyframe_" if declared == 1 else "inter_") + ("with_idr" if observed == "yes" else "without_idr"))
        if (row["declarationRelation"] != relation or (observed == "unknown" and not row["reason"]) or
                page["filter"] == "unknown" and observed != "unknown" or
                page["filter"] == "mismatches" and relation not in ("keyframe_without_idr", "inter_with_idr")):
            raise ValueError("AVC search evidence relation or filter mismatch")


def check_curve_page(report):
    response, selection = report["mediaCurvePage"], report["selection"]
    query = selection.get("mediaCurve")
    summary = report["summary"]
    if not query or not summary or response["requestId"] != selection["summaryRequestId"] or response["requestId"] != summary["requestId"]:
        raise ValueError("Curve summary identity mismatch")
    page, overview = response["page"], summary["overview"]
    if page["indexedTags"] != overview["indexedTags"] or page["scanComplete"] != overview["scanComplete"] or page["complete"] != overview["complete"]:
        raise ValueError("Curve summary coverage mismatch")
    bounds = page["bounds"]
    start, next_page, total = (int(bounds[key]) for key in ("start", "next", "total"))
    indexed = int(page["indexedTags"])
    if (bounds["start"] != query["start"] or bounds["limit"] != query["count"] or page["windowTags"] != 256 or
            total != (indexed + 255) // 256 or not 0 <= start <= total or next_page != min(start + query["count"], total) or
            len(page["points"]) != next_page - start):
        raise ValueError("Curve page coordinates mismatch")
    for ordinal, point in enumerate(page["points"], start):
        first, end = int(point["startTag"]), int(point["nextTag"])
        if int(point["index"]) != ordinal or first != ordinal * 256 or end != min(first + 256, indexed) or int(point["unclassifiedTags"]) > end - first:
            raise ValueError("Curve point coordinates mismatch")
        for codec in ("audio", "video", "mp3"):
            rate = point[codec]
            size, duration = int(rate["coveredBytes"]), int(rate["coveredIntervalMs"])
            if duration == 0:
                if size != 0 or rate["bitsPerSecond"] is not None or rate["tagId"] is not None:
                    raise ValueError("Curve unknown rate invented values")
            elif rate["bitsPerSecond"] is None or int(rate["bitsPerSecond"]) != size * 8000 // duration or rate["tagId"] is None or not first <= int(rate["tagId"]) < end:
                raise ValueError("Curve rate arithmetic or source Tag mismatch")
        gop = point["gop"]
        count, duration = int(gop["closedCount"]), int(gop["totalDurationMs"])
        if not count <= int(gop["declaredCount"]) <= end - first:
            raise ValueError("Curve GOP counts exceed source window")
        if count == 0:
            if duration != 0 or any(gop[key] is not None for key in ("minDurationMs", "maxDurationMs", "averageDurationMs", "tagId")):
                raise ValueError("Curve unknown GOP invented values")
        else:
            if any(gop[key] is None for key in ("minDurationMs", "maxDurationMs", "averageDurationMs", "tagId")):
                raise ValueError("Curve known GOP missing values")
            minimum, maximum, average = (int(gop[key]) for key in ("minDurationMs", "maxDurationMs", "averageDurationMs"))
            if (not 0 < minimum <= average <= maximum or average != duration // count or
                    not maximum + minimum * (count - 1) <= duration <= minimum + maximum * (count - 1) or
                    not first <= int(gop["tagId"]) < end):
                raise ValueError("Curve GOP arithmetic or source Tag mismatch")


def check_source(report, source_path, max_source_bytes):
    result = {}
    with BoundedSource(source_path, max_source_bytes) as source:
        if report["fileName"] != source_path.name or int(report["session"]["progress"]["sourceSize"]) != len(source):
            raise ValueError("Source name or size differs from snapshot")
        tagged = []
        if report["selectedTag"] is not None:
            tagged.append((int(report["selectedTag"]["id"]), report["selectedTag"]))
        if report["trend"] is not None:
            tagged.extend((int(point["tagId"]), point) for point in report["trend"]["points"])
        search_rows = ((report.get("avcSearchPage") or {}).get("page") or {}).get("rows", [])
        tagged.extend((int(row["tagId"]), row) for row in search_rows)
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
        for row in search_rows:
            offset = int(row["offset"])
            header = source[offset:offset + 11]
            dts = int.from_bytes(header[4:7], "big") | header[7] << 24
            if header[0] & 31 != 9 or dts != int(row["dts"]):
                raise ValueError("AVC search Tag type or DTS differs from source")
            declared = int.from_bytes(header[1:4], "big")
            span = row["span"]
            if span is not None:
                begin = int(span["byteOffset"]) * 8 + span["bitOffset"]
                end = begin + int(span["bitLength"])
                if not (offset + 11) * 8 <= begin < end <= min(offset + 11 + declared, len(source)) * 8:
                    raise ValueError("AVC search source span exceeds its Tag payload")
            if declared and row["declaredFrameType"] is not None and source[offset + 11:offset + 12][0] >> 4 != row["declaredFrameType"]:
                raise ValueError("AVC search declaration differs from source")
        if report.get("avcSearchPage") is not None:
            result["avcSearchRowsMatchingSource"] = len(search_rows)
        result["sourceSHA256"] = source.digest()
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
        if tag_id >= MAX_TAG_VISITS:
            raise ValueError("Source-order validation exceeded one million Tag headers")
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
    parser.add_argument("--max-source-bytes", type=int, default=MAX_FIXTURE)
    args = parser.parse_args()
    print(json.dumps(check_snapshot(args.report, args.source, args.max_source_bytes), ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
