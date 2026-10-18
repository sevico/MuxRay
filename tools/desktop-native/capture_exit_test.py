"""Subprocess-only capture tests. These do not exercise Wails or a native GUI."""

import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest import mock

import capture_exit


class CaptureExitTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.root = Path(self.temporary.name)
        self.output = self.root / "capture"

    def tearDown(self):
        self.temporary.cleanup()

    def run_child(self, code, **kwargs):
        return capture_exit.capture(sys.executable, self.output, ["-c", code], **kwargs)

    def test_abrupt_nonzero_exit_preserves_separate_binary_streams(self):
        record = self.run_child("import os; os.write(1,b'out\\x00\\xff'); os.write(2,b'err\\xfe'); os._exit(1)")
        self.assertEqual(record["exitCode"], 1)
        self.assertTrue(record["exitConfirmed"])
        self.assertTrue(record["captureComplete"])
        self.assertEqual(capture_exit.collector_exit_code(record), 1)
        self.assertEqual((self.output / "stdout.bin").read_bytes(), b"out\x00\xff")
        self.assertEqual((self.output / "stderr.bin").read_bytes(), b"err\xfe")
        self.assertEqual(record["exeSha256"], capture_exit.sha256(Path(sys.executable).resolve()))
        self.assertIsNotNone(record["pid"])
        self.assertIsNotNone(record["launchRequestedUtc"])
        if os.name == "nt":
            self.assertGreater(int(record["creationFiletime"]), 0)
            self.assertIsNotNone(record["osStartedUtc"])
        saved = json.loads((self.output / "capture.json").read_text(encoding="utf-8"))
        self.assertEqual(saved, record)

    def test_caps_both_streams_but_drains_to_actual_exit(self):
        blocks = 40
        record = self.run_child(
            "import os\nfor i in range(40):\n os.write(1,b'A'*65536)\n os.write(2,b'B'*65536)\nos._exit(7)"
        )
        self.assertEqual(record["exitCode"], 7)
        self.assertTrue(record["captureComplete"])
        for name, byte in (("stdout", b"A"), ("stderr", b"B")):
            stream = record["streams"][name]
            self.assertEqual(stream["observedBytes"], blocks * 65536)
            self.assertEqual(stream["retainedBytes"], 1 << 20)
            self.assertEqual(stream["discardedBytes"], blocks * 65536 - (1 << 20))
            self.assertTrue(stream["truncated"])
            self.assertTrue(stream["eofObserved"])
            self.assertFalse(stream["tailUnknown"])
            self.assertTrue(stream["missingData"])
            raw = (self.output / stream["file"]).read_bytes()
            self.assertEqual(raw, byte * (1 << 20))
            self.assertEqual(stream["sha256"], hashlib.sha256(raw).hexdigest())
            self.assertEqual(stream["writtenBytes"], 1 << 20)
            self.assertEqual(stream["verifiedBytes"], len(raw))
            self.assertEqual(stream["persistedSha256"], stream["sha256"])
            self.assertTrue(stream["persistenceComplete"])

    def test_timeout_records_deadline_but_does_not_terminate_target(self):
        record = self.run_child(
            "import os,time; os.write(1,b'before'); time.sleep(.3); os.write(1,b'after'); os._exit(0)",
            timeout_seconds=0.05,
        )
        self.assertTrue(record["timedOut"])
        self.assertEqual(record["exitCode"], 0)
        self.assertTrue(record["captureComplete"])
        self.assertEqual((self.output / "stdout.bin").read_bytes(), b"beforeafter")
        deadline = json.loads((self.output / "capture-timeout.json").read_text(encoding="utf-8"))
        self.assertTrue(deadline["timedOut"])
        self.assertFalse(deadline["exitConfirmed"])
        self.assertIsNone(deadline["exitCode"])
        self.assertEqual(capture_exit.collector_exit_code(record), 2)

    def test_inherited_pipe_tail_is_not_reported_as_complete(self):
        record = self.run_child(
            "import os,subprocess,sys; subprocess.Popen([sys.executable,'-c','import time; time.sleep(.6)'],"
            "stdout=sys.stdout,stderr=sys.stderr); os.write(1,b'parent'); os._exit(0)",
            drain_timeout_seconds=0.05,
        )
        self.assertTrue(record["exitConfirmed"])
        self.assertEqual(record["exitCode"], 0)
        self.assertTrue(record["drainTimedOut"])
        self.assertFalse(record["captureComplete"])
        self.assertTrue(any(stream["tailUnknown"] for stream in record["streams"].values()))
        self.assertEqual(capture_exit.collector_exit_code(record), 2)
        before = (self.output / "stdout.bin").read_bytes()
        time.sleep(0.8)  # Let the fixture descendant exit naturally, never kill it.
        self.assertEqual((self.output / "stdout.bin").read_bytes(), before)

    def test_environment_is_inherited_without_dumping_it_to_receipt(self):
        with mock.patch.dict(os.environ, {"MUXRAY_CAPTURE_TEST": "inherited-value"}):
            record = self.run_child("import os; os.write(1,os.environ['MUXRAY_CAPTURE_TEST'].encode())")
        self.assertEqual((self.output / "stdout.bin").read_bytes(), b"inherited-value")
        self.assertNotIn("inherited-value", json.dumps(record))

    def test_existing_evidence_directory_is_rejected(self):
        self.output.mkdir()
        keep = self.output / "keep"
        keep.write_bytes(b"unchanged")
        with self.assertRaises(FileExistsError):
            self.run_child("raise SystemExit(99)")
        self.assertEqual(keep.read_bytes(), b"unchanged")
        self.assertEqual(list(self.output.iterdir()), [keep])

    def test_launch_failure_is_recorded_without_claiming_exit(self):
        invalid = self.root / "invalid.exe"
        invalid.write_bytes(b"not an executable")
        record = capture_exit.capture(invalid, self.output)
        self.assertEqual(record["phase"], "launch_failed")
        self.assertFalse(record["exitConfirmed"])
        self.assertIsNone(record["exitCode"])
        self.assertFalse(record["captureComplete"])
        self.assertEqual(capture_exit.collector_exit_code(record), 2)

    def test_cli_preserves_target_exit_separately(self):
        result = subprocess.run(
            [sys.executable, str(Path(capture_exit.__file__)), "--exe", sys.executable,
             "--output", str(self.output), "--", "-c", "import os; os.write(2,b'last'); os._exit(23)"],
            capture_output=True, timeout=10,
        )
        self.assertEqual(result.returncode, 1, result.stderr)
        summary = json.loads(result.stdout)
        self.assertEqual(summary["exitCode"], 23)
        self.assertTrue(summary["captureComplete"])
        self.assertEqual((self.output / "stderr.bin").read_bytes(), b"last")

    def test_help_discloses_privacy_timeout_and_gui_limits(self):
        result = subprocess.run([sys.executable, str(Path(capture_exit.__file__)), "--help"],
                                capture_output=True, timeout=10)
        self.assertEqual(result.returncode, 0)
        for text in (b"CAN contain paths", b"not hidden", b"NOT permission to terminate", b"never uploaded"):
            self.assertIn(text, result.stdout)

    def test_blocked_disk_sync_does_not_block_pipes_exit_or_observation_deadline(self):
        release = threading.Event()
        blocked = self.root / "disk-blocked"
        writers = []
        original = capture_exit.DiskWriter._sync

        def stalled(writer, file):
            if writer not in writers:
                writers.append(writer)
            blocked.write_bytes(b"ready")
            if not release.wait(4):
                raise OSError("Synthetic disk stall was not released")
            return original(writer, file)

        code = (
            "import os,time\n"
            "deadline=time.monotonic()+3\n"
            f"while not os.path.exists({str(blocked)!r}) and time.monotonic()<deadline: time.sleep(.01)\n"
            f"if not os.path.exists({str(blocked)!r}): os._exit(98)\n"
            "time.sleep(.15)\n"
            "for i in range(48):\n os.write(1,b'A'*65536)\n os.write(2,b'B'*65536)\n"
            "os._exit(0)"
        )
        started = time.monotonic()
        try:
            with mock.patch.object(capture_exit.DiskWriter, "_sync", stalled):
                record = self.run_child(code, timeout_seconds=.05, writer_wait_seconds=.1)
                self.assertEqual(record["exitCode"], 0)
                self.assertTrue(record["exitConfirmed"])
                self.assertTrue(record["timedOut"])
                self.assertTrue(record["writerTimedOut"])
                self.assertTrue(record["receiptMayBeStale"])
                self.assertFalse(record["captureComplete"])
                self.assertFalse(record["persistenceComplete"])
                self.assertLess(time.monotonic() - started, 2)
                for stream in record["streams"].values():
                    self.assertEqual(stream["observedBytes"], 48 * 65536)
                    self.assertEqual(stream["retainedBytes"], 1 << 20)
                    self.assertTrue(stream["eofObserved"])
                    self.assertTrue(stream["missingData"])
                self.assertFalse(release.is_set())
                self.assertTrue(writers)
        finally:
            release.set()
            for writer in writers:
                self.assertTrue(writer.done.wait(3))

    def test_blocked_receipt_write_does_not_block_pipe_drain(self):
        release = threading.Event()
        blocked = self.root / "receipt-blocked"
        original = capture_exit.write_json
        writers = []
        original_init = capture_exit.DiskWriter.__init__

        def remember(writer, *args):
            original_init(writer, *args)
            writers.append(writer)

        def stalled(path, value):
            if threading.current_thread().name == "capture-disk-writer":
                blocked.write_bytes(b"ready")
                if not release.wait(4):
                    raise OSError("Synthetic receipt stall was not released")
            return original(path, value)

        code = (
            "import os,time\ndeadline=time.monotonic()+3\n"
            f"while not os.path.exists({str(blocked)!r}) and time.monotonic()<deadline: time.sleep(.01)\n"
            f"if not os.path.exists({str(blocked)!r}): os._exit(98)\n"
            "for i in range(24):\n os.write(1,b'C'*65536)\n os.write(2,b'D'*65536)\n"
            "os._exit(4)"
        )
        try:
            with mock.patch.object(capture_exit.DiskWriter, "__init__", remember), mock.patch.object(
                    capture_exit, "write_json", stalled):
                record = self.run_child(code, writer_wait_seconds=.1)
                self.assertEqual(record["exitCode"], 4)
                self.assertTrue(record["writerTimedOut"])
                self.assertFalse(record["captureComplete"])
                self.assertEqual(capture_exit.collector_exit_code(record), 2)
                for stream in record["streams"].values():
                    self.assertEqual(stream["observedBytes"], 24 * 65536)
                    self.assertTrue(stream["eofObserved"])
        finally:
            release.set()
            for writer in writers:
                self.assertTrue(writer.done.wait(3))

    def test_short_writes_are_completed_and_read_back(self):
        calls = []

        def short_write(writer, file, block):
            calls.append(len(block))
            return file.write(block[:7])

        with mock.patch.object(capture_exit.DiskWriter, "_write_chunk", short_write):
            record = self.run_child("import os; os.write(1,b'prefix'*100); os.write(2,b'last'); os._exit(0)")
        self.assertTrue(record["captureComplete"])
        self.assertGreater(len(calls), 2)
        for stream in record["streams"].values():
            self.assertEqual(stream["verifiedBytes"], stream["retainedBytes"])
            self.assertEqual(stream["persistedSha256"], stream["sha256"])

    def test_disk_write_failure_keeps_draining_and_marks_prefix_incomplete(self):
        def failed_stdout(writer, file, block):
            if file.name.endswith("stdout.bin"):
                if file.tell() >= 3:
                    raise OSError("Synthetic disk full")
                return file.write(block[:3])
            return file.write(block)

        with mock.patch.object(capture_exit.DiskWriter, "_write_chunk", failed_stdout):
            record = self.run_child(
                "import os\nfor i in range(20):\n os.write(1,b'A'*65536)\n os.write(2,b'B'*65536)\nos._exit(0)"
            )
        self.assertEqual(record["exitCode"], 0)
        self.assertFalse(record["captureComplete"])
        self.assertFalse(record["persistenceComplete"])
        stream = record["streams"]["stdout"]
        self.assertEqual(stream["observedBytes"], 20 * 65536)
        self.assertEqual(stream["retainedBytes"], 1 << 20)
        self.assertEqual(stream["writtenBytes"], 3)
        self.assertEqual((self.output / "stdout.bin").stat().st_size, 3)
        self.assertTrue(stream["eofObserved"])
        self.assertIsNotNone(stream["writeError"])
        self.assertIsNone(stream["persistedSha256"])
        self.assertTrue(stream["missingData"])
        self.assertTrue(record["streams"]["stderr"]["persistenceComplete"])

    def test_readback_mismatch_cannot_claim_persistence_complete(self):
        original = capture_exit.DiskWriter._sync

        def corrupt(writer, file):
            original(writer, file)
            if file.name.endswith("stdout.bin") and file.tell() > 0:
                with Path(file.name).open("r+b") as altered:
                    altered.write(b"!")

        with mock.patch.object(capture_exit.DiskWriter, "_sync", corrupt):
            record = self.run_child("import os; os.write(1,b'original'); os._exit(0)")
        stream = record["streams"]["stdout"]
        self.assertEqual(stream["verifiedBytes"], stream["retainedBytes"])
        self.assertNotEqual(stream["persistedSha256"], stream["sha256"])
        self.assertIsNotNone(stream["writeError"])
        self.assertFalse(stream["persistenceComplete"])
        self.assertFalse(record["captureComplete"])


if __name__ == "__main__":
    unittest.main()
