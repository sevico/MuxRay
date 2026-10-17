"""Synthetic unit inputs only; these tests do not establish native acceptance."""

import argparse
import contextlib
import io
import json
from pathlib import Path
import tempfile
import unittest

from codec_search_oracle import generate
from codec_search_test import response
from generate import ROOT, load_expectations
import windows_codec_check as checker


class WindowsCodecCheckTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.directory = Path(self.temp.name)
        generate(self.directory)
        self.expected = load_expectations(self.directory)
        self.case = self.expected['codecSearch']['sei-pagination']
        self.source = self.directory / self.case['file']
        self.paths, previous = [], None
        for index in range(3):
            wire = response(self.case, 'all', index, previous)
            snapshot = json.loads((ROOT / 'testdata/golden/desktop-diagnostic-snapshot-minimal-v4.json').read_text())
            snapshot['fileName'] = self.case['file']
            wire['codecSearchPage']['sessionId'] = 'scan-1'
            snapshot['session']['progress'].update(sourceSize=str(self.source.stat().st_size), bytesScanned=str(self.source.stat().st_size), tagCount='2')
            snapshot['selection']['codecSearch'] = wire['selection']['codecSearch']
            snapshot['codecSearchPage'] = wire['codecSearchPage']
            snapshot['omitted'] = [row for row in snapshot['omitted'] if row['section'] != 'codecSearchPage']
            path = self.directory / f'unit-snapshot-{index}.json'
            path.write_text(json.dumps(snapshot), encoding='utf-8')
            self.paths.append(path)
            previous = wire['codecSearchPage']

    def tearDown(self):
        self.temp.cleanup()

    def mutate(self, index, update):
        data = json.loads(self.paths[index].read_text())
        update(data)
        self.paths[index].write_text(json.dumps(data), encoding='utf-8')

    def test_three_page_sequence_checks_schema_physical_sources_and_zero_io(self):
        result, checks = checker.check_pages(self.paths, self.case, 'all', self.source)
        self.assertTrue(result['page']['exhausted'])
        self.assertEqual([c['codecSearchRowsMatchingSource'] for c in checks], [64, 64, 2])
        self.assertTrue(all(c['schemaValid'] for c in checks))

    def test_schema_wrong_version_and_unknown_fields_fail(self):
        self.mutate(0, lambda s: s.update(schemaVersion=3))
        with self.assertRaises(Exception):
            checker.check_pages(self.paths, self.case, 'all', self.source)
        self.mutate(0, lambda s: s.update(schemaVersion=4, invented=True))
        with self.assertRaises(Exception):
            checker.check_pages(self.paths, self.case, 'all', self.source)

    def test_missing_duplicate_and_wrong_token_pages_fail(self):
        for paths in [self.paths[1:], [self.paths[0], self.paths[0]], [self.paths[0], self.paths[2]]]:
            with self.subTest(paths=paths), self.assertRaises(Exception):
                checker.check_pages(paths, self.case, 'all', self.source)

    def test_zero_io_claim_and_wrong_source_span_fail(self):
        original = self.paths[1].read_bytes()
        self.mutate(1, lambda s: s['codecSearchPage']['page'].update(bytesRead='1'))
        with self.assertRaisesRegex(RuntimeError, 'unexpectedly reread'):
            checker.check_pages(self.paths, self.case, 'all', self.source)
        self.paths[1].write_bytes(original)
        self.mutate(1, lambda s: s['codecSearchPage']['page']['rows'][0]['span'].update(byteOffset='1'))
        with self.assertRaises(Exception):
            checker.check_pages(self.paths, self.case, 'all', self.source)

    def test_no_ui_is_explicit_and_does_not_claim_native_acceptance(self):
        result = checker.run(argparse.Namespace(input_dir=self.directory, case='sei-pagination', operation='pages',
                            ui=None, snapshot=self.paths, filter='all'))
        self.assertTrue(result['passed'])
        self.assertFalse(result['ui']['checked'])
        self.assertEqual(result['nativeAcceptance'], 'not-established-by-this-read-only-checker')

    def test_raw_ui_capture_preserves_hash_and_requires_full_page(self):
        result = json.loads(self.paths[-1].read_text())['codecSearchPage']
        p = result['page']
        text = (f'编解码器检索完成：sei · Tag 1 / 帧 0 → Tag 2 / 帧 0 · 检查 0 个 Tag、'
                f'0 个帧头 · 本批 2 条 · 待显示 0 条 · 读取 0 byte。\n')
        for row in p['rows']:
            text += f'从编解码器检索跳转 {checker.identity(row)} Tag\n定位编解码器 {checker.identity(row)} 证据\n'
        path = self.directory / 'unit-ui.json'
        path.write_text(json.dumps({'content': [{'text': text}]}, ensure_ascii=False), encoding='utf-8')
        observed, receipt = checker.captured_ui(path)
        self.assertEqual(receipt['sha256'], checker.digest(path))
        self.assertEqual(checker.require_ui_page(observed, result)['controlsCompared'], 4)
        with self.assertRaisesRegex(RuntimeError, 'current row/source controls'):
            checker.require_ui_page(observed.replace('1:-:2:1 Tag', 'wrong Tag'), result)
        with self.assertRaisesRegex(RuntimeError, 'frame action'):
            checker.require_ui_page(observed + '\n从编解码器检索跳转 1:-:2:1 帧', result)

    def test_fixture_mutation_fails_before_acceptance(self):
        with self.source.open('ab') as stream:
            stream.write(b'!')
        with self.assertRaises(Exception):
            checker.load_case(self.directory, 'sei-pagination')

    def test_missing_context_ui_cannot_offer_sps_or_activation_actions(self):
        case = self.expected['codecSearch']['sei-missing']
        result = response(case, 'unknown', 0)['codecSearchPage']
        result['page']['pending'] = 0
        text = ('编解码器检索完成：sei · Tag 0 / 帧 0 → Tag 2 / 帧 0 · 检查 2 个 Tag、'
                f'0 个帧头 · 本批 1 条 · 待显示 0 条 · 读取 {result["page"]["bytesRead"]} byte。\n'
                '从编解码器检索跳转 1:-:0:0 Tag\n定位编解码器 1:-:0:0 证据\n')
        self.assertEqual(checker.require_ui_page(text, result)['controlsCompared'], 2)
        for label in ('SPS', '选择依据'):
            with self.subTest(label=label), self.assertRaisesRegex(RuntimeError, 'without an authored span'):
                checker.require_ui_page(text + f'定位编解码器 1:-:0:0 {label}\n', result)

    def test_frame_ui_requires_current_expansion_not_target_elsewhere(self):
        prefix = 'MP3 逐帧详情\nMP3 帧号 2600\n从编解码器检索跳转 0:2600:-:- 帧\n'
        selected = '当前展开 MP3 帧 2600 · 已检查 64 个帧头，64 帧可选（索引从 0 开始）。'
        self.assertEqual(checker.require_ui_frame(prefix + selected, 2600)['coverage'],
                         'frame-panel-and-exact-expanded-frame-status')
        for status in ('', '当前展开 MP3 帧 0', '当前展开 MP3 帧 26000',
                       selected + '\n当前展开 MP3 帧 0'):
            with self.subTest(status=status), self.assertRaisesRegex(RuntimeError, 'exact currently expanded'):
                checker.require_ui_frame(prefix + status, 2600)

    def test_receipt_create_new_preserves_previous_failure(self):
        output = self.directory / 'unit-result.json'
        argv = ['pages', '--input-dir', str(self.directory), '--case', 'sei-pagination', '--filter', 'all',
                '--snapshot', str(self.paths[1]), '--output', str(output)]
        with contextlib.redirect_stdout(io.StringIO()):
            self.assertEqual(checker.main(argv), 1)
        original = output.read_bytes()
        with contextlib.redirect_stdout(io.StringIO()), self.assertRaises(FileExistsError):
            checker.main(argv)
        self.assertEqual(output.read_bytes(), original)

    def test_source_destination_checks_authored_exact_offset(self):
        row = next(r for r in self.case['rows'] if checker.identity(r) == self.case['sourceTarget'])
        snapshot = {'hex': {'window': {'offset': row['span']['byteOffset']}},
                    'codecSearchPage': {'page': {'rows': [row]}}}
        self.assertEqual(checker.check_source(snapshot, self.case, 'hex'), row['span'])
        snapshot['hex']['window']['offset'] = str(int(row['span']['byteOffset']) + 1)
        with self.assertRaisesRegex(RuntimeError, 'authored source'):
            checker.check_source(snapshot, self.case, 'hex')

    @staticmethod
    def source_ui_fragment():
        # Actual node ordering/labels captured by Windows on 2026-10-17;
        # this isolated fragment remains unit-test input, not a native receipt.
        text = (
            '\t284 组 Hex 定位范围\n'
            '\t\t285 文本 编解码器证据 0:2600:-:- hex · 1084225 : 0 · 3336 bit；结束 1084642 : 0（不含）。\n'
            '\t3662 组 Hex 实际窗口\n'
            '\t\t3663 文本 实际窗口 [\n'
            '\t\t3664 文本 1084225\n'
            '\t\t3665 文本 ,\n'
            '\t\t3666 文本 1084737\n'
            '\t\t3667 文本 ) ·\n'
            '\t\t3668 文本 512\n'
            '\t\t3669 文本 byte / 文件\n'
            '\t\t3670 文本 1251029\n'
            '\t\t3671 文本 byte\n'
            '\t3672 按钮 前一窗口\n'
        )
        span = {'byteOffset': '1084225', 'bitOffset': 0, 'bitLength': 3336}
        window = {'offset': '1084225', 'hex': '00' * 512, 'sourceSize': '1251029', 'eof': False}
        return text, span, window

    def test_source_ui_accepts_actual_ordered_ax_text_fragments(self):
        text, span, window = self.source_ui_fragment()
        result = checker.require_ui_source(text, span, window)
        self.assertEqual(result['textMode'], 'ordered-AX-group-text-children')
        self.assertEqual(result['actualWindow'],
                         {'start': 1084225, 'end': 1084737, 'bytes': 512, 'sourceSize': 1251029, 'eof': False})

    def test_source_ui_rejects_reordered_or_wrong_ax_coverage(self):
        text, span, window = self.source_ui_fragment()
        variants = {
            'order': text.replace('3664 文本 1084225', '3664 文本 1084737').replace('3666 文本 1084737', '3666 文本 1084225'),
            'offset': text.replace('3664 文本 1084225', '3664 文本 1084226'),
            'length': text.replace('3668 文本 512', '3668 文本 511'),
            'file size': text.replace('3670 文本 1251029', '3670 文本 1251030'),
            'delimiter': text.replace('\t\t3665 文本 ,\n', ''),
        }
        for label, variant in variants.items():
            with self.subTest(label=label), self.assertRaises(RuntimeError):
                checker.require_ui_source(variant, span, window)

    def test_source_ui_never_borrows_text_from_other_ax_groups(self):
        text, span, window = self.source_ui_fragment()
        variants = {
            'wrong group': text.replace('组 Hex 实际窗口', '组 Unrelated values'),
            'boundary': text.replace('\t\t3665 文本 ,', '\t9999 组 Unrelated values\n\t\t3665 文本 ,'),
            'nested': text.replace('\t\t3664 文本 1084225', '\t\t9999 组 Nested values\n\t\t\t3664 文本 1084225'),
            'selection elsewhere': text.replace('组 Hex 定位范围', '组 Search result tooltip'),
        }
        unrelated_plain_text = '\n实际窗口 [1084225, 1084737) · 512 byte / 文件 1251029 byte\n'
        for label, variant in variants.items():
            with self.subTest(label=label), self.assertRaises(RuntimeError):
                checker.require_ui_source(variant + unrelated_plain_text, span, window)

    def test_source_ui_rejects_conflicting_captured_ax_windows(self):
        text, span, window = self.source_ui_fragment()
        stale = text.replace('3664 文本 1084225', '3664 文本 1084226')
        with self.assertRaisesRegex(RuntimeError, 'actual byte coverage'):
            checker.require_ui_source(text + stale, span, window)

    def test_source_ui_requires_exact_selection_numbers_not_suffixes(self):
        text, span, window = self.source_ui_fragment()
        variants = [
            text.replace('1084225 : 0 · 3336 bit', '11084225 : 0 · 3336 bit'),
            text.replace('1084225 : 0 · 3336 bit', '1084225 : 10 · 3336 bit'),
            text.replace('1084225 : 0 · 3336 bit', '1084225 : 0 · 33336 bit'),
        ]
        for variant in variants:
            with self.subTest(variant=variant), self.assertRaisesRegex(RuntimeError, 'exact source selection'):
                checker.require_ui_source(variant, span, window)
        # The physical Hex window can be correct while the displayed selection
        # points to another offset: 93 must never match the suffix of 193.
        small_span = {'byteOffset': '93', 'bitOffset': 0, 'bitLength': 80}
        small_window = {'offset': '93', 'hex': '00' * 10, 'sourceSize': '103', 'eof': True}
        plain = '193 : 0 · 80 bit\n实际窗口 [93, 103) · 10 byte / 文件 103 byte'
        with self.assertRaisesRegex(RuntimeError, 'exact source selection'):
            checker.require_ui_source(plain, small_span, small_window)
        ax = text.replace('1084225 : 0 · 3336 bit', '193 : 0 · 80 bit')
        ax = ax.replace('3664 文本 1084225', '3664 文本 93').replace('3666 文本 1084737', '3666 文本 103')
        ax = ax.replace('3668 文本 512', '3668 文本 10').replace('3670 文本 1251029', '3670 文本 103')
        with self.assertRaisesRegex(RuntimeError, 'exact source selection'):
            checker.require_ui_source(ax, small_span, small_window)

    def test_source_ui_eof_suffix_must_match_exported_window(self):
        text, span, window = self.source_ui_fragment()
        text = text.replace('3670 文本 1251029', '3670 文本 1084737')
        window.update(sourceSize='1084737', eof=True)
        eof_text = text.replace('\t3672 按钮', '\t\t9999 文本 · 已到 EOF\n\t3672 按钮')
        self.assertTrue(checker.require_ui_source(eof_text, span, window)['actualWindow']['eof'])
        with self.assertRaisesRegex(RuntimeError, 'actual byte coverage'):
            checker.require_ui_source(text, span, window)
        window['eof'] = False
        with self.assertRaisesRegex(RuntimeError, 'actual byte coverage'):
            checker.require_ui_source(eof_text, span, window)
        with self.assertRaisesRegex(RuntimeError, 'ordered complete'):
            checker.require_ui_source(eof_text.replace('已到 EOF', 'unknown suffix'), span, window)

    def test_source_ui_empty_and_contiguous_eof_window_formats(self):
        # HexPanel.tsx keeps the same heading for a zero-byte EOF window.
        span = {'byteOffset': '103', 'bitOffset': 0, 'bitLength': 80}
        window = {'offset': '103', 'hex': '', 'sourceSize': '103', 'eof': True}
        selection = '103 : 0 · 80 bit；结束 113 : 0（不含）。 范围超出当前窗口，仅高亮已读取部分。 声明范围超过文件末尾，缺失字节不会补零显示。'
        heading = '实际窗口 [103, 103) · 0 byte / 文件 103 byte · 已到 EOF'
        result = checker.require_ui_source(selection + '\n' + heading, span, window)
        self.assertEqual(result['actualWindow'],
                         {'start': 103, 'end': 103, 'bytes': 0, 'sourceSize': 103, 'eof': True})
        ax = f'1 组 Hex 定位范围\n\t2 文本 {selection}\n3 组 Hex 实际窗口\n\t4 文本 {heading}\n'
        self.assertEqual(checker.require_ui_source(ax, span, window)['actualWindow']['bytes'], 0)

    def test_source_ui_plain_capture_matches_entire_window(self):
        _, span, window = self.source_ui_fragment()
        text = ('1084225 : 0 · 3336 bit\n'
                '实际窗口 [1084225, 1084737) · 512 byte / 文件 1251029 byte')
        self.assertEqual(checker.require_ui_source(text, span, window)['textMode'], 'contiguous-captured-text')
        with self.assertRaisesRegex(RuntimeError, 'actual byte coverage'):
            checker.require_ui_source(text.replace('1084737', '1084738'), span, window)


if __name__ == '__main__':
    unittest.main()
