#!/usr/bin/env python3
"""Record bounded backend timing/allocation/read baselines, without thresholds."""

import argparse
import json
import os
from pathlib import Path
import platform
import re
import statistics
import sys
import traceback

from generate import ROOT, Commands, digest, executable, new_directory, require, save


BENCHMARKS = ('BenchmarkMP3PageNavigation/checkpoints_cold', 'BenchmarkMP3PageNavigation/checkpoints_warm',
              'BenchmarkSEITiming/parse_including_configuration', 'BenchmarkSEITiming/parse_with_existing_context',
              'BenchmarkSEITiming/compact_context_clone')


def parse_benchmarks(text, count):
    records = {name: [] for name in BENCHMARKS}
    for line in text.splitlines():
        if not line.startswith('Benchmark'):
            continue
        parts = line.split()
        name = re.sub(r'-\d+$', '', parts[0])
        require(name in records and len(parts) >= 8 and len(parts) % 2 == 0, 'unexpected benchmark result: ' + line)
        values = {parts[i + 1]: float(parts[i]) for i in range(2, len(parts), 2)}
        require(int(parts[1]) > 0 and all(v >= 0 for v in values.values()), 'invalid benchmark measurement')
        require({'ns/op', 'B/op', 'allocs/op', 'source-B/op', 'reads/op'} <= set(values), 'benchmark omitted resource metrics')
        records[name].append({'iterations': int(parts[1]), 'metrics': values})
    require(all(len(rows) == count for rows in records.values()), 'benchmark repeats missing or duplicated')
    # These are deterministic workload identities, never cross-machine speed
    # thresholds. A faster benchmark that skips the intended work is invalid.
    for name, pages, requests, reads in [('checkpoints_cold', 41, 3, 94800), ('checkpoints_warm', 1, 1, 2312)]:
        for row in records['BenchmarkMP3PageNavigation/' + name]:
            values = row['metrics']
            require(values['pages/op'] == pages and values['requests/op'] == requests and values['source-B/op'] == reads,
                    'MP3 performance workload changed its bounded page/read coverage')
    for row in records['BenchmarkSEITiming/compact_context_clone']:
        require(row['metrics']['source-B/op'] == 0 and row['metrics']['reads/op'] == 0, 'context clone unexpectedly reread source')
    return {name: {'samples': rows, 'median': {key: statistics.median(row['metrics'][key] for row in rows)
                                             for key in rows[0]['metrics']}} for name, rows in records.items()}


def run(args, commands):
    metadata = {'platform': platform.platform(), 'machine': platform.machine(), 'python': sys.version,
                'goToolchain': os.environ.get('GOTOOLCHAIN'), 'cpu': next((line.split(':', 1)[1].strip()
                for line in Path('/proc/cpuinfo').read_text().splitlines() if line.startswith('model name')), None)}
    for name, argv in [('go-version', [args.go, 'version']), ('git-head', ['git', 'rev-parse', 'HEAD']),
                       ('git-status', ['git', 'status', '--short'])]:
        result = commands.run(name, argv, cwd=ROOT)
        metadata[name] = result.read_text().strip()
    output = commands.run('backend-benchmarks', [args.go, 'test', './internal/document', './internal/avc', '-run', '^$',
        '-bench', '^Benchmark(MP3PageNavigation|SEITiming)$', '-benchmem', '-benchtime', str(args.benchtime_ms) + 'ms',
        '-count', str(args.count)], cwd=ROOT, timeout=180)
    results = parse_benchmarks(output.read_text(), args.count)
    return {'passed': True, 'metadata': metadata, 'repeats': args.count, 'benchtimeMilliseconds': args.benchtime_ms,
            'benchmarks': results, 'outputSHA256': digest(output), 'scope': {
                'mp3': 'Already-open local file; document checkpoint cold/warm. Scan, open, dependency resolution, transport and UI excluded. OS page cache is not flushed.',
                'sei': 'In-memory ReaderAt; parse including SPS/HRD configuration, parse with existing context, and compact-context clone.',
                'memory': 'Go B/op and allocs/op measure allocations, not RSS or peak live heap.',
                'thresholds': 'No machine-dependent elapsed-time or memory pass/fail thresholds.'}}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--go', type=executable, default=executable('go'))
    parser.add_argument('--output-dir', type=new_directory, required=True)
    parser.add_argument('--count', type=int, default=3)
    parser.add_argument('--benchtime-ms', type=int, default=200)
    args = parser.parse_args(argv)
    if not 1 <= args.count <= 5 or not 50 <= args.benchtime_ms <= 500:
        parser.error('--count must be 1–5 and --benchtime-ms must be 50–500')
    args.output_dir.mkdir(parents=True)
    try:
        result = run(args, Commands(args.output_dir))
    except Exception as error:
        result = {'passed': False, 'error': str(error), 'traceback': traceback.format_exc()}
    save(args.output_dir, 'result.json', result)
    print(json.dumps(result, indent=2))
    return 0 if result['passed'] else 1


if __name__ == '__main__':
    sys.exit(main())
