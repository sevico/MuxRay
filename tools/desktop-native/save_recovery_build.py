"""Prepare an opt-in save-recovery Go overlay; never alter production sources.

Use a new absolute run directory outside the repository. Build the production
frontend first. This tool records and reuses those assets, without installing
dependencies or launching a GUI. --build-run builds only the separate acceptance
exe offline; --release-run releases close_during_save after an observed UI close.
Never reuse a run or distribute its executable as a production build.
"""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import subprocess
import uuid

from summary_fixtures import fixtures as summary_fixtures


MODES = ("lost_reply", "write_failure", "close_during_save")
WRITER_ANCHOR = "\tif err := writer(path, captured.Data, source, commit); err != nil {"
RETURN_ANCHOR = "\tresult.BytesWritten = strconv.Itoa(len(captured.Data))\n\treturn finish(result)"

WRITER_HOOK = r'''
	// ACCEPTANCE OVERLAY ONLY: one operation on this run's copied fixture.
	acceptanceRun := snapshotAcceptanceClaim(captured.SourcePath)
	if acceptanceRun && snapshotAcceptanceMode != "lost_reply" {
		originalWriter := writer
		writer = func(path string, data []byte, source snapshotSource, commit func(func() error) error) error {
			if snapshotAcceptanceMode == "write_failure" {
				if err := snapshotAcceptanceReceipt("write-failed.json", map[string]any{"requestId": request.RequestID, "fileName": filepath.Base(path), "forcedBeforeDestinationIO": true}); err != nil { return err }
				return errors.New("acceptance-only forced save failure")
			}
			if err := snapshotAcceptanceWait(request.RequestID, path); err != nil { return err }
			return originalWriter(path, data, source, commit)
		}
	}
'''
RETURN_HOOK = r'''	result.BytesWritten = strconv.Itoa(len(captured.Data))
	result = finish(result)
	if acceptanceRun {
		// The real write and retained native result are complete before delaying
		// the SaveSnapshot reply. GetSaveResult remains available during the wait.
		if err := snapshotAcceptanceReceipt("completed.json", map[string]any{"requestId": request.RequestID, "result": result}); err != nil { return result }
		if snapshotAcceptanceMode == "lost_reply" {
			time.Sleep(8 * time.Second)
			_ = snapshotAcceptanceReceipt("return-released.json", map[string]any{"requestId": request.RequestID})
		}
	}
	return result'''

HELPERS = r'''

// ACCEPTANCE OVERLAY ONLY. This code is not in the normal production source.
const snapshotAcceptanceMode = @MODE@
const snapshotAcceptanceDirectory = @DIRECTORY@
const snapshotAcceptanceFixture = @FIXTURE@
const snapshotAcceptanceRunID = @RUNID@
var snapshotAcceptanceOnce sync.Once
var snapshotAcceptanceProcessToken = strconv.Itoa(os.Getpid()) + "-" + time.Now().UTC().Format(time.RFC3339Nano)

func snapshotAcceptanceClaim(source string) bool {
	if !strings.EqualFold(filepath.Clean(source), filepath.Clean(snapshotAcceptanceFixture)) { return false }
	claimed := false
	snapshotAcceptanceOnce.Do(func() { claimed = true })
	return claimed
}

func snapshotAcceptanceReceipt(name string, extra map[string]any) error {
	record := map[string]any{"runId": snapshotAcceptanceRunID, "mode": snapshotAcceptanceMode,
		"pid": os.Getpid(), "processToken": snapshotAcceptanceProcessToken, "timeUTC": time.Now().UTC().Format(time.RFC3339Nano)}
	for key, value := range extra { record[key] = value }
	data, err := json.Marshal(record)
	if err != nil { return err }
	temp, err := os.CreateTemp(snapshotAcceptanceDirectory, ".save-receipt-*.tmp")
	if err != nil { return err }
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(append(data, '\n'))
	if writeErr == nil { writeErr = temp.Sync() }
	closeErr := temp.Close()
	if writeErr != nil { return writeErr }
	if closeErr != nil { return closeErr }
	// A same-volume hard link publishes complete bytes and refuses overwrite.
	return os.Link(temp.Name(), filepath.Join(snapshotAcceptanceDirectory, name))
}

func snapshotAcceptanceWait(requestID, path string) error {
	if err := snapshotAcceptanceReceipt("entered.json", map[string]any{"requestId": requestID, "fileName": filepath.Base(path), "timeoutSeconds": 30}); err != nil { return err }
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(filepath.Join(snapshotAcceptanceDirectory, "release.json"))
		var release struct { RunID string `json:"runId"`; PID int `json:"pid"`; ProcessToken string `json:"processToken"` }
		if err == nil && json.Unmarshal(data, &release) == nil && release.RunID == snapshotAcceptanceRunID && release.PID == os.Getpid() && release.ProcessToken == snapshotAcceptanceProcessToken {
			return snapshotAcceptanceReceipt("released.json", map[string]any{"requestId": requestID})
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = snapshotAcceptanceReceipt("timed-out.json", map[string]any{"requestId": requestID})
	return errors.New("acceptance save gate timed out before destination I/O")
}
'''


