#!/usr/bin/env python3
"""Real Linux AVC search / MP3 history acceptance against independent byte oracles."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys
import traceback

from generate import ROOT, Commands, digest, executable, load_expectations, new_directory, require, save
from native import verify_case
from native_browse_evidence import root_fields, source_bits, verify_saved_snapshot
from native_context_side import native_span
from native_v3 import child_fields


RELATIONS = {'keyframe_with_idr': '标记为关键帧，已观察到 IDR NAL',
             'keyframe_without_idr': '标记为关键帧，未观察到 IDR NAL',
             'inter_with_idr': '标记为非关键帧，已观察到 IDR NAL',
             'inter_without_idr': '标记为非关键帧，未观察到 IDR NAL',
             'undetermined': '检查范围不足，暂不能确定对照结果',
             'other_declaration': '该 FLV 帧类型不参与关键帧对照'}
STATUS = re.compile(r'H\.264 检索完成：Tag \[(\d+), (\d+)\) · 检查 (\d+) 条 · 本批命中 (\d+) 条 · 读取 (\d+) byte。')
CASES = ['mp3-' + name for name in ('merged', 'split', 'interleaved', 'audio-barrier', 'missing-prefix')] + [
         'avc-' + name for name in ('pagination', 'unknown', 'idr-partial', 'missing-config')]


def read_ui(directory, stage):
    path = directory / stage['uiFile']
    require(digest(path) == stage['uiSHA256'], 'native UI evidence changed')
    return json.loads(path.read_text())


def verify_hex_ui(ui, span, label, source):
    span = native_span(span)
    text = f'{label} · {span["byteOffset"]} : {span["bitOffset"]} · {span["bitLength"]} bit'
    require(any(text in row['text'] for row in ui), 'source click did not retain exact byte/bit selection')
    windows = [row for row in ui if row['name'] == 'Hex 实际窗口']
    require(len(windows) == 1 and windows[0]['text'].startswith(f'实际窗口 [{span["byteOffset"]}, '), 'source click opened the wrong Hex window')
    bits = [row for row in ui if row['name'].startswith('bit ')]
    require(len(bits) == 8, 'source click did not display exactly eight bits')
    value = source[int(span['byteOffset'])]
    for index, row in enumerate(bits):
        inside = span['bitOffset'] <= index < min(8, span['bitOffset'] + span['bitLength'])
        require(row['name'] == f'bit {index}' + (' 范围内' if inside else '') and row['text'] == str(value >> (7 - index) & 1), 'source click bit value/highlight differs')


def verify_mp3(snapshot, want, source):
    detail = snapshot['detail']['detail']
    require(snapshot['selectedTag']['id'] == str(want['tagId']) and detail['kind'] == 'mp3Audio', 'MP3 target Tag differs')
    selection = root_fields(detail, 'Frame inspection')
    require(selection['selectedFrameIndex']['value'] == str(want['frameIndex']), 'MP3 selected frame differs')
    fields, meta = detail['fields'], root_fields(detail, 'MP3 dependency sources')
    state = 'unknown' if want['state'] in ('unknown', 'incomplete') else 'resolved'
    expected = {'dependencyState': state, 'targetTagId': str(want['tagId']), 'targetFrameIndex': str(want['frameIndex']),
                'requestedPreviousBytes': str(want['backBytes']), 'resolvedPreviousBytes': str(want['backBytes'] - want['missingBytes']),
                'missingPreviousBytes': str(want['missingBytes']), 'part23Bits': str(want['part23Bits'])}
    expected['availableBits'] = str((want['backBytes'] - want['missingBytes']) * 8 + want['mainDataSpan']['bitLength'])
    ranges = [{**r, 'current': False} for r in want['ranges']]
    if want['mainDataSpan']['bitLength']:
        ranges.append({'tagId': want['tagId'], 'frameIndex': want['frameIndex'], 'span': want['mainDataSpan'], 'current': True})
    expected['sourceCount'] = str(len(ranges))
    for name, value in expected.items():
        require(meta[name]['value'] == value, 'MP3 history metadata differs: ' + name)
    if state == 'resolved':
        reason = {'none': 'no_backward_reference', 'within_tag': 'within_tag', 'across_tags': 'across_tags'}[want['state']]
        require(meta['dependencyReason']['value'] == reason, 'MP3 history reason differs')
    else:
        require(bool(meta['dependencyReason']['value']), 'unknown MP3 history lacks a reason')
    for value, limit in [('scannedTags', 512), ('audioTags', 64), ('framesInspected', 256),
                         ('coverageBytes', 1048576), ('bytesRead', 1048576)]:
        require(0 <= int(meta[value]['value']) <= limit, 'MP3 history exceeded its budget: ' + value)
    root = next(f for f in fields if f['name'] == 'MP3 dependency sources')
    require(root['value'] == state, 'MP3 dependency root/state differ')
    containers = [f for f in fields if f['parentId'] == root['id'] and f['type'] == 'physical main-data source']
    require(len(containers) == len(ranges) and len(fields) <= 512, 'MP3 source or detail field count differs')
    for got, expected in zip(containers, ranges):
        child = child_fields(fields, got['id'])
        for name, value in [('sourceTagId', str(expected['tagId'])), ('sourceFrameIndex', str(expected['frameIndex'])),
                            ('sourceCurrent', str(expected['current']).lower())]:
            require(child[name]['value'] == value, 'MP3 source identity/order differs: ' + name)
        require(child['sourceSpan']['span'] == native_span(expected['span']), 'MP3 source bytes differ')
    encoded_back = int(source_bits(source, want['backpointerSpan']), 2)
    require(encoded_back == want['backBytes'], 'MP3 backpointer oracle differs from source bits')
    window = b''.join(source[r['span']['byteOffset']:r['span']['byteOffset'] + r['span']['bitLength'] // 8] for r in want['ranges'])
    require(hashlib.sha256(window).hexdigest() == want['windowSHA256'], 'MP3 physical window differs from original source bytes')
    return {'tagId': want['tagId'], 'frameIndex': want['frameIndex'], 'state': state, 'sources': len(ranges),
            'backBytes': want['backBytes'], 'missingBytes': want['missingBytes'], 'windowSHA256': want['windowSHA256']}


def table_rows(ui):
    result, ancestors = [], []
    for index, row in enumerate(ui):
        while ancestors and ui[ancestors[-1]]['depth'] >= row['depth']:
            ancestors.pop()
        match = re.fullmatch(r'从 H\.264 检索跳转 Tag (\d+)', row['name'])
        if match:
            parent = next((n for n in reversed(ancestors) if ui[n]['role'] in ('table row', 'row')), None)
            require(parent is not None, 'search result button has no accessible table row')
            end = next((i for i in range(index + 1, len(ui)) if ui[i]['depth'] <= ui[parent]['depth']), len(ui))
            result.append((int(match[1]), ui[parent:end]))
        ancestors.append(index)
    return result


def verify_avc_page(ui, status, oracle, filter_name):
    match = STATUS.match(status)
    require(match is not None and any(status == row['text'] for row in ui), 'AVC search status was not observed')
    start, end, scanned, count, read = map(int, match.groups())
    require(0 <= start < end <= oracle['indexedTags'] and scanned == end - start <= 256 and count <= 64 and 0 < read <= 1048576,
            'AVC page bounds/read budget differ')
    want = [r for r in oracle['rows'] if start <= r['tagId'] < end and
            (filter_name == 'all' or filter_name == 'unknown' and r['observedIDR'] == 'unknown' or
             filter_name == 'mismatches' and r['declarationRelation'] in ('keyframe_without_idr', 'inter_with_idr'))]
    rows = table_rows(ui)
    require([tag for tag, _ in rows] == [r['tagId'] for r in want] and count == len(want), 'AVC search page omitted, duplicated or invented a result')
    for (_, actual), expected in zip(rows, want):
        texts = [r['text'] for r in actual]
        require(RELATIONS[expected['declarationRelation']] in texts and 'IDR：' + expected['observedIDR'] in texts and
                str(expected['dts']) in texts, 'AVC search DTS/relation/IDR differs')
        span = expected['evidenceSpan']
        buttons = [row for row in actual if row['name'] == f'定位 H.264 Tag {expected["tagId"]} 证据']
        description = f'{span["byteOffset"]} : {span["bitOffset"]} · {span["bitLength"]} bit'
        require(len(buttons) == 1 and buttons[0].get('description') == description, 'AVC search source position differs')
    require(('已到当前索引末尾。' in status) == (end == oracle['indexedTags']), 'AVC search exhaustion differs')
    return {'start': start, 'next': end, 'scanned': scanned, 'rows': count, 'bytesRead': read}


def verify_search(directory, oracle, source, filter_name):
    native = json.loads((directory / 'native-result.json').read_text())
    pages, end = [], 0
    for stage in native['avcSearchPages']:
        page = verify_avc_page(read_ui(directory, stage), stage['status'], oracle, filter_name)
        require(page['start'] == end, 'AVC search continuation skipped or repeated indexed Tags')
        end = page['next']
        pages.append(page)
    require(end == oracle['indexedTags'], 'native search stopped before the full fixture was inspected')
    if len(pages) > 1:
        stage = native['avcSearchPrevious']
        require(verify_avc_page(read_ui(directory, stage), stage['status'], oracle, filter_name) == pages[-2], 'previous batch differs from the original page')
    if any(page['rows'] for page in pages[-1:]):
        stage = native['avcSearchSource']
        want = next(r for r in oracle['rows'] if r['tagId'] == stage['tagId'])
        verify_hex_ui(read_ui(directory, stage), want['evidenceSpan'], f'H.264 Tag {stage["tagId"]} 证据', source)
        selected = native['avcSearchSelected']
        snapshot = verify_saved_snapshot(directory / selected['file'], selected['snapshotCheck'], source)
        require(snapshot['selectedTag']['id'] == str(want['tagId']), 'AVC row did not navigate to its Tag')
    return {'pages': pages, 'filter': filter_name, 'exhausted': True}


def run(args, commands):
    expected = load_expectations(args.input_dir)
    binary_hash, rows = digest(args.binary), []
    selected = expected['selectedStreamFrameIndex']
    cases = []
    for name in ('merged', 'split', 'interleaved', 'audio-barrier', 'missing-prefix'):
        case = expected['mp3Cases'][name]
        target = case['frames'][0 if name == 'missing-prefix' else 5 if name == 'audio-barrier' else selected]
        cases.append(('mp3-' + name, case, target, None))
    for name, filter_name in [('pagination', 'mismatches'), ('unknown', 'unknown'), ('idr-partial', 'all'), ('missing-config', 'unknown')]:
        case = expected['avcCases'][name]
        cases.append(('avc-' + name, case, case['rows'][0], filter_name))
    for name, case, target, filter_name in cases:
        if args.case and name not in args.case:
            continue
        directory, source_path = args.output_dir / name, args.input_dir / case['file']
        source = source_path.read_bytes()
        argv = [sys.executable, '-B', ROOT / 'tools/desktop-native/linux_smoke.py', '--binary', args.binary,
                '--output-dir', directory, '--source', source_path, '--tag', str(target['tagId']),
                '--timeout-seconds', str(args.timeout_seconds)]
        if filter_name:
            argv += ['--avc-search', filter_name, '--avc-search-pages', '16']
        else:
            argv += ['--mp3-history', '--mp3-frame', str(target['frameIndex'])]
        commands.run(name, argv, timeout=args.timeout_seconds + 30)
        def semantics(_name, snapshot, _expected):
            return verify_search(directory, case, source, filter_name) if filter_name else verify_mp3(snapshot, target, source)
        row = verify_case(directory, name, expected, semantic_check=semantics)
        require(row['binarySHA256'] == binary_hash and row['sourceSHA256'] == hashlib.sha256(source).hexdigest(), 'native source/binary identity differs')
        if not filter_name:
            native = json.loads((directory / 'native-result.json').read_text())
            initial = native['mp3HistoryInitial']
            snapshot = verify_saved_snapshot(directory / initial['file'], initial['snapshotCheck'], source)
            verify_mp3(snapshot, target, source)
            ranges = target['ranges'] + [{'tagId': target['tagId'], 'frameIndex': target['frameIndex'], 'span': target['mainDataSpan']}]
            require(len(native['mp3HistorySources']) == len(ranges), 'MP3 source clicks missing or duplicated')
            for stage, wanted in zip(native['mp3HistorySources'], ranges):
                require((stage['tagId'], stage['frameIndex'], stage['span']) ==
                        (str(wanted['tagId']), str(wanted['frameIndex']), native_span(wanted['span'])), 'MP3 source click targeted the wrong frame')
                verify_hex_ui(read_ui(directory, stage), wanted['span'], f'MP3 来源 Tag {wanted["tagId"]} 帧 {wanted["frameIndex"]}', source)
            selected_stage = native['mp3HistorySourceDetail']
            selected_snapshot = verify_saved_snapshot(directory / selected_stage['file'], selected_stage['snapshotCheck'], source)
            require(selected_snapshot['selectedTag']['id'] == str(ranges[0]['tagId']) and
                    root_fields(selected_snapshot['detail']['detail'], 'Frame inspection')['selectedFrameIndex']['value'] == str(ranges[0]['frameIndex']),
                    'MP3 source button did not navigate to the source Tag/frame')
        rows.append(row)
        save(args.output_dir, 'cases.json', rows)
    require(digest(args.binary) == binary_hash, 'production binary changed during acceptance')
    load_expectations(args.input_dir)
    return {'passed': True, 'rounds': len(rows), 'binarySHA256': binary_hash, 'cases': rows}


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--binary', type=executable, required=True)
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--timeout-seconds', type=int, default=300)
    parser.add_argument('--case', action='append', choices=CASES, help='rerun selected rounds in a new evidence directory; default all nine')
    args = parser.parse_args(argv)
    args.input_dir = args.input_dir.resolve()
    if not (args.input_dir / 'expectations.json').is_file():
        parser.error('--input-dir must contain generated expectations.json')
    if not 30 <= args.timeout_seconds <= 600:
        parser.error('--timeout-seconds must be 30 through 600')
    return args


def main(argv=None):
    args = arguments(argv)
    args.output_dir.mkdir(parents=True)
    try:
        save(args.output_dir, 'run.json', {'python': sys.version, 'scriptSHA256': digest(Path(__file__)),
                                         'binary': str(args.binary), 'binarySHA256': digest(args.binary), 'input': str(args.input_dir), 'cases': args.case or CASES})
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
