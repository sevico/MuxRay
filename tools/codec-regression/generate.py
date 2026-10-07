#!/usr/bin/env python3
"""Generate small codec regression inputs and independent byte/FFmpeg oracles."""

import argparse
import datetime
import hashlib
import json
from pathlib import Path
import re
import shutil
import subprocess
import sys
import traceback


ROOT = Path(__file__).resolve().parents[2]
H264 = 'h264-sar-bt709-hrd.flv'
BFRAMES = 'h264-bframes-multislice.flv'
CRC = 'crc-match-mismatch-structural.flv'
MP3_NAMES = ('xing-vbr', 'info-cbr', 'xing-vbr-tail-truncated')


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def save(directory, name, value):
    (directory / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def executable(value):
    found = shutil.which(str(value))
    if not found:
        raise argparse.ArgumentTypeError(f'executable not found: {value}')
    return Path(found).resolve()


def new_directory(value):
    path = Path(value).resolve()
    if path.exists():
        raise argparse.ArgumentTypeError(f'output already exists; preserve evidence and choose a new directory: {path}')
    return path


class Commands:
    """Persist command metadata even when a tool fails or times out."""
    def __init__(self, directory):
        self.directory = directory
        self.rows = []

    def run(self, name, argv, expected=0, timeout=90, cwd=None):
        directory = self.directory
        stdout, stderr = directory / (name + '.stdout.log'), directory / (name + '.stderr.log')
        row = {'name': name, 'argv': [str(v) for v in argv], 'cwd': str(cwd or directory),
               'startedUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(),
               'stdout': stdout.name, 'stderr': stderr.name, 'expectedExitCode': expected}
        self.rows.append(row)
        try:
            with stdout.open('wb') as out, stderr.open('wb') as err:
                process = subprocess.run(row['argv'], cwd=row['cwd'], stdout=out, stderr=err, timeout=timeout)
            row['exitCode'] = process.returncode
        except BaseException as error:
            row['error'] = str(error)
            raise
        finally:
            save(directory, 'commands.json', self.rows)
        require(row['exitCode'] == expected,
                f'{name}: expected exit {expected}, got {row["exitCode"]}; see {stderr}')
        return stdout


def wrap_mp3(data):
    payload = b'\x2f' + data
    return (b'FLV\x01\x04\x00\x00\x00\x09' + b'\0' * 4 + b'\x08' +
            len(payload).to_bytes(3, 'big') + b'\0' * 7 + payload + (11 + len(payload)).to_bytes(4, 'big'))


def flv_tags(data):
    require(len(data) >= 13 and data[:4] == b'FLV\x01', 'invalid FLV header')
    start = int.from_bytes(data[5:9], 'big')
    require(start >= 9 and data[start:start + 4] == b'\0' * 4, 'invalid first PreviousTagSize')
    tags, pos = [], start + 4
    while pos < len(data):
        require(pos + 15 <= len(data), 'truncated FLV Tag')
        length = int.from_bytes(data[pos + 1:pos + 4], 'big')
        end = pos + 11 + length
        require(end + 4 <= len(data), 'FLV payload exceeds source')
        require(int.from_bytes(data[end:end + 4], 'big') == 11 + length, 'invalid PreviousTagSize')
        require(data[pos + 8:pos + 11] == b'\0' * 3, 'nonzero FLV stream ID')
        tags.append({'tagId': len(tags), 'offset': pos, 'type': data[pos] & 31, 'dataSize': length,
                     'dts': int.from_bytes(data[pos + 4:pos + 7], 'big') + (data[pos + 7] << 24),
                     'payloadOffset': pos + 11, 'payloadEnd': end})
        pos = end + 4
    return tags


def crc_reference(data):
    # Polynomial long division, independent of the product byte-feedback loop.
    bits = (0xffff << (len(data) * 8)) ^ (int.from_bytes(data, 'big') << 16)
    polynomial = 0x18005
    for shift in range(bits.bit_length() - 17, -1, -1):
        if bits & (1 << (shift + 16)):
            bits ^= polynomial << shift
    return bits


def structural_crc():
    frame = bytearray(417)
    frame[:4] = bytes.fromhex('fffa9064')
    computed = crc_reference(frame[2:4] + frame[6:38])
    require(computed == 0x576f, 'CRC independent reference vector differs')
    frame[4:6] = computed.to_bytes(2, 'big')
    corrupted = bytearray(frame)
    corrupted[5] ^= 1
    return bytes(frame + corrupted)


def mp3_frames(data, source_offset=25):
    frames, pos = [], 0
    while pos < len(data):
        require(len(data) - pos >= 4, 'oracle needs a complete final MP3 header')
        raw = data[pos:pos + 4]
        h = int.from_bytes(raw, 'big')
        version, layer, bi, si = (h >> 19) & 3, (h >> 17) & 3, (h >> 12) & 15, (h >> 10) & 3
        require(h >> 21 == 2047 and version != 1 and layer == 1 and bi not in (0, 15) and si != 3,
                'invalid or free-format MP3 oracle header')
        bitrate = ([0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320] if version == 3 else
                   [0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160])[bi] * 1000
        sample_rate = [44100, 48000, 32000][si] // {3: 1, 2: 2, 0: 4}[version]
        length = (144 if version == 3 else 72) * bitrate // sample_rate + ((h >> 9) & 1)
        available = min(length, len(data) - pos)
        channels, protected = (1 if (h >> 6) & 3 == 3 else 2), not bool((h >> 16) & 1)
        crc = {'status': 'not_protected'}
        if protected:
            side_size = (17 if channels == 1 else 32) if version == 3 else (9 if channels == 1 else 17)
            require(available >= 6 + side_size, 'CRC oracle prefix is truncated')
            stored = int.from_bytes(data[pos + 4:pos + 6], 'big')
            computed = crc_reference(raw[2:] + data[pos + 6:pos + 6 + side_size])
            crc = {'status': 'match' if stored == computed else 'mismatch', 'stored': stored, 'computed': computed}
        frames.append({'index': len(frames), 'offset': pos, 'sourceOffset': source_offset + pos,
                       'headerHex': raw.hex(), 'bitrateBps': bitrate, 'sampleRateHz': sample_rate,
                       'length': length, 'availableLength': available, 'complete': available == length,
                       'channels': channels, 'versionBits': version, 'crc': crc})
        if available < length:
            break
        pos += length
    require(frames, 'empty MP3 oracle')
    first = frames[0]
    side = ((17 if first['channels'] == 1 else 32) if first['versionBits'] == 3 else
            (9 if first['channels'] == 1 else 17))
    marker = 4 + side  # Canonical Xing offset does not move for CRC protection.
    vbr = None
    if data[marker:marker + 4] in (b'Xing', b'Info'):
        require(marker + 8 <= first['availableLength'], 'VBR flags exceed first frame')
        flags = int.from_bytes(data[marker + 4:marker + 8], 'big')
        vbr, at = {'kind': data[marker:marker + 4].decode('ascii'), 'offset': source_offset + marker, 'flags': flags}, marker + 8
        require(flags & ~15 == 0, 'unsupported oracle Xing flags')
        for mask, name, size in ((1, 'declaredFrames', 4), (2, 'declaredBytes', 4), (4, 'toc', 100), (8, 'quality', 4)):
            if flags & mask:
                require(at + size <= first['availableLength'], 'VBR field exceeds first frame')
                vbr[name] = {'offset': source_offset + at, 'bytes': size,
                             'value': data[at:at + size].hex() if name == 'toc' else int.from_bytes(data[at:at + size], 'big')}
                at += size
    return {'sourceBytes': len(data), 'sourceOffset': source_offset, 'physicalFrameCount': len(frames),
            'completeFrameCount': sum(f['complete'] for f in frames), 'frames': frames, 'vbr': vbr}


def avc_nals(data, tags):
    nals, configurations, length_size = [], [], None
    for tag in tags:
        begin, end = tag['payloadOffset'], tag['payloadEnd']
        if tag['type'] != 9 or end - begin < 5 or data[begin] & 15 != 7:
            continue
        kind, at = data[begin + 1], begin + 5
        def append_nal(offset, size):
            require(size > 0 and offset + size <= end, 'AVC NAL exceeds FLV payload')
            nals.append({'tagId': tag['tagId'], 'nalIndex': sum(n['tagId'] == tag['tagId'] for n in nals),
                         'offset': offset, 'bytes': size, 'type': data[offset] & 31})
        if kind == 0:
            require(at + 6 <= end and data[at] == 1, 'invalid avcC oracle')
            configurations.append(tag['tagId'])
            length_size, count, at = (data[at + 4] & 3) + 1, data[at + 5] & 31, at + 6
            for group in range(2):
                if group:
                    require(at < end, 'missing PPS count')
                    count, at = data[at], at + 1
                for _ in range(count):
                    require(at + 2 <= end, 'truncated parameter set length')
                    size = int.from_bytes(data[at:at + 2], 'big')
                    append_nal(at + 2, size)
                    at += 2 + size
        elif kind == 1:
            require(length_size in (1, 2, 4), 'missing NAL length oracle context')
            while at < end:
                require(at + length_size <= end, 'truncated NAL prefix')
                size = int.from_bytes(data[at:at + length_size], 'big')
                append_nal(at + length_size, size)
                at += length_size + size
    return configurations, nals


def rbsp_mapping(nal, offset):
    logical, mapping, zeros = bytearray(), [], 0
    for i, value in enumerate(nal):
        if zeros == 2 and value == 3 and i + 1 < len(nal) and nal[i + 1] <= 3:
            zeros = 0
            continue
        logical.append(value)
        mapping.append(offset + i)
        zeros = zeros + 1 if value == 0 else 0
    return ''.join(f'{value:08b}' for value in logical), mapping


def trace_blocks(trace):
    blocks, current = [], None
    for line in trace.splitlines():
        title = re.search(r'\]\s+(Sequence Parameter Set|Picture Parameter Set|Slice Header)\s*$', line)
        if title:
            current = {'kind': title[1], 'fields': []}
            blocks.append(current)
            continue
        match = re.search(r'\]\s+(\d+)\s+(\S+)\s+([01]+)\s+=\s+(-?\d+)\s*$', line)
        if match and current is not None:
            # A new, unsupported NAL begins with its own forbidden_zero_bit.
            if current['fields'] and int(match[1]) == 0:
                current = None
                continue
            current['fields'].append({'name': match[2], 'bit': int(match[1]), 'bits': match[3], 'value': int(match[4])})
    return blocks


def map_trace_fields(fields, nal, offset):
    bits, mapping = rbsp_mapping(nal, offset)
    result = []
    for field in fields:
        start, length = field['bit'], len(field['bits'])
        require(bits[start:start + length] == field['bits'], f'trace/source bit mismatch: {field["name"]}')
        segments = []
        for bit in range(start, start + length):
            absolute = mapping[bit // 8] * 8 + bit % 8
            if segments and segments[-1]['byteOffset'] * 8 + segments[-1]['bitOffset'] + segments[-1]['bitLength'] == absolute:
                segments[-1]['bitLength'] += 1
            else:
                segments.append({'byteOffset': absolute // 8, 'bitOffset': absolute % 8, 'bitLength': 1})
        result.append({'name': field['name'], 'value': field['value'], 'encodedBits': field['bits'],
                       'rbspBitStartIncludingNALHeader': start, 'bitLength': length, 'sourceBitSegments': segments})
    return result


def h264_oracle(data, trace):
    tags = flv_tags(data)
    configurations, nals = avc_nals(data, tags)
    require(configurations, 'no AVC configuration found')
    slices, slice_at, mapped = [n for n in nals if n['type'] in (1, 5)], 0, []
    for block in trace_blocks(trace):
        if block['kind'] == 'Slice Header':
            require(slice_at < len(slices), 'more trace slices than source slices')
            nal, slice_at = slices[slice_at], slice_at + 1
        else:
            nal_type = 7 if block['kind'] == 'Sequence Parameter Set' else 8
            nal = next(n for n in nals if n['type'] == nal_type)
        fields = map_trace_fields(block['fields'], data[nal['offset']:nal['offset'] + nal['bytes']], nal['offset'])
        mapped.append({'kind': block['kind'], **nal, 'fields': fields})
    require(slice_at == len(slices), 'source slice missing from FFmpeg trace')
    sps = next(block for block in mapped if block['kind'] == 'Sequence Parameter Set')
    fields, in_vui = {}, False
    for field in sps['fields']:
        in_vui = in_vui or field['name'] == 'vui_parameters_present_flag'
        if in_vui and not field['name'].startswith('rbsp_'):
            fields[field['name']] = field
    return {'configurationTagId': configurations[0], 'configurationTagIds': configurations,
            'tags': tags, 'nals': nals, 'traceBlocks': mapped, 'vuiFieldsFromFFmpegTrace': fields}


def load_expectations(directory):
    require((directory / 'result.json').is_file(), 'generation result is missing')
    result = json.loads((directory / 'result.json').read_text())
    require(result['passed'], 'generation did not pass')
    require(digest(directory / 'expectations.json') == result['expectationsSHA256'], 'expectations changed since generation')
    expected = json.loads((directory / 'expectations.json').read_text())
    require(expected['schemaVersion'] == 1, 'unsupported expectation version')
    for name, identity in expected['sources'].items():
        path = directory / name
        require(path.is_file() and path.stat().st_size == identity['bytes'] and digest(path) == identity['sha256'],
                f'source changed since generation: {name}')
    return expected


def generate(args, commands):
    out, ffmpeg, ffprobe = args.output_dir, args.ffmpeg, args.ffprobe
    commands.run('ffmpeg-version', [ffmpeg, '-version'])
    commands.run('ffprobe-version', [ffprobe, '-version'])
    commands.run('ffmpeg-encoders', [ffmpeg, '-hide_banner', '-encoders'])
    prefix = [ffmpeg, '-hide_banner', '-nostdin', '-n']
    base = ['-f', 'lavfi', '-i', 'testsrc2=size=160x96:rate=25:duration=0.8', '-an', '-c:v', 'libx264',
            '-threads', '1', '-pix_fmt', 'yuv420p', '-profile:v', 'high', '-level:v', '3.0', '-map_metadata', '-1']
    commands.run('h264-encode', prefix + base + ['-vf', 'setsar=4/3', '-b:v', '128k', '-minrate', '128k',
                 '-maxrate', '128k', '-bufsize', '256k', '-g', '25', '-bf', '0', '-color_range', 'tv',
                 '-colorspace', 'bt709', '-color_primaries', 'bt709', '-color_trc', 'bt709',
                 '-x264-params', 'nal-hrd=cbr:force-cfr=1:pic-struct=1', '-f', 'flv', H264])
    commands.run('h264-bframes-encode', prefix + base + ['-g', '10', '-bf', '2', '-x264-params',
                 'b-adapt=0:scenecut=0:slices=2:threads=1', '-f', 'flv', BFRAMES])
    for name, coding in (('xing-vbr', ['-q:a', '4']), ('info-cbr', ['-b:a', '96k'])):
        commands.run(name + '-encode', prefix + ['-f', 'lavfi', '-i', 'sine=frequency=997:sample_rate=44100:duration=0.2',
                     '-ac', '2', '-c:a', 'libmp3lame', '-threads', '1', *coding, '-id3v2_version', '0',
                     '-write_id3v1', '0', '-write_xing', '1', '-map_metadata', '-1', '-f', 'mp3', name + '.mp3'])
        raw = (out / (name + '.mp3')).read_bytes()
        (out / (name + '-single-tag.flv')).write_bytes(wrap_mp3(raw))
        if name == 'xing-vbr':
            (out / 'xing-vbr-tail-truncated.mp3').write_bytes(raw[:-17])
            (out / 'xing-vbr-tail-truncated-single-tag.flv').write_bytes(wrap_mp3(raw[:-17]))
    (out / CRC).write_bytes(wrap_mp3(structural_crc()))
    valid = [H264, BFRAMES, 'xing-vbr.mp3', 'xing-vbr-single-tag.flv', 'info-cbr.mp3', 'info-cbr-single-tag.flv']
    for name in valid:
        commands.run(name + '-ffprobe', [ffprobe, '-v', 'error', '-count_frames', '-show_format', '-show_streams', '-show_packets', '-of', 'json', name])
        commands.run(name + '-decode', [ffmpeg, '-hide_banner', '-nostdin', '-v', 'error', '-xerror', '-i', name, '-map', '0', '-f', 'null', '-'])
    for name, stem in ((H264, 'h264-trace'), (BFRAMES, 'h264-bframes-trace')):
        commands.run(stem, [ffmpeg, '-hide_banner', '-nostdin', '-i', name, '-map', '0:v:0', '-c:v', 'copy', '-bsf:v', 'trace_headers', '-f', 'null', '-'])
    expected = {'schemaVersion': 1, 'provenance': 'Synthetic inputs encoded by recorded FFmpeg/libx264/libmp3lame. Oracles use direct bytes and FFmpeg trace_headers, never the product parser.',
                'sources': {}, 'flv': {}, 'mp3': {}, 'validDecodeSamples': valid,
                'limitations': ['Tail truncation is a malformed mutation; CRC sample is structural zero-filled main data, not decodable audio.',
                                'No naturally encoded VBRI sample; VBRI remains covered by core unit tests.',
                                'Encoded bytes may vary by installed encoder version. Recorded source hashes identify a run, not cross-version golden output.']}
    for path in sorted(out.iterdir()):
        if path.suffix in ('.flv', '.mp3'):
            expected['sources'][path.name] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
        if path.suffix == '.flv':
            expected['flv'][path.name] = {'tags': flv_tags(path.read_bytes())}
    for name in MP3_NAMES:
        raw, wrapped = (out / (name + '.mp3')).read_bytes(), (out / (name + '-single-tag.flv')).read_bytes()
        require(wrapped[25:-4] == raw and len(flv_tags(wrapped)) == 1, 'MP3 wrapper differs from source')
        expected['mp3'][name] = mp3_frames(raw)
        require(expected['mp3'][name]['vbr'] is not None, 'encoder did not produce Xing/Info')
    expected['mp3']['crc'] = mp3_frames((out / CRC).read_bytes()[25:-4])
    truncated = expected['mp3']['xing-vbr-tail-truncated']
    require(truncated['frames'][-1]['length'] - truncated['frames'][-1]['availableLength'] == 17, 'tail mutation did not truncate inside one frame')
    expected['h264'] = h264_oracle((out / H264).read_bytes(), (out / 'h264-trace.stderr.log').read_text())
    expected['h264BFrames'] = h264_oracle((out / BFRAMES).read_bytes(), (out / 'h264-bframes-trace.stderr.log').read_text())
    vui = expected['h264']['vuiFieldsFromFFmpegTrace']
    for name, value in {'colour_primaries': 1, 'transfer_characteristics': 1, 'matrix_coefficients': 1,
                        'nal_hrd_parameters_present_flag': 1}.items():
        require(vui[name]['value'] == value, f'encoder omitted requested VUI field: {name}')
    aspect = vui['aspect_ratio_idc']['value']
    require(aspect == 14 or aspect == 255 and vui['sar_width']['value'] * 3 == vui['sar_height']['value'] * 4,
            'encoder omitted requested SAR 4:3')
    require(vui['num_units_in_tick']['value'] > 0 and vui['time_scale']['value'] == 50 * vui['num_units_in_tick']['value'],
            'encoder omitted requested timing ratio')
    slices = [b for b in expected['h264BFrames']['traceBlocks'] if b['kind'] == 'Slice Header']
    require(any(f['name'] == 'slice_type' and f['value'] % 5 == 1 for b in slices for f in b['fields']), 'B-frame sample has no B slices')
    require(any(sum(n['tagId'] == b['tagId'] for n in slices) >= 2 for b in slices), 'multi-slice sample has no multi-slice picture packet')
    save(out, 'expectations.json', expected)
    return {'passed': True, 'sources': expected['sources'], 'expectationsSHA256': digest(out / 'expectations.json'),
            'h264TraceFields': len(vui), 'bframeSliceHeaders': len(slices), 'commands': len(commands.rows)}


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
        save(args.output_dir, 'run.json', {'python': sys.version, 'ffmpeg': str(args.ffmpeg), 'ffprobe': str(args.ffprobe),
                                         'scriptSHA256': digest(Path(__file__))})
        result = generate(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
