"""No-GUI false-positive checks for native search and physical history evidence."""

import copy
import unittest

from native_search_history import RELATIONS, table_rows, verify_avc_page, verify_hex_ui, verify_mp3
from native_context_side import native_span
from search_history_oracle import avc_headers, make_flv, mp3_history
from search_history_oracle_test import CONFIG, frame, nal_packet


def field(fields, parent, name, value='', kind='derived', span=None):
    row = {'id': str(len(fields)), 'parentId': parent, 'name': name, 'type': kind, 'value': str(value), 'span': span}
    fields.append(row)
    return row['id']


def mp3_snapshot(want):
    fields = []
    parent = field(fields, None, 'Frame inspection')
    field(fields, parent, 'selectedFrameIndex', want['frameIndex'])
    state = 'unknown' if want['missingBytes'] else 'resolved'
    parent = field(fields, None, 'MP3 dependency sources', state, 'bounded cross-tag dependency')
    values = {'dependencyState': state, 'dependencyReason': 'before_file' if want['missingBytes'] else 'across_tags',
              'targetTagId': want['tagId'], 'targetFrameIndex': want['frameIndex'], 'requestedPreviousBytes': want['backBytes'],
              'resolvedPreviousBytes': want['backBytes'] - want['missingBytes'], 'missingPreviousBytes': want['missingBytes'],
              'part23Bits': want['part23Bits'], 'availableBits': (want['backBytes'] - want['missingBytes']) * 8 + want['mainDataSpan']['bitLength'],
              'sourceCount': len(want['ranges']) + 1, 'scannedTags': 3, 'audioTags': 3, 'framesInspected': 3,
              'coverageBytes': 1251, 'bytesRead': 108}
    for name, value in values.items():
        field(fields, parent, name, value)
    ranges = [{**r, 'current': False} for r in want['ranges']] + [{'tagId': want['tagId'], 'frameIndex': want['frameIndex'], 'span': want['mainDataSpan'], 'current': True}]
    for i, interval in enumerate(ranges):
        root = field(fields, parent, 'Source ' + str(i), '', 'physical main-data source')
        field(fields, root, 'sourceTagId', interval['tagId'])
        field(fields, root, 'sourceFrameIndex', interval['frameIndex'])
        field(fields, root, 'sourceCurrent', str(interval['current']).lower())
        field(fields, root, 'sourceSpan', '', span=native_span(interval['span']))
    return {'selectedTag': {'id': str(want['tagId'])}, 'detail': {'detail': {'kind': 'mp3Audio', 'fields': fields}}}


def avc_ui(oracle):
    status = f'H.264 检索完成：Tag [0, {oracle["indexedTags"]}) · 检查 {oracle["indexedTags"]} 条 · 本批命中 {len(oracle["rows"])} 条 · 读取 20 byte。已到当前索引末尾。'
    ui = [{'depth': 0, 'role': 'statusbar', 'name': '', 'text': status}]
    for row in oracle['rows']:
        span = row['evidenceSpan']
        ui.extend([{'depth': 0, 'role': 'table row', 'name': '', 'text': ''},
                   {'depth': 1, 'role': 'push button', 'name': f'从 H.264 检索跳转 Tag {row["tagId"]}', 'text': str(row['tagId'])},
                   {'depth': 1, 'role': 'table cell', 'name': '', 'text': str(row['dts'])},
                   {'depth': 1, 'role': 'table cell', 'name': '', 'text': RELATIONS[row['declarationRelation']]},
                   {'depth': 2, 'role': 'paragraph', 'name': '', 'text': 'IDR：' + row['observedIDR']},
                   {'depth': 1, 'role': 'push button', 'name': f'定位 H.264 Tag {row["tagId"]} 证据',
                    'description': f'{span["byteOffset"]} : {span["bitOffset"]} · {span["bitLength"]} bit', 'text': ''}])
    return ui, status


