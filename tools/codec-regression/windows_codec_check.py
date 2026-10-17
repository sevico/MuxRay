#!/usr/bin/env python3
"""Read-only codec oracle checks for actual Windows exports and captured UI.

This tool performs no GUI actions and makes no lifecycle/automation claim.
It never creates product snapshots, UI evidence, or success-action receipts.
"""

import argparse
import hashlib
import json
from pathlib import Path
import re
import sys

from codec_search_verify import identity, page_rows
from generate import ROOT, digest, load_expectations, require
from verify_paged_codec import verify_mp3_native_page

sys.path.insert(0, str(ROOT / 'tools/desktop-native'))
from snapshot_check import check_snapshot
from linux_smoke import check_mp3_navigation_stage

MAX_UI_BYTES = 8 << 20


def strings(value):
    if isinstance(value, str):
        yield value
    elif isinstance(value, list):
        for item in value:
            yield from strings(item)
    elif isinstance(value, dict):
        for item in value.values():
            yield from strings(item)


def captured_ui(path):
    """Accept unchanged UTF-8 state text or JSON returned by the GUI tool."""
    raw = path.read_bytes()
    require(len(raw) <= MAX_UI_BYTES, 'UI evidence exceeds 8 MiB')
    text = raw.decode('utf-8-sig')
    try:
        value = json.loads(text)
    except json.JSONDecodeError:
        pass
    else:
        text = '\n'.join(strings(value))
    require(bool(text.strip()), 'UI evidence has no observed text')
    return text, {'file': str(path.resolve()), 'sha256': hashlib.sha256(raw).hexdigest(), 'bytes': len(raw)}


def require_ui_page(text, response):
    p = response['page']
    status = (f'编解码器检索完成：{p["kind"]} · Tag {p["start"]["tagId"]} / 帧 {p["start"]["frameIndex"]} → '
              f'Tag {p["next"]["tagId"]} / 帧 {p["next"]["frameIndex"]} · 检查 {p["scannedTags"]} 个 Tag、'
              f'{p["scannedFrames"]} 个帧头 · 本批 {len(p["rows"])} 条 · 待显示 {p["pending"]} 条 · 读取 {p["bytesRead"]} byte。')
    require(status in text, 'captured UI lacks the exact current page coverage/status')
    names = []
    for row in p['rows']:
        row_id = identity(row)
        names.append(f'从编解码器检索跳转 {row_id} Tag')
        frame = f'从编解码器检索跳转 {row_id} 帧'
        if row['frameIndex'] is not None and row['complete'] and row['status'] == 'complete':
            names.append(frame)
        else:
            require(frame not in text, 'captured UI offers a frame action for an incomplete/non-frame row')
        for key, label in [('span', '证据'), ('spsSpan', 'SPS'), ('evidenceSpan', '选择依据')]:
            control = f'定位编解码器 {row_id} {label}'
            if row[key] is not None:
                names.append(control)
            else:
                require(control not in text, 'captured UI offers a source action without an authored span')
    require(all(name in text for name in names), 'captured UI lacks one or more current row/source controls; preserve full state')
    return {'coverage': 'exact-status-and-expected-row-controls', 'controlsCompared': len(names),
            'limits': 'Text presence checks do not establish clicks, visual layout, source-title attributes, or native lifecycle.'}


def require_ui_frame(text, target):
    # A target number in the search result or input is not evidence that the
    # displayed detail actually selected it. Reject conflicting status text too.
    expanded = set(re.findall(r'当前展开 MP3 帧 ([0-9]+)', text))
    require('MP3 逐帧详情' in text and expanded == {str(target)},
            'captured UI does not expose the exact currently expanded MP3 frame')
    return {'coverage': 'frame-panel-and-exact-expanded-frame-status'}


def ax_group_text(text, label):
    """Read only ordered direct text children of an actual named AX group.

    Windows exposes JSX text fragments as separate nodes. Keep group and
    indentation boundaries: numbers from another group or nested controls
    must never complete a missing field. No expected values enter this parser.
    """
    lines = text.splitlines()
    groups = []
    heading = re.compile(r'^([ \t]*)[0-9]+ 组 ' + re.escape(label) + r'$')
    node = re.compile(r'^([ \t]*)[0-9]+ (\S+)(?: (.*))?$')
    for index, line in enumerate(lines):
        match = heading.fullmatch(line)
        if match is None:
            continue
        level = len(match[1].expandtabs(8))
        child_level, fragments = None, []
        for following in lines[index + 1:]:
            if not following.strip():
                break
            child = node.fullmatch(following)
            if child is None:
                break
            depth = len(child[1].expandtabs(8))
            if depth <= level:
                break
            if child_level is None:
                child_level = depth
            if depth == child_level and child[2] == '文本':
                fragments.append(child[3] or '')
        groups.append(''.join(fragments))
    return groups


