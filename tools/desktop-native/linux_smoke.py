#!/usr/bin/python3
"""Bounded real GTK/WebKit smoke in a private D-Bus/Xvfb session (no build)."""

import argparse
import ctypes
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
import shutil
import signal
import subprocess
import sys
import time
import traceback


ROOT = Path(__file__).resolve().parents[2]
MIN_SOURCE_LIMIT = 16 << 20
MAX_SOURCE_LIMIT = 1 << 30


def arguments(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, default=ROOT / 'desktop/build/bin/MuxRay')
    parser.add_argument('--output-dir', type=Path, required=True, help='new absolute evidence directory')
    parser.add_argument('--source', type=Path, help='read-only local FLV; otherwise generate a small structural fixture')
    parser.add_argument('--tag', type=int, help='supported detail Tag; default generated=4, supplied source=0')
    parser.add_argument('--continue-context', type=int, default=0, help='explicit AVC parameter replay actions, 0 through 4; export every stage')
    parser.add_argument('--revisit-tag', type=int, help='after replay, visit an earlier Tag then return to the selected Tag')
    parser.add_argument('--mp3-browse', action='store_true', help='select frame 2 then last selectable frame, exporting each')
    parser.add_argument('--avc-evidence', action='store_true', help='click AVC declaration/NAL/slice source buttons and save each Hex selection UI')
    parser.add_argument('--avc-search', choices=['mismatches', 'unknown', 'all'], help='inspect real bounded AVC search pages and source/detail navigation')
    parser.add_argument('--avc-search-pages', type=int, default=1, help='maximum explicit AVC search batches, 1 through 16')
    parser.add_argument('--mp3-history', action='store_true', help='inspect and export bounded MP3 dependency sources and click their source ranges')
    parser.add_argument('--mp3-frame', type=int, help='select a specific inspected MP3 frame before resolving history, 0 through 63')
    parser.add_argument('--media-curves', choices=['audio', 'video', 'mp3', 'gopAverage', 'gopMax'], help='analyze media and load the current curve page before details')
    parser.add_argument('--max-source-bytes', type=int, default=MIN_SOURCE_LIMIT, help='16 MiB default; at most 1 GiB')
    parser.add_argument('--timeout-seconds', type=int, default=180, help='whole session deadline, 30–600 seconds; cleanup gets up to 10 more')
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if not args.output_dir.is_absolute():
        parser.error('--output-dir must be absolute')
    if not MIN_SOURCE_LIMIT <= args.max_source_bytes <= MAX_SOURCE_LIMIT:
        parser.error('--max-source-bytes must be between 16777216 and 1073741824')
    if not 30 <= args.timeout_seconds <= 600:
        parser.error('--timeout-seconds must be between 30 and 600')
    if args.tag is None:
        args.tag = 0 if args.source else 4
    if not 0 <= args.tag < 1_000_000:
        parser.error('--tag must be between 0 and 999999 (desktop index budget)')
    if not 0 <= args.continue_context <= 4:
        parser.error('--continue-context must be between 0 and 4')
    if args.revisit_tag is not None and (not args.continue_context or not 0 <= args.revisit_tag < args.tag):
        parser.error('--revisit-tag requires replay and an earlier nonnegative Tag')
    if not 1 <= args.avc_search_pages <= 16 or (args.avc_search_pages != 1 and not args.avc_search):
        parser.error('--avc-search-pages requires search and must be 1 through 16')
    if args.mp3_frame is not None and (not args.mp3_history or not 0 <= args.mp3_frame < 64):
        parser.error('--mp3-frame requires history and must be 0 through 63')
    if args.avc_search and args.mp3_history:
        parser.error('AVC search and MP3 history are separate native rounds')
    args.binary = args.binary.resolve()
    if not args.binary.is_file() or not os.access(args.binary, os.X_OK):
        parser.error('--binary must be an executable file; build with desktop/build-production.sh first')
    if args.source:
        args.source = args.source.resolve()
        if not args.source.is_file() or not 13 <= args.source.stat().st_size <= args.max_source_bytes:
            parser.error('--source must be a regular FLV file, 13 bytes through --max-source-bytes')
    if not args.worker and args.output_dir.exists():
        parser.error('--output-dir already exists; use a new directory to preserve evidence')
    return args


