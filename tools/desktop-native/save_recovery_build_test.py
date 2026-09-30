"""Validate acceptance overlay generation and evidence handling without building a GUI."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest.mock import patch

import save_recovery_build as recovery


# Deliberately small input keeps these tests independent of evolving product
# behavior. Parsing checks syntax; product/native tests verify the real behavior.
SOURCE = '''package main

import (
    "errors"
    "os"
    "path/filepath"
    "strconv"
    "strings"
)

func save() SaveResponse {
    writer := writeSnapshotFile
	if err := writer(path, captured.Data, source, commit); err != nil {
        return finish(saveFailure(request.RequestID, "write_failed", "Failed"))
    }
    result := saveResponse(request.RequestID)
	result.BytesWritten = strconv.Itoa(len(captured.Data))
	return finish(result)
}
'''


class SaveRecoveryBuildTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="muxray-save-overlay-")
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name).resolve()
        self.repository = self.root / "repository"
        (self.repository / "desktop/frontend/dist").mkdir(parents=True)
        (self.repository / "desktop/save.go").write_text(SOURCE, encoding="utf-8")
        (self.repository / "desktop/frontend/dist/index.html").write_text("test assets", encoding="utf-8")
        (self.repository / "go.mod").write_text("module example\n", encoding="utf-8")
        self.output = self.root / "acceptance"

    def prepare(self, mode="lost_reply"):
        return recovery.prepare(self.repository, self.output, mode)

    def receipt(self, plan, **changes):
        record = {"runId": plan["runId"], "mode": "close_during_save", "pid": 1234,
                  "processToken": "1234-observed-start", "requestId": "1",
                  "timeUTC": "2026-09-29T00:00:00Z"}
        record.update(changes)
        recovery.write_json(self.output / "entered.json", record)

    def test_each_mode_generates_one_hook_and_explicit_identity(self):
        for mode in recovery.MODES:
            with self.subTest(mode=mode):
                generated = recovery.patch_source(SOURCE, mode, self.output, self.output / "input.flv", "run-id")
                self.assertEqual(generated.count("acceptanceRun :="), 1)
                self.assertEqual(generated.count(recovery.WRITER_ANCHOR), 1)
                self.assertIn(f'const snapshotAcceptanceMode = "{mode}"', generated)
                self.assertIn('const snapshotAcceptanceRunID = "run-id"', generated)
                self.assertIn("time.Sleep(8 * time.Second)", generated)
                self.assertIn("time.Now().Add(30 * time.Second)", generated)
                self.assertIn("result = finish(result)", generated)
                self.assertLess(generated.index("result = finish(result)"), generated.index('"completed.json"'))
                self.assertIn("return originalWriter(path, data, source, commit)", generated)
        self.assertEqual((self.repository / "desktop/save.go").read_text(encoding="utf-8"), SOURCE)

    def test_generated_modes_parse_as_go_without_compilation(self):
        formatter = shutil.which("gofmt")
        if formatter is None:
            self.skipTest("gofmt unavailable; syntax parse runs on the Go-enabled CI host")
        for mode in recovery.MODES:
            generated = recovery.patch_source(SOURCE, mode, self.output, self.output / "input.flv", "run-id")
            parsed = subprocess.run([formatter], input=generated, text=True, capture_output=True, check=False)
            self.assertEqual(parsed.returncode, 0, parsed.stderr)

    def test_imports_are_not_duplicated(self):
        source = SOURCE.replace("import (\n", 'import (\n\t"encoding/json"\n\t"sync"\n\t"time"\n')
        generated = recovery.patch_source(source, "lost_reply", self.output, self.output / "input.flv", "r")
        for name in ("encoding/json", "sync", "time"):
            self.assertEqual(generated.count(f'"{name}"'), 1)

    def test_current_product_source_supports_all_overlay_anchors_and_go_syntax(self):
        source = (Path(__file__).resolve().parents[2] / "desktop/save.go").read_text(encoding="utf-8-sig")
        formatter = shutil.which("gofmt")
        for mode in recovery.MODES:
            generated = recovery.patch_source(source, mode, self.output, self.output / "input.flv", "run-id")
            if formatter:
                parsed = subprocess.run([formatter], input=generated, text=True, capture_output=True, check=False)
                self.assertEqual(parsed.returncode, 0, parsed.stderr)

    def test_rejects_bad_mode_or_changed_anchors_before_output(self):
        with self.assertRaisesRegex(ValueError, "Unknown"):
            recovery.patch_source(SOURCE, "unknown", self.output, self.output / "i.flv", "r")
        for anchor in ("import (\n", recovery.WRITER_ANCHOR, recovery.RETURN_ANCHOR):
            for source in (SOURCE.replace(anchor, ""), SOURCE + anchor):
                with self.subTest(anchor=anchor), self.assertRaisesRegex(ValueError, "exactly one"):
                    recovery.patch_source(source, "lost_reply", self.output, self.output / "i.flv", "r")
        self.assertFalse(self.output.exists())

    def test_refuses_second_instrumentation(self):
        generated = recovery.patch_source(SOURCE, "lost_reply", self.output, self.output / "i.flv", "r")
        with self.assertRaisesRegex(ValueError, "already instrumented"):
            recovery.patch_source(generated, "lost_reply", self.output, self.output / "i.flv", "r")

    def test_preparation_records_overlay_fixture_and_asset_identity_without_source_edits(self):
        before = recovery.input_hashes(self.repository)
        plan = self.prepare()
        self.assertEqual(before, recovery.input_hashes(self.repository))
        self.assertEqual(plan["inputs"], before)
        self.assertTrue(plan["acceptanceOnly"])
        self.assertEqual(plan["fixture"]["bytes"], 497)
        self.assertEqual(plan["fixture"]["sha256"], recovery.digest(self.output / "native-save-controlled.flv"))
        self.assertEqual(plan["overlaySourceSHA256"], recovery.digest(self.output / "save_acceptance.go"))
        overlay = json.loads((self.output / "overlay.json").read_text(encoding="utf-8"))
        self.assertEqual(overlay, {"Replace": {str(self.repository / "desktop/save.go"): str(self.output / "save_acceptance.go")}})
        self.assertFalse((self.output / "MuxRay-save-acceptance.exe").exists())

    def test_output_is_new_and_outside_repository(self):
        with self.assertRaisesRegex(ValueError, "outside"):
            recovery.prepare(self.repository, self.repository / "run", "lost_reply")
        self.prepare()
        files = {p.name: p.read_bytes() for p in self.output.iterdir()}
        with self.assertRaises(FileExistsError):
            self.prepare()
        self.assertEqual(files, {p.name: p.read_bytes() for p in self.output.iterdir()})

    def test_requires_absolute_paths_and_built_assets(self):
        with self.assertRaisesRegex(ValueError, "absolute"):
            recovery.prepare(Path("relative-repository"), self.output, "lost_reply")
        (self.repository / "desktop/frontend/dist/index.html").unlink()
        with self.assertRaisesRegex(ValueError, "frontend"):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_custom_fixture_is_copied_and_invalid_fixture_leaves_no_run(self):
        fixture = self.root / "input.flv"
        fixture.write_bytes(b"invalid fixture")
        with self.assertRaisesRegex(ValueError, "FLV"):
            recovery.prepare(self.repository, self.output, "lost_reply", fixture)
        self.assertFalse(self.output.exists())
        fixture.write_bytes(b"FLV\x01custom")
        recovery.prepare(self.repository, self.output, "lost_reply", fixture)
        self.assertEqual((self.output / "native-save-controlled.flv").read_bytes(), fixture.read_bytes())

    def test_input_hashes_notice_added_source_or_changed_assets(self):
        before = recovery.input_hashes(self.repository)
        (self.repository / "desktop/new.go").write_text("package main\n", encoding="utf-8")
        after_source = recovery.input_hashes(self.repository)
        self.assertNotEqual(before, after_source)
        (self.repository / "desktop/frontend/dist/index.html").write_text("changed", encoding="utf-8")
        self.assertNotEqual(after_source, recovery.input_hashes(self.repository))

    @unittest.skipUnless(os.name == "nt", "Acceptance build is Windows only")
    def test_build_refuses_changed_inputs_without_invoking_go(self):
        self.prepare()
        (self.repository / "desktop/save.go").write_text(SOURCE + "\n", encoding="utf-8")
        with patch.object(recovery.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "changed"):
                recovery.build_run(self.output)
            run.assert_not_called()

    @unittest.skipUnless(os.name == "nt", "Acceptance build is Windows only")
    def test_build_refuses_changed_overlay_or_existing_executable_without_invoking_go(self):
        self.prepare()
        overlay = self.output / "save_acceptance.go"
        original = overlay.read_bytes()
        overlay.write_bytes(original + b"\n")
        with patch.object(recovery.subprocess, "run") as run:
            with self.assertRaisesRegex(ValueError, "overlay"):
                recovery.build_run(self.output)
            overlay.write_bytes(original)
            (self.output / "MuxRay-save-acceptance.exe").write_bytes(b"preserve")
            with self.assertRaises(FileExistsError):
                recovery.build_run(self.output)
            run.assert_not_called()
        self.assertEqual((self.output / "MuxRay-save-acceptance.exe").read_bytes(), b"preserve")

    def test_release_copies_run_and_process_identity_and_never_replaces_evidence(self):
        plan = self.prepare("close_during_save")
        self.receipt(plan)
        result = recovery.release_run(self.output)
        self.assertEqual({k: result[k] for k in ("runId", "pid", "processToken")},
                         {"runId": plan["runId"], "pid": 1234, "processToken": "1234-observed-start"})
        self.assertEqual(result["action"], "ReleaseAfterObservedClose")
        original = (self.output / "release.json").read_bytes()
        with self.assertRaises(FileExistsError):
            recovery.release_run(self.output)
        self.assertEqual((self.output / "release.json").read_bytes(), original)
        self.assertEqual(list(self.output.glob(".release-*.tmp")), [])

    def test_release_refuses_another_run(self):
        plan = self.prepare("close_during_save")
        self.receipt(plan, runId="unrelated-run")
        with self.assertRaisesRegex(ValueError, "matching"):
            recovery.release_run(self.output)
        self.assertFalse((self.output / "release.json").exists())

    def test_release_refuses_other_modes_or_expired_gate(self):
        plan = self.prepare("lost_reply")
        self.receipt(plan)
        with self.assertRaisesRegex(ValueError, "matching"):
            recovery.release_run(self.output)
        plan["mode"] = "close_during_save"
        (self.output / "build-plan.json").write_text(json.dumps(plan), encoding="utf-8")
        recovery.write_json(self.output / "timed-out.json", {})
        with self.assertRaisesRegex(ValueError, "matching"):
            recovery.release_run(self.output)
        self.assertFalse((self.output / "release.json").exists())

    def test_release_refuses_invalid_process_identity(self):
        plan = self.prepare("close_during_save")
        self.receipt(plan, pid=0, processToken="")
        with self.assertRaisesRegex(ValueError, "matching"):
            recovery.release_run(self.output)
        self.assertFalse((self.output / "release.json").exists())


if __name__ == "__main__":
    unittest.main()
