"""Evidence-integrity tests for the archived Document scan comparison runner."""

from pathlib import Path
import tempfile
import unittest
import zipfile

import scan_compare as compare


def runs(pairs=5):
    facts = dict(tagCount=10, sourceSize=1000, bytesScanned=1000, complete=True,
                 hasErrors=False, diagnosticCount=0, recordsAndDiagnosticsSHA256="same")
    result = []
    for pair, label in compare.pair_order(pairs):
        result.append(dict(phase="measured", pair=pair, label=label,
                           facts=dict(facts, mode="open", scanNanoseconds=1_000_000 if label == "baseline" else 2_000_000)))
    for label in ("baseline", "candidate"):
        result.append(dict(phase="counted", pair=0, label=label,
                           facts=dict(facts, mode="counted", scanNanoseconds=999_000_000,
                                      readCalls=30 if label == "baseline" else 34,
                                      readRequestedBytes=100 if label == "baseline" else 108,
                                      readReturnedBytes=100 if label == "baseline" else 108)))
    return result


class ScanComparisonTests(unittest.TestCase):
    def test_counted_measurements_cannot_pollute_timing(self):
        summary = compare.summarize(runs(), 5)
        self.assertEqual(summary["scanMilliseconds"]["baseline"]["median"], 1)
        self.assertEqual(summary["scanMilliseconds"]["candidate"]["max"], 2)
        self.assertEqual(summary["pairedRatio"]["median"], 2)
        self.assertEqual(summary["readIncrease"], dict(readCalls=4, readRequestedBytes=8, readReturnedBytes=8))

    def test_alternation_and_complete_pair_coverage_required(self):
        sequence = compare.pair_order(5)
        self.assertEqual(sequence[:4], [(1, "baseline"), (1, "candidate"), (2, "candidate"), (2, "baseline")])
        data = runs()
        for changed in (data[1:], data[:1] + data, [data[1], data[0]] + data[2:]):
            with self.assertRaisesRegex(ValueError, "coverage"):
                compare.summarize(changed, 5)
        for count in (0, 4, 31):
            with self.assertRaises(ValueError):
                compare.pair_order(count)

    def test_result_or_record_digest_mismatch_refuses_summary(self):
        for field, value in (("tagCount", 9), ("recordsAndDiagnosticsSHA256", "different"), ("diagnosticCount", 1)):
            data = runs()
            data[2]["facts"][field] = value
            with self.assertRaisesRegex(ValueError, "differ"):
                compare.summarize(data, 5)

    def test_incomplete_scan_and_wrong_mode_refused(self):
        data = runs()
        for run in data:
            run["facts"]["complete"] = False
        with self.assertRaisesRegex(ValueError, "clean scan"):
            compare.summarize(data, 5)
        data = runs()
        data[0]["facts"]["mode"] = "counted"
        with self.assertRaisesRegex(ValueError, "mode"):
            compare.summarize(data, 5)
        data = runs()
        data.pop()
        with self.assertRaisesRegex(ValueError, "counted run"):
            compare.summarize(data, 5)

    def test_percentile_convention_and_bad_duration(self):
        value = compare.distribution(list(range(1, 11)))
        self.assertEqual(value["median"], 5.5)
        self.assertEqual(value["p95NearestRank"], 10)
        for invalid in ([], [0], [-1], [float("nan")], [float("inf")]):
            with self.assertRaises(ValueError):
                compare.distribution(invalid)

    def test_identity_detects_same_length_change(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "source.flv"
            path.write_bytes(b"first")
            expected = compare.identity(path)
            compare.verify_identity(expected)
            path.write_bytes(b"other")
            with self.assertRaisesRegex(ValueError, "changed"):
                compare.verify_identity(expected)

    def test_new_evidence_is_never_overwritten(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "result.json"
            compare.write_json(path, {"a": 1})
            before = path.read_bytes()
            with self.assertRaises(FileExistsError):
                compare.write_json(path, {"a": 2})
            self.assertEqual(path.read_bytes(), before)

    def test_archive_rejects_traversal_and_links(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for number, name in enumerate(("../escaped.txt", "link")):
                archive = root / f"bad-{number}.zip"
                with zipfile.ZipFile(archive, "w") as output:
                    item = zipfile.ZipInfo(name)
                    if name == "link":
                        item.external_attr = 0o120777 << 16
                    output.writestr(item, b"outside")
                with self.assertRaisesRegex(ValueError, "Unsafe"):
                    compare.extract_archive(archive, root / f"extract-{number}")
            self.assertFalse((root / "escaped.txt").exists())


if __name__ == "__main__":
    unittest.main()