def save(out, name, value):
    (out / name).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def digest(path):
    value = hashlib.sha256()
    with path.open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            value.update(block)
    return value.hexdigest()


def generated_fixture():
    from hex_fixtures import fixtures, tag
    data, records = fixtures()['hex-details.flv']
    selected = records[4]
    end = selected['offset'] + 15 + selected['payloadBytes']
    # The historical Hex fixture's 65 88 NAL predates slice-prefix parsing.
    # Keep its other Tags but supply the minimum valid prefix for its PPS/SPS:
    # first_mb=0, I slice=2, PPS=0, frame_num=0 (4 bits), idr_pic_id=0.
    payload = bytes.fromhex('17010000000000000365b840')
    return data[:selected['offset']] + tag(9, payload) + data[end:]


def parse_stat(raw):
    # comm may contain spaces or parentheses; fields after its last ')' start at state.
    fields = raw[raw.rfind(')') + 2:].split()
    return {'ppid': int(fields[1]), 'startTicks': fields[19], 'state': fields[0]}


def process(pid):
    try:
        return {'pid': pid, **parse_stat((Path('/proc') / str(pid) / 'stat').read_text())}
    except (OSError, ValueError, IndexError):
        return None


def alive(row):
    current = process(row['pid'])
    return current is not None and current['startTicks'] == row['startTicks']


def running(row):
    current = process(row['pid'])
    return current is not None and current['startTicks'] == row['startTicks'] and current['state'] != 'Z'


def observe(root_pid, known):
    table = {}
    for entry in Path('/proc').iterdir():
        if entry.name.isdecimal():
            row = process(int(entry.name))
            if row:
                table[row['pid']] = row
    owned = {pid for pid, row in known.items()
             if pid in table and table[pid]['startTicks'] == row['startTicks']}
    if root_pid in table and root_pid not in known:
        owned.add(root_pid)
    while True:
        children = {pid for pid, row in table.items() if row['ppid'] in owned}
        if children <= owned:
            break
        owned.update(children)
    for pid in owned:
        known[pid] = table[pid]
    return [known[pid] for pid in sorted(owned)]


def source_fds(rows, source):
    stat = source.stat()
    held = []
    for row in rows:
        if not alive(row):
            continue
        try:
            for fd in (Path('/proc') / str(row['pid']) / 'fd').iterdir():
                try:
                    item = fd.stat()
                    if (item.st_dev, item.st_ino) == (stat.st_dev, stat.st_ino):
                        held.append({'pid': row['pid'], 'fd': fd.name})
                except FileNotFoundError:
                    pass
        except FileNotFoundError:
            pass
    return held


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def validate_export(snapshot, tag, generated):
    require(snapshot['session']['scan']['complete'], 'smoke requires a complete container index')
    selected = snapshot['selectedTag']
    require(selected is not None and int(selected['id']) == tag, 'exported Tag identity differs')
    require(snapshot['detail'] is not None and snapshot['hex'] is not None, 'detail/Hex missing from export')
    require(snapshot['hex']['window']['offset'] == selected['offset'], 'Hex is not positioned at the selected Tag')
    if generated:
        require(snapshot['detail']['detail']['complete'], 'generated fixture detail is incomplete')


