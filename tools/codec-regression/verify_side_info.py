#!/usr/bin/env python3
"""Check MP3 v3 side information against independently parsed original bits."""

import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, Commands, digest, load_expectations, require, save
from side_info_oracle import source_field
from verify import arguments


CHANNEL_FIELDS = {
    'part23Length': 'part2_3_length', 'bigValues': 'big_values', 'globalGain': 'global_gain',
    'scalefacCompress': 'scalefac_compress', 'windowSwitchingFlag': 'window_switching_flag',
    'blockType': 'block_type', 'mixedBlockFlag': 'mixed_block_flag', 'region0Count': 'region0_count',
    'region1Count': 'region1_count', 'preflag': 'preflag', 'scalefacScale': 'scalefac_scale',
    'count1TableSelect': 'count1table_select',
}
BOOLEAN_FIELDS = {'window_switching_flag', 'mixed_block_flag', 'preflag', 'scalefac_scale', 'count1table_select'}


def flatten_side_info(side):
    fields = {}
    for name, path in (('mainDataBegin', 'main_data_begin'), ('privateBits', 'private_bits')):
        if name in side:
            fields[path] = side[name]
    for channel, row in enumerate(side['scfsiChannels']):
        require(row['channel'] == channel, 'SCFSI channel order differs')
        for band, flag in enumerate(row['flags']):
            fields[f'scfsi[{channel}][{band}]'] = flag
    for granule, row in enumerate(side['granules']):
        require(row['index'] == granule, 'granule order differs')
        for channel, unit in enumerate(row['channels']):
            require(unit['index'] == channel, 'granule channel order differs')
            prefix = f'granules[{granule}].channels[{channel}].'
            for name, path in CHANNEL_FIELDS.items():
                if name in unit:
                    fields[prefix + path] = unit[name]
            for name, path in (('tableSelect', 'table_select'), ('subblockGain', 'subblock_gain')):
                for index, field in enumerate(unit[name]):
                    fields[prefix + f'{path}[{index}]'] = field
    return fields


def compare_side_info(got, want, source):
    require(got['span'] == want['span'] and got['declaredBytes'] == want['expectedBytes'], 'side-info source extent differs')
    # The product budget includes the available tail bits of an incomplete
    # field; the oracle only consumes complete encoded fields.
    consumed = want['bitsConsumed'] if want['syntaxComplete'] else want['availableBytes'] * 8
    require(got['bitsConsumed'] == consumed and got['complete'] == want['valid'], 'side-info completeness/budget differs')
    require([len(g['channels']) for g in got['granules']] == [len(g) for g in want['granules']], 'granule/channel shape differs')
    actual = flatten_side_info(got)
    expected = {f['path']: f for f in want['fields']}
    require(set(actual) == set(expected), 'encoded field presence differs: ' + str(set(actual) ^ set(expected)))
    for path, field in actual.items():
        oracle = expected[path]
        is_bool = path.startswith('scfsi[') or path.rsplit('.', 1)[-1] in BOOLEAN_FIELDS
        value = bool(oracle['value']) if is_bool else oracle['value']
        require(type(field['value']) is type(value) and field['value'] == value, 'side-info value/type differs: ' + path)
        require(field['span'] == oracle['span'], 'side-info source mapping differs: ' + path)
        source_field(source, oracle)
    return len(actual)


def expected_dependency(frame):
    dep, area = frame['dependency'], frame['mainDataArea']
    result = {'state': 'unknown', 'mainDataSpan': {'byteOffset': area['byteOffset'], 'bitOffset': 0, 'bitLength': area['availableBytes'] * 8},
              'knownPreviousBytes': dep['priorMainDataSlotBytes']}
    if 'backBytes' not in dep:
        return result
    back, known = dep['backBytes'], dep['priorMainDataSlotBytes']
    result.update(part23Bits=dep['declaredDataBits'], availableBits=(min(back, known) + area['availableBytes']) * 8)
    if back == 0:
        result['state'] = 'none'
    elif back <= known:
        result['state'] = 'within_tag'
    elif dep['historyOrigin'] == 'tag_start':
        result.update(state='before_tag', missingPreviousBytes=back - known)
    if not dep['declaredBitsFit']:
        result['state'] = 'unknown'
    return result


