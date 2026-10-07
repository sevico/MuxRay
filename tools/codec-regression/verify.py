#!/usr/bin/env python3
"""Compare CLI v1/v2 results with generated independent codec expectations."""

import argparse
import json
from pathlib import Path
import re
import sys
import traceback

from generate import (ROOT, CRC, H264, MP3_NAMES, Commands, digest, executable,
                      load_expectations, new_directory, require, save)


def normalized(name):
    return re.sub(r'[^a-z0-9]', '', name.lower()).removesuffix('0') if name.endswith('[0]') else re.sub(r'[^a-z0-9]', '', name.lower())


def encoded_fields(value):
    fields = {}
    def visit(obj):
        if isinstance(obj, dict):
            for name, item in obj.items():
                if isinstance(item, dict) and 'value' in item and 'span' in item:
                    key = normalized(name)
                    require(key not in fields, f'ambiguous field in generated VUI fixture: {name}')
                    fields[key] = item
                else:
                    visit(item)
        elif isinstance(obj, list):
            for item in obj:
                visit(item)
    visit(value)
    return fields


def verify_h264(document, expected):
    detail = document['detail']
    sps = detail['avcDecoderConfigurationRecord']['sequenceParameterSets'][0]['sequenceParameterSet']
    vui = sps['vui']
    fields = encoded_fields(vui)
    fields[normalized('vui_parameters_present_flag')] = sps['vuiParametersPresentFlag']
    for name, want in expected['vuiFieldsFromFFmpegTrace'].items():
        got = fields[normalized(name)]
        require(got['value'] == want['value'], f'VUI value differs: {name}')
        span = got['span']
        require(span['rbspBitOffset'] + 8 == want['rbspBitStartIncludingNALHeader'] and
                span['bitLength'] == want['bitLength'] and span['sourceSpans'] == want['sourceBitSegments'],
                f'VUI original source bit mapping differs: {name}')
    require(detail['complete'] and vui['complete'] and vui['trailingBitsValid'] and
            vui['remainingRbspBits'] == 0 and not document['diagnostics'], 'VUI completion/diagnostics differ')


