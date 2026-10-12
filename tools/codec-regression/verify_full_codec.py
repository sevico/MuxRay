#!/usr/bin/env python3
"""Compare AVC detail v4 / MP3 summary v3 with independent source evidence."""

from fractions import Fraction
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, H264, BFRAMES, Commands, digest, flv_tags, load_expectations, require, save
from full_codec_oracle import mp3_all_frames
from verify import arguments


def mapped_fields(value):
    fields = {}
    def visit(item):
        if isinstance(item, dict):
            span = item.get('span', {})
            if 'value' in item and 'rbspBitOffset' in span:
                identity = (span['rbspBitOffset'], span['bitLength'])
                require(identity not in fields or fields[identity] == item, 'contradictory duplicated mapped field')
                fields[identity] = item
            else:
                for child in item.values():
                    visit(child)
        elif isinstance(item, list):
            for child in item:
                visit(child)
    visit(value)
    return fields


def compare_full_header(unit, block):
    actual = mapped_fields(unit)
    count, identities = 0, set()
    for field in block['fields']:
        if field['rbspBitStartIncludingNALHeader'] < 8 or field['name'].startswith(('rbsp_', 'cabac_alignment')):
            continue
        identity = (field['rbspBitStartIncludingNALHeader'] - 8, field['bitLength'])
        identities.add(identity)
        require(identity in actual, 'missing full-header field: ' + field['name'])
        got = actual[identity]
        require(got['value'] == field['value'], 'full-header value differs: ' + field['name'])
        require(got['span']['sourceSpans'] == field['sourceBitSegments'], 'full-header source bits differ: ' + field['name'])
        count += 1
    require(set(actual) == identities, 'unexpected encoded full-header fields')
    return count


def verify_slice_views(prefix, full):
    for label, value in [('prefix', prefix), ('full', full)]:
        view = value['rbsp']
        require(view['ebspSpan']['bitLength'] == value['bytesRead'] * 8,
                label + ': RBSP source range differs from its read count')
        require(sum(segment['rbspByteLength'] for segment in view['sourceSegments']) == view['byteLength'],
                label + ': RBSP source mapping does not cover its bytes')
    require(prefix['rbsp']['ebspSpan']['byteOffset'] == full['rbsp']['ebspSpan']['byteOffset'] and
            prefix['bytesRead'] <= full['bytesRead'] and
            prefix['syntaxBits'] == full['rbspBitOffset'] and
            full['headerBits'] == full['rbspBitOffset'] + full['syntaxBits'],
            'prefix and full header boundaries do not join')


