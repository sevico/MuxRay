"""Identity, attribution and measurement tests; Windows smoke uses owned helpers."""

import argparse
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

import resource_probe as probe


class FakeAPI:
    def __init__(self):
        self.entries = {}
        self.current = {}
        self.table = []
        self.closed = []
        self.unavailable = set()

    def add(self, pid, created, parent=0, exe=None):
        key = (pid, str(created))
        self.current[pid] = key
        self.table.append((pid, parent))
        self.entries[key] = {"identity": {"pid": pid, "created": str(created), "exe": str(exe or Path(__file__).resolve())},
                             "exited": False, "exitFileTime": None, "exitCode": None,
                             "kernel100ns": "0", "user100ns": "0", "privateBytes": 100, "workingSetBytes": 200, "handles": 3, "errors": []}
        return key

    def open(self, pid):
        if pid in self.unavailable or pid not in self.current:
            raise OSError("Process is unavailable")
        return self.current[pid]

    def close(self, handle):
        if handle is not None:
            self.closed.append(handle)

    def identity(self, handle, pid):
        return self.entries[handle]["identity"].copy()

    def lifecycle(self, handle):
        result = self.entries[handle]
        return {key: result[key] for key in ("exited", "exitFileTime", "exitCode", "kernel100ns", "user100ns")}

    def metrics(self, handle):
        result = self.entries[handle].copy()
        result.pop("identity")
        return result

    def processes(self):
        return self.table.copy()


