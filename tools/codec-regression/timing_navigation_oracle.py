#!/usr/bin/env python3
"""Author timing SEI and direct MP3 navigation evidence from raw syntax bits.

These structural fixtures do not contain decodable picture/audio bodies. No
MuxRay parser/output is used to derive an expected value or source mapping.
"""

import argparse
import json
from pathlib import Path
import sys
import traceback

from full_codec_oracle import Syntax, pps, slice_header
from generate import avc_nals, digest, flv_tags, map_trace_fields, new_directory, require, save
from paged_codec_oracle import escape_rbsp, extend_255, mp3_frame, mp3_oracle, sei_oracle
from search_history_oracle import make_flv


REFERENCE = 'https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/cbs_h264_syntax_template.c'


def hrd(count=2, initial=8, cpb=8, dpb=7, offset=6):
    return dict(count=count, initial=initial, cpb=cpb, dpb=dpb, offset=offset)


def timing_sps(sps_id=0, nal=None, vcl=None, pic_struct=True):
    b = Syntax()
    for name, value, width in [('profileIdc', 66, 8), ('constraintFlags', 0, 8), ('levelIdc', 30, 8)]:
        b.u(name, value, width)
    b.ue('seqParameterSetId', sps_id)
    for name, value in [('log2MaxFrameNumMinus4', 0), ('picOrderCntType', 0),
                        ('log2MaxPicOrderCntLsbMinus4', 0), ('maxNumRefFrames', 2)]:
        b.ue(name, value)
    b.u('gapsInFrameNumValueAllowedFlag', 0)
    b.ue('picWidthInMbsMinus1', 9)
    b.ue('picHeightInMapUnitsMinus1', 5)
    for name, value in [('frameMbsOnlyFlag', 1), ('direct8x8InferenceFlag', 1),
                        ('frameCroppingFlag', 0), ('vuiParametersPresentFlag', 1),
                        ('aspectRatioInfoPresentFlag', 0), ('overscanInfoPresentFlag', 0),
                        ('videoSignalTypePresentFlag', 0), ('chromaLocInfoPresentFlag', 0),
                        ('timingInfoPresentFlag', 1)]:
        b.u(name, value)
    b.u('numUnitsInTick', 1001, 32)
    b.u('timeScale', 60000, 32)
    b.u('fixedFrameRateFlag', 1)
    for kind, config in [('nal', nal), ('vcl', vcl)]:
        b.u(kind + 'HrdParametersPresentFlag', int(config is not None))
        if config is None:
            continue
        prefix = kind + 'Hrd.'
        b.ue(prefix + 'cpbCntMinus1', config['count'] - 1)
        b.u(prefix + 'bitRateScale', 0, 4)
        b.u(prefix + 'cpbSizeScale', 0, 4)
        for index in range(config['count']):
            b.ue(prefix + f'bitRateValueMinus1[{index}]', 15 + index)
            # H.264 E.2.2: CPB sizes are non-increasing as the schedule
            # index grows, while the bit rates are strictly increasing.
            b.ue(prefix + f'cpbSizeValueMinus1[{index}]', 31 - index)
            b.u(prefix + f'cbrFlag[{index}]', index % 2)
        for name, key in [('initialCpbRemovalDelayLengthMinus1', 'initial'),
                          ('cpbRemovalDelayLengthMinus1', 'cpb'), ('dpbOutputDelayLengthMinus1', 'dpb')]:
            b.u(prefix + name, config[key] - 1, 5)
        b.u(prefix + 'timeOffsetLength', config['offset'], 5)
    if nal is not None or vcl is not None:
        b.u('lowDelayHrdFlag', 0)
    b.u('picStructPresentFlag', int(pic_struct))
    b.u('bitstreamRestrictionFlag', 0)
    return b.nal(0x67), {'id': sps_id, 'nal': nal, 'vcl': vcl, 'picStruct': pic_struct}


def payload(syntax):
    bits = syntax.bits
    # payload_bit_equal_to_one is present only when syntax does not end on
    # a byte boundary; the outer SEI has its own rbsp_trailing_bits.
    if len(bits) % 8:
        bits += '1' + '0' * (-(len(bits) + 1) % 8)
    return bytes(int(bits[i:i + 8], 2) for i in range(0, len(bits), 8))


def buffering(config, sps_id=None):
    b = Syntax()
    b.ue('sequenceParameterSetId', config['id'] if sps_id is None else sps_id)
    for kind in ('nal', 'vcl'):
        h = config[kind]
        if h is None:
            continue
        for i in range(h['count']):
            b.u(f'{kind}InitialDelays[{i}].initialCpbRemovalDelay', (17 + i * 11) % (1 << h['initial']), h['initial'])
            b.u(f'{kind}InitialDelays[{i}].initialCpbRemovalDelayOffset', (3 + i * 7) % (1 << h['initial']), h['initial'])
    return 0, payload(b), b.fields


