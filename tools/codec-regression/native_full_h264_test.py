"""Native full-header evidence rejects changed values, spans, hierarchy and scope."""

import copy
import unittest

from full_codec_oracle import synthetic_h264
from native_full_h264 import CASES, PPS_BASE, PPS_FIELDS, SLICE_FIELDS, compare_header, field_path


def projected_fields(block):
    pps = block['kind'] == 'Picture Parameter Set'
    names = ['PPS prefix', 'PPS extended syntax'] if pps else ['Slice prefix NAL[0]', 'Slice full header NAL[0]']
    fields = [{'id': str(i), 'parentId': '', 'name': name, 'type': 'RBSP syntax', 'span': None, 'value': ''}
              for i, name in enumerate(names)]
    groups = {}
    for oracle in block['fields']:
        if pps and oracle['name'] in PPS_BASE:
            continue
        path = field_path(oracle['name'])
        parent = '0' if oracle['name'] in (PPS_FIELDS if pps else SLICE_FIELDS) else '1'
        if len(path) == 2:
            if path[0] not in groups:
                group = str(len(fields))
                groups[path[0]] = group
                fields.append({'id': group, 'parentId': parent, 'name': path[0], 'type': 'RBSP syntax', 'value': '', 'span': None})
            parent = groups[path[0]]
        boolean = 'flag' in oracle['name']
        segment = oracle['sourceBitSegments'][0]
        fields.append({'id': str(len(fields)), 'parentId': parent, 'name': path[-1],
                       'type': 'RBSP boolean' if boolean else 'RBSP signed' if oracle['value'] < 0 else 'RBSP unsigned',
                       'value': ('true' if oracle['value'] else 'false') if boolean else str(oracle['value']),
                       'span': {**segment, 'byteOffset': str(segment['byteOffset'])}})
    return fields


class FullNativeTests(unittest.TestCase):
    def test_suite_covers_all_slice_types_and_fmo_maps(self):
        self.assertEqual(len(CASES), 14)
        self.assertTrue({f'slice-{k}' for k in ('i', 'p', 'b', 'sp', 'si')} <= {c[0] for c in CASES})
        self.assertTrue({f'pps-fmo-{m}' for m in range(7)} <= {c[0] for c in CASES})

    def test_standard_labels_map_to_native_nested_groups(self):
        expected = {
            'delta_scale[6][63]': ('Scaling list[6]', 'deltaScale[63]'),
            'chroma_weight_l1[0][1]': ('Prediction weight L1[0]', 'chromaWeight[1]'),
            'luma_weight_l0_flag[1]': ('Prediction weight L0[1]', 'lumaWeightFlag'),
            'modification_of_pic_nums_idc_l0[2]': ('Reference list L0[2]', 'modificationOfPicNumsIdc'),
            'memory_management_control_operation[5]': ('Reference marking[5]', 'memoryManagementControlOperation'),
            'slice_group_id[59]': ('sliceGroupId[59]',),
            'cabac_init_idc': ('cabacInitIdc',),
        }
        for name, path in expected.items():
            self.assertEqual(field_path(name), path)

    def test_author_bits_match_every_native_conditional_field(self):
        for kind in ('I', 'P', 'B', 'SP', 'SI'):
            _, oracle = synthetic_h264(kind, scaling=kind == 'I')
            for block in oracle['traceBlocks']:
                fields = projected_fields(block)
                count = compare_header(fields, block)
                self.assertEqual(count, len(block['fields']) - (4 if block['kind'] == 'Picture Parameter Set' else 0))
        for group in range(7):
            _, oracle = synthetic_h264(map_type=group)
            for block in oracle['traceBlocks']:
                self.assertGreater(compare_header(projected_fields(block), block), 10)

    def test_mutated_native_evidence_fails(self):
        _, oracle = synthetic_h264('B')
        block = oracle['traceBlocks'][1]
        original = projected_fields(block)
        for mutation in ('value', 'span', 'name', 'missing', 'extra', 'parent', 'complete-root', 'duplicate-id'):
            fields = copy.deepcopy(original)
            field = next(f for f in fields if f['name'] == 'sliceBetaOffsetDiv2')
            if mutation == 'value':
                field['value'] = '99'
            elif mutation == 'span':
                field['span']['bitOffset'] = (field['span']['bitOffset'] + 1) % 8
            elif mutation == 'name':
                field['name'] = 'inventedBeta'
            elif mutation == 'missing':
                fields.remove(field)
            elif mutation == 'extra':
                extra = copy.deepcopy(field)
                extra.update(id='999', name='inventedEncodedField')
                fields.append(extra)
            elif mutation == 'parent':
                field['parentId'] = 'unknown'
            elif mutation == 'complete-root':
                fields[1]['name'] = 'Slice full header NAL[9]'
            else:
                field['id'] = '0'
            with self.subTest(mutation=mutation), self.assertRaises(RuntimeError):
                compare_header(fields, block)

    def test_discontinuous_source_does_not_invent_a_contiguous_span(self):
        _, oracle = synthetic_h264('I')
        block = copy.deepcopy(oracle['traceBlocks'][1])
        fields = projected_fields(block)
        target = next(f for f in block['fields'] if f['name'] == 'slice_beta_offset_div2')
        target['sourceBitSegments'].append({'byteOffset': 999, 'bitOffset': 0, 'bitLength': 1})
        actual = next(f for f in fields if f['name'] == 'sliceBetaOffsetDiv2')
        with self.assertRaisesRegex(RuntimeError, 'source bits'):
            compare_header(fields, block)
        actual['span'] = None
        self.assertGreater(compare_header(fields, block), 0)


if __name__ == '__main__':
    unittest.main()
