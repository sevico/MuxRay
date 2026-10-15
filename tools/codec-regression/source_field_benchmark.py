#!/usr/bin/env python3
"""Bounded whole-source vs span-window oracle comparison; no latency pass threshold."""

import argparse
import json
import statistics
import time
import tracemalloc

from side_info_oracle import parse_side_info, source_field


class MeasuredSource:
    def __init__(self, data):
        self.data, self.bytes_visited = data, 0

    def __len__(self):
        return len(self.data)

    def __iter__(self):
        self.bytes_visited += len(self.data)
        return iter(self.data)

    def __getitem__(self, key):
        result = self.data[key]
        self.bytes_visited += len(result)
        return result


def original_source_field(data, field):
    span = field['span']
    at = span['byteOffset'] * 8 + span['bitOffset']
    bits = ''.join(f'{value:08b}' for value in data)
    actual = bits[at:at + span['bitLength']]
    if actual != field['encodedBits'] or int(actual, 2) != field['value']:
        raise RuntimeError('original algorithm disagrees with authored field')


def compare(source_bytes, repetitions):
    # A late complete MPEG-1 stereo side-info block has 62 small fields.
    data = bytes(source_bytes)
    fields = parse_side_info(bytes(32), 3, 2, source_bytes - 32)['fields']
    result = {'sourceBytes': source_bytes, 'fieldsPerPass': len(fields), 'repetitions': repetitions,
              'scope': 'Python oracle only; no native UI/product parsing, OS cache flushing or latency threshold.',
              'algorithms': {}}
    for name, operation in [('whole_source_reference', original_source_field), ('span_window', source_field)]:
        measured, times = MeasuredSource(data), []
        for _ in range(repetitions):
            start = time.perf_counter()
            for field in fields:
                operation(measured, field)
            times.append(time.perf_counter() - start)
        # Measure one call separately so allocation tracing does not distort timing.
        tracemalloc.start()
        operation(data, fields[0])
        _, peak = tracemalloc.get_traced_memory()
        tracemalloc.stop()
        result['algorithms'][name] = {'seconds': times, 'medianSeconds': statistics.median(times),
                                      'sourceBytesVisited': measured.bytes_visited,
                                      'singleCallTracemallocPeakBytes': peak}
    expected = repetitions * sum((f['span']['bitOffset'] + f['span']['bitLength'] + 7) // 8 for f in fields)
    if result['algorithms']['span_window']['sourceBytesVisited'] != expected:
        raise RuntimeError('window algorithm visited bytes outside the exact field spans')
    return {'passed': True, **result}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-bytes', type=int, default=1251029)
    parser.add_argument('--repetitions', type=int, default=1)
    args = parser.parse_args(argv)
    if not 32 <= args.source_bytes <= 2 << 20 or not 1 <= args.repetitions <= 3:
        parser.error('source bytes must be 32 through 2097152; repetitions 1 through 3')
    print(json.dumps(compare(args.source_bytes, args.repetitions), indent=2))
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
