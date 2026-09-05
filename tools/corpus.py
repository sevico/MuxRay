#!/usr/bin/env python3
"""Offline, bounded corpus verification; never downloads or updates expectations."""
from __future__ import annotations

import argparse
from collections import Counter
from functools import lru_cache
import hashlib
import json
import os
from pathlib import Path
import signal
import subprocess
import threading
import time

from jsonschema import Draft202012Validator, ValidationError

ROOT = Path(__file__).resolve().parent.parent
MAX_SAMPLE_BYTES = 32 << 20
MAX_OUTPUT_BYTES = 16 << 20
MAX_TAGS = 50_000


def require(condition, message):
    if not condition:
        raise ValueError(message)


@lru_cache(maxsize=8)
def report_validator(schema_name):
    schema = json.loads((ROOT / "docs/schemas" / schema_name).read_text(encoding="utf-8"))
    return Draft202012Validator(schema)


def parse_report(output, schema_name):
    document = json.loads(output)
    report_validator(schema_name).validate(document)
    return document


def stop_process(process):
    """Only terminate the process group/tree this invocation created."""
    if process.poll() is not None:
        return
    if os.name == "nt":
        try:
            subprocess.run(["taskkill.exe", "/PID", str(process.pid), "/T", "/F"],
                           stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                           timeout=2, check=False, creationflags=subprocess.CREATE_NO_WINDOW)
        except (OSError, subprocess.TimeoutExpired):
            process.kill()
    else:
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
    if process.poll() is None:
        process.kill()


def bounded_run(argv, timeout=30, limit=MAX_OUTPUT_BYTES):
    """Argument vector, bounded output/time, no shell and no unbounded communicate."""
    require(timeout > 0 and limit > 0, "invalid process limits")
    options = {"start_new_session": True} if os.name != "nt" else {
        "creationflags": subprocess.CREATE_NEW_PROCESS_GROUP | subprocess.CREATE_NO_WINDOW}
    process = subprocess.Popen([str(arg) for arg in argv], stdin=subprocess.DEVNULL,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, **options)
    overflow = threading.Event()
    buffers = [bytearray(), bytearray()]

    def drain(pipe, buffer):
        try:
            while chunk := pipe.read(65536):
                if len(buffer) + len(chunk) > limit:
                    overflow.set()
                    return
                buffer.extend(chunk)
        finally:
            pipe.close()

    threads = [threading.Thread(target=drain, args=pair, daemon=True)
               for pair in zip((process.stdout, process.stderr), buffers)]
    for thread in threads:
        thread.start()
    deadline = time.monotonic() + timeout
    try:
        while process.poll() is None or any(thread.is_alive() for thread in threads):
            require(not overflow.is_set(), "external process exceeded output limit")
            require(time.monotonic() < deadline, "external process timed out")
            time.sleep(0.005)
        require(not overflow.is_set(), "external process exceeded output limit")
        return process.returncode, bytes(buffers[0]), bytes(buffers[1])
    finally:
        stop_process(process)
        process.wait(timeout=2)
        for thread in threads:
            thread.join(timeout=2)


def safe_sample(directory, name):
    require(name not in ("", ".", "..") and not any(ch in name for ch in '/\\:'), "unsafe sample basename")
    root = directory.resolve()
    candidate = (root / name).resolve()
    require(candidate.parent == root, "sample escapes media directory")
    return candidate


def file_identity(path):
    require(path.is_file(), "sample is not a regular file")
    size = path.stat().st_size
    require(size <= MAX_SAMPLE_BYTES, "sample exceeds small-corpus limit; use largefile tool")
    digest = hashlib.sha256()
    read_bytes = 0
    with path.open("rb") as source:
        while chunk := source.read(1 << 20):
            read_bytes += len(chunk)
            require(read_bytes <= size, "sample grew while hashing")
            digest.update(chunk)
    require(read_bytes == size and path.stat().st_size == size, "sample changed while hashing")
    return size, digest.hexdigest()


