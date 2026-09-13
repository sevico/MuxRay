"""Create deterministic FLV media-summary fixtures with only the standard library.

The absolute output directory must be new. No output is playable media: ordinary
audio/video bodies are framing samples, and no decoder or frame completeness is
asserted. Expected values below describe source order and the ADR-016 budgets;
the CLI/core must independently verify them before claiming desktop parity.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct

from hex_fixtures import AVCC, HEADER, tag, tag_header


DESKTOP_BUDGETS = {"maxKeyframes": 1024, "maxConfigurationEvents": 128,
                   "maxConfigurationBytes": 16 << 20,
                   "maxAnalysisDiagnostics": 256, "maxMP3HeaderEvents": 256}


def avcc(length_size: int) -> bytes:
    value = bytearray(AVCC)
    value[9] = 0xFC | (length_size - 1)
    return bytes(value)


def video(keyframe: bool = True, composition: int = 0, length_size: int = 4) -> bytes:
    return (bytes([0x17 if keyframe else 0x27, 1])
            + (composition & 0xFFFFFF).to_bytes(3, "big")
            + (2).to_bytes(length_size, "big") + b"\x65\x88")


class Fixture:
    def __init__(self):
        self.data = bytearray(HEADER)
        self.tags = []

    def add(self, kind: int, payload: bytes, dts: int, name: str):
        self.tags.append({"id": len(self.tags), "offset": len(self.data),
                          "payloadOffset": len(self.data) + 11,
                          "payloadBytes": len(payload), "tagType": kind,
                          "dts": dts, "name": name})
        self.data.extend(tag(kind, payload, dts))

    def result(self, expected: dict) -> tuple[bytes, list[dict], dict]:
        return bytes(self.data), self.tags, expected


def fixtures() -> dict[str, tuple[bytes, list[dict], dict]]:
    mixed = Fixture()
    rows = [
        (8, bytes.fromhex("af001210"), 0, "aac-44100-config"),
        (9, avcc(4), 0, "avc-length4-config"),
        (8, bytes.fromhex("af01aabb"), 0, "aac-raw"),
        (9, video(composition=10), 0, "keyframe-positive-pts"),
        (8, bytes.fromhex("af01aabb"), 20, "aac-forward"),
        (9, video(False, -10), 40, "inter-negative-composition"),
        (8, bytes.fromhex("af01aabb"), 20, "aac-duplicate"),
        (9, video(), 80, "keyframe-forward"),
        (8, bytes.fromhex("af01aabb"), 10, "aac-regression"),
        (8, bytes.fromhex("af001190"), 30, "aac-48000-config-change"),
        (9, avcc(2), 30, "avc-length2-config-change"),
        (9, video(length_size=2), 60, "keyframe-regression"),
        (8, bytes.fromhex("2ffffb9000"), 100, "mp3-128-stereo-codec-switch"),
        (8, bytes.fromhex("2ffffba000"), 126, "mp3-160-bitrate-change"),
        (8, bytes.fromhex("2ffffba0c0"), 152, "mp3-mono-parameter-change"),
        (8, bytes.fromhex("2ffffba0c0"), 152, "mp3-duplicate"),
        (8, bytes.fromhex("2ffffb90c0"), 140, "mp3-regression-resets-comparison"),
        (8, bytes.fromhex("af01aabb"), 200, "aac-codec-switch-back"),
        (18, b"", 0, "script-framing-only"),
        (31, b"\x01\x02\x03", 0, "unknown-tag"),
    ]
    for row in rows:
        mixed.add(*row)
    expected = {
        "summary.complete": True, "summary.tags.total": 20,
        "summary.tags.audio": 12, "summary.tags.video": 6,
        "summary.tags.script": 1, "summary.tags.other": 1,
        "summary.audioCodecs.aacTagCount": 7, "summary.audioCodecs.mp3TagCount": 5,
        "summary.audioCodecs.switchCount": 2,
        "summary.audio.timing.duplicateCount": 2, "summary.audio.timing.regressionCount": 2,
        "summary.audio.bitrate.bytes": 9, "summary.audio.bitrate.coveredIntervalMs": 46,
        "summary.audio.bitrate.averageBitsPerSecond": 9 * 8000 // 46,
        "summary.video.timing.regressionCount": 1,
        "summary.video.bitrate.bytes": 22, "summary.video.bitrate.coveredIntervalMs": 80,
        "summary.video.bitrate.averageBitsPerSecond": 2200,
        "summary.videoPts.minMs": 10, "summary.videoPts.maxMs": 80,
        "summary.compositionTime.minMs": -10, "summary.compositionTime.maxMs": 10,
        "summary.keyframes.declaredCount": 3,
        "summary.gop.closedDurationCount": 1, "summary.gop.averageDurationMs": 80,
        "summary.aacConfigurations.eventCount": 2, "summary.aacConfigurations.changeCount": 1,
        "summary.avcConfigurations.eventCount": 2, "summary.avcConfigurations.changeCount": 1,
        "summary.mp3.headers.eventCount": 5, "summary.mp3.headers.parameterChangeCount": 1,
        "summary.mp3.headers.bitrateChangeCount": 1,
        "summary.mp3.track.bitrate.bytes": 5, "summary.mp3.track.bitrate.coveredIntervalMs": 26,
        "summary.mp3.track.bitrate.averageBitsPerSecond": 5 * 8000 // 26,
        "absent": ["summary.avSync"],
    }

    timing = Fixture()
    timing.add(8, bytes.fromhex("af001210"), 0, "aac-config")
    timing.add(9, avcc(4), 0, "avc-config")
    for dts in (0, 40):
        timing.add(8, bytes.fromhex("af01aabb"), dts, "aac-raw")
        timing.add(9, video(composition=10), dts, "declared-keyframe")
    timing_expected = {
        "summary.complete": True, "summary.tags.total": 6,
        "summary.audio.bitrate.bytes": 4, "summary.audio.bitrate.coveredIntervalMs": 40,
        "summary.audio.bitrate.averageBitsPerSecond": 800,
        "summary.video.bitrate.bytes": 11, "summary.video.bitrate.coveredIntervalMs": 40,
        "summary.video.bitrate.averageBitsPerSecond": 2200,
        "summary.gop.closedDurationCount": 1, "summary.gop.averageDurationMs": 40,
        "summary.avSync.startExtremaSkewMs": 10, "summary.avSync.endExtremaSkewMs": 10,
        "summary.avSync.rangesOverlap": True, "summary.avSync.overlapMs": 30,
    }

    retention = Fixture()
    for index in range(130):
        retention.add(8, bytes.fromhex("af001210" if index % 2 == 0 else "af001190"), index, "aac-config")
    for index in range(130):
        retention.add(9, avcc(4 if index % 2 == 0 else 2), index, "avc-config")
    for index in range(1100):
        retention.add(9, video(length_size=2), index * 40, "declared-keyframe")
    for index in range(300):
        retention.add(8, bytes.fromhex("2ffffb9000" if index % 2 == 0 else "2ffffba000"), index * 26, "mp3-header")
    retained_expected = {
        "summary.complete": True, "summary.tags.total": 1660,
        "summary.keyframes.declaredCount": 1100, "summary.keyframes.truncated": True,
        "summary.aacConfigurations.eventCount": 130, "summary.aacConfigurations.changeCount": 129,
        "summary.aacConfigurations.truncated": True,
        "summary.avcConfigurations.eventCount": 130, "summary.avcConfigurations.changeCount": 129,
        "summary.avcConfigurations.truncated": True,
        "summary.mp3.headers.eventCount": 300, "summary.mp3.headers.validCount": 300,
        "summary.mp3.headers.bitrateChangeCount": 299, "summary.mp3.headers.truncated": True,
        "lengths": {"summary.keyframes.entries": 1024, "summary.aacConfigurations.events": 128,
                    "summary.avcConfigurations.events": 128, "summary.mp3.headers.events": 256},
    }
    malformed = Fixture()
    for index in range(300):
        malformed.add(8, bytes.fromhex("2f7ffb9000"), index * 26, "invalid-mp3-sync")
    malformed_expected = {
        "summary.complete": False, "summary.tags.total": 300,
        "summary.mp3.headers.eventCount": 300, "summary.mp3.headers.validCount": 0,
        "summary.mp3.headers.malformedCount": 300, "summary.mp3.headers.truncated": True,
        "lengths": {"analysisDiagnostics": 256, "summary.mp3.headers.events": 256},
        "lastDiagnosticCode": "MEDIA_DIAGNOSTIC_LIMIT_REACHED",
    }
    partial_data = bytes(mixed.data) + tag_header(8, 10, 250) + bytes.fromhex("af00")
    partial_tags = mixed.tags + [{"id": 20, "offset": len(mixed.data),
                                "payloadOffset": len(mixed.data) + 11,
                                "tagType": 8, "dts": 250, "declaredBytes": 10,
                                "availableBytes": 2, "name": "container-truncated"}]
    partial_expected = {"scan.complete": False, "scan.indexedTagCount": 21,
                        "summary.complete": False, "summary.tags.total": 21,
                        "summary.audioCodecs.unknownTagCount": 1}
    return {"summary-mixed.flv": mixed.result(expected),
            "summary-change.flv": mixed.result(expected),
            "summary-timing.flv": timing.result(timing_expected),
            "summary-retention.flv": retention.result(retained_expected),
            "summary-diagnostics.flv": malformed.result(malformed_expected),
            "summary-truncated.flv": (partial_data, partial_tags, partial_expected)}


def many_tags(path: Path, count: int) -> dict:
    """Write bounded chunks, with one MP3 header after count-1 empty Script Tags."""
    digest = hashlib.sha256()
    size = 0
    with path.open("xb") as stream:
        def write(data):
            nonlocal size
            stream.write(data)
            digest.update(data)
            size += len(data)
        write(HEADER)
        remaining = count - 1
        row = tag(18, b"")
        while remaining:
            chunk = min(4096, remaining)
            write(row * chunk)
            remaining -= chunk
        tail = size
        write(tag(8, bytes.fromhex("2ffffb9000"), 1000))
    return {"name": path.name, "bytes": size, "sha256": digest.hexdigest(), "tagCount": count,
            "tailTag": {"id": count - 1, "offset": tail, "payloadOffset": tail + 11},
            "expected": {"summary.tags.total": count, "summary.tags.script": count - 1,
                         "summary.mp3.headers.eventCount": 1, "summary.complete": True},
            "scope": "High Tag count and shared index ownership; small physical file, not a >1 GiB test"}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, required=True)
    parser.add_argument("--many-tags", type=int, default=200000)
    args = parser.parse_args()
    if not args.output_dir.is_absolute():
        parser.error("Use an absolute, new output directory.")
    if args.many_tags and not 32 <= args.many_tags <= 1_000_000:
        parser.error("--many-tags must be 0 or 32..1000000.")
    args.output_dir.mkdir(parents=True, exist_ok=False)
    files = []
    for name, (data, records, expected) in fixtures().items():
        with (args.output_dir / name).open("xb") as stream:
            stream.write(data)
        files.append({"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                      "tagCount": len(records), "tags": records, "expected": expected})
    if args.many_tags:
        files.append(many_tags(args.output_dir / "summary-many-tags.flv", args.many_tags))
    manifest = {"format": "muxray-desktop-summary-fixtures-v1", "files": files,
                "budgets": DESKTOP_BUDGETS,
                "scope": "Container/source-order facts and bounded first-header analysis; no decode, frame count, playable duration, real I/O latency or real A/V synchronization proof"}
    with (args.output_dir / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(json.dumps({"files": [{key: f[key] for key in ("name", "bytes", "sha256", "tagCount")}
                                 for f in files]}, indent=2))


if __name__ == "__main__":
    main()
