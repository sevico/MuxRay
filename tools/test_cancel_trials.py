import unittest
from unittest.mock import patch
from pathlib import Path
import tempfile
import json

from cancel_trials import distribution, run_trials


class CancelTrialsTests(unittest.TestCase):
    def test_distribution_and_bad_values(self):
        self.assertEqual(distribution([4, 1, 2, 3]), {"median": 2.5, "p95": 4, "max": 4, "values": [4, 1, 2, 3]})
        for values in ([], [-1], [float("nan")], [float("inf")]):
            with self.assertRaises(ValueError):
                distribution(values)

    def test_failures_preserve_completed_trials_and_no_false_success(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "fixture.flv"
            path.write_bytes(b"x" * 1513)
            tool = Path(temp) / "tool.exe"
            tool.write_bytes(b"test")
            trial = {"sizeBytes": 1513, "tagCount": 100000, "coldCancelMs": 607.1234}
            with patch("cancel_trials.bounded_run", side_effect=[(0, json.dumps(trial).encode(), b""), (1, b"", b"failed")]):
                report = run_trials(tool, path, 2, legacy=True)
            self.assertFalse(report["complete"])
            self.assertEqual(report["trials"], [trial])
            self.assertEqual(report["failures"][0]["trial"], 1)
            self.assertEqual(report["distributions"]["coldCancelMs"]["max"], 607.1234)
            for count in (0, 101):
                with self.assertRaises(ValueError):
                    run_trials(tool, path, count)
            del trial["coldCancelMs"]
            with patch("cancel_trials.bounded_run", return_value=(0, json.dumps(trial).encode(), b"")):
                report = run_trials(tool, path, 1, legacy=True)
            self.assertFalse(report["complete"])
            self.assertEqual(report["trials"], [])

    def test_saved_evidence_counts_statistics_and_exit_contract(self):
        directory = Path(__file__).parent.parent / "docs/validation/20260905/cancellation"
        for name, count in (("cancel-cold-lazy", 30), ("cancel-cold-primed", 30), ("cancel-after-scans", 30), ("cancel-legacy-controls", 10)):
            report = json.loads((directory / (name + ".json")).read_text(encoding="utf-8"))
            self.assertTrue(report["complete"])
            self.assertEqual(report["failures"], [])
            self.assertEqual(len(report["trials"]), count)
            self.assertEqual(report["requestedTrials"], count)
            for key, stats in report["distributions"].items():
                self.assertEqual(stats, distribution([t[key] for t in report["trials"]]))
        native = []
        for name, count in (("ctrlc-first-smoke", 1), ("ctrlc-matrix", 21), ("ctrlc-matrix-verified", 21)):
            report = json.loads((directory / (name + ".json")).read_text(encoding="utf-8"))
            self.assertTrue(report["complete"])
            self.assertEqual(report["failures"], [])
            self.assertEqual(len(report["trials"]), count)
            events = [t for t in report["trials"] if t["event"]]
            self.assertEqual(report["eventToExitAndDrainMs"], distribution([t["eventToExitAndDrainMs"] for t in events]))
            native.extend(events)
        self.assertEqual(len(native), 37)
        for trial in native:
            self.assertEqual(trial["event"], "CTRL_C_EVENT")
            self.assertEqual(trial["exitCode"], 130)
            self.assertTrue(trial["discardStdout"])
            self.assertFalse(trial["forcedTermination"])
            self.assertTrue(trial["consoleOwnershipChecked"])
            self.assertEqual(trial["consoleMembersAfterExit"], 1)
