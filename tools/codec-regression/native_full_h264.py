#!/usr/bin/env python3
"""Fourteen isolated native PPS/full-slice header checks with authored bit evidence."""

import json
from pathlib import Path
import re
import sys
import traceback

from generate import ROOT, Commands, digest, load_expectations, require, save
from native import arguments, verify_case
from native_v3 import PPS_FIELDS, SLICE_FIELDS

CASES = [('pps-scaling', 'synthetic-i', 0)] + [
    (f'pps-fmo-{i}', f'synthetic-fmo-{i}', 0) for i in range(7)] + [
    (f'slice-{kind}', f'synthetic-{kind}', 1) for kind in ('i', 'p', 'b', 'sp', 'si')] + [
    ('slice-fmo-3', 'synthetic-fmo-3', 1)]
ENCODED_TYPES = {'RBSP unsigned', 'RBSP signed', 'RBSP boolean'}
PPS_BASE = {'pic_parameter_set_id', 'seq_parameter_set_id', 'entropy_coding_mode_flag',
            'bottom_field_pic_order_in_frame_present_flag'}


def camel(name):
    parts = name.split('_')
    return parts[0] + ''.join(part[:1].upper() + part[1:] for part in parts[1:])


def field_path(name):
    """Map standard syntax labels to the deliberate native grouping contract."""
    if name in PPS_FIELDS:
        return (PPS_FIELDS[name],)
    if name in SLICE_FIELDS:
        return (SLICE_FIELDS[name],)
    match = re.fullmatch(r'pic_scaling_list_present_flag\[(\d+)\]', name)
    if match:
        return (f'Scaling list[{match[1]}]', 'scalingListPresentFlag')
    match = re.fullmatch(r'delta_scale\[(\d+)\]\[(\d+)\]', name)
    if match:
        return (f'Scaling list[{match[1]}]', f'deltaScale[{match[2]}]')
    match = re.fullmatch(r'(modification_of_pic_nums_idc|abs_diff_pic_num_minus1|long_term_pic_num)_l([01])\[(\d+)\]', name)
    if match:
        return (f'Reference list L{match[2]}[{match[3]}]', camel(match[1]))
    match = re.fullmatch(r'(luma|chroma)_(weight|offset)_l([01])(_flag)?\[(\d+)\](?:\[(\d+)\])?', name)
    if match:
        key = match[1] + match[2].title() + ('Flag' if match[4] else '')
        if match[6] is not None:
            key += f'[{match[6]}]'
        return (f'Prediction weight L{match[3]}[{match[5]}]', key)
    match = re.fullmatch(r'(memory_management_control_operation|difference_of_pic_nums_minus1|long_term_pic_num|long_term_frame_idx|max_long_term_frame_idx_plus1)\[(\d+)\]', name)
    if match:
        return (f'Reference marking[{match[2]}]', camel(match[1]))
    return (camel(name),)


def descendant_fields(fields, root_ids):
    by_id = {field['id']: field for field in fields}
    require(len(by_id) == len(fields), 'duplicate native field identity')
    result = {}
    for field in fields:
        if field['type'] not in ENCODED_TYPES:
            continue
        path, parent, seen = [field['name']], field['parentId'], set()
        while parent not in root_ids and parent in by_id:
            require(parent not in seen, 'native field parent cycle')
            seen.add(parent)
            node = by_id[parent]
            path.insert(0, node['name'])
            parent = node['parentId']
        if parent in root_ids:
            key = tuple(path)
            require(key not in result, 'duplicate native syntax path')
            result[key] = field
    return result


def compare_header(fields, block):
    pps = block['kind'] == 'Picture Parameter Set'
    names = ['PPS prefix', 'PPS extended syntax'] if pps else [
        f'Slice prefix NAL[{block["nalIndex"]}]', f'Slice full header NAL[{block["nalIndex"]}]']
    roots = []
    for name in names:
        matches = [field for field in fields if field['name'] == name]
        require(len(matches) == 1, 'missing or duplicate native syntax root: ' + name)
        roots.append(matches[0]['id'])
    actual = descendant_fields(fields, set(roots))
    expected_paths = set()
    for oracle in block['fields']:
        if pps and oracle['name'] in PPS_BASE:
            continue  # Legacy desktop base IDs/flags are derived text without spans.
        if oracle['name'].startswith(('rbsp_', 'cabac_alignment')):
            continue
        path = field_path(oracle['name'])
        require(path in actual, 'native header field missing: ' + oracle['name'])
        expected_paths.add(path)
        field = actual[path]
        value = str(oracle['value'])
        if field['type'] == 'RBSP boolean':
            value = 'true' if oracle['value'] else 'false'
        require(field['value'] == value, 'native header value differs: ' + oracle['name'])
        segments = oracle['sourceBitSegments']
        span = ({'byteOffset': str(segments[0]['byteOffset']), 'bitOffset': segments[0]['bitOffset'],
                 'bitLength': segments[0]['bitLength']} if len(segments) == 1 else None)
        require(field['span'] == span, 'native header source bits differ: ' + oracle['name'])
    require(set(actual) == expected_paths, 'native header contains unexpected encoded fields')
    return len(expected_paths)


