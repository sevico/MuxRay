#!/usr/bin/env python3
"""Validate the pinned pre-v6 CLI source before building a regression baseline."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys


ROOT = Path(__file__).resolve().parents[2]
MANIFEST = Path(__file__).with_name('timing_baseline.json')


def git(repo, *args):
    result = subprocess.run(['git', '-C', str(repo), *args], capture_output=True,
                            text=True, encoding='utf-8', timeout=30)
    if result.returncode:
        raise RuntimeError(f'git {" ".join(args)} failed: {result.stderr.strip()}')
    return result.stdout.strip()


def check(repo, manifest):
    raw = manifest.read_bytes()
    entry = json.loads(raw)
    if entry.get('schemaVersion') != 1:
        raise ValueError('unsupported timing baseline manifest version')
    for field in ('baselineCommit', 'baselineTree', 'featureCommit', 'retiredUnavailableCommit'):
        if not isinstance(entry.get(field), str) or not re.fullmatch(r'[0-9a-f]{40}', entry[field]):
            raise ValueError(f'{field} must be a full lowercase Git object ID')
    baseline, feature = entry['baselineCommit'], entry['featureCommit']
    for commit in (baseline, feature):
        if git(repo, 'cat-file', '-t', commit) != 'commit':
            raise ValueError(f'baseline history object is not a commit: {commit}')
    tree = git(repo, 'rev-parse', baseline + '^{tree}')
    if tree != entry['baselineTree']:
        raise ValueError('baseline tree does not match the registered source')
    parents = git(repo, 'show', '-s', '--format=%P', feature).split()
    if not parents or parents[0] != baseline:
        raise ValueError('baseline must be the feature commit first parent')
    head = git(repo, 'rev-parse', 'HEAD')
    git(repo, 'merge-base', '--is-ancestor', feature, head)
    return {'passed': True, 'headCommit': head, 'baselineCommit': baseline,
            'baselineTree': tree, 'featureCommit': feature,
            'manifestSHA256': hashlib.sha256(raw).hexdigest(),
            'registration': entry['registration'],
            'retiredUnavailableCommit': entry['retiredUnavailableCommit']}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--repo', type=Path, default=ROOT)
    parser.add_argument('--manifest', type=Path, default=MANIFEST)
    parser.add_argument('--output', type=Path, help='new JSON receipt; existing evidence is never replaced')
    parser.add_argument('--print-commit', action='store_true', help='print only the validated baseline commit on success')
    args = parser.parse_args(argv)
    # Reserve the evidence path before validation. Repeated invocations must use
    # another path, including when an earlier validation failed.
    output = args.output.open('x', encoding='utf-8') if args.output else None
    try:
        try:
            result = check(args.repo, args.manifest)
        except Exception as error:
            result = {'passed': False, 'error': str(error)}
        rendered = json.dumps(result, ensure_ascii=False, indent=2) + '\n'
        if output:
            output.write(rendered)
        if args.print_commit and result['passed']:
            print(result['baselineCommit'])
        else:
            print(rendered, end='', file=sys.stderr if args.print_commit else sys.stdout)
        return 0 if result['passed'] else 1
    finally:
        if output:
            output.close()


if __name__ == '__main__':
    sys.exit(main())