def expected_mp3(data, max_frames=1000000, max_events=256):
    raw = mp3_all_frames(data)
    rows = {row['offset']: row for row in raw['frames']}
    stops = {row['offset']: row['reason'] for row in raw['stops']}
    result = dict(complete=True, scanComplete=True, tagCount=0, completeTagCount=0, partialTagCount=0,
                  unknownAudioTagCount=0, unsupportedAudioTagCount=0, soundDataBytes=0, frameBytes=0,
                  unresolvedBytes=0, attemptedHeaderCount=0, headerBytesRead=0, completeFrameCount=0,
                  sampleCount=0, parameterChangeCount=0, bitrateChangeCount=0, truncatedHeaderCount=0,
                  malformedHeaderCount=0, unsupportedLayerCount=0, truncatedFrameCount=0, freeFormatCount=0,
                  budgetStoppedTagCount=0, maxFrames=max_frames, maxEvents=max_events)
    statuses = {'header_truncated': ('truncatedHeader', 'truncatedHeaderCount'),
                'invalid_header': ('malformedHeader', 'malformedHeaderCount'),
                'free_format': ('freeFormat', 'freeFormatCount'),
                'body_truncated': ('truncatedFrame', 'truncatedFrameCount')}
    events, duration, rates, previous = [], Fraction(0), {}, None
    for tag in flv_tags(data):
        body = data[tag['payloadOffset']:tag['payloadEnd']]
        if tag['type'] != 8:
            continue
        require(body and body[0] >> 4 == 2, 'all-frame oracle fixture contains unsupported audio')
        result['tagCount'] += 1
        result['soundDataBytes'] += len(body) - 1
        pos, index = tag['payloadOffset'] + 1, 0
        if previous is not None and tag['dts'] < previous['dts']:
            previous = None
        while pos < tag['payloadEnd']:
            row, stop = rows.get(pos), stops.get(pos)
            event = {'tagId': tag['tagId'], 'frameIndex': index, 'dts': tag['dts']}
            if result['attemptedHeaderCount'] >= max_frames:
                status, counter = 'frameBudget', 'budgetStoppedTagCount'
            else:
                result['attemptedHeaderCount'] += 1
                if tag['payloadEnd'] - pos >= 4:
                    result['headerBytesRead'] += 4
                status, counter = statuses[stop] if stop else ('complete', None)
                if row:
                    event['parameters'] = {
                        'version': {'value': row['version'], 'name': {3: 'MPEG-1', 2: 'MPEG-2', 0: 'MPEG-2.5'}[row['version']]},
                        'samplingFrequencyHz': row['sampleRateHz'],
                        'channelMode': {'value': row['channelMode'], 'name': ['stereo', 'joint_stereo', 'dual_channel', 'single_channel'][row['channelMode']]},
                        'channels': row['channels'], 'samplesPerFrame': row['samples']}
                    if row['bitrateKbps']:
                        event['bitrateKbps'] = row['bitrateKbps']
            length = row['frameLength'] if status == 'complete' else tag['payloadEnd'] - pos
            event.update(status=status, sourceSpan={'byteOffset': pos, 'bitOffset': 0, 'bitLength': length * 8})
            if status == 'complete':
                if previous is not None:
                    changed = tuple(row[k] for k in ('version', 'sampleRateHz', 'channelMode', 'channels', 'samples')) != tuple(previous[k] for k in ('version', 'sampleRateHz', 'channelMode', 'channels', 'samples'))
                    bitrate_changed = row['bitrateKbps'] != previous['bitrateKbps']
                    event.update(parametersChangedFromPrevious=changed, bitrateChangedFromPrevious=bitrate_changed)
                    result['parameterChangeCount'] += int(changed)
                    result['bitrateChangeCount'] += int(bitrate_changed)
                previous = row
                result['frameBytes'] += length
                result['completeFrameCount'] += 1
                result['sampleCount'] += row['samples']
                duration += Fraction(row['samples'], row['sampleRateHz'])
                rate = rates.setdefault(row['sampleRateHz'], {'samplingFrequencyHz': row['sampleRateHz'], 'frameCount': 0, 'sampleCount': 0})
                rate['frameCount'] += 1
                rate['sampleCount'] += row['samples']
            else:
                result[counter] += 1
                result['partialTagCount'] += 1
                result['unresolvedBytes'] += length
                result['complete'], previous = False, None
            events.append(event)
            if status != 'complete':
                break
            pos, index = pos + length, index + 1
        else:
            result['completeTagCount'] += 1
    result.update(eventCount=len(events), events=events[:max_events], eventsTruncated=len(events) > max_events,
                  durationSeconds={'numerator': duration.numerator, 'denominator': duration.denominator},
                  rates=[rates[key] for key in sorted(rates)])
    return result


def verify_mp3_summary(document, expected):
    require(document['schemaVersion'] == 3, 'wrong MP3 summary schema version')
    got = document['summary']['mp3Frames']
    # Names are presentation text; original encoded enumerations carry identity.
    def without_names(value):
        if isinstance(value, dict):
            return {k: without_names(v) for k, v in value.items() if k != 'name'}
        if isinstance(value, list):
            return [without_names(v) for v in value]
        return value
    for field, value in expected.items():
        actual, wanted = without_names(got[field]), without_names(value)
        if field == 'rates':
            actual = sorted(actual, key=lambda row: row['samplingFrequencyHz'])
            wanted = sorted(wanted, key=lambda row: row['samplingFrequencyHz'])
        require(actual == wanted, 'MP3 all-frame field differs: ' + field)
    require(got['frameBytes'] + got['unresolvedBytes'] == got['soundDataBytes'], 'MP3 bytes are lost or double-counted')
    return len(expected['events'])


