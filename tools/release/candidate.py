"""Build, attribute, package and verify a Windows desktop candidate; never publish it."""
from __future__ import annotations

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import re
import shutil
import subprocess
import sys
import tempfile
import zipfile

HERE = Path(__file__).resolve().parent
VERSION = re.compile(r"(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)(?:-(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?")
PRIVATE_PATH = re.compile(r"(?i)(?:\b[a-z]:[\\/]|/(?:Users|home|tmp|private|mnt)/|\\\\[^\s\\]+\\|(?<![:/])//[^\s/]+/)")
INPUTS = ('go.mod', 'desktop/go.mod', 'desktop/go.sum',
          'desktop/frontend/package.json', 'desktop/frontend/package-lock.json')
TAGS = ['desktop', 'wv2runtime.error', 'production']
PROJECT_MODULES = {'github.com/sevico/muxray', 'github.com/sevico/muxray/desktop'}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def sha(path):
    with Path(path).open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def read_json(path):
    return json.loads(Path(path).read_text(encoding='utf-8-sig'))


def write_json(path, value):
    Path(path).write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n', encoding='utf-8')


def run(argv, cwd, env=None):
    return subprocess.check_output([str(a) for a in argv], cwd=cwd, env=env,
                                   text=True, encoding='utf-8', errors='strict')


def validate_version(version):
    require(len(version) <= 64 and VERSION.fullmatch(version), 'Version must be SemVer without a v prefix (not dev).')


def check_source(repo, commit):
    require(re.fullmatch('[0-9a-f]{40}', commit), 'Commit must be the full lowercase 40-character SHA.')
    require(run(['git', 'rev-parse', 'HEAD'], repo).strip() == commit, 'HEAD does not match requested commit.')
    require(not run(['git', 'status', '--porcelain', '--untracked-files=all'], repo).strip(),
            'Source must be clean, including untracked files. Commit changes or use a clean checkout.')


def relative_name(name):
    require(isinstance(name, str) and name and '\\' not in name and ':' not in name,
            'Invalid archive/manifest path.')
    p = PurePosixPath(name)
    require(not p.is_absolute() and all(part not in ('', '.', '..') for part in name.split('/')),
            'Path must be a normalized relative path without traversal.')
    require(all(not part.endswith((' ', '.')) for part in p.parts), 'Nonportable path.')
    return p


def contained_file(root, name):
    relative_name(name)
    root = Path(root).resolve()
    path = root / name
    require(path.resolve().is_relative_to(root), 'Path escapes input directory.')
    require(path.is_file() and not path.is_symlink(), 'Required regular file is missing: ' + name)
    require(all(not parent.is_symlink() for parent in path.parents if parent != root and parent.is_relative_to(root)),
            'Symlinked input directories are not allowed.')
    return path


def public_text(path):
    text = Path(path).read_text(encoding='utf-8-sig')
    require(not PRIVATE_PATH.search(text), 'Private absolute path in public file: ' + Path(path).name)
    return text


def binary_metadata(raw):
    first = raw.splitlines()[0]
    go_version = first.rsplit(': ', 1)[-1]
    require(re.fullmatch(r'go\d+\.\d+(?:\.\d+)?', go_version), 'Unrecognized Go version.')
    settings = dict(re.findall(r'^\s*build\s+([^=\s]+)=(.*)$', raw, re.M))
    require(settings.get('GOOS') == 'windows' and settings.get('GOARCH') == 'amd64', 'Expected Windows amd64 executable.')
    require(settings.get('-buildmode') == 'exe' and settings.get('-trimpath') == 'true', 'Expected trimmed production executable.')
    require(settings.get('-tags', '').split(',') == TAGS, 'Unexpected build tags (overlay or prototype builds are unsupported).')
    require(re.search(r'^\s*path\s+github.com/sevico/muxray/desktop$', raw, re.M), 'Wrong application module.')
    deps = dict(re.findall(r'^\s*dep\s+(\S+)\s+(\S+)', raw, re.M))
    return {'goVersion': go_version, 'dependencies': deps, 'buildTags': TAGS}


