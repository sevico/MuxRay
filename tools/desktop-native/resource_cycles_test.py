"""Adversarial offline evidence tests; no application or live sampler required."""

from datetime import datetime, timedelta
from pathlib import Path
import tempfile
import unittest

import resource_cycles as cycles
import resource_probe as probe


def fixture(count=2):
    manifest = {"schemaVersion": 1, "runId": "fixture-run", "startedUTC": "2026-10-01T00:00:00+00:00",
        "target": {"pid": 10, "created": "100", "exe": "C:\\app\\MuxRay.exe"},
        "intervalMs": 500, "logicalProcessors": 16, "exeSHA256": "a" * 64}
    base = datetime.fromisoformat(manifest["startedUTC"])
    stamp = lambda seconds: (base + timedelta(seconds=seconds)).isoformat()
    labels = [(0, "baseline-idle")]
    for cycle in range(1, count + 1):
        labels += [(35 + (cycle - 1) * 45 + i, f"cycle-{cycle:02d}-{phase}") for i, phase in enumerate(cycles.PHASES)]
    exit_request = labels[-1][0] + 32
    labels.append((exit_request, "exit-request"))
    exit_time = exit_request + 1
    marks = [{"runId": manifest["runId"], "requestId": f"request-{i}", "action": "mark", "stage": label,
        "requestedUTC": stamp(second), "observedUTC": stamp(second), "observedElapsedSeconds": float(second),
        "appliesFromSample": second * 2} for i, (second, label) in enumerate(labels)]
    inventory = [{**manifest["target"], "identity": "10:100", "parentIdentity": None, "group": "root"},
        {"pid": 11, "created": "110", "exe": "C:\\wv\\msedgewebview2.exe", "identity": "11:110", "parentIdentity": "10:100", "group": "webview2"}]
    samples = []
    for index in range((exit_time + 5) * 2 + 1):
        elapsed = index / 2
        stage = [label for second, label in labels if second <= elapsed][-1]
        rows = []
        for identity in inventory if elapsed <= exit_time else []:
            exited = elapsed == exit_time
            rows.append({**identity, "exited": exited, "privateBytes": None if exited else 1000 + index,
                "workingSetBytes": None if exited else 2000 + index, "handles": None if exited else 20,
                "cpuPercent": None if index == 0 else 10.0, "cpuPercentOfMachine": None if index == 0 else .625,
                "errors": [], "exitFileTime": "10000" if exited else None, "exitCode": 0 if exited else None,
                "kernel100ns": str(index * 100), "user100ns": str(index * 200)})
        groups = {group: probe.aggregate(rows, None if group == "all" else group) for group in cycles.GROUPS}
        samples.append({"index": index, "observedUTC": stamp(elapsed), "elapsedSeconds": elapsed,
            "stage": stage, "sampleLagSeconds": 0.0, "processes": rows, "groups": groups,
            "treeCoverageComplete": True, "discoveryErrors": []})
    summary = {"schemaVersion": 1, "runId": manifest["runId"], "startedUTC": manifest["startedUTC"],
        "finishedUTC": stamp(samples[-1]["elapsedSeconds"] + .01), "elapsedSeconds": samples[-1]["elapsedSeconds"] + .01,
        "stopReason": "target_exit_grace_complete", "error": None, "samples": len(samples),
        "maxSampleLagSeconds": 0.0, "targetExitConfirmed": True, "allObservedDescendantsExited": True,
        "discoveryFailures": 0, "incompleteTreeSamples": 0, "marks": marks, "processes": []}
    exit_rows = samples[exit_time * 2]["processes"]
    for row in exit_rows:
        identity = {key: row[key] for key in ("pid", "created", "exe", "identity", "parentIdentity", "group")}
        identity.update(exit={key: row[key] for key in ("exitFileTime", "exitCode", "kernel100ns", "user100ns", "exited")}, exitConfirmed=True)
        summary["processes"].append(identity)
    data = {"manifest.json": manifest, "samples.jsonl": samples, "marks.jsonl": marks, "summary.json": summary}
    reproject(data)
    return data


