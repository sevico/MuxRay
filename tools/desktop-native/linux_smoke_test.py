"""No-GUI checks for Linux smoke input, identity and acceptance boundaries."""

import contextlib
import copy
import io
import json
import os
from pathlib import Path
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import Mock, patch

import linux_smoke as smoke


def accessible_combo(names, missing_index=None):
    items = []
    for name in names:
        item = Mock()
        item.name, item.childCount = name, 0
        item.getRoleName.return_value = 'menu item'
        item.queryAction.return_value.nActions = 1
        item.queryAction.return_value.doAction.return_value = True
        items.append(item)
    root = Mock()
    root.childCount = len(items)
    root.getRoleName.return_value = 'combo box'
    root.getChildAtIndex.side_effect = lambda i: None if i == missing_index else items[i]
    return root, items


def forensic_node(name, children=(), pid=42):
    node = Mock()
    node.name, node.description, node.childCount = name, '', len(children)
    node.getRoleName.return_value = 'application' if name == 'app' else 'section'
    node.get_process_id.return_value = pid
    node.getChildAtIndex.side_effect = lambda i: children[i]
    node.queryText.side_effect = NotImplementedError
    return node


class OwnedWindowTest(unittest.TestCase):
    def window(self, pid, title, legacy_property=False):
        window = Mock()
        properties = {'_NET_WM_PID': SimpleNamespace(value=[pid])} if pid is not None else {}
        if not legacy_property:
            properties['_NET_WM_NAME'] = SimpleNamespace(value=title.encode('utf-8'))
        window.get_full_property.side_effect = lambda name, kind: properties.get(name)
        window.get_wm_name.return_value = title
        return window

    def lookup(self, windows, title=None):
        display = Mock()
        display.intern_atom.side_effect = lambda name: name
        display.screen.return_value.root.query_tree.return_value.children = windows
        return smoke.owned_window(display, 42, 0, title)

    def test_selects_only_owned_main_window_for_old_and_versioned_titles(self):
        titles = ['MuxRay · 码流透镜', 'MuxRay dev · 码流透镜', 'MuxRay 0.1.0-dev · 码流透镜',
                  'MuxRay 1.0.0 · 码流透镜', 'MuxRay 12.34.56-rc.9+build.005 · 码流透镜']
        for title in titles:
            with self.subTest(title=title):
                target = self.window(42, title)
                self.assertIs(self.lookup([self.window(17, title), self.window(None, title),
                                          self.window(42, '选择 FLV 文件'), target]), target)

    def test_legacy_wm_name_fallback_retains_pid_fence(self):
        target = self.window(42, 'MuxRay · 码流透镜', legacy_property=True)
        self.assertIs(self.lookup([self.window(17, 'MuxRay · 码流透镜', legacy_property=True), target]), target)

    def test_title_alone_cannot_authorize_foreign_window(self):
        with self.assertRaisesRegex(RuntimeError, 'missing/ambiguous owned window'):
            self.lookup([self.window(17, 'MuxRay 4.5.6 · 码流透镜'),
                         self.window(None, 'MuxRay · 码流透镜')])

    def test_multiple_owned_main_windows_are_rejected_including_mixed_versions(self):
        for second in ['MuxRay 2.3.4 · 码流透镜', 'MuxRay · 码流透镜']:
            with self.subTest(second=second), self.assertRaisesRegex(RuntimeError, 'missing/ambiguous owned window'):
                self.lookup([self.window(42, 'MuxRay 2.3.4 · 码流透镜'), self.window(42, second)])

    def test_owned_similar_titles_are_not_main_windows(self):
        invalid = ['MuxRay 1.2 · 码流透镜', 'MuxRay 01.2.3 · 码流透镜', 'MuxRay 1.2.3-01 · 码流透镜',
                   'MuxRay 1.2.3-rc..1 · 码流透镜', 'MuxRay v1.2.3 · 码流透镜', 'MuxRay DEV · 码流透镜',
                   'MuxRay 1.2.3\n · 码流透镜', 'MuxRay 1.2.3 · 码流透镜 - Save',
                   'Other MuxRay 1.2.3 · 码流透镜', 'MuxRay 1.0.0+' + 'a' * 59 + ' · 码流透镜']
        for title in invalid:
            with self.subTest(title=title), self.assertRaisesRegex(RuntimeError, 'missing/ambiguous owned window'):
                self.lookup([self.window(42, title)])

    def test_picker_lookup_remains_exact_and_cannot_fall_back_to_main(self):
        main = self.window(42, 'MuxRay 8.9.10-beta.2 · 码流透镜')
        picker = self.window(42, '选择 FLV 文件')
        self.assertIs(self.lookup([main, self.window(17, '选择 FLV 文件'), picker], '选择 FLV 文件'), picker)
        with self.assertRaisesRegex(RuntimeError, 'missing/ambiguous owned window'):
            self.lookup([main, self.window(42, '选择 FLV 文件 copy')], '选择 FLV 文件')


