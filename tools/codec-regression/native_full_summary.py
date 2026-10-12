#!/usr/bin/env python3
"""Native MP3 all-frame counts, bounded event pages and late-frame source checks.

Authors 300 physical MPEG frames in one FLV Tag; encoded audio is opaque. The
oracle reads original frame headers, independently of product summary output.
"""
import argparse
import hashlib
import json
from pathlib import Path
import sys
import traceback

from generate import ROOT, Commands, digest, executable, new_directory, require, save
from native import verify_case
from native_search_history import read_ui
from search_history_oracle import make_flv
from verify_full_codec import expected_mp3


def fixture():
    frames = []
    for index in range(300):
        # A same-Tag rate/bitrate change, then a channel change beyond retention.
        header, length = (bytes.fromhex('ff fba4 64'), 480) if index == 150 else (bytes.fromhex('ff fb90 c4') if index == 270 else bytes.fromhex('ff fb90 64'), 417)
        frames.append(header + bytes(length - 4))
    return make_flv([(8, b'\x2f' + b''.join(frames), 0)])


def metric_map(metrics):
    keys = [(m['section'], m['name']) for m in metrics]
    require(len(keys) == len(set(keys)), 'duplicate summary metric')
    return {key: m['value'] for key, m in zip(keys, metrics)}


def verify_summary(snapshot, oracle, start):
    require(snapshot['schemaVersion'] == 3 and snapshot['selection']['schemaVersion'] == 3, 'summary snapshot version differs')
    summary, response = snapshot['summary'], snapshot['summaryPage']
    require(summary is not None and summary['state'] == 'ready' and summary['error'] is None and response is not None and response['error'] is None, 'summary/page is not ready')
    require(summary['sessionId'] == response['sessionId'] == snapshot['session']['sessionId'] and summary['requestId'] == response['requestId'] == snapshot['selection']['summaryRequestId'], 'summary identity differs')
    require(response['kind'] == 'mp3' and snapshot['selection']['summaryPage'] == {'kind': 'mp3', 'start': str(start), 'count': 100}, 'exported page selection differs')
    overview, page = summary['overview'], response['page']
    require(overview['complete'] and not overview['hasErrors'] and overview['scanComplete'] and not overview['scanHasErrors'], 'valid complete-frame fixture became incomplete')
    require(overview['sourceSize'] == str(len(fixture())) and overview['indexedTags'] == '1', 'summary container coverage differs')
    metrics = metric_map(overview['metrics'])
    require(len(metrics) <= 128, 'overview exceeded public metric cap')
    fields = ('completeFrameCount', 'attemptedHeaderCount', 'sampleCount', 'frameBytes', 'unresolvedBytes',
              'parameterChangeCount', 'bitrateChangeCount', 'completeTagCount', 'partialTagCount',
              'freeFormatCount', 'truncatedHeaderCount', 'malformedHeaderCount', 'truncatedFrameCount',
              'unsupportedLayerCount', 'budgetStoppedTagCount', 'maxFrames')
    for name in fields:
        require(metrics[('mp3Frames', name)] == str(oracle[name]), 'native aggregate differs: ' + name)
    duration = oracle['durationSeconds']
    require(metrics[('mp3Frames', 'durationSeconds')] == f'{duration["numerator"]}/{duration["denominator"]}' and
            metrics[('mp3Frames', 'complete')] == metrics[('mp3Frames', 'scanComplete')] == 'true', 'native exact duration/coverage differs')
    retained = next(row for row in overview['lists'] if row['kind'] == 'mp3')
    require(retained == {'kind': 'mp3', 'observed': '300', 'retained': '256', 'truncated': True}, 'event retention was confused with total frame coverage')
    expected = oracle['events'][start:start + 100]
    require(page['bounds'] == {'start': str(start), 'limit': 100, 'total': '256', 'next': str(start + len(expected))} and len(page['events']) == len(expected), 'native event page bounds differ')
    for index, (event, want) in enumerate(zip(page['events'], expected), start):
        require(event['id'] == str(index) and event['title'] == 'mp3Frame' and event['tagId'] == str(want['tagId']) and
                event['offset'] == str(want['sourceSpan']['byteOffset']) and event['bitOffset'] == 0 and event['dts'] == str(want['dts']), 'native event/source identity differs')
        values = {m['name']: m['value'] for m in event['values']}
        params = want['parameters']
        expected_values = {'frameIndex': str(want['frameIndex']), 'frameStatus': want['status'],
                           'sourceBitLength': str(want['sourceSpan']['bitLength']),
                           'samplingFrequencyHz': str(params['samplingFrequencyHz']), 'channels': str(params['channels']),
                           'samplesPerFrame': str(params['samplesPerFrame']), 'bitrateKbps': str(want['bitrateKbps'])}
        for name in ('parametersChangedFromPrevious', 'bitrateChangedFromPrevious'):
            expected_values[name] = str(want[name]).lower() if name in want else None
        for name, value in expected_values.items():
            require(values[name] == value, f'native event {index} differs: {name}')
    return len(expected)


