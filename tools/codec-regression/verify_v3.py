#!/usr/bin/env python3
"""Check bounded PPS/slice detail against independently mapped FFmpeg traces."""

import json
from pathlib import Path
import re
import sys
import traceback

from generate import (ROOT, H264, BFRAMES, Commands, digest, flv_tags,
                      load_expectations, require, save)
from verify import arguments


def key(name):
    name = name.replace('pic_parameter_set_id', 'picture_parameter_set_id')
    name = name.replace('seq_parameter_set_id', 'sequence_parameter_set_id')
    return re.sub('[^a-z0-9]', '', name.lower())


def compare_fields(actual, block, supported):
    expected = {key(f['name']): f for f in block['fields']}
    present = {key(name) for name, field in actual.items()
               if isinstance(field, dict) and 'value' in field and 'span' in field}
    require(present == set(expected) & {key(name) for name in supported},
            'supported prefix fields missing or invented: ' + str(present ^ (set(expected) & {key(name) for name in supported})))
    count = 0
    for name, field in actual.items():
        if not isinstance(field, dict) or 'value' not in field or 'span' not in field:
            continue
        want = expected[key(name)]
        require(field['value'] == want['value'], f'{name}: value differs from FFmpeg')
        span = field['span']
        require(span['rbspBitOffset'] + 8 == want['rbspBitStartIncludingNALHeader'] and
                span['bitLength'] == want['bitLength'] and span['sourceSpans'] == want['sourceBitSegments'],
                f'{name}: source bits differ from FFmpeg')
        count += 1
    return count


PPS_BASE = 'pic_parameter_set_id seq_parameter_set_id entropy_coding_mode_flag bottom_field_pic_order_in_frame_present_flag'.split()
PPS_PREFIX = ('num_slice_groups_minus1 num_ref_idx_l0_default_active_minus1 num_ref_idx_l1_default_active_minus1 '
              'weighted_pred_flag weighted_bipred_idc pic_init_qp_minus26 pic_init_qs_minus26 chroma_qp_index_offset '
              'deblocking_filter_control_present_flag constrained_intra_pred_flag redundant_pic_cnt_present_flag').split()
SLICE_PREFIX = ('first_mb_in_slice slice_type pic_parameter_set_id colour_plane_id frame_num field_pic_flag '
                'bottom_field_flag idr_pic_id pic_order_cnt_lsb delta_pic_order_cnt_bottom delta_pic_order_cnt[0] '
                'delta_pic_order_cnt[1] redundant_pic_cnt').split()


def make_flv(payloads):
    result = bytearray(b'FLV\x01\x01\x00\x00\x00\x09' + b'\0' * 4)
    for payload in payloads:
        result += b'\x09' + len(payload).to_bytes(3, 'big') + b'\0' * 7
        result += payload + (11 + len(payload)).to_bytes(4, 'big')
    return bytes(result)


