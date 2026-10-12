#!/usr/bin/env python3
"""Generate full-header and all-frame regression inputs without product output.

Encoded I/P/B evidence comes from FFmpeg trace_headers. Synthetic H.264 inputs
contain authored syntax and opaque slice data, and are not decoding fixtures.
MP3 expectations walk MPEG header bytes at declared physical frame boundaries.
"""

import argparse
import json
from pathlib import Path
import sys
import traceback

import generate as baseline
from generate import Commands, digest, executable, flv_tags, map_trace_fields, new_directory, require, save
from search_history_oracle import make_flv, records as flv_records


class Syntax:
    def __init__(self):
        self.bits = ''
        self.fields = []

    def add(self, name, value, bits):
        self.fields.append({'name': name, 'value': value, 'bit': len(self.bits) + 8, 'bits': bits})
        self.bits += bits

    def u(self, name, value, width=1):
        require(0 <= value < 1 << width, 'fixed field does not fit: ' + name)
        self.add(name, value, f'{value:0{width}b}')

    def ue(self, name, value):
        require(value >= 0, 'negative unsigned Exp-Golomb')
        binary = f'{value + 1:b}'
        self.add(name, value, '0' * (len(binary) - 1) + binary)

    def se(self, name, value):
        code = -2 * value if value <= 0 else 2 * value - 1
        binary = f'{code + 1:b}'
        self.add(name, value, '0' * (len(binary) - 1) + binary)

    def nal(self, header, opaque=b''):
        bits = self.bits + '1'
        bits += '0' * (-len(bits) % 8)
        raw = bytes(int(bits[i:i + 8], 2) for i in range(0, len(bits), 8)) + opaque
        escaped, zeros = bytearray([header]), 0
        for value in raw:
            if zeros >= 2 and value <= 3:
                escaped.append(3)
                zeros = 0
            escaped.append(value)
            zeros = zeros + 1 if value == 0 else 0
        return bytes(escaped)


def sps(width_minus1=9):
    b = Syntax()
    for name, value, width in [('profile_idc', 100, 8), ('constraint_flags', 0, 8), ('level_idc', 30, 8)]:
        b.u(name, value, width)
    for name, value in [('seq_parameter_set_id', 0), ('chroma_format_idc', 1),
                        ('bit_depth_luma_minus8', 0), ('bit_depth_chroma_minus8', 0)]:
        b.ue(name, value)
    b.u('qpprime_y_zero_transform_bypass_flag', 0)
    b.u('seq_scaling_matrix_present_flag', 0)
    for name, value in [('log2_max_frame_num_minus4', 0), ('pic_order_cnt_type', 0),
                        ('log2_max_pic_order_cnt_lsb_minus4', 0), ('max_num_ref_frames', 2)]:
        b.ue(name, value)
    b.u('gaps_in_frame_num_value_allowed_flag', 0)
    b.ue('pic_width_in_mbs_minus1', width_minus1)
    b.ue('pic_height_in_map_units_minus1', 5)
    for name, value in [('frame_mbs_only_flag', 1), ('direct_8x8_inference_flag', 1),
                        ('frame_cropping_flag', 0), ('vui_parameters_present_flag', 0)]:
        b.u(name, value)
    return b.nal(0x67)


