#!/usr/bin/env python3
"""Check AVC detail v5 and native MP3 pages against authored source evidence."""

import argparse
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, Commands, digest, executable, load_expectations, new_directory, require, save


def expected_span(field):
    return {'rbspBitOffset': field['rbspBitStartIncludingNALHeader'] - 8,
            'bitLength': field['bitLength'], 'sourceSpans': field['sourceBitSegments']}


def prefix_span(field, count):
    require(0 <= count <= field['bitLength'], 'observed span exceeds authored payload')
    result, remaining = [], count
    for item in field['sourceBitSegments']:
        take = min(remaining, item['bitLength'])
        if take:
            result.append({**item, 'bitLength': take})
            remaining -= take
    require(remaining == 0, 'authored source mapping lacks observed bits')
    return {'rbspBitOffset': field['rbspBitStartIncludingNALHeader'] - 8,
            'bitLength': count, 'sourceSpans': result}


def check_mapped(actual, expected, label):
    require(actual['value'] == expected['value'], label + ': value differs')
    require(actual['span'] == expected_span(expected), label + ': source mapping differs')


def verify_sei_unit(got, want, budget=False):
    require(0 <= got['bytesRead'] <= 65536, 'SEI per-NAL read budget exceeded')
    require(len(got['messages']) <= 64, 'SEI per-NAL message budget exceeded')
    require(got['rbsp']['ebspSpan']['bitLength'] == got['bytesRead'] * 8,
            'SEI RBSP source bytes disagree with actual read accounting')
    if not budget:
        require(got['complete'] == want['complete'], 'SEI completion differs')
        if 'framingComplete' in want:
            require(got['framingComplete'] == want['framingComplete'], 'SEI framing completion differs')
        require(len(got['messages']) == len(want['messages']), 'SEI messages lost, repeated or invented')
    else:
        require(len(got['messages']) <= len(want['messages']), 'SEI budget invented messages')
    encoded = 0
    for index, (actual, expected) in enumerate(zip(got['messages'], want['messages'])):
        label = f'SEI[{index}]'
        require(actual['index'] == expected['index'], label + ': index differs')
        for name in ('payloadType', 'payloadSize'):
            if name in expected and name in actual:
                check_mapped(actual[name], expected[name], label + '.' + name)
                encoded += 1
            elif name in expected and not budget:
                raise RuntimeError(label + ': missing ' + name)
        if 'payloadSpan' not in expected:
            require(not actual['complete'], label + ': partial header reported complete')
            continue
        require(actual['kind'] == expected['kind'], label + ': payload kind differs')
        require(actual['syntaxSupported'] == (expected['kind'] != 'unknown'), label + ': supported syntax flag differs')
        if 'payloadSpan' not in actual:
            require(expected['payloadSpan']['bitLength'] == 0 or budget, label + ': nonempty payload mapping is missing')
            if not budget:
                require(actual['payloadComplete'] == expected['payloadComplete'] and actual['complete'] == expected['complete'],
                        label + ': empty payload completeness differs')
            continue
        if not budget:
            require(actual['payloadSpan'] == expected_span(expected['payloadSpan']), label + ': payload span differs')
            require(actual['payloadComplete'] == expected['payloadComplete'] and
                    actual['complete'] == expected['complete'], label + ': payload completeness differs')
        else:
            require(actual['payloadSpan'] == prefix_span(expected['payloadSpan'], actual['payloadSpan']['bitLength']),
                    label + ': inspected payload prefix differs')
        if 'recoveryFields' in expected and actual.get('recoveryPoint'):
            recovery = actual['recoveryPoint']
            for field_index, field in enumerate(expected['recoveryFields']):
                if not recovery.get(field['name']) and not expected['complete'] and field_index > 0:
                    continue  # The parser may stop immediately after an invalid encoded value.
                check_mapped(recovery[field['name']], field, label + '.' + field['name'])
                encoded += 1
            if expected['complete'] or recovery.get('payloadAlignmentBits'):
                require(recovery['payloadAlignmentBits'] == expected_span(expected['alignment']),
                        label + ': recovery alignment source bits differ')
        elif 'recoveryFields' in expected and not budget:
            raise RuntimeError(label + ': recovery point is missing')
        if 'uuid' in expected and actual.get('userDataUnregistered'):
            user = actual['userDataUnregistered']
            for field in ('uuid', 'dataPreview'):
                require(user[field]['hex'].lower() == expected[field]['hex'] and
                        user[field]['span'] == expected_span(expected[field]['span']), label + ': ' + field + ' differs')
            require(user['dataBytes'] == expected['dataBytes'] and
                    user['previewTruncated'] == expected['previewTruncated'], label + ': preview bounds differ')
        elif 'uuid' in expected and not budget:
            raise RuntimeError(label + ': user data UUID is missing')
    return encoded


