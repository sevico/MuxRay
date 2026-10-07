"""No-encoder/no-GUI checks for regression input, oracle and failure boundaries."""

import contextlib
import copy
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest

import generate
import native
import native_v3
import verify


class RegressionTest(unittest.TestCase):
    def setUp(self):
        directory = tempfile.TemporaryDirectory()
        self.addCleanup(directory.cleanup)
        self.root = Path(directory.name)

    def test_output_must_be_new_and_executables_must_exist(self):
        base = ['--output-dir', str(self.root / 'new'), '--ffmpeg', sys.executable, '--ffprobe', sys.executable]
        self.assertEqual(generate.arguments(base).output_dir, self.root / 'new')
        for extra in (['--output-dir', str(self.root)], ['--ffmpeg', str(self.root / 'missing')]):
            with self.subTest(extra=extra), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                generate.arguments(base + extra)
        self.assertFalse((self.root / 'new').exists())

    def test_native_arguments_fail_before_start(self):
        (self.root / 'expectations.json').write_text('{}')
        base = ['--input-dir', str(self.root), '--binary', sys.executable, '--output-dir', str(self.root / 'native')]
        self.assertEqual(native.arguments(base).timeout_seconds, 180)
        for extra in (['--timeout-seconds', '29'], ['--timeout-seconds', '601'], ['--input-dir', str(self.root / 'missing')]):
            with self.subTest(extra=extra), contextlib.redirect_stderr(io.StringIO()), self.assertRaises(SystemExit):
                native.arguments(base + extra)
        self.assertFalse((self.root / 'native').exists())

    def test_flv_wrapping_and_damage(self):
        data = generate.wrap_mp3(bytes.fromhex('fffb9064'))
        tag = generate.flv_tags(data)[0]
        self.assertEqual((tag['tagId'], tag['payloadOffset'], tag['payloadEnd']), (0, 24, 29))
        for mutation in (data[:-1], data[:8], data[:-4] + b'\0' * 4, data[:21] + b'\1' + data[22:]):
            with self.subTest(mutation=mutation.hex()), self.assertRaises(RuntimeError):
                generate.flv_tags(mutation)

    def test_crc_independent_vector_and_extent(self):
        data = generate.structural_crc()
        expected = generate.mp3_frames(data)
        self.assertEqual(expected['completeFrameCount'], 2)
        self.assertEqual([f['crc']['status'] for f in expected['frames']], ['match', 'mismatch'])
        self.assertEqual([f['crc']['computed'] for f in expected['frames']], [0x576f, 0x576f])
        tail = generate.mp3_frames(data[:-17])
        self.assertEqual(tail['completeFrameCount'], 1)
        self.assertEqual(tail['frames'][1]['length'] - tail['frames'][1]['availableLength'], 17)
        damaged = bytearray(data)
        damaged[0] = 0
        with self.assertRaises(RuntimeError):
            generate.mp3_frames(damaged)

    def test_xing_offset_does_not_shift_for_protection_bit(self):
        frame = bytearray(417)
        frame[:4] = bytes.fromhex('fffa9064')
        frame[36:40] = b'Xing'
        frame[40:44] = (3).to_bytes(4, 'big')
        frame[44:48] = (7).to_bytes(4, 'big')
        frame[48:52] = (999).to_bytes(4, 'big')
        metadata = generate.mp3_frames(frame)['vbr']
        self.assertEqual((metadata['offset'], metadata['declaredFrames']['value'], metadata['declaredBytes']['value']), (61, 7, 999))

    def test_trace_preserves_emulation_prevention_gap_and_rejects_wrong_bits(self):
        fields = [{'name': 'field', 'bit': 16, 'bits': '0000000000000001', 'value': 1}]
        mapped = generate.map_trace_fields(fields, bytes.fromhex('6700000301'), 100)[0]
        self.assertEqual(mapped['sourceBitSegments'], [{'byteOffset': 102, 'bitOffset': 0, 'bitLength': 8},
                                                     {'byteOffset': 104, 'bitOffset': 0, 'bitLength': 8}])
        fields[0]['bits'] = '1' * 16
        with self.assertRaises(RuntimeError):
            generate.map_trace_fields(fields, bytes.fromhex('6700000301'), 100)

    def test_trace_block_boundaries_do_not_absorb_sei(self):
        trace = ('[trace_headers @ 0x1] Sequence Parameter Set\n'
                 '[trace_headers @ 0x1] 0 forbidden_zero_bit 0 = 0\n'
                 '[trace_headers @ 0x1] 3 nal_unit_type 00111 = 7\n'
                 '[trace_headers @ 0x1] Supplemental Enhancement Information\n'
                 '[trace_headers @ 0x1] 0 forbidden_zero_bit 0 = 0\n'
                 '[trace_headers @ 0x1] 3 nal_unit_type 00110 = 6\n'
                 '[trace_headers @ 0x1] Slice Header\n'
                 '[trace_headers @ 0x1] 0 forbidden_zero_bit 0 = 0\n')
        blocks = generate.trace_blocks(trace)
        self.assertEqual([len(b['fields']) for b in blocks], [2, 1])

    def test_command_failure_keeps_logs_and_result(self):
        commands = generate.Commands(self.root)
        with self.assertRaises(RuntimeError):
            commands.run('failure', [sys.executable, '-c', 'import sys; print("evidence"); sys.exit(3)'])
        record = json.loads((self.root / 'commands.json').read_text())[0]
        self.assertEqual(record['exitCode'], 3)
        self.assertEqual((self.root / record['stdout']).read_text().strip(), 'evidence')

    def test_changed_sample_cannot_reuse_expectations(self):
        sample = self.root / 'sample.flv'
        sample.write_bytes(b'original')
        generate.save(self.root, 'expectations.json', {'schemaVersion': 1, 'sources': {sample.name: {'bytes': 8, 'sha256': generate.digest(sample)}}})
        generate.save(self.root, 'result.json', {'passed': True, 'expectationsSHA256': generate.digest(self.root / 'expectations.json')})
        self.assertTrue(generate.load_expectations(self.root))
        sample.write_bytes(b'modified')
        with self.assertRaisesRegex(RuntimeError, 'source changed'):
            generate.load_expectations(self.root)
        sample.write_bytes(b'original')
        (self.root / 'expectations.json').write_text('{}')
        with self.assertRaisesRegex(RuntimeError, 'expectations changed'):
            generate.load_expectations(self.root)

    def test_vui_oracle_rejects_wrong_source_mapping(self):
        field = {'value': 50, 'span': {'rbspBitOffset': 12, 'bitLength': 8,
                                      'sourceSpans': [{'byteOffset': 40, 'bitOffset': 4, 'bitLength': 8}]}}
        sps = {'vuiParametersPresentFlag': {'value': True, 'span': {}},
               'vui': {'timeScale': field, 'complete': True, 'trailingBitsValid': True, 'remainingRbspBits': 0}}
        document = {'detail': {'complete': True, 'avcDecoderConfigurationRecord': {'sequenceParameterSets': [{'sequenceParameterSet': sps}]}}, 'diagnostics': []}
        expected = {'vuiFieldsFromFFmpegTrace': {'time_scale': {'value': 50, 'rbspBitStartIncludingNALHeader': 20, 'bitLength': 8,
                                                            'sourceBitSegments': field['span']['sourceSpans']}}}
        verify.verify_h264(document, expected)
        damaged = copy.deepcopy(document)
        damaged['detail']['avcDecoderConfigurationRecord']['sequenceParameterSets'][0]['sequenceParameterSet']['vui']['timeScale']['span']['sourceSpans'][0]['byteOffset'] += 1
        with self.assertRaisesRegex(RuntimeError, 'source bit mapping'):
            verify.verify_h264(damaged, expected)

    def test_native_vui_requires_matching_value_and_unique_field(self):
        fields = [{'name': 'timeScale', 'value': '50'}, {'name': 'vuiComplete', 'value': 'true'},
                  {'name': 'spsTrailingBitsValid', 'value': 'true'}]
        snapshot = {'detail': {'detail': {'complete': True, 'truncated': False, 'fields': fields}}}
        expected = {'h264': {'vuiFieldsFromFFmpegTrace': {'time_scale': {'value': 50}}}}
        native.verify_semantics('h264', snapshot, expected)
        fields[0]['value'] = '51'
        with self.assertRaisesRegex(RuntimeError, 'value differs'):
            native.verify_semantics('h264', snapshot, expected)
        fields[0]['value'] = '50'
        fields.append(fields[0])
        with self.assertRaisesRegex(RuntimeError, 'expected one'):
            native.verify_semantics('h264', snapshot, expected)

    def test_native_v3_checks_each_slice_value_source_and_reference(self):
        fields, traces = [], []
        for index, first_mb in enumerate((0, 40)):
            parent = str(index * 10)
            fields.append({'id': parent, 'parentId': '', 'name': f'Slice prefix NAL[{index}]'})
            trace_fields = []
            for name, value in (('first_mb_in_slice', first_mb), ('slice_type', 6), ('pic_parameter_set_id', 0), ('frame_num', 1)):
                span = {'byteOffset': 100 + index * 10 + len(trace_fields), 'bitOffset': 0, 'bitLength': 8}
                trace_fields.append({'name': name, 'value': value, 'sourceBitSegments': [span]})
                fields.append({'parentId': parent, 'name': native_v3.SLICE_FIELDS[name], 'type': 'RBSP unsigned',
                               'value': str(value), 'span': {**span, 'byteOffset': str(span['byteOffset'])}})
            for name, value in (('prefixComplete', 'true'), ('sliceTypeName', 'B')):
                fields.append({'parentId': parent, 'name': name, 'value': value})
            for name, offset in (('referencedSPS', 20), ('referencedPPS', 30)):
                fields.append({'parentId': parent, 'name': name, 'span': {'byteOffset': str(offset), 'bitOffset': 0, 'bitLength': 24}})
            traces.append({'kind': 'Slice Header', 'tagId': 5, 'nalIndex': index, 'fields': trace_fields})
        expected = {'h264BFrames': {'configurationTagId': 1, 'traceBlocks': traces,
                                   'nals': [{'tagId': 1, 'type': 7, 'offset': 20, 'bytes': 3}, {'tagId': 1, 'type': 8, 'offset': 30, 'bytes': 3}]}}
        snapshot = {'selectedTag': {'id': '5'}, 'detail': {'detail': {'complete': True, 'truncated': False, 'diagnostics': [], 'fields': fields}}}
        self.assertEqual(native_v3.verify_semantics('b-slices', snapshot, expected)['prefixFieldsCompared'], 8)
        for mutation in ('value', 'span', 'reference', 'missing', 'duplicate', 'wrongTag'):
            damaged = copy.deepcopy(snapshot)
            items = damaged['detail']['detail']['fields']
            if mutation == 'value':
                next(f for f in items if f['name'] == 'sliceType')['value'] = '5'
            elif mutation == 'span':
                next(f for f in items if f['name'] == 'frameNum')['span']['bitOffset'] = 1
            elif mutation == 'reference':
                next(f for f in items if f['name'] == 'referencedSPS')['span']['byteOffset'] = '21'
            elif mutation == 'missing':
                items[:] = [f for f in items if f['name'] != 'pictureParameterSetId']
            elif mutation == 'duplicate':
                items.append(next(f for f in items if f['name'] == 'frameNum'))
            else:
                damaged['selectedTag']['id'] = '6'
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                native_v3.verify_semantics('b-slices', damaged, expected)

    def test_native_allows_owned_harness_cleanup_but_rejects_forced_app_exit(self):
        result = {'passed': True, 'remainingOwnedProcesses': [],
                  'forcedCleanup': [{'pid': 10, 'startTicks': '100', 'signal': 'SIGTERM'}]}
        detail = {'passed': True, 'pid': 20, 'exitCode': 0, 'sourceUnchanged': True, 'sourceFDsBeforeClose': [{'pid': 20, 'fd': '7'}],
                  'sourceFDsAfterClose': [], 'remainingAppProcesses': [], 'observedAppProcesses': [{'pid': 20, 'startTicks': '200'}]}
        native.verify_lifecycle(result, detail)
        result['forcedCleanup'].append({'pid': 20, 'startTicks': '200', 'signal': 'SIGTERM'})
        with self.assertRaisesRegex(RuntimeError, 'app process required'):
            native.verify_lifecycle(result, detail)
        result['forcedCleanup'].pop()
        detail['remainingAppProcesses'] = [{'pid': 20, 'startTicks': '200'}]
        with self.assertRaisesRegex(RuntimeError, 'lifecycle failed'):
            native.verify_lifecycle(result, detail)


if __name__ == '__main__':
    unittest.main()