def picture(config, structure=5, seconds=42, ct_type=1, counting_type=4, offset=-17):
    b = Syntax()
    h = config['nal'] or config['vcl']
    if h:
        b.u('cpbRemovalDelay', min(37, (1 << h['cpb']) - 1), h['cpb'])
        b.u('dpbOutputDelay', min(19, (1 << h['dpb']) - 1), h['dpb'])
    if config['picStruct']:
        b.u('picStruct', structure, 4)
        for index in range([1, 1, 1, 2, 2, 3, 3, 2, 3][structure] if structure < 9 else 0):
            prefix = f'clockTimestamps[{index}].'
            b.u(prefix + 'clockTimestampFlag', int(index != 1))
            if index == 1:
                continue
            for name, value, width in [('ctType', ct_type, 2), ('nuitFieldBasedFlag', index % 2, 1),
                                       ('countingType', counting_type, 5), ('fullTimestampFlag', int(index == 0), 1),
                                       ('discontinuityFlag', 0, 1), ('cntDroppedFlag', 1, 1), ('nFrames', 11, 8)]:
                b.u(prefix + name, value, width)
            if index == 0:
                b.u(prefix + 'secondsValue', seconds, 6)
                b.u(prefix + 'minutesValue', 21, 6)
                b.u(prefix + 'hoursValue', 7, 5)
            else:
                for name, value, width in [('secondsFlag', 1, 1), ('secondsValue', 3, 6),
                                           ('minutesFlag', 1, 1), ('minutesValue', 4, 6), ('hoursFlag', 0, 1)]:
                    b.u(prefix + name, value, width)
            width = h['offset'] if h else 24
            if width:
                value = offset if index == 0 else 5
                b.add(prefix + 'timeOffset', value, f'{value % (1 << width):0{width}b}')
    return 1, payload(b), b.fields


def sei(messages):
    raw, fields = bytearray(), []
    for index, (kind, body, syntax) in enumerate(messages):
        raw.extend(extend_255(kind) + extend_255(len(body)))
        begin = len(raw) * 8
        for f in syntax:
            typed = 'bufferingPeriod' if kind == 0 else 'pictureTiming'
            fields.append({**f, 'name': f'messages[{index}].{typed}.' + f['name'], 'bit': f['bit'] + begin})
        raw.extend(body)
    raw.append(128)
    return escape_rbsp(raw), fields


def config_record(sps_list):
    picture_set = pps(cabac=False).nal(0x68)
    header = b'\x01\x42\x00\x1e\xff' + bytes([0xe0 | len(sps_list)])
    header += b''.join(len(n).to_bytes(2, 'big') + n for n in sps_list)
    header += b'\x01' + len(picture_set).to_bytes(2, 'big') + picture_set
    return 9, b'\x17\x00\0\0\0' + header, 0


def data_record(nals, timestamp=40):
    return 9, b'\x27\x01\0\0\0' + b''.join(len(n).to_bytes(4, 'big') + n for n in nals), timestamp


def truncated(message, count):
    kind, body, fields = message
    return kind, body[:count], [f for f in fields if f['bit'] - 8 + len(f['bits']) <= count * 8]


