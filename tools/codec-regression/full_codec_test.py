"""Independent oracle mutation tests, without MuxRay binaries or generated output."""

import copy
from fractions import Fraction
import unittest

from full_codec_oracle import Syntax, synthetic_h264
from generate import flv_tags, map_trace_fields
from search_history_oracle import make_flv
from verify_full_codec import compare_full_header, expected_mp3, verify_mp3_summary, verify_slice_views


def frame(raw=b'\xff\xfb\x90\x64'):
    return raw + bytes(413)


class FullCodecOracleTests(unittest.TestCase):
    def test_authored_exp_golomb_and_emulation_mapping(self):
        syntax = Syntax()
        syntax.u('zero', 0, 24)
        syntax.ue('unsigned', 3)
        syntax.se('signed', -2)
        nal = syntax.nal(0x68)
        self.assertIn(b'\x00\x00\x03', nal)
        mapped = map_trace_fields(syntax.fields, nal, 100)
        self.assertEqual(mapped[1]['encodedBits'], '00100')
        self.assertEqual(mapped[2]['encodedBits'], '00101')
        self.assertGreater(len(mapped[0]['sourceBitSegments']), 1)

    def test_all_synthetic_headers_map_back_to_original_bits(self):
        for kind in ('I', 'P', 'B', 'SP', 'SI'):
            for map_type in (None, 0, 1, 2, 3, 4, 5, 6):
                data, oracle = synthetic_h264(kind, map_type, scaling=True)
                self.assertEqual(len(flv_tags(data)), 2)
                self.assertEqual(oracle['sliceType'], kind)
                self.assertTrue(all(block['fields'] for block in oracle['traceBlocks']))
                last = oracle['traceBlocks'][1]['fields'][-1]['name']
                self.assertEqual(last, 'slice_group_change_cycle' if map_type in (3, 4, 5) else 'slice_beta_offset_div2')

    def test_header_comparison_rejects_wrong_value_missing_field_and_wrong_span(self):
        _, oracle = synthetic_h264('P')
        block = oracle['traceBlocks'][1]
        good = {'fields': [{'value': f['value'], 'span': {'rbspBitOffset': f['rbspBitStartIncludingNALHeader'] - 8,
                'bitLength': f['bitLength'], 'sourceSpans': f['sourceBitSegments']}} for f in block['fields']]}
        self.assertEqual(compare_full_header(good, block), len(block['fields']))
        for mutation in ('value', 'missing', 'span', 'invented'):
            bad = copy.deepcopy(good)
            if mutation == 'value':
                bad['fields'][-1]['value'] += 1
            elif mutation == 'missing':
                bad['fields'].pop()
            elif mutation == 'span':
                bad['fields'][-1]['span']['sourceSpans'][0]['bitOffset'] ^= 1
            else:
                invented = copy.deepcopy(bad['fields'][-1])
                invented['span']['rbspBitOffset'] += 100
                bad['fields'].append(invented)
            with self.assertRaises(RuntimeError):
                compare_full_header(bad, block)

    def test_full_frames_exceed_old_detail_budget_and_split_invariant(self):
        frames = [frame()] * 70
        merged = make_flv([(8, b'\x2f' + b''.join(frames), 0)])
        split = make_flv([(8, b'\x2f' + f, i * 26) for i, f in enumerate(frames)])
        a, b = expected_mp3(merged), expected_mp3(split)
        for key in ('completeFrameCount', 'frameBytes', 'sampleCount', 'durationSeconds', 'parameterChangeCount', 'bitrateChangeCount'):
            self.assertEqual(a[key], b[key])
        self.assertEqual(a['completeFrameCount'], 70)
        self.assertEqual(Fraction(**{'numerator': a['durationSeconds']['numerator'], 'denominator': a['durationSeconds']['denominator']}), Fraction(70 * 1152, 44100))

    def test_prefix_and_full_rbsp_reads_are_separate(self):
        def view(length):
            return {'ebspSpan': {'byteOffset': 100, 'bitLength': length * 8}, 'byteLength': length,
                    'sourceSegments': [{'rbspByteLength': length}]}
        prefix = {'rbsp': view(2), 'bytesRead': 2, 'syntaxBits': 14}
        full = {'rbsp': view(5), 'bytesRead': 5, 'rbspBitOffset': 14, 'syntaxBits': 22, 'headerBits': 36}
        verify_slice_views(prefix, full)
        bad = copy.deepcopy(prefix)
        bad['rbsp'] = copy.deepcopy(full['rbsp'])
        with self.assertRaisesRegex(RuntimeError, 'source range'):
            verify_slice_views(bad, full)

    def test_later_header_changes_are_observed(self):
        data = make_flv([(8, b'\x2f' + frame() + frame(b'\xff\xfb\x90\xe4') + frame(), 0)])
        result = expected_mp3(data)
        self.assertEqual(result['parameterChangeCount'], 2)
        self.assertEqual(result['bitrateChangeCount'], 0)
        self.assertEqual([e['frameIndex'] for e in result['events']], [0, 1, 2])

    def test_sample_duration_keeps_exact_mixed_rate_fraction(self):
        lower = bytes.fromhex('fff380c4') + bytes(204)
        data = make_flv([(8, b'\x2f' + frame() + lower + frame(), 0)])
        result = expected_mp3(data)
        duration = result['durationSeconds']
        self.assertEqual(Fraction(duration['numerator'], duration['denominator']), Fraction(2 * 1152, 44100) + Fraction(576, 22050))
        self.assertEqual(result['sampleCount'], 2880)
        self.assertEqual(len(result['rates']), 2)
        self.assertEqual(result['parameterChangeCount'], 2)
        self.assertEqual(result['bitrateChangeCount'], 2)

    def test_malformed_and_partial_tail_are_not_complete_frames(self):
        for tail, counter in [(frame()[:-17], 'truncatedFrameCount'), (b'\xff\xfb\x00\x64', 'freeFormatCount'),
                              (b'\x00\xfb\x90\x64', 'malformedHeaderCount'), (b'\xff\xfb', 'truncatedHeaderCount')]:
            data = make_flv([(8, b'\x2f' + frame() + tail, 0)])
            result = expected_mp3(data)
            self.assertFalse(result['complete'])
            self.assertEqual(result['completeFrameCount'], 1)
            self.assertEqual(result[counter], 1)
            self.assertEqual(result['unresolvedBytes'], len(tail))
            self.assertEqual(result['events'][-1]['sourceSpan']['bitLength'], len(tail) * 8)

    def test_compute_budget_differs_from_retention_limit(self):
        data = make_flv([(8, b'\x2f' + frame() * 5, 0), (8, b'\x2f' + frame(), 26)])
        retained = expected_mp3(data, max_events=2)
        bounded = expected_mp3(data, max_frames=2)
        self.assertEqual(retained['completeFrameCount'], 6)
        self.assertTrue(retained['complete'] and retained['eventsTruncated'])
        self.assertEqual(bounded['completeFrameCount'], 2)
        self.assertEqual(bounded['budgetStoppedTagCount'], 2)
        self.assertFalse(bounded['complete'])
        self.assertEqual(bounded['attemptedHeaderCount'], 2)
        self.assertEqual(bounded['events'][-1]['status'], 'frameBudget')

    def test_mp3_checker_rejects_wrong_duration_count_span_and_status(self):
        data = make_flv([(8, b'\x2f' + frame() * 2, 0)])
        want = expected_mp3(data)
        good = {'schemaVersion': 3, 'summary': {'mp3Frames': want}}
        self.assertEqual(verify_mp3_summary(good, want), 2)
        for mutation in ('duration', 'count', 'span', 'status'):
            bad = copy.deepcopy(good)
            got = bad['summary']['mp3Frames']
            if mutation == 'duration':
                got['durationSeconds']['numerator'] += 1
            elif mutation == 'count':
                got['completeFrameCount'] -= 1
            elif mutation == 'span':
                got['events'][1]['sourceSpan']['byteOffset'] += 1
            else:
                got['events'][1]['status'] = 'freeFormat'
            with self.assertRaises(RuntimeError):
                verify_mp3_summary(bad, want)


if __name__ == '__main__':
    unittest.main()
