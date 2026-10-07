"""No-GUI checks of native side-info and replay evidence acceptance."""

import copy
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from generate import save
import native
import native_context_side as check


class NativeContextSideTests(unittest.TestCase):
    def test_old_native_requires_exact_side_info_omission_and_all_crc_rows(self):
        expected = {'physicalFrameCount': 3, 'completeFrameCount': 3, 'vbr': None,
                    'frames': [{'crc': {'status': 'not_protected'}} for _ in range(3)]}
        fields = [{'name': 'Frame inspection', 'value': '3 complete frame boundaries; 3 headers; stop=end;'},
                  {'name': 'sideInfoOmittedFrames', 'value': '1; desktop expands the first two side-info records; CLI v3 retains all inspected records'},
                  *[{'name': f'Side info Frame {i}', 'value': ''} for i in range(2)],
                  *[{'name': 'CRC16', 'value': 'not_protected (scope)'} for _ in range(3)]]
        snapshot = {'detail': {'detail': {'complete': True, 'truncated': True, 'fields': fields}}}
        native.verify_semantics('info-cbr', snapshot, {'mp3': {'info-cbr': expected}})
        for mode in ('count', 'marker', 'crc', 'complete'):
            damaged = copy.deepcopy(snapshot)
            detail = damaged['detail']['detail']
            if mode == 'count':
                detail['fields'][1]['value'] = '99; arbitrary omission'
            elif mode == 'marker':
                detail['fields'].pop(1)
            elif mode == 'crc':
                detail['fields'].pop()
            else:
                detail['complete'] = False
            with self.subTest(mode=mode), self.assertRaises(RuntimeError):
                native.verify_semantics('info-cbr', damaged, {'mp3': {'info-cbr': expected}})

    def test_native_field_paths_keep_granule_and_channel_identity(self):
        fields = [{'id': 'main', 'parentId': 'root', 'name': 'mainDataBegin'},
                  {'id': 'g0', 'parentId': 'root', 'name': 'Granule 0'},
                  {'id': 'c0', 'parentId': 'g0', 'name': 'Channel 0'},
                  {'id': 'c1', 'parentId': 'g0', 'name': 'Channel 1'},
                  {'id': 'a', 'parentId': 'c0', 'name': 'bigValues'},
                  {'id': 'b', 'parentId': 'c1', 'name': 'bigValues'}]
        got = check.mp3_fields(fields, {'id': 'root'})
        self.assertEqual(set(got), {'main_data_begin', 'granules[0].channels[0].big_values', 'granules[0].channels[1].big_values'})
        fields.append({'id': 'c', 'parentId': 'c0', 'name': 'bigValues'})
        with self.assertRaises(RuntimeError):
            check.mp3_fields(fields, {'id': 'root'})

    def test_replay_evidence_rejects_restart_premature_publication_and_cache_miss(self):
        def snapshot(start, end, target, state, checkpoints=1):
            values = {'contextSegmentStartTagId': start, 'nextTagId': end, 'selectedTagId': target,
                      'replayedTags': end - start, 'checkpointCount': checkpoints, 'contextBytesRead': 2,
                      'contextCanContinue': 'true' if state == 'budget_exhausted' else 'false',
                      'availableSPS': 0 if state == 'budget_exhausted' else 1,
                      'availablePPS': 0 if state == 'budget_exhausted' else 1}
            fields = [{'id': 'root', 'parentId': '', 'name': 'Parameter context', 'value': state}]
            fields += [{'id': name, 'parentId': 'root', 'name': name, 'value': str(value)} for name, value in values.items()]
            return {'detail': {'detail': {'fields': fields, 'truncated': state == 'budget_exhausted'}}}
        stages = {'context-initial.json': snapshot(0, 8192, 20002, 'budget_exhausted'),
                  'context-1.json': snapshot(8192, 16384, 20002, 'budget_exhausted', 2),
                  'context-2.json': snapshot(16384, 20002, 20002, 'found', 3),
                  'context-revisit.json': snapshot(8192, 8193, 8193, 'found', 4),
                  'snapshot.json': snapshot(20002, 20002, 20002, 'found', 4)}
        config = {'stages': 3, 'selectedTagId': 20002, 'revisitTagId': 8193}
        with tempfile.TemporaryDirectory() as directory, patch.object(check, 'verify_long'):
            path = Path(directory)
            for name, value in stages.items():
                save(path, name, value)
            self.assertEqual(len(check.verify_stages(path, config)), 3)
            for filename, field, value in (('context-1.json', 'contextSegmentStartTagId', '0'),
                                            ('context-initial.json', 'availableSPS', '1'),
                                            ('snapshot.json', 'replayedTags', '20002')):
                changed = copy.deepcopy(stages[filename])
                next(f for f in changed['detail']['detail']['fields'] if f['name'] == field)['value'] = value
                save(path, filename, changed)
                with self.subTest(field=field), self.assertRaises(RuntimeError):
                    check.verify_stages(path, config)
                save(path, filename, stages[filename])


if __name__ == '__main__':
    unittest.main()
