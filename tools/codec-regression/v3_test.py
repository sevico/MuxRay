"""Oracle mutations must fail even when the remaining fields are correct."""
import copy
import unittest

from generate import flv_tags
from verify_v3 import SLICE_PREFIX, compare_fields, make_flv


class SliceOracleTests(unittest.TestCase):
    def setUp(self):
        self.block = {'fields': []}
        self.actual = {}
        for trace, name, value in [('frame_num', 'frameNum', 2), ('pic_order_cnt_lsb', 'picOrderCntLsb', 4)]:
            self.block['fields'].append({'name': trace, 'value': value, 'rbspBitStartIncludingNALHeader': 16,
                                         'bitLength': 4, 'sourceBitSegments': [{'byteOffset': 20, 'bitOffset': 0, 'bitLength': 4}]})
            self.actual[name] = {'value': value, 'span': {'rbspBitOffset': 8, 'bitLength': 4,
                                                       'sourceSpans': [{'byteOffset': 20, 'bitOffset': 0, 'bitLength': 4}]}}

    def test_rejects_missing_conditional_field(self):
        self.assertEqual(compare_fields(self.actual, self.block, SLICE_PREFIX), 2)
        del self.actual['picOrderCntLsb']
        with self.assertRaisesRegex(RuntimeError, 'missing or invented'):
            compare_fields(self.actual, self.block, SLICE_PREFIX)

    def test_rejects_correct_value_at_wrong_source(self):
        changed = copy.deepcopy(self.actual)
        changed['frameNum']['span']['sourceSpans'][0]['bitOffset'] = 1
        with self.assertRaisesRegex(RuntimeError, 'source bits'):
            compare_fields(changed, self.block, SLICE_PREFIX)

    def test_mutated_container_preserves_framing(self):
        payloads = [b'\x17\0\0\0\0\x01\x64', b'\x27\x01\0\0\0\0\0\0\x01\x65']
        data = make_flv(payloads)
        tags = flv_tags(data)
        self.assertEqual(len(tags), 2)
        self.assertEqual([data[t['payloadOffset']:t['payloadEnd']] for t in tags], payloads)


if __name__ == '__main__':
    unittest.main()
