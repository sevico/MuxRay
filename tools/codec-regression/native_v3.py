#!/usr/bin/env python3
"""Two additional Linux native checks for H.264 PPS and B/multi-slice prefixes."""

import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, BFRAMES, Commands, digest, load_expectations, require, save
from native import arguments, verify_case


PPS_FIELDS = {
    'num_slice_groups_minus1': 'numSliceGroupsMinus1',
    'num_ref_idx_l0_default_active_minus1': 'numRefIdxL0DefaultActiveMinus1',
    'num_ref_idx_l1_default_active_minus1': 'numRefIdxL1DefaultActiveMinus1',
    'weighted_pred_flag': 'weightedPredFlag', 'weighted_bipred_idc': 'weightedBipredIdc',
    'pic_init_qp_minus26': 'picInitQpMinus26', 'pic_init_qs_minus26': 'picInitQsMinus26',
    'chroma_qp_index_offset': 'chromaQpIndexOffset',
    'deblocking_filter_control_present_flag': 'deblockingFilterControlPresentFlag',
    'constrained_intra_pred_flag': 'constrainedIntraPredFlag',
    'redundant_pic_cnt_present_flag': 'redundantPicCntPresentFlag',
}
SLICE_FIELDS = {
    'first_mb_in_slice': 'firstMbInSlice', 'slice_type': 'sliceType',
    'pic_parameter_set_id': 'pictureParameterSetId', 'colour_plane_id': 'colourPlaneId',
    'frame_num': 'frameNum', 'field_pic_flag': 'fieldPicFlag', 'bottom_field_flag': 'bottomFieldFlag',
    'idr_pic_id': 'idrPicId', 'pic_order_cnt_lsb': 'picOrderCntLsb',
    'delta_pic_order_cnt_bottom': 'deltaPicOrderCntBottom', 'delta_pic_order_cnt[0]': 'deltaPicOrderCnt0',
    'delta_pic_order_cnt[1]': 'deltaPicOrderCnt1', 'redundant_pic_cnt': 'redundantPicCnt',
}


def first_b_tag(expected):
    for block in expected['h264BFrames']['traceBlocks']:
        if block['kind'] == 'Slice Header' and any(f['name'] == 'slice_type' and f['value'] % 5 == 1 for f in block['fields']):
            return block['tagId']
    raise RuntimeError('generated trace has no B slice')


def child_fields(fields, parent):
    result = {}
    for field in fields:
        if field['parentId'] == parent:
            require(field['name'] not in result, 'duplicate prefix field: ' + field['name'])
            result[field['name']] = field
    return result


def compare_prefix(fields, trace, mapping):
    require(fields['prefixComplete']['value'] == 'true', 'native prefix is incomplete')
    compared = 0
    for oracle in trace['fields']:
        if oracle['name'] not in mapping:
            continue
        name = mapping[oracle['name']]
        require(name in fields, f'native prefix field missing: {name}')
        field = fields[name]
        expected = ('true' if oracle['value'] else 'false') if field['type'] == 'RBSP boolean' else str(oracle['value'])
        require(field['value'] == expected, f'native prefix value differs: {name}')
        segments = oracle['sourceBitSegments']
        span = ({'byteOffset': str(segments[0]['byteOffset']), 'bitOffset': segments[0]['bitOffset'],
                 'bitLength': segments[0]['bitLength']} if len(segments) == 1 else None)
        require(field['span'] == span, f'native prefix source mapping differs: {name}')
        compared += 1
    require(compared > 0, 'no prefix fields compared')
    return compared


def verify_semantics(name, snapshot, expected):
    detail = snapshot['detail']['detail']
    require(detail['complete'] and not detail['truncated'] and not detail['diagnostics'], 'native H.264 prefix detail is incomplete or diagnosed')
    fields, oracle = detail['fields'], expected['h264BFrames']
    tag_id = oracle['configurationTagId'] if name == 'pps' else first_b_tag(expected)
    require(snapshot['selectedTag']['id'] == str(tag_id), 'native prefix selected the wrong Tag')
    compared = 0
    if name == 'pps':
        roots = [f for f in fields if f['name'] == 'PPS prefix']
        traces = [b for b in oracle['traceBlocks'] if b['kind'] == 'Picture Parameter Set' and b['tagId'] == tag_id]
        require(len(roots) == 1 and traces, 'expected one native PPS prefix')
        got = child_fields(fields, roots[0]['id'])
        compared = compare_prefix(got, traces[0], PPS_FIELDS)
        require(compared == len(PPS_FIELDS), 'PPS fixture no longer covers the required prefix fields')
    else:
        require(name == 'b-slices', 'unknown native v3 case')
        traces = [b for b in oracle['traceBlocks'] if b['kind'] == 'Slice Header' and b['tagId'] == tag_id]
        roots = [f for f in fields if f['name'].startswith('Slice prefix NAL[')]
        require(len(traces) >= 2 and len(roots) == len(traces), 'native multi-slice count differs')
        first_mbs = []
        for trace in traces:
            require({'first_mb_in_slice', 'slice_type', 'pic_parameter_set_id', 'frame_num'} <= {f['name'] for f in trace['fields']},
                    'B-slice trace is missing required prefix fields')
            matches = [f for f in roots if f['name'] == f'Slice prefix NAL[{trace["nalIndex"]}]']
            require(len(matches) == 1, 'native slice NAL identity differs')
            got = child_fields(fields, matches[0]['id'])
            compared += compare_prefix(got, trace, SLICE_FIELDS)
            require(got['sliceTypeName']['value'] == 'B', 'native B slice classification differs')
            first_mbs.append(got['firstMbInSlice']['value'])
            for name, nal_type in (('referencedSPS', 7), ('referencedPPS', 8)):
                target = next(n for n in oracle['nals'] if n['tagId'] == oracle['configurationTagId'] and n['type'] == nal_type)
                require(got[name]['span'] == {'byteOffset': str(target['offset']), 'bitOffset': 0, 'bitLength': target['bytes'] * 8}, 'native parameter-set reference source differs')
        require(len(set(first_mbs)) == len(traces), 'multi-slice first_mb addresses are not distinct')
    return {'fields': len(fields), 'detailComplete': True, 'prefixFieldsCompared': compared}


def run(args, commands):
    expected, rows = load_expectations(args.input_dir), []
    binary_hash = digest(args.binary)
    for name, tag in (('pps', expected['h264BFrames']['configurationTagId']), ('b-slices', first_b_tag(expected))):
        directory = args.output_dir / name
        commands.run(name, [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                           '--source', args.input_dir / BFRAMES, '--tag', str(tag), '--output-dir', directory,
                           '--timeout-seconds', str(args.timeout_seconds)], timeout=args.timeout_seconds + 30)
        row = verify_case(directory, name, expected, semantic_check=verify_semantics)
        require(row['binarySHA256'] == binary_hash, 'native v3 case used a different binary')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash, 'production binary changed during v3 acceptance')
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
