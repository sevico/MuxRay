#!/usr/bin/env python3
"""Generate bounded-search/history fixtures and independent source-byte oracles.

No product parser/output is used. MP3 windows describe physical main-data slots,
not reservoir consumption; AVC observations inspect NAL headers, not pictures.
"""

import argparse
import hashlib
import json
from pathlib import Path
import sys
import traceback

from generate import Commands, digest, executable, flv_tags, h264_oracle, mp3_frames, new_directory, require, save
from side_info_oracle import stream_oracle, write_transients


def make_flv(records):
    result = bytearray(b'FLV\x01\x05\x00\x00\x00\x09' + bytes(4))
    for kind, body, dts in records:
        require(0 <= dts < 1 << 32 and len(body) < 1 << 24, 'FLV record exceeds fixed-width fields')
        result.extend(bytes([kind]) + len(body).to_bytes(3, 'big') + (dts & 0xffffff).to_bytes(3, 'big') +
                      bytes([dts >> 24]) + bytes(3) + body + (11 + len(body)).to_bytes(4, 'big'))
    return bytes(result)


def records(data):
    return [(data[t['offset']], data[t['payloadOffset']:t['payloadEnd']], t['dts']) for t in flv_tags(data)]


def source_span(offset, length, bit_offset=0):
    return {'byteOffset': offset, 'bitOffset': bit_offset, 'bitLength': length}


