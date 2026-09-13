"""Compare generated Summary fixture facts with an explicitly supplied local CLI.

Stores exact command, exit code, stdout, stderr and file identities in a new
output directory. This verifies CLI/core values, not the Wails/native UI.
Codec parser defaults are safe for these tiny headers; the five aggregate
budgets are exactly those in the ADR-016 fixture manifest.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import subprocess


FLAGS = {"maxKeyframes": "--max-keyframes", "maxConfigurationEvents": "--max-configuration-events",
         "maxConfigurationBytes": "--max-configuration-bytes",
         "maxAnalysisDiagnostics": "--max-analysis-diagnostics",
         "maxMP3HeaderEvents": "--max-mp3-header-events"}


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def lookup(document, path):
    for name in path.split("."):
        document = document[name]
    return document


def compare(document, expected):
    failures = []
    count = 0
    for path, value in expected.items():
        if path in ("lengths", "absent", "lastDiagnosticCode"):
            continue
        count += 1
        try:
            actual = lookup(document, path)
        except KeyError:
            actual = "<missing>"
        if type(actual) is not type(value) or actual != value:
            failures.append({"path": path, "expected": value, "actual": actual})
    for path, length in expected.get("lengths", {}).items():
        count += 1
        actual = len(lookup(document, path))
        if actual != length:
            failures.append({"path": path + ".length", "expected": length, "actual": actual})
    for path in expected.get("absent", []):
        count += 1
        try:
            actual = lookup(document, path)
        except KeyError:
            continue
        failures.append({"path": path, "expected": "<absent>", "actual": actual})
    if "lastDiagnosticCode" in expected:
        count += 1
        actual = document["analysisDiagnostics"][-1]["code"]
        if actual != expected["lastDiagnosticCode"]:
            failures.append({"path": "analysisDiagnostics[-1].code",
                             "expected": expected["lastDiagnosticCode"], "actual": actual})
    return count, failures


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures-dir", type=Path, required=True)
    parser.add_argument("--muxray", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    if not all(path.is_absolute() for path in (args.fixtures_dir, args.muxray, args.output_dir)):
        parser.error("Use absolute paths; --output-dir must be new.")
    manifest_path = args.fixtures_dir / "fixtures.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest["format"] != "muxray-desktop-summary-fixtures-v1":
        parser.error("Unsupported fixture manifest.")
    args.output_dir.mkdir(parents=True, exist_ok=False)
    record = {"format": "muxray-summary-cli-check-v1", "startedUTC": datetime.now(timezone.utc).isoformat(),
              "executable": str(args.muxray), "executableSHA256": sha256(args.muxray),
              "manifestSHA256": sha256(manifest_path), "budgets": manifest["budgets"], "files": []}
    for source in manifest["files"]:
        name = source["name"]
        if Path(name).name != name:
            raise ValueError("Fixture name must be a basename")
        path = args.fixtures_dir / name
        if path.stat().st_size != source["bytes"] or sha256(path) != source["sha256"]:
            raise ValueError("Fixture identity changed: " + name)
        command = [str(args.muxray), "summary", "--schema-version", "2"]
        for key, flag in FLAGS.items():
            command.extend([flag, str(manifest["budgets"][key])])
        command.append(str(path))
        output = subprocess.run(command, capture_output=True, timeout=120, check=False)
        (args.output_dir / (name + ".stdout.json")).write_bytes(output.stdout)
        (args.output_dir / (name + ".stderr.txt")).write_bytes(output.stderr)
        if output.returncode not in (0, 2):
            count, failures = 0, [{"exitCode": output.returncode, "expected": "0 or 2"}]
        else:
            count, failures = compare(json.loads(output.stdout), source["expected"])
        record["files"].append({"name": name, "sha256": source["sha256"], "command": command,
                                "exitCode": output.returncode, "assertions": count,
                                "passed": not failures, "failures": failures})
    record["completedUTC"] = datetime.now(timezone.utc).isoformat()
    record["passed"] = all(item["passed"] for item in record["files"])
    (args.output_dir / "check.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"passed": record["passed"], "files": [{key: item[key] for key in
                      ("name", "exitCode", "assertions", "passed", "failures")} for item in record["files"]]}, indent=2))
    raise SystemExit(0 if record["passed"] else 1)


if __name__ == "__main__":
    main()
