"""Independent framing and fact checks; this is not a substitute for the Go analyzer."""

import hashlib
from pathlib import Path
import tempfile
import unittest

import summary_fixtures as fixture


class SummaryFixtureTests(unittest.TestCase):
    def setUp(self):
        self.files = fixture.fixtures()

    def test_complete_framing_and_named_coordinates(self):
        for name, (data, records, _) in self.files.items():
            if name == "summary-truncated.flv":
                continue
            self.assertEqual(data[:13], fixture.HEADER)
            offset = 13
            for tag_id, row in enumerate(records):
                self.assertEqual((row["id"], row["offset"], row["payloadOffset"]), (tag_id, offset, offset + 11))
                size = int.from_bytes(data[offset + 1:offset + 4], "big")
                self.assertEqual(size, row["payloadBytes"])
                self.assertEqual(data[offset], row["tagType"])
                dts = int.from_bytes(data[offset + 4:offset + 7], "big") | data[offset + 7] << 24
                self.assertEqual(dts, row["dts"])
                self.assertEqual(int.from_bytes(data[offset + 11 + size:offset + 15 + size], "big"), 11 + size)
                offset += 15 + size
            self.assertEqual(offset, len(data))

    def test_mixed_source_order_and_parameter_changes(self):
        data, rows, _ = self.files["summary-mixed.flv"]
        self.assertEqual(len(rows), 20)
        self.assertEqual([r["dts"] for r in rows if r["name"].startswith("aac-") and "config" not in r["name"]], [0, 20, 20, 10, 200])
        self.assertEqual([r["dts"] for r in rows if r["name"].startswith("mp3-")], [100, 126, 152, 152, 140])
        self.assertEqual(fixture.avcc(4)[9] & 3, 3)
        self.assertEqual(fixture.avcc(2)[9] & 3, 1)
        self.assertEqual(fixture.video(False, -10)[2:5], bytes.fromhex("fffff6"))
        self.assertEqual(data[rows[9]["payloadOffset"]:rows[9]["payloadOffset"] + 4], bytes.fromhex("af001190"))
        self.assertEqual(self.files["summary-change.flv"], self.files["summary-mixed.flv"])

    def test_retention_exceeds_each_cap_and_page(self):
        _, rows, expected = self.files["summary-retention.flv"]
        counts = {name: sum(r["name"] == name for r in rows)
                  for name in ("aac-config", "avc-config", "declared-keyframe", "mp3-header")}
        self.assertEqual(counts, {"aac-config": 130, "avc-config": 130, "declared-keyframe": 1100, "mp3-header": 300})
        for retained in expected["lengths"].values():
            self.assertGreater(retained, 100)
        self.assertEqual(rows[260 + 1023]["name"], "declared-keyframe")
        self.assertEqual(rows[1360 + 255]["name"], "mp3-header")
        _, diagnostics, diagnostic_expected = self.files["summary-diagnostics.flv"]
        self.assertEqual(len(diagnostics), 300)
        self.assertEqual(diagnostic_expected["lengths"]["analysisDiagnostics"], 256)

    def test_incomplete_container_keeps_only_real_bytes(self):
        original, _, _ = self.files["summary-mixed.flv"]
        data, rows, _ = self.files["summary-truncated.flv"]
        self.assertEqual(data[:len(original)], original)
        self.assertEqual(len(rows), 21)
        self.assertEqual(data[rows[-1]["payloadOffset"]:], bytes.fromhex("af00"))
        self.assertEqual(int.from_bytes(data[rows[-1]["offset"] + 1:rows[-1]["offset"] + 4], "big"), 10)

    def test_streamed_many_tags_and_no_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "many.flv"
            metadata = fixture.many_tags(path, 10003)
            data = path.read_bytes()
            self.assertEqual(len(data), 13 + 10003 * 15 + 5)
            self.assertEqual(metadata["sha256"], hashlib.sha256(data).hexdigest())
            self.assertEqual(metadata["tailTag"]["offset"], 13 + 10002 * 15)
            self.assertEqual(data[metadata["tailTag"]["payloadOffset"]:-4], bytes.fromhex("2ffffb9000"))
            with self.assertRaises(FileExistsError):
                fixture.many_tags(path, 32)
            self.assertEqual(path.read_bytes(), data)


if __name__ == "__main__":
    unittest.main()
