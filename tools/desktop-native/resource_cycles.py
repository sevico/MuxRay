"""Offline audit of resource_probe repeated native open/close runs.

No GUI, process, or source-file access occurs. Raw samples are authoritative;
stored aggregates, counts, stages, identity inventory and exits are checked
before reporting fixed 30-second idle windows. This is descriptive evidence,
not a memory-leak classifier. A process exit never verifies source-file closure.
An optional cycle-NN-summary-verified acknowledgment between summary-ready and
hex-ready explicitly supersedes an early operator label; both remain in output.
"""

import argparse
from bisect import bisect_left, bisect_right
from datetime import datetime
import hashlib
import json
import math
from pathlib import Path
import re
import statistics
import sys

GROUPS = ("root", "webview2", "otherDescendants", "all")
METRICS = ("privateBytes", "workingSetBytes", "handles", "cpuPercent", "cpuPercentOfMachine")
PHASES = ("open-request", "scan-ready", "detail-ready", "summary-ready", "hex-ready", "closed")
LIMITATIONS = [
    "Operator stage acknowledgments do not independently prove GUI state or source-file closure.",
    "Source closure requires a separate GUI empty-state observation and exclusive-read receipt.",
    "Private bytes are committed private memory; summed working sets can double-count shared pages.",
    "Sampled peaks miss activity between samples; only observed descendants are included.",
    "CPU 100% means one logical core. Weighted CPU uses adjacent sample elapsed times as interval boundaries.",
    "Idle memory and handle means are sample arithmetic means; a fixed window does not prove GC completion or convergence.",
    "Repeated increases, decreases or fitted slopes alone cannot establish or disprove a resource leak.",
]


def require(condition, message):
    if not condition:
        raise ValueError(message)


def number(value, where, integer=False, minimum=0):
    require(type(value) in ((int,) if integer else (int, float)), f"{where}: expected numeric value")
    require(math.isfinite(value) and value >= minimum, f"{where}: nonfinite or out-of-range value")
    return value


def decimal(value, where):
    require(isinstance(value, str) and re.fullmatch(r"(?:0|[1-9][0-9]*)", value), f"{where}: invalid decimal string")
    return int(value)


def timestamp(value):
    require(isinstance(value, str), "timestamp must be a string")
    result = datetime.fromisoformat(value.replace("Z", "+00:00"))
    require(result.utcoffset() is not None, "timestamp must have a timezone")
    return result


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, f"duplicate JSON key: {key}")
        result[key] = value
    return result


def parse_json(raw):
    def reject(value):
        raise ValueError(f"nonfinite JSON token: {value}")
    return json.loads(raw, object_pairs_hook=unique_object, parse_constant=reject)


def read_input(directory):
    values, receipts = {}, {}
    for name in ("manifest.json", "samples.jsonl", "marks.jsonl", "summary.json"):
        path = directory / name
        require(path.stat().st_size <= 256 << 20, f"oversized input: {name}")
        raw = path.read_bytes()
        receipts[name] = {"bytes": len(raw), "sha256": hashlib.sha256(raw).hexdigest()}
        if name.endswith("jsonl"):
            lines = raw.splitlines()
            require(all(line.strip() for line in lines), f"blank JSONL record: {name}")
            values[name] = [parse_json(line) for line in lines]
        else:
            values[name] = parse_json(raw)
    return values, receipts


def aggregate(rows, group):
    selected = [row for row in rows if group == "all" or row["group"] == group]
    live = [row for row in selected if row["exited"] is not True]
    result = {"liveProcesses": len(live), "observedProcesses": len(selected)}
    for metric in METRICS:
        values = [row[metric] for row in (selected if metric.startswith("cpu") else live)]
        value = sum(values) if all(value is not None for value in values) else None
        result[metric] = round(value, 4) if value is not None and metric.startswith("cpu") else value
    result["completeMetrics"] = all(result[key] is not None for key in METRICS[:-1])
    return result


