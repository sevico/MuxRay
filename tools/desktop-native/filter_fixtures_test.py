"""Verify binary fixture framing independently of the generator metadata."""

import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import filter_fixtures as fixtures


def inspect(data):
    offset, records = 13, []
    while offset + 11 <= len(data):
        size = int.from_bytes(data[offset + 1:offset + 4], "big")
        available = min(size, len(data) - offset - 11)
        next_offset = offset + 11 + size
        present = next_offset + 4 <= len(data)
        previous = int.from_bytes(data[next_offset:next_offset + 4], "big") if present else None
        records.append({"id": len(records), "offset": offset, "typeCode": data[offset] & 31,
                        "dts": int.from_bytes(data[offset + 4:offset + 7], "big") | data[offset + 7] << 24,
                        "dataSize": size, "availableDataSize": available, "payloadComplete": size == available,
                        "previousPresent": present, "previousValid": present and previous == 11 + size})
        offset = next_offset + 4
    return records


def matches(row, query):
    name = {8: "audio", 9: "video", 18: "script"}.get(row["typeCode"], "other")
    return ((query["type"] == "all" or name == query["type"])
            and (query["dtsMin"] is None or row["dts"] >= int(query["dtsMin"]))
            and (query["dtsMax"] is None or row["dts"] <= int(query["dtsMax"]))
            and {"all": True, "payload_incomplete": not row["payloadComplete"],
                 "previous_size_missing": not row["previousPresent"],
                 "previous_size_invalid": row["previousPresent"] and not row["previousValid"]}[query["structure"]])


class FilterFixtureTests(unittest.TestCase):
    def test_binary_records_match_declared_facts(self):
        for name, fixture in fixtures.fixtures().items():
            with self.subTest(name=name):
                self.assertEqual(inspect(fixture.data), fixture.tags)

    def test_query_expectations_match_binary_facts(self):
        for name, fixture in fixtures.fixtures().items():
            rows = inspect(fixture.data)
            for query in fixture.queries:
                with self.subTest(file=name, query=query["name"]):
                    self.assertEqual([r["id"] for r in rows if matches(r, query["filter"])], query["ids"])
                    for page in query.get("pages", []):
                        start, end = int(page["start"]), int(page["next"])
                        self.assertEqual(end - start, page["scanned"])
                        self.assertLessEqual(page["scanned"], 4096)
                        self.assertEqual([r["id"] for r in rows[start:end] if matches(r, query["filter"])], page["ids"])
                        self.assertLessEqual(len(page["ids"]), 100)
                        self.assertEqual(page["exhausted"], end == len(rows))

    def test_duplicate_regressing_dts_and_structural_distinction(self):
        files = fixtures.fixtures()
        rows = inspect(files["filter-ranges.flv"].data)
        self.assertEqual([r["dts"] for r in rows[:4]], [10000, 10000, 12000, 11000])
        self.assertEqual(rows[9]["dts"], 4294967295)
        rows = inspect(files["filter-structures.flv"].data)
        self.assertEqual([r["id"] for r in rows if not r["previousValid"]], [1, 3, 6])
        self.assertTrue(rows[6]["payloadComplete"])
        self.assertFalse(rows[6]["previousPresent"])
        rows = inspect(files["filter-truncated.flv"].data)
        self.assertEqual((rows[-1]["dataSize"], rows[-1]["availableDataSize"]), (10, 2))

    def test_summary_fixture_is_reused_byte_for_byte(self):
        original, _, _ = fixtures.summary_fixtures()["summary-mixed.flv"]
        self.assertEqual(bytes(fixtures.fixtures()["summary-mixed.flv"].data), original)

    def test_output_identity_and_refusal_to_overwrite(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "filter"
            manifest = fixtures.write_fixtures(output)
            self.assertEqual(json.loads((output / "fixtures.json").read_text()), manifest)
            for record in manifest["files"]:
                data = (output / record["name"]).read_bytes()
                self.assertEqual((len(data), hashlib.sha256(data).hexdigest()), (record["bytes"], record["sha256"]))
            with self.assertRaises(FileExistsError):
                fixtures.write_fixtures(output)


if __name__ == "__main__":
    unittest.main()
