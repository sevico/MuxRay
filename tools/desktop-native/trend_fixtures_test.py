"""Independently verify trend fixture bytes, lookback boundaries, and output identity."""

import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import trend_fixtures as fixtures


def inspect(data):
    offset, records = 13, []
    while offset + 11 <= len(data):
        size = int.from_bytes(data[offset + 1:offset + 4], "big")
        available = min(size, len(data) - offset - 11)
        after = offset + 11 + size
        state = "missing" if after + 4 > len(data) else "valid" if int.from_bytes(data[after:after + 4], "big") == 11 + size else "invalid"
        records.append({"tagId": str(len(records)), "offset": str(offset),
                        "typeCode": data[offset] & 31, "streamId": int.from_bytes(data[offset + 8:offset + 11], "big"),
                        "dts": str(int.from_bytes(data[offset + 4:offset + 7], "big") | data[offset + 7] << 24),
                        "dataSize": size, "availableDataSize": available,
                        "payloadComplete": size == available, "filtered": bool(data[offset] & 32),
                        "previousTagSizeState": state})
        offset = after + 4
    return records


class TrendFixtureTests(unittest.TestCase):
    def test_all_binary_records_match_manifest(self):
        for name, fixture in fixtures.fixtures().items():
            with self.subTest(file=name):
                self.assertEqual(inspect(fixture.data), fixture.tags)

    def test_windows_match_independent_nearest_predecessor_search(self):
        for name, fixture in fixtures.fixtures().items():
            rows = inspect(fixture.data)
            for window in fixture.windows:
                with self.subTest(file=name, window=window["name"]):
                    request, page = window["request"], window["expected"]
                    start, context_start = int(request["start"]), int(page["contextStart"])
                    end = min(len(rows), start + request["count"])
                    self.assertEqual(page["bounds"], {"start": str(start), "next": str(end), "total": str(len(rows)), "limit": request["count"]})
                    self.assertEqual(page["contextScanned"], start - context_start)
                    self.assertLessEqual(page["contextScanned"], 4096)
                    self.assertEqual(len(page["points"]), end - start)
                    # Search every prior row in the actual bounded source region,
                    # independently of the generator's reverse traversal/cache.
                    for index, point in enumerate(page["points"], start):
                        row = rows[index]
                        candidates = [r for r in rows[context_start:index]
                                      if (r["typeCode"], r["streamId"]) == (row["typeCode"], row["streamId"])]
                        previous = candidates[-1] if candidates else None
                        expected = {**row, "previousTagId": previous["tagId"] if previous else None,
                                    "previousDts": previous["dts"] if previous else None,
                                    "deltaMs": str(int(row["dts"]) - int(previous["dts"])) if previous else None,
                                    "context": "found" if previous else "start" if context_start == 0 else "budget_exhausted"}
                        self.assertEqual(point, expected)

    def test_known_page_boundary_stream_and_dts_facts(self):
        page = fixtures.fixtures()["trends-mixed.flv"].windows[1]["expected"]
        self.assertEqual((page["contextStart"], page["contextScanned"], len(page["points"])), ("0", 256, 44))
        points = {p["tagId"]: p for p in page["points"]}
        for tag_id, previous, delta in [("256", "254", "0"), ("257", "255", "-100"),
                                         ("258", "256", "2001"), ("260", "258", "4294960294"),
                                         ("261", "259", "-40"), ("264", "260", "-4294967295")]:
            self.assertEqual((points[tag_id]["previousTagId"], points[tag_id]["deltaMs"]), (previous, delta))
        self.assertEqual((points["259"]["context"], points["259"]["streamId"]), ("start", 1))
        self.assertTrue(points["262"]["filtered"])
        short = fixtures.fixtures()["trends-mixed.flv"].windows[2]["expected"]
        self.assertEqual((short["contextStart"], short["contextScanned"]), ("11", 9))

    def test_budget_boundary_and_in_window_recovery(self):
        fixture = fixtures.fixtures()["trends-sparse.flv"]
        page = fixture.windows[0]["expected"]
        self.assertEqual((page["contextStart"], page["contextScanned"]), ("1", 4096))
        points = {p["tagId"]: p for p in page["points"]}
        self.assertEqual((points["4097"]["previousTagId"], points["4097"]["deltaMs"]), ("1", "100"))
        self.assertEqual(points["4098"]["context"], "budget_exhausted")
        self.assertIsNone(points["4098"]["deltaMs"])
        self.assertEqual((points["4099"]["previousTagId"], points["4099"]["deltaMs"]), ("4098", "10"))
        self.assertEqual((points["4100"]["previousTagId"], points["4100"]["deltaMs"]), ("4097", "-10"))
        self.assertEqual(points["4101"]["context"], "budget_exhausted")
        for window in fixture.windows[1:3]:
            self.assertEqual(window["expected"]["contextScanned"], 4096)

    def test_codec_prefix_structures_and_empty_eof(self):
        files = fixtures.fixtures()
        original, _, _ = fixtures.summary_fixtures()["summary-mixed.flv"]
        self.assertEqual(bytes(files["trends-mixed.flv"].data[:len(original)]), original)
        structural = inspect(files["trends-structures.flv"].data)
        self.assertEqual([p["tagId"] for p in structural if p["previousTagSizeState"] == "invalid"], ["1", "3"])
        self.assertEqual([p["tagId"] for p in structural if p["previousTagSizeState"] == "missing"], ["6"])
        self.assertEqual(structural[4]["streamId"], 16777215)
        partial = inspect(files["trends-truncated.flv"].data)[1]
        self.assertEqual((partial["dataSize"], partial["availableDataSize"], partial["previousTagSizeState"]), (10, 2, "missing"))
        for fixture in files.values():
            page = fixtures.trend_page(fixture.tags, len(fixture.tags), 256)
            self.assertEqual((page["contextStart"], page["contextScanned"], page["points"]), (str(len(fixture.tags)), 0, []))

    def test_invalid_windows_and_refusal_to_overwrite(self):
        for start, count in [(-1, 1), (1, 1), (0, 0), (0, 513)]:
            with self.assertRaises(ValueError):
                fixtures.trend_page([], start, count)
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "trends"
            manifest = fixtures.write_fixtures(output)
            self.assertEqual(json.loads((output / "fixtures.json").read_text()), manifest)
            for record in manifest["files"]:
                data = (output / record["name"]).read_bytes()
                self.assertEqual((len(data), hashlib.sha256(data).hexdigest()), (record["bytes"], record["sha256"]))
            with self.assertRaises(FileExistsError):
                fixtures.write_fixtures(output)


if __name__ == "__main__":
    unittest.main()