def add_peaks(peaks, groups):
    for group, values in groups.items():
        target = peaks.setdefault(group, {"samples": 0, "incompleteSamples": 0, "max": {}})
        target["samples"] += 1
        target["incompleteSamples"] += not values["completeMetrics"]
        for key, value in values.items():
            if key not in ("completeMetrics", "observedProcesses") and value is not None:
                target["max"][key] = max(target["max"].get(key, value), value)


def identity_key(row):
    number(row["pid"], "pid", integer=True, minimum=1)
    created = decimal(row["created"], "created")
    require(row["pid"] <= 0xffffffff and 0 < created <= (1 << 64) - 1
        and isinstance(row["exe"], str) and row["exe"], "invalid process identity")
    return f'{row["pid"]}:{row["created"]}'


def audit_raw(values, receipts):
    manifest, samples, marks, summary = [values[name] for name in ("manifest.json", "samples.jsonl", "marks.jsonl", "summary.json")]
    require(type(manifest["schemaVersion"]) is int and type(summary["schemaVersion"]) is int
        and manifest["schemaVersion"] == summary["schemaVersion"] == 1, "unsupported probe schema")
    require(manifest["runId"] == summary["runId"], "runId differs")
    require(isinstance(manifest["runId"], str) and manifest["runId"], "invalid runId")
    require(isinstance(manifest["exeSHA256"], str) and re.fullmatch(r"[0-9a-fA-F]{64}", manifest["exeSHA256"]),
        "invalid executable SHA-256")
    interval = number(manifest["intervalMs"], "intervalMs", integer=True, minimum=250) / 1000
    require(interval <= 5, "interval exceeds sampler limit")
    number(manifest["logicalProcessors"], "logicalProcessors", integer=True, minimum=1)
    require(samples, "empty sample stream")
    number(summary["samples"], "summary sample count", integer=True)
    number(summary["sampleLogBytes"], "sampleLogBytes", integer=True)
    require(summary["samples"] == len(samples), "summary sample count differs")
    require(summary["sampleLogBytes"] == receipts["samples.jsonl"]["bytes"], "sample byte count differs")
    require(summary["marks"] == marks, "summary marks differ from mark log")
    require(summary["startedUTC"] == manifest["startedUTC"], "start timestamps differ")
    require(timestamp(summary["finishedUTC"]) >= timestamp(manifest["startedUTC"]), "wall-clock end precedes start")
    number(summary["elapsedSeconds"], "summary elapsedSeconds")
    inventory = {}
    for row in summary["processes"]:
        key = identity_key(row)
        require(row["identity"] == key and key not in inventory, "duplicate or inconsistent inventory identity")
        require(row["group"] in GROUPS[:-1], "invalid inventory group")
        require(type(row["exitConfirmed"]) is bool, "invalid exitConfirmed")
        inventory[key] = row
    root_key = identity_key(manifest["target"])
    require(root_key in inventory and inventory[root_key]["group"] == "root", "target missing from inventory")
    require(sum(item["group"] == "root" for item in inventory.values()) == 1, "multiple root identities")
    require(inventory[root_key]["parentIdentity"] is None, "root has parent")
    for key in ("pid", "created", "exe"):
        require(inventory[root_key][key] == manifest["target"][key], "target identity differs")
    for key, row in inventory.items():
        if key == root_key:
            continue
        parent = inventory.get(row["parentIdentity"])
        require(parent is not None and row["parentIdentity"] != key, "missing or self parent")
        require(int(row["created"]) >= int(parent["created"]), "child predates parent identity")
        if parent["exit"] is not None:
            require(int(row["created"]) <= decimal(parent["exit"]["exitFileTime"], "parent exit"), "child postdates parent exit")
        expected_group = "webview2" if row["exe"].replace("\\", "/").rsplit("/", 1)[-1].lower() == "msedgewebview2.exe" else "otherDescendants"
        require(row["group"] == expected_group, "descendant group disagrees with executable")
        visited = {key}
        while parent["identity"] != root_key:
            require(parent["identity"] not in visited, "cycle in process ancestry")
            visited.add(parent["identity"])
            parent = inventory.get(parent["parentIdentity"])
            require(parent is not None, "ancestry does not reach root")
    previous_time, seen, exited, active = -1, set(), {}, set()
    peaks, stages, observed_errors = {}, {}, []
    incomplete_tree = 0
    for index, sample in enumerate(samples):
        require(type(sample["index"]) is int and sample["index"] == index, "noncontiguous sample indexes")
        elapsed = number(sample["elapsedSeconds"], "sample elapsedSeconds")
        require(elapsed > previous_time, "sample time did not increase")
        previous_time = elapsed
        timestamp(sample["observedUTC"])
        number(sample["sampleLagSeconds"], "sampleLagSeconds")
        require(isinstance(sample["stage"], str) and sample["stage"], "invalid stage")
        require(isinstance(sample["discoveryErrors"], list), "invalid discovery errors")
        require(type(sample["treeCoverageComplete"]) is bool and sample["treeCoverageComplete"] == (not sample["discoveryErrors"]), "tree coverage disagrees with errors")
        incomplete_tree += not sample["treeCoverageComplete"]
        present = set()
        for row in sample["processes"]:
            key = identity_key(row)
            require(key == row["identity"] and key in inventory and key not in present, "unknown, duplicate or inconsistent sample identity")
            require(key not in exited, "sample after confirmed process exit")
            for field in ("pid", "created", "exe", "parentIdentity", "group"):
                require(row[field] == inventory[key][field], "identity or attribution changed")
            require(row["exited"] is None or type(row["exited"]) is bool, "invalid exited flag")
            require(isinstance(row["errors"], list) and all(isinstance(item, str) for item in row["errors"]), "invalid process errors")
            for metric in METRICS:
                if row[metric] is not None:
                    number(row[metric], metric, integer=not metric.startswith("cpu"))
            if row["errors"]:
                observed_errors.append({"sample": index, "identity": key, "errors": row["errors"]})
            missing = []
            if row["exited"] is None:
                missing.append("process lifecycle status is unknown")
            if row["exited"] is not True:
                missing.extend("missing live " + metric for metric in METRICS[:3] if row[metric] is None)
            if key in seen:
                missing.extend("missing interval " + metric for metric in METRICS[3:] if row[metric] is None)
            if missing:
                observed_errors.append({"sample": index, "identity": key, "errors": missing})
            if row["exited"] is True:
                exit_value = {field: row[field] for field in ("exitFileTime", "exitCode", "kernel100ns", "user100ns", "exited")}
                require(decimal(row["exitFileTime"], "exitFileTime") >= int(row["created"]), "exit predates process creation")
                number(row["exitCode"], "exitCode", integer=True)
                decimal(row["kernel100ns"], "kernel100ns")
                decimal(row["user100ns"], "user100ns")
                exited[key] = exit_value
            present.add(key)
        require(active <= present, "live process disappeared without exit observation")
        require(root_key in present or root_key in exited, "root observation missing")
        active = present - set(exited)
        seen |= present
        groups = {group: aggregate(sample["processes"], group) for group in GROUPS}
        for group in GROUPS:
            for field in ("liveProcesses", "observedProcesses"):
                number(sample["groups"][group][field], "aggregate " + field, integer=True)
            require(type(sample["groups"][group]["completeMetrics"]) is bool, "invalid completeMetrics")
        require(sample["groups"] == groups, f"sample {index} aggregate differs")
        add_peaks(peaks, groups)
        stage = stages.setdefault(sample["stage"], {"firstSample": index, "lastSample": index,
            "firstObservedElapsedSeconds": elapsed, "lastObservedElapsedSeconds": elapsed, "peaks": {}})
        stage.update(lastSample=index, lastObservedElapsedSeconds=elapsed)
        add_peaks(stage["peaks"], groups)
    require(seen == set(inventory), "inventory differs from observed identities")
    require(summary["elapsedSeconds"] >= samples[-1]["elapsedSeconds"], "summary ends before last sample")
    require(summary["peaks"] == peaks and summary["stages"] == stages, "summary peaks or stages differ")
    for stage in summary["stages"].values():
        number(stage["firstSample"], "stage first sample", integer=True)
        number(stage["lastSample"], "stage last sample", integer=True)
    for projection in [summary["peaks"]] + [stage["peaks"] for stage in summary["stages"].values()]:
        for group in GROUPS:
            number(projection[group]["samples"], "peak sample count", integer=True)
            number(projection[group]["incompleteSamples"], "peak incomplete count", integer=True)
    number(summary["maxSampleLagSeconds"], "maximum sample lag")
    require(summary["maxSampleLagSeconds"] == max(row["sampleLagSeconds"] for row in samples), "maximum sample lag differs")
    require(type(summary["incompleteTreeSamples"]) is int and summary["incompleteTreeSamples"] == incomplete_tree, "incomplete tree count differs")
    number(summary["discoveryFailures"], "discoveryFailures", integer=True)
    require((summary["discoveryFailures"] == 0) == (incomplete_tree == 0), "discovery failure count disagrees with samples")
    for key, row in inventory.items():
        require(row["exit"] == exited.get(key) and row["exitConfirmed"] == (key in exited), "inventory exit differs from samples")
    require(type(summary["targetExitConfirmed"]) is bool and summary["targetExitConfirmed"] == (root_key in exited), "root exit summary differs")
    descendants_exited = all(key in exited for key in inventory if key != root_key)
    require(type(summary["allObservedDescendantsExited"]) is bool and summary["allObservedDescendantsExited"] == descendants_exited, "descendant exit summary differs")
    acknowledged, request_ids, previous_index, previous_elapsed = [], set(), -1, -1
    for mark in marks:
        require(mark["runId"] == manifest["runId"] and mark["action"] in ("mark", "stop"), "foreign or invalid mark")
        require(mark.get("confirmation") != "unconfirmed", "unconfirmed mark")
        require(isinstance(mark["requestId"], str) and mark["requestId"] and mark["requestId"] not in request_ids, "duplicate or invalid mark request")
        request_ids.add(mark["requestId"])
        number(mark["appliesFromSample"], "mark sample index", integer=True)
        index = mark["appliesFromSample"]
        require(previous_index <= index < len(samples), "mark sample index out of order or unobserved")
        elapsed = number(mark["observedElapsedSeconds"], "mark elapsedSeconds")
        require(elapsed >= previous_elapsed and elapsed == samples[index]["elapsedSeconds"], "mark timestamp differs from applied sample")
        require(timestamp(mark["observedUTC"]) >= timestamp(mark["requestedUTC"]), "mark acknowledged before request")
        previous_index, previous_elapsed = index, elapsed
        if mark["action"] == "mark":
            require(isinstance(mark["stage"], str) and mark["stage"], "invalid mark label")
            acknowledged.append(mark)
    cursor, label = 0, "unmarked"
    for index, sample in enumerate(samples):
        while cursor < len(marks) and marks[cursor]["appliesFromSample"] == index:
            if marks[cursor]["action"] == "mark":
                label = marks[cursor]["stage"]
            require(marks[cursor]["stage"] == label, "stop acknowledgment has wrong stage")
            cursor += 1
        require(sample["stage"] == label, "sample stage disagrees with acknowledged controls")
    return manifest, samples, acknowledged, summary, observed_errors, interval


