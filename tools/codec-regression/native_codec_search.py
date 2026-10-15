#!/usr/bin/env python3
"""Six isolated Linux codec search scenarios against independently authored bytes."""

import argparse
import json
from pathlib import Path
import sys
import traceback

from codec_search_verify import verify
from generate import ROOT, Commands, digest, executable, load_expectations, new_directory, require, save
from native import verify_case


CASES = ('mp3-late-change', 'mp3-bitrate-stops', 'sei-pagination', 'sei-cross-256', 'sei-missing', 'sei-budget')


def run(args):
    expected, commands, rows = load_expectations(args.input_dir), Commands(args.output_dir), []
    binary_hash = digest(args.binary)
    sys.path.insert(0, str(ROOT / 'tools/desktop-native'))
    for name in CASES:
        if args.case and name not in args.case:
            continue
        case = expected['codecSearch'][name]
        source, directory = args.input_dir / case['file'], args.output_dir / name
        command = [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                   '--source', source, '--tag', '0', '--codec-search', case['kind'], '--codec-search-target', case['sourceTarget'],
                   '--output-dir', directory, '--timeout-seconds', str(args.timeout_seconds)]
        for filter_name in case['filters']:
            command.extend(['--codec-search-filter', filter_name])
        for kind in case['sourceKinds']:
            command.extend(['--codec-search-source', kind])
        if 'selectedFrame' in case:
            command.append('--codec-search-frame')
        if name == 'sei-pagination':
            command.append('--codec-search-restart')
        commands.run(name, command, timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, {}, semantic_check=lambda *_: verify(directory, case, source.read_bytes()))
        require(row['binarySHA256'] == binary_hash, 'native binary changed')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash, 'native binary changed during run')
    load_expectations(args.input_dir)
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash,
            'expectationsSHA256': digest(args.input_dir / 'expectations.json'), 'cases': rows}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--timeout-seconds', type=int, default=900)
    parser.add_argument('--case', action='append', choices=CASES)
    args = parser.parse_args(argv)
    if not 30 <= args.timeout_seconds <= 1800:
        parser.error('timeout must be 30 through 1800 seconds')
    args.input_dir = args.input_dir.resolve()
    args.output_dir.mkdir(parents=True)
    save(args.output_dir, 'run.json', {'binary': str(args.binary), 'binarySHA256': digest(args.binary),
                                      'scriptSHA256': digest(Path(__file__)), 'cases': args.case or list(CASES)})
    try:
        result = run(args)
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
