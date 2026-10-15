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
    parser.add_argument('--mp3-summary', action='store_true', help='inspect all retained MP3 summary event pages and a late-frame Hex source')
    parser.add_argument('--mp3-summary-link', action='store_true', help='open the last retained complete MP3 event in paginated frame details')
    parser.add_argument('--mp3-page-steps', type=int, default=0, help='explicit next-frame-page actions, 0 through 64; retain each UI state')
    parser.add_argument('--mp3-jump-frame', type=int, help='enter an original Tag-relative uint32 frame index and explicitly continue bounded locating requests')
    parser.add_argument('--mp3-frame-walk', action='append', choices=['next', 'previous'], default=[], help='after direct jump, activate an adjacent frame button; at most 8 actions')
    parser.add_argument('--field-name', help='click one uniquely named source-backed detail field and retain Hex evidence')
    parser.add_argument('--avc-evidence', action='store_true', help='click AVC declaration/NAL/slice source buttons and save each Hex selection UI')
    parser.add_argument('--avc-search', choices=['mismatches', 'unknown', 'all'], help='inspect real bounded AVC search pages and source/detail navigation')
    parser.add_argument('--avc-search-pages', type=int, default=1, help='maximum explicit AVC search batches, 1 through 96')
    parser.add_argument('--avc-search-long', action='store_true', help='export only last page and check the complete 64-entry back-history boundary')
    parser.add_argument('--avc-search-gate', type=Path, help='explicit controlled overlay build directory; requires --avc-search-action')
    parser.add_argument('--avc-search-action', choices=['cancel', 'close'], help='native action while the controlled first source read is blocked')
    parser.add_argument('--codec-search', choices=['mp3', 'sei'], help='inspect ready codec evidence pages through native actions')
    parser.add_argument('--codec-search-filter', action='append', default=[], help='ordered codec filters; at most four distinct filters')
    parser.add_argument('--codec-search-pages', type=int, default=4, help='maximum pages per codec filter, 1 through 16')
    parser.add_argument('--codec-search-target', help='exact row identity tag:frame:nal:message, absent indices use -')
    parser.add_argument('--codec-search-frame', action='store_true', help='open the target complete MP3 frame and explicitly continue locating')
    parser.add_argument('--codec-search-source', action='append', choices=['hex', 'sps', 'evidence'], default=[])
    parser.add_argument('--codec-search-restart', action='store_true', help='retain first page, restart once and verify a new first request')
    parser.add_argument('--mp3-history', action='store_true', help='inspect and export bounded MP3 dependency sources and click their source ranges')
    parser.add_argument('--mp3-frame', type=int, help='select a global MP3 frame index on the inspected page')
    parser.add_argument('--media-curves', choices=['audio', 'video', 'mp3', 'gopAverage', 'gopMax'], help='analyze media and load the current curve page before details')
    parser.add_argument('--max-source-bytes', type=int, default=MIN_SOURCE_LIMIT, help='16 MiB default; at most 1 GiB')
    parser.add_argument('--timeout-seconds', type=int, default=180, help='whole session deadline, 30–1800 seconds; cleanup gets up to 10 more')
    parser.add_argument('--worker', action='store_true', help=argparse.SUPPRESS)
    args = parser.parse_args(argv)
    if not args.output_dir.is_absolute():
        parser.error('--output-dir must be absolute')
    if not MIN_SOURCE_LIMIT <= args.max_source_bytes <= MAX_SOURCE_LIMIT:
        parser.error('--max-source-bytes must be between 16777216 and 1073741824')
    if not 30 <= args.timeout_seconds <= 1800:
        parser.error('--timeout-seconds must be between 30 and 1800')
    if args.tag is None:
        args.tag = 0 if args.source else 4
    if not 0 <= args.tag < 1_000_000:
        parser.error('--tag must be between 0 and 999999 (desktop index budget)')
    if args.field_name is not None and (not args.field_name or len(args.field_name) > 256 or not args.field_name.isprintable()):
        parser.error('--field-name must be 1 through 256 printable characters')
    if not 0 <= args.continue_context <= 4:
        parser.error('--continue-context must be between 0 and 4')
    if args.revisit_tag is not None and (not args.continue_context or not 0 <= args.revisit_tag < args.tag):
        parser.error('--revisit-tag requires replay and an earlier nonnegative Tag')
    if not 1 <= args.avc_search_pages <= 96 or (args.avc_search_pages != 1 and not args.avc_search):
        parser.error('--avc-search-pages requires search and must be 1 through 96')
    if args.avc_search_long and (not args.avc_search or args.avc_search_pages <= 64):
        parser.error('--avc-search-long requires more than 64 search pages')
    if bool(args.avc_search_gate) != bool(args.avc_search_action) or (args.avc_search_gate and not args.avc_search):
        parser.error('controlled search requires gate directory, action and search')
    if args.avc_search_gate:
        from avc_search_gate import validate_build
        validate_build(args.avc_search_gate, args.binary)
        if not args.worker and (args.avc_search_gate / 'entered.json').exists():
            parser.error('controlled gate has already run; prepare a fresh build directory')
    if args.mp3_summary_link and not args.mp3_summary:
        parser.error('--mp3-summary-link requires --mp3-summary')
    if not 0 <= args.mp3_page_steps <= 64 or (args.mp3_page_steps and (args.mp3_browse or args.avc_search)):
        parser.error('--mp3-page-steps must be 0 through 64 and cannot combine with browse or AVC search')
    if args.mp3_jump_frame is not None and (not 0 <= args.mp3_jump_frame <= 4294967295 or args.mp3_browse or args.avc_search or args.mp3_page_steps or args.mp3_summary_link or args.mp3_frame is not None):
        parser.error('--mp3-jump-frame must fit uint32 and cannot combine with another frame-selection flow')
    if args.mp3_frame_walk and (args.mp3_jump_frame is None or len(args.mp3_frame_walk) > 8):
        parser.error('--mp3-frame-walk requires --mp3-jump-frame and permits at most 8 explicit actions')
    if args.mp3_frame is not None and (not (args.mp3_history or args.mp3_page_steps or args.mp3_summary_link) or not 0 <= args.mp3_frame <= 4294967295):
        parser.error('--mp3-frame requires history, pages or summary link and must fit uint32')
    if args.avc_search and args.mp3_history:
        parser.error('AVC search and MP3 history are separate native rounds')
    filters = ('all', 'parameters', 'bitrate', 'stops') if args.codec_search == 'mp3' else ('all', 'bufferingPeriod', 'pictureTiming', 'unknown')
    if args.codec_search:
        if args.avc_search or args.mp3_browse or args.mp3_summary or args.mp3_history or args.mp3_jump_frame is not None or args.mp3_page_steps:
            parser.error('codec search requires a separate native flow')
        if not args.codec_search_filter:
            args.codec_search_filter = ['all']
        if len(args.codec_search_filter) > 4 or len(set(args.codec_search_filter)) != len(args.codec_search_filter) or any(f not in filters for f in args.codec_search_filter):
            parser.error('codec filters must be distinct and valid for the selected kind')
    elif args.codec_search_filter or args.codec_search_target or args.codec_search_frame or args.codec_search_source or args.codec_search_restart or args.codec_search_pages != 4:
        parser.error('codec search options require --codec-search')
    if not 1 <= args.codec_search_pages <= 16:
        parser.error('codec search pages must be 1 through 16')
    if args.codec_search_target and not re.fullmatch(r'(0|[1-9][0-9]{0,5}):(-|0|[1-9][0-9]{0,9}):(-|0|[1-9][0-9]{0,9}):(-|0|[1-9][0-9]{0,9})', args.codec_search_target):
        parser.error('invalid codec search row identity')
    if args.codec_search_target and any(part != '-' and int(part) > 4294967295 for part in args.codec_search_target.split(':')[1:]):
        parser.error('codec search row indices must fit uint32')
    if (args.codec_search_source or args.codec_search_frame) and not args.codec_search_target or args.codec_search_frame and args.codec_search != 'mp3':
        parser.error('codec source/frame actions require an exact compatible target')
    if len(set(args.codec_search_source)) != len(args.codec_search_source):
        parser.error('codec source actions must be distinct')
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
    # Keep its other Tags but provide the complete IDR header for its PPS/SPS:
    # prefix (10 bits), IDR flags 00, se(0) QP delta, ue(1) deblocking disabled.
    # The remaining byte is RBSP trailing bits; macroblocks are not decoded.
    payload = bytes.fromhex('17010000000000000465b84a80')
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


