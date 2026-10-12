"""Build auditable Linux-only source-read gates for native search cancel/close.

The separate Go overlay blocks the first actual AVC search ReaderAt call for at
most 60 seconds, and extends only AVC task deadlines to 90 seconds. Normal source
files and production builds are untouched. Every run uses a new outside-repository
directory, a copied fixture, source/assets hashes, and PID/run/token receipts.
"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import uuid


HELPERS = r'''
// ACCEPTANCE OVERLAY ONLY: never included by normal builds.
const acceptanceSearchDirectory = @DIRECTORY@
const acceptanceSearchFixture = @FIXTURE@
const acceptanceSearchRun = @RUN@
var acceptanceSearchOnce sync.Once
var acceptanceSearchToken = strconv.Itoa(os.Getpid()) + "-" + time.Now().UTC().Format(time.RFC3339Nano)

type acceptanceSearchReader struct { ctx context.Context; source sourceFile }
func acceptanceSearchReceipt(name string, extra map[string]any) {
    row := map[string]any{"runId": acceptanceSearchRun, "pid": os.Getpid(), "processToken": acceptanceSearchToken, "timeUTC": time.Now().UTC().Format(time.RFC3339Nano)}
    for k, v := range extra { row[k] = v }
    data, err := json.Marshal(row); if err != nil { panic(err) }
    path := filepath.Join(acceptanceSearchDirectory, name)
    temp := path + ".tmp"
    file, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600); if err != nil { panic(err) }
    if _, err = file.Write(data); err != nil { panic(err) }; if err = file.Close(); err != nil { panic(err) }
    if err = os.Link(temp, path); err != nil { panic(err) }; if err = os.Remove(temp); err != nil { panic(err) }
}
func (r *acceptanceSearchReader) ReadAt(p []byte, offset int64) (int, error) {
    expected, err := os.Stat(acceptanceSearchFixture); if err != nil { return 0, err }
    actual, err := r.source.Stat(); if err != nil { return 0, err }
    if !os.SameFile(expected, actual) { return r.source.ReadAt(p, offset) }
    claimed := false
    acceptanceSearchOnce.Do(func() { claimed = true })
    if !claimed { return r.source.ReadAt(p, offset) }
    acceptanceSearchReceipt("entered.json", map[string]any{"offset": offset, "bytes": len(p), "source": acceptanceSearchFixture})
    timer := time.NewTimer(60 * time.Second); defer timer.Stop()
    ticker := time.NewTicker(20 * time.Millisecond); defer ticker.Stop()
    done := r.ctx.Done()
    for {
        select {
        case <-done:
            acceptanceSearchReceipt("context-canceled.json", map[string]any{"contextError": r.ctx.Err().Error()})
            done = nil
        case <-timer.C:
            acceptanceSearchReceipt("timed-out.json", map[string]any{"safetyRelease": true})
            return 0, fmt.Errorf("acceptance gate timed out")
        case <-ticker.C:
            data, err := os.ReadFile(filepath.Join(acceptanceSearchDirectory, "release.json")); if err != nil { continue }
            var release struct { RunID string `json:"runId"`; PID int `json:"pid"`; Token string `json:"processToken"` }
            if json.Unmarshal(data, &release) != nil || release.RunID != acceptanceSearchRun || release.PID != os.Getpid() || release.Token != acceptanceSearchToken { continue }
            reason := ""; if r.ctx.Err() != nil { reason = r.ctx.Err().Error() }
            acceptanceSearchReceipt("released.json", map[string]any{"contextError": reason})
            return r.source.ReadAt(p, offset)
        }
    }
}
'''


def digest(path):
    result = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda: stream.read(1 << 20), b''):
            result.update(block)
    return result.hexdigest()


def write_json(path, value):
    with Path(path).open('x') as stream:
        json.dump(value, stream, indent=2)
        stream.write('\n')


def patch_sources(search, document, directory, fixture, run):
    anchor = 'avcsearch.Search(ctx, d.source, d.scan.Tag'
    timeout_anchor = '\t\tctx, cancel := context.WithTimeout(d.ctx, timeout)'
    if search.count(anchor) != 1 or document.count(timeout_anchor) != 1 or search.count('\t"context"') != 1:
        raise ValueError('AVC source/deadline seam changed; review the overlay')
    search = search.replace('\t"context"', '\t"context"\n\t"encoding/json"\n\t"fmt"\n\t"os"\n\t"path/filepath"\n\t"strconv"\n\t"sync"\n\t"time"')
    search = search.replace(anchor, 'avcsearch.Search(ctx, &acceptanceSearchReader{ctx: ctx, source: d.source}, d.scan.Tag')
    helpers = HELPERS.replace('@DIRECTORY@', json.dumps(str(directory))).replace('@FIXTURE@', json.dumps(str(fixture))).replace('@RUN@', json.dumps(run))
    search += helpers
    document = document.replace(timeout_anchor, '\t\t// ACCEPTANCE OVERLAY ONLY: allow observed GUI cancellation before deadline.\n\t\tif selected.kind == avcSearchWork { timeout = 90 * time.Second }\n' + timeout_anchor)
    return search, document


def input_hashes(repository):
    # Exclude generated cache, node_modules, test data and this run's outputs.
    files = set()
    for part in ('internal', 'desktop'):
        files.update(p for p in (repository / part).rglob('*.go') if 'node_modules' not in p.parts)
    files.update((repository / 'desktop/frontend/dist').rglob('*'))
    files.update(repository / p for p in ('go.mod', 'go.sum', 'desktop/go.mod', 'desktop/go.sum', 'desktop/frontend/package-lock.json'))
    return {str(p.relative_to(repository)): digest(p) for p in sorted(files) if p.is_file()}


def prepare(repository, source, output):
    repository, source, output = map(lambda p: Path(p).resolve(), (repository, source, output))
    if repository == output or repository in output.parents:
        raise ValueError('Controlled overlays require a new directory outside the repository')
    if not (repository / 'desktop/frontend/dist/index.html').is_file():
        raise ValueError('Build production assets first')
    if not 13 <= source.stat().st_size <= 16 << 20 or source.read_bytes()[:3] != b'FLV':
        raise ValueError('Controlled fixture must be an FLV of at most 16 MiB')
    run = str(uuid.uuid4())
    fixture = output / 'controlled-avc.flv'
    search_path, document_path = repository / 'internal/document/avc_search.go', repository / 'internal/document/document.go'
    search, document = patch_sources(search_path.read_text(), document_path.read_text(), output, fixture, run)
    output.mkdir(parents=True, exist_ok=False, mode=0o700)
    fixture.write_bytes(source.read_bytes())
    replace = {}
    for original, content in ((search_path, search), (document_path, document)):
        overlaid = output / original.name
        overlaid.write_text(content)
        subprocess.run(['gofmt', '-w', str(overlaid)], check=True, timeout=30)
        replace[str(original)] = str(overlaid)
    write_json(output / 'overlay.json', {'Replace': replace})
    plan = {'format': 'muxray-avc-search-gate-v1', 'acceptanceOnly': True, 'runId': run, 'repository': str(repository),
            'directory': str(output), 'inputs': input_hashes(repository), 'source': str(fixture), 'sourceSHA256': digest(fixture),
            'toolSHA256': digest(Path(__file__)), 'overlay': {Path(p).name: digest(p) for p in replace.values()},
            'overlaySHA256': digest(output / 'overlay.json'), 'gateSeconds': 60, 'searchDeadlineSeconds': 90,
            'limitations': 'First matching actual ReaderAt only; source-byte blocking and extended deadline exist only in this separate acceptance executable.'}
    write_json(output / 'build-plan.json', plan)
    return plan


def validate_plan(output, check_inputs=True):
    output = Path(output).resolve()
    plan = json.loads((output / 'build-plan.json').read_text())
    if plan['directory'] != str(output) or plan['format'] != 'muxray-avc-search-gate-v1' or not plan['acceptanceOnly']:
        raise ValueError('Wrong controlled build plan')
    if digest(output / 'overlay.json') != plan['overlaySHA256'] or any(digest(output / name) != value for name, value in plan['overlay'].items()):
        raise ValueError('Controlled overlay identity changed')
    if digest(plan['source']) != plan['sourceSHA256'] or digest(Path(__file__)) != plan['toolSHA256']:
        raise ValueError('Controlled fixture or tool identity changed')
    if check_inputs and input_hashes(Path(plan['repository'])) != plan['inputs']:
        raise ValueError('Controlled source/assets changed')
    return plan


def build(output):
    output = Path(output).resolve()
    plan = validate_plan(output)
    binary = output / 'MuxRay-avc-controlled'
    if binary.exists():
        raise FileExistsError(binary)
    argv = ['go', 'build', '-mod=readonly', '-trimpath', '-tags', 'desktop,production,webkit2_41', '-overlay', str(output / 'overlay.json'), '-o', str(binary), '.']
    env = dict(os.environ, GOTOOLCHAIN='go1.27.0')
    with (output / 'build.stdout.log').open('x') as stdout, (output / 'build.stderr.log').open('x') as stderr:
        proc = subprocess.run(argv, cwd=Path(plan['repository']) / 'desktop', env=env, stdout=stdout, stderr=stderr, timeout=180)
    result = {'passed': proc.returncode == 0, 'argv': argv, 'exitCode': proc.returncode, 'binary': str(binary),
              'binarySHA256': digest(binary) if proc.returncode == 0 else None, 'buildPlanSHA256': digest(output / 'build-plan.json')}
    validate_plan(output)
    write_json(output / 'build-result.json', result)
    if not result['passed']:
        raise ValueError('Controlled build failed; inspect preserved build.stderr.log')
    return result


def validate_build(output, binary):
    output = Path(output).resolve()
    plan = validate_plan(output, check_inputs=False)
    result = json.loads((output / 'build-result.json').read_text())
    if (not result['passed'] or Path(binary).resolve() != Path(result['binary']) or digest(binary) != result['binarySHA256'] or
            digest(output / 'build-plan.json') != result['buildPlanSHA256']):
        raise ValueError('Controlled executable identity differs')
    return plan


def receipt(output, name, pid):
    output = Path(output).resolve()
    plan = validate_plan(output, check_inputs=False)
    record = json.loads((output / name).read_text())
    if record['runId'] != plan['runId'] or record['pid'] != pid or not record['processToken']:
        raise ValueError('Controlled receipt PID/run/token differs')
    if name != 'entered.json':
        entered = json.loads((output / 'entered.json').read_text())
        if record['processToken'] != entered['processToken']:
            raise ValueError('Controlled receipt generation differs')
    return record


def release(output, pid):
    output = Path(output).resolve()
    entered = receipt(output, 'entered.json', pid)
    canceled = receipt(output, 'context-canceled.json', pid)
    if canceled['contextError'] != 'context canceled' or (output / 'timed-out.json').exists() or (output / 'released.json').exists():
        raise ValueError('Gate was not canceled by the native command before release')
    path = output / 'release.json'
    temporary = output / 'release.json.tmp'
    write_json(temporary, {key: entered[key] for key in ('runId', 'pid', 'processToken')})
    os.link(temporary, path)
    temporary.unlink()
    return canceled


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repository', type=Path, required=True)
    parser.add_argument('--source', type=Path, required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args()
    prepare(args.repository, args.source, args.output_dir)
    print(json.dumps(build(args.output_dir), indent=2))


if __name__ == '__main__':
    main()
