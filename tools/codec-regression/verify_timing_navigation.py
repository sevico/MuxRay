#!/usr/bin/env python3
"""Check timing v6 source evidence and byte-identical legacy v1-v5 reports."""

import argparse
import json
from pathlib import Path
import re
import sys
import traceback

from generate import ROOT, Commands, digest, executable, load_expectations, new_directory, require, save
from verify_paged_codec import check_mapped, expected_span, check_native_mapped, native_children


def mapped_fields(value, prefix=''):
    if isinstance(value, dict):
        if 'value' in value and 'span' in value and isinstance(value['span'], dict) and 'sourceSpans' in value['span']:
            return {prefix: value}
        result = {}
        for key, child in value.items():
            result.update(mapped_fields(child, prefix + ('.' if prefix else '') + key))
        return result
    if isinstance(value, list):
        result = {}
        for index, child in enumerate(value):
            result.update(mapped_fields(child, prefix + f'[{index}]'))
        return result
    return {}


def timing_parameters(config):
    def hrd(item):
        return {'present': item is not None, 'cpbCount': item['count'] if item else 0,
                **{name: item[key] if item else 0 for name, key in [
                    ('initialCpbRemovalDelayLength', 'initial'), ('cpbRemovalDelayLength', 'cpb'),
                    ('dpbOutputDelayLength', 'dpb'), ('timeOffsetLength', 'offset')]}}
    return {'nalHrd': hrd(config['nal']), 'vclHrd': hrd(config['vcl']), 'picStructPresent': config['picStruct'],
            'timingInfoPresent': True, 'numUnitsInTick': 1001, 'timeScale': 60000, 'fixedFrameRate': True}


def verify_timing(document, expected):
    require(document['schemaVersion'] == 6 and document['tag']['id'] == expected['tagId'], 'wrong timing contract/Tag')
    units = document['detail']['nalUnitSequence']['nalUnits']
    unit = units[expected['nalIndex']]
    require(unit['span']['byteOffset'] == expected['offset'], 'timing NAL source identity differs')
    sei = unit['sei']
    require(sei['complete'] == expected['complete'] and document['detail']['complete'] == expected['complete'],
            'timing completeness differs')
    require(sei['framingComplete'] == (not expected['budgetLimited']), 'timing framing/budget state differs')
    require(sei['bytesRead'] <= 65536 and sei['rbsp']['ebspSpan']['bitLength'] == sei['bytesRead'] * 8,
            'timing SEI read coverage differs')
    count = min(64, len(expected['messageTypes']))
    require(len(sei['messages']) == count, 'timing SEI message count differs')
    wanted = {f['name']: f for f in expected['fields']}
    actual_fields, compared = {}, 0
    for i, message in enumerate(sei['messages']):
        old = expected['framing']['messages'][i]
        for name in ('payloadType', 'payloadSize'):
            check_mapped(message[name], old[name], f'message[{i}].{name}')
            compared += 1
        if old['payloadSpan']['bitLength']:
            require(message['payloadSpan'] == expected_span(old['payloadSpan']), 'timing payload source mapping differs')
        else:
            require('payloadSpan' not in message, 'empty timing payload invented a source span')
        kind = 'bufferingPeriod' if expected['messageTypes'][i] == 0 else 'pictureTiming'
        require(message['index'] == i and message['kind'] == kind and message['syntaxSupported'] is True,
                'timing message identity/type differs')
        require(message['payloadComplete'] is True, 'authored payload framing reported truncated')
        typed = message[kind]
        context = typed['context']
        if expected['complete']:
            require(message['complete'] is True and context['state'] == 'resolved', 'valid timing syntax lost activation')
        if context['state'] == 'resolved':
            require(context['spsId'] == expected['timingParameters']['id'] and context['spsSpan'] == expected['spsSpan'],
                    'timing SPS identity/source differs')
            require(context['parameters'] == timing_parameters(expected['timingParameters']), 'timing SPS width declarations differ')
            require(context['source'] == expected['activationSource'], 'timing activation evidence kind differs')
            if 'activationSpan' in expected:
                require(context['evidenceSpan'] == expected['activationSpan'], 'timing activation source differs')
        else:
            require('parameters' not in context and 'spsSpan' not in context and 'evidenceSpan' not in context,
                    'unresolved timing context fabricated source evidence')
        actual_fields.update(mapped_fields(typed, f'messages[{i}].{kind}'))
    for name, actual in actual_fields.items():
        require(name in wanted, 'timing field was not encoded in source: ' + name)
        check_mapped(actual, wanted[name], name)
        compared += 1
    if expected['complete']:
        require(set(actual_fields) == set(wanted), 'valid timing syntax fields omitted')
    if expected['budgetLimited']:
        require(any(d['code'] == 'AVC_SEI_MESSAGE_LIMIT_REACHED' for d in document['diagnostics']),
                'timing message budget diagnostic missing')
    return {'mappedFields': compared, 'messages': count, 'complete': sei['complete']}


