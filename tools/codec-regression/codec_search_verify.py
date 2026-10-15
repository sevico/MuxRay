"""Independent native codec-page, UI and original-source checks."""

import hashlib
import json

from codec_search_oracle import retained
from generate import digest, require
from native_search_history import read_ui, verify_hex_ui
from verify_paged_codec import verify_mp3_native_page


def identity(row):
    return ':'.join(str(row[key]) if row[key] is not None else '-' for key in ('tagId', 'frameIndex', 'nalIndex', 'messageIndex'))


def snapshot_file(directory, stage, source):
    checked, path = stage['snapshotCheck'], directory / stage['file']
    require(checked['schemaValid'] and checked['sha256'] == digest(path) and
            checked['sourceSHA256'] == hashlib.sha256(source).hexdigest(), 'codec export receipt/source identity differs')
    return json.loads(path.read_text())


def page_rows(snapshot, case, filter_name, index, previous=None):
    require(snapshot['schemaVersion'] == snapshot['selection']['schemaVersion'] == 4, 'codec evidence requires snapshot v4')
    response = snapshot.get('codecSearchPage')
    require(response is not None and response['state'] == 'ready' and response['error'] is None, 'codec ready page missing')
    page = response['page']
    require(response['sessionId'] == snapshot['session']['sessionId'], 'codec session differs')
    require(snapshot['selection']['codecSearch'] == {'requestId': response['requestId'], 'cursor': page['cursor'],
                                                   'kind': case['kind'], 'filter': filter_name}, 'codec selection identity differs')
    require(page['kind'] == case['kind'] and page['filter'] == filter_name and page['scanComplete'] and
            page['total'] == str(case['indexedTags']), 'codec filter/index coverage differs')
    counts = case['pages'][filter_name]
    require(0 <= index < len(counts), 'unexpected codec page')
    start = sum(counts[:index])
    wanted = retained(case['rows'], case['kind'], filter_name)[start:start + counts[index]]
    require(page['rows'] == wanted, 'codec rows omitted, duplicated, invented or mapped to wrong bytes')
    require(page['exhausted'] == (index == len(counts) - 1), 'codec exhaustion differs')
    require(page['cursor'] == ('0' if previous is None else previous['page']['nextCursor']), 'opaque codec continuation token differs')
    require(page['nextCursor'] == ('0' if page['exhausted'] else response['requestId']), 'codec next cursor identity differs')
    require(int(response['requestId']) > 0 and (previous is None or int(response['requestId']) > int(previous['requestId'])), 'codec request was reused')
    require(page['start'] == ({'tagId': '0', 'frameIndex': 0} if previous is None else previous['page']['next']), 'codec position continuity differs')
    require(0 <= page['scannedTags'] <= 256 and 0 <= page['scannedFrames'] <= 1024 and
            len(page['rows']) <= 64 and int(page['bytesRead']) <= (65536 if case['kind'] == 'mp3' else 1048576), 'codec scan budget exceeded')
    require(int(page['bytesRead']) >= 0 and page['pending'] >= 0, 'negative codec work accounting')
    if page['exhausted']:
        require(page['next'] == {'tagId': str(case['indexedTags']), 'frameIndex': 0} and page['pending'] == 0, 'exhausted page retained unknown work')
    if 'positions' in case:
        require(page['start'] == dict(zip(('tagId', 'frameIndex'), (str(case['positions'][index][0]), case['positions'][index][1]))) and
                page['next'] == dict(zip(('tagId', 'frameIndex'), (str(case['positions'][index + 1][0]), case['positions'][index + 1][1]))), 'long MP3 frame position differs')
        require(page['scannedFrames'] == [1024, 1024, 952][index] and page['scannedTags'] == 1, 'long MP3 inspection accounting differs')
    if index in case.get('zeroIOPages', []):
        require(page['bytesRead'] == '0' and page['scannedTags'] == page['scannedFrames'] == 0, 'same-Tag pending rows unexpectedly reread source')
    if case['file'] == 'sei-pagination.flv':
        require(page['pending'] == [66, 2, 0][index], 'same-Tag pending row count differs')
    if case['file'] == 'sei-cross-256.flv':
        require(page['scannedTags'] == [256, 3][index], 'SEI context did not cross the Tag budget boundary')
    return response


