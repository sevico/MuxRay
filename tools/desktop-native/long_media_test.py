"""Bounded generation process, refusal and independent framing checks."""

from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import long_media as fixture


def tag(kind, payload, dts=0):
    header = bytes([kind]) + len(payload).to_bytes(3, "big") + (dts & 0xffffff).to_bytes(3, "big")
    header += bytes([dts >> 24]) + b"\0" * 3
    return header + payload + (len(payload) + 11).to_bytes(4, "big")


class LongMediaTests(unittest.TestCase):
    def test_recipe_has_explicit_deterministic_bounds(self):
        args = fixture.recipe(2400)
        for option, expected in (("-t", "2400"), ("-g", "50"), ("-bf", "2"),
                                 ("-threads:v", "1"), ("-threads:a", "1"), ("-sc_threshold", "0")):
            self.assertEqual(args[args.index(option) + 1], expected)
        self.assertIn("-n", args)
        for duration in (0, 1, 2401):
            with self.assertRaises(ValueError):
                fixture.recipe(duration)

    def test_existing_directory_and_relative_path_refused(self):
        with tempfile.TemporaryDirectory() as directory:
            with self.assertRaises(FileExistsError):
                fixture.generate(Path(sys.executable), Path(directory), 2)
        with self.assertRaises(ValueError):
            fixture.generate(Path(sys.executable), Path("relative-output"), 2)

    def test_failed_process_keeps_owned_artifacts(self):
        with tempfile.TemporaryDirectory() as directory:
            destination = Path(directory) / "new-run"

            def fake_run(argv, timeout):
                if argv[-1] == "-version":
                    return {"exitCode": 0, "failure": None, "stdout": "ffmpeg test", "stderr": "", "seconds": 0}
                Path(argv[-1]).write_bytes(b"partial")
                return {"exitCode": 1, "failure": None, "stdout": "", "stderr": "failure", "seconds": 0}

            with patch.object(fixture, "bounded_run", side_effect=fake_run):
                with self.assertRaisesRegex(ValueError, "partial output retained"):
                    fixture.generate(Path(sys.executable), destination, 2)
            self.assertEqual((destination / "generated-long-avc-aac-2s.flv").read_bytes(), b"partial")
            self.assertTrue((destination / "generation-failure.json").is_file())

    def test_process_timeout_and_output_limit(self):
        timeout = fixture.bounded_run([sys.executable, "-c", "import time;time.sleep(5)"], 0.1)
        self.assertEqual(timeout["failure"], "timeout")
        overflow = fixture.bounded_run([sys.executable, "-c", "import sys;sys.stderr.write('x'*100000)"], 5)
        self.assertEqual(overflow["failure"], "output_limit")
        self.assertEqual(len(overflow["stderr"]), fixture.OUTPUT_LIMIT)

    def test_framing_retains_only_anchors_and_rejects_damage(self):
        header = bytes.fromhex("464c5601050000000900000000")
        data = header + tag(9, bytes.fromhex("170000000001")) + tag(8, bytes.fromhex("af001190"))
        data += b"".join(tag(9, bytes.fromhex("170100005000"), i * 2000) for i in range(1026))
        data += tag(8, bytes.fromhex("af0100"), 2050000) + tag(9, bytes.fromhex("1702000000"), 2050000)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "test.flv"
            path.write_bytes(data)
            facts = fixture.inspect_framing(path)
            self.assertEqual(facts["tagCount"], 1030)
            self.assertEqual(facts["declaredKeyframes"], 1026)
            self.assertEqual(facts["firstKeyframeBeyondDesktopRetention"]["tagId"], 1026)
            self.assertEqual(facts["firstKeyframeBeyondDesktopRetention"]["dts"], 2048000)
            with self.assertRaisesRegex(ValueError, "Tag count"):
                fixture.inspect_framing(path, max_tags=100)
            path.write_bytes(data[:-1])
            with self.assertRaisesRegex(ValueError, "truncated"):
                fixture.inspect_framing(path)
            path.write_bytes(data[:-4] + b"\0" * 4)
            with self.assertRaisesRegex(ValueError, "PreviousTagSize"):
                fixture.inspect_framing(path)

    def test_duration_coverage_cannot_pass_partial_encoding(self):
        facts = {"counts": {"avcNALU": 50, "aacConfig": 1, "avcConfig": 1, "avcEOS": 1, "aacRaw": 95},
                 "declaredKeyframes": 1, "maxDTS": 2050}
        fixture.validate_duration(facts, 2)
        with self.assertRaisesRegex(ValueError, "video frame count"):
            fixture.validate_duration(facts, 2400)


if __name__ == "__main__":
    unittest.main()