def require_ui_source(text, span, window):
    selection = (int(span['byteOffset']), span['bitOffset'], span['bitLength'])
    selection_pattern = re.compile(r'(?<![0-9])([0-9]+)\s*:\s*([0-9]+)\s*·\s*([0-9]+)\s+bit(?![A-Za-z0-9_])')

    def exact_selection(value):
        spans = [tuple(map(int, match.groups())) for match in selection_pattern.finditer(value)]
        return bool(spans) and all(item == selection for item in spans)

    status = re.compile(r'实际窗口\s*\[\s*([0-9]+)\s*,\s*([0-9]+)\s*\)\s*·\s*([0-9]+)\s*byte / 文件\s*([0-9]+)\s*byte(\s*·\s*已到 EOF)?')

    def window_values(match):
        return (*map(int, match.groups()[:4]), match[5] is not None)
    has_ax = re.search(r'^[ \t]*[0-9]+ 组 ', text, re.MULTILINE) is not None
    if has_ax:
        selections = ax_group_text(text, 'Hex 定位范围')
        windows = ax_group_text(text, 'Hex 实际窗口')
        require(selections and all(exact_selection(value) for value in selections),
                'UI lacks exact source selection within its AX group')
        matches = [status.fullmatch(value) for value in windows]
        require(matches and all(matches), 'UI lacks ordered complete Hex window text within its AX group')
        observed = [window_values(match) for match in matches]
        mode = 'ordered-AX-group-text-children'
    else:
        require(exact_selection(text), 'UI lacks exact source selection')
        observed = [window_values(match) for match in status.finditer(text)]
        require(observed, 'UI lacks exact Hex window text')
        mode = 'contiguous-captured-text'
    offset = int(window['offset'])
    length = len(bytes.fromhex(window['hex']))
    expected = (offset, offset + length, length, int(window['sourceSize']), window['eof'])
    require(all(value == expected for value in observed), 'UI Hex window differs from the exported actual byte coverage')
    return {'coverage': 'exact-source-selection-and-Hex-window-text', 'textMode': mode,
            'actualWindow': {'start': offset, 'end': offset + length, 'bytes': length,
                             'sourceSize': int(window['sourceSize']), 'eof': window['eof']}}


def load_case(directory, name):
    expected = load_expectations(directory)
    require(name in expected['codecSearch'], 'unknown codec fixture case')
    case = expected['codecSearch'][name]
    source_path = directory / case['file']
    source = source_path.read_bytes()
    return case, source_path, source, {'expectationsSHA256': digest(directory / 'expectations.json'),
                                     'source': str(source_path.resolve()), 'sourceSHA256': digest(source_path)}


def checked_snapshot(path, source_path):
    receipt = check_snapshot(path, source_path)
    snapshot = json.loads(path.read_text(encoding='utf-8'))
    require(snapshot['schemaVersion'] == 4, 'latest codec acceptance requires snapshot v4')
    require(snapshot['fileName'] == source_path.name, 'export is for a different source filename')
    return snapshot, receipt


def check_pages(paths, case, filter_name, source_path):
    require(filter_name in case['filters'], 'filter is not part of the authored case')
    require(0 < len(paths) <= len(case['pages'][filter_name]), 'wrong page prefix length')
    previous, receipts, session = None, [], None
    for index, path in enumerate(paths):
        snapshot, checked = checked_snapshot(path, source_path)
        response = page_rows(snapshot, case, filter_name, index, previous)
        require(session is None or response['sessionId'] == session, 'page sequence changes document session')
        session, previous = response['sessionId'], response
        receipts.append({'pageIndex': index, **checked})
    return previous, receipts


