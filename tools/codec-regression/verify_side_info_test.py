"""Negative checks prevent the independent CLI gate from accepting false detail."""

import copy
import unittest

import side_info_oracle as oracle
import verify_side_info as verify


def fixture():
    source = bytes(9)
    expected = oracle.parse_side_info(source, 2, 1)
    fields = {f['path']: {'value': f['value'], 'span': f['span']} for f in expected['fields']}
    channel = {'index': 0, 'tableSelect': [fields[f'granules[0].channels[0].table_select[{i}]'] for i in range(3)], 'subblockGain': []}
    for name, path in verify.CHANNEL_FIELDS.items():
        field = fields.get('granules[0].channels[0].' + path)
        if field:
            channel[name] = copy.deepcopy(field)
            if path in verify.BOOLEAN_FIELDS:
                channel[name]['value'] = bool(field['value'])
    actual = {'span': expected['span'], 'declaredBytes': 9, 'bitsConsumed': 72, 'complete': True,
              'mainDataBegin': fields['main_data_begin'], 'privateBits': fields['private_bits'],
              'scfsiChannels': [], 'granules': [{'index': 0, 'channels': [channel]}]}
    return source, expected, actual


class SideInfoVerificationTests(unittest.TestCase):
    def test_missing_or_invented_conditional_field_is_rejected(self):
        source, expected, actual = fixture()
        self.assertGreater(verify.compare_side_info(actual, expected, source), 10)
        for mutation in ('missing', 'invented'):
            changed = copy.deepcopy(actual)
            channel = changed['granules'][0]['channels'][0]
            if mutation == 'missing':
                del channel['bigValues']
            else:
                channel['preflag'] = copy.deepcopy(channel['scalefacScale'])
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                verify.compare_side_info(changed, expected, source)

    def test_value_source_type_and_array_identity_are_checked(self):
        source, expected, actual = fixture()
        for mutation in ('value', 'span', 'type', 'index', 'array'):
            changed = copy.deepcopy(actual)
            channel = changed['granules'][0]['channels'][0]
            if mutation == 'value':
                channel['bigValues']['value'] = 1
            elif mutation == 'span':
                channel['bigValues']['span']['bitOffset'] += 1
            elif mutation == 'type':
                channel['windowSwitchingFlag']['value'] = 0
            elif mutation == 'index':
                channel['index'] = 1
            else:
                channel['tableSelect'].pop()
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                verify.compare_side_info(changed, expected, source)

    def test_original_bytes_are_reread_even_when_json_fields_agree(self):
        source, expected, actual = fixture()
        with self.assertRaises(RuntimeError):
            verify.compare_side_info(actual, expected, b'\xff' + source[1:])

    def test_before_tag_and_broken_history_are_different(self):
        raw = bytes.fromhex('fffb9000') + bytes(413)
        frame = oracle.stream_oracle(raw)['frames'][0]
        dep = frame['dependency']
        dep.update(backBytes=400, declaredDataBits=3000, declaredBitsFit=True)
        got = verify.expected_dependency(frame)
        self.assertEqual((got['state'], got['missingPreviousBytes'], got['availableBits']), ('before_tag', 400, 381 * 8))
        dep['historyOrigin'] = 'invalid_or_incomplete_frame'
        got = verify.expected_dependency(frame)
        self.assertEqual(got['state'], 'unknown')
        self.assertNotIn('missingPreviousBytes', got)
        dep['priorMainDataSlotBytes'] = 400
        self.assertEqual(verify.expected_dependency(frame)['state'], 'within_tag')
        dep['declaredBitsFit'] = False
        self.assertEqual(verify.expected_dependency(frame)['state'], 'unknown')

    def test_bad_frame_has_no_invented_main_data_budget(self):
        raw = bytes.fromhex('fffb9000') + bytes(31)
        frame = oracle.stream_oracle(raw)['frames'][0]
        dependency = verify.expected_dependency(frame)
        self.assertEqual(dependency['state'], 'unknown')
        self.assertNotIn('part23Bits', dependency)
        self.assertNotIn('availableBits', dependency)

    def test_legacy_diagnostic_boundary_stays_frozen(self):
        raw = bytes.fromhex('fffb9000') + bytes(413)
        field = next(f for f in oracle.stream_oracle(raw)['frames'][0]['sideInfo']['fields'] if f['path'].endswith('.big_values'))
        case = oracle.stream_oracle(oracle.patch_field(raw, field, 511))
        self.assertEqual(verify.expected_diagnostics(case, 1), [])
        self.assertEqual(verify.expected_diagnostics(case, 2), [])
        self.assertEqual(verify.expected_diagnostics(case, 3), [('MP3_SIDE_INFO_VALUE_INVALID', field['span']['byteOffset'])])


if __name__ == '__main__':
    unittest.main()