def failure_ui_tree(desktop, pid, publish, node_budget=2048):
    """Read-only forensic tree; missing nodes/errors never imply an empty UI."""
    report = {'complete': False, 'traversalFinished': False, 'nodes': [], 'errors': [], 'errorCount': 0,
              'nodeBudget': node_budget, 'budgetLimited': False, 'textTruncated': False}
    references = 0

    def read(path, operation, call, default=None):
        try:
            return call()
        except Exception as error:
            report['errorCount'] += 1
            if len(report['errors']) < 32:
                report['errors'].append({'path': path, 'operation': operation, 'error': repr(error)[:512]})
            return default

    def children(node, path):
        nonlocal references
        count = read(path, 'childCount', lambda: node.childCount, 0)
        if not isinstance(count, int) or count < 0:
            read(path, 'validChildCount', lambda: require(False, 'invalid accessible child count'))
            return []
        available = node_budget - references
        if count > available:
            report['budgetLimited'] = True
        result = []
        for index in range(min(count, available)):
            references += 1
            child_path = path + [index]
            child = read(child_path, 'getChildAtIndex', lambda: node.getChildAtIndex(index))
            if child is None:
                read(child_path, 'childPresent', lambda: require(False, 'missing accessible child'))
            else:
                result.append((child, child_path))
        after = read(path, 'childCountAfter', lambda: node.childCount)
        read(path, 'stableChildCount', lambda: require(count == after, 'children changed while reading'))
        return result

    stack = [(node, path) for node, path in children(desktop, [])
             if read(path, 'processId', node.get_process_id) == pid]
    report['ownedRoots'] = len(stack)
    publish(report)
    while stack and len(report['nodes']) < node_budget:
        node, path = stack.pop()
        row = {'path': path, 'depth': len(path) - 1,
               'role': read(path, 'role', node.getRoleName),
               'name': read(path, 'name', lambda: node.name),
               'description': read(path, 'description', lambda: node.description), 'text': ''}

        def text():
            try:
                interface = node.queryText()
            except NotImplementedError:
                return ''  # The node has no Text interface, not a failed read.
            count = interface.characterCount
            report['textTruncated'] |= count > 1200
            return interface.getText(0, min(count, 1200))

        row['text'] = read(path, 'text', text)
        report['nodes'].append(row)
        stack.extend(reversed(children(node, path)))
        if len(report['nodes']) % 32 == 0:
            publish(report)
    report['budgetLimited'] |= bool(stack)
    report['traversalFinished'] = True
    report['complete'] = bool(report['ownedRoots']) and not (report['errorCount'] or report['budgetLimited'] or report['textTruncated'])
    publish(report)
    return report


def failure_probe(request_path):
    # This interpreter inherits only the private test bus/display. It never
    # invokes an action, changes settings, or sends a signal to the application.
    request = json.loads(Path(request_path).read_text())
    out, pid = Path(request_path).parent, request['pid']
    report = {'processSamples': [], 'ui': {'complete': False, 'nodes': [], 'errors': [], 'state': 'notStarted'}}

    def persist():
        save(out, 'failure-probe.tmp', report)
        (out / 'failure-probe.tmp').replace(out / 'failure-probe.json')

    known = {row['pid']: row for row in request['knownProcesses']}
    for index in range(2):
        sample = {'unixSeconds': time.time(), 'monotonicSeconds': time.monotonic()}
        try:
            sample['observedProcesses'] = observe(pid, known)
            sample['sourceFDs'] = source_fds(sample['observedProcesses'], Path(request['source']))
            sample['complete'] = True
        except Exception as error:
            sample.update(complete=False, error=repr(error)[:512])
        report['processSamples'].append(sample)
        persist()
        if index == 0:
            time.sleep(.2)
    try:
        require(alive(request['appIdentity']), 'original app identity no longer exists')
        import pyatspi

        def publish(ui):
            report['ui'] = {**ui, 'state': 'finished' if ui['complete'] else 'partial'}
            persist()

        failure_ui_tree(pyatspi.Registry.getDesktop(0), pid, publish)
    except Exception as error:
        report['ui'].update(complete=False, error=repr(error)[:512])
        persist()