def check_frame(paths, case, source_path, source):
    require('selectedFrame' in case and len(paths) == 3, 'frame 2600 requires all three actual navigation exports')
    receipts, prior, last, session, retained_page = [], None, None, None, None
    for index, path in enumerate(paths):
        last, receipt = checked_snapshot(path, source_path)
        require(session is None or last['session']['sessionId'] == session, 'frame stages change document session')
        require(last.get('codecSearchPage') is not None, 'frame navigation dropped the retained search page')
        require(retained_page is None or last['codecSearchPage'] == retained_page, 'frame navigation changed the retained search page')
        session, retained_page = last['session']['sessionId'], last['codecSearchPage']
        stage = check_mp3_navigation_stage(last, case['selectedFrame'], prior)
        require(stage['state'] == ['preparing', 'preparing', 'ready'][index] and
                stage['nextFrameIndex'] == [1024, 2048, 2624][index] and
                stage['replayPages'] == [16, 16, 9][index], 'frame navigation did not preserve the authored bounded progression')
        receipts.append({**receipt, **stage})
        prior = stage
    fields = verify_mp3_native_page(last, case['frameOracle'], source, 0, 2560, case['selectedFrame'])
    return {'stages': receipts, **fields}


def check_source(snapshot, case, destination):
    require(destination in case['sourceKinds'], 'source action is not in this authored case')
    target = next(row for row in case['rows'] if identity(row) == case['sourceTarget'])
    retained = (snapshot.get('codecSearchPage') or {}).get('page', {}).get('rows', [])
    require(target in retained, 'source export dropped or changed the authored search target row')
    key = {'hex': 'span', 'sps': 'spsSpan', 'evidence': 'evidenceSpan'}[destination]
    span = target[key]
    require(span is not None, 'authored source target has no physical span')
    require(snapshot.get('hex') is not None and snapshot['hex']['window']['offset'] == span['byteOffset'],
            'exported Hex does not start at the clicked authored source')
    return span


def run(args):
    case, source_path, source, provenance = load_case(args.input_dir, args.case)
    ui_text, ui_receipt = captured_ui(args.ui) if args.ui else ('', None)
    result = {'case': args.case, 'operation': args.operation, **provenance,
              'nativeAcceptance': 'not-established-by-this-read-only-checker',
              'ui': {'checked': False, 'reason': 'no actual UI evidence supplied'}}
    if args.operation == 'pages':
        require(args.filter is not None, 'pages requires --filter')
        response, receipts = check_pages(args.snapshot, case, args.filter, source_path)
        result.update(filter=args.filter, pages=receipts, completeFilter=len(receipts) == len(case['pages'][args.filter]))
        if ui_receipt:
            result['ui'] = {'checked': True, **ui_receipt, **require_ui_page(ui_text, response)}
    elif args.operation == 'frame':
        result.update(check_frame(args.snapshot, case, source_path, source))
        if ui_receipt:
            result['ui'] = {'checked': True, **ui_receipt, **require_ui_frame(ui_text, case['selectedFrame'])}
    else:
        require(args.destination is not None and len(args.snapshot) == 1, 'source requires one snapshot and --destination')
        snapshot, receipt = checked_snapshot(args.snapshot[0], source_path)
        span = check_source(snapshot, case, args.destination)
        result.update(snapshot=receipt, target=case['sourceTarget'], destination=args.destination, span=span)
        if ui_receipt:
            result['ui'] = {'checked': True, **ui_receipt,
                            **require_ui_source(ui_text, span, snapshot['hex']['window'])}
    # The independent loader checks every original source again after all reads.
    load_expectations(args.input_dir)
    result['passed'] = True
    return result


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('operation', choices=['pages', 'frame', 'source'])
    parser.add_argument('--input-dir', type=Path, required=True)
    parser.add_argument('--case', required=True)
    parser.add_argument('--filter')
    parser.add_argument('--snapshot', type=Path, action='append', required=True,
                        help='actual exported file, in original action order; repeat for every page from zero')
    parser.add_argument('--destination', choices=['hex', 'sps', 'evidence'])
    parser.add_argument('--ui', type=Path, help='unchanged UTF-8 text or JSON from actual GUI state capture for the final snapshot')
    parser.add_argument('--output', type=Path, help='optional CREATE_NEW verification receipt')
    args = parser.parse_args(argv)
    try:
        result = run(args)
    except Exception as error:
        result = {'passed': False, 'case': args.case, 'operation': args.operation,
                  'error': f'{type(error).__name__}: {error}',
                  'nativeAcceptance': 'not-established-by-this-read-only-checker'}
    encoded = json.dumps(result, ensure_ascii=False, indent=2) + '\n'
    if args.output:
        with args.output.open('x', encoding='utf-8', newline='\n') as stream:
            stream.write(encoded)
    print(encoded)
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
