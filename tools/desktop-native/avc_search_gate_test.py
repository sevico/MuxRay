"""No-GUI checks for controlled AVC read-gate identity and release evidence."""

import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import avc_search_gate as gate


ROOT = Path(__file__).resolve().parents[2]


class AVCSearchGateTests(unittest.TestCase):
    def setUp(self):
        if shutil.which('gofmt') is None:
            self.skipTest('gofmt is required by the controlled build tool')
        temporary = tempfile.TemporaryDirectory(prefix='muxray-avc-gate-test-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.repository = self.root / 'repository'
        (self.repository / 'internal/document').mkdir(parents=True)
        (self.repository / 'desktop/frontend/dist').mkdir(parents=True)
        self.search = (ROOT / 'internal/document/avc_search.go').read_text()
        self.document = (ROOT / 'internal/document/document.go').read_text()
        (self.repository / 'internal/document/avc_search.go').write_text(self.search)
        (self.repository / 'internal/document/document.go').write_text(self.document)
        (self.repository / 'desktop/frontend/dist/index.html').write_text('fixed assets')
        (self.repository / 'go.mod').write_text('module example\n')
        self.source = self.root / 'source.flv'
        self.source.write_bytes(bytes.fromhex('464c5601050000000900000000'))
        self.output = self.root / 'controlled'

    def prepare(self):
        return gate.prepare(self.repository, self.source, self.output)

    def receipts(self, plan, context='context canceled'):
        entered = {'runId': plan['runId'], 'pid': 1234, 'processToken': '1234-current-generation',
                   'timeUTC': '2026-10-12T00:00:00Z', 'offset': 24, 'bytes': 2, 'source': plan['source']}
        gate.write_json(self.output / 'entered.json', entered)
        gate.write_json(self.output / 'context-canceled.json', {**entered, 'contextError': context})
        return entered

    def test_current_product_seams_and_generated_go_syntax(self):
        search, document = gate.patch_sources(self.search, self.document, self.output, self.source, 'fixed-run')
        self.assertEqual(search.count('avcsearch.Search(ctx, &acceptanceSearchReader{'), 1)
        self.assertNotIn('avcsearch.Search(ctx, d.source, d.scan.Tag', search)
        self.assertEqual(document.count('timeout = 90 * time.Second'), 1)
        self.assertIn('if selected.kind == avcSearchWork', document)
        self.assertIn('time.NewTimer(60 * time.Second)', search)
        self.assertIn('return r.source.ReadAt(p, offset)', search)
        for patched in (search, document):
            parsed = subprocess.run(['gofmt'], input=patched, text=True, capture_output=True, check=False, timeout=30)
            self.assertEqual(parsed.returncode, 0, parsed.stderr)

    def test_missing_ambiguous_and_already_instrumented_seams_are_rejected(self):
        anchor = 'avcsearch.Search(ctx, d.source, d.scan.Tag'
        deadline = '\t\tctx, cancel := context.WithTimeout(d.ctx, timeout)'
        for source in (self.search.replace(anchor, ''), self.search + anchor, self.search.replace('\t"context"', '')):
            with self.subTest(source=source[-100:]), self.assertRaisesRegex(ValueError, 'seam changed'):
                gate.patch_sources(source, self.document, self.output, self.source, 'r')
        for document in (self.document.replace(deadline, ''), self.document + deadline):
            with self.assertRaisesRegex(ValueError, 'seam changed'):
                gate.patch_sources(self.search, document, self.output, self.source, 'r')
        patched, document = gate.patch_sources(self.search, self.document, self.output, self.source, 'r')
        with self.assertRaisesRegex(ValueError, 'seam changed'):
            gate.patch_sources(patched, document, self.output, self.source, 'r')

    def test_prepare_is_separate_copied_and_immutable(self):
        plan = self.prepare()
        self.assertTrue(plan['acceptanceOnly'])
        self.assertEqual((plan['gateSeconds'], plan['searchDeadlineSeconds']), (60, 90))
        self.assertEqual(Path(plan['source']).read_bytes(), self.source.read_bytes())
        self.assertNotEqual(Path(plan['source']).stat().st_ino, self.source.stat().st_ino)
        self.assertEqual((self.repository / 'internal/document/avc_search.go').read_text(), self.search)
        self.assertEqual((self.repository / 'internal/document/document.go').read_text(), self.document)
        self.assertEqual(gate.validate_plan(self.output), plan)
        replacements = json.loads((self.output / 'overlay.json').read_text())['Replace']
        self.assertEqual(set(replacements), {str(self.repository / 'internal/document' / name) for name in ('avc_search.go', 'document.go')})
        self.assertTrue(all(Path(value).parent == self.output for value in replacements.values()))
        with self.assertRaises(FileExistsError):
            self.prepare()

    def test_inside_repository_and_invalid_fixtures_fail_before_output(self):
        for target in (self.repository, self.repository / 'build/controlled'):
            with self.assertRaisesRegex(ValueError, 'outside the repository'):
                gate.prepare(self.repository, self.source, target)
        for data in (b'FLV', b'BAD' + bytes(10)):
            self.source.write_bytes(data)
            with self.assertRaisesRegex(ValueError, 'FLV'):
                self.prepare()
            self.assertFalse(self.output.exists())
        self.source.write_bytes(bytes.fromhex('464c5601050000000900000000'))
        with self.source.open('r+b') as stream:
            stream.truncate((16 << 20) + 1)
        with self.assertRaisesRegex(ValueError, 'FLV'):
            self.prepare()
        self.assertFalse(self.output.exists())

    def test_plan_rejects_overlay_fixture_tool_and_input_drift(self):
        plan = self.prepare()
        for name in ('overlay.json', 'avc_search.go', 'document.go', 'controlled-avc.flv'):
            path = self.output / name
            original = path.read_bytes()
            path.write_bytes(original + b'changed')
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, 'identity changed'):
                gate.validate_plan(self.output)
            path.write_bytes(original)
        plan_path = self.output / 'build-plan.json'
        original = plan_path.read_bytes()
        plan_path.write_text(json.dumps({**plan, 'toolSHA256': 'changed'}))
        with self.assertRaisesRegex(ValueError, 'tool identity changed'):
            gate.validate_plan(self.output)
        plan_path.write_bytes(original)
        asset = self.repository / 'desktop/frontend/dist/index.html'
        asset.write_text('new assets')
        with self.assertRaisesRegex(ValueError, 'source/assets changed'):
            gate.validate_plan(self.output)
        # A historical build keeps its captured identities after repository edits.
        self.assertEqual(gate.validate_plan(self.output, check_inputs=False), plan)

    def test_binary_is_validated_against_its_exact_frozen_build(self):
        self.prepare()
        binary = self.output / 'MuxRay-avc-controlled'
        binary.write_bytes(b'fixed controlled executable')
        result = {'passed': True, 'binary': str(binary), 'binarySHA256': gate.digest(binary),
                  'buildPlanSHA256': gate.digest(self.output / 'build-plan.json')}
        gate.write_json(self.output / 'build-result.json', result)
        gate.validate_build(self.output, binary)
        other = self.root / 'MuxRay-production'
        other.write_bytes(binary.read_bytes())
        with self.assertRaisesRegex(ValueError, 'executable identity differs'):
            gate.validate_build(self.output, other)
        binary.write_bytes(b'replaced executable')
        with self.assertRaisesRegex(ValueError, 'executable identity differs'):
            gate.validate_build(self.output, binary)

    def test_receipts_reject_wrong_run_pid_and_process_generation(self):
        plan = self.prepare()
        entered = self.receipts(plan)
        self.assertEqual(gate.receipt(self.output, 'entered.json', 1234), entered)
        with self.assertRaisesRegex(ValueError, 'PID/run/token'):
            gate.receipt(self.output, 'entered.json', 5678)
        path = self.output / 'context-canceled.json'
        original = json.loads(path.read_text())
        for field, value in (('runId', 'another-run'), ('pid', 5678), ('processToken', ''), ('processToken', 'older-generation')):
            path.write_text(json.dumps({**original, field: value}))
            with self.subTest(field=field, value=value), self.assertRaisesRegex(ValueError, 'differs'):
                gate.receipt(self.output, 'context-canceled.json', 1234)

    def test_release_requires_native_cancellation_and_preserves_evidence(self):
        plan = self.prepare()
        entered = self.receipts(plan, context='context deadline exceeded')
        with self.assertRaisesRegex(ValueError, 'native command'):
            gate.release(self.output, 1234)
        self.assertFalse((self.output / 'release.json').exists())
        path = self.output / 'context-canceled.json'
        canceled = {**entered, 'contextError': 'context canceled'}
        path.write_text(json.dumps(canceled))
        for marker in ('timed-out.json', 'released.json'):
            (self.output / marker).write_text('{}')
            with self.subTest(marker=marker), self.assertRaisesRegex(ValueError, 'native command'):
                gate.release(self.output, 1234)
            (self.output / marker).unlink()
        self.assertEqual(gate.release(self.output, 1234), canceled)
        release = self.output / 'release.json'
        expected = {key: entered[key] for key in ('runId', 'pid', 'processToken')}
        self.assertEqual(json.loads(release.read_text()), expected)
        self.assertFalse((self.output / 'release.json.tmp').exists())
        original = release.read_bytes()
        with self.assertRaises(FileExistsError):
            gate.release(self.output, 1234)
        self.assertEqual(release.read_bytes(), original)

    def test_current_overlay_compiles_document_without_running_gui(self):
        if shutil.which('go') is None:
            self.skipTest('Go is required to compile the overlay')
        gate.prepare(ROOT, self.source, self.output)
        result = subprocess.run(['go', 'test', '-overlay', str(self.output / 'overlay.json'), '-run', '^$', './internal/document'],
                                cwd=ROOT, env=dict(os.environ, GOTOOLCHAIN=os.environ.get('GOTOOLCHAIN', 'go1.27.2')),
                                text=True, capture_output=True, timeout=180, check=False)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
