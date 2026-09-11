"""Create deterministic FLV/MP3 header fixtures using only the standard library.

No file promises playable audio. --large-mib physically writes 64..4096 MiB of
filler plus an 8 MiB opaque MP3 tail; neither generation nor hashing loads the
large file into memory. An absolute, previously nonexistent output directory is
required, and every output is created exclusively.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct


HEADER = b"FLV\x01\x04\0\0\0\x09\0\0\0\0"
TAIL_OPAQUE_BYTES = 8 << 20


def tag_header(size: int, kind: int = 8, dts: int = 0) -> bytes:
    return (bytes([kind]) + size.to_bytes(3, "big")
            + (dts & 0xFFFFFF).to_bytes(3, "big") + bytes([dts >> 24]) + b"\0\0\0")


def tag(payload: bytes, dts: int = 0) -> bytes:
    return tag_header(len(payload), dts=dts) + payload + struct.pack(">I", 11 + len(payload))


def normal(version: str, channels: int, rate: int, bitrate: int,
           samples: int, length: int, **changes) -> dict:
    expected = {"state": "ready", "complete": True, "truncated": False,
                "diagnostics": [], "version": version, "channels": channels,
                "samplingFrequencyHz": rate, "bitrateKbps": bitrate,
                "samplesPerFrame": samples, "frameLengthBytes": length,
                "padding": False, "crcProtected": False,
                "bitrateMode": "indexed"}
    expected.update(changes)
    return expected


def malformed(code: str, *, truncated: bool = False) -> dict:
    return {"state": "ready", "complete": False, "truncated": truncated,
            "diagnostics": [code]}


def small_fixtures() -> dict[str, tuple[bytes, list[dict]]]:
    stereo = normal("mpeg1", 2, 44100, 128, 1152, 417)
    examples = [
        ("mpeg1-stereo", "2ffffb9000", stereo,
         "MPEG-1 Layer III, 128 kbps, 44100 Hz, stereo; fixed header only"),
        ("mpeg1-mono", "2ffffb90c0", normal("mpeg1", 1, 44100, 128, 1152, 417),
         "MPEG mono; FLV SoundType still declares stereo"),
        ("mpeg2-stereo", "2ffff38000", normal("mpeg2", 2, 22050, 64, 576, 208),
         "MPEG-2 rate differs from FLV SoundRate declaration"),
        ("mpeg2-mono", "2ffff380c0", normal("mpeg2", 1, 22050, 64, 576, 208),
         "MPEG-2 mono"),
        ("mpeg25-stereo", "2fffe38000", normal("mpeg2.5", 2, 11025, 64, 576, 417),
         "MPEG-2.5 stereo"),
        ("mpeg25-mono", "2fffe380c0", normal("mpeg2.5", 1, 11025, 64, 576, 417),
         "MPEG-2.5 mono"),
        ("padding", "2ffffb9200", normal("mpeg1", 2, 44100, 128, 1152, 418, padding=True),
         "Padding adds one byte to the derived frame length"),
        ("free-format", "2ffffb0000", dict(stereo, bitrateKbps=None, frameLengthBytes=None,
                                          bitrateMode="free", diagnostics=["MP3_FREE_FORMAT_UNRESOLVED"]),
         "Complete header with warning; bitrate and frame length remain unresolved"),
        ("crc-declared", "2ffffa90001234aabb", dict(stereo, crcProtected=True),
         "Protection bit declares CRC; following 12 34 AA BB stays opaque, CRC is not verified"),
        ("bad-sync", "2f7ffb9000", malformed("MP3_SYNC_INVALID"), "Invalid eleven-bit sync"),
        ("reserved-version", "2fffeb9000", malformed("MP3_VERSION_RESERVED"), "Reserved version ID"),
        ("reserved-layer", "2ffff99000", malformed("MP3_LAYER_RESERVED"), "Reserved layer ID"),
        ("layer-ii", "2ffffd9000", malformed("MP3_LAYER_NOT_III"), "Layer II is outside MP3 scope"),
        ("invalid-bitrate", "2ffffbf000", malformed("MP3_BITRATE_INDEX_INVALID"), "Forbidden bitrate index 15"),
        ("reserved-rate", "2ffffb9c00", malformed("MP3_SAMPLING_FREQUENCY_RESERVED"), "Reserved rate index 3"),
        ("reserved-emphasis", "2ffffb9002", dict(stereo, diagnostics=["MP3_EMPHASIS_RESERVED"]),
         "Reserved emphasis is a warning; fixed header remains complete"),
        ("short-header", "2ffffb", malformed("MP3_FRAME_HEADER_TRUNCATED", truncated=True),
         "Only two bytes remain after the FLV audio header"),
        ("empty-mp3-body", "2f", malformed("MP3_FRAME_HEADER_TRUNCATED", truncated=True),
         "FLV audio header is present; no MPEG header bytes remain"),
        ("sound-format-14", "effffb9000", {"state": "failed", "error": "detail_unsupported"},
         "SoundFormat 14 is unsupported even when the following bytes resemble MP3"),
    ]
    data = bytearray(HEADER)
    records = []
    for name, hex_payload, expected, purpose in examples:
        payload = bytes.fromhex(hex_payload)
        records.append({"id": len(records), "name": name, "offset": len(data),
                        "payloadOffset": len(data) + 11, "payloadBytes": len(payload),
                        "frameHeaderOffset": len(data) + 12,
                        "payloadHex": hex_payload, "expected": expected, "purpose": purpose})
        data.extend(tag(payload))
    truncated = bytes(data) + tag_header(10) + b"\x2f\xff"
    partial = {"id": len(records), "name": "container-truncated", "offset": len(data),
               "payloadOffset": len(data) + 11, "declaredBytes": 10, "availableBytes": 2,
               "expected": {"state": "failed", "error": "detail_truncated"},
               "purpose": "Incomplete final container payload; complete prefix remains readable"}
    return {"mp3-details.flv": (bytes(data), records),
            "mp3-change.flv": (bytes(data), records),
            "mp3-truncated.flv": (truncated, records + [partial])}


def large_fixture(path: Path, size_mib: int) -> dict:
    digest = hashlib.sha256()
    offset, tag_id = 0, 0
    block = b"\0" * (1 << 20)
    with path.open("xb") as stream:
        def write(data: bytes) -> None:
            nonlocal offset
            stream.write(data)
            digest.update(data)
            offset += len(data)

        def zeros(count: int) -> None:
            while count:
                part = min(len(block), count)
                write(block[:part])
                count -= part

        write(HEADER)
        remaining = size_mib << 20
        while remaining:
            payload_size = min(0xFFFFFF, remaining)
            write(tag_header(payload_size, kind=18))
            zeros(payload_size)
            write(struct.pack(">I", 11 + payload_size))
            tag_id += 1
            remaining -= payload_size
        tail_offset = offset
        payload_size = 5 + TAIL_OPAQUE_BYTES
        write(tag_header(payload_size, dts=1000))
        write(bytes.fromhex("2ffffb9000"))
        zeros(TAIL_OPAQUE_BYTES)
        write(struct.pack(">I", 11 + payload_size))
        stream.flush()
    return {"name": path.name, "bytes": offset, "sha256": digest.hexdigest(),
            "physicallyWrittenFillerBytes": size_mib << 20,
            "tailOpaqueBytes": TAIL_OPAQUE_BYTES, "tags": tag_id + 1,
            "tailTag": {"id": tag_id, "offset": tail_offset, "payloadOffset": tail_offset + 11,
                        "frameHeaderOffset": tail_offset + 12, "payloadBytes": payload_size,
                        "opaqueOffset": tail_offset + 16, "opaqueBytes": TAIL_OPAQUE_BYTES,
                        "dts": 1000, "expected": normal("mpeg1", 2, 44100, 128, 1152, 417)}}


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
        record = {"name": name, "bytes": len(data), "sha256": hashlib.sha256(data).hexdigest(), "tags": records}
        if name == "mp3-change.flv":
            record["afterControlledMutation"] = {"state": "failed", "error": "source_changed"}
        files.append(record)
    if args.large_mib:
        files.append(large_fixture(args.output_dir / "mp3-large.flv", args.large_mib))
    manifest = {"format": "muxray-desktop-mp3-fixtures-v1", "files": files}
    with (args.output_dir / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(json.dumps({"files": [{"name": f["name"], "bytes": f["bytes"], "sha256": f["sha256"]} for f in files]}, indent=2))


if __name__ == "__main__":
    main()