def run_worker(args):
    import pyatspi
    from Xlib import X, XK, display, protocol
    from Xlib.ext import xtest
    from snapshot_check import BoundedSource, check_snapshot, fixture_tag_offsets

    out, source = args.output_dir, args.source
    require(os.environ.get('GSETTINGS_BACKEND') == 'memory', 'worker requires isolated launcher')
    record = json.loads((out / 'run.json').read_text())
    with BoundedSource(source, args.max_source_bytes) as stream:
        require(args.tag in fixture_tag_offsets(stream, {args.tag}), 'requested Tag does not exist in source')
        before_hash = stream.digest()
    record['sourceSHA256'] = before_hash
    save(out, 'run.json', record)
    subprocess.run(['gdbus', 'call', '--session', '--dest', 'org.a11y.Bus', '--object-path',
                    '/org/a11y/bus', '--method', 'org.freedesktop.DBus.Properties.Set',
                    'org.a11y.Status', 'IsEnabled', '<true>'], check=True, timeout=10)
    screen = display.Display()
    known = {}
    with (out / 'parent.stdout.log').open('w') as stdout, (out / 'parent.stderr.log').open('w') as stderr:
        app = subprocess.Popen([str(args.binary)], stdout=stdout, stderr=stderr)
    observe(app.pid, known)
    executable_hash = digest(Path('/proc') / str(app.pid) / 'exe')
    require(executable_hash == record['binarySHA256'], 'running executable differs from recorded binary')
    save(out, 'app-process.json', {**known[app.pid], 'executableSHA256': executable_hash})

    def rows():
        desktop = pyatspi.Registry.getDesktop(0)
        stack = [desktop.getChildAtIndex(i) for i in range(desktop.childCount)]
        stack = [(node, 0) for node in stack if node.get_process_id() == app.pid]
        visited = 0
        while stack:
            node, depth = stack.pop()
            visited += 1
            require(visited <= 16000, 'accessible tree exceeded 16000-node budget')
            try:
                role, name = node.getRoleName(), node.name
                children = [(node.getChildAtIndex(i), depth + 1) for i in reversed(range(node.childCount))]
            except Exception:
                continue
            yield node, depth, role, name
            stack.extend(children)

    def node_text(node):
        try:
            text = node.queryText()
            return text.getText(0, min(text.characterCount, 1200))
        except Exception:
            return ''

    def dump(name):
        save(out, name, [{'depth': depth, 'role': role, 'name': name_, 'description': node.description, 'text': node_text(node)}
                         for node, depth, role, name_ in rows()])

    def wait(fn, description, seconds=35):
        deadline = time.monotonic() + seconds
        while time.monotonic() < deadline:
            observe(app.pid, known)
            require(app.poll() is None, f'parent exited early: {app.returncode}')
            value = fn()
            if value:
                return value
            time.sleep(.2)
        raise RuntimeError('native UI timeout: ' + description)

    def find(role, name, depth=None):
        matches = wait(lambda: [node for node, d, r, n in rows()
                                if r == role and n == name and (depth is None or d == depth)], f'{role}: {name}')
        require(len(matches) == 1, f'ambiguous accessible target {role}: {name}')
        return matches[0]

    def click(name, role='push button'):
        node = find(role, name)
        require(node.getState().contains(pyatspi.STATE_ENABLED), 'disabled target: ' + name)
        action = node.queryAction()
        require(action.nActions == 1 and action.doAction(0), 'action failed: ' + name)

    def wait_text(fragment, seconds=35):
        return wait(lambda: any(fragment in name or fragment in node_text(node)
                                for node, _, _, name in rows()), fragment, seconds)

    def choose(label, index):
        node = find('combo box', label)
        # WebKitGTK exposes HTML options as menu items rather than Selection.
        # Invoke the selected item's native action once; arrow-key changes can
        # disable the selector while an intermediate detail request is running.
        pending, items = [node], []
        while pending:
            item = pending.pop()
            if item.getRoleName() == 'menu item':
                items.append(item)
            pending.extend(item.getChildAtIndex(i) for i in reversed(range(item.childCount)))
        require(0 <= index < len(items), 'select option out of range: ' + label)
        action = items[index].queryAction()
        require(action.nActions == 1 and action.doAction(0), 'select option action failed: ' + label)

    def window(title):
        found = []
        for item in screen.screen().root.query_tree().children:
            pid = item.get_full_property(screen.intern_atom('_NET_WM_PID'), X.AnyPropertyType)
            text = item.get_full_property(screen.intern_atom('_NET_WM_NAME'), X.AnyPropertyType)
            name = text.value.decode() if text else item.get_wm_name()
            if pid and int(pid.value[0]) == app.pid and name == title:
                found.append(item)
        require(len(found) == 1, 'missing/ambiguous owned window: ' + title)
        return found[0]

    def focus(title):
        window(title).set_input_focus(X.RevertToParent, X.CurrentTime)
        screen.sync()

    def key(name, down):
        code = screen.keysym_to_keycode(XK.string_to_keysym(name))
        require(bool(code), 'missing key: ' + name)
        xtest.fake_input(screen, X.KeyPress if down else X.KeyRelease, code)

    def combo(letter):
        key('Control_L', True)
        key(letter, True)
        key(letter, False)
        key('Control_L', False)
        screen.sync()

    def picker_path(path, depth):
        entry = find('text', '', depth)
        require(entry.queryEditableText().setTextContents(str(path)), 'picker path write failed')
        require(node_text(entry) == str(path), 'picker path readback mismatch')

    def select_tag(tag):
        entry = find('entry', 'Tag ID')
        require(entry.queryComponent().grabFocus(), 'Tag ID focus failed')
        combo('a')
        for char in str(tag):
            key(char, True)
            key(char, False)
        screen.sync()
        wait(lambda: node_text(find('entry', 'Tag ID')) == str(tag), 'Tag ID input')
        click('跳转 Tag')
        click(f'查看 Tag {tag} 详情', 'toggle button')
        wait_text('详情诊断 (')

    def export_snapshot(name, tag):
        click(f'查看 Tag {tag} 原始字节')
        wait(lambda: any(name_.startswith('查看 byte ') for _, _, _, name_ in rows()), 'Hex bytes')
        path = out / name
        click('导出诊断 JSON')
        find('push button', 'Save')
        picker_path(path, 6)
        click('Save')
        wait_text('已保存 ' + name)
        snapshot = json.loads(path.read_text())
        validate_export(snapshot, tag, record['origin'] == 'generated')
        checked = check_snapshot(path, source, args.max_source_bytes)
        require(checked['sourceSHA256'] == before_hash, 'source changed during native flow')
        return snapshot, checked

    result = {'passed': False, 'pid': app.pid}
    try:
        wait_text('从一个文件开始')
        click('打开 FLV 文件')
        find('push button', 'Cancel')
        focus('选择 FLV 文件')
        combo('l')
        picker_path(source, 11)
        click('Open')
        # A broad '部分结果' match also occurs in the still-scanning placeholder.
        wait_text('容器索引完整', 90)
        focus('MuxRay · 码流透镜')
        if args.media_curves:
            click('分析媒体')
            wait(lambda: find('push button', '查看媒体曲线').getState().contains(pyatspi.STATE_ENABLED), 'media summary ready', 90)
            click('查看媒体曲线')
            wait_text(' · 本页 ')
            metric = {'audio': '音频码率', 'video': '视频码率', 'mp3': 'MP3 码率', 'gopAverage': 'GOP 平均时长', 'gopMax': 'GOP 最长时长'}[args.media_curves]
            click(metric, 'toggle button')
        if args.avc_search:
            labels = {'mismatches': 0, 'unknown': 1, 'all': 2}
            if args.avc_search == 'mismatches':
                click('检索 H.264 证据')
            else:
                choose('H.264 证据筛选', labels[args.avc_search])
            result['avcSearchPages'] = []
            previous_status = None
            for index in range(args.avc_search_pages):
                status = wait(lambda: next((node_text(node) for node, _, _, _ in rows()
                              if node_text(node).startswith('H.264 检索完成：') and
                              node_text(node) != previous_status), None), 'AVC search batch ready')
                name = f'avc-search-{index}.json'
                dump(name)
                result['avcSearchPages'].append({'uiFile': name, 'uiSHA256': digest(out / name), 'status': status})
                next_button = find('push button', '继续检索 H.264')
                if not next_button.getState().contains(pyatspi.STATE_ENABLED) or index + 1 == args.avc_search_pages:
                    break
                previous_status = status
                click('继续检索 H.264')
            if len(result['avcSearchPages']) > 1:
                click('上一批 H.264')
                previous = result['avcSearchPages'][-2]['status']
                wait_text(previous)
                dump('avc-search-previous.json')
                result['avcSearchPrevious'] = {'uiFile': 'avc-search-previous.json', 'uiSHA256': digest(out / 'avc-search-previous.json'), 'status': previous}
                click('继续检索 H.264')
                wait_text(result['avcSearchPages'][-1]['status'])
            targets = [(name, int(match[1])) for _, _, _, name in rows()
                       if (match := re.fullmatch(r'定位 H\.264 Tag (\d+) 证据', name))]
            if targets:
                button, target = targets[0]
                click(button)
                wait_text(f'H.264 Tag {target} 证据 · ')
                dump('avc-search-source.json')
                result['avcSearchSource'] = {'tagId': target, 'uiFile': 'avc-search-source.json', 'uiSHA256': digest(out / 'avc-search-source.json')}
                click(f'从 H.264 检索跳转 Tag {target}')
                wait_text('详情诊断 (')
                _, checked = export_snapshot('avc-search-selected.json', target)
                result['avcSearchSelected'] = {'tagId': target, 'file': 'avc-search-selected.json', 'snapshotCheck': checked}
        select_tag(args.tag)
        if args.mp3_history:
            if args.mp3_frame is not None:
                choose('MP3 帧选择', args.mp3_frame + 1)
                wait_text(f'当前展开 MP3 帧 {args.mp3_frame} ·')
            click('定位 MP3 依赖来源')
            wait_text('MP3 依赖定位完成：')
            initial, checked = export_snapshot('mp3-history-initial.json', args.tag)
            result['mp3HistoryInitial'] = {'file': 'mp3-history-initial.json', 'snapshotCheck': checked}
            fields = initial['detail']['detail']['fields']
            roots = [f for f in fields if f['name'] == 'MP3 dependency sources']
            require(len(roots) == 1, 'MP3 dependency root missing')
            sources = [f for f in fields if f['parentId'] == roots[0]['id'] and f['type'] == 'physical main-data source']
            require(len(sources) <= 64, 'MP3 source budget exceeded')
            result['mp3HistorySources'] = []
            for index, item in enumerate(sources):
                child = {f['name']: f for f in fields if f['parentId'] == item['id']}
                tag, frame, span = child['sourceTagId']['value'], child['sourceFrameIndex']['value'], child['sourceSpan']['span']
                click(f'定位 MP3 来源 Tag {tag} 帧 {frame}')
                text = f'MP3 来源 Tag {tag} 帧 {frame} · {span["byteOffset"]} : 0 · {span["bitLength"]} bit'
                wait_text(text)
                name = f'mp3-history-source-{index}.json'
                dump(name)
                result['mp3HistorySources'].append({'tagId': tag, 'frameIndex': frame, 'span': span, 'selectionText': text,
                                                   'uiFile': name, 'uiSHA256': digest(out / name)})
            if sources:
                first = result['mp3HistorySources'][0]
                click(f'跳转 MP3 来源 Tag {first["tagId"]} 帧 {first["frameIndex"]}')
                wait_text(f'当前展开 MP3 帧 {first["frameIndex"]} ·')
                _, checked = export_snapshot('mp3-history-source-detail.json', int(first['tagId']))
                result['mp3HistorySourceDetail'] = {'file': 'mp3-history-source-detail.json', 'snapshotCheck': checked,
                                                  'tagId': first['tagId'], 'frameIndex': first['frameIndex']}
                select_tag(args.tag)
                if args.mp3_frame is not None:
                    choose('MP3 帧选择', args.mp3_frame + 1)
                    wait_text(f'当前展开 MP3 帧 {args.mp3_frame} ·')
                click('定位 MP3 依赖来源')
                wait_text('MP3 依赖定位完成：')
        if args.mp3_browse:
            wait_text('当前显示前两帧预览')
            for index in range(3):
                click('下一 MP3 帧')
                wait_text(f'当前展开 MP3 帧 {index} ·')
            _, checked = export_snapshot('mp3-frame-2.json', args.tag)
            result['mp3Stages'] = [{'file': 'mp3-frame-2.json', 'snapshotCheck': checked}]
            click('末个可选 MP3 帧')
            wait(lambda: not any('当前展开 MP3 帧 2 ·' in name or '当前展开 MP3 帧 2 ·' in node_text(node) for node, _, _, name in rows()), 'last MP3 frame ready')
            wait_text('当前展开 MP3 帧 ')

        if args.avc_evidence:
            initial, checked = export_snapshot('avc-evidence-initial.json', args.tag)
            result['avcEvidenceInitial'] = {'file': 'avc-evidence-initial.json', 'snapshotCheck': checked}
            fields = initial['detail']['detail']['fields']
            roots = [field for field in fields if field['name'] == 'Keyframe evidence']
            require(len(roots) == 1, 'AVC evidence root is unavailable')
            targets = [field for field in fields if field['parentId'] == roots[0]['id'] and field['span'] is not None and
                       (field['name'] in ('declaredFrameType', 'evidenceNALType', 'evidenceIDRNALType') or field['name'].startswith('evidenceSliceType'))]
            require(3 <= len(targets) <= 8, 'AVC evidence source buttons missing or unbounded')
            result['avcEvidenceStages'] = []
            for field in targets:
                name, span = field['name'], field['span']
                button = {'declaredFrameType': '定位 FLV 帧类型', 'evidenceNALType': '定位 NAL 头证据', 'evidenceIDRNALType': '定位 IDR NAL'}.get(name)
                if button is None:
                    button = '定位 ' + name.removeprefix('evidenceSliceType') + ' slice'
                label = 'FLV frameType' if name == 'declaredFrameType' else name
                text = f'{label} · {span["byteOffset"]} : {span["bitOffset"]} · {span["bitLength"]} bit'
                click(button)
                wait_text(text)
                wait(lambda: any(n == f'查看 byte {span["byteOffset"]} 的位' for _, _, _, n in rows()), 'AVC source Hex bytes')
                ui = 'avc-' + name + '-ui.json'
                dump(ui)
                result['avcEvidenceStages'].append({'field': name, 'button': button, 'span': span, 'selectionText': text,
                                                   'uiFile': ui, 'uiSHA256': digest(out / ui)})

        if args.continue_context:
            snapshot, checked = export_snapshot('context-initial.json', args.tag)
            result['contextStages'] = [{'file': 'context-initial.json', 'snapshotCheck': checked}]
            for index in range(args.continue_context):
                next_tag = next(f['value'] for f in snapshot['detail']['detail']['fields'] if f['name'] == 'nextTagId')
                previous_progress = f'参数上下文已检查到 Tag {next_tag} 之前'
                click('继续准备参数')
                wait(lambda: not any(previous_progress in name or previous_progress in node_text(node) for node, _, _, name in rows()), 'parameter context advanced')
                name = f'context-{index + 1}.json'
                snapshot, checked = export_snapshot(name, args.tag)
                result['contextStages'].append({'file': name, 'snapshotCheck': checked})
            if args.revisit_tag is not None:
                select_tag(args.revisit_tag)
                _, checked = export_snapshot('context-revisit.json', args.revisit_tag)
                result['contextStages'].append({'file': 'context-revisit.json', 'snapshotCheck': checked})
                select_tag(args.tag)
        _, result['snapshotCheck'] = export_snapshot('snapshot.json', args.tag)
        dump('saved-ui.json')
        result['sourceFDsBeforeClose'] = source_fds(observe(app.pid, known), source)
        require(result['sourceFDsBeforeClose'], 'no owned process holds the open source')
        click('关闭文件')
        wait_text('从一个文件开始')
        wait(lambda: not source_fds(observe(app.pid, known), source), 'source FD release')
        result['sourceFDsAfterClose'] = source_fds(observe(app.pid, known), source)
        with BoundedSource(source, args.max_source_bytes) as stream:
            require(stream.digest() == before_hash, 'source changed before close completed')
        result['sourceUnchanged'] = True
        dump('closed-ui.json')
        main = window('MuxRay · 码流透镜')
        delete = screen.intern_atom('WM_DELETE_WINDOW')
        require(delete in main.get_wm_protocols(), 'window does not accept WM_DELETE_WINDOW')
        main.send_event(protocol.event.ClientMessage(window=main, client_type=screen.intern_atom('WM_PROTOCOLS'),
                        data=(32, [delete, X.CurrentTime, 0, 0, 0])), event_mask=X.NoEventMask)
        screen.sync()
        result['exitCode'] = app.wait(timeout=15)
        require(result['exitCode'] == 0, 'parent did not exit normally')
        deadline = time.monotonic() + 5
        while any(alive(row) for row in known.values()) and time.monotonic() < deadline:
            time.sleep(.1)
        result['observedAppProcesses'] = list(known.values())
        result['remainingAppProcesses'] = [row for row in known.values() if alive(row)]
        require(not result['remainingAppProcesses'], 'observed app descendants remain after exit')
        result['passed'] = True
    except BaseException as exc:
        result.update(error=repr(exc), traceback=traceback.format_exc())
        save(out, 'native-result.json', result)
        # The outer deadline still applies if an accessibility query itself is stuck.
        if app.poll() is None:
            dump('failure-ui.json')
        raise
    finally:
        save(out, 'native-result.json', result)
        screen.close()
    return 0


