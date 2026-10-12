"""No-GUI negative controls for native all-frame summary artifact verification."""
import copy
import unittest

import native_full_summary as native
from verify_full_codec import expected_mp3


def snapshot(start):
    oracle = expected_mp3(native.fixture())
    metrics = [{'section': 'mp3Frames', 'name': key, 'value': str(value), 'unit': ''} for key, value in oracle.items() if isinstance(value, int) and not isinstance(value, bool)]
    metrics.extend([{'section': 'mp3Frames', 'name': name, 'value': 'true', 'unit': ''} for name in ('complete', 'scanComplete')])
    d = oracle['durationSeconds']
    metrics.append({'section': 'mp3Frames', 'name': 'durationSeconds', 'value': f'{d["numerator"]}/{d["denominator"]}', 'unit': 's'})
    events = []
    for index, event in enumerate(oracle['events'][start:start + 100], start):
        p = event['parameters']
        values = dict(frameIndex=str(event['frameIndex']), frameStatus=event['status'], sourceBitLength=str(event['sourceSpan']['bitLength']),
                      samplingFrequencyHz=str(p['samplingFrequencyHz']), channels=str(p['channels']), samplesPerFrame=str(p['samplesPerFrame']), bitrateKbps=str(event['bitrateKbps']))
        values.update({name: str(event[name]).lower() if name in event else None for name in ('parametersChangedFromPrevious', 'bitrateChangedFromPrevious')})
        events.append(dict(id=str(index), title='mp3Frame', tagId=str(event['tagId']), offset=str(event['sourceSpan']['byteOffset']), bitOffset=0, dts=str(event['dts']),
                           values=[{'name': key, 'value': value} for key, value in values.items()]))
    return oracle, {'schemaVersion': 3, 'session': {'sessionId': 's'},
        'selection': {'schemaVersion': 3, 'summaryRequestId': 'r', 'summaryPage': {'kind': 'mp3', 'start': str(start), 'count': 100}},
        'summary': {'sessionId': 's', 'requestId': 'r', 'state': 'ready', 'error': None,
                    'overview': {'complete': True, 'hasErrors': False, 'scanComplete': True, 'scanHasErrors': False,
                                 'sourceSize': str(len(native.fixture())), 'indexedTags': '1', 'metrics': metrics,
                                 'lists': [{'kind': 'mp3', 'observed': '300', 'retained': '256', 'truncated': True}]}},
        'summaryPage': {'sessionId': 's', 'requestId': 'r', 'kind': 'mp3', 'error': None,
                        'page': {'bounds': {'start': str(start), 'limit': 100, 'total': '256', 'next': str(start + len(events))}, 'events': events}}}


class NativeFullSummaryTest(unittest.TestCase):
    def test_fixture_exercises_all_frames_and_unretained_changes(self):
        oracle = expected_mp3(native.fixture())
        self.assertEqual((oracle['completeFrameCount'], oracle['parameterChangeCount'], oracle['bitrateChangeCount']), (300, 4, 2))
        self.assertEqual(oracle['durationSeconds'], {'numerator': 47987, 'denominator': 6125})
        self.assertEqual(len(oracle['events']), 256)
        self.assertGreater(oracle['events'][-1]['frameIndex'], 64)

    def test_every_retained_page_matches(self):
        for start, count in ((0, 100), (100, 100), (200, 56)):
            oracle, doc = snapshot(start)
            self.assertEqual(native.verify_summary(doc, oracle, start), count)

    def test_reject_stale_identity_or_page_omission(self):
        oracle, original = snapshot(100)
        mutations = [lambda d: d['summary'].update(requestId='old'),
                     lambda d: d['summaryPage']['page']['events'].pop(),
                     lambda d: d['summaryPage']['page']['bounds'].update(start='0'),
                     lambda d: d['summary']['overview']['lists'][0].update(observed='256'),
                     lambda d: d['summary']['overview']['lists'][0].update(truncated=False)]
        for change in mutations:
            doc = copy.deepcopy(original); change(doc)
            with self.assertRaises(RuntimeError): native.verify_summary(doc, oracle, 100)

    def test_reject_wrong_source_frame_and_lost_aggregate_change(self):
        oracle, original = snapshot(200)
        mutations = [lambda d: d['summaryPage']['page']['events'][-1].update(offset='25'),
                     lambda d: d['summaryPage']['page']['events'][-1].update(dts='100'),
                     lambda d: next(m for m in d['summary']['overview']['metrics'] if m['name'] == 'parameterChangeCount').update(value='2'),
                     lambda d: next(m for m in d['summary']['overview']['metrics'] if m['name'] == 'durationSeconds').update(value='8/1'),
                     lambda d: d['summary']['overview']['metrics'].append(d['summary']['overview']['metrics'][0])]
        for change in mutations:
            doc = copy.deepcopy(original); change(doc)
            with self.assertRaises(RuntimeError): native.verify_summary(doc, oracle, 200)


if __name__ == '__main__': unittest.main()
