#!/usr/bin/env python3
"""Run six real Linux desktop codec cases through the isolated native smoke tool."""

import argparse
import json
from pathlib import Path
import sys
import traceback

from generate import (ROOT, CRC, H264, Commands, digest, executable, load_expectations,
                      new_directory, require, save)
from verify import normalized


def field_value(fields, name):
    values = [field['value'] for field in fields if normalized(field['name']) == normalized(name)]
    require(len(values) == 1, f'expected one native field {name}, found {len(values)}')
    return values[0]


def verify_semantics(name, snapshot, expected):
    detail = snapshot['detail']['detail']
    fields = detail['fields']
    if name == 'h264':
        require(detail['complete'] and not detail['truncated'], 'native VUI is incomplete')
        for key, field in expected['h264']['vuiFieldsFromFFmpegTrace'].items():
            if key == 'vui_parameters_present_flag':
                continue  # Desktop calls this generic node "present".
            got = field_value(fields, key)
            require(got == str(field['value']) or got in ('true', 'false') and field['value'] in (0, 1) and (got == 'true') == bool(field['value']),
                    f'native VUI value differs: {key}')
        for key in ('vuiComplete', 'spsTrailingBitsValid'):
            require(field_value(fields, key) == 'true', f'native {key} differs')
    elif name in ('xing-vbr', 'info-cbr', 'xing-vbr-tail-truncated', 'crc'):
        want = expected['mp3'][name]
        stop = 'body_truncated' if name == 'xing-vbr-tail-truncated' else 'diagnostics' if name == 'crc' else 'end'
        prefix = f'{want["completeFrameCount"]} complete frame boundaries; {want["physicalFrameCount"]} headers; stop={stop};'
        require(field_value(fields, 'Frame inspection').startswith(prefix), 'native MP3 frame summary differs')
        omitted = max(0, want['physicalFrameCount'] - 2)
        require(detail['complete'] == (name not in ('xing-vbr-tail-truncated', 'crc')) and
                detail['truncated'] == (name == 'xing-vbr-tail-truncated' or omitted > 0), 'native MP3 complete/truncated distinction differs')
        omission = [f for f in fields if f['name'] == 'sideInfoOmittedFrames']
        require(len(omission) == bool(omitted), 'native side-info omission marker differs')
        if omitted:
            require(omission[0]['value'] == f'{omitted}; desktop expands the first two side-info records; CLI v3 retains all inspected records',
                    'native side-info omission count/reason differs')
        require(len([f for f in fields if f['name'].startswith('Side info Frame ')]) == min(2, want['physicalFrameCount']),
                'native side-info preview count differs')
        if want['vbr'] is not None:
            require('complete=true' in field_value(fields, want['vbr']['kind']), 'native VBR is incomplete')
            require(field_value(fields, 'declaredStreamFrames') == str(want['vbr']['declaredFrames']['value']), 'native VBR declaration differs')
        crcs = [field for field in fields if field['name'] == 'CRC16']
        require(len(crcs) == want['physicalFrameCount'], 'native CRC entry count differs')
        for got, frame in zip(crcs, want['frames']):
            require(got['value'].startswith(frame['crc']['status'] + ' '), 'native CRC status differs')
            if 'stored' in frame['crc']:
                crc = frame['crc']
                require(f'stored=0x{crc["stored"]:04x} computed=0x{crc["computed"]:04x}' in got['value'], 'native CRC values differ')
                require(got['span'] == {'byteOffset': str(frame['sourceOffset'] + 4), 'bitOffset': 0, 'bitLength': 16}, 'native CRC source location differs')
        if name == 'xing-vbr-tail-truncated':
            tail = want['frames'][-1]
            require(field_value(fields, 'uninspectedFrames').startswith(f'{tail["availableLength"]} bytes'), 'native remainder differs')
            require(any(d['code'] == 'MP3_FRAME_BODY_TRUNCATED' and d['offset'] == str(tail['sourceOffset'])
                        for d in detail['diagnostics']), 'native truncation diagnostic missing')
        elif name == 'crc':
            crc = want['frames'][1]['crc']
            require(any(d['code'] == 'MP3_CRC_MISMATCH' and d['expected'] == str(crc['computed']) and d['actual'] == str(crc['stored'])
                        for d in detail['diagnostics']), 'native CRC mismatch diagnostic missing')
    else:
        require(name == 'default' and detail['complete'], 'default native detail failed')
    return {'fields': len(fields), 'detailComplete': detail['complete']}


