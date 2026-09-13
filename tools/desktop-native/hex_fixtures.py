"""Create small deterministic FLV fixtures for desktop Hex navigation.

Uses only the standard library. The output directory must be absolute and new;
outputs are never overwritten. No playable media or large file is generated.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct


HEADER = bytes.fromhex("464c5601050000000900000000")
AVCC = bytes.fromhex("17000000000142c00bffe100166742c00bd902c4ec0440000003004000000c83c50a9201000568cb83cb20")


def tag_header(kind: int, size: int, dts: int = 0) -> bytes:
    return (bytes([kind]) + size.to_bytes(3, "big")
            + (dts & 0xFFFFFF).to_bytes(3, "big") + bytes([dts >> 24]) + b"\0\0\0")


def tag(kind: int, payload: bytes, dts: int = 0) -> bytes:
    return tag_header(kind, len(payload), dts) + payload + struct.pack(">I", 11 + len(payload))


def string(value: bytes) -> bytes:
    return b"\x02" + struct.pack(">H", len(value)) + value


def span(byte_offset: int, bit_offset: int, bit_length: int) -> dict:
    return {"byteOffset": str(byte_offset), "bitOffset": bit_offset, "bitLength": bit_length}


def window(data: bytes, offset: int, count: int) -> dict:
    actual = data[offset:offset + count]
    return {"offset": offset, "count": count, "returnedBytes": len(actual),
            "eof": offset + len(actual) == len(data), "hex": actual.hex()}


def fixtures() -> dict[str, tuple[bytes, list[dict]]]:
    text = b"0123456789ABCDEF" * 64
    metadata = string(b"onMetaData") + b"\x03\0\x08boundary" + string(text) + b"\0\0\x09"
    examples = [
        (18, metadata, "amf-boundary", "amf0ScriptData", "1024-byte string crosses Hex windows; display preview is shorter than its original span"),
        (8, bytes.fromhex("af001210"), "aac-config", "aacAudio", "AAC-LC fields include a frequency index across a byte boundary"),
        (9, AVCC, "avc-config", "avcVideo", "AVC configuration with original lengthSizeMinusOne bits and derived SPS summary"),
        (8, bytes.fromhex("2ffffb9000"), "mp3-header", "mp3Audio", "MPEG-1 header; source bit spans and derived values stay distinct"),
        (9, bytes.fromhex("1701000000000000026588"), "avc-nalu", "avcVideo", "Four-byte NAL length from Tag 2; raw NAL bytes remain accessible"),
        (31, bytes(range(256)) * 8, "unknown-tag", None, "Unknown Tag remains indexed and all 2048 payload bytes are readable in Hex"),
        (8, bytes.fromhex("af0012"), "aac-short", "aacAudio", "Diagnostic identifies the first unavailable ASC bit at the payload end"),
    ]
    data = bytearray(HEADER)
    records = []

    def append(kind: int, payload: bytes, name: str, detail_kind, purpose: str) -> dict:
        record = {"id": len(records), "name": name, "offset": len(data),
                  "payloadOffset": len(data) + 11, "payloadBytes": len(payload),
                  "tagType": kind, "detailKind": detail_kind, "purpose": purpose}
        records.append(record)
        data.extend(tag(kind, payload))
        return record

    for item in examples:
        append(*item)
    while len(records) < 105:
        append(18, b"", "framing-only", "amf0ScriptData", "Empty ScriptData provides an indexed paging row, not valid AMF content")
    tail = append(8, bytes.fromhex("2ffffb92c0"), "second-page-mp3", "mp3Audio",
                  "Tag ID 105 is outside the first 100-row page; MP3 mono with padding")

    records[0]["links"] = {"value": span(records[0]["payloadOffset"] + 27, 0, len(text) * 8)}
    records[0]["linkParentNames"] = {"value": "boundary"}
    records[1]["links"] = {"initialAudioObjectType": span(records[1]["payloadOffset"] + 2, 0, 5),
                            "samplingFrequencyIndex": span(records[1]["payloadOffset"] + 2, 5, 4)}
    records[2]["links"] = {"lengthSizeMinusOne": span(records[2]["payloadOffset"] + 9, 6, 2),
                            "SPS summary": None}
    records[3]["links"] = {"syncWord": span(records[3]["payloadOffset"] + 1, 0, 11),
                            "bitrateKbps": None, "frameLengthBytes": None}
    records[4]["links"] = {"nalUnitType": span(records[4]["payloadOffset"] + 9, 3, 5)}
    records[5]["expectedDiagnostic"] = {"code": "FLV_TAG_TYPE_UNKNOWN", "offset": records[5]["offset"], "bitOffset": 0}
    records[5]["expectedDetailError"] = "detail_unsupported"
    records[6]["expectedDiagnostic"] = {"code": "AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED",
                                         "offset": records[6]["payloadOffset"] + 3, "bitOffset": 0}
    tail["links"] = {"padding": span(tail["payloadOffset"] + 3, 6, 1), "frameLengthBytes": None}

    partial = {"id": len(records), "name": "container-truncated", "offset": len(data),
               "payloadOffset": len(data) + 11, "declaredBytes": 10, "availableBytes": 2,
               "expectedDetailError": "detail_truncated",
               "purpose": "Hex may read the existing AF 00 bytes despite incomplete container payload"}
    truncated = bytes(data) + tag_header(8, 10) + bytes.fromhex("af00")
    return {"hex-details.flv": (bytes(data), records),
            "hex-change.flv": (bytes(data), records),
            "hex-truncated.flv": (truncated, records + [partial])}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path)
    target = parser.parse_args().output_dir
    if not target.is_absolute():
        parser.error("Use an absolute, new output directory.")
    target.mkdir(parents=True, exist_ok=False)
    files = []
    for name, (data, records) in fixtures().items():
        with (target / name).open("xb") as stream:
            stream.write(data)
        windows = [window(data, offset, count) for offset, count in
                   [(0, 512), (512, 512), (1024, 512), (0, 4096),
                    (len(data) - 5, 512), (len(data), 512)]]
        for tag_id in (0, 1, 2, 3, 4, 5, 6, 105):
            windows.append({"purpose": "Tag offset link", "tagId": tag_id,
                            **window(data, records[tag_id]["offset"], 512)})
        windows.append({"purpose": "Unknown Tag payload 00..FF pattern", "tagId": 5,
                        **window(data, records[5]["payloadOffset"], 512)})
        for offset in (51, 563):
            windows.append({"purpose": "boundary / value field and next window", "tagId": 0,
                            **window(data, offset, 512)})
        windows.append({"purpose": "Short AAC diagnostic offset", "tagId": 6,
                        **window(data, records[6]["expectedDiagnostic"]["offset"], 512)})
        if name == "hex-truncated.flv":
            windows.append({"purpose": "Existing bytes of incomplete container payload", "tagId": 106,
                            **window(data, records[-1]["payloadOffset"], 512)})
        record = {"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(),
                  "tags": records, "windows": windows,
                  "invalidRequests": [{"offset": len(data) + 1, "count": 512, "error": "invalid_argument"},
                                      {"offset": 0, "count": 4097, "error": "invalid_argument"}]}
        if name == "hex-change.flv":
            record["afterControlledMutation"] = {"error": "source_changed"}
        files.append(record)
    manifest = {"format": "muxray-desktop-hex-fixtures-v1", "files": files,
                "scope": "Raw ranges, Tag/field/diagnostic navigation and bit positions; no audio/video decode"}
    with (target / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(json.dumps({"files": [{"name": f["name"], "bytes": f["bytes"], "sha256": f["sha256"],
                                  "tagCount": len(f["tags"]), "tailTagOffset": f["tags"][105]["offset"]}
                                 for f in files]}, indent=2))


if __name__ == "__main__":
    main()