def verify_mp3(document, expected, truncated=False, crc=False):
    detail, frames = document['detail'], document['detail']['frames']
    require(len(frames['items']) == expected['physicalFrameCount'] and
            frames['completeFrames'] == expected['completeFrameCount'], 'MP3 frame counts differ')
    for got, want in zip(frames['items'], expected['frames']):
        require(got['index'] == want['index'] and got['span'] ==
                {'byteOffset': want['sourceOffset'], 'bitOffset': 0, 'bitLength': want['availableLength'] * 8}, 'MP3 frame source extent differs')
        require(got['boundaryComplete'] == want['complete'] and got['header']['frameLengthBytes'] == want['length'] and
                got['header']['bitrateKbps'] * 1000 == want['bitrateBps'] and
                got['header']['samplingFrequencyHz'] == want['sampleRateHz'], 'MP3 frame header differs')
        for name, value in want['crc'].items():
            require(got['crc'][name] == value, f'MP3 CRC {name} differs')
    if expected['vbr'] is not None:
        want, got = expected['vbr'], frames['vbr']
        require(got['kind'] == want['kind'] and got['flags'] == want['flags'] and got['complete'], 'VBR kind/flags/completeness differs')
        for name in ('declaredFrames', 'declaredBytes', 'quality'):
            if name in want:
                require(got[name] == want[name]['value'], f'VBR {name} differs')
        if 'toc' in want:
            require(got['tocSpan'] == {'byteOffset': want['toc']['offset'], 'bitOffset': 0, 'bitLength': 800}, 'Xing TOC source extent differs')
    require(frames['maxFrames'] == 64 and frames['maxBytes'] == 1 << 20, 'unexpected default frame budgets')
    require(frames['bytesRead'] < frames['span']['bitLength'] // 8, 'frame inspection unexpectedly read whole audio payload')
    if truncated:
        tail = expected['frames'][-1]
        require(not detail['complete'] and not frames['complete'] and frames['stopReason'] == 'body_truncated', 'truncated MP3 incorrectly complete')
        require(frames['bytesCovered'] == tail['offset'] and frames['uninspectedSpan'] ==
                {'byteOffset': tail['sourceOffset'], 'bitOffset': 0, 'bitLength': tail['availableLength'] * 8}, 'MP3 remainder differs')
        require(any(d['code'] == 'MP3_FRAME_BODY_TRUNCATED' and d['offset'] == tail['sourceOffset'] for d in document['diagnostics']), 'missing truncation diagnostic')
    elif crc:
        require(not detail['complete'] and not frames['complete'] and frames['stopReason'] == 'diagnostics' and
                frames['bytesCovered'] == expected['sourceBytes'], 'CRC mismatch completion/coverage differs')
        want = expected['frames'][1]
        require(any(d['code'] == 'MP3_CRC_MISMATCH' and d['offset'] == want['sourceOffset'] + 4 and
                    d['expected'] == want['crc']['computed'] and d['actual'] == want['crc']['stored']
                    for d in document['diagnostics']), 'missing CRC mismatch diagnostic')
    else:
        require(detail['complete'] and frames['complete'] and frames['bytesCovered'] == expected['sourceBytes'] and
                not document['diagnostics'], 'normal MP3 completion/coverage differs')


def cases(expected):
    return [('h264', H264, expected['h264']['configurationTagId'])] + [
        (name, name + '-single-tag.flv', 0) for name in MP3_NAMES] + [('crc', CRC, 0)]


def verify(args, commands):
    import jsonschema
    expected = load_expectations(args.input_dir)
    binary_hash = digest(args.cli)
    documents, schema_checks = {}, []
    for name, filename, tag in cases(expected):
        source = args.input_dir / filename
        for version in (1, 2):
            exit_code = 2 if version == 2 and name in ('xing-vbr-tail-truncated', 'crc') else 0
            path = commands.run(name + f'.v{version}', [args.cli, 'detail', '--schema-version', str(version), '--tag', str(tag), source], expected=exit_code)
            document = json.loads(path.read_text())
            documents[(name, version)] = document
            require(document['schemaVersion'] == version, 'wrong detail version')
            schema = ROOT / 'docs/schemas' / (f'flv-{"avc" if name == "h264" else "mp3"}-tag-detail-v{version}.schema.json')
            jsonschema.validate(document, json.loads(schema.read_text()))
            schema_checks.append({'case': name, 'version': version, 'schema': str(schema.relative_to(ROOT)), 'sha256': digest(schema)})
        path = commands.run(name + '.default', [args.cli, 'detail', '--tag', str(tag), source])
        require(json.loads(path.read_text()) == documents[(name, 1)], 'default CLI no longer equals explicit v1')
        old = documents[(name, 1)]['detail']
        if name == 'h264':
            require('vui' not in old['avcDecoderConfigurationRecord']['sequenceParameterSets'][0]['sequenceParameterSet'], 'v1 unexpectedly expanded VUI')
            verify_h264(documents[(name, 2)], expected['h264'])
        else:
            require('frames' not in old and old['complete'], 'v1 MP3 fixed-header behavior changed')
            verify_mp3(documents[(name, 2)], expected['mp3'][name], name == 'xing-vbr-tail-truncated', name == 'crc')
    require(digest(args.cli) == binary_hash, 'CLI executable changed during validation')
    load_expectations(args.input_dir)
    return {'passed': True, 'cliSHA256': binary_hash, 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
            'cases': len(cases(expected)), 'h264TraceFieldsCompared': len(expected['h264']['vuiFieldsFromFFmpegTrace']),
            'sourceBitMappingsCompared': True, 'defaultEqualsV1': True, 'schemaChecks': schema_checks,
            'limits': 'CLI codec evidence only; GUI checks are a separate native.py run. CRC input is structural, not decoded audio.'}


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--cli', type=executable, required=True, help='already built muxray CLI executable')
    parser.add_argument('--output-dir', type=new_directory, required=True)
    args = parser.parse_args(argv)
    args.input_dir = args.input_dir.resolve()
    if not (args.input_dir / 'expectations.json').is_file():
        parser.error('--input-dir must contain generated expectations.json')
    return args


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'cli': str(args.cli), 'cliSHA256': digest(args.cli),
                                         'input': str(args.input_dir), 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
                                         'scriptSHA256': digest(Path(__file__))})
        result = verify(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