def verify_sei(document, expected):
    require(document['schemaVersion'] == 5 and document['documentType'] == 'flv-avc-tag-detail', 'wrong SEI contract')
    require(document['tag']['id'] == expected['tagId'], 'wrong SEI selected Tag')
    units = document['detail']['nalUnitSequence']['nalUnits']
    require(len(units) == len(expected['nals']), 'SEI NAL list differs')
    fields, byte_count, messages, partial = 0, 0, 0, False
    for got, want in zip(units, expected['nals']):
        require(got['span']['byteOffset'] == want['offset'] and got['header']['nalUnitType']['value'] == 6,
                'SEI NAL source/type differs')
        require('sei' in got, 'SEI details are missing')
        fields += verify_sei_unit(got['sei'], want, expected['budgetLimited'])
        byte_count += got['sei']['bytesRead']
        messages += len(got['sei']['messages'])
        partial |= not got['sei']['complete']
    require(byte_count <= 262144 and messages <= 256, 'SEI Tag budget exceeded')
    complete = all(nal['complete'] for nal in expected['nals']) and not expected['budgetLimited']
    require(document['detail']['complete'] == complete, 'SEI Tag complete differs')
    if not complete:
        require(partial and any(row['code'] == expected['diagnostic'] for row in document['diagnostics']),
                'SEI partial result lacks its expected diagnostic')
    return fields


def native_children(detail, parent):
    return {field['name']: field for field in detail['fields'] if field['parentId'] == parent}


def native_root(detail, name):
    rows = [field for field in detail['fields'] if field['name'] == name]
    require(len(rows) == 1, 'missing or ambiguous native root: ' + name)
    return rows[0], native_children(detail, rows[0]['id'])


def native_span(value):
    return {**value, 'byteOffset': str(value['byteOffset'])}


def verify_mp3_native_frames(snapshot, case, tag_id, page_start, selected):
    """Page frame identity/source checks; independent of UI button labels."""
    require(snapshot['detail']['state'] == 'ready' and snapshot['detail']['error'] is None,
            'MP3 native detail is not ready')
    detail = snapshot['detail']['detail']
    require(snapshot['selectedTag']['id'] == str(tag_id) and detail['kind'] == 'mp3Audio', 'MP3 Tag identity differs')
    root, meta = native_root(detail, 'Frame inspection')
    require(meta['selectedFrameIndex']['value'] == str(selected), 'MP3 selected global frame index differs')
    expected = [frame for frame in case['frames'] if frame['tagId'] == tag_id and
                page_start <= frame['frameIndex'] < page_start + 64]
    if 'selectableFrameOffsets' in meta:
        offsets = ','.join(str(row['frameIndex'] - page_start) for row in expected if row['complete'])
        require(meta['selectableFrameOffsets']['value'] == offsets, 'MP3 page-relative selection indices differ')
        absolute = ','.join(str(row['frameIndex']) for row in expected if row['complete'])
        require(meta['selectableFrameIndices']['value'] == (absolute if len(absolute) <= 256 else ''),
                'MP3 absolute choices were truncated or rebased')
    items = [field for field in detail['fields'] if field['parentId'] == root['id'] and field['type'] == 'MPEG frame']
    require(len(items) == len(expected), 'MP3 page frame count differs')
    by_name = {field['name']: field for field in items}
    require(len(by_name) == len(items) and set(by_name) == {'Frame ' + str(row['frameIndex']) for row in expected},
            'MP3 frame summaries have duplicate, omitted or rebased identities')
    for want in expected:
        field = by_name['Frame ' + str(want['frameIndex'])]
        require(field['span'] == native_span(want['span']),
                'MP3 global frame identity/source range differs')
        children = native_children(detail, field['id'])
        require(children['header']['value'].lower() == '0x' + want['headerHex'], 'MP3 frame header differs')
    require(selected in [row['frameIndex'] for row in expected if row['complete']], 'selected frame is outside complete page')
    return len(items)


