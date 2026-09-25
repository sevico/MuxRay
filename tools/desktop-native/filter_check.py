"""Check fixture facts against the real Go CLI's bounded Tag pages.

This checks parser/index data used by filters, not the Wails filter binding or
native GUI. Writes raw CLI outputs and identities to a new absolute directory.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import subprocess


def sha256(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--fixtures-dir", type=Path, required=True)
    parser.add_argument("--muxray", type=Path, required=True)
    parser.add_argument("--output-dir", type=Path, required=True)
    args = parser.parse_args()
    if not all(p.is_absolute() for p in (args.fixtures_dir, args.muxray, args.output_dir)):
        parser.error("Use absolute paths and a new output directory.")
    manifest_path = args.fixtures_dir / "fixtures.json"
    manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    if manifest["format"] != "muxray-desktop-filter-fixtures-v1":
        parser.error("Unsupported manifest format.")
    args.output_dir.mkdir(parents=True, exist_ok=False)
    record = {"format": "muxray-filter-cli-check-v1", "startedUTC": datetime.now(timezone.utc).isoformat(),
              "executable": str(args.muxray), "executableSHA256": sha256(args.muxray),
              "manifestSHA256": sha256(manifest_path), "files": []}
    for source in manifest["files"]:
        name = source["name"]
        if Path(name).name != name:
            raise ValueError("Fixture name must be a basename")
        path = args.fixtures_dir / name
        if path.stat().st_size != source["bytes"] or sha256(path) != source["sha256"]:
            raise ValueError("Fixture identity changed: " + name)
        start, rows, calls, failures = 0, [], [], []
        while True:
            command = [str(args.muxray), "tags", "--start", str(start), "--count", "4096", str(path)]
            output = subprocess.run(command, capture_output=True, timeout=30, check=False)
            (args.output_dir / f"{name}.{start}.stdout.json").write_bytes(output.stdout)
            (args.output_dir / f"{name}.{start}.stderr.txt").write_bytes(output.stderr)
            calls.append({"command": command, "exitCode": output.returncode})
            if output.returncode not in (0, 2):
                failures.append({"exitCode": output.returncode, "expected": "0 or 2"})
                break
            document = json.loads(output.stdout)
            for tag in document["tags"]:
                row = {key: tag[key] for key in ("id", "offset", "typeCode", "dts", "dataSize", "availableDataSize", "payloadComplete")}
                row.update(previousPresent=tag["previousTagSize"]["present"], previousValid=tag["previousTagSize"]["valid"])
                rows.append(row)
            page = document["page"]
            if page["total"] != source["tagCount"]:
                failures.append({"total": page["total"], "expected": source["tagCount"]})
                break
            if page["next"] == page["total"]:
                break
            if page["next"] <= start:
                raise ValueError("CLI cursor failed to advance")
            start = page["next"]
        if rows != source["tags"]:
            failures.append({"error": "Real CLI Tag records differ from fixture facts", "actualCount": len(rows), "expectedCount": len(source["tags"])})
            for i, (actual, expected) in enumerate(zip(rows, source["tags"])):
                if actual != expected:
                    failures[-1].update(firstMismatch=i, actual=actual, expected=expected)
                    break
        record["files"].append({"name": name, "sha256": source["sha256"], "commands": calls,
                                "checkedTags": len(rows), "passed": not failures, "failures": failures})
    record["completedUTC"] = datetime.now(timezone.utc).isoformat()
    record["passed"] = all(file["passed"] for file in record["files"])
    (args.output_dir / "check.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({"passed": record["passed"], "files": [{k: f[k] for k in
                     ("name", "checkedTags", "passed", "failures")} for f in record["files"]]}, indent=2))
    raise SystemExit(0 if record["passed"] else 1)


if __name__ == "__main__":
    main()
