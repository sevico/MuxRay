"""Independent paging/SEI oracle and checker negative controls; no GUI needed."""

import copy
import json
from pathlib import Path
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

import native_paged_codec as native
from paged_codec_oracle import extend_255, generate
from verify_paged_codec import expected_span, verify_legacy_sei, verify_mp3_native_frames, verify_sei_unit
from verify_paged_codec import check_native_mapped


def sei_result(want):
    """A correct witness for deliberately corrupting individual checker inputs."""
    result = {'bytesRead': want['nalBytes'] - 1, 'rbsp': {'ebspSpan': {'bitLength': (want['nalBytes'] - 1) * 8}},
              'complete': want['complete'], 'framingComplete': want.get('framingComplete', False), 'messages': []}
    for expected in want['messages']:
        actual = {'index': expected['index'], 'complete': expected['complete']}
        for name in ('payloadType', 'payloadSize'):
            if name in expected:
                actual[name] = {'value': expected[name]['value'], 'span': expected_span(expected[name])}
        if 'payloadSpan' in expected:
            actual.update(kind=expected['kind'], payloadSpan=expected_span(expected['payloadSpan']),
                          payloadComplete=expected['payloadComplete'], syntaxSupported=expected['kind'] != 'unknown')
        if 'recoveryFields' in expected:
            actual['recoveryPoint'] = {field['name']: {'value': field['value'], 'span': expected_span(field)}
                                       for field in expected['recoveryFields']}
            actual['recoveryPoint']['payloadAlignmentBits'] = expected_span(expected['alignment'])
        if 'uuid' in expected:
            actual['userDataUnregistered'] = {field: {'hex': expected[field]['hex'], 'span': expected_span(expected[field]['span'])}
                                              for field in ('uuid', 'dataPreview')}
            actual['userDataUnregistered'].update(dataBytes=expected['dataBytes'], previewTruncated=expected['previewTruncated'])
        result['messages'].append(actual)
    return result


class PagedCodecTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        cls.result = generate(cls.root)
        cls.expected = json.loads((cls.root / 'expectations.json').read_text())

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_late_frames_cross_old_frame_and_byte_boundaries(self):
        case = self.expected['mp3Paging']['long-tag']
        self.assertEqual(case['completeFrames'], 3000)
        rows = case['frames']
        self.assertEqual([rows[i]['frameIndex'] for i in (63, 64, 255, 2600)], [63, 64, 255, 2600])
        # Only frame 150 adds 63 bytes; mono side info does not change frame length.
        self.assertEqual(rows[2600]['offset'], 25 + 2600 * 417 + 63)
        self.assertGreater(rows[2600]['offset'], 1 << 20)
        self.assertEqual(rows[150]['sampleRateHz'], 48000)
        self.assertEqual(rows[270]['channels'], 1)
        self.assertEqual(rows[255]['missingBytes'], 0)
        self.assertEqual([(r['frameIndex'], r['span']['bitLength'] // 8) for r in rows[64]['ranges']], [(62, 130), (63, 381)])

    def test_cross_tag_dependency_and_barrier_are_distinct(self):
        cross = next(row for row in self.expected['mp3Paging']['cross-tag']['frames'] if row['tagId'] == 2 and row['frameIndex'] == 0)
        self.assertEqual([(r['tagId'], r['frameIndex']) for r in cross['ranges']], [(0, 62), (0, 63)])
        self.assertEqual(cross['missingBytes'], 0)
        barrier = next(row for row in self.expected['mp3Paging']['audio-barrier']['frames'] if row['tagId'] == 3 and row['frameIndex'] == 0)
        self.assertEqual(barrier['missingBytes'], 511)
        self.assertEqual(barrier['ranges'], [])

    def test_parameter_change_does_not_reuse_old_physical_history(self):
        rows = self.expected['mp3Paging']['long-tag']['frames']
        for index in (150, 151, 270, 271):
            self.assertEqual(rows[index]['missingBytes'], 511)
        self.assertEqual(rows[273]['missingBytes'], 0)

    def test_partial_late_frames_are_not_complete(self):
        truncated = self.expected['mp3Paging']['truncated-late']
        free = self.expected['mp3Paging']['free-format-late']
        self.assertEqual(truncated['completeFrames'], 130)
        self.assertEqual(truncated['stops'][0]['reason'], 'body_truncated')
        self.assertEqual(free['completeFrames'], 128)
        self.assertEqual(free['stops'][0]['reason'], 'free_format')
        self.assertIsNone(free['frames'][-1]['frameLength'])

    def test_sei_extended_lengths_and_epb_spans_match_original_bytes(self):
        case = self.expected['sei']['mixed']
        source = (self.root / case['file']).read_bytes()
        messages = case['nals'][0]['messages']
        self.assertEqual([m['payloadType']['value'] for m in messages], [6, 5, 511])
        self.assertGreater(messages[1]['payloadSize']['value'], 255)
        self.assertEqual(messages[2]['payloadType']['encodedBits'], '111111111111111100000001')
        self.assertGreater(len(messages[1]['uuid']['span']['sourceBitSegments']), 1)
        for message in messages:
            for field in [message['payloadType'], message['payloadSize'], message['payloadSpan']]:
                actual = ''
                for s in field['sourceBitSegments']:
                    bits = ''.join(f'{value:08b}' for value in source[s['byteOffset']:s['byteOffset'] + (s['bitOffset'] + s['bitLength'] + 7) // 8])
                    actual += bits[s['bitOffset']:s['bitOffset'] + s['bitLength']]
                self.assertEqual(actual, field['encodedBits'])
        self.assertEqual(len(messages[1]['dataPreview']['hex']), 128)
        self.assertTrue(messages[1]['previewTruncated'])

    def test_zero_unknown_payload_and_recovery_alignment(self):
        messages = self.expected['sei']['empty-unknown']['nals'][0]['messages']
        self.assertEqual(messages[0]['payloadSpan']['bitLength'], 0)
        self.assertEqual(messages[1]['recoveryFields'][0]['value'], 0)
        self.assertEqual(messages[1]['alignment']['encodedBits'], '100')
        self.assertEqual(extend_255(255), b'\xff\0')
        self.assertEqual(extend_255(510), b'\xff\xff\0')

    def test_sei_truncation_is_not_unknown_success(self):
        for name in ('truncated-type', 'truncated-size', 'truncated-payload', 'short-uuid', 'short-recovery'):
            with self.subTest(name=name):
                want = self.expected['sei'][name]['nals'][0]
                self.assertFalse(want['complete'])
                self.assertFalse(want['messages'][-1]['complete'])
        self.assertEqual(len(self.expected['sei']['message-budget']['nals'][0]['messages']), 65)
        self.assertGreater(self.expected['sei']['byte-budget']['nals'][0]['nalBytes'], 65536)
        self.assertGreater(sum(n['nalBytes'] for n in self.expected['sei']['tag-byte-budget']['nals']), 262144)

    def test_sei_checker_rejects_wrong_fields_ranges_preview_and_omissions(self):
        want = self.expected['sei']['mixed']['nals'][0]
        correct = sei_result(want)
        self.assertGreater(verify_sei_unit(correct, want), 5)
        mutations = [
            lambda x: x['messages'][0]['payloadType'].__setitem__('value', 5),
            lambda x: x['messages'][0]['recoveryPoint']['recoveryFrameCnt'].__setitem__('value', 4),
            lambda x: x['messages'][1]['userDataUnregistered']['uuid']['span']['sourceSpans'][0].__setitem__('byteOffset', 0),
            lambda x: x['messages'][1]['userDataUnregistered']['dataPreview'].__setitem__('hex', '00'),
            lambda x: x['messages'][1]['userDataUnregistered'].__setitem__('previewTruncated', False),
            lambda x: x['messages'].pop(),
            lambda x: x['messages'].append(copy.deepcopy(x['messages'][0])),
            lambda x: x.__setitem__('bytesRead', 65537),
        ]
        for mutate in mutations:
            changed = copy.deepcopy(correct)
            mutate(changed)
            with self.assertRaises(RuntimeError):
                verify_sei_unit(changed, want)

    def test_mp3_page_checker_rejects_reindexing_and_source_aliases(self):
        case = self.expected['mp3Paging']['long-tag']
        rows = case['frames'][192:256]
        fields = [{'id': '0', 'parentId': '', 'name': 'Frame inspection', 'type': 'bounded sequence'},
                  {'id': '1', 'parentId': '0', 'name': 'selectedFrameIndex', 'type': 'derived selection', 'value': '255'}]
        for i, row in enumerate(rows):
            field_id = str(2 + 2 * i)
            fields.extend([{'id': field_id, 'parentId': '0', 'name': 'Frame ' + str(row['frameIndex']),
                            'type': 'MPEG frame', 'span': {**row['span'], 'byteOffset': str(row['span']['byteOffset'])}},
                           {'id': str(3 + 2 * i), 'parentId': field_id, 'name': 'header', 'value': '0x' + row['headerHex']}])
        snapshot = {'detail': {'state': 'ready', 'error': None, 'detail': {'kind': 'mp3Audio', 'fields': fields}},
                    'selectedTag': {'id': '0'}}
        self.assertEqual(verify_mp3_native_frames(snapshot, case, 0, 192, 255), 64)
        for position, name, value in [(2, 'name', 'Frame 0'), (2, 'span', fields[4]['span']), (1, 'value', '63')]:
            changed = copy.deepcopy(snapshot)
            changed['detail']['detail']['fields'][position][name] = value
            with self.assertRaises(RuntimeError):
                verify_mp3_native_frames(changed, case, 0, 192, 255)

    def test_native_split_mapping_rejects_including_prevention_bytes(self):
        expected = self.expected['sei']['mixed']['nals'][0]['messages'][1]['uuid']['span']
        field = {'id': 'uuid', 'name': 'uuid', 'span': None}
        children = [{'id': str(i), 'parentId': 'uuid', 'name': f'uuid.source[{i}]',
                     'span': {**s, 'byteOffset': str(s['byteOffset'])}} for i, s in enumerate(expected['sourceBitSegments'])]
        detail = {'fields': children}
        check_native_mapped(detail, field, expected, 'uuid')
        wrong = copy.deepcopy(field)
        first, last = expected['sourceBitSegments'][0], expected['sourceBitSegments'][-1]
        wrong['span'] = {'byteOffset': str(first['byteOffset']), 'bitOffset': 0,
                         'bitLength': (last['byteOffset'] - first['byteOffset']) * 8 + last['bitLength']}
        with self.assertRaises(RuntimeError):
            check_native_mapped(detail, wrong, expected, 'uuid')
        wrong_detail = copy.deepcopy(detail)
        wrong_detail['fields'][1]['span']['byteOffset'] = str(int(children[1]['span']['byteOffset']) - 1)
        with self.assertRaises(RuntimeError):
            check_native_mapped(wrong_detail, field, expected, 'uuid')

    def test_native_rounds_keep_original_page_coordinates(self):
        with tempfile.TemporaryDirectory() as output:
            commands = []
            args = SimpleNamespace(input_dir=self.root, binary=self.root / 'fixed-binary', output_dir=Path(output),
                                   case=None, timeout_seconds=600)
            def record(name, argv, **kwargs):
                commands.append((name, [str(value) for value in argv]))
            with patch.object(native, 'load_expectations', return_value=self.expected), \
                 patch.object(native, 'digest', return_value='fixed-sha'), \
                 patch.object(native, 'verify_case', return_value={'binarySHA256': 'fixed-sha'}):
                result = native.run(args, SimpleNamespace(run=record))
            self.assertTrue(result['passed'])
            self.assertEqual([name for name, _ in commands], list(native.CASES))
            by_name = dict(commands)
            long = by_name['mp3-long-page']
            self.assertEqual(long[long.index('--mp3-page-steps') + 1], '37')
            self.assertEqual(long[long.index('--mp3-frame') + 1], '2600')
            self.assertIn('--mp3-summary-link', long)
            cross = by_name['mp3-cross-page']
            self.assertEqual(cross[cross.index('--tag') + 1], '2')
            self.assertEqual(cross[cross.index('--mp3-page-steps') + 1], '1')
            self.assertEqual(cross[cross.index('--mp3-frame') + 1], '64')
            self.assertEqual(by_name['sei-uuid-source'][-2:], ['--field-name', 'uuid.source[1]'])

    def test_legacy_checker_rejects_new_semantics_even_in_nested_nals(self):
        document = {'schemaVersion': 4, 'detail': {'complete': True, 'nalUnitSequence': {'nalUnits': [{'header': {'name': 'sei'}}]}}}
        verify_legacy_sei(document, 4)
        leaked = copy.deepcopy(document)
        leaked['detail']['nalUnitSequence']['nalUnits'][0]['sei'] = {'complete': True}
        with self.assertRaises(RuntimeError):
            verify_legacy_sei(leaked, 4)
        changed = copy.deepcopy(document)
        changed['detail']['complete'] = False
        with self.assertRaises(RuntimeError):
            verify_legacy_sei(changed, 4)


if __name__ == '__main__':
    unittest.main()