def generate(directory):
    expected = {'schemaVersion': 1, 'sources': {}, 'seiTiming': {}, 'mp3Navigation': {},
                'references': [REFERENCE], 'provenance': 'Authored bits and original-byte maps, independent of product output.'}
    frames = [mp3_frame(i, 0 if i < 2 else 511) for i in range(3000)]
    data = make_flv([(8, b'\x2f' + b''.join(frames), 0)])
    name = 'mp3-navigation-3000.flv'
    (directory / name).write_bytes(data)
    expected['mp3Navigation']['long'] = {'file': name, **mp3_oracle(data, {(0, n) for n in (0, 63, 64, 65, 255, 2600)})}
    dual_nal, dual = timing_sps(nal=hrd(), vcl=hrd(count=1))
    nal_nal, nal = timing_sps(nal=hrd())
    vcl_nal, vcl = timing_sps(vcl=hrd(count=1, initial=6, cpb=6, dpb=5, offset=5))
    plain_nal, plain = timing_sps()
    zero_nal, zero = timing_sps(nal=hrd(offset=0), pic_struct=False)
    changed_nal, changed = timing_sps(nal=hrd(initial=9, cpb=10, dpb=9, offset=8))
    wide_nal, wide = timing_sps(nal=hrd(initial=32, cpb=32, dpb=32, offset=31))
    other_nal, _ = timing_sps(sps_id=1, nal=hrd())
    specifications = {
        'dual-hrd': ([dual_nal], [], [buffering(dual), picture(dual)], True),
        'nal-hrd': ([nal_nal], [], [buffering(nal), picture(nal, structure=0)], True),
        'vcl-hrd': ([vcl_nal], [], [buffering(vcl), picture(vcl, structure=0, offset=-9)], True),
        'no-hrd-clocks': ([plain_nal], [], [buffering(plain), picture(plain, structure=0, offset=-12345)], True),
        'delays-only': ([zero_nal], [], [buffering(zero), picture(zero)], True),
        'replacement': ([nal_nal], [changed_nal], [buffering(changed), picture(changed)], True),
        'wide-epb': ([wide_nal], [], [buffering(wide), picture(wide, structure=0, offset=-1073741824)], True),
        'unknown-sps': ([nal_nal], [], [buffering(nal, sps_id=3)], False),
        'ambiguous-sps': ([nal_nal, other_nal], [], [picture(nal)], False),
        'unactivated-sps': ([nal_nal], [], [picture(nal)], False),
        'truncated-buffering': ([nal_nal], [], [truncated(buffering(nal), 1)], False),
        'truncated-picture': ([nal_nal], [], [buffering(nal), truncated(picture(nal), 1)], False),
        'invalid-structure': ([nal_nal], [], [buffering(nal), picture(nal, structure=9)], False),
        'invalid-clock-type': ([nal_nal], [], [buffering(nal), picture(nal, structure=0, ct_type=3)], False),
        'invalid-counting-type': ([nal_nal], [], [buffering(nal), picture(nal, structure=0, counting_type=7)], False),
        'invalid-seconds': ([nal_nal], [], [buffering(nal), picture(nal, structure=0, seconds=63)], False),
        'message-budget': ([nal_nal], [], [buffering(nal)] + [picture(nal, structure=0)] * 64, False),
    }
    def write_case(case_name, sets, prefix, messages, complete, prior=(), suffix=(), config=None, source='bufferingPeriod'):
        unit, fields = sei(messages)
        data = make_flv([config_record(sets), *prior, data_record(prefix + [unit] + list(suffix), 40 * (len(prior) + 1))])
        filename = 'avc-timing-' + case_name + '.flv'
        (directory / filename).write_bytes(data)
        tag = flv_tags(data)[-1]
        offset = tag['payloadOffset'] + 5 + sum(4 + len(n) for n in prefix) + 4
        require(data[offset:offset + len(unit)] == unit, 'SEI source offset mismatch')
        mapped = map_trace_fields(fields, unit, offset)
        nal_rows = avc_nals(data, flv_tags(data))[1]
        sps_rows = [row for row in nal_rows if row['type'] == 7]
        selected_sps = sps_rows[-1] if prefix else sps_rows[0]
        framing = sei_oracle(unit, offset)
        activation = None
        if source == 'slice':
            n = next(row for row in nal_rows if row['tagId'] == tag['tagId'] and row['type'] in (1, 5))
            activation = {'byteOffset': n['offset'], 'bitOffset': 0, 'bitLength': n['bytes'] * 8}
        elif any(m[0] == 0 for m in messages):
            activation = next(m['payloadSpan'] for m in framing['messages'] if m['payloadType']['value'] == 0)
        elif prior:
            n = next(row for row in nal_rows if row['tagId'] == 1 and row['type'] == 6)
            activation = sei_oracle(data[n['offset']:n['offset'] + n['bytes']], n['offset'])['messages'][0]['payloadSpan']
        if activation and 'sourceBitSegments' in activation:
            first, last = activation['sourceBitSegments'][0], activation['sourceBitSegments'][-1]
            activation = {**first, 'bitLength': (last['byteOffset'] - first['byteOffset']) * 8 +
                          last['bitOffset'] + last['bitLength'] - first['bitOffset']}
        expected['seiTiming'][case_name] = {'file': filename, 'tagId': tag['tagId'], 'nalIndex': len(prefix),
            'offset': offset, 'complete': complete, 'budgetLimited': case_name == 'message-budget',
            'fields': mapped, 'framing': framing, 'messageTypes': [m[0] for m in messages],
            'spsSpan': {'byteOffset': selected_sps['offset'], 'bitOffset': 0, 'bitLength': selected_sps['bytes'] * 8},
            'timingParameters': config, 'activationSource': source}
        if activation:
            expected['seiTiming'][case_name]['activationSpan'] = activation
    for case_name, (sets, prefix, messages, complete) in specifications.items():
        config = {'dual-hrd': dual, 'vcl-hrd': vcl, 'no-hrd-clocks': plain, 'delays-only': zero,
                  'replacement': changed, 'wide-epb': wide}.get(case_name, nal)
        write_case(case_name, sets, prefix, messages, complete, config=config)
    bp = sei([buffering(nal)])[0]
    write_case('cross-tag', [nal_nal], [], [picture(nal)], True, prior=[data_record([bp])], config=nal)
    write_case('replacement-unactivated', [nal_nal], [changed_nal], [picture(changed)], False,
               prior=[data_record([bp])], config=changed)
    intra = slice_header('I', cabac=False).nal(0x65, b'\x55' * 16)
    write_case('same-packet-slice', [nal_nal], [], [picture(nal)], True, suffix=[intra], config=nal, source='slice')
    for path in sorted(directory.glob('*.flv')):
        expected['sources'][path.name] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    save(directory, 'expectations.json', expected)
    result = {'passed': True, 'files': len(expected['sources']), 'seiTimingCases': len(expected['seiTiming']),
              'expectationsSHA256': digest(directory / 'expectations.json')}
    save(directory, 'result.json', result)
    return result


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