def verify(args, commands):
    from jsonschema import Draft202012Validator
    expected = load_expectations(args.input_dir)
    schema = ROOT / 'docs/schemas/flv-avc-tag-detail-v3.schema.json'
    validator = Draft202012Validator(json.loads(schema.read_text()))
    cli_hash = digest(args.cli)
    fields, slices, types, multi = 0, 0, set(), 0

    def detail(name, source, tag, code=0, extra=()):
        output = commands.run(name, [args.cli, 'detail', '--schema-version', '3', '--tag', str(tag), *extra, source], expected=code)
        doc = json.loads(output.read_text())
        validator.validate(doc)
        return doc

    for name, filename in [('h264', H264), ('h264BFrames', BFRAMES)]:
        oracle, source = expected[name], args.input_dir / filename
        documents = {}
        for block in oracle['traceBlocks']:
            if block['kind'] not in ('Picture Parameter Set', 'Slice Header'):
                continue
            tag = block['tagId']
            if tag not in documents:
                documents[tag] = detail(f'{name}.tag{tag}', source, tag)
            doc = documents[tag]
            require(doc['detail']['complete'] and not doc['diagnostics'], 'valid prefix is incomplete')
            if block['kind'] == 'Picture Parameter Set':
                unit = next(u for u in doc['detail']['avcDecoderConfigurationRecord']['pictureParameterSets'] if u['span']['byteOffset'] == block['offset'])
                pps = unit['pictureParameterSet']
                require(pps['prefix']['prefixComplete'], 'PPS prefix incomplete')
                fields += compare_fields(pps, block, PPS_BASE) + compare_fields(pps['prefix'], block, PPS_PREFIX)
            else:
                units = doc['detail']['nalUnitSequence']['nalUnits']
                prefix = units[block['nalIndex']]['sliceHeaderPrefix']
                require(prefix['prefixComplete'] and doc['detail']['parameterContext']['state'] == 'found', 'slice/context incomplete')
                require(prefix['referencedSpsSpan']['byteOffset'] < doc['tag']['offset'] and
                        prefix['referencedPpsSpan']['byteOffset'] < doc['tag']['offset'], 'external parameter source missing')
                require(0 < prefix['bytesRead'] <= 256 and prefix['bytesRead'] < block['bytes'], 'slice body was read')
                fields += compare_fields(prefix, block, SLICE_PREFIX)
                types.add(prefix['sliceTypeName'])
                slices += 1
        for doc in documents.values():
            prefixes = [u['sliceHeaderPrefix'] for u in doc['detail'].get('nalUnitSequence', {}).get('nalUnits', []) if 'sliceHeaderPrefix' in u]
            if len(prefixes) > 1:
                require(len({p['firstMbInSlice']['value'] for p in prefixes}) > 1, 'multi-slice positions lost')
                multi += 1

    require(types >= {'I', 'P', 'B'} and multi > 0, 'oracle does not cover I/P/B and multi-slice')
    # Mutate only container assembly. Expected missing/truncated states are not
    # derived from the product, and retained logs make every source inspectable.
    data = (args.input_dir / BFRAMES).read_bytes()
    tags = flv_tags(data)
    config_tag = expected['h264BFrames']['configurationTagId']
    slice_block = next(b for b in expected['h264BFrames']['traceBlocks'] if b['kind'] == 'Slice Header')
    config = data[tags[config_tag]['payloadOffset']:tags[config_tag]['payloadEnd']]
    nalu = data[tags[slice_block['tagId']]['payloadOffset']:tags[slice_block['tagId']]['payloadEnd']]
    cases = [
        ('missing', [nalu], 0, ('--avc-nal-length-size', '4'), 'AVC_SLICE_PARAMETER_SET_MISSING'),
        ('invalid-replacement', [config, b'\x17\0\0\0\0\x01\x64', nalu], 2, (), 'AVC_SLICE_PARAMETER_SET_MISSING'),
        ('truncated', [config, b'\x17\x01\0\0\0\0\0\0\x01\x65'], 1, (), 'AVC_RBSP_SYNTAX_TRUNCATED'),
    ]
    for name, payloads, tag, extra, diagnostic in cases:
        source = args.output_dir / (name + '.flv')
        source.write_bytes(make_flv(payloads))
        doc = detail(name, source, tag, 2, extra)
        require(not doc['detail']['complete'] and any(d['code'] == diagnostic for d in doc['diagnostics']), name + ': false completeness or missing diagnostic')
        prefixes = [u['sliceHeaderPrefix'] for u in doc['detail']['nalUnitSequence']['nalUnits'] if 'sliceHeaderPrefix' in u]
        require(prefixes and all(not p['prefixComplete'] and 'frameNum' not in p for p in prefixes), name + ': reused or invented parameter-dependent fields')

    load_expectations(args.input_dir)
    require(digest(args.cli) == cli_hash, 'CLI changed during validation')
    return {'passed': True, 'cliSHA256': cli_hash, 'schemaSHA256': digest(schema),
            'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
            'sourceMappedFieldsCompared': fields, 'sliceHeadersCompared': slices,
            'sliceTypes': sorted(types), 'multiSliceTags': multi, 'negativeCases': len(cases),
            'scope': 'PPS necessary fields and slice prefix only; not complete header or picture decoding.'}


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