def reproject(data):
    """Produce genuine sampler projections after an intentional raw mutation."""
    samples, summary = data["samples.jsonl"], data["summary.json"]
    peaks, stages = {}, {}
    for sample in samples:
        sample["groups"] = {group: probe.aggregate(sample["processes"], None if group == "all" else group) for group in cycles.GROUPS}
        probe.update_peaks(peaks, sample["groups"])
        stage = stages.setdefault(sample["stage"], {"firstSample": sample["index"], "lastSample": sample["index"],
            "firstObservedElapsedSeconds": sample["elapsedSeconds"], "lastObservedElapsedSeconds": sample["elapsedSeconds"], "peaks": {}})
        stage.update(lastSample=sample["index"], lastObservedElapsedSeconds=sample["elapsedSeconds"])
        probe.update_peaks(stage["peaks"], sample["groups"])
    summary.update(peaks=peaks, stages=stages, samples=len(samples),
        maxSampleLagSeconds=max(row["sampleLagSeconds"] for row in samples),
        incompleteTreeSamples=sum(not row["treeCoverageComplete"] for row in samples))


def save(directory, data):
    for name in ("manifest.json", "samples.jsonl", "marks.jsonl"):
        value = data[name]
        raw = b"".join(probe.json_bytes(row) for row in value) if name.endswith("jsonl") else probe.json_bytes(value)
        (directory / name).write_bytes(raw)
    data["summary.json"]["sampleLogBytes"] = (directory / "samples.jsonl").stat().st_size
    (directory / "summary.json").write_bytes(probe.json_bytes(data["summary.json"]))