def window_stats(samples, start, label, interval):
    end, tail = start + 30, start + 20
    times = [sample["elapsedSeconds"] for sample in samples]
    left, right = bisect_right(times, start) - 1, bisect_left(times, end)
    errors = []
    if left < 0 or right == len(samples):
        errors.append("samples do not bracket the fixed 30-second idle window")
    covered = samples[max(0, left):min(len(samples), right + 1)]
    max_gap = max((b["elapsedSeconds"] - a["elapsedSeconds"] for a, b in zip(covered, covered[1:])), default=0)
    gap_limit = max(1, 2 * interval)
    if max_gap > gap_limit:
        errors.append("sampling gap exceeds the window coverage limit")
    for row in covered:
        if row["elapsedSeconds"] < end and row["stage"] != label:
            errors.append("idle label changed before the fixed window ended")
        if not row["treeCoverageComplete"] or any(process["errors"] or process["exited"] is None for process in row["processes"]):
            errors.append("incomplete process/tree observation in idle window")
        if row["groups"]["root"]["liveProcesses"] != 1:
            errors.append("root was not alive throughout the bracketed idle window")
        if any(row["groups"][group][metric] is None for group in GROUPS for metric in METRICS):
            # The first-ever process sample has no CPU delta; it is outside
            # the reported tail and does not invalidate memory coverage.
            if row["elapsedSeconds"] >= tail or any(row["groups"][group][metric] is None for group in GROUPS for metric in METRICS[:3]):
                errors.append("missing resource metrics in idle window")
    tail_samples = [row for row in covered if tail <= row["elapsedSeconds"] <= end]
    if not tail_samples:
        errors.append("no samples in the final ten seconds")
    stats = {}
    if not errors:
        for group in GROUPS:
            metrics = {}
            for metric in METRICS:
                points = [row["groups"][group][metric] for row in tail_samples]
                require(all(value is not None for value in points), "missing tail metric")
                if metric.startswith("cpu"):
                    weighted, duration = 0, 0
                    for before, after in zip(covered, covered[1:]):
                        overlap = max(0, min(end, after["elapsedSeconds"]) - max(tail, before["elapsedSeconds"]))
                        if overlap:
                            value = after["groups"][group][metric]
                            require(value is not None, "missing interval CPU metric")
                            weighted += overlap * value
                            duration += overlap
                    require(math.isclose(duration, 10, abs_tol=1e-6), "CPU intervals do not cover final ten seconds")
                    metrics[metric] = {"timeWeightedMean": weighted / duration, "sampleMax": max(points)}
                else:
                    metrics[metric] = {"sampleMean": statistics.fmean(points), "sampleMin": min(points), "sampleMax": max(points)}
            stats[group] = metrics
    return {"stage": label, "valid": not errors, "errors": list(dict.fromkeys(errors)),
        "idleStartSeconds": start, "idleEndSeconds": end, "statisticsStartSeconds": tail,
        "statisticsEndSeconds": end, "tailSampleCount": len(tail_samples),
        "bracketFirstSample": covered[0]["index"] if covered else None,
        "bracketLastSample": covered[-1]["index"] if covered else None,
        "maxGapSeconds": max_gap, "gapLimitSeconds": gap_limit, "groups": stats}


