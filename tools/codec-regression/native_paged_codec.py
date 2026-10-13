#!/usr/bin/env python3
"""Real MP3 page/summary navigation and SEI source evidence regression rounds."""

import argparse
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, Commands, digest, executable, load_expectations, new_directory, require, save
from native import verify_case
from native_browse_evidence import verify_saved_snapshot
from native_full_summary import metric_map
from native_search_history import read_ui, verify_hex_ui
from verify_full_codec import expected_mp3
from verify_paged_codec import verify_mp3_native_page, verify_sei_native


CASES = ('mp3-summary-link', 'mp3-long-page', 'mp3-cross-tag', 'mp3-cross-page',
         'sei-recovery', 'sei-uuid-source', 'sei-truncated', 'sei-budget')


def summary_evidence(directory, native, data, case):
    expected = expected_mp3(data)
    pages = native['mp3SummaryPages']
    require(len(pages) == 3, 'summary did not visit all retained event pages')
    for stage, start in zip(pages, (0, 100, 200)):
        path = directory / stage['file']
        checked = stage['snapshotCheck']
        require(checked['schemaValid'] and checked['sha256'] == digest(path), 'summary export hash/schema differs')
        snapshot = json.loads(path.read_text())
        overview = snapshot['summary']['overview']
        metrics = metric_map(overview['metrics'])
        for name in ('completeFrameCount', 'sampleCount', 'parameterChangeCount', 'bitrateChangeCount', 'frameBytes', 'unresolvedBytes'):
            require(metrics[('mp3Frames', name)] == str(expected[name]), 'native summary aggregate differs: ' + name)
        retained = next(row for row in overview['lists'] if row['kind'] == 'mp3')
        require(retained == {'kind': 'mp3', 'observed': str(expected['eventCount']), 'retained': '256', 'truncated': True},
                'summary retained events confused with frame count')
        events = snapshot['summaryPage']['page']['events']
        require(len(events) == min(100, 256-start), 'summary page event count differs')
        for event, wanted in zip(events, expected['events'][start:start + 100]):
            values = {item['name']: item['value'] for item in event['values']}
            require(event['tagId'] == str(wanted['tagId']) and event['offset'] == str(wanted['sourceSpan']['byteOffset']) and
                    values['frameIndex'] == str(wanted['frameIndex']) and values['frameStatus'] == wanted['status'],
                    'summary event/frame source differs')
        read_ui(directory, stage)
    stage = native['mp3SummaryDetail']
    require(stage['frameIndex'] == 255 and stage['pageStart'] == 192, 'summary link did not target late-frame page')
    snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
    return verify_mp3_native_page(snapshot, case, data, 0, 192, 255)