def mp3_history(data):
    """Walk known frame boundaries; map the suffix of prior slots back to FLV."""
    history, rows, previous_format, barrier = [], [], None, 'file_start'
    for tag in flv_tags(data):
        body = data[tag['payloadOffset']:tag['payloadEnd']]
        if tag['type'] != 8:
            continue  # Video and metadata interleave without altering MP3 bytes.
        if data[tag['offset']] & 0x20 or not body or body[0] >> 4 != 2:
            history, previous_format, barrier = [], None, 'other_or_filtered_audio'
            continue
        try:
            frames = stream_oracle(body[1:], tag['payloadOffset'] + 1)['frames']
        except RuntimeError:
            history, previous_format, barrier = [], None, 'invalid_frame_boundary'
            continue
        for frame in frames:
            fmt = (frame['versionBits'], frame['sampleRateHz'], frame['channels'])
            if previous_format is not None and previous_format != fmt:
                history, barrier = [], 'format_change'
            previous_format = fmt
            side = frame['sideInfo']
            area = frame['mainDataArea']
            row = {'tagId': tag['tagId'], 'frameIndex': frame['index'], 'streamFrameIndex': len(rows),
                   'frameSpan': source_span(frame['sourceOffset'], frame['availableLength'] * 8),
                   'mainDataSpan': source_span(area['byteOffset'], area['availableBytes'] * 8),
                   'historyOrigin': barrier, 'priorSlotBytes': sum(r['byteLength'] for r in history),
                   'ranges': [], 'state': 'unknown'}
            rows.append(row)
            back = side.get('main_data_begin')
            usable = side['valid'] and frame['complete'] and frame['crc']['status'] in ('not_protected', 'match')
            if usable:
                usable = side['part23TotalBits'] <= (back + area['availableBytes']) * 8
            if not usable:
                history, barrier = [], 'invalid_or_incomplete_frame'
                continue
            field = next(f for f in side['fields'] if f['path'] == 'main_data_begin')
            remaining, reverse = back, []
            for slot in reversed(history):
                count = min(remaining, slot['byteLength'])
                if count:
                    reverse.append({'tagId': slot['tagId'], 'frameIndex': slot['frameIndex'],
                                    'span': source_span(slot['byteOffset'] + slot['byteLength'] - count, count * 8)})
                    remaining -= count
                if not remaining:
                    break
            row.update(backBytes=back, backpointerSpan=field['span'], missingBytes=remaining,
                       ranges=list(reversed(reverse)), part23Bits=side['part23TotalBits'])
            row['state'] = ('none' if back == 0 else 'incomplete' if remaining else
                            'within_tag' if all(r['tagId'] == tag['tagId'] for r in reverse) else 'across_tags')
            window = b''.join(data[r['span']['byteOffset']:r['span']['byteOffset'] + r['span']['bitLength'] // 8]
                              for r in row['ranges'])
            row['windowSHA256'] = hashlib.sha256(window).hexdigest()
            require(len(window) + remaining == back, 'history oracle lost or duplicated window bytes')
            history.append({'tagId': tag['tagId'], 'frameIndex': frame['index'],
                            'byteOffset': area['byteOffset'], 'byteLength': area['availableBytes']})
    return {'frames': rows, 'frameCount': len(rows)}


def avc_headers(data):
    """Independent length-prefix/NAL-byte observations for complete FLV envelopes."""
    width, rows = None, []
    for tag in flv_tags(data):
        if tag['type'] != 9:
            continue
        body = data[tag['payloadOffset']:tag['payloadEnd']]
        if data[tag['offset']] & 0x20 or len(body) < 5 or body[0] & 15 != 7:
            width = None
            continue
        if body[1] == 0:
            width = (body[9] & 3) + 1 if len(body) >= 11 and body[5] == 1 else None
            if width not in (1, 2, 4):
                width = None
            continue
        if body[1] != 1:
            width = None
            continue
        declaration, at, headers, complete, reason = body[0] >> 4, 5, [], width is not None, ''
        if width is None:
            reason = 'missing_configuration'
        while complete and at < len(body):
            if at + width > len(body):
                complete, reason = False, 'truncated_length'
                break
            length = int.from_bytes(body[at:at + width], 'big')
            at += width
            if not length or at + length > len(body):
                complete, reason = False, 'invalid_nal_boundary'
                break
            value = body[at]
            kind, ref = value & 31, value >> 5 & 3
            usable = not value & 128 and 1 <= kind <= 13
            usable = usable and (ref != 0 if kind in (5, 7, 8) else ref == 0 if kind in (6, 9, 10, 11, 12) else True)
            headers.append({'type': kind, 'usable': usable,
                            'span': source_span(tag['payloadOffset'] + at, 5, 3)})
            at += length
        usable = [h for h in headers if h['usable']]
        idrs = [h for h in usable if h['type'] == 5]
        coverage = complete and all(h['usable'] for h in headers)
        observed = 'yes' if idrs else 'no' if coverage else 'unknown'
        relation = ('other_declaration' if declaration not in (1, 2) else 'undetermined' if observed == 'unknown' else
                    ('keyframe_' if declaration == 1 else 'inter_') + ('with_idr' if idrs else 'without_idr'))
        rows.append({'tagId': tag['tagId'], 'offset': tag['offset'], 'dts': tag['dts'],
                     'declaredFrameType': declaration, 'declarationSpan': source_span(tag['payloadOffset'], 4),
                     'observedIDR': observed, 'declarationRelation': relation, 'coverageComplete': coverage,
                     'reason': reason or ('' if coverage else 'unusable_header'), 'headers': headers,
                     'evidenceSpan': (idrs or usable)[0]['span'] if usable else source_span(tag['payloadOffset'], 4)})
    return {'indexedTags': len(flv_tags(data)), 'rows': rows,
            'mismatchTagIds': [r['tagId'] for r in rows if r['declarationRelation'] in ('keyframe_without_idr', 'inter_with_idr')],
            'unknownTagIds': [r['tagId'] for r in rows if r['observedIDR'] == 'unknown']}


def generate(args, commands):
    out, ffmpeg = args.output_dir, args.ffmpeg
    commands.run('ffmpeg-version', [ffmpeg, '-version'])
    commands.run('ffprobe-version', [args.ffprobe, '-version'])
    write_transients(out / 'transients.wav', 44100, 2)
    commands.run('mp3-encode', [ffmpeg, '-hide_banner', '-nostdin', '-n', '-i', 'transients.wav', '-af', 'apad=pad_dur=0.3', '-c:a', 'libmp3lame',
                 '-ar', '44100', '-ac', '2', '-b:a', '64k', '-reservoir', '1', '-write_xing', '0', '-id3v2_version', '0',
                 '-write_id3v1', '0', '-map_metadata', '-1', '-f', 'mp3', 'history.mp3'])
    commands.run('avc-encode', [ffmpeg, '-hide_banner', '-nostdin', '-n', '-f', 'lavfi', '-i',
                 'testsrc2=size=160x96:rate=25:duration=1.2', '-an', '-c:v', 'libx264', '-threads', '1', '-pix_fmt', 'yuv420p',
                 '-g', '10', '-bf', '2', '-x264-params', 'open-gop=1:scenecut=0:b-adapt=0:min-keyint=10:threads=1',
                 '-map_metadata', '-1', '-f', 'flv', 'avc-open-gop.flv'])
    commands.run('avc-trace', [ffmpeg, '-hide_banner', '-nostdin', '-i', 'avc-open-gop.flv', '-map', '0:v:0', '-c:v',
                 'copy', '-bsf:v', 'trace_headers', '-f', 'null', '-'])
    video = (out / 'avc-open-gop.flv').read_bytes()
    trace = h264_oracle(video, (out / 'avc-trace.stderr.log').read_text())
    non_idr_i = sorted({b['tagId'] for b in trace['traceBlocks'] if b['kind'] == 'Slice Header' and b['type'] == 1 and
                        any(f['name'] == 'slice_type' and f['value'] % 5 == 2 for f in b['fields'])})
    require(non_idr_i, 'encoder did not produce a natural non-IDR I slice')
    raw = (out / 'history.mp3').read_bytes()
    frames = mp3_frames(raw, 0)['frames']
    require(all(f['complete'] for f in frames), 'encoded MP3 ended in a partial frame')
    split = [(8, b'\x2f' + raw[f['offset']:f['offset'] + f['length']], i * 1152 * 1000 // 44100)
             for i, f in enumerate(frames)]
    video_records = [r for r in records(video) if r[0] == 9]
    interleaved = sorted(split + video_records, key=lambda r: (r[2], 0 if r[0] == 9 else 1))
    mp3_variants = {'merged': [(8, b'\x2f' + raw, 0)], 'split': split, 'interleaved': interleaved,
                    'audio-barrier': split[:5] + [(8, b'\xaf\x00\x12\x10', split[5][2])] + split[5:],
                    'missing-prefix': split[5:]}
    expected = {'schemaVersion': 1, 'sources': {}, 'mp3Cases': {}, 'avcCases': {},
                'provenance': 'Generated FFmpeg/libmp3lame/libx264 inputs; original source bytes plus independent FFmpeg trace, never product output.',
                'limitations': ['Physical MP3 backward slots only, not reservoir consumption or audio validity.',
                               'AVC declaration relations are header observations, not proof of independent decoding.',
                               'Structural mutations and repeated pagination input are not decode-success samples.']}
    for name, items in mp3_variants.items():
        filename = 'mp3-' + name + '.flv'
        data = make_flv(items)
        (out / filename).write_bytes(data)
        expected['mp3Cases'][name] = {'file': filename, **mp3_history(data)}
    rows = expected['mp3Cases']['merged']['frames']
    candidates = [r['streamFrameIndex'] for r in rows if r.get('backBytes', 0) > 0 and len(r['ranges']) >= 2]
    require(candidates, 'encoder produced no backward window covering two preceding frames')
    selected = candidates[-1]
    expected['selectedStreamFrameIndex'] = selected
    for name in ('split', 'interleaved'):
        other = expected['mp3Cases'][name]['frames']
        require(len(other) == len(rows), 'repacketization changed the frame count')
        for left, right in zip(rows, other):
            require((left['backBytes'], left['missingBytes'], left['windowSHA256']) ==
                    (right['backBytes'], right['missingBytes'], right['windowSHA256']), 'repacketization changed backward-slot bytes')
    original = records(video)
    baseline = avc_headers(video)
    idr_tag = next(r['tagId'] for r in baseline['rows'] if r['observedIDR'] == 'yes')
    ordinary = next(r['tagId'] for r in baseline['rows'] if r['observedIDR'] == 'no' and r['tagId'] not in non_idr_i)
    mutated = list(original)
    for tag, declaration in ((idr_tag, 2), (ordinary, 1)):
        kind, body, dts = mutated[tag]
        mutated[tag] = (kind, bytes([(declaration << 4) | (body[0] & 15)]) + body[1:], dts)
    unknown = list(original)
    kind, body, dts = unknown[ordinary]
    nal = next(n for n in trace['nals'] if n['tagId'] == ordinary)
    at = nal['offset'] - trace['tags'][ordinary]['payloadOffset']
    unknown[ordinary] = (kind, body[:at] + b'\x1f' + body[at + 1:], dts)
    truncated = list(original)
    truncated[ordinary] = (kind, body[:5] + (len(body) + 1).to_bytes(4, 'big') + body[9:], dts)
    positive_partial = list(original)
    kind, body, dts = positive_partial[idr_tag]
    positive_partial[idr_tag] = (kind, body + b'\x00\x00\x00\x01\x1f', dts)
    variants = {'open-gop': original, 'declarations': mutated, 'unknown': unknown, 'truncated': truncated,
                'idr-partial': positive_partial,
                'missing-config': [r for r in original if not (r[0] == 9 and r[1][1] == 0)],
                'pagination': [(kind, body, dts + repetition * 1600) for repetition in range(24)
                               for kind, body, dts in mutated]}
    for name, items in variants.items():
        filename = 'avc-' + name + '.flv'
        data = make_flv(items)
        (out / filename).write_bytes(data)
        expected['avcCases'][name] = {'file': filename, **avc_headers(data)}
    expected['naturalNonIDRITagIds'] = non_idr_i
    expected['naturalNonIDRIFields'] = [{'tagId': block['tagId'], 'nalHeaderSpan': source_span(block['offset'], 5, 3),
                                       'sliceType': field} for block in trace['traceBlocks']
                                      if block['kind'] == 'Slice Header' and block['type'] == 1
                                      for field in block['fields'] if field['name'] == 'slice_type' and field['value'] % 5 == 2]
    expected['traceFile'] = 'avc-trace.stderr.log'
    require(expected['avcCases']['declarations']['mismatchTagIds'] and expected['avcCases']['unknown']['unknownTagIds'], 'AVC mutations did not produce their intended evidence')
    valid = ['history.mp3', 'mp3-merged.flv', 'mp3-split.flv', 'mp3-interleaved.flv', 'avc-open-gop.flv']
    for filename in valid:
        commands.run(filename + '-decode', [ffmpeg, '-hide_banner', '-nostdin', '-v', 'error', '-xerror', '-i', filename, '-f', 'null', '-'])
        commands.run(filename + '-probe', [args.ffprobe, '-v', 'error', '-show_streams', '-show_format', '-of', 'json', filename])
    expected['validDecodeSamples'] = valid
    for path in sorted(out.iterdir()):
        if path.suffix in ('.flv', '.mp3', '.wav'):
            expected['sources'][path.name] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    save(out, 'expectations.json', expected)
    return {'passed': True, 'expectationsSHA256': digest(out / 'expectations.json'), 'mp3Cases': len(mp3_variants),
            'avcCases': len(variants), 'selectedStreamFrameIndex': selected, 'naturalNonIDRITagIds': non_idr_i,
            'sources': expected['sources'], 'traceSHA256': digest(out / expected['traceFile']), 'commands': len(commands.rows)}


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--ffmpeg', type=executable, default='ffmpeg')
    parser.add_argument('--ffprobe', type=executable, default='ffprobe')
    return parser.parse_args(argv)


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)),
                                         'ffmpeg': str(args.ffmpeg), 'ffprobe': str(args.ffprobe)})
        result = generate(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
