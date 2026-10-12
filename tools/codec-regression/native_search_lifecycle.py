#!/usr/bin/env python3
"""One real Linux cancel/close round against an explicitly controlled read gate.

Build the separate outside-repository gate first with avc_search_gate.py. This
runner never presents that binary as production acceptance; it independently
checks native evidence, receipt ordering, original bytes and recovered v3 pages.
"""

import argparse
from datetime import datetime
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, Commands, digest, load_expectations, new_directory, require, save
from native import verify_case
from native_search_history import read_ui, verify_search

sys.path.insert(0, str(ROOT / 'tools/desktop-native'))
from avc_search_gate import receipt, validate_build


def verify_gate(directory, gate_dir, action):
    native = json.loads((directory / 'native-result.json').read_text())
    stage = native['avcSearchGate']
    plan = json.loads((gate_dir / 'build-plan.json').read_text())
    build = json.loads((gate_dir / 'build-result.json').read_text())
    run = json.loads((directory / 'run.json').read_text())
    validate_build(gate_dir, Path(run['binary']))
    require(stage['action'] == action and run['avcSearchAction'] == action and run['avcSearchGate'] == str(gate_dir), 'native gate action/directory differs')
    require(run['binarySHA256'] == build['binarySHA256'] and run['sourceSHA256'] == plan['sourceSHA256'], 'controlled build/source differs')
    entered = receipt(gate_dir, 'entered.json', native['pid'])
    canceled = receipt(gate_dir, 'context-canceled.json', native['pid'])
    released = receipt(gate_dir, 'released.json', native['pid'])
    require(stage['entered'] == entered and stage['canceled'] == canceled and stage['released'] == released, 'saved gate receipts differ from original records')
    require(canceled['contextError'] == released['contextError'] == 'context canceled' and not (gate_dir / 'timed-out.json').exists(), 'timeout is not cancellation')
    times = [datetime.fromisoformat(r['timeUTC'].replace('Z', '+00:00')) for r in (entered, canceled, released)]
    require(times[0] < times[1] < times[2] and 0 < entered['bytes'] <= 1048576 and entered['offset'] >= 0, 'read gate/order is not observable')
    require(stage['fdsBeforeAction'] and stage['fdsBeforeRelease'], 'source ownership disappeared during blocked read')
    for kind in ('pending', 'command', 'terminal'):
        read_ui(directory, {'uiFile': stage[kind + 'UI'], 'uiSHA256': stage[kind + 'UISHA256']})
    pending = read_ui(directory, {'uiFile': stage['pendingUI'], 'uiSHA256': stage['pendingUISHA256']})
    require(any('正在从 Tag 0 检查本批证据' in row['text'] for row in pending), 'running search was not observed before native action')
    command = read_ui(directory, {'uiFile': stage['commandUI'], 'uiSHA256': stage['commandUISHA256']})
    phrase = '正在确认 H.264 检索取消' if action == 'cancel' else '正在释放当前文件；底层 I/O 结束后才能打开下一个文件。'
    require(any(phrase in row['text'] for row in command), 'native cancellation/closing state was not observed')
    if action == 'cancel':
        checked = stage['canceledSnapshot']
        path = directory / 'avc-gate-canceled-snapshot.json'
        require(checked['schemaValid'] and checked['sha256'] == digest(path), 'canceled snapshot identity differs')
        snapshot = json.loads(path.read_text())
        require(snapshot['schemaVersion'] == 3 and snapshot['avcSearchPage'] is None and
                any(r['section'] == 'avcSearchPage' for r in snapshot['omitted']), 'canceled snapshot invented a completed page')
    else:
        require(stage['fdsAfterGatedClose'] == [] and stage['openDisabledBeforeRelease'], 'gated close admitted another source or failed to release before reopening')
    return {'action': action, 'contextError': canceled['contextError'], 'gateSeconds': (times[2] - times[0]).total_seconds(),
            'acceptanceOnly': True, 'actualRead': {'offset': entered['offset'], 'bytes': entered['bytes']},
            'workerDeadlineSeconds': plan['searchDeadlineSeconds'], 'safetyGateSeconds': plan['gateSeconds'],
            'releaseAfterObservedCommand': True, 'blockedSourceRetained': True, 'recovered': True}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--gate-dir', type=Path, required=True)
    parser.add_argument('--action', choices=('cancel', 'close'), required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--timeout-seconds', type=int, default=420)
    args = parser.parse_args(argv)
    if not 60 <= args.timeout_seconds <= 600:
        parser.error('--timeout-seconds must be 60 through 600')
    args.gate_dir = args.gate_dir.resolve()
    args.output_dir.mkdir(parents=True)
    try:
        expected = load_expectations(args.input_dir)
        case = expected['avcCases']['declarations']
        plan = json.loads((args.gate_dir / 'build-plan.json').read_text())
        build = json.loads((args.gate_dir / 'build-result.json').read_text())
        binary, source_path = Path(build['binary']), Path(plan['source'])
        validate_build(args.gate_dir, binary)
        source = source_path.read_bytes()
        require(digest(source_path) == expected['sources'][case['file']]['sha256'], 'controlled copied fixture differs from independent oracle')
        directory = args.output_dir / args.action
        save(args.output_dir, 'run.json', {'acceptanceOnly': True, 'gate': str(args.gate_dir), 'binarySHA256': digest(binary),
                                         'scriptSHA256': digest(Path(__file__)), 'action': args.action})
        argv = [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', binary, '--source', source_path,
                '--output-dir', directory, '--tag', str(case['rows'][0]['tagId']), '--avc-search', 'mismatches',
                '--avc-search-pages', '16', '--avc-search-gate', args.gate_dir, '--avc-search-action', args.action,
                '--timeout-seconds', str(args.timeout_seconds)]
        Commands(args.output_dir).run('native-' + args.action, argv, timeout=args.timeout_seconds + 30)
        row = verify_case(directory, args.action, expected, semantic_check=lambda *ignored: verify_search(directory, case, source, 'mismatches'))
        result = {'passed': True, 'acceptanceOnly': True, 'round': row, 'gate': verify_gate(directory, args.gate_dir, args.action)}
    except Exception as error:
        result = {'passed': False, 'acceptanceOnly': True, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