def validate_audit(audit, exe, metadata, repo=None):
    inventory_file = contained_file(audit, 'license-inventory.json')
    inventory = read_json(inventory_file)
    require(inventory.get('schemaVersion') == 1, 'Unsupported license inventory.')
    binary = inventory['auditedBinary']
    require(binary['sha256'] == sha(exe) and binary['bytes'] == exe.stat().st_size,
            'License inventory belongs to a different executable.')
    require(binary['goVersion'] == metadata['goVersion'] and binary['buildTags'] == TAGS, 'License build identity mismatch.')
    require(inventory.get('missingLicenseTexts') == [], 'License collection is incomplete.')
    components = inventory['components']
    require(components and len({c['id'] for c in components}) == len(components), 'Missing or duplicate license components.')
    linked = {k: v for k, v in metadata['dependencies'].items() if k not in PROJECT_MODULES}
    attributed = {c['name']: c['version'] for c in components if c['inclusionBasis']['kind'] == 'production-go-module'}
    require(linked == attributed, 'Go module coverage differs from the actual EXE.')
    inputs = inventory['inputs']
    require(len(inputs) == len(INPUTS) and {i['file'] for i in inputs} == {'REPO/' + n for n in INPUTS},
            'License input manifest is incomplete.')
    if repo:
        for entry in inputs:
            require(sha(contained_file(repo, entry['file'][5:])) == entry['sha256'], 'License source inputs changed.')
    files = {'license-inventory.json', 'THIRD-PARTY-NOTICES.txt'}
    notice_text = public_text(contained_file(audit, 'THIRD-PARTY-NOTICES.txt'))
    require(not inventory.get('nativeExclusions') or all(e['fullPayloadOffsetInAuditedEXE'] == -1 for e in inventory['nativeExclusions']),
            'Unexpected redistributed native runtime.')
    for component in components:
        require(component.get('noticeSources'), 'Missing license text for ' + component['id'])
        for notice in component['noticeSources']:
            name = notice['copiedFile']
            require(relative_name(name).parts[0] == 'licenses', 'Notice must be below licenses/.')
            source = contained_file(audit, name)
            require(sha(source) == notice['copiedSHA256'] and source.stat().st_size == notice['bytes'], 'License text hash mismatch.')
            require(public_text(source).rstrip() in notice_text, 'Combined notices omit a required license.')
            files.add(name)
    for name in files:
        public_text(contained_file(audit, name))
    return inventory, files


def collect_audit(repo, exe, audit, raw, metadata, env):
    audit.mkdir()
    goenv = json.loads(run(['go', 'env', '-json', 'GOROOT', 'GOMODCACHE'], repo / 'desktop', env))
    write_json(audit / 'audit-environment.json', {
        'repoRoot': str(repo), 'goRoot': goenv['GOROOT'], 'modCache': goenv['GOMODCACHE'],
        'goVersion': metadata['goVersion'], 'binaryPath': str(exe),
        'state': 'Candidate inventory bound to the recorded executable SHA-256; publisher review required.',
    })
    (audit / 'go-production-binary-metadata.txt').write_text(raw, encoding='utf-8')
    graph = run(['go', 'list', '-deps', '-json', '-tags', ','.join(TAGS), '.'], repo / 'desktop', env)
    decoder, position, packages = json.JSONDecoder(), 0, []
    while position < len(graph):
        if graph[position].isspace():
            position += 1
            continue
        package, position = decoder.raw_decode(graph, position)
        packages.append(package)
    modules = {p['Module']['Path']: p['Module'] for p in packages if 'Module' in p}
    write_json(audit / 'go-production-modules.json', {
        'modules': list(modules.values()),
        'packages': [{'importPath': p['ImportPath'], 'dir': p.get('Dir'), 'standard': p.get('Standard', False)} for p in packages],
    })
    run(['node', HERE / 'frontend-audit.mjs', audit / 'frontend-bundle-modules.json'], repo / 'desktop/frontend', env)
    shutil.copyfile(HERE / 'Unicode-LICENSE.txt', audit / 'Unicode-LICENSE.txt')
    run([sys.executable, '-B', HERE / 'collect_notices.py', '--audit-dir', audit], repo, env)
    validate_audit(audit, exe, metadata, repo)