def verify_mp3_native_page(snapshot, case, source, tag_id, page_start, selected, dependency=False):
    from native_context_side import mp3_fields
    from native_search_history import verify_mp3
    from side_info_oracle import source_field
    from verify_side_info import BOOLEAN_FIELDS
    count = verify_mp3_native_frames(snapshot, case, tag_id, page_start, selected)
    detail = snapshot['detail']['detail']
    _, page = native_root(detail, 'MP3 frame page')
    next_index = page_start + count
    all_rows = [row for row in case['frames'] if row['tagId'] == tag_id]
    has_next = count == 64 and any(row['frameIndex'] >= next_index for row in all_rows)
    expected = {'pageState': 'ready', 'requestedPageStart': str(page_start), 'pageStartIndex': str(page_start),
                'pageSize': '64', 'pageFrames': str(count), 'nextFrameIndex': str(next_index),
                'hasNext': str(has_next).lower(), 'nextPageStart': str(next_index) if has_next else 'none',
                'requestedFrameIndex': str(selected), 'canContinue': 'false'}
    for name, value in expected.items():
        require(page[name]['value'] == value, 'native page metadata differs: ' + name)
    for name, limit in [('pageBytesCovered', 1 << 20), ('pageBytesRead', 64 << 10),
                        ('replayPages', 16), ('checkpointCount', 64)]:
        require(0 <= int(page[name]['value']) <= limit, 'native page exceeded budget: ' + name)
    require(page['pageReadBudget']['value'] == str(64 << 10) and page['replayPageBudget']['value'] == '16',
            'native page budget declaration differs')
    want = next(row for row in all_rows if row['frameIndex'] == selected)
    side = want['sideInfo']
    roots = [field for field in detail['fields'] if field['name'].startswith('Side info Frame ')]
    require(len(roots) == 1 and roots[0]['name'] == f'Side info Frame {selected}' and
            roots[0]['span'] == native_span(side['span']), 'selected side-info identity/source differs')
    fields = mp3_fields(detail['fields'], roots[0])
    require(set(fields) == {field['path'] for field in side['fields']}, 'native selected side-info field set differs')
    for original in side['fields']:
        name = original['path']
        is_bool = name.startswith('scfsi[') or name.rsplit('.', 1)[-1] in BOOLEAN_FIELDS
        value = str(bool(original['value'])).lower() if is_bool else str(original['value'])
        require(fields[name]['value'] == value and fields[name]['span'] == native_span(original['span']),
                'native selected side-info value/source differs: ' + name)
        source_field(source, original)
    result = {'tagId': tag_id, 'pageStart': page_start, 'frames': count, 'selectedFrameIndex': selected,
              'mappedSideInfoFields': len(fields)}
    if dependency:
        back = want['mainDataBegin']
        target = {**want, 'backBytes': back,
                  'backpointerSpan': next(field['span'] for field in side['fields'] if field['path'] == 'main_data_begin'),
                  'state': 'incomplete' if want['missingBytes'] else 'none' if back == 0 else
                           'across_tags' if any(r['tagId'] != tag_id for r in want['ranges']) else 'within_tag'}
        result['dependency'] = verify_mp3(snapshot, target, source)
    return result


def check_native_mapped(detail, field, expected, label, prefix=False):
    parts = [native_span(part) for part in expected['sourceBitSegments']]
    children = [item for item in detail['fields'] if item['parentId'] == field['id']]
    if prefix:
        actual = [field['span']] if field['span'] is not None else [item['span'] for item in children]
        bits = sum(part['bitLength'] for part in actual)
        parts = [native_span(part) for part in prefix_span(expected, bits)['sourceSpans']]
    if len(parts) == 1:
        require(field['span'] == parts[0] and not children, label + ': contiguous source span differs')
    else:
        require(field['span'] is None, label + ': discontiguous source has a fabricated contiguous span')
        require([item['span'] for item in children] == parts and
                [item['name'] for item in children] == [field['name'] + f'.source[{i}]' for i in range(len(parts))],
                label + ': split source spans differ')


