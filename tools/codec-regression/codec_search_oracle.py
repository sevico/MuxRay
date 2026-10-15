#!/usr/bin/env python3
"""Author codec-search fixtures and physical evidence without product parsers."""

import argparse
import json

from generate import avc_nals, digest, flv_tags, new_directory, require, save
from paged_codec_oracle import mp3_frame, mp3_oracle, sei_oracle
from search_history_oracle import make_flv
from timing_navigation_oracle import buffering, config_record, data_record, hrd, picture, sei, timing_sps


def span(offset, bits):
    return {'byteOffset': str(offset), 'bitOffset': 0, 'bitLength': bits}


def envelope(mapped):
    segments = [s for field in mapped for s in field['sourceBitSegments']]
    start = min(s['byteOffset'] * 8 + s['bitOffset'] for s in segments)
    end = max(s['byteOffset'] * 8 + s['bitOffset'] + s['bitLength'] for s in segments)
    return {'byteOffset': str(start // 8), 'bitOffset': start % 8, 'bitLength': end - start}


def base_row(tag, offset, kind, status, complete, source_span, frame=None, nal=None, message=None):
    return dict(tagId=str(tag['tagId']), dts=str(tag['dts']), offset=str(offset), frameIndex=frame,
                nalIndex=nal, messageIndex=message, kind=kind, status=status, contextState='', complete=complete,
                span=source_span, spsSpan=None, evidenceSpan=None, values=[])


def parameters(prefix, mono=False, bitrate=128):
    values = [('version', 'mpeg1'), ('sampleRateHz', '44100'),
              ('channelMode', 'singleChannel' if mono else 'jointStereo'), ('channels', '1' if mono else '2'),
              ('samplesPerFrame', '1152')]
    if bitrate is not None:
        values.append(('bitrateKbps', str(bitrate)))
    return [{'name': prefix + '.' + name, 'value': value} for name, value in values]


def retained(rows, kind, filter_name):
    if filter_name == 'all':
        return rows
    if kind == 'mp3':
        wanted = {'parameters': ('parameters', 'parametersAndBitrate'), 'bitrate': ('bitrate', 'parametersAndBitrate'), 'stops': ('stop',)}[filter_name]
        return [r for r in rows if r['kind'] in wanted]
    return [r for r in rows if r['kind'] == 'coverage' or
            (not r['complete'] or r['contextState'] in ('missing', 'ambiguous') if filter_name == 'unknown' else r['kind'] == filter_name)]


def generate(directory):
    manifest = {'schemaVersion': 1, 'sources': {}, 'codecSearch': {},
                'provenance': 'Authored MPEG headers, FLV envelopes and SEI syntax. No product output is used as an oracle.',
                'limits': ['Structural samples have opaque audio/picture bodies and are not decode-success samples.']}

    def record(name, data, kind, rows, filters, pages, **extra):
        filename = name + '.flv'
        (directory / filename).write_bytes(data)
        manifest['sources'][filename] = {'bytes': len(data), 'sha256': digest(directory / filename)}
        manifest['codecSearch'][name] = dict(file=filename, kind=kind, rows=rows, filters=filters, pages=pages,
                                           indexedTags=len(flv_tags(data)), **extra)

    frames = [mp3_frame(i, mono=i >= 2600) for i in range(3000)]
    data = make_flv([(8, b'\x2f' + b''.join(frames), 0)])
    tag = flv_tags(data)[0]
    offset = tag['payloadOffset'] + 1 + sum(map(len, frames[:2600]))
    row = base_row(tag, offset, 'parameters', 'complete', True, span(offset, len(frames[2600]) * 8), frame=2600)
    row['values'] = parameters('current', mono=True) + parameters('previous')
    record('mp3-late-change', data, 'mp3', [row], ['parameters'], {'parameters': [0, 0, 1]},
           positions=[[0, 0], [0, 1024], [0, 2048], [1, 0]], selectedFrame=2600,
           frameOracle=mp3_oracle(data, {(0, 2600)}), sourceTarget='0:2600:-:-', sourceKinds=['hex'])

    variable = bytes.fromhex('fffba064') + bytes(522 - 4)  # 144000*160//44100, no padding.
    bodies = [b'\x2f' + mp3_frame(0) + variable + mp3_frame(2), b'\x2f\xff\xfb',
              b'\x2f\x00\x00\x00\x00', b'\x2f\xff\xfd\x90\x64' + bytes(8),
              b'\x2f\xff\xfb\x00\x64' + bytes(8), b'\x2f\xff\xfb\x90\x64' + bytes(8)]
    data = make_flv([(8, body, i * 40) for i, body in enumerate(bodies)])
    tags, rows = flv_tags(data), []
    for index, off, size, current, previous in [(1, 417, 522, 160, 128), (2, 417 + 522, 417, 128, 160)]:
        start = tags[0]['payloadOffset'] + 1 + off
        row = base_row(tags[0], start, 'bitrate', 'complete', True, span(start, size * 8), frame=index)
        row['values'] = parameters('current', bitrate=current) + parameters('previous', bitrate=previous)
        rows.append(row)
    for tag, status in zip(tags[1:], ['truncatedHeader', 'malformedHeader', 'unsupportedLayer', 'freeFormat', 'truncatedFrame']):
        start = tag['payloadOffset'] + 1
        row = base_row(tag, start, 'stop', status, False, span(start, (tag['payloadEnd'] - start) * 8), frame=0)
        if status in ('freeFormat', 'truncatedFrame'):
            row['values'] = parameters('current', bitrate=None if status == 'freeFormat' else 128)
        rows.append(row)
    record('mp3-bitrate-stops', data, 'mp3', rows, ['all', 'bitrate', 'stops'], {'all': [7], 'bitrate': [2], 'stops': [5]},
           sourceTarget='5:0:-:-', sourceKinds=['hex'])

    sps, config = timing_sps(nal=hrd(count=1))

    def sei_rows(data, timing=False, missing=False, budget=False):
        tags = flv_tags(data)
        nals = avc_nals(data, tags)[1]
        sps_row = next(n for n in nals if n['type'] == 7)
        sps_span, activation, result = span(sps_row['offset'], sps_row['bytes'] * 8), None, []
        for tag in tags:
            local = [n for n in nals if n['tagId'] == tag['tagId']]
            for nal_index, nal in enumerate(local):
                if nal['type'] != 6:
                    continue
                parsed = sei_oracle(data[nal['offset']:nal['offset'] + nal['bytes']], nal['offset'])
                for message in parsed['messages'][:64]:
                    payload_type = message['payloadType']['value']
                    source_span = envelope([message['payloadType'], message['payloadSize'], message['payloadSpan']])
                    kind = {0: 'bufferingPeriod', 1: 'pictureTiming'}.get(payload_type, 'unknown')
                    row = base_row(tag, tag['offset'], kind, 'opaque' if kind == 'unknown' else 'unknown' if missing else 'complete',
                                   not missing, source_span, nal=nal_index, message=message['index'])
                    row['contextState'] = 'not_applicable' if kind == 'unknown' else 'missing' if missing else 'resolved'
                    row['values'] = [{'name': 'payloadType', 'value': str(payload_type)}]
                    if timing:
                        if payload_type == 0:
                            activation = envelope([message['payloadSpan']])
                        row.update(spsSpan=sps_span, evidenceSpan=activation)
                        row['values'] += [{'name': 'spsId', 'value': '0'}, {'name': 'contextSource', 'value': 'bufferingPeriod'}]
                    if missing:
                        row['values'].append({'name': 'reason', 'value': 'timing_context_missing'})
                    elif kind == 'unknown':
                        row['values'].append({'name': 'reason', 'value': 'unsupported_payload_type'})
                    result.append(row)
            if budget and local and any(n['type'] == 6 for n in local):
                row = base_row(tag, tag['offset'], 'coverage', 'unknown', False,
                               span(tag['payloadOffset'], (tag['payloadEnd'] - tag['payloadOffset']) * 8))
                row['contextState'] = 'not_applicable'
                row['values'] = [{'name': 'reason', 'value': 'sei_message_limit'}]
                result.append(row)
        return result

    units = [sei([(3, bytes([64 + i % 64]), []) for i in range(count)])[0] for count in (64, 64, 2)]
    data = make_flv([config_record([sps]), data_record(units)])
    rows = sei_rows(data)
    record('sei-pagination', data, 'sei', rows, ['all'], {'all': [64, 64, 2]}, zeroIOPages=[1, 2],
           sourceTarget='1:-:2:1', sourceKinds=['hex'])

    bp = sei([buffering(config)])[0]
    pt = sei([picture(config, structure=0)])[0]
    data = make_flv([config_record([sps]), data_record([bp]), *[(18, b'\x05', 80 + i) for i in range(256)], data_record([pt], 400)])
    rows = sei_rows(data, timing=True)
    record('sei-cross-256', data, 'sei', rows, ['all', 'bufferingPeriod', 'pictureTiming'],
           {'all': [1, 1], 'bufferingPeriod': [1, 0], 'pictureTiming': [0, 1]},
           sourceTarget='258:-:0:0', sourceKinds=['hex', 'sps', 'evidence'])

    data = make_flv([config_record([sps]), data_record([pt])])
    rows = sei_rows(data, missing=True)
    record('sei-missing', data, 'sei', rows, ['unknown'], {'unknown': [1]}, sourceTarget='1:-:0:0', sourceKinds=['hex'])

    data = make_flv([config_record([sps]), data_record([sei([(3, b'\x80', [])] * 65)[0]])])
    rows = sei_rows(data, budget=True)
    record('sei-budget', data, 'sei', rows, ['all', 'unknown', 'pictureTiming'],
           {'all': [64, 1], 'unknown': [1], 'pictureTiming': [1]}, sourceTarget='1:-:-:-', sourceKinds=['hex'])

    for case in manifest['codecSearch'].values():
        for name in case['filters']:
            require(sum(case['pages'][name]) == len(retained(case['rows'], case['kind'], name)), 'authored filter/page counts differ')
    save(directory, 'expectations.json', manifest)
    result = {'passed': True, 'files': len(manifest['sources']), 'rows': sum(len(c['rows']) for c in manifest['codecSearch'].values()),
              'expectationsSHA256': digest(directory / 'expectations.json')}
    save(directory, 'result.json', result)
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    args = parser.parse_args(argv)
    args.output_dir.mkdir(parents=True)
    print(json.dumps(generate(args.output_dir), indent=2))


if __name__ == '__main__':
    main()