def raw_framing(path):
    """Independent fixed-field walk; no MuxRay imports or codec parser reuse."""
    size = path.stat().st_size
    with path.open("rb") as source:
        header = source.read(9)
        require(len(header) == 9 and header[:3] == b"FLV", "oracle requires a complete FLV header")
        offset = int.from_bytes(header[5:9], "big")
        require(9 <= offset <= size - 4, "oracle requires an initial PreviousTagSize")
        source.seek(offset)
        previous0 = int.from_bytes(source.read(4), "big")
        facts = {"present": True, "version": header[3], "typeFlags": header[4],
                 "hasAudio": bool(header[4] & 4), "hasVideo": bool(header[4] & 1),
                 "dataOffset": offset, "previousTagSize0": previous0}
        offset += 4
        records = []
        complete = True
        while offset < size:
            require(len(records) < MAX_TAGS, "oracle tag limit reached")
            source.seek(offset)
            tag_header = source.read(11)
            if len(tag_header) != 11:
                complete = False
                break
            declared = int.from_bytes(tag_header[1:4], "big")
            available = min(declared, size - offset - 11)
            trailer_offset = offset + 11 + declared
            present = trailer_offset + 4 <= size
            trailer = None
            if present:
                source.seek(trailer_offset)
                trailer = int.from_bytes(source.read(4), "big")
            source.seek(offset + 11)
            prefix = source.read(min(available, 5))
            records.append({"id": len(records), "offset": offset,
                            "rawTypeFlags": tag_header[0], "typeCode": tag_header[0] & 31,
                            "dataSize": declared, "availableDataSize": available,
                            "dts": int.from_bytes(tag_header[4:7], "big") | tag_header[7] << 24,
                            "streamId": int.from_bytes(tag_header[8:11], "big"),
                            "previous": trailer, "prefix": prefix})
            if not present:
                complete = False
                break
            offset = trailer_offset + 4
    return facts, records, complete


def compare_framing(document, header, records, complete):
    require(document["scan"]["complete"] == complete, "scan completeness differs from framing oracle")
    for field, value in header.items():
        require(document["header"][field] == value, f"header field differs: {field}")
    require(len(document["tags"]) == len(records), "tag count differs from framing oracle")
    for actual, raw in zip(document["tags"], records):
        for field in ("id", "offset", "rawTypeFlags", "typeCode", "dataSize", "availableDataSize", "dts", "streamId"):
            require(actual[field] == raw[field], f"tag {raw['id']} differs: {field}")
        require(actual["headerSpan"] == {"byteOffset": raw["offset"], "bitOffset": 0, "bitLength": 88}, "header span differs")
        require(actual["payloadSpan"] == {"byteOffset": raw["offset"] + 11, "bitOffset": 0,
                                           "bitLength": raw["availableDataSize"] * 8}, "payload span differs")
        require(actual["span"] == {"byteOffset": raw["offset"], "bitOffset": 0,
                                    "bitLength": (11 + raw["availableDataSize"]) * 8}, "tag span differs")
        require(actual["payloadComplete"] == (raw["availableDataSize"] == raw["dataSize"]), "payload completeness differs")
        previous = actual["previousTagSize"]
        require(previous["present"] == (raw["previous"] is not None), "trailer presence differs")
        require(previous["expected"] == raw["dataSize"] + 11, "expected trailer differs")
        require(previous["value"] == (raw["previous"] or 0), "trailer value differs")
        require(previous["valid"] == (raw["previous"] == raw["dataSize"] + 11), "trailer validity differs")
        require(actual["filter"] == bool(raw["rawTypeFlags"] & 0x20), "filter flag differs")
        if raw["previous"] is not None:
            require(previous["span"] == {"byteOffset": raw["offset"] + 11 + raw["dataSize"],
                                           "bitOffset": 0, "bitLength": 32}, "trailer span differs")
        else:
            require("span" not in previous, "absent trailer has a source span")