def verify_lifecycle(result, native):
    require(result['passed'] and not result['remainingOwnedProcesses'], 'native cleanup/acceptance failed')
    require(native['passed'] and native['exitCode'] == 0 and native['sourceUnchanged'] and native['sourceFDsBeforeClose'] and
            not native['sourceFDsAfterClose'] and not native['remainingAppProcesses'], 'native lifecycle failed')
    app_processes = {(p['pid'], p['startTicks']) for p in native['observedAppProcesses']}
    require(any(pid == native['pid'] for pid, _ in app_processes), 'application process identity was not observed')
    require(all((p['pid'], p['startTicks']) not in app_processes for p in result['forcedCleanup']),
            'app process required forced cleanup')


def verify_case(directory, name, expected, semantic_check=verify_semantics):
    result = json.loads((directory / 'result.json').read_text())
    native = json.loads((directory / 'native-result.json').read_text())
    run = json.loads((directory / 'run.json').read_text())
    snapshot = json.loads((directory / 'snapshot.json').read_text())
    verify_lifecycle(result, native)
    require(native['snapshotCheck']['schemaValid'] and native['snapshotCheck']['hex']['matchesSource'], 'native export/Hex failed')
    return {'case': name, 'passed': True, 'seconds': result['seconds'], 'binarySHA256': run['binarySHA256'],
            'snapshotSHA256': native['snapshotCheck']['sha256'], 'sourceSHA256': run['sourceSHA256'],
            'harnessCleanupSignals': result['forcedCleanup'],
            'sourceFDReleased': True, 'normalExit': True, **semantic_check(name, snapshot, expected)}


def native(args, commands):
    expected = load_expectations(args.input_dir)
    binary_hash = digest(args.binary)
    rows = []
    cases = [('default', None, 4), ('h264', H264, expected['h264']['configurationTagId']),
             ('xing-vbr', 'xing-vbr-single-tag.flv', 0), ('info-cbr', 'info-cbr-single-tag.flv', 0),
             ('xing-vbr-tail-truncated', 'xing-vbr-tail-truncated-single-tag.flv', 0), ('crc', CRC, 0)]
    for name, filename, tag in cases:
        directory = args.output_dir / name
        argv = [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                '--output-dir', directory, '--tag', str(tag), '--timeout-seconds', str(args.timeout_seconds)]
        if filename:
            argv.extend(['--source', args.input_dir / filename])
        # linux_smoke owns Xvfb/D-Bus, memory-only GSettings, process cleanup and
        # its timeout. Give that cleanup time before the outer command deadline.
        commands.run(name, argv, timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, expected)
        require(row['binarySHA256'] == binary_hash, 'native case used a different production binary')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash, 'production binary changed during acceptance')
    load_expectations(args.input_dir)
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash,
            'expectationsSHA256': digest(args.input_dir / 'expectations.json'), 'cases': rows,
            'checks': 'Real GTK open/save, codec semantics, source-linked Hex, schema-valid export, unchanged source, FD release, normal exit and process cleanup.'}


def arguments(argv=None, description=__doc__):
    parser = argparse.ArgumentParser(description=description)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--timeout-seconds', type=int, default=180, help='per native case, 30 through 600')
    args = parser.parse_args(argv)
    args.input_dir = args.input_dir.resolve()
    if not (args.input_dir / 'expectations.json').is_file():
        parser.error('--input-dir must contain generated expectations.json')
    if not 30 <= args.timeout_seconds <= 600:
        parser.error('--timeout-seconds must be between 30 and 600')
    return args


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'binary': str(args.binary), 'binarySHA256': digest(args.binary),
                                         'input': str(args.input_dir), 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
                                         'scriptSHA256': digest(Path(__file__))})
        result = native(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
