"""Authored bytes and adversarial checks for paginated codec evidence."""

import copy
from pathlib import Path
import tempfile
import unittest

from codec_search_oracle import generate, retained
from codec_search_verify import identity, page_rows, page_ui
from generate import avc_nals, flv_tags, load_expectations
from paged_codec_oracle import sei_oracle


def response(case, filter_name, index, previous=None):
    counts = case['pages'][filter_name]
    begin = sum(counts[:index])
    rows = retained(case['rows'], case['kind'], filter_name)[begin:begin + counts[index]]
    end = index == len(counts) - 1
    start = {'tagId': '0', 'frameIndex': 0} if previous is None else previous['page']['next']
    page = dict(kind=case['kind'], filter=filter_name, cursor='0' if previous is None else previous['page']['nextCursor'],
                nextCursor='0' if end else str(index + 1), start=start,
                next={'tagId': str(case['indexedTags']) if end else '1', 'frameIndex': 0},
                total=str(case['indexedTags']), scannedTags=0 if index else 2, scannedFrames=0,
                pending=[66, 2, 0][index], bytesRead='0' if index else '600', exhausted=end, scanComplete=True, rows=rows)
    wire = dict(apiVersion=1, sessionId='1', requestId=str(index + 1), state='ready', error=None, page=page)
    return {'schemaVersion': 4, 'selection': {'schemaVersion': 4, 'codecSearch': {
                'requestId': wire['requestId'], 'cursor': page['cursor'], 'kind': case['kind'], 'filter': filter_name}},
            'session': {'sessionId': '1'}, 'codecSearchPage': wire}


class CodecSearchTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.directory = Path(cls.temp.name)
        generate(cls.directory)
        cls.expected = load_expectations(cls.directory)

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_raw_long_frame_changes_only_at_authored_frame_2600(self):
        case = self.expected['codecSearch']['mp3-late-change']
        data = (self.directory / case['file']).read_bytes()
        tag = flv_tags(data)[0]
        begin = tag['payloadOffset'] + 1
        modes = [data[begin + n * 417 + 3] >> 6 for n in range(3000)]
        self.assertEqual(modes, [1] * 2600 + [3] * 400)
        row = case['rows'][0]
        self.assertEqual((row['offset'], row['span']['bitLength']), (str(begin + 2600 * 417), 417 * 8))
        self.assertEqual(len(case['rows']), 1)

    def test_stops_have_five_distinct_original_headers_and_no_synthetic_resync(self):
        case = self.expected['codecSearch']['mp3-bitrate-stops']
        data = (self.directory / case['file']).read_bytes()
        rows = retained(case['rows'], 'mp3', 'stops')
        self.assertEqual([r['status'] for r in rows], ['truncatedHeader', 'malformedHeader', 'unsupportedLayer', 'freeFormat', 'truncatedFrame'])
        self.assertEqual([data[int(r['span']['byteOffset']):int(r['span']['byteOffset']) + min(4, r['span']['bitLength'] // 8)].hex() for r in rows],
                         ['fffb', '00000000', 'fffd9064', 'fffb0064', 'fffb9064'])
        self.assertEqual([r['frameIndex'] for r in rows], [0] * 5)
        self.assertEqual([r['kind'] for r in retained(case['rows'], 'mp3', 'bitrate')], ['bitrate', 'bitrate'])

    def test_130_messages_are_three_real_nals_and_all_identities_unique(self):
        case = self.expected['codecSearch']['sei-pagination']
        data = (self.directory / case['file']).read_bytes()
        nals = [n for n in avc_nals(data, flv_tags(data))[1] if n['type'] == 6]
        self.assertEqual([len(sei_oracle(data[n['offset']:n['offset'] + n['bytes']], n['offset'])['messages']) for n in nals], [64, 64, 2])
        self.assertEqual(len({identity(row) for row in case['rows']}), 130)
        self.assertEqual(identity(case['rows'][-1]), '1:-:2:1')

    def test_context_references_previous_tags_and_unknown_filter_preserves_coverage(self):
        case = self.expected['codecSearch']['sei-cross-256']
        self.assertEqual(case['indexedTags'], 259)
        bp, pt = case['rows']
        self.assertEqual((bp['tagId'], pt['tagId']), ('1', '258'))
        self.assertEqual(pt['spsSpan'], bp['spsSpan'])
        self.assertEqual(pt['evidenceSpan'], bp['evidenceSpan'])
        self.assertLess(int(pt['evidenceSpan']['byteOffset']), int(pt['span']['byteOffset']))
        budget = self.expected['codecSearch']['sei-budget']
        self.assertEqual(len(retained(budget['rows'], 'sei', 'all')), 65)
        for filter_name in ('unknown', 'pictureTiming', 'bufferingPeriod'):
            self.assertEqual([r['kind'] for r in retained(budget['rows'], 'sei', filter_name)], ['coverage'])
        missing = self.expected['codecSearch']['sei-missing']['rows'][0]
        self.assertFalse(missing['complete'])
        self.assertIsNone(missing['spsSpan'])

    def test_all_pages_validate_and_negative_controls_reject_data_and_cursor_corruption(self):
        case = self.expected['codecSearch']['sei-pagination']
        first = response(case, 'all', 0)
        a = page_rows(first, case, 'all', 0)
        second = response(case, 'all', 1, a)
        b = page_rows(second, case, 'all', 1, a)
        page_rows(response(case, 'all', 2, b), case, 'all', 2, b)
        for mutation in ('duplicate', 'omitted', 'value', 'span', 'cursor', 'request', 'reread', 'position', 'pending', 'exhausted', 'selection'):
            bad = copy.deepcopy(second)
            wire, page = bad['codecSearchPage'], bad['codecSearchPage']['page']
            if mutation == 'duplicate': page['rows'][1] = copy.deepcopy(page['rows'][0])
            elif mutation == 'omitted': page['rows'].pop()
            elif mutation == 'value': page['rows'][0]['values'][0]['value'] = '6'
            elif mutation == 'span': page['rows'][0]['span']['byteOffset'] = '0'
            elif mutation == 'cursor': page['cursor'] = '999'
            elif mutation == 'request': wire['requestId'] = '1'
            elif mutation == 'reread': page['bytesRead'] = '1'
            elif mutation == 'position': page['start']['tagId'] = '2'
            elif mutation == 'pending': page['pending'] = 3
            elif mutation == 'exhausted': page['exhausted'] = True
            else: bad['selection']['codecSearch']['kind'] = 'mp3'
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                page_rows(bad, case, 'all', 1, a)

    def test_existing_output_and_source_mutation_are_rejected(self):
        data = (self.directory / 'sei-pagination.flv').read_bytes()
        with tempfile.TemporaryDirectory() as name:
            directory = Path(name)
            generate(directory)
            (directory / 'sei-pagination.flv').write_bytes(data[:-1])
            with self.assertRaises(RuntimeError):
                load_expectations(directory)

    def test_ui_requires_exact_row_order_source_title_and_frame_actions(self):
        case = self.expected['codecSearch']['sei-pagination']
        wire = response(case, 'all', 0)['codecSearchPage']
        page = wire['page']
        status = '编解码器检索完成：sei · Tag 0 / 帧 0 → Tag 1 / 帧 0 · 检查 2 个 Tag、0 个帧头 · 本批 64 条 · 待显示 66 条 · 读取 600 byte。可继续下一批。'
        ui = [{'role': 'paragraph', 'name': '', 'text': status, 'description': ''}]
        for row in page['rows']:
            source = row['span']
            ui.extend([{'role': 'push button', 'name': f'从编解码器检索跳转 {identity(row)} Tag', 'text': '', 'description': ''},
                       {'role': 'push button', 'name': f'定位编解码器 {identity(row)} 证据', 'text': '',
                        'description': f'{source["byteOffset"]} : 0 · {source["bitLength"]} bit'}])
        page_ui(ui, {'status': status}, wire)
        for mutation in ('duplicate', 'source', 'frame'):
            bad = copy.deepcopy(ui)
            if mutation == 'duplicate': bad[3]['name'] = bad[1]['name']
            elif mutation == 'source': bad[2]['description'] = '0 : 0 · 8 bit'
            else: bad.append({'role': 'push button', 'name': '从编解码器检索跳转 1:-:0:0 帧', 'text': '', 'description': ''})
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                page_ui(bad, {'status': status}, wire)


if __name__ == '__main__':
    unittest.main()