def verify_mp3_directory(directory, case, data, tag, start, selected, steps, summary, dependency):
    native = json.loads((directory / 'native-result.json').read_text())
    result = {}
    if summary:
        result['summaryLink'] = summary_evidence(directory, native, data, case)
    pages = native.get('mp3FramePages', [])
    origin = 192 if summary else 0
    require([stage['start'] for stage in pages] == list(range(origin + 64, start + 1, 64)) and len(pages) == steps,
            'native page transitions skipped, repeated or rebased frames')
    for stage in pages:
        ui = read_ui(directory, stage)
        require(any(f'MP3 帧页 {stage["start"]} · 本页 ' in row['text'] for row in ui), 'native page UI differs from audited transition')
    if 'mp3PageSelected' in native:
        stage = native['mp3PageSelected']
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        result['selected'] = verify_mp3_native_page(snapshot, case, data, tag, start, selected)
    if dependency:
        stage = native['mp3HistoryInitial']
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        result['history'] = verify_mp3_native_page(snapshot, case, data, tag, start, selected, True)
        for stage in native['mp3HistorySources']:
            verify_hex_ui(read_ui(directory, stage), stage['span'],
                          f'MP3 来源 Tag {stage["tagId"]} 帧 {stage["frameIndex"]}', data)
        require(native['mp3HistorySources'], 'no physical source navigation occurred')
        stage = native['mp3HistorySourceDetail']
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        source_tag, source_frame = int(stage['tagId']), int(stage['frameIndex'])
        # Source jumps can select a frame not chosen for the standalone oracle's
        # detailed expansion. Its header, original index and range remain exact.
        from verify_paged_codec import verify_mp3_native_frames
        verify_mp3_native_frames(snapshot, case, source_tag, source_frame // 64 * 64, source_frame)
    final = json.loads((directory / 'snapshot.json').read_text())
    result['final'] = verify_mp3_native_page(final, case, data, tag, start, selected, dependency)
    result['pageSteps'] = steps
    return result


def verify_sei_directory(directory, case, data, field_name):
    native = json.loads((directory / 'native-result.json').read_text())
    snapshot = json.loads((directory / 'snapshot.json').read_text())
    result = verify_sei_native(snapshot, case)
    if field_name:
        stage = native['fieldSource']
        require(stage['fieldName'] == field_name, 'SEI did not activate the requested field source')
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        verify_sei_native(snapshot, case)
        fields = [field for field in snapshot['detail']['detail']['fields'] if field['name'] == field_name]
        require(len(fields) == 1 and fields[0]['span'] == stage['span'], 'SEI source selection identity differs')
        verify_hex_ui(read_ui(directory, stage), stage['span'], field_name, data)
        result['sourceField'] = field_name
    return result


def run(args, commands):
    expected, binary_hash = load_expectations(args.input_dir), digest(args.binary)
    rows = []
    for name in CASES:
        if args.case and name not in args.case:
            continue
        directory = args.output_dir / name
        if name.startswith('mp3-'):
            long = name in ('mp3-summary-link', 'mp3-long-page')
            case = expected['mp3Paging']['long-tag' if long else 'cross-tag']
            tag, start, selected, steps = {
                'mp3-summary-link': (0, 192, 255, 0), 'mp3-long-page': (0, 2560, 2600, 37),
                'mp3-cross-tag': (2, 0, 0, 0), 'mp3-cross-page': (2, 64, 64, 1),
            }[name]
            dependency = name != 'mp3-summary-link'
            flags = ['--mp3-frame', str(selected)]
            if long:
                flags += ['--mp3-summary', '--mp3-summary-link']
            if steps:
                flags += ['--mp3-page-steps', str(steps)]
            if dependency:
                flags += ['--mp3-history']
            def semantics(_name, _snapshot, _expected):
                return verify_mp3_directory(directory, case, data, tag, start, selected, steps, long, dependency)
        else:
            source_case, field_name = {
                'sei-recovery': ('mixed', 'recoveryFrameCnt'), 'sei-uuid-source': ('mixed', 'uuid.source[1]'),
                'sei-truncated': ('short-uuid', None), 'sei-budget': ('byte-budget', None),
            }[name]
            case = expected['sei'][source_case]
            tag, flags = case['tagId'], ['--field-name', field_name] if field_name else []
            def semantics(_name, _snapshot, _expected):
                return verify_sei_directory(directory, case, data, field_name)
        source = args.input_dir / case['file']
        data = source.read_bytes()
        commands.run(name, [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                     '--source', source, '--tag', str(tag), '--output-dir', directory,
                     '--timeout-seconds', str(args.timeout_seconds), *flags], timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, {}, semantic_check=semantics)
        require(row['binarySHA256'] == binary_hash, 'native production executable changed')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash, 'production executable changed during native acceptance')
    load_expectations(args.input_dir)
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash,
            'expectationsSHA256': digest(args.input_dir / 'expectations.json'), 'cases': rows,
            'scope': 'Real UI paging, selected syntax/source mappings and lifecycle; no audio or image decoding.'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--case', action='append', choices=CASES)
    parser.add_argument('--timeout-seconds', type=int, default=1800)
    args = parser.parse_args(argv)
    if not 30 <= args.timeout_seconds <= 1800:
        parser.error('--timeout-seconds must be 30 through 1800')
    args.input_dir = args.input_dir.resolve()
    args.output_dir.mkdir(parents=True)
    save(args.output_dir, 'run.json', {'python': sys.version, 'binary': str(args.binary), 'binarySHA256': digest(args.binary),
                                      'scriptSHA256': digest(Path(__file__)), 'cases': args.case or list(CASES)})
    try:
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
