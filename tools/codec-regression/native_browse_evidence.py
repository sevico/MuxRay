#!/usr/bin/env python3
"""Three isolated native rounds for MP3 frame browsing, AVC evidence and curve export.

Evidence uses saved FFmpeg traces and independent source-bit oracles. CLI summary
is a separate aggregate cross-check, never an oracle for codec syntax evidence.
"""

import argparse
import hashlib
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, BFRAMES, Commands, digest, executable, flv_tags, load_expectations, new_directory, require, save
from native import verify_case
from native_context_side import mp3_fields, native_span
from native_v3 import child_fields, compare_prefix, first_b_tag, SLICE_FIELDS
from side_info_oracle import source_field
from verify_side_info import BOOLEAN_FIELDS, expected_dependency


def root_fields(detail, name):
    roots = [f for f in detail['fields'] if f['name'] == name]
    require(len(roots) == 1, 'expected one native root: ' + name)
    return child_fields(detail['fields'], roots[0]['id'])


def verify_saved_snapshot(path, checked, source):
    require(checked['schemaValid'] and checked['hex']['matchesSource'] and checked['sha256'] == digest(path) and
            checked['sourceSHA256'] == hashlib.sha256(source).hexdigest(), 'saved snapshot/source identity differs from native validation')
    snapshot = json.loads(path.read_text())
    selection, detail, hex_view = snapshot['selection'], snapshot['detail'], snapshot['hex']
    require(snapshot['session']['sessionId'] == selection['sessionId'] and snapshot['selectedTag']['id'] == selection['selectedTagId'], 'snapshot selection identity differs')
    require(detail['apiVersion'] == 1 and detail['state'] == 'ready' and detail['error'] is None and
            detail['sessionId'] == selection['sessionId'] and detail['requestId'] == selection['detailRequestId'] and
            detail['tagId'] == selection['selectedTagId'], 'snapshot detail response identity differs')
    require(hex_view['apiVersion'] == 1 and hex_view['state'] == 'ready' and hex_view['error'] is None and
            hex_view['sessionId'] == selection['sessionId'] and hex_view['requestId'] == selection['hexRequestId'], 'snapshot Hex response identity differs')
    window = hex_view['window']
    raw, offset = bytes.fromhex(window['hex']), int(window['offset'])
    require(raw and 0 <= offset <= len(source) and raw == source[offset:offset + len(raw)], 'saved Hex bytes differ from source')
    return snapshot


def verify_mp3_selection(snapshot, case, source, selected):
    detail, count = snapshot['detail']['detail'], 0
    fields = detail['fields']
    require(snapshot['selectedTag']['id'] == '0' and detail['kind'] == 'mp3Audio', 'MP3 selected Tag differs')
    require(detail['complete'] and detail['truncated'] and not detail['diagnostics'], 'MP3 completion/omission differs')
    metadata = root_fields(detail, 'Frame inspection')
    for name, value in {'selectedFrameIndex': str(selected), 'checkedFrameCount': str(case['physicalFrameCount']),
                        'selectableFrameIndices': ','.join(str(i) for i in range(case['physicalFrameCount'])),
                        'frameBudget': '64', 'byteCoverageBudget': '1048576'}.items():
        require(metadata[name]['value'] == value, 'MP3 selection metadata differs: ' + name)
    omitted = case['physicalFrameCount'] - 1
    require(metadata['sideInfoOmittedFrames']['value'] == f'{omitted}; desktop expands only the selected side-info record; CLI v3 retains all inspected records', 'MP3 omission count differs')
    roots = [f for f in fields if f['name'].startswith('Side info Frame ')]
    require(len(roots) == 1 and roots[0]['name'] == f'Side info Frame {selected}', 'wrong side-info frame expanded')
    want, root = case['frames'][selected]['sideInfo'], roots[0]
    require(root['span'] == native_span(want['span']) and root['value'] == f'complete=true; {want["bitsConsumed"]} of {want["expectedBytes"] * 8} bits inspected', 'selected side-info source/scope differs')
    actual = mp3_fields(fields, root)
    require(set(actual) == {f['path'] for f in want['fields']}, 'selected encoded field presence differs')
    for oracle in want['fields']:
        path, field = oracle['path'], actual[oracle['path']]
        boolean = path.startswith('scfsi[') or path.rsplit('.', 1)[-1] in BOOLEAN_FIELDS
        value = str(bool(oracle['value'])).lower() if boolean else str(oracle['value'])
        require(field['value'] == value and field['span'] == native_span(oracle['span']), 'selected field value/source differs: ' + path)
        source_field(source, oracle)
        count += 1
    frames = [f for f in fields if f['type'] == 'MPEG frame']
    require(len(frames) == case['physicalFrameCount'] and len(fields) <= 512, 'frame summary omitted or field budget exceeded')
    for root, oracle in zip(frames, case['frames']):
        require(root['name'] == f'Frame {oracle["index"]}' and root['span'] == native_span({'byteOffset': oracle['sourceOffset'], 'bitOffset': 0, 'bitLength': oracle['availableLength'] * 8}), 'frame identity/source differs')
        child, dep = child_fields(fields, root['id']), expected_dependency(oracle)
        require(child['CRC16']['value'].startswith(oracle['crc']['status'] + ' '), 'frame CRC status differs')
        value = f'{dep["state"]}; prior same-tag slot bytes={dep["knownPreviousBytes"]}'
        if 'missingPreviousBytes' in dep:
            value += f'; bytes before tag={dep["missingPreviousBytes"]}'
        if 'part23Bits' in dep:
            value += f'; declared bits={dep["part23Bits"]}, same-tag window bits={dep["availableBits"]}'
        require(child['mainDataDependency']['value'] == value + '; not reservoir or decoded-audio validation', 'frame dependency lost same-Tag history')
    return {'selectedFrameIndex': selected, 'sourceMappedFieldsCompared': count, 'fields': len(fields)}


