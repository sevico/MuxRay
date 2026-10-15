#!/usr/bin/env python3
"""Generate MP3 side-info samples and independent raw-bit expectations.

This is a test oracle, not a decoder. Field layout is cross-checked against
FFmpeg's mp_decode_layer3; no MuxRay parser or generated product output is used.
"""

import argparse
import json
import math
from pathlib import Path
import struct
import sys
import traceback
import wave

from generate import Commands, digest, executable, flv_tags, mp3_frames, new_directory, require, save, structural_crc, wrap_mp3


REFERENCE = 'https://ffmpeg.org/doxygen/8.1/mpegaudiodec__template_8c_source.html'
VERSIONS = (('mpeg1', 44100, 3, '128k'), ('mpeg2', 22050, 2, '64k'), ('mpeg25', 11025, 0, '32k'))


class TruncatedField(Exception):
    pass


class SideBits:
    def __init__(self, data, source_offset):
        self.bits = ''.join(f'{value:08b}' for value in data)
        self.source_offset = source_offset
        self.position = 0
        self.fields = []
        self.missing = None

    def read(self, path, count):
        start = self.position
        if start + count > len(self.bits):
            self.missing = {'path': path, 'bitOffsetInSideInfo': start, 'wantedBits': count,
                            'availableBits': len(self.bits) - start}
            raise TruncatedField(path)
        encoded = self.bits[start:start + count]
        absolute = self.source_offset * 8 + start
        field = {'path': path, 'value': int(encoded, 2), 'encodedBits': encoded,
                 'bitOffsetInSideInfo': start,
                 'span': {'byteOffset': absolute // 8, 'bitOffset': absolute % 8, 'bitLength': count}}
        self.fields.append(field)
        self.position += count
        return field['value']


def parse_side_info(data, version_bits, channels, source_offset=0):
    require(version_bits in (0, 2, 3) and channels in (1, 2), 'invalid side-info layout')
    mpeg1 = version_bits == 3
    expected_bytes = (17 if channels == 1 else 32) if mpeg1 else (9 if channels == 1 else 17)
    reader = SideBits(data[:expected_bytes], source_offset)
    result = {'expectedBytes': expected_bytes, 'availableBytes': min(len(data), expected_bytes),
              'granuleCount': 2 if mpeg1 else 1, 'channels': channels,
              'span': {'byteOffset': source_offset, 'bitOffset': 0, 'bitLength': min(len(data), expected_bytes) * 8},
              'fields': reader.fields, 'granules': [], 'syntaxComplete': False, 'valid': False, 'issues': []}

    def read(path, width, target, key=None):
        value = reader.read(path, width)
        target[key or path.rsplit('.', 1)[-1]] = value
        return value

    try:
        read('main_data_begin', 9 if mpeg1 else 8, result)
        read('private_bits', (5 if channels == 1 else 3) if mpeg1 else channels, result)
        if mpeg1:
            result['scfsi'] = []
            for channel in range(channels):
                result['scfsi'].append([reader.read(f'scfsi[{channel}][{band}]', 1) for band in range(4)])
        for granule in range(result['granuleCount']):
            row = []
            result['granules'].append(row)
            for channel in range(channels):
                prefix = f'granules[{granule}].channels[{channel}]'
                unit = {'granule': granule, 'channel': channel}
                row.append(unit)
                for name, width in (('part2_3_length', 12), ('big_values', 9), ('global_gain', 8),
                                    ('scalefac_compress', 4 if mpeg1 else 9), ('window_switching_flag', 1)):
                    read(prefix + '.' + name, width, unit)
                if unit['big_values'] > 288:
                    result['issues'].append({'kind': 'big_values_out_of_range', 'path': prefix + '.big_values'})
                if unit['window_switching_flag']:
                    read(prefix + '.block_type', 2, unit)
                    read(prefix + '.mixed_block_flag', 1, unit)
                    unit['table_select'] = [reader.read(prefix + f'.table_select[{i}]', 5) for i in range(2)]
                    unit['subblock_gain'] = [reader.read(prefix + f'.subblock_gain[{i}]', 3) for i in range(3)]
                    if unit['block_type'] == 0:
                        result['issues'].append({'kind': 'switched_block_type_zero', 'path': prefix + '.block_type'})
                else:
                    unit['table_select'] = [reader.read(prefix + f'.table_select[{i}]', 5) for i in range(3)]
                    read(prefix + '.region0_count', 4, unit)
                    read(prefix + '.region1_count', 3, unit)
                for i, table in enumerate(unit['table_select']):
                    if table in (4, 14):
                        result['issues'].append({'kind': 'reserved_huffman_table', 'path': prefix + f'.table_select[{i}]'})
                if mpeg1:
                    read(prefix + '.preflag', 1, unit)
                read(prefix + '.scalefac_scale', 1, unit)
                read(prefix + '.count1table_select', 1, unit)
        require(reader.position == expected_bytes * 8, 'oracle side-info layout did not consume its fixed size')
        result['syntaxComplete'] = True
        result['valid'] = not result['issues']
    except TruncatedField:
        result['missingField'] = reader.missing
        result['issues'].append({'kind': 'side_info_truncated', 'path': reader.missing['path']})
    result['bitsConsumed'] = reader.position
    if result['syntaxComplete']:
        result['part23TotalBits'] = sum(unit['part2_3_length'] for row in result['granules'] for unit in row)
    return result


def stream_oracle(data, source_offset=25):
    """Frame slots are physical byte coverage, not unused decoder reservoir bytes."""
    frames = mp3_frames(data, source_offset)['frames']
    prior_slots, prior_format, origin = 0, None, 'tag_start'
    for frame in frames:
        at, available = frame['offset'], frame['availableLength']
        header = int.from_bytes(data[at:at + 4], 'big')
        protected = not bool((header >> 16) & 1)
        prefix = 4 + (2 if protected else 0)
        layout = (frame['versionBits'], frame['channels'], frame['sampleRateHz'])
        if prior_format is not None and layout != prior_format:
            prior_slots, origin = 0, 'format_change'
        prior_format = layout
        side = parse_side_info(data[at + prefix:at + available], frame['versionBits'], frame['channels'], frame['sourceOffset'] + prefix)
        frame['sideInfo'] = side
        area = max(0, available - prefix - side['expectedBytes'])
        declared_area = max(0, frame['length'] - prefix - side['expectedBytes'])
        frame['mainDataArea'] = {'byteOffset': frame['sourceOffset'] + min(available, prefix + side['expectedBytes']),
                                 'availableBytes': area, 'declaredBytes': declared_area}
        dependency = {'priorMainDataSlotBytes': prior_slots, 'historyOrigin': origin,
                      'status': 'unknown', 'meaning': 'Physical same-Tag source coverage only; no Huffman, data consumption or decoder-state validation.'}
        usable = side['valid'] and frame['complete'] and frame['crc']['status'] in ('not_protected', 'match')
        if usable:
            back = side['main_data_begin']
            dependency.update(backBytes=back, missingHistoryBytes=max(0, back - prior_slots),
                              status='within_tag' if back <= prior_slots else 'history_unknown',
                              declaredDataBits=side['part23TotalBits'],
                              maximumBitsFromBackpointerAndCurrentArea=(back + area) * 8,
                              declaredBitsFit=side['part23TotalBits'] <= (back + area) * 8)
        frame['dependency'] = dependency
        if usable and dependency['declaredBitsFit']:
            prior_slots += declared_area
        else:
            prior_slots, origin = 0, 'invalid_or_incomplete_frame'
    return {'physicalFrameCount': len(frames), 'completeFrameCount': sum(f['complete'] for f in frames),
            'frames': frames, 'shortBlockCount': sum(unit.get('block_type') == 2 for f in frames for row in f['sideInfo']['granules'] for unit in row)}


def source_field(data, field, source_offset=0):
    span = field['span']
    offset, bit, width = span['byteOffset'], span['bitOffset'], span['bitLength']
    require(all(type(value) is int for value in (source_offset, offset, bit, width)) and
            source_offset >= 0 and offset >= source_offset and 0 <= bit < 8 and width > 0,
            'invalid oracle field source span: ' + field['path'])
    start = offset - source_offset
    end = start + (bit + width + 7) // 8
    require(end <= len(data), 'oracle field exceeds its source: ' + field['path'])
    bits = ''.join(f'{value:08b}' for value in data[start:end])
    actual = bits[bit:bit + width]
    require(type(field['value']) is int and len(actual) == width and
            actual == field['encodedBits'] and int(actual, 2) == field['value'],
            'oracle field does not map to its original source bits: ' + field['path'])


def patch_field(data, field, value, source_offset=25):
    span = field['span']
    require(0 <= value < 1 << span['bitLength'], 'mutation does not fit the encoded field')
    bits = list(''.join(f'{byte:08b}' for byte in data))
    at = (span['byteOffset'] - source_offset) * 8 + span['bitOffset']
    bits[at:at + span['bitLength']] = f'{value:0{span["bitLength"]}b}'
    return bytes(int(''.join(bits[i:i + 8]), 2) for i in range(0, len(bits), 8))


def wrap(data, sample_rate, channels):
    result = bytearray(wrap_mp3(data))
    result[24] = 0x20 | ({44100: 3, 22050: 2, 11025: 1}[sample_rate] << 2) | 2 | (channels == 2)
    return bytes(result)


def write_transients(path, sample_rate, channels):
    pcm, state = bytearray(), 0x12345678
    for index in range(int(sample_rate * .36)):
        state = (1664525 * state + 1013904223) & 0xffffffff
        noise = (state / 0xffffffff) * 2 - 1
        burst = index % (sample_rate // 10) < sample_rate // 125
        for channel in range(channels):
            time = (index + channel * 3) / sample_rate
            value = (.6 * noise + .25 * math.sin(2 * math.pi * 2137 * time)) if burst else .015 * math.sin(2 * math.pi * 997 * time)
            pcm.extend(struct.pack('<h', round(max(-1, min(1, value)) * 32767)))
    with wave.open(str(path), 'wb') as output:
        output.setnchannels(channels)
        output.setsampwidth(2)
        output.setframerate(sample_rate)
        output.writeframes(pcm)


def generate(args, commands):
    out = args.output_dir
    commands.run('ffmpeg-version', [args.ffmpeg, '-version'])
    commands.run('ffprobe-version', [args.ffprobe, '-version'])
    expected = {'schemaVersion': 1, 'purpose': 'Independent MPEG Layer III side-info raw bits and same-Tag main-data slot coverage.',
                'reference': REFERENCE, 'sources': {}, 'cases': {},
                'limits': ['Generated synthetic audio only. Main-data and audio samples are not decoded by the oracle.',
                           'Slot coverage is not a simulation of the decoder bit reservoir. Missing preceding Tag history remains unknown.',
                           'Only normal encoded inputs are decode-success samples; structural mutations are explicitly separate.']}
    for name, rate, version_bits, bitrate in VERSIONS:
        for channels in (1, 2):
            stem = f'{name}-{"mono" if channels == 1 else "stereo"}'
            wav = out / (stem + '.wav')
            write_transients(wav, rate, channels)
            commands.run(stem + '-encode', [args.ffmpeg, '-hide_banner', '-nostdin', '-n', '-i', wav.name,
                         '-c:a', 'libmp3lame', '-ar', str(rate), '-ac', str(channels), '-b:a', bitrate,
                         '-reservoir', '1', '-write_xing', '0', '-id3v2_version', '0', '-write_id3v1', '0',
                         '-map_metadata', '-1', '-f', 'mp3', stem + '.mp3'])
            raw = (out / (stem + '.mp3')).read_bytes()
            (out / (stem + '.flv')).write_bytes(wrap(raw, rate, channels))
            oracle = stream_oracle(raw)
            require(all(f['versionBits'] == version_bits and f['channels'] == channels and f['sideInfo']['valid'] for f in oracle['frames']), 'encoder layout or independent side-info validation differs')
            require(all(f['dependency']['status'] == 'within_tag' and f['dependency']['declaredBitsFit'] for f in oracle['frames']), 'valid encoded sample has unaccounted physical slots')
            expected['cases'][stem] = {'file': stem + '.flv', 'tagId': 0, 'kind': 'encoded', 'sampleRateHz': rate,
                                        'channels': channels, **oracle}
            for suffix in ('.mp3', '.flv'):
                filename = stem + suffix
                commands.run(filename + '-probe', [args.ffprobe, '-v', 'error', '-show_streams', '-show_format', '-of', 'json', filename])
                commands.run(filename + '-decode', [args.ffmpeg, '-hide_banner', '-nostdin', '-v', 'error', '-xerror', '-i', filename, '-f', 'null', '-'])
    require(sum(case['shortBlockCount'] for case in expected['cases'].values()) > 0, 'encoder did not generate any natural short blocks')
    base = (out / 'mpeg1-stereo.mp3').read_bytes()
    full = expected['cases']['mpeg1-stereo']
    first = full['frames'][0]
    invalid_field = next(f for f in first['sideInfo']['fields'] if f['path'] == 'granules[0].channels[0].big_values')
    short_frame = next(f for f in full['frames'] if any(unit.get('block_type') == 2 for row in f['sideInfo']['granules'] for unit in row))
    short_field = next(f for f in short_frame['sideInfo']['fields'] if f['path'].endswith('.block_type') and f['value'] == 2)
    late = next(f for f in full['frames'][1:] if f['sideInfo']['main_data_begin'] > 0)
    variants = {
        'side-truncated': (base[:4 + first['sideInfo']['expectedBytes'] - 1], 'Remove final side-info byte and the main data; repair only the FLV envelope.'),
        'big-values-invalid': (patch_field(base, invalid_field, 511), 'Replace first big_values with 511 while preserving MP3 frame boundaries.'),
        'switched-block-zero': (patch_field(base, short_field, 0), 'Replace an encoded short block_type with reserved zero while preserving frame boundaries.'),
        'history-outside-tag': (base[late['offset']:], 'Start the Tag at a complete MPEG frame with a nonzero backpointer; prior audio history is deliberately absent.'),
        'main-data-length-invalid': (patch_field(base, next(f for f in first['sideInfo']['fields'] if f['path'] == 'granules[0].channels[0].part2_3_length'), 4095),
                                    'Declare 4095 main-data bits in the first granule, beyond the first frame slots and zero backpointer.'),
        'crc-side-info-structural': (structural_crc(), 'Two structural zero-filled frames with matching then mismatching stored CRC; not a decoded audio sample.'),
    }
    for stem, (raw, operation) in variants.items():
        (out / (stem + '.flv')).write_bytes(wrap(raw, 44100, 2))
        oracle = stream_oracle(raw)
        expected['cases'][stem] = {'file': stem + '.flv', 'tagId': 0, 'kind': 'structural-mutation',
                                    'mutation': operation, 'sampleRateHz': 44100, 'channels': 2, **oracle}
    require(not expected['cases']['side-truncated']['frames'][0]['sideInfo']['syntaxComplete'], 'side-info mutation was not truncated')
    require(any(issue['kind'] == 'big_values_out_of_range' for issue in expected['cases']['big-values-invalid']['frames'][0]['sideInfo']['issues']), 'invalid big_values mutation escaped oracle')
    require(any(issue['kind'] == 'switched_block_type_zero' for frame in expected['cases']['switched-block-zero']['frames'] for issue in frame['sideInfo']['issues']), 'invalid switched block mutation escaped oracle')
    require(expected['cases']['history-outside-tag']['frames'][0]['dependency']['status'] == 'history_unknown', 'missing history was incorrectly treated as known')
    require(not expected['cases']['main-data-length-invalid']['frames'][0]['dependency']['declaredBitsFit'], 'impossible main-data bit demand escaped oracle')
    # Two encoded prefix frames fit comfortably in the desktop field budget.
    # Preserve actual encoded bytes, and verify this complete-frame excerpt by decoding it.
    stem = 'mpeg1-stereo-native-prefix'
    end = full['frames'][1]['offset'] + full['frames'][1]['length']
    (out / (stem + '.flv')).write_bytes(wrap(base[:end], 44100, 2))
    expected['cases'][stem] = {'file': stem + '.flv', 'tagId': 0, 'kind': 'encoded-prefix', 'sampleRateHz': 44100,
                                'channels': 2, 'origin': 'First two complete encoded MPEG frames of mpeg1-stereo.mp3.', **stream_oracle(base[:end])}
    commands.run(stem + '-decode', [args.ffmpeg, '-hide_banner', '-nostdin', '-v', 'error', '-xerror', '-i', stem + '.flv', '-f', 'null', '-'])
    for case in expected['cases'].values():
        path = out / case['file']
        data = path.read_bytes()
        require(len(flv_tags(data)) == 1, 'generated envelope did not contain one complete FLV Tag')
        for frame in case['frames']:
            for field in frame['sideInfo']['fields']:
                source_field(data, field)
    for path in sorted(out.iterdir()):
        if path.suffix in ('.wav', '.mp3', '.flv'):
            expected['sources'][path.name] = {'bytes': path.stat().st_size, 'sha256': digest(path)}
    save(out, 'expectations.json', expected)
    return {'passed': True, 'expectationsSHA256': digest(out / 'expectations.json'),
            'caseCount': len(expected['cases']), 'encodedCases': 6,
            'frames': sum(c['physicalFrameCount'] for c in expected['cases'].values()),
            'shortBlocks': sum(c['shortBlockCount'] for c in expected['cases'].values()), 'commands': len(commands.rows)}


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
