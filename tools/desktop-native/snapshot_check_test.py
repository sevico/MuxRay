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

    def v2_report(self):
        return json.loads((checker.ROOT / "testdata/golden/desktop-diagnostic-snapshot-full-v2.json").read_text(encoding="utf-8"))

    def test_v2_retains_selected_mp3_frame_curve_and_source_bytes(self):
        report = self.v2_report()
        source = self.directory / report["fileName"]
        source.write_bytes(bytes.fromhex(report["hex"]["window"]["hex"]))
        self.assertEqual(source.stat().st_size, int(report["session"]["progress"]["sourceSize"]))
        self.assertTrue(self.check(report, source)["schemaValid"])
        self.assertEqual(next(f["value"] for f in report["detail"]["detail"]["fields"] if f["name"] == "selectedFrameIndex"), "2")
        self.assertIsNotNone(report["mediaCurvePage"]["page"]["points"][0]["mp3"]["bitsPerSecond"])

    def test_v2_curve_identity_coordinates_arithmetic_and_unknowns_cannot_be_forged(self):
        mutations = {
            "identity": lambda r: r["mediaCurvePage"].update(requestId="1"),
            "coverage": lambda r: r["mediaCurvePage"]["page"].update(indexedTags="3"),
            "bounds": lambda r: r["mediaCurvePage"]["page"]["bounds"].update(next="0"),
            "point": lambda r: r["mediaCurvePage"]["page"]["points"][0].update(startTag="1"),
            "rate": lambda r: r["mediaCurvePage"]["page"]["points"][0]["mp3"].update(bitsPerSecond="1"),
            "rate_source": lambda r: r["mediaCurvePage"]["page"]["points"][0]["mp3"].update(tagId="2"),
            "unknown_rate": lambda r: r["mediaCurvePage"]["page"]["points"][0]["video"].update(coveredBytes="1"),
            "unknown_gop": lambda r: r["mediaCurvePage"]["page"]["points"][0]["gop"].update(totalDurationMs="1"),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                report = self.v2_report()
                mutate(report)
                self.report_path.write_text(json.dumps(report), encoding="utf-8")
                with self.assertRaises((ValueError, ValidationError)):
                    checker.check_snapshot(self.report_path)

    def test_snapshot_versions_and_omissions_remain_distinct(self):
        for name in ("minimal-v1", "minimal-v2", "full-v1", "full-v2", "minimal-v3", "full-v3", "minimal-v4", "full-v4", "mp3-v4"):
            path = checker.ROOT / f"testdata/golden/desktop-diagnostic-snapshot-{name}.json"
            self.assertTrue(checker.check_snapshot(path)["schemaValid"])
        for version in (0, 5, True, "2"):
            report = self.v2_report()
            report["schemaVersion"] = version
            self.report_path.write_text(json.dumps(report), encoding="utf-8")
            with self.subTest(version=version), self.assertRaisesRegex(ValueError, "Unsupported snapshot version"):
                checker.check_snapshot(self.report_path)
        report = self.v2_report()
        report["mediaCurvePage"] = None
        self.report_path.write_text(json.dumps(report), encoding="utf-8")
        with self.assertRaises((ValueError, ValidationError)):
            checker.check_snapshot(self.report_path)

    def v3_report(self):
        return json.loads((checker.ROOT / "testdata/golden/desktop-diagnostic-snapshot-full-v3.json").read_text())

    def v4_report(self, kind="sei"):
        name = "full" if kind == "sei" else "mp3"
        return json.loads((checker.ROOT / f"testdata/golden/desktop-diagnostic-snapshot-{name}-v4.json").read_text())

    def test_v4_sei_pending_page_has_zero_reads_and_precise_source(self):
        report = self.v4_report()
        page = report["codecSearchPage"]["page"]
        self.assertEqual((page["start"], page["next"], page["scannedTags"], page["bytesRead"], page["pending"]),
                         ({"tagId": "1", "frameIndex": 0}, {"tagId": "1", "frameIndex": 0}, 0, "0", 2))
        self.assertFalse(page["exhausted"])
        source = self.directory / report["fileName"]
        source.write_bytes(bytes.fromhex(report["hex"]["window"]["hex"]))
        result = self.check(report, source)
        self.assertEqual(result["codecSearchRowsMatchingSource"], 64)
        # Tokens deliberately exceed both source size and Tag coordinates.
        self.assertGreater(int(page["cursor"]), source.stat().st_size)

    def test_v4_mp3_late_frame_indices_and_values_match_source(self):
        report = self.v4_report("mp3")
        data = bytearray(b"FLV\x01\x05\x00\x00\x00\x09\x00\x00\x00\x00")
        payload = bytearray(1 + 417 * 3000)
        payload[0] = 0x2f
        for i in range(3000):
            payload[1+i*417:5+i*417] = bytes.fromhex("fffb9064")
        payload[1+2600*417+3] = 0xe4
        for p in (payload, bytes.fromhex("2f00000000")):
            header = bytearray(11)
            header[0], header[1:4] = 8, len(p).to_bytes(3, "big")
            data.extend(header + p + (11+len(p)).to_bytes(4, "big"))
        source = self.directory / report["fileName"]
        source.write_bytes(data)
        result = self.check(report, source)
        self.assertEqual(result["codecSearchRowsMatchingSource"], 3)
        for name, mutate in {
                "index": lambda r: r["codecSearchPage"]["page"]["rows"][0].update(frameIndex=2599),
                "source_value": lambda r: r["codecSearchPage"]["page"]["rows"][0]["values"][0].update(value="mpeg2"),
                "frame_span": lambda r: r["codecSearchPage"]["page"]["rows"][0]["span"].update(bitLength=32),
        }.items():
            changed = copy.deepcopy(report)
            mutate(changed)
            with self.subTest(name=name), self.assertRaises((ValueError, ValidationError)):
                self.check(changed, source)

    def test_v4_codec_negative_identity_coverage_filters_and_source_spans(self):
        for kind, mutations in {
                "sei": {
                    "request": lambda r: r["codecSearchPage"].update(requestId="1"),
                    "cursor": lambda r: r["codecSearchPage"]["page"].update(cursor="1"),
                    "next_cursor": lambda r: r["codecSearchPage"]["page"].update(nextCursor="2"),
                    "no_pending": lambda r: r["codecSearchPage"]["page"].update(pending=0),
                    "invented_complete": lambda r: r["codecSearchPage"]["page"].update(exhausted=True),
                    "scan_count": lambda r: r["codecSearchPage"]["page"].update(scannedTags=2),
                    "filter": lambda r: r["codecSearchPage"]["page"].update(filter="pictureTiming"),
                    "bad_type": lambda r: r["codecSearchPage"]["page"]["rows"][0]["values"][0].update(value="0"),
                    "out_of_source": lambda r: r["codecSearchPage"]["page"]["rows"][0]["span"].update(byteOffset="9999999"),
                    "duplicate": lambda r: r["codecSearchPage"]["page"]["rows"].__setitem__(1, copy.deepcopy(r["codecSearchPage"]["page"]["rows"][0])),
                    "false_context": lambda r: r["codecSearchPage"]["page"]["rows"][0].update(contextState="resolved"),
                    "missing_span": lambda r: r["codecSearchPage"]["page"]["rows"][0].update(span=None),
                    "missing_type": lambda r: r["codecSearchPage"]["page"]["rows"][0].update(values=[]),
                    "row_budget": lambda r: r["codecSearchPage"]["page"]["rows"].append(copy.deepcopy(r["codecSearchPage"]["page"]["rows"][0])),
                },
                "mp3": {
                    "frame_budget": lambda r: r["codecSearchPage"]["page"].update(scannedFrames=1025),
                    "read_budget": lambda r: r["codecSearchPage"]["page"].update(bytesRead="65537"),
                    "pending": lambda r: r["codecSearchPage"]["page"].update(pending=1),
                    "no_change": lambda r: r["codecSearchPage"]["page"]["rows"][0].update(kind="bitrate"),
                    "missing_previous": lambda r: r["codecSearchPage"]["page"]["rows"][0]["values"].pop(),
                    "forged_status": lambda r: r["codecSearchPage"]["page"]["rows"][2].update(status="unknown"),
                    "duplicate_value": lambda r: r["codecSearchPage"]["page"]["rows"][0]["values"].__setitem__(1, copy.deepcopy(r["codecSearchPage"]["page"]["rows"][0]["values"][0])),
                },
        }.items():
            for name, mutate in mutations.items():
                report = self.v4_report(kind)
                mutate(report)
                self.report_path.write_text(json.dumps(report))
                with self.subTest(kind=kind, name=name), self.assertRaises((ValueError, ValidationError)):
                    checker.check_snapshot(self.report_path)

    def test_v4_search_source_tag_and_sps_spans_cannot_be_reassigned(self):
        report = self.v4_report()
        source = self.directory / report["fileName"]
        source.write_bytes(bytes.fromhex(report["hex"]["window"]["hex"]))
        for key, value in (("offset", "14"), ("dts", "1")):
            changed = copy.deepcopy(report)
            changed["codecSearchPage"]["page"]["rows"][0][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.check(changed, source)
        changed = copy.deepcopy(report)
        changed["codecSearchPage"]["page"]["rows"][0]["span"]["byteOffset"] = "24"
        with self.assertRaises(ValueError):
            self.check(changed, source)

    def test_v4_legacy_schemas_reject_new_selection_and_scope(self):
        for version in (1, 2, 3):
            report = self.v4_report()
            report["schemaVersion"] = version
            report["selection"]["schemaVersion"] = version
            self.report_path.write_text(json.dumps(report))
            with self.subTest(version=version), self.assertRaises(ValidationError):
                checker.check_snapshot(self.report_path)

    def test_v3_unknown_null_declaration_is_a_valid_bounded_observation(self):
        report = self.v3_report()
        row = report["avcSearchPage"]["page"]["rows"][0]
        row.update(declaredFrameType=None, observedIDR="unknown", declarationRelation="undetermined", reason="filtered", span=None)
        report["avcSearchPage"]["page"]["filter"] = "all"
        report["selection"]["avcSearch"]["filter"] = "all"
        checker.check_avc_search_page(report)

    def test_v3_search_rejects_forged_identity_bounds_filters_and_spans(self):
        mutations = {
            "session": lambda r: r["avcSearchPage"].update(sessionId="other"),
            "request": lambda r: r["avcSearchPage"].update(requestId="1"),
            "start": lambda r: r["avcSearchPage"]["page"].update(start="1"),
            "total": lambda r: r["avcSearchPage"]["page"].update(total="900"),
            "scanned": lambda r: r["avcSearchPage"]["page"].update(scanned=0),
            "read_budget": lambda r: r["avcSearchPage"]["page"].update(bytesRead="1048577"),
            "filter": lambda r: r["selection"]["avcSearch"].update(filter="unknown"),
            "row_order": lambda r: r["avcSearchPage"]["page"]["rows"].append(r["avcSearchPage"]["page"]["rows"][0]),
            "relation": lambda r: r["avcSearchPage"]["page"]["rows"][0].update(declarationRelation="undetermined"),
            "exhausted": lambda r: r["avcSearchPage"]["page"].update(exhausted=False),
            "omission": lambda r: r.update(avcSearchPage=None),
        }
        for name, mutate in mutations.items():
            report = self.v3_report()
            mutate(report)
            self.report_path.write_text(json.dumps(report))
            with self.subTest(name=name), self.assertRaises((ValueError, ValidationError)):
                checker.check_snapshot(self.report_path)

    def test_v2_gop_extrema_must_occur_in_the_retained_intervals(self):
        report = self.v2_report()
        gop = report["mediaCurvePage"]["page"]["points"][0]["gop"]
        gop.update(declaredCount="1", closedCount="1", totalDurationMs="100", minDurationMs="100",
                   maxDurationMs="100", averageDurationMs="100", tagId="1")
        checker.check_curve_page(report)
        for change in ({"minDurationMs": "1", "maxDurationMs": "200"}, {"declaredCount": "0"}):
            altered = copy.deepcopy(report)
            altered["mediaCurvePage"]["page"]["points"][0]["gop"].update(change)
            with self.subTest(change=change), self.assertRaises(ValueError):
                checker.check_curve_page(altered)

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
