"""Offline identity, missing-data, bounded-cadence and exit regression tests."""

import argparse
import json
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest

import handle_probe as probe


class Clock:
    def __init__(self):
        self.now = 0.0
        self.oversleep = 0.0

    def __call__(self):
        return self.now

    def sleep(self, seconds):
        self.now += seconds + self.oversleep
        self.oversleep = 0.0

    def utc(self):
        return f"fake-utc-{self.now:.3f}"


class FakeAPI:
    def __init__(self, clock, identity):
        self.clock, self.expected = clock, identity
        self.exit_at = None
        self.created = identity["created"]
        self.metric_missing = False
        self.unknown_type = False
        self.thread_failure = False
        self.open_failure = False
        self.metrics_duration = 0.0
        self.change_identity_at = None
        self.closed = []

    def open(self, pid):
        if self.open_failure:
            raise OSError("Target absent or access denied")
        return "retained-process-handle"

    def close(self, handle):
        self.closed.append(handle)

    def identity(self, handle, pid):
        created = self.created
        if self.change_identity_at is not None and self.clock() >= self.change_identity_at:
            created = "999"
        return {**self.expected, "created": created}

    def lifecycle(self, handle):
        exited = self.exit_at is not None and self.clock() >= self.exit_at
        return {"exited": exited, "exitCode": 0 if exited else None, "exitFileTime": "9999" if exited else None}

    def metrics(self, handle):
        self.clock.now += self.metrics_duration
        return {"handles": None if self.metric_missing else 7, "privateBytes": 100, "workingSetBytes": 200, "errors": []}

    def threads(self, pid):
        if self.thread_failure:
            raise OSError("Thread enumeration unavailable")
        return 3

    def gui(self, handle):
        return {"gdiObjects": 0, "userObjects": 2}

    def handles(self, handle):
        return {"handleTypes": [{"typeIndex": 3, "typeName": None if self.unknown_type else "Event", "count": 7,
                                 "typeQueryFailures": ["duplicate failed"] if self.unknown_type else []}],
                "snapshotHandleCount": 7, "typeCountsSum": 7, "handleValuesByTypeIndex": {"3": [1, 2, 3, 4, 5, 6, 7]}}


class HandleProbeTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        directory = Path(self.temporary.name)
        self.exe = directory / "MuxRay.exe"
        self.exe.write_bytes(b"offline identity fixture")
        self.args = SimpleNamespace(pid=10, created="100", exe=self.exe, run_dir=directory / "run",
                                    interval_seconds=5.0, duration_seconds=20.0)
        self.identity = {"pid": 10, "created": "100", "exe": str(self.exe)}
        self.clock = Clock()
        self.api = FakeAPI(self.clock, self.identity)

    def record(self):
        result = probe.record(self.args, self.api, self.clock, self.clock.sleep, self.clock.utc)
        self.assertEqual(json.loads((self.args.run_dir / "summary.json").read_text()), result)
        return result

    def samples(self):
        return [json.loads(line) for line in (self.args.run_dir / "samples.jsonl").read_text().splitlines()]

    def test_bounded_record_has_final_boundary_sample_and_retained_handle(self):
        result = self.record()
        self.assertTrue(result["complete"])
        self.assertEqual(result["stopReason"], "duration_reached")
        self.assertFalse(result["targetExitConfirmed"])
        self.assertEqual([row["elapsedSeconds"] for row in self.samples()], [0, 5, 10, 15, 20])
        self.assertEqual(result["maxSampleGapSeconds"], 5)
        self.assertEqual(self.api.closed, ["retained-process-handle"])

    def test_natural_exit_recorded_without_zero_filling(self):
        self.api.exit_at = 11
        result = self.record()
        self.assertTrue(result["complete"])
        self.assertTrue(result["targetExitConfirmed"])
        self.assertEqual(result["stopReason"], "target_exit")
        self.assertEqual(result["liveSamples"], 3)
        final = self.samples()[-1]
        self.assertEqual(final["status"], "exited")
        self.assertIsNone(final["processMetrics"])
        self.assertIsNone(final["snapshotHandleCount"])

    def test_exit_during_snapshot_is_explicit_partial_endpoint(self):
        self.api.exit_at = 8
        self.api.metrics_duration = 3
        result = self.record()
        self.assertTrue(result["complete"])
        self.assertEqual(result["liveSamples"], 1)
        self.assertEqual(self.samples()[-1]["status"], "exited_during_snapshot")
        self.assertTrue(result["targetExitConfirmed"])

    def test_already_exited_is_not_a_completed_record(self):
        self.api.exit_at = 0
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertEqual(result["samples"], 0)
        self.assertIn("already exited", result["error"])
        self.assertEqual(len(self.api.closed), 1)

    def test_pid_reuse_at_attach_is_rejected_and_handle_closed(self):
        self.api.created = "101"
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertEqual(result["samples"], 0)
        self.assertIn("creation identity", result["error"])
        self.assertEqual(len(self.api.closed), 1)

    def test_identity_change_stops_recording_instead_of_following_pid(self):
        self.api.change_identity_at = 10
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertEqual(result["samples"], 2)
        self.assertIn("identity mismatch", result["error"])
        self.assertEqual(result["stopReason"], "error")

    def test_executable_and_pid_are_both_checked(self):
        for replacement in ({"pid": 11}, {"exe": str(self.exe.with_name("Other.exe"))}):
            with self.subTest(replacement=replacement), self.assertRaisesRegex(ValueError, "identity mismatch"):
                probe.check_identity({**self.identity, **replacement}, self.identity)

    def test_missing_target_creates_failure_evidence(self):
        self.api.open_failure = True
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertIn("Target absent", result["error"])
        self.assertTrue((self.args.run_dir / "manifest.json").exists())
        self.assertEqual(self.api.closed, [])

    def test_missing_metric_is_not_zero_or_complete(self):
        self.api.metric_missing = True
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertEqual(result["failedLiveSamples"], 5)
        self.assertIsNone(self.samples()[0]["processMetrics"]["handles"])

    def test_unknown_type_records_index_and_failure(self):
        self.api.unknown_type = True
        result = self.record()
        self.assertFalse(result["complete"])
        row = self.samples()[0]
        self.assertIn("Unresolved object type index 3 (7 handles)", row["errors"])
        self.assertEqual(row["handleTypes"][0]["typeQueryFailures"], ["duplicate failed"])

    def test_thread_failure_is_explicit_and_other_metrics_survive(self):
        self.api.thread_failure = True
        result = self.record()
        self.assertFalse(result["complete"])
        row = self.samples()[0]
        self.assertIsNone(row["threadCount"])
        self.assertEqual(row["snapshotHandleCount"], 7)
        self.assertIn("Thread enumeration unavailable", row["errors"][0])

    def test_large_gap_is_not_complete_and_no_catchup_burst_occurs(self):
        self.clock.oversleep = 7
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertEqual(result["maxSampleGapSeconds"], 12)
        self.assertEqual([row["elapsedSeconds"] for row in self.samples()], [0, 12, 17, 20])

    def test_slow_query_duration_is_visible(self):
        self.api.metrics_duration = 11
        result = self.record()
        self.assertFalse(result["complete"])
        self.assertEqual(result["maxSampleDurationSeconds"], 11)
        self.assertGreaterEqual(result["elapsedSeconds"], 20)

    def test_existing_evidence_directory_is_never_overwritten(self):
        self.args.run_dir.mkdir()
        evidence = self.args.run_dir / "manifest.json"
        evidence.write_text("original evidence")
        with self.assertRaises(FileExistsError):
            self.record()
        self.assertEqual(evidence.read_text(), "original evidence")
        self.assertEqual(self.api.closed, [])

    def test_nonfinite_or_nonpositive_interval_rejected(self):
        for text in ("nan", "inf", "-inf", "0", "-1"):
            with self.subTest(text=text), self.assertRaises(argparse.ArgumentTypeError):
                probe.positive_seconds(text)


if __name__ == "__main__":
    unittest.main()