def native_timing_fields(detail, expected):
    """Resolve authored paths using parent identity, never duplicate leaf names."""
    roots = [f for f in detail['fields'] if f['name'] == f'SEI messages NAL[{expected["nalIndex"]}]']
    require(len(roots) == 1, 'native timing NAL root missing/ambiguous')
    root_fields = native_children(detail, roots[0]['id'])
    count = min(64, len(expected['messageTypes']))
    require(root_fields['seiMessageCount']['value'] == str(count), 'native timing declared message count differs')
    messages = [f for f in detail['fields'] if f['parentId'] == roots[0]['id'] and f['name'].startswith('SEI message[')]
    cutoff = expected['budgetLimited'] and detail['truncated'] and len(detail['fields']) == 512
    require(len(messages) == count or cutoff and 0 < len(messages) < count, 'native timing messages omitted without projection budget')
    result = {}
    for position, message in enumerate(messages):
        index = int(re.search(r'\[(\d+)\]', message['name'])[1])
        require(index == position, 'native timing message indices skipped or repeated')
        at_boundary = cutoff and position == len(messages) - 1
        message_fields = native_children(detail, message['id'])
        for name in ('payloadType', 'payloadSize'):
            if name not in message_fields:
                require(at_boundary, 'native timing message header omitted')
                continue
            want = expected['framing']['messages'][index][name]
            require(message_fields[name]['value'] == str(want['value']), 'native timing message header value differs')
            check_native_mapped(detail, message_fields[name], want, name)
        kind = 'bufferingPeriod' if expected['messageTypes'][index] == 0 else 'pictureTiming'
        title = 'Buffering period' if kind == 'bufferingPeriod' else 'Picture timing'
        groups = [f for f in detail['fields'] if f['parentId'] == message['id'] and f['name'] == title]
        require(message['value'] == kind, 'native timing message type differs')
        if not groups:
            require(at_boundary, 'native timing type omitted without projection budget')
            continue
        require(len(groups) == 1, 'native timing type ambiguous')
        group = groups[0]
        prefix = f'messages[{index}].{kind}.'
        for field in detail['fields']:
            if field['parentId'] == group['id']:
                result[prefix + field['name']] = field
                for pattern, array in [(r'NAL initial CPB\[(\d+)\]', 'nalInitialDelays'),
                                       (r'VCL initial CPB\[(\d+)\]', 'vclInitialDelays'),
                                       (r'Clock timestamp\[(\d+)\]', 'clockTimestamps')]:
                    match = re.fullmatch(pattern, field['name'])
                    if match:
                        for child in detail['fields']:
                            if child['parentId'] == field['id']:
                                result[prefix + f'{array}[{match[1]}].' + child['name']] = child
        if prefix + 'timingContextState' not in result:
            require(at_boundary, 'native timing context state omitted')
            continue
        state = result[prefix + 'timingContextState']['value']
        if expected['complete']:
            require(state == 'resolved', 'native timing lost SPS activation')
        if state == 'resolved':
            source = result[prefix + 'timingSpsSource']['span']
            want = expected['spsSpan']
            require(source == {**want, 'byteOffset': str(want['byteOffset'])}, 'native timing SPS source differs')
    return result


def verify_timing_native(snapshot, expected):
    detail = snapshot['detail']['detail']
    fields = native_timing_fields(detail, expected)
    compared = 0
    for field in expected['fields']:
        observed = fields.get(field['name'])
        if observed is None:
            require(not expected['complete'], 'native valid timing field missing: ' + field['name'])
            continue
        boolean = field['name'].endswith('Flag')
        value = str(bool(field['value'])).lower() if boolean else str(field['value'])
        require(observed['value'] == value, 'native timing field value differs: ' + field['name'])
        check_native_mapped(detail, observed, field, field['name'])
        compared += 1
    require(detail['complete'] == expected['complete'], 'native timing completeness differs')
    return {'mappedFields': compared, 'complete': expected['complete']}


def verify(args, commands):
    from jsonschema import Draft202012Validator
    expected = load_expectations(args.input_dir)
    hashes = {'cli': digest(args.cli), 'baselineCLI': digest(args.baseline_cli)}
    schemas = {v: ROOT / f'docs/schemas/flv-avc-tag-detail-v{v}.schema.json' for v in range(1, 7)}
    validators = {v: Draft202012Validator(json.loads(path.read_text())) for v, path in schemas.items()}
    cases, total, legacy = [], 0, 0
    for name, oracle in expected['seiTiming'].items():
        source = args.input_dir / oracle['file']
        report = commands.run(name + '-v6', [args.cli, 'detail', '--schema-version', '6', '--tag', str(oracle['tagId']), source],
                              expected=0 if oracle['complete'] else 2)
        document = json.loads(report.read_text())
        validators[6].validate(document)
        verified = verify_timing(document, oracle)
        total += verified['mappedFields']
        row = {'name': name, **verified, 'reportSHA256': digest(report), 'legacyReportSHA256': {}}
        for version in range(1, 6):
            # Authored timing messages are opaque and complete in v1-v5; all
            # NAL/FLV framing and ordinary slice headers remain valid.
            argv = ['detail', '--schema-version', str(version), '--tag', str(oracle['tagId']), source]
            legacy_exit = 2 if version == 5 and oracle['budgetLimited'] else 0
            old = commands.run(f'{name}-baseline-v{version}', [args.baseline_cli, *argv], expected=legacy_exit)
            new = commands.run(f'{name}-current-v{version}', [args.cli, *argv], expected=legacy_exit)
            require(old.read_bytes() == new.read_bytes(), f'{name}: legacy v{version} report changed')
            validators[version].validate(json.loads(new.read_text()))
            row['legacyReportSHA256'][str(version)] = digest(new)
            legacy += 1
        cases.append(row)
    require(hashes == {'cli': digest(args.cli), 'baselineCLI': digest(args.baseline_cli)}, 'verification binary changed')
    load_expectations(args.input_dir)
    return {'passed': True, 'timingCases': len(cases), 'legacyComparisons': legacy, 'mappedFields': total,
            'binarySHA256': hashes, 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
            'schemaSHA256': {str(v): digest(p) for v, p in schemas.items()}, 'cases': cases}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--cli', type=executable, required=True)
    parser.add_argument('--baseline-cli', type=executable, required=True)
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
