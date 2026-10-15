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

    def test_source_window_matches_whole_bit_oracle_at_every_alignment(self):
        data = bytes((i * 73 + 19) % 256 for i in range(35))
        whole = ''.join(f'{byte:08b}' for byte in data)
        for base in (0, 100, 1 << 40):
            for start in range(len(whole)):
                for width in (1, 7, 8, 9, 12, 31):
                    if start + width > len(whole):
                        continue
                    encoded = whole[start:start + width]
                    field = {'path': 'independent', 'value': int(encoded, 2), 'encodedBits': encoded,
                             'span': {'byteOffset': base + start // 8, 'bitOffset': start % 8, 'bitLength': width}}
                    oracle.source_field(data, field, base)

    def test_source_window_rejects_negative_aliases_invalid_types_and_truncation(self):
        data = bytes.fromhex('aa55aa55')
        good = {'path': 'edge', 'value': 0x55, 'encodedBits': '01010101',
                'span': {'byteOffset': 103, 'bitOffset': 0, 'bitLength': 8}}
        oracle.source_field(data, good, 100)
        # Python's old whole-bit negative slice could alias valid tail bytes;
        # an out-of-range bitOffset could similarly normalize into another byte.
        for span in ({'byteOffset': 98, 'bitOffset': 0, 'bitLength': 8},
                     {'byteOffset': 101, 'bitOffset': 8, 'bitLength': 8}):
            alias = {'path': 'alias', 'value': 0xaa, 'encodedBits': '10101010', 'span': span}
            start = (span['byteOffset'] - 100) * 8 + span['bitOffset']
            self.assertEqual(''.join(f'{b:08b}' for b in data)[start:start + 8], alias['encodedBits'])
            with self.assertRaises(RuntimeError):
                oracle.source_field(data, alias, 100)
        for key, value in [('byteOffset', 99), ('byteOffset', 104), ('byteOffset', 103.0),
                           ('byteOffset', True), ('bitOffset', -1), ('bitOffset', 8),
                           ('bitOffset', False), ('bitLength', 0), ('bitLength', -8),
                           ('bitLength', 9), ('bitLength', 1 << 40), ('bitLength', '8')]:
            bad = copy.deepcopy(good)
            bad['span'][key] = value
            with self.subTest(key=key, value=value), self.assertRaises(RuntimeError):
                oracle.source_field(data, bad, 100)
        for offset in (-1, True, 100.0):
            with self.subTest(source_offset=offset), self.assertRaises(RuntimeError):
                oracle.source_field(data, good, offset)
        for key, value in [('value', 0x54), ('value', 85.0), ('value', True),
                           ('encodedBits', '01010100'), ('encodedBits', '1010101'), ('encodedBits', '0101010x')]:
            bad = {**good, key: value}
            with self.subTest(key=key, value=value), self.assertRaises(RuntimeError):
                oracle.source_field(data, bad, 100)

    def test_source_window_reads_only_necessary_late_bytes(self):
        class MeasuredSource:
            def __init__(self):
                self.reads = []

            def __len__(self):
                return 2 << 20

            def __getitem__(self, key):
                self.reads.append(key)
                return b'\x01\x80'

            def __iter__(self):
                raise AssertionError('whole-source iteration is forbidden')

        source = MeasuredSource()
        field = {'path': 'late', 'value': 3, 'encodedBits': '11',
                 'span': {'byteOffset': 100 + (1 << 20), 'bitOffset': 7, 'bitLength': 2}}
        oracle.source_field(source, field, 100)
        self.assertEqual(source.reads, [slice(1 << 20, (1 << 20) + 2)])

    def test_benchmark_compares_byte_work_without_latency_threshold(self):
        from source_field_benchmark import compare, main
        result = compare(64, 2)
        self.assertTrue(result['passed'])
        self.assertEqual(result['fieldsPerPass'], 62)
        old, new = (result['algorithms'][key] for key in ('whole_source_reference', 'span_window'))
        self.assertEqual(old['sourceBytesVisited'], 64 * 62 * 2)
        self.assertLess(new['sourceBytesVisited'], old['sourceBytesVisited'])
        self.assertEqual(len(new['seconds']), 2)
        for args in (['--source-bytes', '31'], ['--source-bytes', '2097153'], ['--repetitions', '4']):
            with contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                main(args)

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