def verify(args, commands):
    from jsonschema import Draft202012Validator
    expected = load_expectations(args.input_dir)
    cli_hash = digest(args.cli)
    avc_schema = ROOT / 'docs/schemas/flv-avc-tag-detail-v4.schema.json'
    mp3_schema = ROOT / 'docs/schemas/flv-media-summary-v3.schema.json'
    avc_validator = Draft202012Validator(json.loads(avc_schema.read_text()))
    mp3_validator = Draft202012Validator(json.loads(mp3_schema.read_text()))
    fields, slices, pps_count, types = 0, 0, 0, set()
    avc_cases = [(name, case['file'], case) for name, case in expected['fullH264'].items()]
    avc_cases += [('encoded', H264, expected['h264']), ('encoded-bframes', BFRAMES, expected['h264BFrames'])]
    for name, filename, oracle in avc_cases:
        documents = {}
        for block in oracle['traceBlocks']:
            if block['kind'] not in ('Picture Parameter Set', 'Slice Header'):
                continue
            tag = block['tagId']
            if tag not in documents:
                output = commands.run(f'{name}.tag{tag}', [args.cli, 'detail', '--schema-version', '4', '--tag', str(tag), args.input_dir / filename])
                documents[tag] = json.loads(output.read_text())
                avc_validator.validate(documents[tag])
            doc = documents[tag]
            require(doc['detail']['complete'] and not doc['diagnostics'], name + ': valid full-header fixture is incomplete')
            if block['kind'] == 'Picture Parameter Set':
                unit = next(item for item in doc['detail']['avcDecoderConfigurationRecord']['pictureParameterSets'] if item['span']['byteOffset'] == block['offset'])
                require(unit['pictureParameterSet']['full']['complete'], name + ': PPS full analysis incomplete')
                pps_count += 1
            else:
                unit = doc['detail']['nalUnitSequence']['nalUnits'][block['nalIndex']]
                full = unit['sliceHeaderFull']
                verify_slice_views(unit['sliceHeaderPrefix'], full)
                require(full['complete'] and 0 < full['bytesRead'] < block['bytes'], name + ': slice full header/body scope differs')
                require(full['headerBits'] == max(f['rbspBitStartIncludingNALHeader'] + f['bitLength'] - 8 for f in block['fields']
                        if not f['name'].startswith(('rbsp_', 'cabac_alignment'))), name + ': header ending bit differs')
                types.add(unit['sliceHeaderPrefix']['sliceTypeName'])
                slices += 1
            fields += compare_full_header(unit, block)
    require(types == {'I', 'P', 'B', 'SP', 'SI'}, 'full-header slice types missing')
    for name, oracle in expected['fullH264Negative'].items():
        output = commands.run('negative-' + name, [args.cli, 'detail', '--schema-version', '4', '--tag',
                              str(oracle['tagId']), args.input_dir / oracle['file']], expected=2)
        doc = json.loads(output.read_text())
        avc_validator.validate(doc)
        require(not doc['detail']['complete'] and any(d['code'] == oracle['diagnostic'] for d in doc['diagnostics']),
                name + ': invalid parameter dependency was accepted')
    mp3_cases, events = 0, 0
    for name, oracle in expected['allFrameMP3'].items():
        source = args.input_dir / oracle['file']
        for suffix, frame_budget, event_budget in [('default', 1000000, 256)] + ([('events', 1000000, 3), ('budget', 3, 256)] if name in ('merged', 'split') else []):
            want = expected_mp3(source.read_bytes(), frame_budget, event_budget)
            output = commands.run(f'mp3-{name}.{suffix}', [args.cli, 'summary', '--schema-version', '3',
                    '--max-mp3-frames', str(frame_budget), '--max-mp3-frame-events', str(event_budget), source],
                    expected=0 if want['complete'] else 2)
            doc = json.loads(output.read_text())
            mp3_validator.validate(doc)
            events += verify_mp3_summary(doc, want)
            mp3_cases += 1
    load_expectations(args.input_dir)
    require(digest(args.cli) == cli_hash, 'CLI changed during validation')
    return {'passed': True, 'cliSHA256': cli_hash, 'expectationsSHA256': digest(args.input_dir / 'expectations.json'),
            'avcSchemaSHA256': digest(avc_schema), 'mp3SchemaSHA256': digest(mp3_schema),
            'h264Cases': len(avc_cases), 'h264MappedFieldsCompared': fields, 'h264SliceHeadersCompared': slices,
            'h264PPSCompared': pps_count, 'sliceTypes': sorted(types), 'mp3SummaryCases': mp3_cases,
            'h264NegativeCases': len(expected['fullH264Negative']),
            'mp3EventsCompared': events, 'scope': 'Encoded FFmpeg traces plus structural authored headers and physical MP3 frame boundaries; no decoding/reservoir proof.'}


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)),
                                         'input': str(args.input_dir), 'cli': str(args.cli), 'cliSHA256': digest(args.cli)})
        result = verify(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
