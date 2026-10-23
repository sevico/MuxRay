"""Offline candidate safety tests; no production build, network or GUI."""
import copy
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
import zipfile

import candidate as c

COMMIT = 'a' * 40
RAW = '''app.exe: go1.27.2
 path github.com/sevico/muxray/desktop
 dep example.org/library v1.0.0
 build -buildmode=exe
 build -tags=desktop,wv2runtime.error,production
 build -trimpath=true
 build GOARCH=amd64
 build GOOS=windows
'''


class CandidateTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.repo = self.root / 'repo'
        self.repo.mkdir()
        for file in c.INPUTS:
            path = self.repo / file
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(file, encoding='utf-8')
        (self.repo / 'LICENSE').write_text('Project MIT license\n', encoding='utf-8')
        self.notes = self.repo / 'notes.md'
        self.notes.write_text('# MuxRay v1.2.3\n\nCandidate changes.\n', encoding='utf-8')
        self.exe = self.root / 'MuxRay.exe'
        self.exe.write_bytes(b'MZ-test-executable')
        self.audit = self.root / 'audit'
        (self.audit / 'licenses/library').mkdir(parents=True)
        self.notice = self.audit / 'licenses/library/LICENSE'
        self.notice.write_text('Test library permissive notice.\n', encoding='utf-8')
        (self.audit / 'THIRD-PARTY-NOTICES.txt').write_text('Third party notices\n' + self.notice.read_text(), encoding='utf-8')
        self.metadata = c.binary_metadata(RAW)
        self.inventory = {
            'schemaVersion': 1,
            'auditedBinary': {'sha256': c.sha(self.exe), 'bytes': self.exe.stat().st_size, 'goVersion': 'go1.27.2', 'buildTags': c.TAGS},
            'missingLicenseTexts': [],
            'inputs': [{'file': 'REPO/' + f, 'sha256': c.sha(self.repo / f)} for f in c.INPUTS],
            'components': [{'id': 'library', 'name': 'example.org/library', 'version': 'v1.0.0',
                            'inclusionBasis': {'kind': 'production-go-module'},
                            'noticeSources': [{'copiedFile': 'licenses/library/LICENSE', 'copiedSHA256': c.sha(self.notice), 'bytes': self.notice.stat().st_size}]}],
        }
        self.save_inventory()
        self.output = self.root / 'candidate'
        self.output.mkdir()

    def save_inventory(self):
        c.write_json(self.audit / 'license-inventory.json', self.inventory)

    def package(self):
        with patch.object(c, 'check_source'):
            return c.package(self.repo, '1.2.3', COMMIT, self.exe, self.audit, self.notes, self.output, self.metadata)

    def check_audit(self):
        return c.validate_audit(self.audit, self.exe, self.metadata, self.repo)

    def test_package_round_trip(self):
        assets = self.package()
        result = c.verify(assets)
        self.assertEqual(result['status'], 'candidate')
        self.assertEqual(result['extractedFiles'], 8)
        info = c.read_json(assets / 'BUILD-INFO.json')
        self.assertEqual(info['sourceCommit'], COMMIT)
        self.assertEqual(info['verification']['nativeAcceptance'], 'not-run-by-this-tool')
        self.assertNotIn(str(self.root), json.dumps(info))

    def test_invalid_versions(self):
        for value in ['dev', 'v1.2.3', '1.02.3', '1.2.3-01', '1.2.3;exit', '../1.2.3', '1.2.3\n', '1.2.3-' + 'x' * 64]:
            with self.subTest(value=value), self.assertRaises(ValueError):
                c.validate_version(value)
        for value in ['0.2.0-dev', '1.2.3', '1.2.3-rc.1+build.1']:
            c.validate_version(value)

    def test_wrong_commit(self):
        with patch.object(c, 'run', return_value='b' * 40), self.assertRaisesRegex(ValueError, 'HEAD'):
            c.check_source(self.repo, COMMIT)

    def test_dirty_source(self):
        for status in [' M README.md', '?? secret.txt']:
            with patch.object(c, 'run', side_effect=[COMMIT, status]), self.assertRaisesRegex(ValueError, 'clean'):
                c.check_source(self.repo, COMMIT)

    def test_abbreviated_commit(self):
        with self.assertRaisesRegex(ValueError, 'full lowercase'):
            c.check_source(self.repo, 'abcd123')

    def test_real_git_clean_and_dirty_checks(self):
        subprocess.run(['git', 'init', '-q', self.repo], check=True)
        subprocess.run(['git', 'add', '.'], cwd=self.repo, check=True)
        subprocess.run(['git', '-c', 'user.name=Candidate Test', '-c', 'user.email=test@example.invalid', 'commit', '-qm', 'fixture'], cwd=self.repo, check=True)
        commit = c.run(['git', 'rev-parse', 'HEAD'], self.repo).strip()
        c.check_source(self.repo, commit)
        (self.repo / 'LICENSE').write_text('changed', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'clean'):
            c.check_source(self.repo, commit)

    def test_license_wrong_binary(self):
        self.exe.write_bytes(b'different executable')
        with self.assertRaisesRegex(ValueError, 'different executable'):
            self.check_audit()

    def test_license_missing(self):
        self.notice.unlink()
        with self.assertRaisesRegex(ValueError, 'missing'):
            self.check_audit()

    def test_license_tampered(self):
        self.notice.write_text('altered', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'hash mismatch'):
            self.check_audit()

    def test_unresolved_licenses(self):
        self.inventory['missingLicenseTexts'] = ['missing']
        self.save_inventory()
        with self.assertRaisesRegex(ValueError, 'incomplete'):
            self.check_audit()

    def test_missing_combined_notice(self):
        (self.audit / 'THIRD-PARTY-NOTICES.txt').write_text('incomplete\n', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'omit'):
            self.check_audit()

    def test_modules_do_not_match_binary(self):
        self.inventory['components'][0]['version'] = 'v9.9.9'
        self.save_inventory()
        with self.assertRaisesRegex(ValueError, 'Go module coverage'):
            self.check_audit()

    def test_source_manifest_hash_mismatch(self):
        (self.repo / 'go.mod').write_text('changed', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'inputs changed'):
            self.check_audit()

    def test_source_manifest_missing(self):
        self.inventory['inputs'].pop()
        self.save_inventory()
        with self.assertRaisesRegex(ValueError, 'input manifest'):
            self.check_audit()

    def test_manifest_path_escape(self):
        self.inventory['components'][0]['noticeSources'][0]['copiedFile'] = 'licenses/../../secret.txt'
        self.save_inventory()
        with self.assertRaisesRegex(ValueError, 'traversal'):
            self.check_audit()

    def test_nonportable_paths(self):
        for value in ['../file', '/file', 'a//b', 'a/./b', 'C:/private', 'a\\b', 'a/file.', 'a/file ']:
            with self.subTest(value=value), self.assertRaises(ValueError):
                c.relative_name(value)

    def test_private_paths_rejected(self):
        for text in ['C:\\Users\\someone\\source', '/home/test/project', '/tmp/project', 'D:/workspace/repo',
                     r'\\server\share\source', '//server/share/source', r'\\?\UNC\server\share\source']:
            self.notes.write_text(text, encoding='utf-8')
            with self.subTest(text=text), self.assertRaisesRegex(ValueError, 'Private absolute'):
                c.public_text(self.notes)

    def test_public_urls_are_not_private_paths(self):
        self.notes.write_text('https://github.com/sevico/MuxRay\nhttps://www.unicode.org/license.txt\n', encoding='utf-8')
        c.public_text(self.notes)

    def test_notes_wrong_version(self):
        self.notes.write_text('# MuxRay v0.1.0\n', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'heading'):
            self.package()

    def test_incorrect_production_metadata(self):
        for before, after in [('GOOS=windows', 'GOOS=linux'), ('GOARCH=amd64', 'GOARCH=arm64'),
                              ('-trimpath=true', '-trimpath=false'), ('production', 'production,pickerprocess'),
                              ('muxray/desktop', 'other/app')]:
            with self.subTest(before=before), self.assertRaises(ValueError):
                c.binary_metadata(RAW.replace(before, after))

    def test_existing_output_not_overwritten(self):
        self.package()
        with self.assertRaises(FileExistsError):
            self.package()

    def test_checksum_tamper(self):
        assets = self.package()
        (assets / 'BUILD-INFO.json').write_text('{}', encoding='utf-8')
        with self.assertRaisesRegex(ValueError, 'checksum mismatch'):
            c.verify(assets)

    def test_fake_success_claim_rejected(self):
        assets = self.package()
        info = c.read_json(assets / 'BUILD-INFO.json')
        info['verification']['ci'] = 'passed'
        c.write_json(assets / 'BUILD-INFO.json', info)
        self.rehash_assets(assets)
        with self.assertRaisesRegex(ValueError, 'unsupported validation'):
            c.verify(assets)

    def rehash_assets(self, assets):
        files = sorted(p for p in assets.iterdir() if p.name != 'SHA256SUMS.txt')
        (assets / 'SHA256SUMS.txt').write_text(''.join(f'{c.sha(p)}  {p.name}\n' for p in files), encoding='ascii')

    def test_extra_zip_file_rejected(self):
        assets = self.package()
        archive = next(assets.glob('*.zip'))
        with zipfile.ZipFile(archive, 'a') as zipped:
            zipped.writestr('../secret.txt', 'secret')
        self.rehash_assets(assets)
        with self.assertRaisesRegex(ValueError, 'whitelist'):
            c.verify(assets)

    def test_unlisted_source_files_not_packaged(self):
        (self.audit / 'local-source-files.json').write_text('private evidence', encoding='utf-8')
        (self.repo / 'secret.txt').write_text('do not ship', encoding='utf-8')
        assets = self.package()
        with zipfile.ZipFile(next(assets.glob('*.zip'))) as zipped:
            self.assertFalse(any('secret' in n or 'local-source-files' in n for n in zipped.namelist()))


if __name__ == '__main__':
    unittest.main()
