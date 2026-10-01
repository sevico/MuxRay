"""Independent bounded-I/O and evidence-corruption tests for snapshot_check."""

import copy
import hashlib
import json
import os
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

from jsonschema import ValidationError

import snapshot_check as checker


class ReadSpy:
    def __init__(self, stream, reads, after_read=None):
        self.stream, self.reads, self.after_read = stream, reads, after_read

    def __getattr__(self, name):
        return getattr(self.stream, name)

    def read(self, size=-1):
        self.reads.append(size)
        if not 0 <= size <= 1 << 20:
            raise AssertionError(f"Unbounded source read: {size}")
        result = self.stream.read(size)
        if self.after_read is not None:
            self.after_read(result)
        return result


class SourcePathSpy:
    """A real file with observable reads and a controllable pathname identity."""
    def __init__(self, path):
        self.path, self.name = path, path.name
        self.reads, self.streams = [], []
        self.stat_override = None

    def open(self, mode):
        stream = ReadSpy(self.path.open(mode), self.reads)
        self.streams.append(stream)
        return stream

    def stat(self):
        return self.stat_override or self.path.stat()


class SnapshotCheckTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix="muxray-snapshot-check-")
        self.addCleanup(self.temporary.cleanup)
        self.directory = Path(self.temporary.name)
        golden = checker.ROOT / "testdata/golden/desktop-diagnostic-snapshot-full-v1.json"
        self.report = json.loads(golden.read_text(encoding="utf-8"))
        self.data = bytes.fromhex(self.report["hex"]["window"]["hex"])
        self.assertEqual(self.report["hex"]["window"]["offset"], "0")
        self.assertEqual(len(self.data), int(self.report["session"]["progress"]["sourceSize"]))
        self.source = self.directory / self.report["fileName"]
        self.source.write_bytes(self.data)
        self.report_path = self.directory / "snapshot.json"

    def check(self, report=None, source=None, **kwargs):
        self.report_path.write_text(json.dumps(self.report if report is None else report, ensure_ascii=False), encoding="utf-8")
        return checker.check_snapshot(self.report_path, self.source if source is None else source, **kwargs)

    def test_committed_full_snapshot_round_trip_and_exact_request_ids(self):
        result = self.check()
        self.assertTrue(result["schemaValid"])
        self.assertEqual(result["sourceSHA256"], hashlib.sha256(self.data).hexdigest())
        self.assertEqual(result["hex"]["bytes"], len(self.data))
        self.assertEqual(result["trendPointsMatchingSource"], 3)
        self.assertEqual(self.report["detail"]["requestId"], "9007199254740994")

    def test_large_source_requires_explicit_limit_and_reads_at_most_one_mib(self):
        length = checker.MAX_FIXTURE + 1
        source = self.directory / "large-acceptance.flv"
        with source.open("wb") as stream:
            stream.write(self.data[:13])
            stream.truncate(length)
        report = json.loads((checker.ROOT / "testdata/golden/desktop-diagnostic-snapshot-minimal-v1.json").read_text(encoding="utf-8"))
        report["fileName"] = source.name
        report["session"]["progress"]["sourceSize"] = str(length)
        report["session"]["state"] = "canceled"
        report["session"]["scan"]["complete"] = False
        spy = SourcePathSpy(source)
        with self.assertRaisesRegex(ValueError, "exceeds byte limit"):
            self.check(report, spy)
        self.assertTrue(spy.streams[-1].closed)
        result = self.check(report, spy, max_source_bytes=length)
        digest = hashlib.sha256()
        with source.open("rb") as stream:
            for block in iter(lambda: stream.read(1 << 20), b""):
                digest.update(block)
        self.assertEqual(result["sourceSHA256"], digest.hexdigest())
        self.assertGreater(len(spy.reads), 16)
        self.assertEqual(max(spy.reads), 1 << 20)
        self.assertTrue(all(0 <= size <= 1 << 20 for size in spy.reads))
        self.assertTrue(all(stream.closed for stream in spy.streams))

    def test_limit_range_and_slice_read_budget(self):
        for limit in (0, checker.MAX_FIXTURE - 1, checker.MAX_SOURCE + 1):
            with self.subTest(limit=limit), self.assertRaises(ValueError):
                checker.BoundedSource(self.source, limit)
        checker.BoundedSource(self.source, checker.MAX_SOURCE)
        with self.source.open("r+b") as stream:
            stream.truncate((1 << 20) + 1)
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            with self.assertRaisesRegex(ValueError, "buffer budget"):
                source[:]
            with self.assertRaises(ValueError):
                source[::2]
            with self.assertRaises(TypeError):
                source[0]
            self.assertEqual(source[len(source):len(source) + 1], b"")
            self.assertEqual(len(source[:1 << 20]), 1 << 20)

    def test_expired_deadline_rejects_slice_and_digest(self):
        for operation in (lambda source: source[:1], lambda source: source.digest()):
            with self.subTest(operation=operation), checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
                source.deadline = 0
                with self.assertRaisesRegex(ValueError, "time budget"):
                    operation(source)

    def test_slice_rejects_deadline_crossed_during_read(self):
        clock = [0]
        with mock.patch.object(checker.time, "monotonic", side_effect=lambda: clock[0]):
            with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
                source.stream = ReadSpy(source.stream, [], lambda data: clock.__setitem__(0, checker.MAX_SOURCE_SECONDS + 1))
                with self.assertRaisesRegex(ValueError, "time budget"):
                    source[:1]

    def test_digest_rejects_deadline_crossed_during_final_eof_read(self):
        clock = [0]

        def delay_eof(data):
            if not data:
                clock[0] = checker.MAX_SOURCE_SECONDS + 1

        with mock.patch.object(checker.time, "monotonic", side_effect=lambda: clock[0]):
            with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
                source.stream = ReadSpy(source.stream, [], delay_eof)
                with self.assertRaisesRegex(ValueError, "time budget"):
                    source.digest()

    def test_modified_and_truncated_sources_are_rejected(self):
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            before = self.source.stat()
            with self.source.open("r+b") as stream:
                stream.write(b"X")
            os.utime(self.source, ns=(before.st_atime_ns, before.st_mtime_ns + 1_000_000_000))
            with self.assertRaisesRegex(ValueError, "identity changed"):
                source.digest()
        self.source.write_bytes(self.data)
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            with self.source.open("r+b") as stream:
                stream.truncate(8)
            with self.assertRaisesRegex(ValueError, "short read"):
                source[:11]

    def test_growth_past_limit_and_pathname_identity_change_are_rejected(self):
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            with self.source.open("r+b") as stream:
                stream.truncate(checker.MAX_FIXTURE + 1)
            with self.assertRaisesRegex(ValueError, "grew beyond"):
                source.digest()
        self.source.write_bytes(self.data)
        spy = SourcePathSpy(self.source)
        with checker.BoundedSource(spy, checker.MAX_FIXTURE) as source:
            before = self.source.stat()
            spy.stat_override = SimpleNamespace(st_dev=before.st_dev, st_ino=before.st_ino + 1,
                                                st_size=before.st_size, st_mtime_ns=before.st_mtime_ns)
            with self.assertRaisesRegex(ValueError, "identity changed"):
                source.digest()

    def test_source_order_identity_and_offset_cannot_be_consistently_forged(self):
        changed = copy.deepcopy(self.report)
        changed["selection"]["selectedTagId"] = "1"
        changed["selectedTag"]["id"] = "1"
        changed["detail"]["tagId"] = "1"
        with self.assertRaisesRegex(ValueError, "source-order offset"):
            self.check(changed)
        changed = copy.deepcopy(self.report)
        changed["selectedTag"]["offset"] = "41"
        with self.assertRaisesRegex(ValueError, "source-order offset"):
            self.check(changed)

    def test_truncated_payload_is_indexed_but_incomplete_header_is_not(self):
        # Source-order Tag 1 begins at 41; its AAC payload begins at 52.
        self.source.write_bytes(self.data[:54])
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            self.assertEqual(checker.fixture_tag_offsets(source, {0, 1, 2}), {0: 13, 1: 41})
            checker.verify_tag(source, 41, 8, 0, 0, 4, 2)
            with self.assertRaisesRegex(ValueError, "metadata differs"):
                checker.verify_tag(source, 41, 8, 0, 0, 4, 4)
        self.source.write_bytes(self.data[:51])
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            self.assertEqual(checker.fixture_tag_offsets(source, {0, 1}), {0: 13})
            with self.assertRaisesRegex(ValueError, "outside source"):
                checker.verify_tag(source, 41, 8, 0, 0, 4, 0)

    def test_missing_previous_size_keeps_last_tag_and_header_visits_are_bounded(self):
        self.source.write_bytes(self.data[:58])  # Two bytes of Tag 1 PreviousTagSize.
        with checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            self.assertEqual(checker.fixture_tag_offsets(source, {0, 1, 2}), {0: 13, 1: 41})
            checker.verify_tag(source, 41, 8, 0, 0, 4, 4)
        with mock.patch.object(checker, "MAX_TAG_VISITS", 1), checker.BoundedSource(self.source, checker.MAX_FIXTURE) as source:
            self.assertEqual(checker.fixture_tag_offsets(source, {0}), {0: 13})
            with self.assertRaisesRegex(ValueError, "Tag headers"):
                checker.fixture_tag_offsets(source, {1})

    def test_hex_byte_count_offset_source_size_and_eof_mismatches_are_rejected(self):
        mutations = {
            "byte": lambda r: r["hex"]["window"].update(hex="00" + r["hex"]["window"]["hex"][2:]),
            "shortened": lambda r: r["hex"]["window"].update(hex=r["hex"]["window"]["hex"][:2], eof=False),
            "request_offset": lambda r: r["hex"].update(offset="1"),
            "source_size": lambda r: r["hex"]["window"].update(sourceSize="80"),
            "eof": lambda r: r["hex"]["window"].update(eof=False),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                changed = copy.deepcopy(self.report)
                mutate(changed)
                with self.assertRaises(ValueError):
                    self.check(changed)
        changed = copy.deepcopy(self.report)
        changed["hex"]["offset"] = str(len(self.data))
        changed["hex"]["window"].update(offset=str(len(self.data)), hex="", eof=True)
        self.assertEqual(self.check(changed)["hex"]["bytes"], 0)

    def test_missing_omission_and_detail_without_selected_tag_are_rejected(self):
        changed = copy.deepcopy(self.report)
        changed["hex"] = None
        with self.assertRaises(ValidationError):
            self.check(changed)
        changed = copy.deepcopy(self.report)
        changed["selectedTag"] = None
        changed["omitted"].append({"section": "selectedTag", "reason": "not_requested"})
        with self.assertRaisesRegex(ValueError, "different Tag"):
            self.check(changed)


if __name__ == "__main__":
    unittest.main()
