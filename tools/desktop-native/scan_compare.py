"""Compare real Document.Open scans in two immutable Git archive snapshots.

prepare creates a new evidence directory and builds a test-only probe in each
archive. run takes an exclusive measurement window, hashes the source (warming
the OS cache), performs one excluded warm-up per version, then alternating
pairs. Fresh processes call production Document.Open + Wait with raw os.File.
Two separate, excluded counted runs use the unchanged document.start seam to
measure ReaderAt calls and requested/returned bytes. None of these are physical
disk I/O measurements. No worktree, checkout, product file or GUI is changed.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import statistics
import subprocess
import sys
import zipfile


FACT_KEYS = ("tagCount", "sourceSize", "bytesScanned", "complete", "hasErrors",
             "diagnosticCount", "recordsAndDiagnosticsSHA256")
LIMITATIONS = [
    "OS-cache-warmed scans: source hashing and excluded warm-up runs occur before timing; no cache eviction is attempted.",
    "Timed interval is production Document.Open through Document.Wait; test process startup, record validation and Close are excluded.",
    "Each timed sample uses a fresh process; this is not the native WebView UI or a continuous memory-retention test.",
    "Separate counted runs call the unchanged document.start seam with an os.File wrapper; counted timings are excluded from distributions.",
    "ReaderAt requested/returned bytes are application I/O, not physical disk reads or OS cache misses.",
    "Alternating order reduces time-order bias but does not isolate operating-system scheduling, security software or unrelated host load.",
    "Observed timing describes this source, machine and cache state; it is not a universal latency guarantee.",
]


def now():
    return datetime.now(timezone.utc).isoformat(timespec="milliseconds")


def sha256(path):
    digest = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def write_json(path, value):
    with Path(path).open("x", encoding="utf-8", newline="\n") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")


def command(argv, cwd, log=None, env=None, timeout=300):
    result = subprocess.run([str(arg) for arg in argv], cwd=cwd, env=env,
                            capture_output=True, text=True, encoding="utf-8",
                            errors="replace", timeout=timeout)
    evidence = {"argv": [str(arg) for arg in argv], "cwd": str(cwd),
                "exitCode": result.returncode, "stdout": result.stdout, "stderr": result.stderr}
    if log:
        write_json(log, evidence)
    if result.returncode:
        raise RuntimeError(f"Command failed ({result.returncode}): {argv[0]}; evidence: {log}")
    return result.stdout.strip()


def identity(path):
    path = Path(path).resolve(strict=True)
    return {"path": str(path), "bytes": path.stat().st_size, "sha256": sha256(path)}


def verify_identity(expected):
    actual = identity(expected["path"])
    if actual != expected:
        raise ValueError(f"Evidence input changed: {expected['path']}")


def extract_archive(archive, directory):
    # The archive is produced from the local repository, but reject traversal
    # and symbolic links anyway. Retain failed outputs for inspection.
    directory = Path(directory).resolve()
    directory.mkdir()
    with zipfile.ZipFile(archive) as source:
        for item in source.infolist():
            target = (directory / item.filename).resolve()
            if not target.is_relative_to(directory) or (item.external_attr >> 16) & 0o170000 == 0o120000:
                raise ValueError("Unsafe archive member")
        source.extractall(directory)


def prepare(repo, source, baseline, candidate, output):
    repo, source, output = Path(repo), Path(source), Path(output)
    if not all(path.is_absolute() for path in (repo, source, output)):
        raise ValueError("Repository, source and output must be absolute paths")
    source_identity = identity(source)
    output.mkdir(parents=False, exist_ok=False)
    probe = Path(__file__).with_name("scan_compare_probe_test.go.txt")
    manifest = {"formatVersion": 1, "preparedAtUTC": now(), "repo": str(repo.resolve()),
                "source": source_identity, "runner": identity(__file__), "probe": identity(probe),
                "platform": platform.platform(), "pythonVersion": sys.version,
                "goVersion": command(["go", "version"], repo),
                "goEnv": json.loads(command(["go", "env", "-json", "GOOS", "GOARCH", "CGO_ENABLED", "GOFLAGS", "GOTOOLCHAIN"], repo)),
                "versions": {}, "limitations": LIMITATIONS}
    for label, ref in (("baseline", baseline), ("candidate", candidate)):
        commit = command(["git", "rev-parse", "--verify", ref + "^{commit}"], repo)
        archive = output / (label + ".zip")
        command(["git", "archive", "--format=zip", "--output=" + str(archive), commit], repo,
                output / (label + "-archive-command.json"))
        checkout = output / (label + "-source")
        extract_archive(archive, checkout)
        injected = checkout / "internal/document/zz_scan_comparison_probe_test.go"
        with injected.open("xb") as stream:
            stream.write(probe.read_bytes())
        binary = output / (label + ("-scan.exe" if os.name == "nt" else "-scan"))
        command(["go", "test", "-c", "-trimpath", "-o", binary, "./internal/document"], checkout,
                output / (label + "-build.json"))
        manifest["versions"][label] = {"commit": commit, "requestedRef": ref,
                                       "archive": identity(archive), "binary": identity(binary),
                                       "injectedProbe": identity(injected)}
    write_json(output / "prepared.json", manifest)
    return manifest


def pair_order(pairs):
    if not 5 <= pairs <= 30:
        raise ValueError("Use between 5 and 30 measured pairs")
    return [(pair, label) for pair in range(1, pairs + 1)
            for label in (("baseline", "candidate") if pair % 2 else ("candidate", "baseline"))]


def distribution(values):
    values = sorted(values)
    if not values or any(not math.isfinite(value) or value <= 0 for value in values):
        raise ValueError("Expected positive finite measurements")
    return {"count": len(values), "min": values[0], "median": statistics.median(values),
            "mean": statistics.mean(values), "p95NearestRank": values[math.ceil(.95 * len(values)) - 1],
            "max": values[-1], "sampleStdev": statistics.stdev(values) if len(values) > 1 else 0}


def summarize(runs, expected_pairs):
    measured = [run for run in runs if run["phase"] == "measured"]
    expected_order = pair_order(expected_pairs)
    if [(run["pair"], run["label"]) for run in measured] != expected_order:
        raise ValueError("Measured pair order or coverage is incomplete")
    reference = {key: runs[0]["facts"][key] for key in FACT_KEYS}
    for run in runs:
        if {key: run["facts"][key] for key in FACT_KEYS} != reference:
            raise ValueError("Scan records/diagnostics differ between runs")
        if run["facts"]["mode"] != ("counted" if run["phase"] == "counted" else "open"):
            raise ValueError("Wrong probe mode for phase")
    if not reference["complete"] or reference["hasErrors"] or reference["bytesScanned"] != reference["sourceSize"]:
        raise ValueError("Source did not complete a clean scan")
    distributions = {label: distribution([run["facts"]["scanNanoseconds"] / 1e6 for run in measured if run["label"] == label])
                     for label in ("baseline", "candidate")}
    paired = []
    for pair in range(1, expected_pairs + 1):
        values = {run["label"]: run["facts"]["scanNanoseconds"] for run in measured if run["pair"] == pair}
        paired.append({"pair": pair, "candidateMinusBaselineMilliseconds": (values["candidate"] - values["baseline"]) / 1e6,
                       "candidateOverBaseline": values["candidate"] / values["baseline"]})
    counted = {}
    for label in ("baseline", "candidate"):
        selected = [run for run in runs if run["phase"] == "counted" and run["label"] == label]
        if len(selected) != 1:
            raise ValueError("Exactly one excluded counted run required per version")
        counted[label] = {key: selected[0]["facts"][key] for key in ("readCalls", "readRequestedBytes", "readReturnedBytes")}
    return {"sameScanFacts": reference, "scanMilliseconds": distributions, "paired": paired,
            "pairedRatio": distribution([value["candidateOverBaseline"] for value in paired]),
            "countedReads": counted, "readIncrease": {key: counted["candidate"][key] - counted["baseline"][key] for key in counted["baseline"]}}


def run(prepared, output, pairs):
    order = pair_order(pairs)
    prepared, output = Path(prepared), Path(output)
    if not prepared.is_absolute() or not output.is_absolute():
        raise ValueError("Prepared manifest and output must be absolute paths")
    manifest = json.loads(prepared.read_text(encoding="utf-8"))
    # This full read warms the OS cache, intentionally and explicitly.
    verify_identity(manifest["source"])
    for version in manifest["versions"].values():
        verify_identity(version["binary"])
    verify_identity(manifest["runner"])
    verify_identity(manifest["probe"])
    output.mkdir(parents=False, exist_ok=False)
    plan = [("warmup", 0, "baseline"), ("warmup", 0, "candidate")]
    plan += [("measured", pair, label) for pair, label in order]
    plan += [("counted", 0, "baseline"), ("counted", 0, "candidate")]
    write_json(output / "plan.json", {"startedAtUTC": now(), "prepared": identity(prepared), "pairs": pairs,
                                     "plan": plan, "limitations": LIMITATIONS})
    runs = []
    for sequence, (phase, pair, label) in enumerate(plan, 1):
        stem = f"{sequence:03d}-{phase}-{label}"
        result_path = output / (stem + ".json")
        env = dict(os.environ, MUXRAY_SCAN_SOURCE=manifest["source"]["path"],
                   MUXRAY_SCAN_OUTPUT=str(result_path), MUXRAY_SCAN_MODE="counted" if phase == "counted" else "open")
        started = now()
        command([manifest["versions"][label]["binary"]["path"], "-test.run=^TestScanComparisonProbe$", "-test.count=1", "-test.timeout=60s"],
                prepared.parent, output / (stem + "-process.json"), env=env, timeout=65)
        facts = json.loads(result_path.read_text(encoding="utf-8"))
        runs.append({"phase": phase, "pair": pair, "label": label, "startedAtUTC": started, "finishedAtUTC": now(), "facts": facts})
        print(f"{stem}: {facts['scanNanoseconds'] / 1e6:.3f} ms; {facts['tagCount']} tags", flush=True)
    verify_identity(manifest["source"])
    for version in manifest["versions"].values():
        verify_identity(version["binary"])
    summary = {"finishedAtUTC": now(), "prepared": str(prepared), "pairs": pairs, "runs": runs,
               "summary": summarize(runs, pairs), "limitations": LIMITATIONS}
    write_json(output / "comparison.json", summary)
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="command", required=True)
    prepare_parser = commands.add_parser("prepare")
    for field in ("repo", "source", "baseline", "candidate", "output"):
        prepare_parser.add_argument("--" + field, required=True)
    run_parser = commands.add_parser("run")
    run_parser.add_argument("--prepared", required=True)
    run_parser.add_argument("--output", required=True)
    run_parser.add_argument("--pairs", type=int, default=10)
    args = vars(parser.parse_args())
    action = args.pop("command")
    result = prepare(**args) if action == "prepare" else run(**args)
    print(json.dumps(result["summary"] if action == "run" else {"versions": result["versions"]}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
