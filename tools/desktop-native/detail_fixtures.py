"""Generate deterministic, synthetic AMF0/AAC desktop acceptance fixtures.

No downloads, codecs or user media. The output directory must be new.
Payloads exercise syntax; the AAC raw packet is opaque test data, not playable.
"""

import argparse
import hashlib
import json
from pathlib import Path
import struct


def amf_string(value: str) -> bytes:
    raw = value.encode("utf-8")
    return b"\x02" + struct.pack(">H", len(raw)) + raw


def member(name: str, value: bytes) -> bytes:
    raw = name.encode("utf-8")
    return struct.pack(">H", len(raw)) + raw + value


def tag(kind: int, payload: bytes, dts: int = 0) -> bytes:
    return (bytes([kind]) + len(payload).to_bytes(3, "big")
            + (dts & 0xFFFFFF).to_bytes(3, "big") + bytes([dts >> 24])
            + b"\0\0\0" + payload + struct.pack(">I", 11 + len(payload)))


def fixtures() -> dict[str, bytes]:
    header = b"FLV\x01\x04\0\0\0\x09\0\0\0\0"
    metadata = amf_string("onMetaData") + b"\x03" + b"".join([
        member("title", amf_string("MuxRay 桌面详情")),
        member("duration", b"\x00" + struct.pack(">d", 1.25)),
        member("stereo", b"\x01\x01"),
        member("audio", b"\x03" + member("codec", amf_string("AAC-LC")) + b"\0\0\x09"),
    ]) + b"\0\0\x09"
    tags = [
        tag(18, metadata),
        tag(8, bytes.fromhex("af001210")),  # AAC-LC, 44100 Hz, stereo ASC.
        tag(8, bytes.fromhex("af0111223344"), 23),
        tag(18, amf_string("broken") + b"\x00\x3f"),  # Short AMF number.
        tag(8, bytes.fromhex("af0012")),  # Short ASC.
        tag(9, bytes.fromhex("1701000000")),  # AVC NALU without preceding configuration.
        tag(18, amf_string("onPreview") + amf_string("预览" * 200)),
        tag(18, amf_string("onBudget") + b"\x0a" + struct.pack(">I", 600) + b"\x05" * 600),
    ]
    complete = header + b"".join(tags)
    # Header declares ten payload bytes, but the file contains only two.
    partial = complete + bytes.fromhex("0800000a00000000000000af00")
    return {"details.flv": complete, "details-truncated.flv": partial,
            "details-change.flv": complete}


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", required=True, type=Path)
    target = parser.parse_args().output_dir
    if not target.is_absolute():
        parser.error("Use an absolute, new output directory.")
    target.mkdir(parents=True, exist_ok=False)
    records = []
    for name, data in fixtures().items():
        with (target / name).open("xb") as stream:
            stream.write(data)
        records.append({"name": name, "bytes": len(data),
                        "sha256": hashlib.sha256(data).hexdigest()})
    manifest = {"format": "muxray-desktop-details-fixtures-v1", "files": records,
                "tags": ["AMF0 metadata", "AAC-LC ASC 44100 Hz stereo",
                         "AAC opaque raw", "damaged AMF0", "damaged ASC",
                         "AVC NALU missing configuration", "UTF-8 preview", "field budget"],
                "truncatedExtraTag": "ID 8: declared 10 bytes, available 2"}
    with (target / "fixtures.json").open("x", encoding="utf-8") as stream:
        json.dump(manifest, stream, ensure_ascii=False, indent=2)
        stream.write("\n")
    print(json.dumps(manifest, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