def verify_sei_native(snapshot, case):
    detail = snapshot['detail']['detail']
    require(snapshot['selectedTag']['id'] == str(case['tagId']) and detail['kind'] == 'avcVideo', 'native SEI Tag differs')
    complete = all(nal['complete'] for nal in case['nals']) and not case['budgetLimited']
    require(detail['complete'] == complete, 'native SEI completeness differs')
    if not complete:
        require(any(d['code'] == case['diagnostic'] for d in detail['diagnostics']), 'native SEI diagnostic differs')
    compared, messages = 0, 0
    for want in case['nals']:
        root, meta = native_root(detail, f'SEI messages NAL[{want["nalIndex"]}]')
        if not case['budgetLimited']:
            require(meta['seiComplete']['value'] == str(want['complete']).lower(), 'native SEI sequence completeness differs')
        require(0 <= int(meta['seiBytesRead']['value']) <= 65536, 'native SEI read budget exceeded')
        groups = [field for field in detail['fields'] if field['parentId'] == root['id'] and field['type'] == 'SEI message']
        require(len(groups) == int(meta['seiMessageCount']['value']), 'native SEI omitted projected messages')
        if not case['budgetLimited']:
            require(len(groups) == len(want['messages']), 'native SEI message count differs')
        for group, expected in zip(groups, want['messages']):
            fields = native_children(detail, group['id'])
            require(group['name'] == f'SEI message[{expected["index"]}]' and group['value'] == expected['kind'],
                    'native SEI message identity/type differs')
            for name in ('payloadType', 'payloadSize'):
                require(fields[name]['value'] == str(expected[name]['value']), 'native SEI value differs: ' + name)
                check_native_mapped(detail, fields[name], expected[name], name)
                compared += 1
            require(fields['syntaxSupported']['value'] == str(expected['kind'] != 'unknown').lower(), 'native SEI syntax scope differs')
            if not case['budgetLimited']:
                require(fields['messageComplete']['value'] == str(expected['complete']).lower() and
                        fields['payloadComplete']['value'] == str(expected['payloadComplete']).lower(), 'native SEI payload completeness differs')
            if expected['payloadSpan']['bitLength']:
                check_native_mapped(detail, fields['seiPayload'], expected['payloadSpan'], 'payload', prefix=case['budgetLimited'])
            for field in expected.get('recoveryFields', []):
                name = field['name']
                if name not in fields and not expected['complete']:
                    continue
                value = str(bool(field['value'])).lower() if name in ('exactMatchFlag', 'brokenLinkFlag') else str(field['value'])
                require(fields[name]['value'] == value, 'native recovery point value differs: ' + name)
                check_native_mapped(detail, fields[name], field, name)
                compared += 1
            if 'uuid' in expected:
                for name, key in [('uuid', 'uuid'), ('userDataPreview', 'dataPreview')]:
                    require(fields[name]['value'].lower() == expected[key]['hex'], 'native SEI hex bytes differ: ' + name)
                    check_native_mapped(detail, fields[name], expected[key]['span'], name)
                require(fields['userDataBytes']['value'] == str(expected['dataBytes']) and
                        fields['userDataPreviewTruncated']['value'] == str(expected['previewTruncated']).lower(),
                        'native SEI preview coverage differs')
            messages += 1
    return {'messages': messages, 'mappedFields': compared, 'complete': complete}


def verify_legacy_sei(document, version):
    require(document['schemaVersion'] == version and document['detail']['complete'],
            'legacy SEI framing contract changed')
    def visit(value):
        if isinstance(value, dict):
            require('sei' not in value, 'SEI semantic fields leaked into a legacy contract')
            for item in value.values():
                visit(item)
        elif isinstance(value, list):
            for item in value:
                visit(item)
    visit(document)


def verify(args, commands):
    from jsonschema import Draft202012Validator
    expected, cli_hash = load_expectations(args.input_dir), digest(args.cli)
    schemas = {version: ROOT / f'docs/schemas/flv-avc-tag-detail-v{version}.schema.json' for version in range(1, 6)}
    validators = {version: Draft202012Validator(json.loads(path.read_text())) for version, path in schemas.items()}
    cases, fields, legacy_cases = [], 0, 0
    for name, oracle in expected['sei'].items():
        complete = all(nal['complete'] for nal in oracle['nals']) and not oracle['budgetLimited']
        output = commands.run('sei-' + name, [args.cli, 'detail', '--schema-version', '5', '--tag',
                              str(oracle['tagId']), args.input_dir / oracle['file']], expected=0 if complete else 2)
        document = json.loads(output.read_text())
        validators[5].validate(document)
        fields += verify_sei(document, oracle)
        reports = {'5': digest(output)}
        for version in range(1, 5):
            # Every fixture retains valid FLV/NAL framing. Earlier contracts
            # identify SEI NALs but leave these payload bytes uninterpreted.
            legacy = commands.run(f'sei-{name}.v{version}', [args.cli, 'detail', '--schema-version', str(version),
                                  '--tag', str(oracle['tagId']), args.input_dir / oracle['file']])
            old = json.loads(legacy.read_text())
            validators[version].validate(old)
            verify_legacy_sei(old, version)
            reports[str(version)] = digest(legacy)
            legacy_cases += 1
        cases.append({'name': name, 'complete': complete, 'reportSHA256': reports})
    require(digest(args.cli) == cli_hash, 'CLI changed during SEI verification')
    load_expectations(args.input_dir)
    return {'passed': True, 'seiCases': len(cases), 'legacyCases': legacy_cases, 'mappedFields': fields, 'cases': cases,
            'cliSHA256': cli_hash, 'schemaSHA256': {str(v): digest(path) for v, path in schemas.items()},
            'expectationsSHA256': digest(args.input_dir / 'expectations.json')}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--cli', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    args = parser.parse_args(argv)
    args.input_dir = args.input_dir.resolve()
    args.output_dir.mkdir(parents=True)
    try:
        result = verify(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