class PickerSubmitTest(unittest.TestCase):
    def setUp(self):
        self.now, self.stage, self.check_parent = 0, {}, Mock()
        self.clock = patch.object(smoke.time, 'monotonic', side_effect=lambda: self.now)
        self.sleep = patch.object(smoke.time, 'sleep', side_effect=self.advance)
        self.clock.start()
        self.sleep.start()
        self.addCleanup(self.clock.stop)
        self.addCleanup(self.sleep.stop)

    def advance(self, seconds):
        self.now += seconds

    def button(self, enabled):
        node = Mock()
        node.getState.return_value.contains.return_value = enabled
        node.queryAction.return_value.nActions = 1
        node.queryAction.return_value.doAction.return_value = True
        return node

    def submit(self, lookup, name='Open'):
        smoke.submit_picker_once(lookup, 'enabled', name, self.check_parent, self.stage, seconds=.5)

    def test_waits_for_fresh_enabled_target_then_acts_once(self):
        for name in ('Open', 'Save'):
            with self.subTest(name=name):
                disabled, ready = self.button(False), self.button(True)
                lookup = Mock(side_effect=[[], [disabled], [ready]])
                self.submit(lookup, name)
                self.assertEqual(lookup.call_count, 3)
                disabled.queryAction.assert_not_called()
                ready.queryAction.return_value.doAction.assert_called_once_with(0)
                self.assertEqual([row['enabled'] for row in self.stage['readiness']], [None, False, True])
                self.assertEqual(self.stage['state'], 'actionReturned')
                self.assertTrue(self.stage['actionAccepted'])

    def test_permanent_disabled_or_missing_target_times_out_without_action(self):
        for missing in (False, True):
            with self.subTest(missing=missing):
                disabled = self.button(False)
                lookup = Mock(return_value=[] if missing else [disabled])
                start = self.now
                with self.assertRaisesRegex(RuntimeError, 'timeout: enabled picker target: Open'):
                    self.submit(lookup)
                self.assertAlmostEqual(self.now - start, .5)
                self.assertEqual(lookup.call_count, 3)
                disabled.queryAction.assert_not_called()
                self.assertFalse(self.stage['actionAttempted'])
                self.assertEqual(self.stage['state'], 'readinessTimeout')

    def test_ambiguous_target_is_rejected_even_when_only_one_is_enabled(self):
        disabled, ready = self.button(False), self.button(True)
        lookup = Mock(return_value=[disabled, ready])
        with self.assertRaisesRegex(RuntimeError, 'ambiguous accessible picker target'):
            self.submit(lookup)
        lookup.assert_called_once_with()
        disabled.queryAction.assert_not_called()
        ready.queryAction.assert_not_called()
        self.assertEqual(self.stage['targetCount'], 2)

    def test_parent_exit_during_wait_or_before_action_never_submits(self):
        for before_action in (False, True):
            with self.subTest(before_action=before_action):
                node = self.button(before_action)
                lookup = Mock(return_value=[node])
                self.check_parent.side_effect = [None, RuntimeError('parent exited early: 1')]
                with self.assertRaisesRegex(RuntimeError, 'parent exited early'):
                    self.submit(lookup)
                lookup.assert_called_once_with()
                node.queryAction.return_value.doAction.assert_not_called()
                self.assertFalse(self.stage['actionAttempted'])

    def test_state_query_failure_is_not_treated_as_ready(self):
        node = self.button(True)
        node.getState.side_effect = RuntimeError('AT-SPI state unavailable')
        lookup = Mock(return_value=[node])
        with self.assertRaisesRegex(RuntimeError, 'state unavailable'):
            self.submit(lookup)
        lookup.assert_called_once_with()
        node.queryAction.assert_not_called()

    def test_target_disabled_again_before_action_is_rejected_without_retry(self):
        node = self.button(True)
        node.getState.return_value.contains.side_effect = [True, False]
        lookup = Mock(return_value=[node])
        with self.assertRaisesRegex(RuntimeError, 'disabled target: Open'):
            self.submit(lookup)
        lookup.assert_called_once_with()
        node.queryAction.return_value.doAction.assert_not_called()
        self.assertFalse(self.stage['actionAttempted'])

    def test_readiness_query_that_exhausts_deadline_does_not_submit(self):
        node = self.button(True)

        def slow_lookup():
            self.advance(.5)
            return [node]

        with self.assertRaisesRegex(RuntimeError, 'timeout: enabled picker target'):
            self.submit(slow_lookup)
        node.queryAction.return_value.doAction.assert_not_called()
        self.assertEqual(self.stage['state'], 'readinessTimeout')

    def test_failed_action_is_never_replayed(self):
        for outcome in (RuntimeError('AT-SPI no reply'), False):
            with self.subTest(outcome=outcome):
                node = self.button(True)
                action = node.queryAction.return_value
                if isinstance(outcome, Exception):
                    action.doAction.side_effect = outcome
                else:
                    action.doAction.return_value = outcome
                lookup = Mock(return_value=[node])
                with self.assertRaises(RuntimeError):
                    self.submit(lookup)
                lookup.assert_called_once_with()
                action.doAction.assert_called_once_with(0)
                self.assertTrue(self.stage['actionAttempted'])
                self.assertEqual(self.stage['state'], 'actionPending' if isinstance(outcome, Exception) else 'actionReturned')


class LinuxSmokeTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.argv = ['--binary', sys.executable, '--output-dir', str(self.root / 'new')]

    @patch.object(smoke.time, 'sleep')
    def test_combo_reacquires_all_options_after_missing_middle_child(self, sleep):
        names = ['本页前两帧预览'] + [f'帧 {i}' for i in range(2560, 2624)]
        stale, stale_items = accessible_combo(names, 4)
        fresh, items = accessible_combo(names)
        lookup = Mock(side_effect=[stale, fresh])
        smoke.choose_accessible_option(lookup, 41, 'MP3 帧选择', '帧 2600')
        self.assertEqual(lookup.call_count, 2)
        self.assertTrue(all(not item.queryAction.called for item in stale_items))
        items[41].queryAction.return_value.doAction.assert_called_once_with(0)
        self.assertTrue(all(not item.queryAction.called for i, item in enumerate(items) if i != 41))

    @patch.object(smoke.time, 'sleep')
    def test_combo_persistent_stale_tree_stops_without_action(self, sleep):
        root, items = accessible_combo(['preview', 'frame 0', 'frame 1'], 1)
        lookup = Mock(return_value=root)
        with self.assertRaisesRegex(RuntimeError, 'changed during enumeration'):
            smoke.choose_accessible_option(lookup, 2, 'frames', 'frame 1')
        self.assertEqual(lookup.call_count, 5)
        self.assertEqual(sleep.call_count, 4)
        self.assertTrue(all(not item.queryAction.called for item in items))

    @patch.object(smoke.time, 'sleep')
    def test_combo_rejects_shifted_identity_and_does_not_repeat_failed_action(self, sleep):
        root, items = accessible_combo(['preview', 'frame 1', 'frame 2'])
        with self.assertRaisesRegex(RuntimeError, 'identity differs'):
            smoke.choose_accessible_option(lambda: root, 1, 'frames', 'frame 0')
        self.assertTrue(all(not item.queryAction.called for item in items))
        action = items[1].queryAction.return_value
        action.doAction.return_value = False
        lookup = Mock(return_value=root)
        with self.assertRaisesRegex(RuntimeError, 'action failed'):
            smoke.choose_accessible_option(lookup, 1, 'frames', 'frame 1')
        self.assertEqual(lookup.call_count, 1)
        action.doAction.assert_called_once_with(0)

    def test_defaults_and_supplied_source_default_tag(self):
        args = smoke.arguments(self.argv)
        self.assertEqual((args.tag, args.max_source_bytes, args.timeout_seconds), (4, 16 << 20, 180))
        source = self.root / 'source.flv'
        source.write_bytes(bytes.fromhex('464c5601050000000900000000'))
        args = smoke.arguments(self.argv + ['--source', str(source)])
        self.assertEqual(args.tag, 0)

    def test_direct_mp3_navigation_is_explicit_and_bounded(self):
        args = smoke.arguments(self.argv + ['--mp3-jump-frame', '63', '--mp3-frame-walk', 'next', '--mp3-frame-walk', 'previous'])
        self.assertEqual((args.mp3_jump_frame, args.mp3_frame_walk), (63, ['next', 'previous']))
        for extra in [['--mp3-jump-frame', '-1'], ['--mp3-jump-frame', '4294967296'],
                      ['--mp3-frame-walk', 'next'], ['--mp3-jump-frame', '2600', '--mp3-page-steps', '1'],
                      ['--mp3-jump-frame', '2600', '--mp3-frame', '2600', '--mp3-history'],
                      ['--mp3-jump-frame', '63'] + ['--mp3-frame-walk', 'next'] * 9]:
            with self.subTest(extra=extra), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                smoke.arguments(self.argv + extra)

    def test_avc_evidence_is_explicit_and_preserves_other_defaults(self):
        self.assertFalse(smoke.arguments(self.argv).avc_evidence)
        args = smoke.arguments(self.argv + ['--avc-evidence', '--media-curves', 'video'])
        self.assertTrue(args.avc_evidence)
        self.assertEqual(args.media_curves, 'video')
        self.assertFalse(args.mp3_browse)

    def test_codec_search_options_are_bounded_and_require_one_explicit_flow(self):
        args = smoke.arguments(self.argv + ['--codec-search', 'mp3', '--codec-search-filter', 'parameters',
                     '--codec-search-target', '0:2600:-:-', '--codec-search-frame', '--codec-search-source', 'hex'])
        self.assertEqual((args.codec_search, args.codec_search_filter, args.codec_search_target), ('mp3', ['parameters'], '0:2600:-:-'))
        self.assertEqual(smoke.arguments(self.argv + ['--codec-search', 'sei']).codec_search_filter, ['all'])
        for options in (['--codec-search-filter', 'all'], ['--codec-search', 'sei', '--codec-search-filter', 'bitrate'],
                        ['--codec-search', 'mp3', '--codec-search-filter', 'all', '--codec-search-filter', 'all'],
                        ['--codec-search', 'mp3', '--codec-search-pages', '17'], ['--codec-search', 'mp3', '--codec-search-source', 'hex'],
                        ['--codec-search', 'sei', '--codec-search-frame', '--codec-search-target', '1:-:0:0'],
                        ['--codec-search', 'mp3', '--mp3-jump-frame', '3'],
                        ['--codec-search', 'mp3', '--codec-search-target', '01:2:-:-']):
            with self.subTest(options=options), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                smoke.arguments(self.argv + options)

    def test_rejects_invalid_inputs_before_launch(self):
        invalid = [
            ['--tag', '-1'], ['--tag', '1000000'], ['--tag', 'abc'],
            ['--timeout-seconds', '29'], ['--timeout-seconds', '1801'],
            ['--max-source-bytes', str((16 << 20) - 1)], ['--max-source-bytes', str((1 << 30) + 1)],
            ['--output-dir', 'relative'], ['--output-dir', str(self.root)],
            ['--binary', str(self.root / 'missing')], ['--source', str(self.root)],
            ['--continue-context', '-1'], ['--continue-context', '5'], ['--revisit-tag', '0'],
            ['--continue-context', '2', '--revisit-tag', '4'],
            ['--avc-search-pages', '0'], ['--avc-search-pages', '97'], ['--avc-search-pages', '2'],
            ['--avc-search-long'], ['--avc-search-action', 'cancel'], ['--avc-search-gate', '/tmp/unused'],
            ['--mp3-frame', '0'], ['--mp3-history', '--mp3-frame', '4294967296'],
            ['--mp3-history', '--mp3-frame', '-1'], ['--avc-search', 'all', '--mp3-history'],
            ['--field-name', ''], ['--field-name', '\n'], ['--field-name', 'a' * 257],
        ]
        for extra in invalid:
            with self.subTest(extra=extra), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit) as caught:
                smoke.arguments(self.argv + extra)
            self.assertEqual(caught.exception.code, 2)
        self.assertFalse((self.root / 'new').exists())

    def test_replay_parameters_are_bounded_and_explicit(self):
        args = smoke.arguments(self.argv + ['--tag', '20002', '--continue-context', '2', '--revisit-tag', '8193'])
        self.assertEqual((args.tag, args.continue_context, args.revisit_tag), (20002, 2, 8193))

    def test_search_and_history_require_explicit_bounded_options(self):
        args = smoke.arguments(self.argv + ['--avc-search', 'unknown', '--avc-search-pages', '4'])
        self.assertEqual((args.avc_search, args.avc_search_pages), ('unknown', 4))
        long = smoke.arguments(self.argv + ['--avc-search', 'mismatches', '--avc-search-pages', '96', '--avc-search-long', '--timeout-seconds', '1800'])
        self.assertTrue(long.avc_search_long)
        self.assertEqual(long.avc_search_pages, 96)
        args = smoke.arguments(self.argv + ['--mp3-history', '--mp3-frame', '26'])
        self.assertTrue(args.mp3_history)
        self.assertEqual(args.mp3_frame, 26)
        pages = smoke.arguments(self.argv + ['--mp3-summary', '--mp3-summary-link', '--mp3-page-steps', '37', '--mp3-frame', '2600'])
        self.assertEqual((pages.mp3_summary_link, pages.mp3_page_steps, pages.mp3_frame), (True, 37, 2600))
        for extra in (['--mp3-summary-link'], ['--mp3-page-steps', '65'], ['--mp3-page-steps', '-1']):
            with self.subTest(extra=extra), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                smoke.arguments(self.argv + extra)

    def test_codec_browsing_and_curve_metric_are_explicit(self):
        defaults = smoke.arguments(self.argv)
        self.assertFalse(defaults.mp3_browse)
        self.assertFalse(defaults.mp3_summary)
        self.assertTrue(smoke.arguments(self.argv + ['--mp3-summary']).mp3_summary)
        self.assertEqual(smoke.arguments(self.argv + ['--field-name', 'sliceBetaOffsetDiv2']).field_name, 'sliceBetaOffsetDiv2')
        self.assertIsNone(defaults.media_curves)
        for metric in ('audio', 'video', 'mp3', 'gopAverage', 'gopMax'):
            args = smoke.arguments(self.argv + ['--mp3-browse', '--media-curves', metric])
            self.assertTrue(args.mp3_browse)
            self.assertEqual(args.media_curves, metric)
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            smoke.arguments(self.argv + ['--media-curves', 'gop'])

    def test_source_limit_is_checked_without_reading_large_file(self):
        source = self.root / 'large.flv'
        with source.open('wb') as stream:
            stream.truncate((16 << 20) + 1)
        with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
            smoke.arguments(self.argv + ['--source', str(source)])
        self.assertEqual(smoke.arguments(self.argv + ['--source', str(source), '--max-source-bytes', str(32 << 20)]).source, source)

    def test_generated_fixture_has_full_idr_header_without_changing_other_tags(self):
        from hex_fixtures import fixtures
        old, records = fixtures()['hex-details.flv']
        data = smoke.generated_fixture()
        target = records[4]
        begin, old_end = target['offset'], target['offset'] + 15 + target['payloadBytes']
        self.assertEqual(data[:begin], old[:begin])
        self.assertEqual(data[old_end + 2:], old[old_end:])
        self.assertEqual(int.from_bytes(data[begin + 1:begin + 4], 'big'), 13)
        self.assertEqual(data[begin + 11:begin + 21], bytes.fromhex('17010000000000000465'))
        self.assertEqual(int.from_bytes(data[begin + 24:begin + 28], 'big'), 24)
        # Its existing SPS has POC type 2/frame_num width 4; its PPS enables
        # deblocking syntax, with no redundant_pic_cnt or slice groups.
        # The IDR I header must end after disable_deblocking_filter_idc=1.
        codewords = [
            '1', '011', '1', '0000', '1',  # first_mb=0, I, PPS=0, frame=0, idr_pic_id=0
            '0', '0',                    # no_output_of_prior_pics, long_term_reference
            '1', '010',                  # slice_qp_delta=0, disable_deblocking_filter_idc=1
        ]
        header = ''.join(codewords)
        self.assertEqual(len(header), 16)
        rbsp = ''.join(f'{b:08b}' for b in data[begin + 21:begin + 24])
        self.assertEqual(rbsp, header + '10000000')

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

    def test_forensic_ui_marks_missing_and_failed_reads_without_losing_siblings(self):
        readable, broken = forensic_node('readable'), forensic_node('broken')
        broken.queryText.side_effect = RuntimeError('remote text read failed')
        app = forensic_node('app', [readable, None, broken])
        desktop = forensic_node('desktop', [app])
        result = smoke.failure_ui_tree(desktop, 42, lambda report: None)
        self.assertFalse(result['complete'])
        self.assertEqual([row['name'] for row in result['nodes']], ['app', 'readable', 'broken'])
        self.assertIn('childPresent', [row['operation'] for row in result['errors']])
        self.assertIn('text', [row['operation'] for row in result['errors']])
        self.assertFalse(app.queryAction.called)

    def test_forensic_ui_complete_and_bounded_are_distinct(self):
        child = forensic_node('readable')
        app = forensic_node('app', [child])
        desktop = forensic_node('desktop', [app])
        good = smoke.failure_ui_tree(desktop, 42, lambda report: None)
        self.assertTrue(good['complete'])
        limited = smoke.failure_ui_tree(desktop, 42, lambda report: None, node_budget=1)
        self.assertFalse(limited['complete'])
        self.assertTrue(limited['budgetLimited'])
        self.assertEqual(len(limited['nodes']), 1)
        child.queryText.side_effect = None
        child.queryText.return_value.characterCount = 1201
        child.queryText.return_value.getText.return_value = 'x' * 1200
        truncated = smoke.failure_ui_tree(desktop, 42, lambda report: None)
        self.assertFalse(truncated['complete'])
        self.assertTrue(truncated['textTruncated'])
        absent = smoke.failure_ui_tree(desktop, 999, lambda report: None)
        self.assertFalse(absent['complete'])
        self.assertTrue(absent['traversalFinished'])
        self.assertEqual(absent['ownedRoots'], 0)

    def test_close_action_exception_or_false_result_is_never_replayed(self):
        for outcome in (RuntimeError('AT-SPI no reply'), False):
            stage, action = {}, Mock()
            action.nActions = 1
            if isinstance(outcome, Exception):
                action.doAction.side_effect = outcome
            else:
                action.doAction.return_value = outcome
            with self.subTest(outcome=outcome), self.assertRaises(RuntimeError):
                smoke.accessible_action_once(action, '关闭文件', stage)
            action.doAction.assert_called_once_with(0)
            self.assertTrue(stage['actionAttempted'])
            self.assertEqual(stage['state'], 'actionPending' if isinstance(outcome, Exception) else 'actionReturned')

    def test_failure_probe_timeout_kills_and_reaps_only_probe(self):
        result = smoke.run_failure_probe([sys.executable, '-c', 'import time; time.sleep(60)'], timeout=.05)
        self.assertTrue(result['timedOut'] and result['reaped'])
        self.assertEqual((result['terminationSignal'], result['exitCode']), ('SIGKILL', -9))
        self.assertIsNone(smoke.process(result['pid']))
        self.assertIsNotNone(smoke.process(os.getpid()))
        normal = smoke.run_failure_probe([sys.executable, '-c', 'pass'], timeout=5)
        self.assertFalse(normal['timedOut'])
        self.assertEqual(normal['exitCode'], 0)
        self.assertTrue(normal['reaped'])

    @patch.object(smoke.time, 'sleep')
    def test_failure_probe_saves_resource_samples_before_ui_and_never_actions(self, sleep):
        source = self.root / 'source.flv'
        source.write_bytes(b'FLV')
        identity = smoke.process(os.getpid())
        app = forensic_node('app', pid=os.getpid())
        desktop = forensic_node('desktop', [app])
        registry = Mock()
        registry.getDesktop.return_value = desktop
        request = self.root / 'failure-probe-request.json'
        request.write_text(json.dumps({'pid': os.getpid(), 'source': str(source), 'appIdentity': identity,
                                       'knownProcesses': [identity]}))
        with source.open('rb') as stream, patch.dict(sys.modules, {'pyatspi': SimpleNamespace(Registry=registry)}):
            smoke.failure_probe(request)
            result = json.loads((self.root / 'failure-probe.json').read_text())
            self.assertEqual(len(result['processSamples']), 2)
            for sample in result['processSamples']:
                self.assertTrue(sample['complete'])
                self.assertIn({'pid': os.getpid(), 'fd': str(stream.fileno())}, sample['sourceFDs'])
            self.assertTrue(result['ui']['complete'])
        self.assertEqual(source.read_bytes(), b'FLV')
        self.assertFalse(app.queryAction.called)

        def failed_ui(_):
            partial = json.loads((self.root / 'failure-probe.json').read_text())
            self.assertEqual(len(partial['processSamples']), 2)
            raise RuntimeError('UI bus unavailable')

        registry.getDesktop.side_effect = failed_ui
        with patch.dict(sys.modules, {'pyatspi': SimpleNamespace(Registry=registry)}):
            smoke.failure_probe(request)
        partial = json.loads((self.root / 'failure-probe.json').read_text())
        self.assertFalse(partial['ui']['complete'])
        self.assertIn('UI bus unavailable', partial['ui']['error'])

    def test_failure_observation_keeps_primary_error_and_timeout_partial_receipt(self):
        result = {'passed': False, 'error': 'original close error', 'traceback': 'original stack'}
        with patch.object(smoke, 'capture_failure_observation', side_effect=OSError('probe failed')):
            smoke.record_failure_observation(result, self.root, None, None, {})
        self.assertEqual(result['error'], 'original close error')
        self.assertEqual(result['traceback'], 'original stack')
        self.assertIn('probe failed', result['failureObservationError'])
        self.assertFalse(result['passed'])
        self.assertEqual(json.loads((self.root / 'native-result.json').read_text())['error'], 'original close error')

        def timed_out(command):
            self.assertIn('failure_probe', command[3])
            smoke.save(self.root, 'failure-probe.json', {'processSamples': [{'complete': True, 'sourceFDs': []}],
                       'ui': {'complete': True, 'nodes': [], 'errors': []}})
            return {'timedOut': True, 'terminationSignal': 'SIGKILL', 'reaped': True}

        app = Mock(pid=42)
        app.poll.side_effect = [None, 0]
        with patch.object(smoke, 'run_failure_probe', side_effect=timed_out):
            receipt = smoke.capture_failure_observation(self.root, app, self.root / 'source.flv',
                         {42: {'pid': 42, 'startTicks': 'original'}}, [{'state': 'actionPending'}])
        self.assertEqual((receipt['appPollBefore'], receipt['appPollAfter']), (None, 0))
        self.assertFalse(receipt['ui']['complete'])
        self.assertTrue(receipt['ui']['interrupted'])
        self.assertFalse(receipt['actionReplayed'])
        self.assertNotIn('passed', receipt)

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