def analyze(directory, cycle_count=5):
    require(type(cycle_count) is int and 1 <= cycle_count <= 10, "cycles must be 1..10")
    values, receipts = read_input(directory)
    manifest, samples, marks, summary, observed_errors, interval = audit_raw(values, receipts)
    expected = ["baseline-idle"] + [f"cycle-{cycle:02d}-{phase}" for cycle in range(1, cycle_count + 1) for phase in PHASES]
    cycle_marks = [mark for mark in marks if mark["stage"] == "baseline-idle" or mark["stage"].startswith("cycle-")]
    canonical, corrections = [], {}
    for mark in cycle_marks:
        verified = re.fullmatch(r"cycle-([0-9]{2})-summary-verified", mark["stage"])
        if verified:
            cycle = int(verified[1])
            require(1 <= cycle <= cycle_count and cycle not in corrections, "invalid or duplicate summary correction")
            require(canonical and canonical[-1]["stage"] == f"cycle-{cycle:02d}-summary-ready",
                "summary correction must follow its original summary-ready and precede hex-ready")
            corrections[cycle] = {"cycle": cycle, "originalOperatorMark": canonical[-1], "verifiedOperatorMark": mark,
                "originalReadinessTimestampSuperseded": True,
                "guiStateIndependentlyVerifiedByThisTool": False}
        else:
            canonical.append(mark)
    require([mark["stage"] for mark in canonical] == expected, "missing, duplicated or out-of-order baseline/cycle phases")
    require(all(left["appliesFromSample"] < right["appliesFromSample"] for left, right in zip(cycle_marks, cycle_marks[1:])),
        "required phases lack distinct observed samples")
    lookup = {mark["stage"]: mark for mark in cycle_marks}
    baseline = window_stats(samples, lookup["baseline-idle"]["observedElapsedSeconds"], "baseline-idle", interval)
    cycles = []
    for cycle in range(1, cycle_count + 1):
        prefix = f"cycle-{cycle:02d}-"
        opened, closed = [lookup[prefix + phase]["observedElapsedSeconds"] for phase in ("open-request", "closed")]
        require(opened < closed, "cycle has no observed active duration")
        active = [sample for sample in samples if opened <= sample["elapsedSeconds"] < closed]
        peaks = {}
        for sample in active:
            add_peaks(peaks, sample["groups"])
        idle = window_stats(samples, closed, prefix + "closed", interval)
        cycles.append({"cycle": cycle, "openObservedSeconds": opened, "closedObservedSeconds": closed,
            "activeSamples": len(active), "activePeaks": peaks, "closedIdle": idle,
            "summaryReadinessMark": corrections[cycle]["verifiedOperatorMark"] if cycle in corrections else lookup[prefix + "summary-ready"]})
    root = next(row for row in summary["processes"] if row["group"] == "root")
    exit_complete = (summary["targetExitConfirmed"] and summary["allObservedDescendantsExited"]
        and all(row["exit"]["exitCode"] == 0 for row in summary["processes"])
        and summary["stopReason"] == "target_exit_grace_complete" and summary["error"] is None)
    global_gaps = [b["elapsedSeconds"] - a["elapsedSeconds"] for a, b in zip(samples, samples[1:])]
    continuous = all(gap <= max(1, 2 * interval) for gap in global_gaps)
    observations_complete = continuous and summary["incompleteTreeSamples"] == 0 and not observed_errors and summary["error"] is None
    return {"schemaVersion": 1, "runId": manifest["runId"], "inputs": receipts,
        "analysisToolSHA256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "target": manifest["target"], "exeSHA256": manifest["exeSHA256"], "samples": len(samples),
        "cycleCount": cycle_count, "fixedIdleSeconds": 30, "tailStatisticsSeconds": 10,
        "rawCrossChecksPassed": True, "continuousSamplingComplete": continuous, "observationErrors": observed_errors,
        "operatorStageCorrections": list(corrections.values()), "perPhaseLatencyInferred": False,
        "incompleteTreeSamples": summary["incompleteTreeSamples"], "baseline": baseline, "cycles": cycles,
        "gaps": {"maxSeconds": max(global_gaps, default=0),
            "overOneSecond": sum(gap > 1 for gap in global_gaps),
            "overWindowLimit": sum(gap > max(1, 2 * interval) for gap in global_gaps)},
        "exit": {"complete": exit_complete, "rootExit": root["exit"], "identities": summary["processes"],
            "stopReason": summary["stopReason"], "sourceClosureVerified": False},
        "resourceEvidenceComplete": observations_complete and baseline["valid"]
            and all(cycle["closedIdle"]["valid"] for cycle in cycles) and exit_complete,
        "leakConclusion": "not inferred", "limitations": LIMITATIONS}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run-dir", type=Path, required=True)
    parser.add_argument("--cycles", type=int, default=5)
    parser.add_argument("--output", type=Path, required=True, help="New JSON file; never overwrite existing evidence")
    args = parser.parse_args()
    try:
        result = analyze(args.run_dir, args.cycles)
        with args.output.open("x", encoding="utf-8") as stream:
            json.dump(result, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write("\n")
        print(json.dumps({"output": str(args.output), "resourceEvidenceComplete": result["resourceEvidenceComplete"],
            "cycles": result["cycleCount"], "samples": result["samples"]}))
        return 0 if result["resourceEvidenceComplete"] else 1
    except (OSError, ValueError, KeyError, TypeError) as error:
        print(f"resource_cycles: {error}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    raise SystemExit(main())