def run_failure_probe(command, timeout=8):
    # A Python alarm cannot bound an AT-SPI C call. Isolate it and reap only
    # this child; its termination is separate from application cleanup records.
    child = subprocess.Popen(command, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    result = {'pid': child.pid, 'identity': process(child.pid), 'budgetSeconds': timeout,
              'timedOut': False, 'terminationSignal': None, 'reaped': False}
    try:
        result['exitCode'] = child.wait(timeout=timeout)
    except subprocess.TimeoutExpired:
        result.update(timedOut=True, terminationSignal='SIGKILL', terminationReason='failure observation deadline')
        child.kill()
        result['exitCode'] = child.wait(timeout=1)
    finally:
        if child.poll() is None:
            child.kill()
            child.wait(timeout=1)
        result['reaped'] = child.poll() is not None
    return result


def capture_failure_observation(out, app, source, known, close_stages):
    observation = {'startedUnixSeconds': time.time(), 'startedMonotonicSeconds': time.monotonic(),
                   'appPollBefore': app.poll(), 'closeStages': close_stages,
                   'actionReplayed': False, 'ui': {'complete': False}}
    try:
        save(out, 'failure-probe-request.json', {'pid': app.pid, 'source': str(source),
             'appIdentity': known[app.pid], 'knownProcesses': list(known.values())})
        command = [sys.executable, '-B', '-c',
                   'import runpy,sys; runpy.run_path(sys.argv[1])["failure_probe"](sys.argv[2])',
                   str(Path(__file__).resolve()), str(out / 'failure-probe-request.json')]
        observation['probe'] = run_failure_probe(command)
        if (out / 'failure-probe.json').exists():
            report = json.loads((out / 'failure-probe.json').read_text())
            ui = report['ui']
            save(out, 'failure-ui.json', ui.pop('nodes'))
            ui.update(file='failure-ui.json', sha256=digest(out / 'failure-ui.json'))
            if observation['probe']['timedOut']:
                ui.update(complete=False, interrupted=True)
            observation.update(report)
    except Exception as error:
        observation['observationError'] = repr(error)[:512]
    observation.update(appPollAfter=app.poll(), finishedUnixSeconds=time.time(),
                       elapsedSeconds=time.monotonic() - observation['startedMonotonicSeconds'])
    save(out, 'failure-observation.json', observation)
    return observation


def record_failure_observation(result, out, app, source, known):
    try:
        save(out, 'native-result.json', result)  # Preserve the primary exception first.
        result['failureObservation'] = capture_failure_observation(out, app, source, known, result.get('closeStages', []))
    except BaseException as error:
        result['failureObservationError'] = repr(error)[:512]


def accessible_action_once(action, name, stage=None):
    require(action.nActions == 1, 'action missing: ' + name)
    if stage is not None:
        stage.update(state='actionPending', actionAttempted=True, actionStartedUnixSeconds=time.time())
    accepted = action.doAction(0)
    if stage is not None:
        stage.update(state='actionReturned', actionReturned=True, actionAccepted=bool(accepted),
                     actionFinishedUnixSeconds=time.time())
    require(accepted, 'action failed: ' + name)


def choose_accessible_option(get_combo, index, label, expected_name=None):
    # A page update can invalidate any descendant proxy. Discard the entire
    # enumeration and acquire the combo again; skipping a child shifts indices.
    for attempt in range(5):
        try:
            pending, items, visited = [get_combo()], [], 0
            while pending:
                item = pending.pop()
                require(item is not None, 'stale accessible option')
                visited += 1
                require(visited <= 256, 'combo tree exceeded node budget')
                if item.getRoleName() == 'menu item':
                    items.append(item)
                count = item.childCount
                children = [item.getChildAtIndex(i) for i in reversed(range(count))]
                require(item.childCount == count and all(child is not None for child in children),
                        'combo options changed during enumeration')
                pending.extend(children)
            require(0 <= index < len(items), 'select option out of range: ' + label)
            if expected_name is not None:
                require(items[index].name == expected_name, 'select option identity differs: ' + label)
            action = items[index].queryAction()
            require(action.nActions == 1, 'select option action missing: ' + label)
        except Exception:
            if attempt == 4:
                raise
            time.sleep(0.1)
            continue
        # Do not retry an action that might already have changed the selection.
        require(action.doAction(0), 'select option action failed: ' + label)
        return


def validate_export(snapshot, tag, generated):
    require(snapshot['session']['scan']['complete'], 'smoke requires a complete container index')
    selected = snapshot['selectedTag']
    require(selected is not None and int(selected['id']) == tag, 'exported Tag identity differs')
    require(snapshot['detail'] is not None and snapshot['hex'] is not None, 'detail/Hex missing from export')
    require(snapshot['hex']['window']['offset'] == selected['offset'], 'Hex is not positioned at the selected Tag')
    if generated:
        require(snapshot['detail']['detail']['complete'], 'generated fixture detail is incomplete')


def check_mp3_navigation_stage(snapshot, target, previous=None):
    detail = snapshot['detail']
    fields = {f['name']: f['value'] for f in detail['detail']['fields']}
    require(detail['state'] == 'ready' and fields['requestedFrameIndex'] == str(target) and
            fields['requestedPageStart'] == str(target // 64 * 64), 'direct MP3 navigation identity differs')
    require(fields['replayPageBudget'] == '16' and 1 <= int(fields['replayPages']) <= 16 and
            int(fields['checkpointCount']) <= 64, 'direct MP3 navigation exceeded replay/checkpoint bounds')
    state, progress = fields['pageState'], int(fields['nextFrameIndex'])
    require(state in ('preparing', 'ready', 'stopped'), 'unexpected MP3 navigation state')
    if previous:
        require(int(detail['requestId']) > int(previous['requestId']) and progress > previous['nextFrameIndex'],
                'direct MP3 continuation reused a request or did not advance')
    if state == 'preparing':
        require(fields['canContinue'] == 'true' and progress <= target // 64 * 64 and
                fields['replayPages'] == '16' and 'Frame inspection' not in fields,
                'preparing MP3 navigation exposed a wrong page or lacked progress')
    elif state == 'ready':
        require(fields['pageStartIndex'] == str(target // 64 * 64) and fields['selectedFrameIndex'] == str(target),
                'ready MP3 navigation selected a different frame')
    return {'requestId': detail['requestId'], 'targetFrame': target, 'state': state,
            'pageStart': int(fields['pageStartIndex']), 'nextFrameIndex': progress, 'replayPages': int(fields['replayPages'])}


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

    def rows(root=None):
        if root is None:
            desktop = pyatspi.Registry.getDesktop(0)
            stack = [desktop.getChildAtIndex(i) for i in range(desktop.childCount)]
            stack = [(node, 0) for node in stack if node.get_process_id() == app.pid]
        else:
            stack = [(root, 0)]
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

    search_root = [None]

    def search_rows():
        if search_root[0] is None:
            search_root[0] = find('landmark', 'H.264 批量证据检索')
        return rows(search_root[0])

    def dump(name, search_only=False):
        save(out, name, [{'depth': depth, 'role': role, 'name': name_, 'description': node.description, 'text': node_text(node)}
                         for node, depth, role, name_ in (search_rows() if search_only else rows())])

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
        scan = search_rows if args.avc_search_long and role == 'push button' and name in ('继续检索 H.264', '上一批 H.264') else rows
        matches = wait(lambda: [node for node, d, r, n in scan()
                                if r == role and n == name and (depth is None or d == depth)], f'{role}: {name}')
        require(len(matches) == 1, f'ambiguous accessible target {role}: {name}')
        return matches[0]

    def click(name, role='push button'):
        stage = None
        if name == '关闭文件':
            stage = {'state': 'locating', 'startedUnixSeconds': time.time(),
                     'actionAttempted': False, 'actionReturned': False}
            result.setdefault('closeStages', []).append(stage)
        node = find(role, name)
        require(node.getState().contains(pyatspi.STATE_ENABLED), 'disabled target: ' + name)
        accessible_action_once(node.queryAction(), name, stage)

    def wait_text(fragment, seconds=35):
        return wait(lambda: any(fragment in name or fragment in node_text(node)
                                for node, _, _, name in rows()), fragment, seconds)

    def wait_search_text(fragment):
        return wait(lambda: any(fragment in name or fragment in node_text(node) for node, _, _, name in
                               (search_rows() if args.avc_search_long else rows())), fragment)

    def choose(label, index, expected_name=None):
        # WebKitGTK exposes HTML options as menu items rather than Selection.
        # Invoke the selected item's native action once; arrow-key changes can
        # disable the selector while an intermediate detail request is running.
        choose_accessible_option(lambda: find('combo box', label), index, label, expected_name)

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

    def mp3_page_start():
        starts = set()
        for node, _, _, name in rows():
            for text in (name, node_text(node)):
                starts.update(int(m[1]) for m in re.finditer(r'MP3 帧页 (\d+) · 本页 ', text))
        require(len(starts) == 1, 'MP3 ready page identity missing or ambiguous')
        return starts.pop()

    def move_mp3_page(target):
        current = mp3_page_start()
        require(target % 64 == 0 and abs(target - current) // 64 <= 64, 'MP3 page navigation budget exceeded')
        while current != target:
            step = 64 if target > current else -64
            click('下一 MP3 帧页' if step > 0 else '上一 MP3 帧页')
            current += step
            wait_text(f'MP3 帧页 {current} · 本页 ')

    def select_mp3_frame(index):
        start = mp3_page_start()
        require(start <= index < start + 64, 'selected MP3 frame is outside the ready page')
        choose('MP3 帧选择', index - start + 1, f'帧 {index}')
        wait_text(f'当前展开 MP3 帧 {index} ·')

    def jump_mp3_frame(index):
        entry = find('entry', 'MP3 帧号')
        require(entry.queryComponent().grabFocus(), 'MP3 frame input focus failed')
        combo('a')
        for char in str(index):
            key(char, True)
            key(char, False)
        screen.sync()
        wait(lambda: node_text(find('entry', 'MP3 帧号')) == str(index), 'MP3 frame input readback')
        click('跳转 MP3 帧')
        result['mp3DirectNavigation'] = []
        previous = None
        for step in range(4):
            wait(lambda: any((f'正在定位 MP3 帧 {index}，' in name or f'当前展开 MP3 帧 {index} ·' in name or
                              f'MP3 帧 {index} 超出' in name or f'MP3 帧 {index} 定位停止' in name) or
                             (f'正在定位 MP3 帧 {index}，' in node_text(node) or f'当前展开 MP3 帧 {index} ·' in node_text(node) or
                              f'MP3 帧 {index} 超出' in node_text(node) or f'MP3 帧 {index} 定位停止' in node_text(node))
                             for node, _, _, name in rows()), 'direct MP3 frame navigation')
            filename = f'mp3-direct-{step}.json'
            snapshot, checked = export_snapshot(filename, args.tag)
            stage = check_mp3_navigation_stage(snapshot, index, previous)
            ui_name = f'mp3-direct-{step}-ui.json'
            dump(ui_name)
            stage.update(file=filename, snapshotCheck=checked, uiFile=ui_name, uiSHA256=digest(out / ui_name))
            result['mp3DirectNavigation'].append(stage)
            if stage['state'] != 'preparing':
                require(stage['state'] == 'ready', 'direct MP3 navigation stopped before target')
                return
            require(step < 3, 'direct MP3 navigation exceeded four explicit requests')
            previous = stage
            click('继续定位 MP3 帧页')
            # The next export must carry a new request identity and progress;
            # a still-visible previous status cannot satisfy that assertion.
            wait_text(f'正在定位 MP3 帧 {index}，已检查到帧 {stage["nextFrameIndex"] + 1024} 之前。' if
                      stage['nextFrameIndex'] + 1024 <= index // 64 * 64 else f'当前展开 MP3 帧 {index} ·')

    def export_snapshot(name, tag=None):
        if tag is not None:
            click(f'查看 Tag {tag} 原始字节')
            wait(lambda: any(name_.startswith('查看 byte ') for _, _, _, name_ in rows()), 'Hex bytes')
        path = out / name
        click('导出诊断 JSON')
        find('push button', 'Save')
        picker_path(path, 6)
        click('Save')
        wait_text('已保存 ' + name)
        snapshot = json.loads(path.read_text())
        if tag is not None:
            validate_export(snapshot, tag, record['origin'] == 'generated')
        checked = check_snapshot(path, source, args.max_source_bytes)
        require(checked['sourceSHA256'] == before_hash, 'source changed during native flow')
        return snapshot, checked

    def open_source():
        search_root[0] = None
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

    def start_search():
        labels = {'mismatches': 0, 'unknown': 1, 'all': 2}
        if args.avc_search == 'mismatches':
            click('检索 H.264 证据')
        else:
            choose('H.264 证据筛选', labels[args.avc_search])

    def codec_search():
        filters = ('all', 'parameters', 'bitrate', 'stops') if args.codec_search == 'mp3' else ('all', 'bufferingPeriod', 'pictureTiming', 'unknown')
        filter_labels = ('全部变化与停止', '参数变化', '码率变化', '无法继续检查') if args.codec_search == 'mp3' else ('全部 SEI 消息', '缓冲周期', '图像时序', '未知或不完整证据')
        previous, request = None, None
        result['codecSearchPages'] = []

        def page(filename, filter_name, index, cursor):
            status = wait(lambda: next((node_text(node) for node, _, _, _ in rows()
                          if node_text(node).startswith('编解码器检索完成：' + args.codec_search + ' · ') and
                          (previous is None or node_text(node) != previous)), None), 'codec search ready page')
            wait(lambda: find('push button', '从头检索编解码器').getState().contains(pyatspi.STATE_ENABLED), 'codec page no longer running')
            ui_name = filename.removesuffix('.json') + '-ui.json'
            dump(ui_name)
            snapshot, checked = export_snapshot(filename)
            response = snapshot.get('codecSearchPage')
            require(snapshot['schemaVersion'] == 4 and response is not None and response['state'] == 'ready' and response['error'] is None, 'codec ready page missing from v4 export')
            current = response['page']
            require(current['kind'] == args.codec_search and current['filter'] == filter_name and current['cursor'] == cursor, 'codec request kind/filter/cursor differs')
            require(request is None or int(response['requestId']) > int(request), 'codec search reused an old request')
            stage = {'file': filename, 'snapshotCheck': checked, 'uiFile': ui_name, 'uiSHA256': digest(out / ui_name),
                     'filter': filter_name, 'pageIndex': index, 'status': status, 'requestId': response['requestId']}
            return stage, current

        if args.codec_search == 'sei':
            choose('编解码器检索类型', 1, 'H.264 SEI 消息')
            wait_text('编解码器检索完成：sei · ')
        for filter_index, filter_name in enumerate(args.codec_search_filter):
            previous = None
            if filter_index == 0 and filter_name == 'all':
                if args.codec_search == 'mp3':
                    click('检索编解码器证据')
            else:
                choose('编解码器检索筛选', filters.index(filter_name), filter_labels[filters.index(filter_name)])
            cursor = '0'
            if args.codec_search_restart and filter_index == 0:
                stage, _ = page('codec-restart-initial.json', filter_name, 0, cursor)
                result['codecSearchRestart'] = stage
                request = stage['requestId']
                click('从头检索编解码器')
            for index in range(args.codec_search_pages):
                stage, current = page(f'codec-{filter_name}-{index}.json', filter_name, index, cursor)
                result['codecSearchPages'].append(stage)
                request, previous = stage['requestId'], stage['status']
                if current['exhausted']:
                    break
                require(index + 1 < args.codec_search_pages, 'codec fixture did not exhaust within explicit page budget')
                cursor = current['nextCursor']
                require(cursor != '0', 'nonexhausted codec page lost continuation token')
                click('继续检索编解码器')
        if not args.codec_search_target:
            return
        identity = lambda row: ':'.join(str(row[key]) if row[key] is not None else '-' for key in ('tagId', 'frameIndex', 'nalIndex', 'messageIndex'))
        targets = [row for row in current['rows'] if identity(row) == args.codec_search_target]
        require(len(targets) == 1, 'exact codec target missing from final retained page')
        target = targets[0]
        if args.codec_search_frame:
            require(target['complete'] and target['status'] == 'complete' and target['frameIndex'] is not None, 'codec row cannot open frame details')
            click(f'从编解码器检索跳转 {args.codec_search_target} 帧')
            result['codecSearchFrameStages'], prior = [], None
            for step in range(4):
                frame = target['frameIndex']
                wait(lambda: any((f'当前展开 MP3 帧 {frame} ·' in node_text(node) or
                                  f'正在定位 MP3 帧 {frame}，已检查到帧 ' in node_text(node)) for node, _, _, _ in rows()), 'codec row frame target')
                filename = f'codec-frame-{step}.json'
                snapshot, checked = export_snapshot(filename, int(target['tagId']))
                navigation = check_mp3_navigation_stage(snapshot, frame, prior)
                stage = {**navigation, 'file': filename, 'snapshotCheck': checked}
                result['codecSearchFrameStages'].append(stage)
                if navigation['state'] == 'ready':
                    break
                require(navigation['state'] == 'preparing' and step < 3, 'codec frame target stopped or exceeded continuation budget')
                prior = navigation
                click('继续定位 MP3 帧页')
                wait_text(f'正在定位 MP3 帧 {frame}，已检查到帧 {prior["nextFrameIndex"] + 1024} 之前。' if
                          prior['nextFrameIndex'] + 1024 <= frame // 64 * 64 else f'当前展开 MP3 帧 {frame} ·')
        result['codecSearchSources'] = []
        for destination in args.codec_search_source:
            label, key_name = {'hex': ('证据', 'span'), 'sps': ('SPS', 'spsSpan'), 'evidence': ('选择依据', 'evidenceSpan')}[destination]
            source_span = target[key_name]
            require(source_span is not None, 'codec target source is unavailable')
            click(f'定位编解码器 {args.codec_search_target} {label}')
            source_label = f'编解码器证据 {args.codec_search_target} {destination}'
            wait_text(f'{source_label} · {source_span["byteOffset"]} : {source_span["bitOffset"]} · {source_span["bitLength"]} bit')
            wait(lambda: any(n == f'查看 byte {source_span["byteOffset"]} 的位' for _, _, _, n in rows()), 'codec source Hex bytes')
            filename, ui = f'codec-source-{destination}.json', f'codec-source-{destination}-ui.json'
            dump(ui)
            _, checked = export_snapshot(filename)
            result['codecSearchSources'].append({'destination': destination, 'target': args.codec_search_target,
                   'span': source_span, 'label': source_label, 'file': filename, 'snapshotCheck': checked,
                   'uiFile': ui, 'uiSHA256': digest(out / ui)})

    result = {'passed': False, 'pid': app.pid}
    try:
        open_source()
        if args.media_curves or args.mp3_summary:
            click('分析媒体')
            wait(lambda: find('push button', '查看媒体曲线').getState().contains(pyatspi.STATE_ENABLED), 'media summary ready', 90)
        if args.mp3_summary:
            click('MP3 帧头 (256)', 'page tab')
            result['mp3SummaryPages'] = []
            for index in range(3):
                start = index * 100
                expected_buttons = [f'定位媒体事件 {i} 原始字节' for i in range(start, min(start + 100, 256))]
                wait(lambda: [name for _, _, role, name in rows() if role == 'push button' and
                              re.fullmatch(r'定位媒体事件 \d+ 原始字节', name)] == expected_buttons, 'MP3 summary event page ready')
                ui, snapshot_name = f'mp3-summary-page-{index}-ui.json', f'mp3-summary-page-{index}.json'
                dump(ui)
                snapshot, checked = export_snapshot(snapshot_name)
                result['mp3SummaryPages'].append({'uiFile': ui, 'uiSHA256': digest(out / ui), 'start': start, 'rowCount': len(expected_buttons),
                                                  'file': snapshot_name, 'snapshotCheck': checked})
                if not find('push button', '媒体事件下一页').getState().contains(pyatspi.STATE_ENABLED):
                    break
                require(index < 2, 'summary exceeded the expected 256-event retained prefix')
                click('媒体事件下一页')
            require(len(result['mp3SummaryPages']) == 3, 'summary did not expose all three retained event pages')
            last = snapshot['summaryPage']['page']['events'][-1]
            require(last['title'] == 'mp3Frame', 'summary did not expose full-frame observations')
            click(f'定位媒体事件 {last["id"]} 原始字节')
            wait_text(f'媒体概览 · mp3Frame · {last["offset"]} : 0 · 仅标记位置，无已知范围。')
            wait(lambda: any(n == f'查看 byte {last["offset"]} 的位' for _, _, _, n in rows()), 'summary late-frame Hex source')
            dump('mp3-summary-source-ui.json')
            _, checked = export_snapshot('mp3-summary-source.json')
            result['mp3SummarySource'] = {'uiFile': 'mp3-summary-source-ui.json',
                                           'uiSHA256': digest(out / 'mp3-summary-source-ui.json'),
                                           'file': 'mp3-summary-source.json', 'snapshotCheck': checked,
                                           'eventId': last['id'], 'offset': last['offset']}
            if args.mp3_summary_link:
                values = {item['name']: item['value'] for item in last['values'] if item['section'] == ''}
                require(values.get('frameStatus') == 'complete' and int(last['tagId']) == args.tag, 'summary link requires a complete event in the requested Tag')
                frame = int(values['frameIndex'])
                click(f'从媒体事件跳转 Tag {last["tagId"]} 帧 {frame}')
                wait_text(f'当前展开 MP3 帧 {frame} ·')
                _, checked = export_snapshot('mp3-summary-detail.json', args.tag)
                result['mp3SummaryDetail'] = {'file': 'mp3-summary-detail.json', 'snapshotCheck': checked, 'frameIndex': frame, 'pageStart': frame // 64 * 64}
        if args.media_curves:
            click('查看媒体曲线')
            wait_text(' · 本页 ')
            metric = {'audio': '音频码率', 'video': '视频码率', 'mp3': 'MP3 码率', 'gopAverage': 'GOP 平均时长', 'gopMax': 'GOP 最长时长'}[args.media_curves]
            click(metric, 'toggle button')
        if args.avc_search:
            start_search()
            if args.avc_search_gate:
                from avc_search_gate import receipt, release
                gate = args.avc_search_gate
                require(source == Path(json.loads((gate / 'build-plan.json').read_text())['source']), 'controlled gate requires its copied source')
                wait(lambda: (gate / 'entered.json').is_file(), 'controlled actual source read entered')
                entered = receipt(gate, 'entered.json', app.pid)
                require(entered['bytes'] > 0 and entered['offset'] >= 0, 'gate lacks an actual read range')
                wait_text('正在从 Tag 0 检查本批证据…')
                dump('avc-gate-pending.json')
                gate_record = {'entered': entered, 'pendingUI': 'avc-gate-pending.json',
                               'pendingUISHA256': digest(out / 'avc-gate-pending.json'), 'action': args.avc_search_action}
                gate_record['fdsBeforeAction'] = source_fds(observe(app.pid, known), source)
                require(gate_record['fdsBeforeAction'], 'gate did not retain source ownership')
                click('取消 H.264 检索' if args.avc_search_action == 'cancel' else '关闭文件')
                wait(lambda: (gate / 'context-canceled.json').is_file(), 'native command canceled the active worker')
                canceled = receipt(gate, 'context-canceled.json', app.pid)
                require(canceled['contextError'] == 'context canceled', 'deadline expiration is not native cancellation')
                wait_text('正在确认 H.264 检索取消…' if args.avc_search_action == 'cancel' else '正在释放当前文件；底层 I/O 结束后才能打开下一个文件。')
                dump('avc-gate-command.json')
                gate_record.update(canceled=canceled, commandUI='avc-gate-command.json',
                                   commandUISHA256=digest(out / 'avc-gate-command.json'))
                if args.avc_search_action == 'close':
                    gate_record['openDisabledBeforeRelease'] = not find('push button', '打开 FLV 文件').getState().contains(pyatspi.STATE_ENABLED)
                    require(gate_record['openDisabledBeforeRelease'], 'closed source admitted another open while read was blocked')
                gate_record['fdsBeforeRelease'] = source_fds(observe(app.pid, known), source)
                require(gate_record['fdsBeforeRelease'], 'source was closed while its read was blocked')
                require(not (gate / 'released.json').exists(), 'read finished before native action was observed')
                release(gate, app.pid)
                wait(lambda: (gate / 'released.json').is_file(), 'controlled read released')
                gate_record['released'] = receipt(gate, 'released.json', app.pid)
                require(gate_record['released']['contextError'] == 'context canceled' and not (gate / 'timed-out.json').exists(), 'controlled release was a deadline or safety timeout')
                if args.avc_search_action == 'cancel':
                    wait(lambda: find('push button', '重试当前 H.264 批次').getState().contains(pyatspi.STATE_ENABLED) and
                         not find('push button', '取消 H.264 检索').getState().contains(pyatspi.STATE_ENABLED), 'canceled search exposes retry and disables cancel')
                    require(not any('正在确认 H.264 检索取消' in node_text(node) or 'H.264 检索完成：' in node_text(node) for node, _, _, _ in rows()), 'canceled search retained running/ready UI')
                    dump('avc-gate-canceled.json')
                    gate_record['terminalUI'] = 'avc-gate-canceled.json'
                    gate_record['terminalUISHA256'] = digest(out / 'avc-gate-canceled.json')
                    snapshot, checked = export_snapshot('avc-gate-canceled-snapshot.json')
                    require(snapshot['avcSearchPage'] is None and any(item['section'] == 'avcSearchPage' for item in snapshot['omitted']), 'canceled search leaked a ready page into snapshot')
                    gate_record['canceledSnapshot'] = checked
                    click('重试当前 H.264 批次')
                else:
                    wait_text('从一个文件开始')
                    wait(lambda: not source_fds(observe(app.pid, known), source), 'closed gated source FD release')
                    gate_record['fdsAfterGatedClose'] = source_fds(observe(app.pid, known), source)
                    dump('avc-gate-closed.json')
                    gate_record['terminalUI'] = 'avc-gate-closed.json'
                    gate_record['terminalUISHA256'] = digest(out / 'avc-gate-closed.json')
                    open_source()
                    start_search()
                result['avcSearchGate'] = gate_record
            result['avcSearchPages'] = []
            previous_status = None
            for index in range(args.avc_search_pages):
                status = wait(lambda: next((node_text(node) for node, _, _, _ in (search_rows() if args.avc_search_long else rows())
                              if node_text(node).startswith('H.264 检索完成：') and
                              node_text(node) != previous_status), None), 'AVC search batch ready')
                name = f'avc-search-{index}.json'
                dump(name, search_only=args.avc_search_long)
                stage = {'uiFile': name, 'uiSHA256': digest(out / name), 'status': status}
                if not args.avc_search_long or '已到当前索引末尾。' in status:
                    snapshot_name = f'avc-page-{index}.json'
                    _, checked = export_snapshot(snapshot_name)
                    stage.update(file=snapshot_name, snapshotCheck=checked)
                result['avcSearchPages'].append(stage)
                next_button = find('push button', '继续检索 H.264')
                if not next_button.getState().contains(pyatspi.STATE_ENABLED) or index + 1 == args.avc_search_pages:
                    break
                previous_status = status
                click('继续检索 H.264')
            if args.avc_search_long:
                pages = result['avcSearchPages']
                require(len(pages) > 64 and '已到当前索引末尾。' in pages[-1]['status'], 'long search must reach the end beyond 64 pages')
                result['avcSearchHistory'] = []
                for back in range(1, 65):
                    click('上一批 H.264')
                    previous = pages[-back - 1]['status']
                    wait_search_text(previous)
                    name = f'avc-history-{back}.json'
                    dump(name, search_only=True)
                    result['avcSearchHistory'].append({'status': previous, 'uiFile': name, 'uiSHA256': digest(out / name)})
                require(not find('push button', '上一批 H.264').getState().contains(pyatspi.STATE_ENABLED), 'search retained more than 64 history entries')
                result['avcSearchHistoryBounded'] = True
                # Return to the final page by explicit forward work, observing every cursor.
                for previous in pages[-64:]:
                    click('继续检索 H.264')
                    wait_search_text(previous['status'])
            if len(result['avcSearchPages']) > 1:
                click('上一批 H.264')
                previous = result['avcSearchPages'][-2]['status']
                wait_search_text(previous)
                dump('avc-search-previous.json', search_only=args.avc_search_long)
                result['avcSearchPrevious'] = {'uiFile': 'avc-search-previous.json', 'uiSHA256': digest(out / 'avc-search-previous.json'), 'status': previous}
                click('继续检索 H.264')
                wait_search_text(result['avcSearchPages'][-1]['status'])
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
        if not args.mp3_summary_link:
            select_tag(args.tag)
        if args.codec_search:
            codec_search()
            if not args.codec_search_frame:
                select_tag(args.tag)
        if args.mp3_jump_frame is not None:
            jump_mp3_frame(args.mp3_jump_frame)
        selected_target = args.mp3_jump_frame
        if args.mp3_frame_walk:
            result['mp3AdjacentFrames'] = []
            for step, direction in enumerate(args.mp3_frame_walk):
                selected_target += 1 if direction == 'next' else -1
                require(0 <= selected_target <= 4294967295, 'adjacent MP3 target outside uint32')
                click('下一 MP3 帧' if direction == 'next' else '上一 MP3 帧')
                wait_text(f'当前展开 MP3 帧 {selected_target} ·')
                filename = f'mp3-adjacent-{step}.json'
                snapshot, checked = export_snapshot(filename, args.tag)
                stage = check_mp3_navigation_stage(snapshot, selected_target)
                require(stage['state'] == 'ready', 'adjacent MP3 frame did not become ready')
                ui_name = f'mp3-adjacent-{step}-ui.json'
                dump(ui_name)
                stage.update(direction=direction, file=filename, snapshotCheck=checked,
                             uiFile=ui_name, uiSHA256=digest(out / ui_name))
                result['mp3AdjacentFrames'].append(stage)
        if args.mp3_page_steps:
            start = mp3_page_start()
            result['mp3FramePages'] = []
            for index in range(args.mp3_page_steps):
                target = start + 64 * (index + 1)
                move_mp3_page(target)
                name = f'mp3-frame-page-{target}-ui.json'
                dump(name)
                result['mp3FramePages'].append({'start': target, 'uiFile': name, 'uiSHA256': digest(out / name)})
        if args.mp3_frame is not None:
            select_mp3_frame(args.mp3_frame)
            selected_target = args.mp3_frame
            if args.mp3_page_steps or args.mp3_summary_link:
                _, checked = export_snapshot('mp3-page-selected.json', args.tag)
                result['mp3PageSelected'] = {'file': 'mp3-page-selected.json', 'snapshotCheck': checked}
        if args.mp3_history:
            target_page = mp3_page_start()
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
                if int(first['tagId']) != args.tag:
                    select_tag(args.tag)
                move_mp3_page(target_page)
                select_mp3_frame(selected_target if selected_target is not None else target_page)
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
        if args.field_name:
            initial, checked = export_snapshot('field-initial.json', args.tag)
            fields = [field for field in initial['detail']['detail']['fields'] if field['name'] == args.field_name and field['span'] is not None]
            require(len(fields) == 1, 'detail source field is missing or ambiguous: ' + args.field_name)
            span = fields[0]['span']
            click('定位字段 ' + args.field_name)
            wait_text(f'{args.field_name} · {span["byteOffset"]} : {span["bitOffset"]} · {span["bitLength"]} bit')
            wait(lambda: any(n == f'查看 byte {span["byteOffset"]} 的位' for _, _, _, n in rows()), 'detail field Hex source')
            dump('field-source-ui.json')
            _, source_check = export_snapshot('field-source.json')
            result['fieldSource'] = {'fieldName': args.field_name, 'span': span, 'initialFile': 'field-initial.json',
                                      'initialSnapshotCheck': checked, 'file': 'field-source.json', 'snapshotCheck': source_check,
                                      'uiFile': 'field-source-ui.json', 'uiSHA256': digest(out / 'field-source-ui.json')}
        _, result['snapshotCheck'] = export_snapshot('snapshot.json', args.tag)
        dump('saved-ui.json')
        result['sourceFDsBeforeClose'] = source_fds(observe(app.pid, known), source)
        require(result['sourceFDsBeforeClose'], 'no owned process holds the open source')
        click('关闭文件')
        result['closeStages'][-1]['state'] = 'waitingForEmptyUI'
        wait_text('从一个文件开始')
        result['closeStages'][-1]['state'] = 'waitingForSourceFDRelease'
        wait(lambda: not source_fds(observe(app.pid, known), source), 'source FD release')
        result['sourceFDsAfterClose'] = source_fds(observe(app.pid, known), source)
        result['closeStages'][-1].update(state='sourceReleased', sourceReleasedUnixSeconds=time.time())
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
        record_failure_observation(result, out, app, source, known)
        raise
    finally:
        try:
            save(out, 'native-result.json', result)
            screen.close()
        except Exception:
            if result['passed']:
                raise
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
    if args.mp3_summary:
        command.append('--mp3-summary')
    if args.mp3_summary_link:
        command.append('--mp3-summary-link')
    if args.mp3_page_steps:
        command.extend(['--mp3-page-steps', str(args.mp3_page_steps)])
    if args.mp3_jump_frame is not None:
        command.extend(['--mp3-jump-frame', str(args.mp3_jump_frame)])
    if args.codec_search:
        command.extend(['--codec-search', args.codec_search, '--codec-search-pages', str(args.codec_search_pages)])
        for item in args.codec_search_filter:
            command.extend(['--codec-search-filter', item])
        if args.codec_search_target:
            command.extend(['--codec-search-target', args.codec_search_target])
        if args.codec_search_frame:
            command.append('--codec-search-frame')
        if args.codec_search_restart:
            command.append('--codec-search-restart')
        for item in args.codec_search_source:
            command.extend(['--codec-search-source', item])
    for direction in args.mp3_frame_walk:
        command.extend(['--mp3-frame-walk', direction])
    if args.field_name:
        command.extend(['--field-name', args.field_name])
    if args.avc_evidence:
        command.append('--avc-evidence')
    if args.avc_search:
        command.extend(['--avc-search', args.avc_search, '--avc-search-pages', str(args.avc_search_pages)])
    if args.avc_search_long:
        command.append('--avc-search-long')
    if args.avc_search_gate:
        command.extend(['--avc-search-gate', str(args.avc_search_gate), '--avc-search-action', args.avc_search_action])
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
              'mp3Browse': args.mp3_browse, 'mp3Summary': args.mp3_summary, 'mp3SummaryLink': args.mp3_summary_link, 'mp3PageSteps': args.mp3_page_steps,
              'mp3JumpFrame': args.mp3_jump_frame, 'mp3FrameWalk': args.mp3_frame_walk,
              'fieldName': args.field_name, 'avcEvidence': args.avc_evidence, 'mediaCurves': args.media_curves,
              'avcSearch': args.avc_search, 'avcSearchPages': args.avc_search_pages, 'avcSearchLong': args.avc_search_long,
              'avcSearchGate': str(args.avc_search_gate) if args.avc_search_gate else None, 'avcSearchAction': args.avc_search_action, 'mp3History': args.mp3_history, 'mp3Frame': args.mp3_frame,
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