def utc():
    return datetime.now(timezone.utc).isoformat()


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def safe_absolute(path):
    if not path.is_absolute():
        raise ValueError("Use an absolute path")
    for part in (path, *path.parents):
        if part.is_symlink() or getattr(part, "is_junction", lambda: False)():
            raise ValueError("Symlink/junction run paths are not supported")
    return path.resolve()


def patch_source(source, mode, directory, fixture, run_id):
    if mode not in MODES:
        raise ValueError("Unknown acceptance mode")
    if "snapshotAcceptance" in source:
        raise ValueError("Refusing an already instrumented source")
    for anchor in ("import (\n", WRITER_ANCHOR, RETURN_ANCHOR):
        if source.count(anchor) != 1:
            raise ValueError(f"Expected exactly one save anchor: {anchor!r}")
    additions = "".join(f'\t"{name}"\n' for name in ("encoding/json", "sync", "time") if f'"{name}"' not in source)
    patched = source.replace("import (\n", "import (\n" + additions, 1)
    patched = patched.replace(WRITER_ANCHOR, WRITER_HOOK + WRITER_ANCHOR, 1).replace(RETURN_ANCHOR, RETURN_HOOK, 1)
    helpers = HELPERS
    for key, value in {"@MODE@": mode, "@DIRECTORY@": str(directory), "@FIXTURE@": str(fixture), "@RUNID@": run_id}.items():
        helpers = helpers.replace(key, json.dumps(value, ensure_ascii=False))
    return patched + helpers


def input_hashes(repository):
    files = {repository / "go.mod", repository / "go.sum", repository / "desktop/go.mod",
             repository / "desktop/go.sum", repository / "desktop/wails.json"}
    for directory in (repository / "internal", repository / "desktop"):
        files.update(path for path in directory.glob("*.go") if path.is_file())
    files.update(path for path in (repository / "internal").rglob("*.go") if path.is_file())
    files.update(path for path in (repository / "desktop").glob("*.syso") if path.is_file())
    for name in ("src", "wailsjs", "dist"):
        files.update(path for path in (repository / "desktop/frontend" / name).rglob("*") if path.is_file())
    for name in ("package.json", "package-lock.json", "vite.config.ts", "tsconfig.json", "index.html"):
        files.add(repository / "desktop/frontend" / name)
    return {path.relative_to(repository).as_posix(): digest(path) for path in sorted(files) if path.is_file()}


def write_json(path, value):
    with path.open("x", encoding="utf-8") as stream:
        json.dump(value, stream, ensure_ascii=False, indent=2)
        stream.write("\n")


def prepare(repository, output, mode, fixture=None):
    repository, output = safe_absolute(repository), safe_absolute(output)
    if output == repository or repository in output.parents:
        raise ValueError("The acceptance run must be outside the repository")
    source_path = repository / "desktop/save.go"
    source = source_path.read_text(encoding="utf-8-sig")
    if not (repository / "desktop/frontend/dist/index.html").is_file():
        raise ValueError("Build the production frontend before preparing a run")
    copied_fixture = output / "native-save-controlled.flv"
    run_id = str(uuid.uuid4())
    patched = patch_source(source, mode, output, copied_fixture, run_id)
    data = safe_absolute(fixture).read_bytes() if fixture is not None else summary_fixtures()["summary-mixed.flv"][0]
    if not data.startswith(b"FLV") or len(data) > 16 << 20:
        raise ValueError("Use a deterministic FLV fixture no larger than 16 MiB")
    before = input_hashes(repository)
    output.mkdir(parents=True, exist_ok=False)
    with copied_fixture.open("xb") as stream:
        stream.write(data)
    overlay_source = output / "save_acceptance.go"
    with overlay_source.open("x", encoding="utf-8", newline="\n") as stream:
        stream.write(patched)
    overlay = output / "overlay.json"
    write_json(overlay, {"Replace": {str(source_path): str(overlay_source)}})
    record = {"format": "muxray-save-recovery-build-v1", "acceptanceOnly": True, "runId": run_id,
              "mode": mode, "preparedAtUTC": utc(), "repository": str(repository), "runDirectory": str(output),
              "fixture": {"path": str(copied_fixture), "bytes": len(data), "sha256": digest(copied_fixture)},
              "inputs": before, "overlaySHA256": digest(overlay), "overlaySourceSHA256": digest(overlay_source),
              "toolSHA256": digest(Path(__file__)), "buildTags": "desktop,wv2runtime.error,production",
              "purpose": "Controlled save recovery only; never distribute this executable; launch each run once.",
              "limitations": "Reuses already built frontend assets; receipts do not prove GUI observations; 8s reply delay or 30s gate maximum; first matching save only."}
    write_json(output / "build-plan.json", record)
    return record


