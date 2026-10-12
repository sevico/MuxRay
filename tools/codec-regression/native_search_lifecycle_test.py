"""No-GUI rejection checks for controlled native cancel/close evidence."""

from copy import deepcopy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import native_search_lifecycle as lifecycle


class NativeSearchLifecycleTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='muxray-native-lifecycle-test-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name).resolve()
        self.directory, self.gate = self.root / 'native', self.root / 'gate'
        self.directory.mkdir()
        self.gate.mkdir()
        binary = self.gate / 'MuxRay-controlled'
        binary.write_bytes(b'controlled binary')
        self.write(self.gate / 'build-plan.json', {'sourceSHA256': 'fixed-source', 'searchDeadlineSeconds': 90, 'gateSeconds': 60})
        self.write(self.gate / 'build-result.json', {'binarySHA256': lifecycle.digest(binary)})
        self.run = {'binary': str(binary), 'binarySHA256': lifecycle.digest(binary), 'sourceSHA256': 'fixed-source',
                    'avcSearchAction': 'cancel', 'avcSearchGate': str(self.gate)}
        entered = {'runId': 'fixed-run', 'pid': 1234, 'processToken': 'current-generation',
                   'timeUTC': '2026-10-12T00:00:00Z', 'offset': 24, 'bytes': 2}
        self.stage = {'action': 'cancel', 'entered': entered,
                      'canceled': {**entered, 'timeUTC': '2026-10-12T00:00:01Z', 'contextError': 'context canceled'},
                      'released': {**entered, 'timeUTC': '2026-10-12T00:00:02Z', 'contextError': 'context canceled'},
                      'fdsBeforeAction': [{'pid': 1234, 'fd': '7'}], 'fdsBeforeRelease': [{'pid': 1234, 'fd': '7'}],
                      'fdsAfterGatedClose': [], 'openDisabledBeforeRelease': True}
        self.receipt_names = {'entered': 'entered.json', 'canceled': 'context-canceled.json', 'released': 'released.json'}
        self.save_receipts()
        for kind, text in (('pending', '正在从 Tag 0 检查本批证据…'), ('command', '正在确认 H.264 检索取消…'), ('terminal', '任务已取消。')):
            self.ui(kind, text)
        self.snapshot = {'schemaVersion': 3, 'avcSearchPage': None, 'omitted': [{'section': 'avcSearchPage', 'reason': 'not_requested'}]}
        self.snapshot_path = self.directory / 'avc-gate-canceled-snapshot.json'
        self.save_snapshot()
        self.save_native()
        build_patch = patch.object(lifecycle, 'validate_build')
        self.build_mock = build_patch.start()
        self.addCleanup(build_patch.stop)
        # The lower-level gate tests cover receipt authentication. This layer
        # must compare its copied records with the actual on-disk receipts.
        receipt_patch = patch.object(lifecycle, 'receipt', side_effect=lambda directory, name, pid: json.loads((directory / name).read_text()))
        self.receipt_mock = receipt_patch.start()
        self.addCleanup(receipt_patch.stop)

    @staticmethod
    def write(path, value):
        path.write_text(json.dumps(value, ensure_ascii=False) + '\n')

    def save_native(self):
        self.write(self.directory / 'run.json', self.run)
        self.write(self.directory / 'native-result.json', {'pid': 1234, 'avcSearchGate': self.stage})

    def save_receipts(self):
        for kind, name in self.receipt_names.items():
            self.write(self.gate / name, self.stage[kind])

    def save_snapshot(self):
        self.write(self.snapshot_path, self.snapshot)
        self.stage['canceledSnapshot'] = {'schemaValid': True, 'sha256': lifecycle.digest(self.snapshot_path)}

    def ui(self, kind, text):
        path = self.directory / (kind + '-ui.json')
        self.write(path, [{'role': 'statusbar', 'text': text}])
        self.stage[kind + 'UI'], self.stage[kind + 'UISHA256'] = path.name, lifecycle.digest(path)

    def verify(self, action='cancel'):
        return lifecycle.verify_gate(self.directory, self.gate, action)

    def test_valid_cancel_and_close_keep_controlled_identity(self):
        for action in ('cancel', 'close'):
            self.stage['action'] = self.run['avcSearchAction'] = action
            self.ui('command', '正在确认 H.264 检索取消…' if action == 'cancel' else '正在释放当前文件；底层 I/O 结束后才能打开下一个文件。')
            self.save_native()
            result = self.verify(action)
            self.assertEqual(result['action'], action)
            self.assertTrue(result['acceptanceOnly'] and result['blockedSourceRetained'] and result['releaseAfterObservedCommand'])
            self.assertEqual(result['contextError'], 'context canceled')
            self.assertEqual(result['gateSeconds'], 2)
            self.assertEqual(result['actualRead'], {'offset': 24, 'bytes': 2})
        self.build_mock.assert_called_with(self.gate, Path(self.run['binary']))
        self.receipt_mock.assert_any_call(self.gate, 'context-canceled.json', 1234)

    def test_equal_reversed_times_and_unobservable_read_are_rejected(self):
        original = deepcopy(self.stage)
        for kind, key, value in (('canceled', 'timeUTC', self.stage['entered']['timeUTC']),
                                 ('released', 'timeUTC', '2026-10-11T23:59:59Z'),
                                 ('entered', 'bytes', 0), ('entered', 'bytes', 1048577), ('entered', 'offset', -1)):
            self.stage = deepcopy(original)
            self.stage[kind][key] = value
            self.save_receipts()
            self.save_native()
            with self.subTest(kind=kind, key=key), self.assertRaisesRegex(RuntimeError, 'gate/order'):
                self.verify()

    def test_deadline_and_safety_timeout_cannot_count_as_cancellation(self):
        original = deepcopy(self.stage)
        for kind in ('canceled', 'released'):
            self.stage = deepcopy(original)
            self.stage[kind]['contextError'] = 'context deadline exceeded'
            self.save_receipts()
            self.save_native()
            with self.subTest(kind=kind), self.assertRaisesRegex(RuntimeError, 'timeout is not cancellation'):
                self.verify()
        self.stage = original
        self.save_receipts()
        self.save_native()
        self.write(self.gate / 'timed-out.json', {'safetyRelease': True})
        with self.assertRaisesRegex(RuntimeError, 'timeout is not cancellation'):
            self.verify()

    def test_copied_receipt_run_pid_token_and_build_hash_must_match(self):
        original = deepcopy(self.stage)
        for key, value in (('runId', 'another-run'), ('pid', 5678), ('processToken', 'older-generation')):
            self.stage = deepcopy(original)
            self.stage['canceled'][key] = value
            self.save_native()
            with self.subTest(key=key), self.assertRaisesRegex(RuntimeError, 'receipts differ'):
                self.verify()
        self.stage = original
        for key in ('binarySHA256', 'sourceSHA256', 'avcSearchGate', 'avcSearchAction'):
            previous = self.run[key]
            self.run[key] = 'changed'
            self.save_native()
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                self.verify()
            self.run[key] = previous

    def test_ui_hashes_and_observed_states_are_not_trusted_from_result(self):
        original = deepcopy(self.stage)
        for kind in ('pending', 'command', 'terminal'):
            self.stage = deepcopy(original)
            self.stage[kind + 'UISHA256'] = 'changed'
            self.save_native()
            with self.subTest(kind=kind), self.assertRaisesRegex(RuntimeError, 'UI evidence changed'):
                self.verify()
        for kind in ('pending', 'command'):
            self.stage = deepcopy(original)
            path = self.directory / self.stage[kind + 'UI']
            previous = path.read_bytes()
            self.ui(kind, 'not the observed state')
            self.save_native()
            with self.subTest(kind=kind), self.assertRaises(RuntimeError):
                self.verify()
            path.write_bytes(previous)

    def test_fd_ownership_and_disabled_open_are_required_for_close(self):
        self.stage['action'] = self.run['avcSearchAction'] = 'close'
        self.ui('command', '正在释放当前文件；底层 I/O 结束后才能打开下一个文件。')
        original = deepcopy(self.stage)
        for key, value in (('fdsBeforeAction', []), ('fdsBeforeRelease', []),
                           ('fdsAfterGatedClose', [{'pid': 1234, 'fd': '7'}]), ('openDisabledBeforeRelease', False)):
            self.stage = deepcopy(original)
            self.stage[key] = value
            self.save_native()
            with self.subTest(key=key), self.assertRaises(RuntimeError):
                self.verify('close')

    def test_canceled_snapshot_hash_schema_and_ready_page_are_rejected(self):
        original = deepcopy(self.snapshot)
        for key, value in (('schemaVersion', 2), ('avcSearchPage', {'state': 'ready', 'page': {'rows': []}}), ('omitted', [])):
            self.snapshot = {**original, key: value}
            self.save_snapshot()
            self.save_native()
            with self.subTest(key=key), self.assertRaisesRegex(RuntimeError, 'invented a completed page'):
                self.verify()
        self.snapshot = original
        self.save_snapshot()
        for key, value in (('schemaValid', False), ('sha256', 'changed')):
            previous = self.stage['canceledSnapshot'][key]
            self.stage['canceledSnapshot'][key] = value
            self.save_native()
            with self.subTest(key=key), self.assertRaisesRegex(RuntimeError, 'snapshot identity'):
                self.verify()
            self.stage['canceledSnapshot'][key] = previous


if __name__ == '__main__':
    unittest.main()
