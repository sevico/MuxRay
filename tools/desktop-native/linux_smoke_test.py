"""No-GUI checks for Linux smoke input, identity and acceptance boundaries."""

import contextlib
import copy
import io
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import linux_smoke as smoke


class LinuxSmokeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.argv = ['--binary', sys.executable, '--output-dir', str(self.root / 'new')]

    def test_defaults_and_supplied_source_default_tag(self):
        args = smoke.arguments(self.argv)
        self.assertEqual((args.tag, args.max_source_bytes, args.timeout_seconds), (4, 16 << 20, 180))
        source = self.root / 'source.flv'
        source.write_bytes(bytes.fromhex('464c5601050000000900000000'))
        args = smoke.arguments(self.argv + ['--source', str(source)])
        self.assertEqual(args.tag, 0)

    def test_rejects_invalid_inputs_before_launch(self):
        invalid = [
            ['--tag', '-1'], ['--tag', '1000000'], ['--tag', 'abc'],
            ['--timeout-seconds', '29'], ['--timeout-seconds', '601'],
            ['--max-source-bytes', str((16 << 20) - 1)], ['--max-source-bytes', str((1 << 30) + 1)],
            ['--output-dir', 'relative'], ['--output-dir', str(self.root)],
            ['--binary', str(self.root / 'missing')], ['--source', str(self.root)],
            ['--continue-context', '-1'], ['--continue-context', '5'], ['--revisit-tag', '0'],
            ['--continue-context', '2', '--revisit-tag', '4'],
        ]
        for extra in invalid:
            with self.subTest(extra=extra), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as caught:
                smoke.arguments(self.argv + extra)
            self.assertEqual(caught.exception.code, 2)
        self.assertFalse((self.root / 'new').exists())

    def test_replay_parameters_are_bounded_and_explicit(self):
        args = smoke.arguments(self.argv + ['--tag', '20002', '--continue-context', '2', '--revisit-tag', '8193'])
        self.assertEqual((args.tag, args.continue_context, args.revisit_tag), (20002, 2, 8193))

    def test_source_limit_is_checked_without_reading_large_file(self):
        source = self.root / 'large.flv'
        with source.open('wb') as stream:
            stream.truncate((16 << 20) + 1)
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            smoke.arguments(self.argv + ['--source', str(source)])
        self.assertEqual(smoke.arguments(self.argv + ['--source', str(source), '--max-source-bytes', str(32 << 20)]).source, source)

    def test_generated_fixture_has_valid_slice_prefix_without_changing_other_tags(self):
        from hex_fixtures import fixtures
        old, records = fixtures()['hex-details.flv']
        data = smoke.generated_fixture()
        target = records[4]
        begin, old_end = target['offset'], target['offset'] + 15 + target['payloadBytes']
        self.assertEqual(data[:begin], old[:begin])
        self.assertEqual(data[old_end + 1:], old[old_end:])
        self.assertEqual(int.from_bytes(data[begin + 1:begin + 4], 'big'), 12)
        self.assertEqual(data[begin + 11:begin + 23], bytes.fromhex('17010000000000000365b840'))
        self.assertEqual(int.from_bytes(data[begin + 23:begin + 27], 'big'), 23)
        # Ten encoded syntax bits: ue(0), ue(2), ue(0), u(4)=0, ue(0).
        self.assertEqual(''.join(f'{b:08b}' for b in data[begin + 21:begin + 23])[:10], '1011100001')

    def test_proc_stat_comm_and_pid_reuse(self):
        fields = ['S', '123'] + ['0'] * 17 + ['456']
        self.assertEqual(smoke.parse_stat('42 (name with ) spaces) ' + ' '.join(fields)),
                         {'ppid': 123, 'startTicks': '456', 'state': 'S'})
        old = {'pid': 42, 'startTicks': '456'}
        with patch.object(smoke, 'process', return_value={'pid': 42, 'startTicks': '457', 'state': 'S'}):
            self.assertFalse(smoke.alive(old))
            self.assertFalse(smoke.running(old))
        with patch.object(smoke, 'process', return_value=None):
            self.assertFalse(smoke.running(old))

    def test_fd_check_uses_device_inode(self):
        source = self.root / 'source.flv'
        source.write_bytes(b'FLV')
        alias = self.root / 'alias.flv'
        os.link(source, alias)
        row = smoke.process(os.getpid())
        with alias.open('rb') as stream:
            self.assertIn({'pid': os.getpid(), 'fd': str(stream.fileno())}, smoke.source_fds([row], source))
        self.assertEqual(smoke.source_fds([row], source), [])

    def test_export_requires_selected_tag_detail_and_matching_hex(self):
        report = {'session': {'scan': {'complete': True}}, 'selectedTag': {'id': '4', 'offset': '100'},
                  'detail': {'detail': {'complete': True}}, 'hex': {'window': {'offset': '100'}}}
        smoke.validate_export(report, 4, True)
        for change in ('partial', 'tag', 'detail', 'hex', 'hexOffset', 'generatedDetail'):
            altered = copy.deepcopy(report)
            if change == 'partial':
                altered['session']['scan']['complete'] = False
            elif change == 'tag':
                altered['selectedTag']['id'] = '3'
            elif change in ('detail', 'hex'):
                altered[change] = None
            elif change == 'hexOffset':
                altered['hex']['window']['offset'] = '101'
            else:
                altered['detail']['detail']['complete'] = False
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                smoke.validate_export(altered, 4, True)
        # User media can validly return bounded partial syntax with diagnostics.
        altered = copy.deepcopy(report)
        altered['detail']['detail']['complete'] = False
        smoke.validate_export(altered, 4, False)


if __name__ == '__main__':
    unittest.main()
