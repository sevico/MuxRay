"""Real local Git histories guard the frozen CLI baseline; no network or GUI."""

import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest

from check_timing_baseline import check, git, main


class TimingBaselineTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        git(self.repo, 'init', '-b', 'main')
        git(self.repo, 'config', 'user.name', 'Baseline Test')
        git(self.repo, 'config', 'user.email', 'baseline@example.invalid')
        self.commit('baseline')
        self.baseline = git(self.repo, 'rev-parse', 'HEAD')
        tree = git(self.repo, 'rev-parse', 'HEAD^{tree}')
        self.commit('feature')
        self.feature = git(self.repo, 'rev-parse', 'HEAD')
        self.commit('current')
        self.entry = {'schemaVersion': 1, 'baselineCommit': self.baseline, 'baselineTree': tree,
                      'featureCommit': self.feature, 'retiredUnavailableCommit': '0' * 40,
                      'registration': 'New baseline, not historical equivalence.'}
        self.manifest = self.repo / 'baseline.json'
        self.write_manifest()

    def commit(self, text):
        (self.repo / 'source').write_text(text)
        git(self.repo, 'add', 'source')
        git(self.repo, '-c', 'commit.gpgsign=false', 'commit', '-m', text)

    def write_manifest(self):
        self.manifest.write_text(json.dumps(self.entry), encoding='utf-8')

    def test_reachable_parent_and_pinned_tree(self):
        result = check(self.repo, self.manifest)
        self.assertTrue(result['passed'])
        self.assertEqual(result['baselineCommit'], self.baseline)
        self.assertEqual(len(result['manifestSHA256']), 64)

    def test_missing_commit_fails_and_preserves_failure_receipt(self):
        self.entry['baselineCommit'] = '0' * 40
        self.write_manifest()
        receipt = self.repo / 'receipt.json'
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(main(['--repo', str(self.repo), '--manifest', str(self.manifest),
                                   '--output', str(receipt)]), 1)
        self.assertFalse(json.loads(receipt.read_text())['passed'])
        with self.assertRaises(FileExistsError):
            main(['--repo', str(self.repo), '--manifest', str(self.manifest), '--output', str(receipt)])

    def test_wrong_tree_rejected(self):
        self.entry['baselineTree'] = git(self.repo, 'rev-parse', 'HEAD^{tree}')
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'tree does not match'):
            check(self.repo, self.manifest)

    def test_unrelated_feature_history_rejected(self):
        git(self.repo, 'checkout', '--detach', self.baseline)
        self.commit('another branch')
        with self.assertRaisesRegex(RuntimeError, 'merge-base'):
            check(self.repo, self.manifest)

    def test_later_baseline_cannot_include_feature(self):
        self.entry['baselineCommit'] = self.feature
        self.entry['baselineTree'] = git(self.repo, 'rev-parse', self.feature + '^{tree}')
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'first parent'):
            check(self.repo, self.manifest)

    def test_reference_aliases_not_accepted(self):
        self.entry['baselineCommit'] = 'HEAD~2'
        self.write_manifest()
        with self.assertRaisesRegex(ValueError, 'full lowercase'):
            check(self.repo, self.manifest)

    def test_printed_commit_is_only_validated_commit(self):
        capture = io.StringIO()
        with contextlib.redirect_stdout(capture):
            self.assertEqual(main(['--repo', str(self.repo), '--manifest', str(self.manifest), '--print-commit']), 0)
        self.assertEqual(capture.getvalue(), self.baseline + '\n')


if __name__ == '__main__':
    unittest.main()