class CycleEvidenceTests(unittest.TestCase):
    def analyze(self, data, count=2):
        with tempfile.TemporaryDirectory() as temporary:
            directory = Path(temporary)
            save(directory, data)
            return cycles.analyze(directory, count)

    def test_complete_fixed_windows_and_independent_source_closure(self):
        result = self.analyze(fixture())
        self.assertTrue(result["resourceEvidenceComplete"])
        self.assertEqual(result["cycleCount"], 2)
        self.assertEqual(result["baseline"]["tailSampleCount"], 21)
        self.assertEqual(result["baseline"]["groups"]["all"]["privateBytes"]["sampleMean"], 2100)
        self.assertEqual(result["baseline"]["groups"]["all"]["cpuPercent"]["timeWeightedMean"], 20)
        self.assertTrue(result["exit"]["complete"])
        self.assertFalse(result["exit"]["sourceClosureVerified"])
        self.assertEqual(result["leakConclusion"], "not inferred")

    def test_all_five_cycles_supported(self):
        result = self.analyze(fixture(5), 5)
        self.assertTrue(result["resourceEvidenceComplete"])
        self.assertEqual([cycle["cycle"] for cycle in result["cycles"]], [1, 2, 3, 4, 5])

    def test_reject_corrupt_summary_aggregate_counts_or_identity(self):
        mutations = [
            lambda d: d["summary.json"].update(samples=999),
            lambda d: d["samples.jsonl"][50]["groups"]["all"].update(privateBytes=7),
            lambda d: d["samples.jsonl"][50]["groups"]["root"].update(liveProcesses=True),
            lambda d: d["summary.json"]["peaks"]["root"].update(incompleteSamples=True),
            lambda d: d["summary.json"]["processes"][0].update(created="999"),
            lambda d: d["summary.json"].update(targetExitConfirmed=False),
            lambda d: d["summary.json"]["processes"][1]["exit"].update(exitCode=7),
            lambda d: d["samples.jsonl"][50]["processes"][1].update(parentIdentity="unrelated"),
        ]
        for mutation in mutations:
            data = fixture()
            mutation(data)
            with self.subTest(mutation=mutation), self.assertRaises(ValueError):
                self.analyze(data)

    def test_reject_nonfinite_missing_and_boolean_metrics(self):
        for value in (float("nan"), float("inf"), True, -1):
            data = fixture()
            data["samples.jsonl"][50]["processes"][0]["privateBytes"] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.analyze(data)
        data = fixture()
        del data["samples.jsonl"][50]["processes"][0]["privateBytes"]
        with self.assertRaises(KeyError):
            self.analyze(data)

    def test_null_metrics_in_tail_fail_window_without_inventing_zero(self):
        data = fixture()
        data["samples.jsonl"][50]["processes"][0]["privateBytes"] = None
        reproject(data)
        result = self.analyze(data)
        self.assertFalse(result["resourceEvidenceComplete"])
        self.assertFalse(result["baseline"]["valid"])
        self.assertEqual(result["baseline"]["groups"], {})

    def test_missing_active_metrics_or_cpu_after_first_observation_not_a_pass(self):
        for metric in ("privateBytes", "cpuPercent", "cpuPercentOfMachine"):
            data = fixture()
            data["samples.jsonl"][73]["processes"][0][metric] = None
            reproject(data)
            result = self.analyze(data)
            self.assertFalse(result["resourceEvidenceComplete"])
            self.assertTrue(result["baseline"]["valid"])
            self.assertTrue(result["cycles"][0]["closedIdle"]["valid"])
            self.assertTrue(result["observationErrors"])

    def test_discovery_or_metric_failure_is_not_a_pass(self):
        for tree_error in (True, False):
            data = fixture()
            if tree_error:
                data["samples.jsonl"][50].update(discoveryErrors=[{"error": "denied"}], treeCoverageComplete=False)
                data["summary.json"]["discoveryFailures"] = 1
            else:
                data["samples.jsonl"][50]["processes"][0]["errors"] = ["read failed"]
            reproject(data)
            result = self.analyze(data)
            self.assertFalse(result["baseline"]["valid"])
            self.assertFalse(result["resourceEvidenceComplete"])

    def test_unconfirmed_or_wrong_mark_and_phase_order_rejected(self):
        for kind in ("unconfirmed", "bad-index", "reordered", "missing", "sample-label"):
            data = fixture()
            if kind == "unconfirmed":
                data["marks.jsonl"][1]["confirmation"] = "unconfirmed"
            elif kind == "bad-index":
                data["marks.jsonl"][1]["appliesFromSample"] += 1
            elif kind == "reordered":
                data["marks.jsonl"][2], data["marks.jsonl"][3] = data["marks.jsonl"][3], data["marks.jsonl"][2]
            elif kind == "missing":
                data["marks.jsonl"].pop(2)
            else:
                data["samples.jsonl"][80]["stage"] = "unmarked"
                reproject(data)
            with self.subTest(kind=kind), self.assertRaises(ValueError):
                self.analyze(data)

    def test_explicit_summary_correction_preserves_both_marks_and_idle_windows(self):
        data = fixture()
        original = data["marks.jsonl"][4]
        self.assertEqual(original["stage"], "cycle-01-summary-ready")
        verified = {**original, "requestId": "correction-1", "stage": "cycle-01-summary-verified",
            "observedElapsedSeconds": 38.5, "appliesFromSample": 77,
            "observedUTC": "2026-10-01T00:00:38.5+00:00", "requestedUTC": "2026-10-01T00:00:38.5+00:00"}
        data["marks.jsonl"].insert(5, verified)
        data["samples.jsonl"][77]["stage"] = verified["stage"]
        reproject(data)
        result = self.analyze(data)
        self.assertTrue(result["resourceEvidenceComplete"])
        self.assertEqual(result["cycles"][0]["summaryReadinessMark"], verified)
        self.assertEqual(result["operatorStageCorrections"][0]["originalOperatorMark"], original)
        self.assertFalse(result["operatorStageCorrections"][0]["guiStateIndependentlyVerifiedByThisTool"])
        self.assertFalse(result["perPhaseLatencyInferred"])

    def test_summary_correction_before_original_readiness_is_rejected(self):
        data = fixture()
        original = data["marks.jsonl"][3]
        verified = {**original, "requestId": "wrong-correction", "stage": "cycle-01-summary-verified",
            "observedElapsedSeconds": 37.5, "appliesFromSample": 75,
            "observedUTC": "2026-10-01T00:00:37.5+00:00", "requestedUTC": "2026-10-01T00:00:37.5+00:00"}
        data["marks.jsonl"].insert(4, verified)
        data["samples.jsonl"][75]["stage"] = verified["stage"]
        reproject(data)
        with self.assertRaisesRegex(ValueError, "summary correction must follow"):
            self.analyze(data)

    def test_gap_and_incomplete_fixed_window_cannot_be_repaired_by_later_samples(self):
        data = fixture()
        # Keep raw index/mark projections coherent but remove four samples.
        removed = set(range(45, 49))
        data["samples.jsonl"] = [row for row in data["samples.jsonl"] if row["index"] not in removed]
        for index, sample in enumerate(data["samples.jsonl"]):
            sample["index"] = index
        for mark in data["marks.jsonl"]:
            if mark["appliesFromSample"] > 48:
                mark["appliesFromSample"] -= 4
        reproject(data)
        result = self.analyze(data)
        self.assertFalse(result["baseline"]["valid"])
        self.assertEqual(result["baseline"]["maxGapSeconds"], 2.5)
        self.assertTrue(result["cycles"][0]["closedIdle"]["valid"])
        samples = fixture()["samples.jsonl"][:58]
        window = cycles.window_stats(samples, 0, "baseline-idle", .5)
        self.assertFalse(window["valid"])

    def test_early_next_stage_invalidates_idle_window_even_with_later_samples(self):
        data = fixture()
        samples = data["samples.jsonl"]
        for sample in samples[55:70]:
            sample["stage"] = "cycle-01-open-request"
        self.assertFalse(cycles.window_stats(samples, 0, "baseline-idle", .5)["valid"])

    def test_active_gap_invalidates_continuity_even_when_all_idle_windows_pass(self):
        data = fixture()
        for sample in data["samples.jsonl"]:
            if sample["elapsedSeconds"] >= 37:
                sample["elapsedSeconds"] += 5
                sample["observedUTC"] = (datetime.fromisoformat(sample["observedUTC"]) + timedelta(seconds=5)).isoformat()
        for mark in data["marks.jsonl"]:
            if mark["observedElapsedSeconds"] >= 37:
                mark["observedElapsedSeconds"] += 5
                for field in ("observedUTC", "requestedUTC"):
                    mark[field] = (datetime.fromisoformat(mark[field]) + timedelta(seconds=5)).isoformat()
        data["summary.json"]["elapsedSeconds"] += 5
        data["summary.json"]["finishedUTC"] = (datetime.fromisoformat(data["summary.json"]["finishedUTC"]) + timedelta(seconds=5)).isoformat()
        reproject(data)
        result = self.analyze(data)
        self.assertTrue(result["baseline"]["valid"])
        self.assertTrue(all(cycle["closedIdle"]["valid"] for cycle in result["cycles"]))
        self.assertTrue(result["exit"]["complete"])
        self.assertEqual(result["gaps"]["maxSeconds"], 5.5)
        self.assertFalse(result["continuousSamplingComplete"])
        self.assertFalse(result["resourceEvidenceComplete"])

    def test_process_exit_is_checked_separately_from_fixed_idle_statistics(self):
        data = fixture()
        data["summary.json"]["stopReason"] = "operator_stop"
        result = self.analyze(data)
        self.assertTrue(result["baseline"]["valid"])
        self.assertFalse(result["exit"]["complete"])
        self.assertFalse(result["resourceEvidenceComplete"])

    def test_disappearing_live_identity_is_rejected(self):
        data = fixture()
        data["samples.jsonl"][50]["processes"].pop()
        reproject(data)
        with self.assertRaisesRegex(ValueError, "disappeared"):
            self.analyze(data)

    def test_duplicate_json_keys_rejected(self):
        with self.assertRaisesRegex(ValueError, "duplicate"):
            cycles.parse_json('{"samples":1,"samples":2}')

    def test_nonzero_descendant_exit_not_success(self):
        data = fixture()
        data["summary.json"]["processes"][1]["exit"]["exitCode"] = 1
        for sample in data["samples.jsonl"]:
            for row in sample["processes"]:
                if row["exited"] and row["group"] == "webview2":
                    row["exitCode"] = 1
        self.assertFalse(self.analyze(data)["exit"]["complete"])


if __name__ == "__main__":
    unittest.main()