class NativeHistoryTests(unittest.TestCase):
    def test_mp3_sources_require_exact_target_order_counts_and_bytes(self):
        source = make_flv([(8, b'\x2f' + frame(back, i + 1), i * 26) for i, back in enumerate((0, 100, 400))])
        want = mp3_history(source)['frames'][2]
        snapshot = mp3_snapshot(want)
        self.assertEqual(verify_mp3(snapshot, want, source)['sources'], 3)
        for mode in ('selection', 'rootstate', 'state', 'missing', 'source', 'sourceTag', 'current', 'count', 'budget', 'window', 'bits'):
            actual, expected = copy.deepcopy(snapshot), copy.deepcopy(want)
            fields = actual['detail']['detail']['fields']
            named = lambda name: next(f for f in fields if f['name'] == name)
            if mode == 'selection': named('selectedFrameIndex')['value'] = '2'
            elif mode == 'rootstate': named('MP3 dependency sources')['value'] = 'unknown'
            elif mode == 'state': named('dependencyState')['value'] = 'unknown'
            elif mode == 'missing': named('missingPreviousBytes')['value'] = '1'
            elif mode == 'source': named('sourceSpan')['span']['byteOffset'] = '0'
            elif mode == 'sourceTag': named('sourceTagId')['value'] = '1'
            elif mode == 'current': named('sourceCurrent')['value'] = 'true'
            elif mode == 'count': named('sourceCount')['value'] = '2'
            elif mode == 'budget': named('bytesRead')['value'] = '1048577'
            elif mode == 'window': expected['windowSHA256'] = 'wrong'
            else: named('availableBits')['value'] = '1'
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                verify_mp3(actual, expected, source)

    def test_unknown_mp3_requires_missing_bytes_and_explanation(self):
        source = make_flv([(8, b'\x2f' + frame(100, 7), 0)])
        want = mp3_history(source)['frames'][0]
        snapshot = mp3_snapshot(want)
        self.assertEqual(verify_mp3(snapshot, want, source)['state'], 'unknown')
        next(f for f in snapshot['detail']['detail']['fields'] if f['name'] == 'dependencyReason')['value'] = ''
        with self.assertRaises(RuntimeError): verify_mp3(snapshot, want, source)

    def test_avc_page_rejects_false_positive_values_positions_and_missing_rows(self):
        source = make_flv([(9, CONFIG, 0), (9, nal_packet([b'\x65\x80'], 2), 40)])
        oracle = avc_headers(source)
        ui, status = avc_ui(oracle)
        self.assertEqual(verify_avc_page(ui, status, oracle, 'mismatches')['rows'], 1)
        for mode in ('idr', 'relation', 'span', 'tag', 'count', 'budget', 'scanned', 'end', 'filter', 'duplicate'):
            actual, text, filter_name = copy.deepcopy(ui), status, 'mismatches'
            if mode == 'idr': actual[5]['text'] = 'IDR：no'
            elif mode == 'relation': actual[4]['text'] = RELATIONS['keyframe_with_idr']
            elif mode == 'span': actual[6]['description'] = '0 : 0 · 4 bit'
            elif mode == 'tag': actual[2]['name'] = '从 H.264 检索跳转 Tag 0'
            elif mode == 'count': text = text.replace('命中 1 条', '命中 0 条')
            elif mode == 'budget': text = text.replace('读取 20 byte', '读取 1048577 byte')
            elif mode == 'scanned': text = text.replace('检查 2 条', '检查 1 条')
            elif mode == 'end': text = text.replace('已到当前索引末尾。', '还有 Tag 未检查，可继续下一批。')
            elif mode == 'filter': filter_name = 'unknown'
            else: actual += actual[1:]
            actual[0]['text'] = text
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                verify_avc_page(actual, text, oracle, filter_name)

    def test_source_hex_click_requires_original_bit_values_and_highlighting(self):
        source = b'\x65'
        span = {'byteOffset': 0, 'bitOffset': 3, 'bitLength': 5}
        ui = [{'name': '', 'text': 'H.264 Tag 1 证据 · 0 : 3 · 5 bit'},
              {'name': 'Hex 实际窗口', 'text': '实际窗口 [0, 1)'}]
        ui += [{'name': f'bit {i}' + (' 范围内' if i >= 3 else ''), 'text': str(0x65 >> (7 - i) & 1)} for i in range(8)]
        verify_hex_ui(ui, span, 'H.264 Tag 1 证据', source)
        for mode in ('window', 'value', 'highlight', 'length'):
            actual = copy.deepcopy(ui)
            if mode == 'window': actual[1]['text'] = '实际窗口 [1, 2)'
            elif mode == 'value': actual[2]['text'] = '1'
            elif mode == 'highlight': actual[2]['name'] = 'bit 0 范围内'
            else: actual.pop()
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                verify_hex_ui(actual, span, 'H.264 Tag 1 证据', source)

    def test_search_buttons_outside_table_are_not_results(self):
        with self.assertRaises(RuntimeError):
            table_rows([{'depth': 0, 'role': 'push button', 'name': '从 H.264 检索跳转 Tag 1', 'text': '1'}])


if __name__ == '__main__':
    unittest.main()