def package(repo, version, commit, exe, audit, notes, output, metadata):
    """Only the build entry point calls this after building and checking clean source."""
    validate_version(version)
    check_source(repo, commit)
    inventory, licenses = validate_audit(audit, exe, metadata, repo)
    note_text = public_text(notes)
    require(note_text.splitlines()[0].strip() == '# MuxRay v' + version, 'Release notes heading must match Version exactly.')
    public_text(repo / 'LICENSE')
    name = 'MuxRay-v' + version + '-windows-x64-candidate'
    stage = output / 'package' / name
    stage.mkdir(parents=True)
    assets = output / 'assets'
    assets.mkdir()
    shutil.copyfile(exe, stage / 'MuxRay.exe')
    shutil.copyfile(repo / 'LICENSE', stage / 'LICENSE')
    shutil.copyfile(notes, stage / 'RELEASE-NOTES.md')
    for file in sorted(licenses):
        destination = stage / file
        destination.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(audit / file, destination)
    (stage / 'README.txt').write_text(
        f'MuxRay {version} — Windows x64 candidate\n\n'
        'Extract the complete folder and run MuxRay.exe. Microsoft Edge WebView2 Runtime must already be installed.\n'
        'This portable desktop package includes no installer, CLI, or WebView2 Runtime.\n'
        'Open an FLV file to inspect tags, codec details and bytes, then export a diagnostic JSON snapshot.\n'
        'Review snapshots before sharing; they may contain filenames and media-derived values.\n'
        'This is a build candidate, not a published or accepted release. CI, security and native acceptance are separate gates.\n'
        'See RELEASE-NOTES.md, BUILD-INFO.json, LICENSE and THIRD-PARTY-NOTICES.txt.\n'
        'Project: https://github.com/sevico/MuxRay\n', encoding='utf-8')
    files = licenses | {'MuxRay.exe', 'LICENSE', 'README.txt', 'RELEASE-NOTES.md'}
    info = {
        'schemaVersion': 1, 'product': 'MuxRay', 'version': version, 'status': 'candidate',
        'sourceCommit': commit, 'sourceURL': 'https://github.com/sevico/MuxRay/tree/' + commit,
        'platform': 'windows-amd64', 'license': 'MIT', 'archiveRoot': name,
        'build': {'command': 'desktop/build-production.ps1 -Version ' + version,
                  'versionSource': 'linker argument supplied by candidate builder; native display not checked',
                  'goVersion': metadata['goVersion'], 'buildTags': TAGS, 'trimpath': True},
        'verification': {'ci': 'not-run-by-this-tool', 'nativeAcceptance': 'not-run-by-this-tool',
                         'securityScan': 'not-run-by-this-tool', 'licenseInventory': 'license-inventory.json'},
        'files': {n: {'bytes': (stage / n).stat().st_size, 'sha256': sha(stage / n)} for n in sorted(files)},
    }
    write_json(stage / 'BUILD-INFO.json', info)
    public_text(stage / 'BUILD-INFO.json')
    shutil.copyfile(stage / 'BUILD-INFO.json', assets / 'BUILD-INFO.json')
    files.add('BUILD-INFO.json')
    require({p.relative_to(stage).as_posix() for p in stage.rglob('*') if p.is_file()} == files, 'Non-whitelisted package file.')
    archive = assets / (name + '.zip')
    # Stable ordering/timestamps for identical input bytes; no reproducible compiler claim.
    with zipfile.ZipFile(archive, 'x', compression=zipfile.ZIP_DEFLATED, compresslevel=9) as zipped:
        for file in sorted(files):
            member = zipfile.ZipInfo(name + '/' + file, (1980, 1, 1, 0, 0, 0))
            member.compress_type = zipfile.ZIP_DEFLATED
            member.external_attr = 0o100644 << 16
            zipped.writestr(member, (stage / file).read_bytes())
    (assets / 'SHA256SUMS.txt').write_text(''.join(f'{sha(assets / n)}  {n}\n' for n in [archive.name, 'BUILD-INFO.json']), encoding='ascii')
    verify(assets)
    return assets


