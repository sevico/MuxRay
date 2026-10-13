#!/usr/bin/env python3
"""Native direct-frame navigation and timing-SEI evidence on authored bytes."""

import argparse
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, Commands, digest, executable, load_expectations, new_directory, require, save
from native import verify_case
from native_browse_evidence import verify_saved_snapshot
from native_search_history import read_ui, verify_hex_ui
from verify_paged_codec import verify_mp3_native_page
from verify_timing_navigation import verify_timing_native


CASES = ('mp3-direct-2600', 'mp3-boundary-63-64', 'sei-dual-delay', 'sei-cross-tag',
         'sei-offset-epb', 'sei-missing', 'sei-truncated', 'sei-message-budget')


def verify_navigation(directory, oracle, data, target, walk):
    sys.path.insert(0, str(ROOT / 'tools/desktop-native'))
    from linux_smoke import check_mp3_navigation_stage
    native = json.loads((directory / 'native-result.json').read_text())
    stages = native['mp3DirectNavigation']
    require([s['state'] for s in stages] == (['preparing', 'preparing', 'ready'] if target == 2600 else ['ready']),
            'direct navigation did not retain every explicit bounded request')
    if target == 2600:
        require([s['nextFrameIndex'] for s in stages] == [1024, 2048, 2624], 'direct navigation progress differs')
        require([s['replayPages'] for s in stages] == [16, 16, 9], 'direct navigation replay work differs')
    previous, mapped = None, 0
    for stage in stages:
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        checked = check_mp3_navigation_stage(snapshot, target, previous)
        require(all(stage[k] == v for k, v in checked.items()), 'direct navigation receipt differs from export')
        read_ui(directory, stage)
        if stage['state'] == 'ready':
            checked = verify_mp3_native_page(snapshot, oracle, data, 0, target // 64 * 64, target)
            mapped += checked['mappedSideInfoFields']
        previous = stage
    adjacent = native.get('mp3AdjacentFrames', [])
    require([s['direction'] for s in adjacent] == walk, 'adjacent frame action sequence differs')
    for stage in adjacent:
        target += 1 if stage['direction'] == 'next' else -1
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        checked = check_mp3_navigation_stage(snapshot, target)
        require(all(stage[k] == v for k, v in checked.items()), 'adjacent frame receipt differs from export')
        require(int(stage['requestId']) > int(previous['requestId']), 'adjacent action reused an old request')
        read_ui(directory, stage)
        checked = verify_mp3_native_page(snapshot, oracle, data, 0, target // 64 * 64, target)
        mapped += checked['mappedSideInfoFields']
        previous = stage
    final = json.loads((directory / 'snapshot.json').read_text())
    checked = verify_mp3_native_page(final, oracle, data, 0, target // 64 * 64, target)
    mapped += checked['mappedSideInfoFields']
    return {'requests': len(stages) + len(adjacent), 'preparingRequests': len(stages) - 1,
            'finalFrame': target, 'mappedFields': mapped}


def verify_sei(directory, oracle, data, field):
    native = json.loads((directory / 'native-result.json').read_text())
    final = json.loads((directory / 'snapshot.json').read_text())
    result = verify_timing_native(final, oracle)
    if field:
        stage = native['fieldSource']
        require(stage['fieldName'] == field, 'timing source action target differs')
        snapshot = verify_saved_snapshot(directory / stage['file'], stage['snapshotCheck'], data)
        verify_timing_native(snapshot, oracle)
        fields = [f for f in snapshot['detail']['detail']['fields'] if f['name'] == field]
        require(len(fields) == 1 and fields[0]['span'] == stage['span'], 'timing field source receipt differs')
        verify_hex_ui(read_ui(directory, stage), stage['span'], field, data)
        result['sourceField'] = field
    return result


def run(args, commands):
    expected = load_expectations(args.input_dir)
    binary_hash, cases = digest(args.binary), []
    for name in CASES:
        if args.case and name not in args.case:
            continue
        directory = args.output_dir / name
        if name.startswith('mp3-'):
            oracle = expected['mp3Navigation']['long']
            target, walk = (2600, []) if name == 'mp3-direct-2600' else (63, ['next', 'previous'])
            tag, flags = 0, ['--mp3-jump-frame', str(target)]
            for direction in walk:
                flags.extend(['--mp3-frame-walk', direction])
            def semantics(_name, _snapshot, _expected):
                return verify_navigation(directory, oracle, data, target, walk)
        else:
            key, field = {
                'sei-dual-delay': ('dual-hrd', 'cpbRemovalDelay'),
                'sei-cross-tag': ('cross-tag', 'timingSelectionSource'),
                'sei-offset-epb': ('wide-epb', 'timeOffset.source[1]'),
                'sei-missing': ('unactivated-sps', None),
                'sei-truncated': ('truncated-picture', None),
                'sei-message-budget': ('message-budget', None),
            }[name]
            oracle = expected['seiTiming'][key]
            tag, flags = oracle['tagId'], ['--field-name', field] if field else []
            def semantics(_name, _snapshot, _expected):
                return verify_sei(directory, oracle, data, field)
        source = args.input_dir / oracle['file']
        data = source.read_bytes()
        commands.run(name, [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py',
                     '--binary', args.binary, '--source', source, '--tag', str(tag), '--output-dir', directory,
                     '--timeout-seconds', str(args.timeout_seconds), *flags], timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, {}, semantic_check=semantics)
        require(row['binarySHA256'] == binary_hash, 'native executable changed')
        cases.append(row)
        save(args.output_dir, 'cases.json', cases)
    require(digest(args.binary) == binary_hash, 'native executable changed during run')
    load_expectations(args.input_dir)
    return {'passed': True, 'rounds': len(cases), 'binarySHA256': binary_hash,
            'expectationsSHA256': digest(args.input_dir / 'expectations.json'), 'cases': cases,
            'scope': 'Real GTK/WebKit actions and source evidence; native elapsed includes automation/export overhead.'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--case', action='append', choices=CASES)
    parser.add_argument('--timeout-seconds', type=int, default=900)
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