def compare_expected(document, expected):
    require(document["schemaVersion"] == expected["reportSchemaVersion"], "report version differs")
    require(document["scan"]["complete"] == expected["scanComplete"], "expected completeness differs")
    require(len(document["tags"]) == expected["tagCount"], "expected tag count differs")
    for key, value in expected["header"].items():
        require(document["header"][key] == value, f"expected header differs: {key}")
    counts = Counter((d["code"], d["severity"]) for d in document["diagnostics"])
    wanted = {(d["code"], d["severity"]): d["count"] for d in expected["diagnostics"]}
    require(dict(counts) == wanted, "expected diagnostic counts differ")
    for diagnostic in expected["diagnostics"]:
        if "firstAnchor" in diagnostic:
            first = next(d for d in document["diagnostics"] if (d["code"], d["severity"]) ==
                         (diagnostic["code"], diagnostic["severity"]))
            require(first["offset"] == diagnostic["firstAnchor"]["byteOffset"] and
                    first["bitOffset"] == diagnostic["firstAnchor"]["bitOffset"], "first diagnostic anchor differs")


def packet_oracle(records):
    packets = []
    for raw in records:
        prefix = raw["prefix"]
        if not prefix or raw["availableDataSize"] != raw["dataSize"] or raw["rawTypeFlags"] & 0x20:
            continue
        kind = raw["typeCode"]
        framing = 0
        pts = raw["dts"]
        if kind == 8:
            codec = prefix[0] >> 4
            if codec == 10:
                if len(prefix) < 2 or prefix[1] != 1:
                    continue
                framing = 2
            elif codec in (1, 2):  # ADPCM SWF / MP3
                framing = 1
            else:
                raise ValueError("packet oracle audio codec is not covered")
        elif kind == 9:
            codec = prefix[0] & 15
            if codec == 7:
                if len(prefix) < 5 or prefix[1] != 1 or prefix[0] >> 4 == 5:
                    continue
                framing = 5
                pts += int.from_bytes(prefix[2:5], "big", signed=True)
            elif codec == 2:  # Sorenson H.263 / FLV1
                framing = 1
            else:
                raise ValueError("packet oracle video codec is not covered")
        else:
            continue
        packets.append({"codec_type": "audio" if kind == 8 else "video", "dts": raw["dts"],
                        "pts": pts, "pos": str(raw["offset"]), "size": str(raw["dataSize"] - framing)})
    return packets


def approved_packet_difference(manifest, version, actual, expected):
    rules = json.loads((ROOT / "tools/corpus-differences-v1.json").read_text(encoding="utf-8"))
    require(rules["version"] == 1, "unsupported difference allowlist version")
    for rule in rules["rules"]:
        if (rule["sampleId"], rule["sha256"], rule["ffprobeVersion"]) != (manifest["id"], manifest["file"]["sha256"], version):
            continue
        removed = [p for p in expected if p["pos"] in rule["omittedPacketPositions"]]
        remaining = [p for p in expected if p["pos"] not in rule["omittedPacketPositions"]]
        if {p["pos"] for p in removed} == set(rule["omittedPacketPositions"]) and actual == remaining:
            return rule["id"]
    raise ValueError("ffprobe packet count/order/position/size/timestamp differs without a matching reviewed rule")


