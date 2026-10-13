"""Independent source-oracle and verifier negative controls; no native UI."""

import copy
import json
from pathlib import Path
import re
import sys
import tempfile
import unittest

from generate import ROOT
from performance_baseline import BENCHMARKS, parse_benchmarks
from timing_navigation_oracle import generate
from verify_paged_codec import expected_span
from verify_timing_navigation import timing_parameters, verify_timing, verify_timing_native

sys.path.insert(0, str(ROOT / 'tools/desktop-native'))
from linux_smoke import check_mp3_navigation_stage


def assign(root, path, value):
    parts = re.sub(r'\[(\d+)\]', r'.\1', path).split('.')
    for index, part in enumerate(parts):
        key = int(part) if part.isdecimal() else part
        if isinstance(root, list):
            while len(root) <= key:
                root.append({})
        if index == len(parts) - 1:
            root[key] = value
            return
        if isinstance(root, dict) and key not in root:
            root[key] = [] if parts[index + 1].isdecimal() else {}
        root = root[key]


def witness(expected):
    sei = {'complete': True, 'framingComplete': True, 'bytesRead': expected['framing']['nalBytes'] - 1,
           'rbsp': {'ebspSpan': {'bitLength': (expected['framing']['nalBytes'] - 1) * 8}}, 'messages': []}
    for i, old in enumerate(expected['framing']['messages']):
        kind = 'bufferingPeriod' if expected['messageTypes'][i] == 0 else 'pictureTiming'
        context = {'state': 'resolved', 'source': expected['activationSource'], 'spsId': 0,
                   'spsSpan': expected['spsSpan'], 'evidenceSpan': expected['activationSpan'],
                   'parameters': timing_parameters(expected['timingParameters'])}
        m = {'index': i, 'kind': kind, 'syntaxSupported': True, 'complete': True, 'payloadComplete': True,
             kind: {'context': context}}
        for name in ('payloadType', 'payloadSize'):
            m[name] = {'value': old[name]['value'], 'span': expected_span(old[name])}
        if old['payloadSpan']['bitLength']:
            m['payloadSpan'] = expected_span(old['payloadSpan'])
        sei['messages'].append(m)
    for field in expected['fields']:
        assign(sei, field['name'], {'value': field['value'], 'span': expected_span(field)})
    return {'schemaVersion': 6, 'tag': {'id': expected['tagId']}, 'diagnostics': [],
            'detail': {'complete': True, 'nalUnitSequence': {'nalUnits': [
                {'span': {'byteOffset': expected['offset']}, 'sei': sei}]}}}


def navigation(state='preparing', request='1', progress=1024):
    values = {'requestedFrameIndex': '2600', 'requestedPageStart': '2560', 'pageStartIndex': '960',
              'replayPageBudget': '16', 'replayPages': '16', 'checkpointCount': '16',
              'nextFrameIndex': str(progress), 'pageState': state, 'canContinue': str(state == 'preparing').lower()}
    if state == 'ready':
        values.update(pageStartIndex='2560', replayPages='9', selectedFrameIndex='2600')
    return {'detail': {'state': 'ready', 'requestId': request,
                       'detail': {'fields': [{'name': k, 'value': v} for k, v in values.items()]}}}


class TimingNavigationTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.temp = tempfile.TemporaryDirectory()
        cls.root = Path(cls.temp.name)
        generate(cls.root)
        cls.expected = json.loads((cls.root / 'expectations.json').read_text())

    @classmethod
    def tearDownClass(cls):
        cls.temp.cleanup()

    def test_mp3_original_index_and_late_offset_are_authored(self):
        rows = self.expected['mp3Navigation']['long']['frames']
        self.assertEqual(len(rows), 3000)
        for index in (63, 64, 2600):
            self.assertEqual(rows[index]['offset'], 25 + 417 * index)
            self.assertEqual(rows[index]['frameIndex'], index)
            self.assertEqual([r['frameIndex'] for r in rows[index]['ranges']], [index - 2, index - 1])
        self.assertGreater(rows[2600]['offset'], 1 << 20)

    def test_all_authored_timing_bit_segments_reconstruct_source(self):
        for case in self.expected['seiTiming'].values():
            source = (self.root / case['file']).read_bytes()
            for field in case['fields']:
                bits = ''
                for part in field['sourceBitSegments']:
                    offset, start, length = part['byteOffset'], part['bitOffset'], part['bitLength']
                    raw = source[offset:offset + (start + length + 7) // 8]
                    bits += ''.join(f'{b:08b}' for b in raw)[start:start + length]
                self.assertEqual(bits, field['encodedBits'])
                if field['name'].endswith('timeOffset'):
                    self.assertEqual(int(bits, 2) - ((1 << len(bits)) if bits[0] == '1' else 0), field['value'])

    def test_epb_splits_signed_offset_without_inventing_rbsp_bytes(self):
        case = self.expected['seiTiming']['wide-epb']
        field = next(f for f in case['fields'] if f['name'].endswith('timeOffset'))
        self.assertEqual(field['value'], -1073741824)
        self.assertGreater(len(field['sourceBitSegments']), 1)
        self.assertEqual(sum(p['bitLength'] for p in field['sourceBitSegments']), 31)

    def test_timing_witness_is_valid_and_value_source_omission_changes_fail(self):
        case = self.expected['seiTiming']['dual-hrd']
        document = witness(case)
        self.assertEqual(verify_timing(document, case)['mappedFields'], 41)
        for change in ('value', 'source', 'missing', 'context'):
            bad = copy.deepcopy(document)
            timing = bad['detail']['nalUnitSequence']['nalUnits'][0]['sei']['messages'][1]['pictureTiming']
            if change == 'value':
                timing['cpbRemovalDelay']['value'] += 1
            elif change == 'source':
                timing['cpbRemovalDelay']['span']['sourceSpans'][0]['byteOffset'] += 1
            elif change == 'missing':
                del timing['clockTimestamps'][0]['timeOffset']
            else:
                timing['context']['evidenceSpan']['byteOffset'] += 1
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                verify_timing(bad, case)

    def test_missing_or_wrong_sps_activation_fails_complete_witness(self):
        case = self.expected['seiTiming']['dual-hrd']
        for change in ('missing', 'width', 'sps'):
            bad = witness(case)
            # The witness shares expected immutable objects; copy before mutation.
            bad = copy.deepcopy(bad)
            context = bad['detail']['nalUnitSequence']['nalUnits'][0]['sei']['messages'][1]['pictureTiming']['context']
            if change == 'missing':
                context['state'] = 'missing'
            elif change == 'width':
                context['parameters']['nalHrd']['cpbRemovalDelayLength'] += 1
            else:
                context['spsSpan']['byteOffset'] += 1
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                verify_timing(bad, case)

    def test_native_budget_tail_requires_explicit_full_projection(self):
        case = self.expected['seiTiming']['message-budget']
        def f(i, name, value, parent=''):
            return {'id': str(i), 'parentId': parent, 'name': name, 'value': value, 'span': None}
        fields = [f(0, 'SEI messages NAL[0]', ''), f(1, 'seiMessageCount', '64', '0')]
        fields += [f(i, 'filler', '') for i in range(2, 511)]
        fields += [f(511, 'SEI message[0]', 'bufferingPeriod', '0')]
        snapshot = {'detail': {'detail': {'complete': False, 'truncated': True, 'fields': fields}}}
        verify_timing_native(snapshot, case)
        for change in ('no_truncation', 'not_full', 'wrong_index', 'wrong_type'):
            bad = copy.deepcopy(snapshot)
            detail = bad['detail']['detail']
            if change == 'no_truncation':
                detail['truncated'] = False
            elif change == 'not_full':
                detail['fields'].pop(2)
            elif change == 'wrong_index':
                detail['fields'][-1]['name'] = 'SEI message[1]'
            else:
                detail['fields'][-1]['value'] = 'pictureTiming'
            with self.subTest(change=change), self.assertRaises(RuntimeError):
                verify_timing_native(bad, case)

    def test_direct_navigation_requires_new_request_and_forward_cursor(self):
        first = check_mp3_navigation_stage(navigation(), 2600)
        second = check_mp3_navigation_stage(navigation(request='3', progress=2048), 2600, first)
        self.assertEqual(second['nextFrameIndex'], 2048)
        check_mp3_navigation_stage(navigation('ready', '5', 2624), 2600, second)
        for bad in (navigation(request='1', progress=2048), navigation(request='3', progress=1024)):
            with self.assertRaises(RuntimeError):
                check_mp3_navigation_stage(bad, 2600, first)

    def test_direct_navigation_rejects_wrong_page_and_early_syntax(self):
        for name, value in [('requestedPageStart', '0'), ('replayPages', '17'), ('checkpointCount', '65'),
                            ('canContinue', 'false'), ('Frame inspection', 'wrong page')]:
            bad = navigation()
            fields = bad['detail']['detail']['fields']
            existing = next((f for f in fields if f['name'] == name), None)
            if existing:
                existing['value'] = value
            else:
                fields.append({'name': name, 'value': value})
            with self.subTest(name=name), self.assertRaises(RuntimeError):
                check_mp3_navigation_stage(bad, 2600)

    def test_performance_parser_checks_coverage_not_machine_speed(self):
        lines = []
        for name in BENCHMARKS:
            extra = ' 41 pages/op 3 requests/op 94800 source-B/op' if name.endswith('cold') else (
                    ' 1 pages/op 1 requests/op 2312 source-B/op' if name.endswith('warm') else ' 0 source-B/op')
            lines.append(name + '-20 1 99999999 ns/op 100 B/op 1 allocs/op 0 reads/op' + extra)
        text = '\n'.join(lines)
        self.assertEqual(len(parse_benchmarks(text, 1)), 5)
        for corrupted in (text.replace('94800 source-B/op', '1 source-B/op'), text + '\n' + lines[0], text.replace(lines[0], '')):
            with self.assertRaises(RuntimeError):
                parse_benchmarks(corrupted, 1)


if __name__ == '__main__':
    unittest.main()