def page_ui(ui, stage, response):
    page = response['page']
    prefix = (f'编解码器检索完成：{page["kind"]} · Tag {page["start"]["tagId"]} / 帧 {page["start"]["frameIndex"]} → '
              f'Tag {page["next"]["tagId"]} / 帧 {page["next"]["frameIndex"]} · 检查 {page["scannedTags"]} 个 Tag、'
              f'{page["scannedFrames"]} 个帧头 · 本批 {len(page["rows"])} 条 · 待显示 {page["pending"]} 条 · 读取 {page["bytesRead"]} byte。')
    require(stage['status'].startswith(prefix) and any(row['text'] == stage['status'] for row in ui), 'codec UI coverage differs from exported page')
    buttons = [r['name'] for r in ui if r['role'] == 'push button']
    targets = [name for name in buttons if name.startswith('从编解码器检索跳转 ') and name.endswith(' Tag')]
    require(targets == [f'从编解码器检索跳转 {identity(row)} Tag' for row in page['rows']], 'codec UI rows differ from exported page')
    frames = [name for name in buttons if name.startswith('从编解码器检索跳转 ') and name.endswith(' 帧')]
    require(frames == [f'从编解码器检索跳转 {identity(row)} 帧' for row in page['rows'] if row['complete'] and row['status'] == 'complete' and row['frameIndex'] is not None], 'codec stop or frame action availability differs')
    for row in page['rows']:
        for key, label in [('span', '证据'), ('spsSpan', 'SPS'), ('evidenceSpan', '选择依据')]:
            if row[key] is None:
                continue
            items = [item for item in ui if item['name'] == f'定位编解码器 {identity(row)} {label}']
            source = row[key]
            require(len(items) == 1 and items[0]['description'] == f'{source["byteOffset"]} : {source["bitOffset"]} · {source["bitLength"]} bit', 'codec UI source range differs')


def verify(directory, case, source):
    native = json.loads((directory / 'native-result.json').read_text())
    stages = native['codecSearchPages']
    require([(s['filter'], s['pageIndex']) for s in stages] == [(f, i) for f in case['filters'] for i in range(len(case['pages'][f]))], 'codec filter/page sequence differs')
    previous, filter_name, rows, spans = None, None, 0, 0
    for stage in stages:
        if stage['filter'] != filter_name:
            previous, filter_name = None, stage['filter']
        response = page_rows(snapshot_file(directory, stage, source), case, filter_name, stage['pageIndex'], previous)
        page_ui(read_ui(directory, stage), stage, response)
        for row in response['page']['rows']:
            for key in ('span', 'spsSpan', 'evidenceSpan'):
                if row[key] is not None:
                    s = row[key]
                    first = int(s['byteOffset']) * 8 + s['bitOffset']
                    require(0 <= first < first + s['bitLength'] <= len(source) * 8, 'codec source range is outside original bytes')
                    spans += 1
        rows += len(response['page']['rows'])
        previous = response
    restart = native.get('codecSearchRestart')
    require(bool(restart) == (case['file'] == 'sei-pagination.flv'), 'codec restart evidence missing or unexpected')
    if restart:
        first = page_rows(snapshot_file(directory, restart, source), case, case['filters'][0], 0)
        repeated = snapshot_file(directory, stages[0], source)['codecSearchPage']
        require(int(repeated['requestId']) > int(first['requestId']) and repeated['page']['rows'] == first['page']['rows'], 'codec restart retained a stale request or changed first rows')
        page_ui(read_ui(directory, restart), restart, first)
    mapped = 0
    if 'selectedFrame' in case:
        from linux_smoke import check_mp3_navigation_stage
        prior = None
        for stage in native['codecSearchFrameStages']:
            snapshot = snapshot_file(directory, stage, source)
            checked = check_mp3_navigation_stage(snapshot, case['selectedFrame'], prior)
            require(all(stage[key] == value for key, value in checked.items()), 'codec frame navigation receipt differs')
            prior = stage
        require(prior is not None and prior['state'] == 'ready', 'codec row did not reach the exact frame')
        checked = verify_mp3_native_page(snapshot, case['frameOracle'], source, 0, 2560, case['selectedFrame'])
        mapped = checked['mappedSideInfoFields']
    actions = native['codecSearchSources']
    require([s['destination'] for s in actions] == case['sourceKinds'], 'codec source action sequence differs')
    target = next(row for row in case['rows'] if identity(row) == case['sourceTarget'])
    for stage in actions:
        key = {'hex': 'span', 'sps': 'spsSpan', 'evidence': 'evidenceSpan'}[stage['destination']]
        require(stage['target'] == case['sourceTarget'] and stage['span'] == target[key], 'codec source selection identity differs')
        snapshot_file(directory, stage, source)
        verify_hex_ui(read_ui(directory, stage), stage['span'], stage['label'], source)
    final = json.loads((directory / 'snapshot.json').read_text())
    require(final['codecSearchPage'] == previous, 'final export did not preserve the last retained codec page')
    return {'pages': len(stages), 'rowsCompared': rows, 'sourceSpansCompared': spans,
            'sourceActions': len(actions), 'sideInfoFieldsCompared': mapped, 'restartVerified': bool(restart)}