def unique_value(fields, name):
    matches = [field for field in fields if field['name'] == name]
    require(len(matches) == 1, 'native derived field missing or duplicate: ' + name)
    return matches[0]['value']


def verify_semantics(name, snapshot, expected):
    _, source_name, tag = next(case for case in CASES if case[0] == name)
    oracle = expected['fullH264'][source_name]
    detail = snapshot['detail']['detail']
    require(snapshot['selectedTag']['id'] == str(tag), 'native selected the wrong Tag')
    require(detail['complete'] and not detail['truncated'] and not detail['diagnostics'],
            'native full header is incomplete, truncated or diagnosed')
    block = next(block for block in oracle['traceBlocks'] if block['tagId'] == tag)
    fields = detail['fields']
    count = compare_header(fields, block)
    complete_name = 'ppsFullComplete' if tag == 0 else 'headerComplete'
    require(unique_value(fields, complete_name) == 'true', 'native full-header completeness missing')
    if tag == 1:
        require(unique_value(fields, 'sliceTypeName') == oracle['sliceType'], 'native slice type differs')
        end = max(field['rbspBitStartIncludingNALHeader'] + field['bitLength'] - 8 for field in block['fields'])
        require(unique_value(fields, 'headerBits') == str(end), 'native header ending bit differs')
    return {'fields': len(fields), 'encodedFieldsCompared': count, 'detailComplete': True,
            'sliceType': oracle['sliceType'] if tag else None, 'fmoMapType': oracle['mapType']}


def verify_field_source(directory, source, field_name):
    sys.path.insert(0, str(ROOT / 'tools/desktop-native'))
    from snapshot_check import check_snapshot
    native = json.loads((directory / 'native-result.json').read_text())
    stage = native['fieldSource']
    require(stage['fieldName'] == field_name, 'native did not click the requested tail field')
    for filename, receipt in ((stage['initialFile'], stage['initialSnapshotCheck']), ('snapshot.json', native['snapshotCheck'])):
        rechecked = check_snapshot(directory / filename, source)
        require(rechecked['sha256'] == receipt['sha256'] and rechecked['schemaValid'] and rechecked['hex']['matchesSource'],
                'native initial/final artifact recheck failed')
    snapshot = json.loads((directory / stage['file']).read_text())
    checked = check_snapshot(directory / stage['file'], source)
    require(checked['sha256'] == stage['snapshotCheck']['sha256'] and checked['schemaValid'] and checked['hex']['matchesSource'],
            'native tail-field exported source/Hex recheck failed')
    require(digest(directory / stage['uiFile']) == stage['uiSHA256'], 'native tail-field UI changed')
    fields = snapshot['detail']['detail']['fields']
    matches = [field for field in fields if field['name'] == field_name]
    require(len(matches) == 1 and matches[0]['span'] == stage['span'], 'native selected tail source span differs')
    ui = json.loads((directory / stage['uiFile']).read_text())
    require(any(f'查看 byte {stage["span"]["byteOffset"]} 的位' in item.get('name', '') for item in ui),
            'native tail source Hex is absent from UI')
    label = f'{field_name} · {stage["span"]["byteOffset"]} : {stage["span"]["bitOffset"]} · {stage["span"]["bitLength"]} bit'
    require(any(label in (item.get('name', '') + item.get('text', '')) for item in ui), 'native chosen tail-field label missing')
    return {'tailField': field_name, 'tailSnapshotSHA256': checked['sha256'], 'tailSpan': stage['span']}


def run(args, commands):
    expected, rows = load_expectations(args.input_dir), []
    binary_hash = digest(args.binary)
    for name, source_name, tag in CASES:
        directory = args.output_dir / name
        source = args.input_dir / expected['fullH264'][source_name]['file']
        field_name = 'secondChromaQpIndexOffset' if tag == 0 else 'sliceGroupChangeCycle' if name == 'slice-fmo-3' else 'sliceBetaOffsetDiv2'
        commands.run(name, [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                           '--source', source, '--tag', str(tag), '--field-name', field_name, '--output-dir', directory,
                           '--timeout-seconds', str(args.timeout_seconds)], timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, expected, semantic_check=verify_semantics)
        require(row['binarySHA256'] == binary_hash and row['sourceSHA256'] == digest(source), 'native binary/source identity differs')
        row.update(verify_field_source(directory, source, field_name))
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash, 'production binary changed during full-header acceptance')
    load_expectations(args.input_dir)
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash,
            'expectationsSHA256': digest(args.input_dir / 'expectations.json'), 'cases': rows}


def main(argv=None):
    args = arguments(argv, description=__doc__)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'binary': str(args.binary), 'binarySHA256': digest(args.binary),
                                         'input': str(args.input_dir), 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
                                         'scriptSHA256': digest(Path(__file__))})
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