def expected_diagnostics(case, version):
    result = []
    for frame in case['frames']:
        if not frame['complete']:
            result.append(('MP3_FRAME_BODY_TRUNCATED', frame['sourceOffset']))
        if frame['crc']['status'] == 'mismatch':
            result.append(('MP3_CRC_MISMATCH', frame['sourceOffset'] + 4))
        if version < 3:
            continue
        side = frame['sideInfo']
        fields = {f['path']: f for f in side['fields']}
        for issue in side['issues']:
            if issue['kind'] == 'side_info_truncated':
                result.append(('MP3_SIDE_INFO_TRUNCATED', side['span']['byteOffset'] + side['availableBytes']))
            else:
                result.append(('MP3_SIDE_INFO_VALUE_INVALID', fields[issue['path']]['span']['byteOffset']))
        if frame['dependency'].get('declaredBitsFit') is False:
            result.append(('MP3_MAIN_DATA_LENGTH_INVALID', fields['main_data_begin']['span']['byteOffset']))
    return result if version > 1 else []


def compare_document(document, case, source, version):
    require(document['schemaVersion'] == version, 'wrong schema version')
    detail = document['detail']
    codes = expected_diagnostics(case, version)
    require(sorted((d['code'], d['offset']) for d in document['diagnostics']) == sorted(codes), 'MP3 diagnostic code/source differs')
    require(detail['complete'] == (not codes), 'MP3 detail completeness differs')
    if version == 1:
        require('frames' not in detail, 'legacy v1 unexpectedly exposes frame details')
        return 0
    frames = detail['frames']
    require(len(frames['items']) == case['physicalFrameCount'] and frames['completeFrames'] == case['completeFrameCount'], 'MP3 frame counts differ')
    require(frames['complete'] == (not codes), 'MP3 frame inspection completeness differs')
    compared = 0
    for actual, want in zip(frames['items'], case['frames']):
        require(actual['index'] == want['index'] and actual['span'] == {'byteOffset': want['sourceOffset'], 'bitOffset': 0, 'bitLength': want['availableLength'] * 8}, 'MP3 frame extent differs')
        require(actual['boundaryComplete'] == want['complete'] and actual['header']['frameLengthBytes'] == want['length'] and
                actual['header']['samplingFrequencyHz'] == want['sampleRateHz'] and actual['header']['bitrateKbps'] * 1000 == want['bitrateBps'], 'MP3 header differs')
        for name, value in want['crc'].items():
            require(actual['crc'][name] == value, 'MP3 CRC differs: ' + name)
        if version == 2:
            require('sideInfo' not in actual and 'dependency' not in actual, 'legacy v2 unexpectedly expanded')
        else:
            compared += compare_side_info(actual['sideInfo'], want['sideInfo'], source)
            require(actual['dependency'] == expected_dependency(want), 'MP3 dependency coverage differs at frame ' + str(want['index']))
    return compared


def verify(args, commands):
    from jsonschema import Draft202012Validator
    expected = load_expectations(args.input_dir)
    cli_hash = digest(args.cli)
    schemas = {v: ROOT / f'docs/schemas/flv-mp3-tag-detail-v{v}.schema.json' for v in (1, 2, 3)}
    validators = {v: Draft202012Validator(json.loads(path.read_text())) for v, path in schemas.items()}
    compared, rows = 0, []
    for name, case in expected['cases'].items():
        source = args.input_dir / case['file']
        data, documents = source.read_bytes(), {}
        for version in (1, 2, 3):
            output = commands.run(name + f'.v{version}', [args.cli, 'detail', '--schema-version', str(version), '--tag', str(case['tagId']), source],
                                  expected=2 if expected_diagnostics(case, version) else 0)
            doc = json.loads(output.read_text())
            validators[version].validate(doc)
            compared += compare_document(doc, case, data, version)
            documents[version] = doc
        output = commands.run(name + '.default', [args.cli, 'detail', '--tag', str(case['tagId']), source])
        require(json.loads(output.read_text()) == documents[1], 'default CLI changed from v1')
        rows.append({'case': name, 'frames': case['physicalFrameCount'], 'versions': [1, 2, 3], 'defaultEqualsV1': True})
        save(args.output_dir, 'cases.json', rows)
    load_expectations(args.input_dir)
    require(digest(args.cli) == cli_hash, 'CLI changed during verification')
    return {'passed': True, 'cliSHA256': cli_hash, 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
            'sourceMappedFieldsCompared': compared, 'cases': rows,
            'schemaSHA256': {str(v): digest(path) for v, path in schemas.items()},
            'scope': 'Encoded side-info source bits and same-Tag physical slot coverage; no audio or reservoir decoding.'}


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)), 'input': str(args.input_dir), 'cli': str(args.cli)})
        result = verify(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
