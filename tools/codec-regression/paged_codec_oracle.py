#!/usr/bin/env python3
"""Author MP3 paging and H.264 SEI fixtures with independent byte/bit oracles.

No product parser or output establishes expectations. Frame and slice bodies
are structural bytes, not a decoding corpus. SEI syntax follows FFmpeg's
cbs_h264_syntax_template.c and cbs_sei_syntax_template.c.
"""

import argparse
import hashlib
import json
import sys
import traceback

from full_codec_oracle import Syntax, mp3_all_frames, synthetic_h264
from generate import digest, flv_tags, map_trace_fields, new_directory, require, save
from search_history_oracle import make_flv, records
from side_info_oracle import parse_side_info


REFERENCES = [
    'https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/cbs_h264_syntax_template.c',
    'https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/cbs_sei_syntax_template.c',
    'https://ffmpeg.org/doxygen/8.1/mpegaudiodec__template_8c_source.html',
]


def span(offset, length, bit=0):
    return {'byteOffset': offset, 'bitOffset': bit, 'bitLength': length}


def mp3_frame(index, back=0, mono=False, changed_rate=False):
    """MPEG-1 Layer III; author the side-info slots and leave audio opaque."""
    header = bytes.fromhex('fffba464' if changed_rate else 'fffb90c4' if mono else 'fffb9064')
    length, side_bytes = (480 if changed_rate else 417), (17 if mono else 32)
    side = bytearray(side_bytes)
    require(0 <= back <= 511, 'main_data_begin does not fit its 9-bit field')
    side[0], side[1] = back >> 1, (back & 1) << 7
    # A nonzero part2_3_length makes main-data source evidence useful while
    # leaving the compressed syntax deliberately unverified.
    at = 9 + (5 if mono else 3) + (4 if mono else 8)
    bits = list(''.join(f'{value:08b}' for value in side))
    bits[at:at + 12] = f'{24:012b}'
    side = bytes(int(''.join(bits[i:i + 8]), 2) for i in range(0, len(bits), 8))
    body = bytes((index * 17 + i * 29) % 256 for i in range(length - 4 - side_bytes))
    return header + side + body