def verify(assets):
    """Check checksums, whitelist, extraction, inventory and exact executable identity offline."""
    checksums = {}
    for line in contained_file(assets, 'SHA256SUMS.txt').read_text(encoding='ascii').splitlines():
        digest, name = line.split('  ', 1)
        require(name not in checksums and re.fullmatch('[0-9a-f]{64}', digest), 'Invalid checksum list.')
        require(sha(contained_file(assets, name)) == digest, 'Asset checksum mismatch.')
        checksums[name] = digest
    info = read_json(contained_file(assets, 'BUILD-INFO.json'))
    validate_version(info['version'])
    require(info['schemaVersion'] == 1 and info['status'] == 'candidate', 'Expected candidate metadata.')
    require(all(info['verification'].get(k) == 'not-run-by-this-tool' for k in ('ci', 'nativeAcceptance', 'securityScan')),
            'Candidate metadata makes unsupported validation claims.')
    root = 'MuxRay-v' + info['version'] + '-windows-x64-candidate'
    require(info['archiveRoot'] == root, 'Unexpected archive root.')
    require(set(checksums) == {root + '.zip', 'BUILD-INFO.json'}, 'Unexpected asset list.')
    files = info['files']
    require({'MuxRay.exe', 'LICENSE', 'README.txt', 'RELEASE-NOTES.md', 'license-inventory.json', 'THIRD-PARTY-NOTICES.txt'} <= set(files),
            'Required package file missing.')
    expected = {root + '/' + n for n in files} | {root + '/BUILD-INFO.json'}
    for member in expected:
        relative_name(member)
    require(len({n.casefold() for n in expected}) == len(expected), 'Case-insensitive path collision.')
    with tempfile.TemporaryDirectory(prefix='muxray-candidate-verify-') as temporary:
        with zipfile.ZipFile(assets / (root + '.zip')) as zipped:
            require(len(zipped.infolist()) == len(expected) and set(zipped.namelist()) == expected, 'Archive whitelist or duplicate mismatch.')
            require(len(expected) <= 1000 and sum(i.file_size for i in zipped.infolist()) <= 512 * 1024 * 1024, 'Archive exceeds candidate bounds.')
            require(all((i.external_attr >> 16) & 0o170000 == 0o100000 for i in zipped.infolist()), 'Archive contains non-regular files.')
            require(zipped.testzip() is None, 'ZIP CRC mismatch.')
            zipped.extractall(temporary)
        extracted = Path(temporary) / root
        require((extracted / 'BUILD-INFO.json').read_bytes() == (assets / 'BUILD-INFO.json').read_bytes(), 'Embedded metadata differs.')
        for name, entry in files.items():
            path = contained_file(extracted, name)
            require(sha(path) == entry['sha256'] and path.stat().st_size == entry['bytes'], 'Extracted file mismatch.')
            if name != 'MuxRay.exe':
                public_text(path)
        inventory = read_json(extracted / 'license-inventory.json')
        metadata = {'goVersion': info['build']['goVersion'], 'dependencies': {
            c['name']: c['version'] for c in inventory['components'] if c['inclusionBasis']['kind'] == 'production-go-module'}}
        _, notices = validate_audit(extracted, extracted / 'MuxRay.exe', metadata)
        require(set(files) == notices | {'MuxRay.exe', 'LICENSE', 'README.txt', 'RELEASE-NOTES.md'}, 'Unapproved package contents.')
    return {'status': 'candidate', 'checksums': 'verified', 'extractedFiles': len(expected)}


def build(args):
    repo, output = args.repo.resolve(), args.output.resolve()
    validate_version(args.version)
    check_source(repo, args.commit)
    require(os.name == 'nt', 'Windows x64 candidate builds require Windows.')
    notes = contained_file(repo, 'docs/releases/v' + args.version + '.md')
    require(not output.exists(), 'Use a new output directory; existing evidence is never overwritten.')
    # Output inside a checkout is supported only under the ignored .cache/ directory.
    if output.is_relative_to(repo):
        require(output.is_relative_to(repo / '.cache'), 'Repository output must be under .cache/.')
    output.mkdir(parents=True)
    env = dict(os.environ, GOFLAGS='-mod=readonly', GOWORK='off', GOENV='off', GOOS='windows', GOARCH='amd64',
               PYTHONOPTIMIZE='', PYTHONDONTWRITEBYTECODE='1', GOTELEMETRY='off')
    shell = shutil.which('pwsh') or shutil.which('powershell')
    require(shell, 'PowerShell is required.')
    print('Building exact production candidate...', flush=True)
    try:
        log = run([shell, '-NoProfile', '-File', repo / 'desktop/build-production.ps1', '-Version', args.version], repo, env)
    except subprocess.CalledProcessError as error:
        (output / 'build.log').write_text(error.output or '', encoding='utf-8')
        raise
    (output / 'build.log').write_text(log, encoding='utf-8')
    check_source(repo, args.commit)
    exe = output / 'MuxRay.exe'
    shutil.copyfile(repo / 'desktop/build/bin/MuxRay.exe', exe)
    raw = run(['go', 'version', '-m', exe], repo / 'desktop', env)
    metadata = binary_metadata(raw)
    print('Collecting licenses from exact executable and embedded assets...', flush=True)
    audit = output / 'license-audit'
    collect_audit(repo, exe, audit, raw, metadata, env)
    check_source(repo, args.commit)
    assets = package(repo, args.version, args.commit, exe, audit, notes, output, metadata)
    write_json(output / 'verification.json', {**verify(assets), 'verifiedUTC': datetime.now(timezone.utc).isoformat()})
    print('Candidate assets verified: ' + str(assets))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    create = commands.add_parser('build')
    create.add_argument('--repo', type=Path, default=HERE.parents[1])
    create.add_argument('--version', required=True)
    create.add_argument('--commit', required=True)
    create.add_argument('--output', type=Path, required=True)
    check = commands.add_parser('verify')
    check.add_argument('--assets', type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.command == 'build':
            build(args)
        else:
            print(json.dumps(verify(args.assets.resolve()), indent=2))
    except (ValueError, OSError, KeyError, subprocess.CalledProcessError, zipfile.BadZipFile) as error:
        parser.exit(1, 'Candidate failed: ' + str(error) + '\n')


if __name__ == '__main__':
    main()