def pps(cabac=True, weighted=False, map_type=None, scaling=False, initial_qp=-2):
    b = Syntax()
    b.ue('pic_parameter_set_id', 0)
    b.ue('seq_parameter_set_id', 0)
    b.u('entropy_coding_mode_flag', int(cabac))
    b.u('bottom_field_pic_order_in_frame_present_flag', 0)
    b.ue('num_slice_groups_minus1', int(map_type is not None))
    if map_type is not None:
        b.ue('slice_group_map_type', map_type)
        if map_type == 0:
            for i in range(2):
                b.ue(f'run_length_minus1[{i}]', i)
        elif map_type == 2:
            b.ue('top_left[0]', 0)
            b.ue('bottom_right[0]', 11)
        elif map_type in (3, 4, 5):
            b.u('slice_group_change_direction_flag', 1)
            b.ue('slice_group_change_rate_minus1', 1)
        elif map_type == 6:
            b.ue('pic_size_in_map_units_minus1', 59)
            for i in range(60):
                b.u(f'slice_group_id[{i}]', i % 2)
    b.ue('num_ref_idx_l0_default_active_minus1', 0)
    b.ue('num_ref_idx_l1_default_active_minus1', 0)
    b.u('weighted_pred_flag', int(weighted))
    b.u('weighted_bipred_idc', int(weighted), 2)
    for name, value in [('pic_init_qp_minus26', initial_qp), ('pic_init_qs_minus26', 0), ('chroma_qp_index_offset', 1)]:
        b.se(name, value)
    for name, value in [('deblocking_filter_control_present_flag', 1), ('constrained_intra_pred_flag', 0),
                        ('redundant_pic_cnt_present_flag', 0)]:
        b.u(name, value)
    b.u('transform_8x8_mode_flag', int(scaling))
    b.u('pic_scaling_matrix_present_flag', int(scaling))
    if scaling:
        for i in range(8):
            b.u(f'pic_scaling_list_present_flag[{i}]', int(i in (0, 6)))
            if i == 0:
                b.se('delta_scale[0][0]', -8)  # nextScale=0 selects the default list.
            elif i == 6:
                for j in range(64):
                    b.se(f'delta_scale[6][{j}]', 0)
    b.se('second_chroma_qp_index_offset', -1)
    return b


def slice_header(kind, cabac=True, weighted=False, map_type=None):
    b = Syntax()
    b.ue('first_mb_in_slice', 0)
    b.ue('slice_type', {'P': 0, 'B': 1, 'I': 2, 'SP': 3, 'SI': 4}[kind])
    b.ue('pic_parameter_set_id', 0)
    b.u('frame_num', 0 if kind == 'I' else 1, 4)
    if kind == 'I':
        b.ue('idr_pic_id', 0)
    b.u('pic_order_cnt_lsb', 0 if kind == 'I' else 2, 4)
    if kind == 'B':
        b.u('direct_spatial_mv_pred_flag', 1)
    if kind in ('P', 'B', 'SP'):
        b.u('num_ref_idx_active_override_flag', 1)
        b.ue('num_ref_idx_l0_active_minus1', 1)
        if kind == 'B':
            b.ue('num_ref_idx_l1_active_minus1', 0)
        for direction in range(2 if kind == 'B' else 1):
            b.u(f'ref_pic_list_modification_flag_l{direction}', 1)
            for i, op in enumerate((0, 2, 3)):
                b.ue(f'modification_of_pic_nums_idc_l{direction}[{i}]', op)
                if op == 0:
                    b.ue(f'abs_diff_pic_num_minus1_l{direction}[{i}]', 0)
                elif op == 2:
                    b.ue(f'long_term_pic_num_l{direction}[{i}]', 0)
    if weighted and kind in ('P', 'B', 'SP'):
        b.ue('luma_log2_weight_denom', 2)
        b.ue('chroma_log2_weight_denom', 2)
        for direction, count in [(0, 2)] + ([(1, 1)] if kind == 'B' else []):
            for i in range(count):
                b.u(f'luma_weight_l{direction}_flag[{i}]', 1)
                b.se(f'luma_weight_l{direction}[{i}]', 3)
                b.se(f'luma_offset_l{direction}[{i}]', -2)
                b.u(f'chroma_weight_l{direction}_flag[{i}]', 1)
                for j in range(2):
                    b.se(f'chroma_weight_l{direction}[{i}][{j}]', 4)
                    b.se(f'chroma_offset_l{direction}[{i}][{j}]', j - 1)
    if kind == 'I':
        b.u('no_output_of_prior_pics_flag', 1)
        b.u('long_term_reference_flag', 0)
    else:
        b.u('adaptive_ref_pic_marking_mode_flag', 1)
        for i, op in enumerate((1, 2, 3, 4, 5, 6, 0)):
            b.ue(f'memory_management_control_operation[{i}]', op)
            if op in (1, 3):
                b.ue(f'difference_of_pic_nums_minus1[{i}]', 0)
            if op == 2:
                b.ue(f'long_term_pic_num[{i}]', 0)
            if op in (3, 6):
                b.ue(f'long_term_frame_idx[{i}]', 0)
            if op == 4:
                b.ue(f'max_long_term_frame_idx_plus1[{i}]', 1)
    if cabac and kind not in ('I', 'SI'):
        b.ue('cabac_init_idc', 2)
    b.se('slice_qp_delta', 3)
    if kind in ('SP', 'SI'):
        if kind == 'SP':
            b.u('sp_for_switch_flag', 1)
        b.se('slice_qs_delta', -2)
    b.ue('disable_deblocking_filter_idc', 2)
    b.se('slice_alpha_c0_offset_div2', -2)
    b.se('slice_beta_offset_div2', 1)
    if map_type in (3, 4, 5):
        b.u('slice_group_change_cycle', 3, 5)  # ceil(log2(60 / 2 + 1)).
    return b