def build_run(output):
    output = safe_absolute(output)
    plan = json.loads((output / "build-plan.json").read_text(encoding="utf-8"))
    repository = Path(plan["repository"])
    if os.name != "nt" or plan["runDirectory"] != str(output) or not plan.get("acceptanceOnly"):
        raise ValueError("Build this acceptance plan on its original Windows host")
    if input_hashes(repository) != plan["inputs"] or digest(Path(__file__)) != plan["toolSHA256"]:
        raise ValueError("Source/frontend/tool changed; prepare a new run")
    for name, key in (("overlay.json", "overlaySHA256"), ("save_acceptance.go", "overlaySourceSHA256")):
        if digest(output / name) != plan[key]:
            raise ValueError("Acceptance overlay identity changed")
    if digest(output / "native-save-controlled.flv") != plan["fixture"]["sha256"]:
        raise ValueError("Acceptance fixture identity changed")
    exe = output / "MuxRay-save-acceptance.exe"
    for name in (exe.name, "build-result.json", "build.stdout.log", "build.stderr.log"):
        if (output / name).exists():
            raise FileExistsError(output / name)
    command = ["go", "build", "-mod=readonly", "-trimpath", "-tags", plan["buildTags"],
               "-ldflags", "-H windowsgui", "-overlay", str(output / "overlay.json"), "-o", str(exe), "."]
    environment = {**os.environ, "GOTOOLCHAIN": "local", "GOPROXY": "off", "GOSUMDB": "off",
                   "GOOS": "windows", "GOARCH": "amd64", "CGO_ENABLED": "1"}
    with (output / "build.stdout.log").open("xb") as stdout, (output / "build.stderr.log").open("xb") as stderr:
        result = subprocess.run(command, cwd=repository / "desktop", env=environment, stdout=stdout, stderr=stderr,
                                creationflags=subprocess.CREATE_NO_WINDOW, check=False)
    unchanged = input_hashes(repository) == plan["inputs"]
    has_executable = exe.is_file() and exe.stat().st_size > 0
    record = {"runId": plan["runId"], "mode": plan["mode"], "acceptanceOnly": True, "builtAtUTC": utc(),
              "command": command, "workingDirectory": str(repository / "desktop"), "exitCode": result.returncode,
              "inputsUnchanged": unchanged, "passed": result.returncode == 0 and unchanged and has_executable,
              "exe": {"path": str(exe), "bytes": exe.stat().st_size, "sha256": digest(exe)} if exe.exists() else None}
    write_json(output / "build-result.json", record)
    if not record["passed"]:
        raise RuntimeError("Acceptance build failed or inputs changed; keep evidence and use a new run")
    return record


def release_run(output):
    output = safe_absolute(output)
    plan = json.loads((output / "build-plan.json").read_text(encoding="utf-8"))
    entered = json.loads((output / "entered.json").read_text(encoding="utf-8"))
    if (plan["mode"] != "close_during_save" or plan["runDirectory"] != str(output) or not plan.get("acceptanceOnly")
            or entered.get("mode") != plan["mode"] or entered["runId"] != plan["runId"]
            or type(entered.get("pid")) is not int or entered["pid"] <= 0 or not entered.get("processToken")
            or (output / "timed-out.json").exists()):
        raise ValueError("No matching active close-during-save receipt")
    receipt = {key: entered[key] for key in ("runId", "pid", "processToken")}
    receipt.update(timeUTC=utc(), action="ReleaseAfterObservedClose")
    # Completed JSON is atomically published without replacing prior evidence.
    temporary = output / (".release-" + str(uuid.uuid4()) + ".tmp")
    write_json(temporary, receipt)
    try:
        os.link(temporary, output / "release.json")
    finally:
        temporary.unlink()
    return receipt


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", type=Path)
    parser.add_argument("--output-dir", type=Path)
    parser.add_argument("--mode", choices=MODES)
    parser.add_argument("--fixture", type=Path)
    parser.add_argument("--build-run", type=Path)
    parser.add_argument("--release-run", type=Path)
    args = parser.parse_args()
    if args.build_run or args.release_run:
        if (args.build_run and args.release_run) or any((args.repository, args.output_dir, args.mode, args.fixture)):
            parser.error("Use build-run or release-run alone")
        result = build_run(args.build_run) if args.build_run else release_run(args.release_run)
    else:
        if not all((args.repository, args.output_dir, args.mode)):
            parser.error("Preparation requires repository, output-dir, and mode")
        plan = prepare(args.repository, args.output_dir, args.mode, args.fixture)
        result = {key: plan[key] for key in ("runId", "mode", "runDirectory", "fixture", "purpose")}
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