def launch(args):
    missing = [name for name in ('dbus-run-session', 'xvfb-run', 'Xvfb', 'xauth', 'gdbus') if not shutil.which(name)]
    missing += [name for name in ('pyatspi', 'Xlib', 'jsonschema') if importlib.util.find_spec(name) is None]
    require(sys.platform == 'linux', 'Linux is required')
    require(not missing, 'missing dependencies: ' + ', '.join(missing) +
            '; Ubuntu packages: dbus-x11 xvfb xauth libglib2.0-bin at-spi2-core python3-pyatspi python3-xlib python3-jsonschema')
    # Adopt and reap only this launcher's orphaned descendants, including helper daemons.
    require(ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) == 0, 'cannot enable Linux child subreaper')
    out = args.output_dir
    out.mkdir(parents=True, mode=0o700)
    env = os.environ.copy()
    for name in ('AT_SPI_BUS_ADDRESS', 'AT_SPI_DISPLAY', 'WAYLAND_DISPLAY', 'DBUS_SESSION_BUS_ADDRESS'):
        env.pop(name, None)
    env.update(GSETTINGS_BACKEND='memory', GDK_BACKEND='x11', NO_AT_BRIDGE='0', LC_ALL='C.UTF-8')
    for name, dirname in [('XDG_CONFIG_HOME', 'config'), ('XDG_DATA_HOME', 'data'), ('XDG_CACHE_HOME', 'cache'),
                          ('XDG_STATE_HOME', 'state'), ('XDG_RUNTIME_DIR', 'runtime'), ('TMPDIR', 'tmp')]:
        path = out / dirname
        path.mkdir(mode=0o700)
        env[name] = str(path)
    generated = args.source is None
    if generated:
        args.source = out / 'generated-details.flv'
        args.source.write_bytes(generated_fixture())
    # Start the bus inside Xvfb so D-Bus-activated AT-SPI/GTK helpers inherit
    # the private DISPLAY too, rather than the user's physical desktop.
    command = ['xvfb-run', '-a', '-s', '-screen 0 1600x1200x24 -nolisten tcp', 'dbus-run-session', '--',
               sys.executable, '-B', str(Path(__file__).resolve()), '--worker', '--binary', str(args.binary),
               '--output-dir', str(out), '--source', str(args.source), '--tag', str(args.tag),
               '--max-source-bytes', str(args.max_source_bytes), '--timeout-seconds', str(args.timeout_seconds)]
    command.extend(['--continue-context', str(args.continue_context)])
    if args.mp3_browse:
        command.append('--mp3-browse')
    if args.avc_evidence:
        command.append('--avc-evidence')
    if args.avc_search:
        command.extend(['--avc-search', args.avc_search, '--avc-search-pages', str(args.avc_search_pages)])
    if args.mp3_history:
        command.append('--mp3-history')
    if args.mp3_frame is not None:
        command.extend(['--mp3-frame', str(args.mp3_frame)])
    if args.media_curves:
        command.extend(['--media-curves', args.media_curves])
    if args.revisit_tag is not None:
        command.extend(['--revisit-tag', str(args.revisit_tag)])
    record = {'binary': str(args.binary), 'binarySHA256': digest(args.binary), 'source': str(args.source),
              'sourceBytes': args.source.stat().st_size, 'origin': 'generated' if generated else 'user-supplied',
              'tag': args.tag, 'maxSourceBytes': args.max_source_bytes, 'timeoutSeconds': args.timeout_seconds,
              'continueContext': args.continue_context, 'revisitTag': args.revisit_tag,
              'mp3Browse': args.mp3_browse, 'avcEvidence': args.avc_evidence, 'mediaCurves': args.media_curves,
              'avcSearch': args.avc_search, 'avcSearchPages': args.avc_search_pages, 'mp3History': args.mp3_history, 'mp3Frame': args.mp3_frame,
              'argv': command, 'environment': {key: env[key] for key in env if key.startswith('XDG_') or key in
                                            ('GSETTINGS_BACKEND', 'GDK_BACKEND', 'LC_ALL', 'NO_AT_BRIDGE', 'TMPDIR')}}
    save(out, 'run.json', record)
    known, runner = {}, None
    result = {'passed': False, 'forcedCleanup': [], 'remainingOwnedProcesses': []}
    started = time.monotonic()

    def track():
        observe(os.getpid(), known)
        known.pop(os.getpid(), None)
        # Reap adopted children while the worker checks that WebKit descendants disappeared.
        for row in known.values():
            if row['pid'] != runner.pid and row['ppid'] == os.getpid():
                try:
                    os.waitpid(row['pid'], os.WNOHANG)
                except ChildProcessError:
                    pass

    def interrupted(signum, frame):
        raise KeyboardInterrupt(f'signal {signum}')

    signal.signal(signal.SIGTERM, interrupted)

    try:
        with (out / 'runner.stdout.log').open('w') as stdout, (out / 'runner.stderr.log').open('w') as stderr:
            runner = subprocess.Popen(command, env=env, stdout=stdout, stderr=stderr, start_new_session=True)
        while runner.poll() is None:
            track()
            require(time.monotonic() - started < args.timeout_seconds, 'whole native session exceeded deadline')
            time.sleep(.1)
        result['runnerExitCode'] = runner.returncode
        require(runner.returncode == 0, 'native runner failed; inspect runner.stderr.log and native-result.json')
        native = json.loads((out / 'native-result.json').read_text())
        require(native['passed'], 'native checks did not pass')
        result['passed'] = True
    except BaseException as exc:
        result.update(error=repr(exc), traceback=traceback.format_exc())
    finally:
        # Only observed descendants with the same PID generation are signaled.
        if runner:
            track()
        for sig, seconds in ((signal.SIGTERM, 3), (signal.SIGKILL, 3)):
            for row in reversed(list(known.values())):
                if running(row):
                    try:
                        os.kill(row['pid'], sig)
                        result['forcedCleanup'].append({'pid': row['pid'], 'startTicks': row['startTicks'], 'signal': sig.name})
                    except ProcessLookupError:
                        pass
            deadline = time.monotonic() + seconds
            while any(running(row) for row in known.values()) and time.monotonic() < deadline:
                time.sleep(.1)
        if runner:
            runner.wait(timeout=2)
        # A daemon can become an adopted zombie just after its parent's exit.
        # Give reparenting/reaping a bounded grace period, rather than accepting
        # one WNOHANG=0 as proof that every observed descendant has disappeared.
        deadline = time.monotonic() + 2
        while True:
            if runner:
                track()
            try:
                while os.waitpid(-1, os.WNOHANG)[0] != 0:
                    pass
            except ChildProcessError:
                pass
            if not any(alive(row) for row in known.values()) or time.monotonic() >= deadline:
                break
            time.sleep(.05)
        result['remainingOwnedProcesses'] = [row for row in known.values() if alive(row)]
        result['observedProcesses'] = list(known.values())
        result['seconds'] = round(time.monotonic() - started, 2)
        if result['remainingOwnedProcesses']:
            result['passed'] = False
        save(out, 'result.json', result)
    print(json.dumps({'passed': result['passed'], 'evidence': str(out), 'seconds': result['seconds']}, ensure_ascii=False))
    return 0 if result['passed'] else 1


def main():
    args = arguments()
    try:
        return run_worker(args) if args.worker else launch(args)
    except (OSError, RuntimeError, ValueError) as exc:
        print(str(exc), file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