class ProbeTests(unittest.TestCase):
    def test_attach_rejects_reused_pid_image_mismatch_and_exit(self):
        api = FakeAPI()
        key = api.add(1, 10)
        image = api.entries[key]["identity"]["exe"]
        self.assertEqual(probe.attach(api, 1, "10", image)[0], key)
        for created, exe in (("11", image), ("10", image + "-other")):
            with self.subTest(created=created, exe=exe), self.assertRaises(ValueError):
                probe.attach(api, 1, created, exe)
        api.entries[key].update(exited=True, exitFileTime="20", exitCode=0)
        with self.assertRaises(ValueError):
            probe.attach(api, 1, "10", image)
        self.assertEqual(api.closed, [key, key, key])

    def test_tree_includes_out_of_order_grandchild_excludes_unrelated_webview(self):
        api = FakeAPI()
        root = api.add(1, 100)
        api.add(3, 120, 2, Path("/child/msedgewebview2.exe"))
        api.add(2, 110, 1, Path("/child/msedgewebview2.exe"))
        api.add(4, 90, 1)  # Stale parent PID predates the actual root.
        api.add(5, 120, 90, Path("/other/msedgewebview2.exe"))
        tracker = probe.Tracker(api, root, api.identity(root, 1))
        try:
            tracker.discover()
            self.assertEqual(set(tracker.known), {"1:100", "2:110", "3:120"})
            self.assertEqual(tracker.known["3:120"].parent, "2:110")
            self.assertEqual(tracker.known["2:110"].group, "webview2")
        finally:
            tracker.close()

    def test_retained_creation_fences_parent_pid_reuse_but_tracks_orphan(self):
        api = FakeAPI()
        root = api.add(1, 100)
        child = api.add(2, 110, 1)
        tracker = probe.Tracker(api, root, api.identity(root, 1))
        try:
            tracker.discover()
            api.entries[child].update(exited=True, exitFileTime="200", exitCode=0)
            tracker.sample(1)
            api.table = [(1, 0), (3, 2), (4, 2)]
            api.add(3, 190, 2)  # Created before retained parent's exit.
            api.add(4, 220, 2)  # Child of an unrelated reused PID 2.
            tracker.discover()
            self.assertIn("3:190", tracker.known)
            self.assertNotIn("4:220", tracker.known)
            self.assertTrue(next(item for item in tracker.inventory() if item["pid"] == 2)["exitConfirmed"])
        finally:
            tracker.close()

    def test_access_failure_is_visible_and_not_silently_zero(self):
        api = FakeAPI()
        root = api.add(1, 100)
        api.add(2, 110, 1)
        api.unavailable.add(2)
        tracker = probe.Tracker(api, root, api.identity(root, 1))
        try:
            tracker.discover()
            self.assertEqual(tracker.discovery_failures, 1)
            self.assertEqual(tracker.discovery_errors[0]["pid"], 2)
            api.entries[root]["privateBytes"] = None
            self.assertIsNone(probe.aggregate(tracker.sample(1))["privateBytes"])
        finally:
            tracker.close()

    def test_cpu_precision_first_sample_delta_and_machine_normalization(self):
        api = FakeAPI()
        root = api.add(1, 100)
        api.entries[root]["kernel100ns"] = "9007199254740993"
        tracker = probe.Tracker(api, root, api.identity(root, 1), cores=8)
        try:
            first = tracker.sample(10)[0]
            self.assertIsNone(first["cpuPercent"])
            self.assertEqual(first["cpuTime100ns"], "9007199254740993")
            api.entries[root]["user100ns"] = "10000000"
            second = tracker.sample(10.5)[0]
            self.assertEqual(second["cpuDelta100ns"], "10000000")
            self.assertEqual(second["cpuPercent"], 200)
            self.assertEqual(second["cpuPercentOfMachine"], 25)
            api.entries[root]["user100ns"] = "0"
            regressed = tracker.sample(11)[0]
            self.assertIsNone(regressed["cpuPercent"])
            self.assertIn("CPU counter regressed", regressed["errors"])
        finally:
            tracker.close()

    def test_peaks_are_concurrent_group_sums_not_sum_of_individual_peaks(self):
        def row(memory):
            return {"group": "webview2", "exited": False, "privateBytes": memory, "workingSetBytes": memory,
                    "handles": 1, "cpuPercent": 0, "cpuPercentOfMachine": 0}
        peaks = {}
        for rows in ([row(100), row(10)], [row(10), row(100)]):
            probe.update_peaks(peaks, {"all": probe.aggregate(rows)})
        self.assertEqual(peaks["all"]["max"]["privateBytes"], 110)
        self.assertEqual(probe.aggregate([], "webview2")["privateBytes"], 0)

    def test_stage_requests_are_acknowledged_once_with_separate_times(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Path(directory)
            (run / "control").mkdir()
            request = {"runId": "a", "requestId": "r1", "action": "mark", "stage": "operator_observation", "requestedUTC": "2026-01-01T00:00:00Z"}
            probe.write_json(run / "control/001.request.json", request)
            stage, stop, acknowledgments = probe.consume_controls(run, "a", "unmarked", 3, 1.25)
            self.assertEqual(stage, "operator_observation")
            self.assertFalse(stop)
            self.assertEqual(acknowledgments[0]["requestedUTC"], request["requestedUTC"])
            self.assertNotEqual(acknowledgments[0]["observedUTC"], request["requestedUTC"])
            self.assertEqual(acknowledgments[0]["appliesFromSample"], 3)
            self.assertEqual(probe.consume_controls(run, "a", stage, 4, 2)[2], [])
            probe.write_json(run / "control/002.request.json", {**request, "action": "stop", "requestId": "r2"})
            self.assertTrue(probe.consume_controls(run, "a", stage, 4, 2)[1])

    def test_cross_run_controls_and_overwriting_evidence_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            run = Path(directory)
            (run / "control").mkdir()
            path = run / "control/001.request.json"
            probe.write_json(path, {"runId": "other", "action": "stop"})
            with self.assertRaises(ValueError):
                probe.consume_controls(run, "a", "unmarked", 0, 0)
            with self.assertRaises(FileExistsError):
                probe.write_json(path, {"runId": "a"})
            self.assertEqual(json.loads(path.read_text())["runId"], "other")

    def test_decimal_identity_and_duration_cli_bounds(self):
        self.assertEqual(probe.positive_decimal("18446744073709551615"), "18446744073709551615")
        for value in ("0", "01", "-1", "1.0", "18446744073709551616", "１２"):
            with self.subTest(value=value), self.assertRaises(argparse.ArgumentTypeError):
                probe.positive_decimal(value)
        args = probe.parser().parse_args(["record", "--pid", "1", "--created", "1", "--exe", "x", "--output-dir", "y"])
        self.assertEqual(args.interval_ms, 500)
        args.duration_seconds = 7200
        args.interval_ms = 250
        probe.validate_record(args)
        for field, value in (("duration_seconds", 7201), ("duration_seconds", float("nan")), ("duration_seconds", 0),
                             ("interval_ms", 249), ("interval_ms", 5001), ("exit_grace_seconds", 61), ("pid", 1 << 32)):
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                invalid = copy.copy(args)
                setattr(invalid, field, value)
                probe.validate_record(invalid)

    def test_status_replace_retries_windows_sharing_without_deleting_prior(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "status.json"
            probe.write_json(path, {"old": True})
            original_replace = os.replace
            attempts = []

            def sharing_then_replace(source, destination):
                attempts.append(1)
                if len(attempts) < 3:
                    self.assertEqual(json.loads(path.read_text()), {"old": True})
                    error = PermissionError("temporary reader sharing conflict")
                    error.winerror = 5
                    raise error
                original_replace(source, destination)

            with mock.patch.object(probe.os, "replace", side_effect=sharing_then_replace):
                probe.write_json(path, {"new": True}, replace=True)
            self.assertEqual(len(attempts), 3)
            self.assertEqual(json.loads(path.read_text()), {"new": True})
            self.assertEqual(list(Path(directory).glob("*.tmp")), [])

    def test_permanently_locked_status_does_not_discard_samples(self):
        api = FakeAPI()
        root = api.add(1, 10)
        api.add(os.getpid(), 20)
        image = Path(api.entries[root]["identity"]["exe"])
        original_write = probe.write_json

        def locked_status(path, value, replace=False):
            if path.name == "status.json":
                raise PermissionError("held by status reader")
            return original_write(path, value, replace)

        with tempfile.TemporaryDirectory() as directory, mock.patch.object(probe, "write_json", side_effect=locked_status):
            output = Path(directory) / "run"
            result = probe.record(argparse.Namespace(pid=1, created="10", exe=image, output_dir=output,
                                                    duration_seconds=.01, interval_ms=250, exit_grace_seconds=0), api)
            self.assertEqual(result["stopReason"], "duration_limit")
            self.assertIsNone(result["error"])
            self.assertGreater(result["statusWriteFailures"], 0)
            self.assertTrue((output / "summary.json").exists())
            self.assertGreater((output / "samples.jsonl").stat().st_size, 0)
            self.assertFalse(result["targetExitConfirmed"])

    def test_process_identity_budget_fails_without_admitting_unbounded_children(self):
        api = FakeAPI()
        root = api.add(1, 10)
        child = api.add(2, 20, 1)
        tracker = probe.Tracker(api, root, api.identity(root, 1))
        try:
            with mock.patch.object(probe, "MAX_IDENTITIES", 1), self.assertRaises(RuntimeError):
                tracker.discover()
            self.assertEqual(list(tracker.known), ["1:10"])
            self.assertIn(child, api.closed)
        finally:
            tracker.close()

    def test_existing_record_directory_is_preserved(self):
        api = FakeAPI()
        root = api.add(1, 10)
        image = Path(api.entries[root]["identity"]["exe"])
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            sentinel = output / "evidence.txt"
            sentinel.write_text("keep")
            with self.assertRaises(FileExistsError):
                probe.record(argparse.Namespace(pid=1, created="10", exe=image, output_dir=output), api)
            self.assertEqual(sentinel.read_text(), "keep")
            self.assertEqual(api.closed, [root])


@unittest.skipUnless(os.name == "nt", "Windows process metric API smoke")
class WindowsSmokeTests(unittest.TestCase):
    def test_owned_parent_child_metrics_and_natural_exit(self):
        child_code = "import time; data=bytearray(8<<20); time.sleep(1.8)"
        parent_code = "import subprocess,sys,time; child=subprocess.Popen([sys.executable,'-c'," + repr(child_code) + "],creationflags=subprocess.CREATE_NO_WINDOW); print(child.pid,flush=True); time.sleep(1.8); child.wait()"
        parent = subprocess.Popen([sys.executable, "-c", parent_code], stdout=subprocess.PIPE, text=True, creationflags=subprocess.CREATE_NO_WINDOW)
        try:
            child_pid = int(parent.stdout.readline())
            api = probe.WindowsAPI()
            handle = api.open(parent.pid)
            identity = api.identity(handle, parent.pid)
            api.close(handle)
            with tempfile.TemporaryDirectory(prefix="muxray-resource-test-") as directory:
                output = Path(directory) / "run"
                result = probe.record(argparse.Namespace(pid=parent.pid, created=identity["created"], exe=Path(identity["exe"]), output_dir=output,
                                                        duration_seconds=5, interval_ms=250, exit_grace_seconds=.3))
                self.assertIsNone(result["error"])
                self.assertTrue(result["targetExitConfirmed"])
                self.assertTrue(result["allObservedDescendantsExited"])
                self.assertEqual(result["stopReason"], "target_exit_grace_complete")
                self.assertIn(child_pid, [process["pid"] for process in result["processes"]])
                self.assertGreater(result["peaks"]["all"]["max"]["privateBytes"], 8 << 20)
                self.assertEqual(json.loads((output / "manifest.json").read_text())["target"], identity)
        finally:
            parent.wait(timeout=8)
            parent.stdout.close()

    def test_mark_stop_targets_sampler_and_leaves_observed_process_running(self):
        parent = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(4)"], creationflags=subprocess.CREATE_NO_WINDOW)
        sampler = None
        try:
            api = probe.WindowsAPI()
            handle = api.open(parent.pid)
            identity = api.identity(handle, parent.pid)
            api.close(handle)
            with tempfile.TemporaryDirectory(prefix="muxray-resource-control-") as directory:
                output = Path(directory) / "run"
                sampler = subprocess.Popen([sys.executable, "-B", str(Path(probe.__file__).resolve()), "record", "--pid", str(parent.pid),
                                            "--created", identity["created"], "--exe", identity["exe"], "--output-dir", str(output),
                                            "--duration-seconds", "3", "--interval-ms", "250"], stdout=subprocess.PIPE, stderr=subprocess.PIPE, creationflags=subprocess.CREATE_NO_WINDOW)
                deadline = time.monotonic() + 2
                while not (output / "status.json").exists() and time.monotonic() < deadline:
                    time.sleep(.05)
                self.assertTrue((output / "status.json").exists())
                acknowledged = probe.control(argparse.Namespace(command="mark", run_dir=output, stage="ready_observed", note="operator label", wait_seconds=2))
                self.assertEqual(acknowledged["stage"], "ready_observed")
                self.assertIn("observedUTC", acknowledged)
                stopped = probe.control(argparse.Namespace(command="stop", run_dir=output, wait_seconds=2))
                self.assertIn("observedUTC", stopped)
                stdout, stderr = sampler.communicate(timeout=5)
                self.assertEqual(sampler.returncode, 0, stderr.decode(errors="replace"))
                result = json.loads(stdout)
                self.assertEqual(result["stopReason"], "operator_stop")
                self.assertFalse(result["targetExitConfirmed"])
                self.assertIsNone(parent.poll())
                self.assertIn("ready_observed", result["stages"])
        finally:
            if sampler is not None:
                sampler.communicate(timeout=8)
            parent.wait(timeout=8)


if __name__ == "__main__":
    unittest.main()
