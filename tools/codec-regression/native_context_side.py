#!/usr/bin/env python3
"""Four isolated native cases: MP3 side-info and resumable AVC parameter replay."""

import argparse
import copy
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, BFRAMES, Commands, digest, executable, flv_tags, load_expectations, new_directory, require, save
from native import verify_case
from native_v3 import child_fields, compare_prefix, first_b_tag, SLICE_FIELDS
from side_info_oracle import source_field
from verify_side_info import BOOLEAN_FIELDS, CHANNEL_FIELDS, expected_dependency, expected_diagnostics


def native_span(span):
    return {**span, 'byteOffset': str(span['byteOffset'])}


def mp3_fields(fields, root):
    result = {}
    direct = child_fields(fields, root['id'])
    for name, path in (('mainDataBegin', 'main_data_begin'), ('privateBits', 'private_bits')):
        if name in direct:
            result[path] = direct[name]
    for name, row in direct.items():
        if name.startswith('SCFSI channel '):
            channel = int(name.removeprefix('SCFSI channel '))
            for field in child_fields(fields, row['id']).values():
                result[f'scfsi[{channel}]' + field['name'].removeprefix('scfsi')] = field
        if name.startswith('Granule '):
            granule = int(name.removeprefix('Granule '))
            for chname, channel in child_fields(fields, row['id']).items():
                prefix = f'granules[{granule}].channels[{int(chname.removeprefix("Channel "))}].'
                for field in child_fields(fields, channel['id']).values():
                    name = field['name']
                    path = CHANNEL_FIELDS.get(name, name.replace('tableSelect', 'table_select').replace('subblockGain', 'subblock_gain'))
                    result[prefix + path] = field
    return result


def verify_mp3(name, snapshot, expected):
    case, source = expected['case'], expected['source']
    detail, count = snapshot['detail']['detail'], 0
    fields = detail['fields']
    codes = expected_diagnostics(case, 3)
    require(sorted((d['code'], int(d['offset'])) for d in detail['diagnostics']) == sorted(codes), 'native MP3 diagnostics differ')
    omitted = max(0, case['physicalFrameCount'] - 2)
    require(detail['complete'] == (not codes) and detail['truncated'] == (bool(omitted) or case['completeFrameCount'] != case['physicalFrameCount']), 'native MP3 completion/omission differs')
    markers = [f for f in fields if f['name'] == 'sideInfoOmittedFrames']
    require(len(markers) == bool(omitted), 'native MP3 omission marker missing/invented')
    if omitted:
        require(markers[0]['value'] == f'{omitted}; desktop expands the first two side-info records; CLI v3 retains all inspected records', 'native MP3 omitted frame count differs')
    roots = [f for f in fields if f['name'].startswith('Side info Frame ')]
    require(len(roots) == min(2, case['physicalFrameCount']), 'native side-info preview count differs')
    for index, root in enumerate(roots):
        require(root['name'] == f'Side info Frame {index}', 'native side-info frame order differs')
        want = case['frames'][index]['sideInfo']
        consumed = want['bitsConsumed'] if want['syntaxComplete'] else want['availableBytes'] * 8
        require(root['value'] == f'complete={str(want["valid"]).lower()}; {consumed} of {want["expectedBytes"] * 8} bits inspected' and root['span'] == native_span(want['span']), 'native side-info summary/source differs')
        actual = mp3_fields(fields, root)
        require(set(actual) == {f['path'] for f in want['fields']}, 'native encoded field presence differs')
        for oracle in want['fields']:
            path, field = oracle['path'], actual[oracle['path']]
            is_bool = path.startswith('scfsi[') or path.rsplit('.', 1)[-1] in BOOLEAN_FIELDS
            value = str(bool(oracle['value'])).lower() if is_bool else str(oracle['value'])
            require(field['value'] == value and field['span'] == native_span(oracle['span']), 'native field value/source differs: ' + path)
            source_field(source, oracle)
            count += 1
    frames = [f for f in fields if f['type'] == 'MPEG frame']
    require(len(frames) == case['physicalFrameCount'], 'native basic frame summary omitted')
    for root, oracle in zip(frames, case['frames']):
        require(root['name'] == f'Frame {oracle["index"]}' and root['span'] == native_span({'byteOffset': oracle['sourceOffset'], 'bitOffset': 0, 'bitLength': oracle['availableLength'] * 8}), 'native frame identity/source differs')
        child = child_fields(fields, root['id'])
        require(child['CRC16']['value'].startswith(oracle['crc']['status'] + ' '), 'native frame CRC summary differs')
        dep = expected_dependency(oracle)
        value = f'{dep["state"]}; prior same-tag slot bytes={dep["knownPreviousBytes"]}'
        if 'missingPreviousBytes' in dep:
            value += f'; bytes before tag={dep["missingPreviousBytes"]}'
        if 'part23Bits' in dep:
            value += f'; declared bits={dep["part23Bits"]}, same-tag window bits={dep["availableBits"]}'
        require(child['mainDataDependency']['value'] == value + '; not reservoir or decoded-audio validation', 'native dependency overstates source coverage')
    return {'fields': len(fields), 'sourceMappedFieldsCompared': count, 'detailComplete': detail['complete'], 'sideInfoOmittedFrames': omitted}