def verify_one(manifest, directory, muxray, ffprobe, repeat, ffprobe_version=None):
    path = safe_sample(directory, manifest["file"]["name"])
    size, digest = file_identity(path)
    require((size, digest) == (manifest["file"]["sizeBytes"], manifest["file"]["sha256"]), "sample identity differs")
    header, records, complete = raw_framing(path)
    reference = None
    for _ in range(repeat):
        code, output, _ = bounded_run([muxray, "inspect", path])
        require(code in (0, 2), "inspect failed")
        if reference is None:
            reference = output
        require(output == reference, "repeated inspect JSON changed")
    document = parse_report(reference, "flv-scan-report-v1.schema.json")
    require(document["source"]["sizeBytes"] == size and document["scan"]["bytesScanned"] <= size, "source size/cursor differs")
    if complete:
        require(document["scan"]["bytesScanned"] == size, "complete scan cursor did not reach EOF")
    compare_framing(document, header, records, complete)
    compare_expected(document, manifest["expected"])
    for start in sorted({0, min(1023, len(records)), len(records)}):
        code, output, _ = bounded_run([muxray, "tags", "--start", start, "--count", 17, path])
        require(code in (0, 2), "tags failed")
        page = parse_report(output, "flv-tag-page-v1.schema.json")
        require(page["page"] == {"start": start, "limit": 17, "total": len(records), "next": min(start + 17, len(records))}, "page cursor differs")
        require(page["tags"] == document["tags"][start:start + 17], "page changed records")
    summary_code, summary_output, _ = bounded_run([muxray, "summary", "--schema-version", 2, path])
    require(summary_code in (0, 2), "summary failed")
    summary = parse_report(summary_output, "flv-media-summary-v2.schema.json")
    require(summary["scan"]["indexedTagCount"] == len(records), "summary lost indexed tags")
    detail_kinds = set()
    if complete:
        for raw in records:
            prefix = raw["prefix"]
            identity = None
            variant = None
            if prefix and raw["typeCode"] == 8 and prefix[0] >> 4 in (2, 10):
                identity = "flv-mp3-tag-detail" if prefix[0] >> 4 == 2 else "flv-aac-tag-detail"
                variant = prefix[1] if prefix[0] >> 4 == 10 and len(prefix) > 1 else 0
            elif len(prefix) >= 2 and raw["typeCode"] == 9 and prefix[0] & 15 == 7:
                identity, variant = "flv-avc-tag-detail", prefix[1]
            if identity is None or (identity, variant) in detail_kinds:
                continue
            detail_kinds.add((identity, variant))
            code, output, _ = bounded_run([muxray, "detail", "--tag", raw["id"], path])
            require(code == 0, "supported codec detail failed")
            detail = parse_report(output, identity + "-v1.schema.json")
            require(detail["documentType"] == identity and detail["detail"]["complete"], "detail identity/completeness differs")
    packet_count = None
    stream_facts = []
    explained_difference = None
    if ffprobe and complete:
        code, output, _ = bounded_run([ffprobe, "-v", "error", "-protocol_whitelist", "file,pipe",
                                      "-show_packets", "-show_entries", "packet=codec_type,dts,pts,pos,size", "-of", "json", path])
        require(code == 0, "ffprobe packet inspection failed")
        actual_packets = json.loads(output)["packets"]
        wanted_packets = packet_oracle(records)
        if actual_packets != wanted_packets:
            explained_difference = approved_packet_difference(manifest, ffprobe_version, actual_packets, wanted_packets)
            code, output, _ = bounded_run([ffprobe, "-v", "error", "-protocol_whitelist", "file,pipe",
                "-flv_ignore_prevtag", 1, "-show_packets", "-show_entries", "packet=codec_type,dts,pts,pos,size", "-of", "json", path])
            require(code == 0 and json.loads(output)["packets"] == wanted_packets, "ignoring PreviousTagSize did not explain the exact packet difference")
        packet_count = len(actual_packets)
        code, output, _ = bounded_run([ffprobe, "-v", "error", "-protocol_whitelist", "file,pipe",
                                      "-show_entries", "stream=codec_type,codec_name,width,height,sample_rate,channels,time_base", "-of", "json", path])
        require(code == 0, "ffprobe stream inspection failed")
        stream_facts = json.loads(output)["streams"]
        for stream in stream_facts:
            require(stream["time_base"] == "1/1000", "ffprobe time base is not milliseconds")
            codec = stream["codec_name"]
            kind = stream["codec_type"]
            if codec in ("aac", "mp3", "h264"):
                expected_count = sum(p["codec_type"] == kind for p in wanted_packets)
                require(summary["summary"][kind]["mediaTagCount"] == expected_count, "summary media count differs from ffprobe")
                if codec == "aac":
                    event = summary["summary"]["aacConfigurations"]["events"][0]
                    require(event["samplingFrequencyHz"] == int(stream["sample_rate"]) and
                            event["channelConfiguration"]["channelCount"] == stream["channels"], "AAC sample rate/channels differ")
                elif codec == "mp3":
                    parameters = summary["summary"]["mp3"]["headers"]["events"][0]["parameters"]
                    require(parameters["samplingFrequencyHz"] == int(stream["sample_rate"]) and
                            parameters["channels"] == stream["channels"], "MP3 sample rate/channels differ")
                else:
                    dimensions = summary["summary"]["avcConfigurations"]["events"][0]["firstCompleteSps"]
                    require(dimensions["displayWidth"] == stream["width"] and dimensions["displayHeight"] == stream["height"], "AVC dimensions differ")
            # Other codecs are intentionally indexed but not analyzed by phase 1.
    require(file_identity(path) == (size, digest), "sample changed during verification")
    return {"id": manifest["id"], "sha256": digest, "sizeBytes": size,
            "tagCount": len(records), "scanComplete": complete, "repeatCount": repeat,
            "scanDiagnostics": [{"code": d["code"], "offset": d["offset"]} for d in document["diagnostics"]],
            "summaryComplete": summary["summary"]["complete"],
            "detailVariantsChecked": len(detail_kinds),
            "analysisCodes": sorted({d["code"] for d in summary["analysisDiagnostics"]}),
            "ffprobePacketsCompared": packet_count, "ffprobeStreams": stream_facts,
            "explainedDifference": explained_difference}