def mp3_oracle(data, selected):
    parsed = mp3_all_frames(data)
    frames, history, previous_format = [], [], None
    by_tag = {}
    for row in parsed['frames']:
        by_tag.setdefault(row['tagId'], []).append(row)
    for tag in flv_tags(data):
        body = data[tag['payloadOffset']:tag['payloadEnd']]
        if tag['type'] == 8 and (not body or body[0] >> 4 != 2):
            history, previous_format = [], None
        for row in by_tag.get(tag['tagId'], []):
            offset, length = row['offset'], min(row['frameLength'] or 4, tag['payloadEnd'] - row['offset'])
            fmt = row['version'], row['sampleRateHz'], row['channels']
            if previous_format is not None and previous_format != fmt:
                history = []
            previous_format = fmt
            side_size = 17 if row['channels'] == 1 else 32
            side = parse_side_info(data[offset + 4:offset + length], row['version'], row['channels'], offset + 4)
            main_offset, main_length = offset + min(length, 4 + side_size), max(0, length - 4 - side_size)
            item = {**row, 'span': span(offset, length * 8), 'headerHex': data[offset:offset + 4].hex(),
                    'mainDataSpan': span(main_offset, main_length * 8), 'mainDataBegin': side.get('main_data_begin'),
                    'part23Bits': side.get('part23TotalBits'), 'ranges': [], 'missingBytes': None}
            if (row['tagId'], row['frameIndex']) in selected:
                item['sideInfo'] = side
            if row['complete'] and side['valid']:
                needed, ranges = side['main_data_begin'], []
                for old in reversed(history):
                    take = min(needed, old['length'])
                    if take:
                        ranges.append({'tagId': old['tagId'], 'frameIndex': old['frameIndex'],
                                       'span': span(old['offset'] + old['length'] - take, take * 8)})
                        needed -= take
                    if not needed:
                        break
                item.update(ranges=list(reversed(ranges)), missingBytes=needed)
                area = b''.join(data[r['span']['byteOffset']:r['span']['byteOffset'] + r['span']['bitLength'] // 8]
                                for r in item['ranges'])
                item['windowSHA256'] = hashlib.sha256(area).hexdigest()
                require(len(area) + needed == side['main_data_begin'], 'MP3 source window lost bytes')
                history.append({'tagId': row['tagId'], 'frameIndex': row['frameIndex'],
                                'offset': main_offset, 'length': main_length})
                # The backpointer cannot reach more than 511 physical bytes.
                while len(history) > 3 and sum(h['length'] for h in history[1:]) >= 511:
                    history.pop(0)
            else:
                history, previous_format = [], None
            frames.append(item)
    return {'frames': frames, 'stops': parsed['stops'], 'completeFrames': parsed['completeFrames'],
            'selected': [{'tagId': t, 'frameIndex': f} for t, f in sorted(selected)]}


def extend_255(value):
    require(0 <= value < 1 << 32, 'SEI integer outside oracle range')
    return b'\xff' * (value // 255) + bytes([value % 255])


def escape_rbsp(rbsp):
    output, zeros = bytearray([6]), 0
    for value in rbsp:
        if zeros >= 2 and value <= 3:
            output.append(3)
            zeros = 0
        output.append(value)
        zeros = zeros + 1 if value == 0 else 0
    return bytes(output)


def recovery_payload(count=3, changing=2):
    syntax = Syntax()
    syntax.ue('recoveryFrameCnt', count)
    syntax.u('exactMatchFlag', 1)
    syntax.u('brokenLinkFlag', 0)
    syntax.u('changingSliceGroupIdc', changing, 2)
    bits = syntax.bits
    if len(bits) % 8:
        bits += '1' + '0' * (-(len(bits) + 1) % 8)
    return bytes(int(bits[i:i + 8], 2) for i in range(0, len(bits), 8)), syntax.fields


def sei_rbsp(messages, trailing=True):
    result = b''.join(extend_255(kind) + extend_255(len(body)) + body for kind, body in messages)
    return result + (b'\x80' if trailing else b'')


def sei_oracle(nal, offset):
    """Parse message framing from deescaped bytes; typed values use raw bits."""
    raw, mapping, zeros = bytearray(), [], 0
    for index, value in enumerate(nal[1:], 1):
        if zeros >= 2 and value == 3 and index + 1 < len(nal) and nal[index + 1] <= 3:
            zeros = 0
            continue
        raw.append(value)
        mapping.append(offset + index)
        zeros = zeros + 1 if value == 0 else 0

    def mapped(name, value, start, bit_length):
        bits = ''.join(f'{v:08b}' for v in raw)
        return map_trace_fields([{'name': name, 'value': value, 'bit': start + 8,
                                  'bits': bits[start:start + bit_length]}], nal, offset)[0]

    messages, at, complete, reason = [], 0, False, 'missing_trailing_bits'
    while at < len(raw):
        if raw[at:] == b'\x80':
            complete, reason = True, ''
            break
        message = {'index': len(messages)}
        for name in ('payloadType', 'payloadSize'):
            begin, value = at, 0
            while at < len(raw):
                byte = raw[at]
                value += byte
                at += 1
                if byte != 255:
                    break
            else:
                message.update(complete=False, reason='truncated_' + name)
                messages.append(message)
                return {'messages': messages, 'complete': False, 'reason': message['reason'],
                        'rbspBytes': len(raw), 'nalBytes': len(nal)}
            message[name] = mapped(name, value, begin * 8, (at - begin) * 8)
        size, kind = message['payloadSize']['value'], message['payloadType']['value']
        available = min(size, len(raw) - at)
        body = bytes(raw[at:at + available])
        message.update(kind={5: 'userDataUnregistered', 6: 'recoveryPoint'}.get(kind, 'unknown'),
                       payloadHex=body.hex(), payloadStartBit=at * 8, payloadComplete=available == size,
                       complete=available == size,
                       payloadSpan=mapped('payloadSpan', None, at * 8, available * 8))
        if kind == 5 and available >= 16:
            message['uuid'] = {'hex': body[:16].hex(), 'span': mapped('uuid', None, at * 8, 128)}
            message['dataBytes'] = size - 16
            preview = body[16:80]
            message['dataPreview'] = {'hex': preview.hex(), 'span': mapped('preview', None, (at + 16) * 8, len(preview) * 8)}
            message['previewTruncated'] = size - 16 > len(preview)
        elif kind == 5:
            message.update(complete=False, reason='short_uuid')
        elif kind == 6:
            bits = ''.join(f'{value:08b}' for value in body)
            zero = len(bits) - len(bits.lstrip('0'))
            width = 2 * zero + 1
            if width + 4 <= len(bits):
                count = int(bits[zero:width], 2) - 1
                fields = [mapped('recoveryFrameCnt', count, at * 8, width)]
                for name, relative, length in [('exactMatchFlag', width, 1), ('brokenLinkFlag', width + 1, 1),
                                               ('changingSliceGroupIdc', width + 2, 2)]:
                    value = int(bits[relative:relative + length], 2)
                    fields.append(mapped(name, value, at * 8 + relative, length))
                message['recoveryFields'] = fields
                message['alignment'] = mapped('payloadAlignmentBits', None, at * 8 + width + 4,
                                               len(bits) - width - 4)
                if count > 65535 or fields[-1]['value'] > 2:
                    message.update(complete=False, reason='recovery_value_invalid')
            else:
                message.update(complete=False, reason='short_recovery')
        messages.append(message)
        at += available
        if available != size:
            reason = 'truncated_payload'
            break
    return {'messages': messages, 'complete': complete and all(m['complete'] for m in messages),
            'framingComplete': complete, 'reason': reason,
            'rbspBytes': len(raw), 'nalBytes': len(nal)}


def generate(directory):
    expected = {'schemaVersion': 1, 'sources': {}, 'mp3Paging': {}, 'sei': {}, 'references': REFERENCES,
                'provenance': 'Authored bytes and independent Python arithmetic; no product parser/output.',
                'limitations': ['Opaque MP3 main data is not decoded or reservoir-consumption validated.',
                                'SEI fixtures validate syntax and source evidence, not decoded pictures.']}
    base = [mp3_frame(i, 0 if i < 2 else 511, mono=i == 270, changed_rate=i == 150) for i in range(3000)]
    selections = {(0, i) for i in (0, 63, 64, 127, 128, 150, 151, 255, 270, 271, 2600, 2999)}
    split = [(8, b'\x2f' + b''.join(base[:64]), 0), (18, b'\x05', 1),
             (8, b'\x2f' + b''.join(base[64:130]), 1)]
    variants = {
        'long-tag': ([(8, b'\x2f' + b''.join(base), 0)], selections),
        'cross-tag': (split, {(2, 0), (2, 1), (2, 64), (2, 65)}),
        'audio-barrier': (split[:2] + [(8, b'\xaf\x00\x12\x10', 1)] + split[2:], {(3, 0), (3, 1)}),
        'truncated-late': ([(8, b'\x2f' + b''.join(base[:131])[:-17], 0)], {(0, 128), (0, 130)}),
        'free-format-late': ([(8, b'\x2f' + b''.join(base[:128]) + bytes.fromhex('fffb0064'), 0)], {(0, 127), (0, 128)}),
    }
    for name, (rows, chosen) in variants.items():
        filename, data = 'mp3-page-' + name + '.flv', make_flv(rows)
        (directory / filename).write_bytes(data)
        expected['mp3Paging'][name] = {'file': filename, **mp3_oracle(data, chosen)}

    recovery, _ = recovery_payload()
    zero, _ = recovery_payload(0)
    maximum, _ = recovery_payload(65535)
    overflow, _ = recovery_payload(65536)
    invalid_change, _ = recovery_payload(3, 3)
    uuid = bytes.fromhex('00000100000200000300000011223344')
    user = uuid + b'alpha\x00\x00\x01\xff\xfe' + bytes(range(256)) + b'end' * 20
    sei_cases = {
        'mixed': [sei_rbsp([(6, recovery), (5, user), (511, b'\x00\x00\x02\x55')])],
        'empty-unknown': [sei_rbsp([(200, b''), (6, zero)])],
        'maximum-recovery': [sei_rbsp([(6, maximum)])],
        'truncated-type': [b'\xff\xff'],
        'truncated-size': [b'\x06\xff'],
        'truncated-payload': [b'\x05\x28' + uuid[:12]],
        'short-uuid': [sei_rbsp([(5, uuid[:15])])],
        'short-recovery': [sei_rbsp([(6, b'')])],
        'invalid-recovery-count': [sei_rbsp([(6, overflow)])],
        'invalid-recovery-change': [sei_rbsp([(6, invalid_change)])],
        'message-budget': [sei_rbsp([(200, b'') for _ in range(65)])],
        'byte-budget': [sei_rbsp([(200, b'\x55' * 70000)])],
        'tag-byte-budget': [sei_rbsp([(200, b'\x55' * 60000)]) for _ in range(5)],
    }
    configuration = records(synthetic_h264()[0])[0]
    for name, rbsp in sei_cases.items():
        nals = [escape_rbsp(raw) for raw in rbsp]
        body = b'\x27\x01\0\0\0' + b''.join(len(nal).to_bytes(4, 'big') + nal for nal in nals)
        filename, data = 'avc-sei-' + name + '.flv', make_flv([configuration, (9, body, 40)])
        (directory / filename).write_bytes(data)
        offset, blocks = flv_tags(data)[1]['payloadOffset'] + 5, []
        for i, nal in enumerate(nals):
            offset += 4
            blocks.append({'nalIndex': i, 'offset': offset, **sei_oracle(nal, offset)})
            offset += len(nal)
        expected['sei'][name] = {'file': filename, 'tagId': 1, 'nals': blocks,
                                  'budgetLimited': 'budget' in name}
        diagnostics = {'truncated-type': 'AVC_SEI_HEADER_TRUNCATED', 'truncated-size': 'AVC_SEI_HEADER_TRUNCATED',
                       'truncated-payload': 'AVC_SEI_PAYLOAD_TRUNCATED', 'short-uuid': 'AVC_SEI_SYNTAX_INVALID',
                       'short-recovery': 'AVC_RBSP_SYNTAX_TRUNCATED', 'message-budget': 'AVC_SEI_MESSAGE_LIMIT_REACHED',
                       'byte-budget': 'AVC_SEI_BYTE_LIMIT_REACHED', 'tag-byte-budget': 'AVC_SEI_BYTE_LIMIT_REACHED',
                       'invalid-recovery-count': 'AVC_SEI_SYNTAX_INVALID', 'invalid-recovery-change': 'AVC_SEI_SYNTAX_INVALID'}
        if name in diagnostics:
            expected['sei'][name]['diagnostic'] = diagnostics[name]
    for path in sorted(directory.glob('*.flv')):
        expected['sources'][path.name] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    save(directory, 'expectations.json', expected)
    return {'passed': True, 'expectationsSHA256': digest(directory / 'expectations.json'),
            'mp3Cases': len(variants), 'seiCases': len(sei_cases), 'sources': expected['sources']}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    args = parser.parse_args(argv)
    args.output_dir.mkdir(parents=True)
    try:
        result = generate(args.output_dir)
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