def long_fixture(input_dir, expected, directory):
    original = (input_dir / BFRAMES).read_bytes()
    records, source_tag = flv_tags(original), first_b_tag(expected)
    config_tag = expected['h264BFrames']['configurationTagId']
    payload = lambda tag: original[records[tag]['payloadOffset']:records[tag]['payloadEnd']]
    data = bytearray(b'FLV\x01\x01\x00\x00\x00\x09' + bytes(4))
    selected, revisit = 20002, 8193
    mapped = {}
    for tag in range(selected + 1):
        kind, body, origin = (9, payload(config_tag), config_tag) if tag == 0 else (9, payload(source_tag), source_tag) if tag in (revisit, selected) else (18, b'\x02\0\0', None)
        offset = len(data) + 11
        data += bytes([kind]) + len(body).to_bytes(3, 'big') + bytes(7) + body + (11 + len(body)).to_bytes(4, 'big')
        if origin is not None:
            delta = offset - records[origin]['payloadOffset']
            blocks = copy.deepcopy([b for b in expected['h264BFrames']['traceBlocks'] if b['tagId'] == origin])
            for block in blocks:
                block['tagId'], block['offset'] = tag, block['offset'] + delta
                for field in block['fields']:
                    for span in field['sourceBitSegments']:
                        span['byteOffset'] += delta
            mapped[str(tag)] = blocks
    path = directory / 'long-parameter-context.flv'
    path.write_bytes(data)
    oracle = {'file': path.name, 'sourceSHA256': digest(path), 'selectedTagId': selected, 'revisitTagId': revisit, 'tagCount': selected + 1,
              'stages': 3, 'sourceTraceSHA256': digest(input_dir / 'expectations.json'), 'traceBlocksByTag': mapped,
              'scope': 'Container-valid structural long-file fixture assembled from real AVC config and B/multi-slice source bytes; no claim of a decodable long movie.'}
    save(directory, 'long-context-expectations.json', oracle)
    return path, oracle


def context(snapshot):
    fields = snapshot['detail']['detail']['fields']
    root = next(f for f in fields if f['name'] == 'Parameter context')
    result = {f['name']: f['value'] for f in child_fields(fields, root['id']).values()}
    return {'state': root['value'], **result}


def verify_long(name, snapshot, oracle):
    detail = snapshot['detail']['detail']
    require(detail['complete'] and not detail['truncated'] and not detail['diagnostics'], 'native continued AVC detail incomplete')
    fields, compared = detail['fields'], 0
    tag = int(snapshot['selectedTag']['id'])
    traces = [b for b in oracle['traceBlocksByTag'][str(tag)] if b['kind'] == 'Slice Header']
    require(len(traces) >= 2, 'long-file fixture must retain multi-slice B frame')
    roots = [f for f in fields if f['name'].startswith('Slice prefix NAL[')]
    require(len(roots) == len(traces), 'native long-file slice count differs')
    for trace in traces:
        root = next(f for f in roots if f['name'] == f'Slice prefix NAL[{trace["nalIndex"]}]')
        children = child_fields(fields, root['id'])
        compared += compare_prefix(children, trace, SLICE_FIELDS)
        require(children['sliceTypeName']['value'] == 'B', 'long-file slice type differs')
        for name, kind in (('referencedSPS', 'Sequence Parameter Set'), ('referencedPPS', 'Picture Parameter Set')):
            parameter = next(b for b in oracle['traceBlocksByTag']['0'] if b['kind'] == kind)
            require(children[name]['span'] == native_span({'byteOffset': parameter['offset'], 'bitOffset': 0, 'bitLength': parameter['bytes'] * 8}), 'long-file parameter source reference differs')
    return {'fields': len(fields), 'detailComplete': True, 'sourceMappedFieldsCompared': compared}


