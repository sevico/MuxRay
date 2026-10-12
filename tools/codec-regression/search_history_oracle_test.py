"""Source-byte tests for cross-Tag slot mapping and AVC header search fixtures."""

import hashlib
from pathlib import Path
import tempfile
import unittest

from generate import load_expectations, save
from search_history_oracle import arguments, avc_headers, make_flv, mp3_history, records


def frame(back, fill):
    result = bytearray(bytes.fromhex('fffb9064') + bytes(32) + bytes([fill]) * 381)
    result[4:6] = (back << 7).to_bytes(2, 'big')
    return bytes(result)


CONFIG = bytes.fromhex('17000000000164001effe100016701000168')


def nal_packet(nals, declaration=2):
    return bytes([(declaration << 4) | 7, 1, 0, 0, 0]) + b''.join(len(n).to_bytes(4, 'big') + n for n in nals)


class SourceOracleTests(unittest.TestCase):
    def test_repacketization_preserves_original_window_order_and_excludes_headers(self):
        raw = [frame(0, 0x11), frame(100, 0x22), frame(400, 0x33)]
        merged = make_flv([(8, b'\x2f' + b''.join(raw), 0)])
        split = make_flv([(8, b'\x2f' + value, i * 26) for i, value in enumerate(raw)])
        left, right = mp3_history(merged)['frames'], mp3_history(split)['frames']
        self.assertEqual(left[2]['state'], 'within_tag')
        self.assertEqual(right[2]['state'], 'across_tags')
        self.assertEqual([r['span']['bitLength'] // 8 for r in right[2]['ranges']], [19, 381])
        self.assertEqual([r['tagId'] for r in right[2]['ranges']], [0, 1])
        self.assertEqual(left[2]['windowSHA256'], right[2]['windowSHA256'])
        self.assertEqual(right[2]['windowSHA256'], hashlib.sha256(b'\x11' * 19 + b'\x22' * 381).hexdigest())
        for data, rows in ((merged, left), (split, right)):
            for row in rows:
                s = row['backpointerSpan']
                raw_bits = int.from_bytes(data[s['byteOffset']:s['byteOffset'] + 2], 'big') >> 7
                self.assertEqual(raw_bits, row['backBytes'])
                for interval in row['ranges']:
                    self.assertLess(interval['span']['byteOffset'], row['frameSpan']['byteOffset'])

    def test_video_interleaving_keeps_history_but_audio_barrier_clears_it(self):
        raw = [frame(0, 0x11), frame(100, 0x22), frame(400, 0x33)]
        sequence = [(8, b'\x2f' + raw[0], 0), (9, CONFIG, 0), (8, b'\x2f' + raw[1], 26),
                    (9, nal_packet([b'\x65\x80'], 1), 30), (8, b'\x2f' + raw[2], 52)]
        original = mp3_history(make_flv(sequence))['frames']
        self.assertEqual([r['tagId'] for r in original[2]['ranges']], [0, 2])
        sequence[1] = (8, b'\xaf\x00\x12\x10', 0)
        broken = mp3_history(make_flv(sequence))['frames']
        self.assertEqual((broken[1]['missingBytes'], broken[2]['missingBytes']), (100, 19))
        self.assertEqual(broken[2]['historyOrigin'], 'other_or_filtered_audio')
        self.assertEqual(broken[2]['state'], 'incomplete')

    def test_missing_prefix_does_not_invent_history(self):
        row = mp3_history(make_flv([(8, b'\x2f' + frame(400, 0x33), 0)]))['frames'][0]
        self.assertEqual((row['state'], row['missingBytes'], row['ranges']), ('incomplete', 400, []))

    def test_damaged_frame_resets_cross_tag_history(self):
        # The second frame is container-complete but ends inside its MPEG side info.
        sequence = [(8, b'\x2f' + frame(0, 0x11), 0), (8, b'\x2f' + frame(0, 0x22)[:20], 26),
                    (8, b'\x2f' + frame(100, 0x33), 52)]
        rows = mp3_history(make_flv(sequence))['frames']
        self.assertEqual(rows[1]['state'], 'unknown')
        self.assertEqual((rows[2]['state'], rows[2]['missingBytes']), ('incomplete', 100))
        self.assertEqual(rows[2]['historyOrigin'], 'invalid_or_incomplete_frame')

    def test_i_slice_header_is_not_idr_and_declared_keyframe_is_not_evidence(self):
        data = make_flv([(9, CONFIG, 0), (9, nal_packet([b'\x41\xb8'], 1), 40),
                         (9, nal_packet([b'\x65\x80'], 2), 80)])
        result = avc_headers(data)
        self.assertEqual(result['mismatchTagIds'], [1, 2])
        self.assertEqual([r['observedIDR'] for r in result['rows']], ['no', 'yes'])
        self.assertEqual([r['declarationRelation'] for r in result['rows']], ['keyframe_without_idr', 'inter_with_idr'])
        for row in result['rows']:
            s = row['evidenceSpan']
            self.assertEqual((s['bitOffset'], s['bitLength']), (3, 5))
            self.assertEqual(data[s['byteOffset']] & 31, 5 if row['observedIDR'] == 'yes' else 1)

    def test_unusable_headers_are_unknown_not_negative_and_positive_survives_partial(self):
        for bad in (b'\x1f', b'\x05', b'\xe5', b'\x74'):
            with self.subTest(nal=bad.hex()):
                data = make_flv([(9, CONFIG, 0), (9, nal_packet([bad]), 40),
                                 (9, nal_packet([b'\x65\x80', bad], 1), 80)])
                rows = avc_headers(data)['rows']
                self.assertEqual([r['observedIDR'] for r in rows], ['unknown', 'yes'])
                self.assertFalse(any(r['coverageComplete'] for r in rows))

    def test_missing_config_eos_and_truncation_do_not_guess_length_width(self):
        packet = nal_packet([b'\x41\x80'])
        sequence = [(9, packet, 0), (9, CONFIG, 1), (9, packet, 40),
                    (9, bytes.fromhex('1702000000'), 80), (9, packet, 120),
                    (9, CONFIG, 121), (9, packet[:5] + b'\x00\x00\x00\xff\x41', 160)]
        rows = avc_headers(make_flv(sequence))['rows']
        self.assertEqual([r['observedIDR'] for r in rows], ['unknown', 'no', 'unknown', 'unknown'])
        self.assertEqual(rows[-1]['reason'], 'invalid_nal_boundary')

    def test_flv_timestamp_extension_roundtrip_and_bad_envelope_rejected(self):
        sequence = [(8, b'\x2f' + frame(0, 0), (1 << 24) + 9)]
        data = make_flv(sequence)
        self.assertEqual(records(data), sequence)
        damaged = bytearray(data)
        damaged[-1] ^= 1
        with self.assertRaises(RuntimeError):
            mp3_history(bytes(damaged))

    def test_existing_output_directory_and_changed_identity_are_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            with self.assertRaises(SystemExit):
                arguments(['--output-dir', str(path)])
            media = make_flv([(9, CONFIG, 0)])
            (path / 'test.flv').write_bytes(media)
            expected = {'schemaVersion': 1, 'sources': {'test.flv': {'bytes': len(media), 'sha256': hashlib.sha256(media).hexdigest()}}}
            save(path, 'expectations.json', expected)
            save(path, 'result.json', {'passed': True, 'expectationsSHA256': hashlib.sha256((path / 'expectations.json').read_bytes()).hexdigest()})
            self.assertEqual(load_expectations(path), expected)
            (path / 'test.flv').write_bytes(media[:-1] + b'\xff')
            with self.assertRaises(RuntimeError):
                load_expectations(path)


if __name__ == '__main__':
    unittest.main()