def source_bits(source, span):
    offset, count = int(span['byteOffset']) * 8 + span['bitOffset'], span['bitLength']
    require(count > 0 and offset >= 0 and offset + count <= len(source) * 8, 'evidence source span out of range')
    return ''.join(str((source[bit // 8] >> (7 - bit % 8)) & 1) for bit in range(offset, offset + count))


def verify_avc_evidence(snapshot, expected, source, tag_id):
    detail, oracle = snapshot['detail']['detail'], expected['h264BFrames']
    require(snapshot['selectedTag']['id'] == str(tag_id) and detail['kind'] == 'avcVideo', 'AVC selected Tag differs')
    require(detail['complete'] and not detail['truncated'] and not detail['diagnostics'], 'AVC detail incomplete/diagnosed')
    tags = flv_tags(source)
    tag, fields = tags[tag_id], detail['fields']
    nals = [n for n in oracle['nals'] if n['tagId'] == tag_id]
    traces = [b for b in oracle['traceBlocks'] if b['kind'] == 'Slice Header' and b['tagId'] == tag_id]
    require(len(traces) >= 2 and nals, 'fixture no longer provides a multi-slice Tag')
    declaration, idrs = source[tag['payloadOffset']] >> 4, [n for n in nals if n['type'] == 5]
    slice_fields = [(b, next(f for f in b['fields'] if f['name'] == 'slice_type')) for b in traces]
    types = [name for i, name in enumerate(('P', 'B', 'I', 'SP', 'SI')) if any(f['value'] % 5 == i for _, f in slice_fields)]
    relation = ('keyframe_' if declaration == 1 else 'inter_') + ('with_idr' if idrs else 'without_idr')
    got = root_fields(detail, 'Keyframe evidence')
    for name, value in {'declaredFrameType': str(declaration), 'observedIDR': 'yes' if idrs else 'no',
                        'declarationRelation': relation, 'nalHeaderCoverageComplete': 'true', 'sliceTypeCoverageComplete': 'true',
                        'observedSliceTypes': ','.join(types), 'retainedNALUnits': str(len(nals)), 'inspectedNALHeaders': str(len(nals)),
                        'unclassifiedNALHeaders': '0', 'observedSliceHeaders': str(len(traces))}.items():
        require(got[name]['value'] == value, 'AVC evidence differs: ' + name)
    declaration_span = {'byteOffset': str(tag['payloadOffset']), 'bitOffset': 0, 'bitLength': 4}
    require(got['declaredFrameType']['span'] == declaration_span and int(source_bits(source, declaration_span), 2) == declaration, 'FLV declaration source differs')
    expected_names = {'evidenceNALType'} | ({'evidenceIDRNALType'} if idrs else set()) | {'evidenceSliceType' + name for name in types}
    require({name for name in got if name.startswith('evidence') and name != 'evidenceScope'} == expected_names, 'invented/missing source evidence')
    for name, nal in [('evidenceNALType', nals[0])] + ([('evidenceIDRNALType', idrs[0])] if idrs else []):
        span = {'byteOffset': str(nal['offset']), 'bitOffset': 3, 'bitLength': 5}
        require(got[name]['span'] == span and got[name]['value'].startswith(str(nal['type']) + ' (') and got[name]['value'].endswith(f'NAL[{nal["nalIndex"]}])') and int(source_bits(source, span), 2) == nal['type'], 'NAL evidence value/source differs')
    compared = 0
    for name in types:
        block, field = next((b, f) for b, f in slice_fields if ('P', 'B', 'I', 'SP', 'SI')[f['value'] % 5] == name)
        segments = field['sourceBitSegments']
        span = native_span(segments[0]) if len(segments) == 1 else None
        require(got['evidenceSliceType' + name]['span'] == span and got['evidenceSliceType' + name]['value'] == f'{field["value"]} ({name}; NAL[{block["nalIndex"]}])', 'slice evidence differs')
        bits = ''.join(source_bits(source, segment) for segment in segments)
        zeros = len(bits) - len(bits.lstrip('0'))
        require(len(bits) == zeros * 2 + 1 and int(bits[zeros:], 2) - 1 == field['value'], 'slice evidence codeword differs from source')
    for trace in traces:
        roots = [f for f in fields if f['name'] == f'Slice prefix NAL[{trace["nalIndex"]}]']
        require(len(roots) == 1, 'slice prefix omitted')
        compared += compare_prefix(child_fields(fields, roots[0]['id']), trace, SLICE_FIELDS)
    return {'selectedTagId': tag_id, 'declarationRelation': relation, 'sliceTypes': types, 'prefixFieldsCompared': compared}


def curve_oracle(source):
    """Independent aggregation for these valid, monotonic, <256-Tag fixtures."""
    tags = flv_tags(source)
    require(0 < len(tags) <= 256, 'native curve fixture must fit one complete window')
    groups = {'audio': [], 'video': [], 'mp3': []}
    keyframes = []
    for tag in tags:
        payload = source[tag['payloadOffset']:tag['payloadEnd']]
        if tag['type'] == 8 and payload and payload[0] >> 4 == 2:
            groups['audio'].append(tag)
            groups['mp3'].append(tag)
        elif tag['type'] == 9 and len(payload) >= 5 and payload[0] & 15 == 7 and payload[1] == 1:
            groups['video'].append(tag)
            if payload[0] >> 4 == 1:
                keyframes.append(tag)
        else:
            require(tag['type'] == 18 or tag['type'] == 9 and len(payload) >= 5 and payload[1] in (0, 2), 'unexpected fixture codec')
    rates = {}
    for name, track in groups.items():
        require(all(b['dts'] > a['dts'] for a, b in zip(track, track[1:])), 'fixture DTS must be strictly increasing')
        interval = track[-1]['dts'] - track[0]['dts'] if len(track) > 1 else 0
        covered = sum(t['dataSize'] for t in track[:-1]) if interval else 0
        rates[name] = {'coveredBytes': str(covered), 'coveredIntervalMs': str(interval),
                       'bitsPerSecond': str(covered * 8000 // interval) if interval else None,
                       'tagId': str(track[1]['tagId']) if interval else None}
    durations = [b['dts'] - a['dts'] for a, b in zip(keyframes, keyframes[1:])]
    require(all(d > 0 for d in durations), 'fixture GOP duration must be positive')
    maximum = durations.index(max(durations)) + 1 if durations else None
    gop = {'declaredCount': str(len(keyframes)), 'closedCount': str(len(durations)), 'totalDurationMs': str(sum(durations)),
           'minDurationMs': str(min(durations)) if durations else None, 'maxDurationMs': str(max(durations)) if durations else None,
           'averageDurationMs': str(sum(durations) // len(durations)) if durations else None,
           'tagId': str(keyframes[maximum]['tagId']) if durations else None}
    return {'index': '0', 'startTag': '0', 'nextTag': str(len(tags)), **rates, 'gop': gop, 'unclassifiedTags': '0'}


def verify_curve(snapshot, source, cli, metric):
    require(snapshot['schemaVersion'] in (2, 3) and snapshot['selection']['schemaVersion'] == snapshot['schemaVersion'], 'native export did not preserve curve-compatible snapshot version')
    selected, response = snapshot['selection'], snapshot['mediaCurvePage']
    require(selected['mediaCurve'] == {'start': '0', 'count': 64, 'metric': metric}, 'snapshot did not retain current curve selection')
    require(response and response['error'] is None and response['apiVersion'] == 1 and response['sessionId'] == selected['sessionId'] and response['requestId'] == selected['summaryRequestId'], 'curve response identity differs')
    require(snapshot['summary'] and snapshot['summary']['state'] == 'ready' and snapshot['summary']['requestId'] == response['requestId'], 'curve summary is unavailable')
    page, want = response['page'], curve_oracle(source)
    require(page['bounds'] == {'start': '0', 'limit': 64, 'total': '1', 'next': '1'} and page['windowTags'] == 256 and page['indexedTags'] == want['nextTag'] and page['scanComplete'] and page['complete'], 'curve range/coverage differs')
    require(page['points'] == [want], 'curve values differ from independent FLV aggregation')
    require(cli['schemaVersion'] == 2 and cli['scan']['complete'] and cli['scan']['indexedTagCount'] == int(want['nextTag']), 'CLI summary coverage differs')
    summary = cli['summary']
    require(summary['complete'] and summary['tags']['total'] == int(want['nextTag']), 'CLI summary incomplete')
    for name, track in [('audio', summary['audio']), ('video', summary['video']), ('mp3', summary['mp3']['track'])]:
        rate = want[name]
        if rate['bitsPerSecond'] is None:
            require('bitrate' not in track, 'CLI invented an uncovered bitrate')
        else:
            got = track['bitrate']
            require((got['bytes'], got['coveredIntervalMs'], got['averageBitsPerSecond']) == tuple(int(rate[k]) for k in ('coveredBytes', 'coveredIntervalMs', 'bitsPerSecond')), 'CLI/native rate aggregate differs')
    require(summary['gop']['count'] == int(want['gop']['declaredCount']) and summary['gop']['closedDurationCount'] == int(want['gop']['closedCount']), 'CLI/native declared GOP counts differ')
    return {'metric': metric, 'points': 1, 'indexedTags': int(want['nextTag']), 'independentSourceAggregation': True, 'cliSummaryCompared': True}


def verify_evidence_clicks(directory, snapshot, expected, source, tag, cli, metric):
    native = json.loads((directory / 'native-result.json').read_text())
    initial = native['avcEvidenceInitial']
    saved = verify_saved_snapshot(directory / initial['file'], initial['snapshotCheck'], source)
    verify_avc_evidence(saved, expected, source, tag)
    verify_curve(saved, source, cli, metric)
    fields = root_fields(snapshot['detail']['detail'], 'Keyframe evidence')
    targets = {name: field for name, field in fields.items() if field['span'] is not None and
               (name in ('declaredFrameType', 'evidenceNALType', 'evidenceIDRNALType') or name.startswith('evidenceSliceType'))}
    stages = native['avcEvidenceStages']
    require(len(stages) == len(targets) and {stage['field'] for stage in stages} == set(targets), 'source button clicks missing or duplicated')
    for stage in stages:
        span = targets[stage['field']]['span']
        label = 'FLV frameType' if stage['field'] == 'declaredFrameType' else stage['field']
        text = f'{label} · {span["byteOffset"]} : {span["bitOffset"]} · {span["bitLength"]} bit'
        require(stage['span'] == span and stage['selectionText'] == text, 'source button selected wrong byte/bit range')
        offset = int(span['byteOffset'])
        require(0 <= offset < len(source), 'source click Hex offset differs')
        path = directory / stage['uiFile']
        require(stage['uiSHA256'] == digest(path), 'source click UI evidence identity differs')
        ui = json.loads(path.read_text())
        require(any(text in row['text'] for row in ui), 'source click Hex bit-selection text was not observed')
        windows = [row for row in ui if row['name'] == 'Hex 实际窗口']
        require(len(windows) == 1 and windows[0]['text'].startswith(f'实际窗口 [{offset}, '), 'source click displayed Hex offset differs')
        bits = [row for row in ui if row['name'].startswith('bit ')]
        require(len(bits) == 8, 'source click bit display missing or duplicated')
        for index, row in enumerate(bits):
            selected = span['bitOffset'] <= index < min(8, span['bitOffset'] + span['bitLength'])
            require(row['name'] == f'bit {index}' + (' 范围内' if selected else '') and
                    row['text'] == str((source[offset] >> (7 - index)) & 1), 'source click displayed bit/highlight differs')
    return {'clickedSourceFields': list(targets), 'stages': len(stages), 'hexByteAndBitSelectionCompared': True}


def run(args, commands):
    expected, side = load_expectations(args.input_dir), load_expectations(args.side_info_dir)
    binary_hash, cli_hash, rows = digest(args.binary), digest(args.cli), []
    case = side['cases']['mpeg1-stereo']
    require(case['physicalFrameCount'] == 15 and case['completeFrameCount'] == 15, 'MP3 fixture must contain 15 complete frames')
    idr_tag = next(n['tagId'] for n in expected['h264BFrames']['nals'] if n['type'] == 5)
    cases = [('mp3-browse', args.side_info_dir / case['file'], 0, 'mp3'),
             ('b-slice-evidence', args.input_dir / BFRAMES, first_b_tag(expected), 'video'),
             ('idr-evidence', args.input_dir / BFRAMES, idr_tag, 'gopAverage')]
    if args.avc_evidence_only:
        cases = cases[1:]
    for name, source_path, tag, metric in cases:
        source, directory = source_path.read_bytes(), args.output_dir / name
        source_hash = digest(source_path)
        cli_path = commands.run(name + '-summary', [args.cli, 'summary', '--schema-version', '2', source_path])
        cli = json.loads(cli_path.read_text())
        argv = [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                '--source', source_path, '--tag', str(tag), '--output-dir', directory,
                '--media-curves', metric, '--timeout-seconds', str(args.timeout_seconds)]
        if name == 'mp3-browse':
            argv.append('--mp3-browse')
        else:
            argv.append('--avc-evidence')
        commands.run(name, argv, timeout=args.timeout_seconds + 30)
        def semantics(_name, snapshot, _expected):
            codec = verify_mp3_selection(snapshot, case, source, 14) if name == 'mp3-browse' else verify_avc_evidence(snapshot, expected, source, tag)
            return {**codec, 'curve': verify_curve(snapshot, source, cli, metric)}
        row = verify_case(directory, name, expected, semantic_check=semantics)
        require(row['binarySHA256'] == binary_hash and row['sourceSHA256'] == source_hash, 'native source/binary identity differs')
        native = json.loads((directory / 'native-result.json').read_text())
        snapshot = verify_saved_snapshot(directory / 'snapshot.json', native['snapshotCheck'], source)
        if name == 'mp3-browse':
            stages = native['mp3Stages']
            require(len(stages) == 1 and stages[0]['file'] == 'mp3-frame-2.json', 'intermediate MP3 export missing')
            stage = verify_saved_snapshot(directory / 'mp3-frame-2.json', stages[0]['snapshotCheck'], source)
            row['thirdFrame'] = {**verify_mp3_selection(stage, case, source, 2), 'curve': verify_curve(stage, source, cli, metric)}
        else:
            row['sourceClicks'] = verify_evidence_clicks(directory, snapshot, expected, source, tag, cli, metric)
        require(digest(source_path) == source_hash, 'native source changed')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash and digest(args.cli) == cli_hash, 'acceptance binary changed')
    load_expectations(args.input_dir)
    load_expectations(args.side_info_dir)
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash, 'cliSHA256': cli_hash, 'cases': rows}


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--side-info-dir', type=Path, required=True)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--cli', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--avc-evidence-only', action='store_true', help='rerun only the two AVC rounds, preserving earlier MP3 acceptance evidence')
    parser.add_argument('--timeout-seconds', type=int, default=240, help='per native round, 30 through 600')
    args = parser.parse_args(argv)
    args.input_dir, args.side_info_dir = args.input_dir.resolve(), args.side_info_dir.resolve()
    for path in (args.input_dir, args.side_info_dir):
        if not (path / 'expectations.json').is_file():
            parser.error('sample directory must contain expectations.json')
    if not 30 <= args.timeout_seconds <= 600:
        parser.error('--timeout-seconds must be between 30 and 600')
    return args


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)),
             'binary': str(args.binary), 'binarySHA256': digest(args.binary), 'cli': str(args.cli), 'cliSHA256': digest(args.cli),
             'input': str(args.input_dir), 'sideInfo': str(args.side_info_dir)})
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
