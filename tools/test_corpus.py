import json
from pathlib import Path
import struct
import sys
import tempfile
import unittest

from jsonschema import ValidationError

from corpus import (approved_packet_difference, bounded_run, compare_expected, file_identity, load_manifests,
                    packet_oracle, parse_report, raw_framing, safe_sample)


def fixture(payload=b"\xaf\x01\x12", dts=123):
    header = bytes([8]) + len(payload).to_bytes(3, "big") + (dts & 0xffffff).to_bytes(3, "big") + bytes([dts >> 24]) + b"\0\0\0"
    return b"FLV\x01\x04\0\0\0\x09\0\0\0\0" + header + payload + struct.pack(">I", 11 + len(payload))


class CorpusTests(unittest.TestCase):
    def test_report_schema_rejects_unknown_fields_and_wrong_types(self):
        path = Path(__file__).parent.parent / "testdata/golden/flv-basic-v1.json"
        original = path.read_text(encoding="utf-8")
        document = parse_report(original, "flv-scan-report-v1.schema.json")
        document["unexpected"] = True
        with self.assertRaises(ValidationError):
            parse_report(json.dumps(document), "flv-scan-report-v1.schema.json")
        del document["unexpected"]
        document["scan"]["complete"] = "true"
        with self.assertRaises(ValidationError):
            parse_report(json.dumps(document), "flv-scan-report-v1.schema.json")

    def test_raw_framing_packet_offsets_and_truncation(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "test.flv"
            path.write_bytes(fixture(dts=0x12345678))
            header, records, complete = raw_framing(path)
            self.assertTrue(complete)
            self.assertEqual(header["hasAudio"], True)
            self.assertEqual(records[0]["dts"], 0x12345678)
            self.assertEqual(packet_oracle(records), [{"codec_type": "audio", "dts": 0x12345678,
                "pts": 0x12345678, "pos": "13", "size": "1"}])
            path.write_bytes(fixture()[:-2])
            _, records, complete = raw_framing(path)
            self.assertFalse(complete)
            self.assertIsNone(records[0]["previous"])
            self.assertEqual(records[0]["availableDataSize"], 3)
            path.write_bytes(fixture()[:-5])
            _, records, complete = raw_framing(path)
            self.assertFalse(complete)
            self.assertEqual(records[0]["availableDataSize"], 2)
            self.assertEqual(packet_oracle(records), [])

    def test_paths(self):
        with tempfile.TemporaryDirectory() as temp:
            for name in ("", ".", "..", "../a", "..\\a", "C:a.flv", "a/b.flv", "a\\b.flv"):
                with self.assertRaises(ValueError):
                    safe_sample(Path(temp), name)
            self.assertEqual(safe_sample(Path(temp), "中文 sample.flv").parent, Path(temp).resolve())

    def test_hash_and_expected_mismatch(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "sample.flv"
            path.write_bytes(fixture())
            size, digest = file_identity(path)
            self.assertEqual(size, len(fixture()))
            self.assertEqual(len(digest), 64)
        document = {"schemaVersion": 1, "scan": {"complete": True}, "header": {"present": True}, "tags": [], "diagnostics": []}
        expected = {"reportSchemaVersion": 1, "scanComplete": True, "header": {"present": True}, "tagCount": 0, "diagnostics": []}
        compare_expected(document, expected)
        expected["tagCount"] = 1
        with self.assertRaises(ValueError):
            compare_expected(document, expected)

    def test_process_output_failure_timeout_and_limits(self):
        code, output, error = bounded_run([sys.executable, "-c", "print('ok')"], timeout=5)
        self.assertEqual((code, output.strip(), error), (0, b"ok", b""))
        code, _, _ = bounded_run([sys.executable, "-c", "raise SystemExit(7)"], timeout=5)
        self.assertEqual(code, 7)
        with self.assertRaisesRegex(ValueError, "timed out"):
            bounded_run([sys.executable, "-c", "import time; time.sleep(30)"], timeout=0.1)
        with self.assertRaisesRegex(ValueError, "output limit"):
            bounded_run([sys.executable, "-c", "import sys; sys.stdout.write('x'*100000)"], timeout=5, limit=1024)
        with self.assertRaisesRegex(ValueError, "output limit"):
            bounded_run([sys.executable, "-c", "import sys; sys.stderr.write('x'*100000)"], timeout=5, limit=1024)

    def test_registry_rejects_duplicates(self):
        sample = json.loads((Path(__file__).parent.parent / "testdata/manifests/example-minimal-header-only.sample.json").read_text())
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)
            sample["id"] = "test-first"
            (directory / "test-first.sample.json").write_text(json.dumps(sample))
            self.assertEqual(len(load_manifests(directory)), 1)
            sample["id"] = "test-second"
            (directory / "test-second.sample.json").write_text(json.dumps(sample))
            with self.assertRaisesRegex(ValueError, "duplicate sample hash"):
                load_manifests(directory)

    def test_difference_allowlist_is_hash_version_and_packet_exact(self):
        manifest = {"id": "derived-mp3-bad-previous", "file": {"sha256": "2f0be330e384c284910a81b414f7edf8bd4bf44763a198715b1d978cb64e3845"}}
        version = "ffprobe version 7.1.1-full_build-www.gyan.dev"
        packets = [{"pos": "13", "size": "417"}, {"pos": "446", "size": "418"}]
        self.assertEqual(approved_packet_difference(manifest, version, packets[1:], packets), "ffprobe-drops-mp3-with-invalid-previous-size")
        for actual, expected, tool_version in [([], packets, version), ([{"pos": "446", "size": "999"}], packets, version), (packets[1:], packets, "other")]:
            with self.assertRaises(ValueError):
                approved_packet_difference(manifest, tool_version, actual, expected)
        manifest["file"]["sha256"] = "0" * 64
        with self.assertRaises(ValueError):
            approved_packet_difference(manifest, version, packets[1:], packets)


if __name__ == "__main__":
    unittest.main()