def verify_stages(directory, oracle):
    rows, previous = [], 0
    for index in range(oracle['stages']):
        name = 'context-initial.json' if index == 0 else f'context-{index}.json'
        snapshot = json.loads((directory / name).read_text())
        got = context(snapshot)
        start, next_tag = int(got['contextSegmentStartTagId']), int(got['nextTagId'])
        require(start == previous and start < next_tag <= oracle['selectedTagId'], 'context replay restarted or failed to advance')
        require(int(got['replayedTags']) == next_tag - start <= 8192 and int(got['checkpointCount']) <= 8, 'context replay budget/cache differs')
        require(int(got['checkpointCount']) == index + 1, 'completed segment checkpoint was not retained')
        require(got['selectedTagId'] == str(oracle['selectedTagId']), 'context target changed')
        if index + 1 < oracle['stages']:
            require(got['state'] == 'budget_exhausted' and got['contextCanContinue'] == 'true' and snapshot['detail']['detail']['truncated'], 'partial context did not expose continuation')
            require(got['availableSPS'] == got['availablePPS'] == '0', 'partial context published parameter state prematurely')
        else:
            require(got['state'] == 'found' and got['contextCanContinue'] == 'false', 'final replay was not resolved')
            verify_long('continued', snapshot, oracle)
        rows.append({'file': name, **got})
        previous = next_tag
    revisit = json.loads((directory / 'context-revisit.json').read_text())
    verify_long('revisit', revisit, oracle)
    got = context(revisit)
    require(got['state'] == 'found' and int(got['contextSegmentStartTagId']) == 8192 and int(got['replayedTags']) == oracle['revisitTagId'] - 8192, 'earlier random jump failed to reuse nearest checkpoint')
    final = json.loads((directory / 'snapshot.json').read_text())
    got = context(final)
    require(got['state'] == 'found' and int(got['contextSegmentStartTagId']) == oracle['selectedTagId'] and got['replayedTags'] == '0' and got['contextBytesRead'] == '2', 'repeat selected Tag did more than the selected two-byte eligibility probe')
    return rows


def run(args, commands):
    expected, side = load_expectations(args.input_dir), load_expectations(args.side_info_dir)
    binary_hash, rows = digest(args.binary), []
    for name in ('mpeg1-stereo-native-prefix', 'mpeg25-mono', 'side-truncated'):
        case = side['cases'][name]
        source, directory = args.side_info_dir / case['file'], args.output_dir / name
        commands.run(name, [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                           '--source', source, '--tag', '0', '--output-dir', directory, '--timeout-seconds', str(args.timeout_seconds)], timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, {'case': case, 'source': source.read_bytes()}, semantic_check=verify_mp3)
        require(row['binarySHA256'] == binary_hash, 'native MP3 used a different binary')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    source, oracle = long_fixture(args.input_dir, expected, args.output_dir)
    directory = args.output_dir / 'long-context'
    commands.run('long-context', [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                 '--source', source, '--tag', str(oracle['selectedTagId']), '--continue-context', '2', '--revisit-tag', str(oracle['revisitTagId']),
                 '--output-dir', directory, '--timeout-seconds', str(args.timeout_seconds)], timeout=args.timeout_seconds + 30)
    row = verify_case(directory, 'long-context', oracle, semantic_check=verify_long)
    require(row['binarySHA256'] == binary_hash, 'native context used a different binary')
    row['stages'] = verify_stages(directory, oracle)
    require(digest(source) == oracle['sourceSHA256'], 'long fixture changed during acceptance')
    rows.append(row)
    save(args.output_dir, 'cases.json', rows)
    load_expectations(args.input_dir)
    load_expectations(args.side_info_dir)
    require(digest(args.binary) == binary_hash, 'production binary changed during native acceptance')
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash, 'cases': rows}


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True, help='original generate.py codec samples')
    parser.add_argument('--side-info-dir', type=Path, required=True, help='side_info_oracle.py samples')
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--timeout-seconds', type=int, default=240)
    args = parser.parse_args(argv)
    args.input_dir, args.side_info_dir = args.input_dir.resolve(), args.side_info_dir.resolve()
    for path in (args.input_dir, args.side_info_dir):
        if not (path / 'expectations.json').is_file():
            parser.error('sample directory must contain expectations.json')
    if not 30 <= args.timeout_seconds <= 600:
        parser.error('--timeout-seconds must be between 30 and 600')
    return args


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)), 'binary': str(args.binary),
                                         'input': str(args.input_dir), 'sideInfo': str(args.side_info_dir)})
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