def synthetic_h264(kind='I', map_type=None, scaling=False, initial_qp=-2):
    cabac, weighted = kind not in ('SP', 'SI'), kind in ('P', 'B', 'SP')
    s, p, h = sps(), pps(cabac, weighted, map_type, scaling, initial_qp), slice_header(kind, cabac, weighted, map_type)
    pn, hn = p.nal(0x68), h.nal(0x65 if kind == 'I' else 0x41, b'\x55' * 32)
    avcc = b'\x01\x64\x00\x1e\xff\xe1' + len(s).to_bytes(2, 'big') + s
    avcc += b'\x01' + len(pn).to_bytes(2, 'big') + pn + b'\xfd\xf8\xf8\x00'
    data = make_flv([(9, b'\x17\x00\0\0\0' + avcc, 0),
                     (9, (b'\x17' if kind == 'I' else b'\x27') + b'\x01\0\0\0' + len(hn).to_bytes(4, 'big') + hn, 40)])
    tags = flv_tags(data)
    po = tags[0]['payloadOffset'] + 5 + 6 + 2 + len(s) + 1 + 2
    ho = tags[1]['payloadOffset'] + 9
    blocks = []
    for label, tag, offset, nal, syntax in [('Picture Parameter Set', 0, po, pn, p), ('Slice Header', 1, ho, hn, h)]:
        require(data[offset:offset + len(nal)] == nal, 'authored NAL source extent differs')
        blocks.append({'kind': label, 'tagId': tag, 'nalIndex': 0, 'offset': offset, 'bytes': len(nal),
                       'fields': map_trace_fields(syntax.fields, nal, offset)})
    return data, {'traceBlocks': blocks, 'sliceType': kind, 'mapType': map_type, 'scaling': scaling,
                  'provenance': 'Authored syntax with recorded codewords and source bit spans; opaque slice body, not a decoded-picture fixture.'}


def mp3_all_frames(data):
    rows, stops = [], []
    for tag in flv_tags(data):
        body = data[tag['payloadOffset']:tag['payloadEnd']]
        if tag['type'] != 8 or not body or body[0] >> 4 != 2:
            continue
        pos, index = tag['payloadOffset'] + 1, 0
        while pos < tag['payloadEnd']:
            available = tag['payloadEnd'] - pos
            if available < 4:
                stops.append({'tagId': tag['tagId'], 'offset': pos, 'reason': 'header_truncated'})
                break
            raw = int.from_bytes(data[pos:pos + 4], 'big')
            version, layer, rate, frequency = (raw >> 19) & 3, (raw >> 17) & 3, (raw >> 12) & 15, (raw >> 10) & 3
            if raw >> 21 != 2047 or version == 1 or layer != 1 or rate == 15 or frequency == 3:
                stops.append({'tagId': tag['tagId'], 'offset': pos, 'reason': 'invalid_header'})
                break
            hz = [44100, 48000, 32000][frequency] // {3: 1, 2: 2, 0: 4}[version]
            kbps = ([0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320] if version == 3 else
                    [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160])[rate]
            length = (144000 if version == 3 else 72000) * kbps // hz + ((raw >> 9) & 1) if kbps else None
            mode = (raw >> 6) & 3
            row = {'tagId': tag['tagId'], 'frameIndex': index, 'offset': pos, 'dts': tag['dts'],
                   'version': version, 'sampleRateHz': hz, 'bitrateKbps': kbps, 'channelMode': mode,
                   'channels': 1 if mode == 3 else 2, 'samples': 1152 if version == 3 else 576,
                   'frameLength': length, 'complete': length is not None and length <= available}
            rows.append(row)
            if length is None or length > available:
                stops.append({'tagId': tag['tagId'], 'offset': pos, 'reason': 'free_format' if length is None else 'body_truncated'})
                break
            pos, index = pos + length, index + 1
    return {'frames': rows, 'stops': stops, 'completeFrames': sum(r['complete'] for r in rows),
            'completeFrameBytes': sum(r['frameLength'] for r in rows if r['complete'])}


