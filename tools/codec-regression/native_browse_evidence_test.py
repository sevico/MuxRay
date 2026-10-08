"""No-GUI mutations of frame selection, source evidence and exported curves."""

import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from generate import ROOT, digest, flv_tags, save, wrap_mp3
import native_browse_evidence as check
from side_info_oracle import stream_oracle
from verify_v3 import make_flv


class BrowseEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.snapshot = json.loads((ROOT / 'testdata/golden/desktop-diagnostic-snapshot-full-v2.json').read_text())
        raw = (bytes.fromhex('fffb9064') + bytes(413)) * 3
        first = wrap_mp3(raw)
        second = bytearray(first[13:])
        second[6] = 26
        self.source = first[:4] + b'\x00' + first[5:] + second
        self.case = stream_oracle(raw)
        rate = {'bitrate': {'bytes': 1252, 'coveredIntervalMs': 26, 'averageBitsPerSecond': 1252 * 8000 // 26}}
        self.cli = {'schemaVersion': 2, 'scan': {'complete': True, 'indexedTagCount': 2},
                    'summary': {'complete': True, 'tags': {'total': 2}, 'audio': rate, 'video': {},
                                'mp3': {'track': rate}, 'gop': {'count': 0, 'closedDurationCount': 0}}}

    def test_selected_frame_checks_all_fields_and_retained_history(self):
        row = check.verify_mp3_selection(self.snapshot, self.case, self.source, 2)
        self.assertGreater(row['sourceMappedFieldsCompared'], 50)
        for mode in ('selected', 'indices', 'source', 'presence', 'dependency', 'expand'):
            snapshot = copy.deepcopy(self.snapshot)
            fields = snapshot['detail']['detail']['fields']
            field = lambda name: next(f for f in fields if f['name'] == name)
            if mode == 'selected':
                field('selectedFrameIndex')['value'] = '1'
            elif mode == 'indices':
                field('selectableFrameIndices')['value'] = '0,1,2,3'
            elif mode == 'source':
                field('mainDataBegin')['span']['byteOffset'] = '29'
            elif mode == 'presence':
                fields.remove(field('globalGain'))
            elif mode == 'dependency':
                field('mainDataDependency')['value'] = 'within_tag; forged history'
            else:
                field('Side info Frame 2')['name'] = 'Side info Frame 1'
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                check.verify_mp3_selection(snapshot, self.case, self.source, 2)

    def test_snapshot_curve_requires_independent_values_cli_and_identity(self):
        self.assertTrue(check.verify_curve(self.snapshot, self.source, self.cli, 'gopMax')['cliSummaryCompared'])
        for mode in ('value', 'identity', 'metric', 'missing', 'cli', 'range'):
            snapshot, cli = copy.deepcopy(self.snapshot), copy.deepcopy(self.cli)
            if mode == 'value':
                snapshot['mediaCurvePage']['page']['points'][0]['mp3']['coveredBytes'] = '1253'
            elif mode == 'identity':
                snapshot['mediaCurvePage']['requestId'] = '1'
            elif mode == 'metric':
                snapshot['selection']['mediaCurve']['metric'] = 'audio'
            elif mode == 'missing':
                snapshot['mediaCurvePage'] = None
            elif mode == 'range':
                snapshot['mediaCurvePage']['page']['bounds']['next'] = '0'
            else:
                cli['summary']['audio']['bitrate']['averageBitsPerSecond'] += 1
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                check.verify_curve(snapshot, self.source, cli, 'gopMax')

    def test_saved_snapshot_rejects_artifact_and_response_identity_mutations(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'snapshot.json'
            for mode in ('valid', 'artifact', 'source', 'detailSession', 'detailRequest', 'detailTag', 'selectedTag', 'hexSession', 'hexRequest', 'hexBytes'):
                snapshot = copy.deepcopy(self.snapshot)
                if mode.startswith('detail'):
                    key = {'detailSession': 'sessionId', 'detailRequest': 'requestId', 'detailTag': 'tagId'}[mode]
                    snapshot['detail'][key] = '99'
                elif mode == 'selectedTag':
                    snapshot['selectedTag']['id'] = '99'
                elif mode in ('hexSession', 'hexRequest'):
                    snapshot['hex']['sessionId' if mode == 'hexSession' else 'requestId'] = '99'
                elif mode == 'hexBytes':
                    snapshot['hex']['window']['hex'] = '00'
                save(path.parent, path.name, snapshot)
                checked = {'schemaValid': True, 'hex': {'matchesSource': True}, 'sha256': digest(path),
                           'sourceSHA256': hashlib.sha256(self.source).hexdigest()}
                if mode == 'artifact':
                    path.write_text(path.read_text() + '\n')
                elif mode == 'source':
                    checked['sourceSHA256'] = 'bad'
                if mode == 'valid':
                    self.assertEqual(check.verify_saved_snapshot(path, checked, self.source), snapshot)
                else:
                    with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                        check.verify_saved_snapshot(path, checked, self.source)

    def test_avc_evidence_rejects_wrong_declaration_source_and_slice(self):
        source = make_flv([bytes.fromhex('1701000000') + bytes.fromhex('000000026560') * 2])
        tag = flv_tags(source)[0]
        nals = [{'tagId': 0, 'nalIndex': i, 'offset': tag['payloadOffset'] + 9 + i * 6, 'bytes': 2, 'type': 5} for i in range(2)]
        traces = [{'kind': 'Slice Header', **n, 'fields': [{'name': 'slice_type', 'value': 2,
                   'sourceBitSegments': [{'byteOffset': n['offset'] + 1, 'bitOffset': 0, 'bitLength': 3}]}]} for n in nals]
        expected = {'h264BFrames': {'nals': nals, 'traceBlocks': traces}}
        fields = []
        def add(parent, name, value, span=None, kind='derived'):
            result = {'id': str(len(fields)), 'parentId': parent, 'name': name, 'value': value, 'span': span, 'type': kind}
            fields.append(result)
            return result['id']
        root = add('', 'Keyframe evidence', 'keyframe_with_idr')
        for name, value in {'declaredFrameType': '1', 'observedIDR': 'yes', 'declarationRelation': 'keyframe_with_idr',
                            'nalHeaderCoverageComplete': 'true', 'sliceTypeCoverageComplete': 'true', 'observedSliceTypes': 'I',
                            'retainedNALUnits': '2', 'inspectedNALHeaders': '2', 'unclassifiedNALHeaders': '0', 'observedSliceHeaders': '2'}.items():
            add(root, name, value, {'byteOffset': str(tag['payloadOffset']), 'bitOffset': 0, 'bitLength': 4} if name == 'declaredFrameType' else None)
        for name in ('evidenceNALType', 'evidenceIDRNALType'):
            add(root, name, '5 (idrSlice; NAL[0])', {'byteOffset': str(nals[0]['offset']), 'bitOffset': 3, 'bitLength': 5})
        add(root, 'evidenceSliceTypeI', '2 (I; NAL[0])', check.native_span(traces[0]['fields'][0]['sourceBitSegments'][0]))
        for trace in traces:
            prefix = add('', f'Slice prefix NAL[{trace["nalIndex"]}]', '')
            add(prefix, 'prefixComplete', 'true')
            add(prefix, 'sliceType', '2', check.native_span(trace['fields'][0]['sourceBitSegments'][0]), 'RBSP unsigned')
        snapshot = {'selectedTag': {'id': '0'}, 'detail': {'detail': {'kind': 'avcVideo', 'complete': True, 'truncated': False, 'diagnostics': [], 'fields': fields}}}
        self.assertEqual(check.verify_avc_evidence(snapshot, expected, source, 0)['sliceTypes'], ['I'])
        for name, key, value in [('observedIDR', 'value', 'no'), ('declarationRelation', 'value', 'inter_with_idr'),
                                 ('evidenceIDRNALType', 'span', None), ('observedSliceTypes', 'value', 'P'),
                                 ('evidenceSliceTypeI', 'value', '7 (I; NAL[0])')]:
            damaged = copy.deepcopy(snapshot)
            next(f for f in damaged['detail']['detail']['fields'] if f['name'] == name)[key] = value
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                check.verify_avc_evidence(damaged, expected, source, 0)
        changed = bytearray(source)
        changed[nals[0]['offset']] = 0x61
        with self.assertRaisesRegex(RuntimeError, 'NAL evidence'):
            check.verify_avc_evidence(snapshot, expected, changed, 0)

    def test_actual_source_click_requires_saved_hex_bits_and_ui_evidence(self):
        span = {'byteOffset': '0', 'bitOffset': 0, 'bitLength': 4}
        field = {'id': 'decl', 'parentId': 'root', 'name': 'declaredFrameType', 'span': span, 'value': '1'}
        snapshot = {'session': {'sessionId': 's'}, 'selectedTag': {'id': '0'},
                    'selection': {'sessionId': 's', 'selectedTagId': '0', 'detailRequestId': '1', 'hexRequestId': '2'},
                    'detail': {'apiVersion': 1, 'state': 'ready', 'error': None, 'sessionId': 's', 'requestId': '1', 'tagId': '0',
                               'detail': {'fields': [{'id': 'root', 'name': 'Keyframe evidence', 'parentId': '', 'span': None}, field]}},
                    'hex': {'apiVersion': 1, 'state': 'ready', 'error': None, 'sessionId': 's', 'requestId': '2', 'window': {'offset': '0', 'hex': '17'}}}
        text = 'FLV frameType · 0 : 0 · 4 bit'
        native = {'avcEvidenceInitial': {'file': 'initial.json', 'snapshotCheck': {'schemaValid': True, 'hex': {'matchesSource': True},
                  'sha256': '', 'sourceSHA256': hashlib.sha256(b'\x17').hexdigest()}},
                  'avcEvidenceStages': [{'field': 'declaredFrameType', 'span': span, 'selectionText': text, 'uiFile': 'stage-ui.json', 'uiSHA256': ''}]}
        with tempfile.TemporaryDirectory() as directory, patch.object(check, 'verify_avc_evidence'), patch.object(check, 'verify_curve'):
            path = Path(directory)
            def artifacts(mode='valid'):
                saved, run = copy.deepcopy(snapshot), copy.deepcopy(native)
                ui = [{'name': 'Hex 定位范围', 'text': text + '；结束 0 : 4（不含）。'}]
                ui += [{'name': f'bit {i}' + (' 范围内' if i < 4 else ''), 'text': str((0x17 >> (7 - i)) & 1)} for i in range(8)]
                ui.append({'name': 'Hex 实际窗口', 'text': '实际窗口 [0, 1) · 1 byte / 文件 1 byte'})
                if mode == 'missing':
                    run['avcEvidenceStages'] = []
                elif mode == 'span':
                    run['avcEvidenceStages'][0]['span']['bitOffset'] = 1
                elif mode == 'hex':
                    saved['hex']['window']['hex'] = '27'
                elif mode == 'offset':
                    ui[-1]['text'] = '实际窗口 [1, 2) · 1 byte / 文件 2 byte'
                elif mode == 'ui':
                    ui = []
                elif mode == 'bit':
                    ui[-2]['text'] = '0'
                elif mode == 'highlight':
                    ui[-2]['name'] += ' 范围内'
                save(path, 'initial.json', saved)
                run['avcEvidenceInitial']['snapshotCheck']['sha256'] = digest(path / 'initial.json')
                save(path, 'stage-ui.json', ui)
                if run['avcEvidenceStages']:
                    run['avcEvidenceStages'][0]['uiSHA256'] = digest(path / 'stage-ui.json') if mode != 'hash' else 'bad'
                save(path, 'native-result.json', run)
            artifacts()
            self.assertTrue(check.verify_evidence_clicks(path, snapshot, {}, b'\x17', 0, {}, 'video')['hexByteAndBitSelectionCompared'])
            for mode in ('missing', 'span', 'hex', 'offset', 'ui', 'hash', 'bit', 'highlight'):
                artifacts(mode)
                with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                    check.verify_evidence_clicks(path, snapshot, {}, b'\x17', 0, {}, 'video')


if __name__ == '__main__':
    unittest.main()
