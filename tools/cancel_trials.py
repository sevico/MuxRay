#!/usr/bin/env python3
"""Run cancellation measurements in independent, bounded child processes."""
import argparse
import json
import math
from pathlib import Path
import statistics
import sys

from corpus import bounded_run, file_identity, require


def distribution(values):
    ordered = sorted(values)
    require(ordered and all(math.isfinite(value) and value >= 0 for value in values), "invalid timing values")
    return {"median": statistics.median(ordered), "p95": ordered[math.ceil(len(ordered) * .95) - 1],
            "max": ordered[-1], "values": values}


def run_trials(tool, path, count, pre_scans=0, prime=False, legacy=False):
    require(1 <= count <= 100 and 0 <= pre_scans <= 11, "invalid trial count/pre-scans")
    require(path.is_file() and 1513 <= path.stat().st_size <= 12 << 30, "invalid fixture")
    _, binary_hash = file_identity(tool)
    report = {"toolVersion": 1, "binarySha256": binary_hash, "inputName": path.name,
              "sizeBytes": path.stat().st_size, "requestedTrials": count, "independentProcesses": True,
              "mode": "legacy-measure" if legacy else "instrumented-probe", "trials": [], "failures": []}
    keys = ["coldCancelMs"] if legacy else ["boundaryToWorkerMs", "startClockCallMs", "cancelCallMs", "cancelToScanReturnMs",
        "afterCancelToReaderMs", "readerToScanReturnMs", "returnClockCallMs", "legacyWindowMs"]
    initial_stat = path.stat()
    for index in range(count):
        args = [tool, "measure" if legacy else "cancel-probe", "--file", path, "--tags", 100000]
        if legacy:
            args.extend(["--runs", 10])
        else:
            args.extend(["--pre-scans", pre_scans, "--prime-clock=" + str(prime).lower()])
        try:
            code, output, error = bounded_run(args, timeout=45, limit=1 << 20)
            require(code == 0, f"measurement failed with exit {code}: {error.decode('utf-8', errors='replace')[:1024]}")
            trial = json.loads(output)
            require(trial["sizeBytes"] == report["sizeBytes"] and trial["tagCount"] == 100000, "fixture facts differ")
            if not legacy:
                require(trial["canceled"] is True and trial["readCalls"] == 100 and trial["diagnostics"] == 0, "partial cancellation invariant failed")
                require(trial["preScans"] == pre_scans and trial["clockPrimed"] == prime, "probe mode differs")
            for key in keys:
                require(type(trial[key]) in (float, int), f"invalid timing type: {key}")
                distribution([trial[key]])
            report["trials"].append(trial)
        except (ValueError, KeyError, OSError) as error:
            report["failures"].append({"trial": index, "error": str(error)})
            break
        print(f"completed independent trial {index + 1}/{count}", file=sys.stderr, flush=True)
    try:
        after = path.stat()
        require((after.st_size, after.st_mtime_ns) == (initial_stat.st_size, initial_stat.st_mtime_ns), "fixture changed during trials")
        require(file_identity(tool)[1] == binary_hash, "measurement binary changed during trials")
    except (ValueError, OSError) as error:
        report["failures"].append({"error": str(error)})
    report["distributions"] = {key: distribution([t[key] for t in report["trials"]]) for key in keys} if report["trials"] else {}
    report["complete"] = len(report["trials"]) == count and not report["failures"]
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tool", type=Path, required=True)
    parser.add_argument("--file", type=Path, required=True)
    parser.add_argument("--trials", type=int, default=30)
    parser.add_argument("--pre-scans", type=int, default=0)
    parser.add_argument("--prime-clock", action="store_true")
    parser.add_argument("--legacy", action="store_true", help="unchanged historical measure binary, ten measured scans")
    parser.add_argument("--output", type=Path, help="exclusive-create JSON evidence file; never overwrite")
    args = parser.parse_args()
    require(not args.legacy or (args.pre_scans == 0 and not args.prime_clock), "legacy mode controls its own warmup")
    require(args.output is None or not args.output.exists(), "refusing to overwrite evidence")
    report = run_trials(args.tool.resolve(strict=True), args.file.resolve(strict=True), args.trials, args.pre_scans, args.prime_clock, args.legacy)
    if args.output:
        with args.output.open("x", encoding="utf-8") as output:
            json.dump(report, output, indent=2)
            output.write("\n")
        print(json.dumps({"complete": report["complete"], "trials": len(report["trials"]),
                          "distributions": {key: {k: v for k, v in value.items() if k != "values"}
                                            for key, value in report["distributions"].items()}}))
    else:
        print(json.dumps(report, indent=2))
    raise SystemExit(0 if report["complete"] else 1)


if __name__ == "__main__":
    main()