def extend(out):
    expected = json.loads((out / 'expectations.json').read_text())
    expected['fullH264'] = {}
    for name, kind, map_type, scaling in [(f'synthetic-{k.lower()}', k, None, k == 'I') for k in ('I', 'P', 'B', 'SP', 'SI')] + [
            (f'synthetic-fmo-{m}', 'I', m, False) for m in range(7)]:
        data, oracle = synthetic_h264(kind, map_type, scaling)
        filename = name + '.flv'
        (out / filename).write_bytes(data)
        expected['fullH264'][name] = {'file': filename, **oracle}
    original, _ = synthetic_h264('I', 6)
    original_records = flv_records(original)
    replacement = sps(19)
    stale = make_flv([original_records[0], (9, b'\x27\x01\0\0\0' + len(replacement).to_bytes(4, 'big') + replacement, 20), original_records[1]])
    invalid_qp, _ = synthetic_h264('I', initial_qp=-40)
    expected['fullH264Negative'] = {}
    for name, data, tag, diagnostic in [('sps-replacement', stale, 2, 'AVC_SLICE_PARAMETER_SET_MISSING'),
                                       ('pps-qp-bitdepth', invalid_qp, 0, 'AVC_PPS_VALUE_INVALID')]:
        filename = 'negative-' + name + '.flv'
        (out / filename).write_bytes(data)
        expected['fullH264Negative'][name] = {'file': filename, 'tagId': tag, 'diagnostic': diagnostic}
    raw = (out / 'xing-vbr.mp3').read_bytes()
    frames = baseline.mp3_frames(raw, 0)['frames']
    chunks = [raw[r['offset']:r['offset'] + r['length']] for r in frames]
    # Enough physical frames to pass the older per-Tag 64-frame detail budget.
    extended = chunks * 8
    def records(groups, interleave=False):
        items = []
        for i, body in enumerate(groups):
            items.append((8, b'\x2f' + body, i * 26))
            if interleave:
                items.append((18, b'\x05', i * 26))  # AMF0 null metadata.
        return items
    changed = bytearray(chunks[2])
    changed[3] = (changed[3] & 0x3f) | 0xc0  # Later mono header; same physical length.
    free = bytearray(chunks[2][:4])
    free[2] &= 0x0f
    low_rate = bytes.fromhex('fff380c4') + bytes(208 - 4)  # MPEG-2, mono, 64 kb/s, 22.05 kHz.
    cases = {
        'merged': records([b''.join(extended)]),
        'split': records(extended),
        'interleaved': records(extended, True),
        'later-change': records([chunks[0] + chunks[1] + bytes(changed) + chunks[3]]),
        'later-rate-change': records([chunks[0] + low_rate + chunks[1]]),
        'later-truncated': records([b''.join(chunks[:4])[:-17]]),
        'later-free-format': records([b''.join(chunks[:2]) + bytes(free)]),
        'later-invalid': records([b''.join(chunks[:2]) + b'\x00\xfb\x90\x64']),
    }
    expected['allFrameMP3'] = {}
    for name, items in cases.items():
        filename = 'all-frames-' + name + '.flv'
        data = make_flv(items)
        (out / filename).write_bytes(data)
        expected['allFrameMP3'][name] = {'file': filename, **mp3_all_frames(data)}
    for path in sorted(out.glob('*.flv')):
        expected['sources'][path.name] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    expected['limitations'].append('Synthetic full H.264 headers and MP3 later-header mutations are structural syntax fixtures, not valid decoded media.')
    save(out, 'expectations.json', expected)
    return {'passed': True, 'expectationsSHA256': digest(out / 'expectations.json'),
            'sources': expected['sources'], 'syntheticH264Cases': len(expected['fullH264']),
            'negativeH264Cases': len(expected['fullH264Negative']),
            'allFrameMP3Cases': len(expected['allFrameMP3'])}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--ffmpeg', type=executable, default='ffmpeg')
    parser.add_argument('--ffprobe', type=executable, default='ffprobe')
    args = parser.parse_args(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)),
                                         'ffmpeg': str(args.ffmpeg), 'ffprobe': str(args.ffprobe)})
        baseline.generate(args, Commands(args.output_dir))
        result = extend(args.output_dir)
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
