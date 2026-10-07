"""Independent layout, original-bit and missing-history checks; no FFmpeg needed."""

import contextlib
import copy
import io
from pathlib import Path
import sys
import tempfile
import unittest

import side_info_oracle as oracle


def set_bits(data, bit, width, value):
    result = int.from_bytes(data, 'big')
    shift = len(data) * 8 - bit - width
    mask = ((1 << width) - 1) << shift
    return ((result & ~mask) | (value << shift)).to_bytes(len(data), 'big')


class SideInfoOracleTests(unittest.TestCase):
    def test_versions_channels_and_granule_layout(self):
        for version, channels, size, first_part_bit, granules in (
            (3, 1, 17, 18, 2), (3, 2, 32, 20, 2),
            (2, 1, 9, 9, 1), (2, 2, 17, 10, 1),
            (0, 1, 9, 9, 1), (0, 2, 17, 10, 1),
        ):
            with self.subTest(version=version, channels=channels):
                result = oracle.parse_side_info(bytes(size), version, channels, 100)
                self.assertTrue(result['syntaxComplete'] and result['valid'])
                self.assertEqual(result['bitsConsumed'], size * 8)
                self.assertEqual(len(result['granules']), granules)
                self.assertEqual([len(row) for row in result['granules']], [channels] * granules)
                fields = {f['path']: f for f in result['fields']}
                first = fields['granules[0].channels[0].part2_3_length']
                self.assertEqual(first['bitOffsetInSideInfo'], first_part_bit)
                self.assertEqual(first['span'], {'byteOffset': 100 + first_part_bit // 8,
                                                'bitOffset': first_part_bit % 8, 'bitLength': 12})
                self.assertEqual('granules[0].channels[0].preflag' in fields, version == 3)
                self.assertEqual('scfsi[0][0]' in fields, version == 3)

    def test_short_block_branch_has_exact_encoded_fields(self):
        data = set_bits(bytes(32), 53, 1, 1)
        data = set_bits(data, 54, 2, 2)
        data = set_bits(data, 56, 1, 1)
        result = oracle.parse_side_info(data, 3, 2, 29)
        first = result['granules'][0][0]
        self.assertTrue(result['valid'])
        self.assertEqual((first['window_switching_flag'], first['block_type'], first['mixed_block_flag']), (1, 2, 1))
        self.assertEqual((len(first['table_select']), len(first['subblock_gain'])), (2, 3))
        self.assertNotIn('region0_count', first)
        self.assertEqual(''.join(f['encodedBits'] for f in result['fields']), ''.join(f'{b:08b}' for b in data))
        for field in result['fields']:
            oracle.source_field(data, field, 29)

    def test_invalid_values_do_not_hide_field_positions(self):
        # MPEG-1 stereo first big_values begins at side-info bit 32.
        big = oracle.parse_side_info(set_bits(bytes(32), 32, 9, 511), 3, 2)
        self.assertTrue(big['syntaxComplete'])
        self.assertFalse(big['valid'])
        self.assertIn('big_values_out_of_range', [i['kind'] for i in big['issues']])
        block = oracle.parse_side_info(set_bits(bytes(32), 53, 1, 1), 3, 2)
        self.assertIn('switched_block_type_zero', [i['kind'] for i in block['issues']])
        table = oracle.parse_side_info(set_bits(bytes(32), 54, 5, 14), 3, 2)
        self.assertIn('reserved_huffman_table', [i['kind'] for i in table['issues']])

    def test_truncated_side_info_retains_only_available_fields(self):
        for size in range(17):
            result = oracle.parse_side_info(bytes(size), 3, 1, 50)
            self.assertFalse(result['syntaxComplete'])
            self.assertFalse(result['valid'])
            self.assertLessEqual(result['bitsConsumed'], size * 8)
            self.assertIn('missingField', result)
            for field in result['fields']:
                self.assertLessEqual((field['span']['byteOffset'] - 50) * 8 + field['span']['bitOffset'] + field['span']['bitLength'], size * 8)
            self.assertNotIn('part23TotalBits', result)

    def test_dependency_counts_main_data_slots_without_headers(self):
        # MPEG-1 stereo, 417-byte frames: 381 source slots after header/side-info.
        first = bytes.fromhex('fffb9000') + bytes(413)
        second = bytearray(first)
        second[4:36] = set_bits(bytes(32), 0, 9, 400)
        frames = oracle.stream_oracle(first + second)['frames']
        self.assertEqual(frames[0]['mainDataArea']['declaredBytes'], 381)
        self.assertEqual(frames[1]['dependency']['priorMainDataSlotBytes'], 381)
        self.assertEqual(frames[1]['dependency']['missingHistoryBytes'], 19)
        self.assertEqual(frames[1]['dependency']['status'], 'history_unknown')
        self.assertTrue(frames[1]['sideInfo']['valid'])
        # The same backpointer is completely covered after two preceding frames.
        covered = oracle.stream_oracle(first + first + second)['frames'][2]
        self.assertEqual(covered['dependency']['status'], 'within_tag')

    def test_invalid_prior_side_info_breaks_known_history(self):
        frame = bytearray(bytes.fromhex('fffb9000') + bytes(413))
        bad = bytearray(frame)
        bad[4:36] = set_bits(bytes(32), 32, 9, 511)
        frame[4:36] = set_bits(bytes(32), 0, 9, 1)
        final = oracle.stream_oracle(bad + frame)['frames'][1]
        self.assertEqual(final['dependency']['historyOrigin'], 'invalid_or_incomplete_frame')
        self.assertEqual(final['dependency']['priorMainDataSlotBytes'], 0)
        self.assertEqual(final['dependency']['status'], 'history_unknown')

    def test_original_source_verification_rejects_shift_and_wrong_value(self):
        data = bytes.fromhex('000080000000000000')
        field = oracle.parse_side_info(data, 2, 1, 100)['fields'][2]
        oracle.source_field(data, field, 100)
        for mutation in ('value', 'position'):
            bad = copy.deepcopy(field)
            if mutation == 'value':
                bad['value'] += 1
            else:
                bad['span']['byteOffset'] += 1
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                oracle.source_field(data, bad, 100)

    def test_mutation_preserves_every_bit_outside_target(self):
        data = bytes(32)
        field = oracle.parse_side_info(data, 3, 2, 25)['fields'][11]
        changed = oracle.patch_field(data, field, (1 << field['span']['bitLength']) - 1)
        xor = int.from_bytes(data, 'big') ^ int.from_bytes(changed, 'big')
        shift = len(data) * 8 - field['bitOffsetInSideInfo'] - field['span']['bitLength']
        self.assertEqual(xor, ((1 << field['span']['bitLength']) - 1) << shift)
        with self.assertRaises(RuntimeError):
            oracle.patch_field(data, field, 1 << field['span']['bitLength'])

    def test_generator_rejects_existing_output_before_writing(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary)
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                oracle.arguments(['--output-dir', str(path), '--ffmpeg', sys.executable, '--ffprobe', sys.executable])
            self.assertEqual(list(path.iterdir()), [])


if __name__ == '__main__':
    unittest.main()