def verify_directory(directory, source):
    raw = source.read_bytes()
    oracle = expected_mp3(raw)
    require(oracle['completeFrameCount'] == 300 and oracle['parameterChangeCount'] == 4 and len(oracle['events']) == 256, 'native fixture no longer exercises aggregate retention')
    native = json.loads((directory / 'native-result.json').read_text())
    pages = native['mp3SummaryPages']
    require(len(pages) == 3, 'native did not visit all retained pages')
    count = 0
    for stage, start in zip(pages, (0, 100, 200)):
        path, checked = directory / stage['file'], stage['snapshotCheck']
        require(checked['schemaValid'] and checked['sha256'] == digest(path) and checked['sourceSHA256'] == hashlib.sha256(raw).hexdigest(), 'summary snapshot/source changed')
        count += verify_summary(json.loads(path.read_text()), oracle, start)
        ui = read_ui(directory, stage)
        require(stage['start'] == start and stage['rowCount'] == min(100, 256-start) and
                any('观测总数 300 · 实际保留 256 · 事件受保留上限限制' in row['text'] for row in ui), 'native UI lacked explicit retained/total coverage')
        labels = [row['name'] for row in ui if row['name'].startswith('定位媒体事件 ') and row['role'] == 'push button']
        require(labels == [f'定位媒体事件 {i} 原始字节' for i in range(start, min(start + 100, 256))], 'native table omitted or invented event rows')
    require(count == 256, 'retained frame observations were skipped')
    stage = native['mp3SummarySource']
    want = oracle['events'][-1]
    offset = want['sourceSpan']['byteOffset']
    require(stage['eventId'] == '255' and stage['offset'] == str(offset), 'source action was not beyond the detail64frame cap')
    path = directory / stage['file']
    checked = stage['snapshotCheck']
    require(checked['schemaValid'] and checked['hex']['matchesSource'] and checked['sha256'] == digest(path) and checked['sourceSHA256'] == digest(source), 'source snapshot validation differs')
    snapshot = json.loads(path.read_text())
    window = snapshot['hex']['window']
    require(int(window['offset']) == offset and bytes.fromhex(window['hex']) == raw[offset:offset + len(window['hex']) // 2], 'summary source Hex differs')
    ui = read_ui(directory, stage)
    require(any(f'媒体概览 · mp3Frame · {offset} : 0 · 仅标记位置，无已知范围。' in row['text'] for row in ui), 'summary Hex UI lost exact source position')
    final = json.loads((directory / 'snapshot.json').read_text())
    require(final['detail']['detail']['kind'] == 'mp3Audio', 'native detail navigation failed')
    verify_summary(final, oracle, 200)
    return {'completeFrames': 300, 'retainedEvents': count, 'eventPages': 3, 'sourceFrameIndex': 255,
            'sourceOffset': offset, 'exactDurationSeconds': oracle['durationSeconds'], 'parameterChanges': oracle['parameterChangeCount']}


def run(args, commands):
    source = args.output_dir / 'mp3-300-frames.flv'
    source.write_bytes(fixture())
    binary_hash, source_hash = digest(args.binary), digest(source)
    save(args.output_dir, 'oracle.json', expected_mp3(source.read_bytes()))
    directory = args.output_dir / 'native'
    commands.run('native', [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                 '--output-dir', directory, '--source', source, '--tag', '0', '--mp3-summary',
                 '--timeout-seconds', str(args.timeout_seconds)], timeout=args.timeout_seconds + 30)
    row = verify_case(directory, 'mp3-all-frame-summary', {}, semantic_check=lambda *_: verify_directory(directory, source))
    require(row['binarySHA256'] == binary_hash == digest(args.binary) and source_hash == digest(source), 'production binary/source changed')
    return {'passed': True, 'rounds': 1, 'binarySHA256': binary_hash, 'sourceSHA256': source_hash, 'case': row,
            'scope': 'Native all-frame boundary summary and source mapping; authored opaque audio is not decoded.'}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--timeout-seconds', type=int, default=600)
    args = parser.parse_args(argv)
    if not 30 <= args.timeout_seconds <= 1800:
        parser.error('--timeout-seconds must be 30 through 1800')
    args.output_dir.mkdir(parents=True)
    save(args.output_dir, 'run.json', {'python': sys.version, 'binary': str(args.binary), 'binarySHA256': digest(args.binary), 'scriptSHA256': digest(Path(__file__))})
    try:
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