def load_manifests(directory):
    schema = json.loads((ROOT / "testdata/manifests/sample-manifest-v1.schema.json").read_text(encoding="utf-8"))
    validator = Draft202012Validator(schema)
    manifests = []
    ids, hashes = set(), set()
    for path in sorted(directory.glob("*.sample.json")):
        if path.name.startswith("example-"):
            continue
        manifest = json.loads(path.read_text(encoding="utf-8"))
        validator.validate(manifest)
        require(path.name == manifest["id"] + ".sample.json", "manifest filename/ID mismatch")
        require(manifest["id"] not in ids, "duplicate sample ID")
        require(manifest["file"]["sha256"] not in hashes, "duplicate sample hash")
        safe_sample(directory, manifest["file"]["name"])
        ids.add(manifest["id"])
        hashes.add(manifest["file"]["sha256"])
        manifests.append(manifest)
    require(manifests, "no registered manifests selected")
    return manifests


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifests", type=Path, default=ROOT / "testdata/manifests")
    parser.add_argument("--media-dir", type=Path, required=True)
    parser.add_argument("--muxray", type=Path, required=True)
    parser.add_argument("--ffprobe", type=Path)
    parser.add_argument("--repeat", type=int, default=3)
    parser.add_argument("--allow-missing", action="store_true", help="report missing samples, never count them as passed")
    args = parser.parse_args()
    require(1 <= args.repeat <= 10, "repeat must be 1-10")
    muxray = args.muxray.resolve(strict=True)
    ffprobe = args.ffprobe.resolve(strict=True) if args.ffprobe else None
    ffprobe_version = None
    if ffprobe:
        code, output, _ = bounded_run([ffprobe, "-version"], limit=1 << 20)
        require(code == 0, "cannot identify ffprobe version")
        ffprobe_version = output.decode("utf-8").splitlines()[0].split(" Copyright")[0]
    results, missing = [], []
    for manifest in load_manifests(args.manifests):
        path = safe_sample(args.media_dir, manifest["file"]["name"])
        if not path.is_file() and args.allow_missing:
            missing.append(manifest["id"])
            continue
        try:
            results.append(verify_one(manifest, args.media_dir, muxray, ffprobe, args.repeat, ffprobe_version))
        except (ValueError, KeyError, ValidationError) as error:
            raise ValueError(f"{manifest['id']}: {error}") from error
    print(json.dumps({"toolVersion": 1, "ffprobeVersion": ffprobe_version, "verified": results, "missing": missing}, indent=2))


if __name__ == "__main__":
    main()
