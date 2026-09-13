"""In-memory regression checks for Hex fixture framing and navigation targets."""

import unittest

import hex_fixtures as fixture


class HexFixtureTests(unittest.TestCase):
    def setUp(self):
        self.files = fixture.fixtures()
        self.data, self.tags = self.files["hex-details.flv"]

    def test_framing_and_second_page_identity(self):
        self.assertEqual(len(self.tags), 106)
        self.assertEqual(self.data[:13], bytes.fromhex("464c5601050000000900000000"))
        offset = 13
        for tag_id, tag in enumerate(self.tags):
            self.assertEqual(tag["id"], tag_id)
            self.assertEqual(tag["offset"], offset)
            size = int.from_bytes(self.data[offset + 1:offset + 4], "big")
            self.assertEqual(size, tag["payloadBytes"])
            self.assertEqual(self.data[offset], tag["tagType"])
            self.assertEqual(tag["payloadOffset"], offset + 11)
            self.assertEqual(int.from_bytes(self.data[offset + 11 + size:offset + 15 + size], "big"), 11 + size)
            offset += 15 + size
        self.assertEqual(offset, len(self.data))
        self.assertGreater(len(self.data), 4096)
        self.assertEqual(self.tags[105]["name"], "second-page-mp3")
        self.assertGreater(self.tags[105]["offset"], 4096)
        self.assertEqual(self.data[self.tags[105]["payloadOffset"]:-4], bytes.fromhex("2ffffb92c0"))

    def test_encoded_link_bits_and_derived_positions(self):
        def bits(tag_id, name):
            span = self.tags[tag_id]["links"][name]
            start = int(span["byteOffset"]) * 8 + span["bitOffset"]
            value = 0
            for index in range(start, start + span["bitLength"]):
                value = value * 2 + ((self.data[index // 8] >> (7 - index % 8)) & 1)
            return value

        self.assertEqual(bits(1, "initialAudioObjectType"), 2)
        self.assertEqual(bits(1, "samplingFrequencyIndex"), 4)
        self.assertEqual(bits(2, "lengthSizeMinusOne"), 3)
        self.assertEqual(bits(3, "syncWord"), 0x7ff)
        self.assertEqual(bits(4, "nalUnitType"), 5)
        self.assertEqual(bits(105, "padding"), 1)
        self.assertIsNone(self.tags[2]["links"]["SPS summary"])
        self.assertIsNone(self.tags[3]["links"]["bitrateKbps"])
        self.assertIsNone(self.tags[105]["links"]["frameLengthBytes"])

    def test_boundary_string_unknown_bytes_and_diagnostic(self):
        span = self.tags[0]["links"]["value"]
        self.assertEqual(self.tags[0]["linkParentNames"]["value"], "boundary")
        offset = int(span["byteOffset"])
        length = span["bitLength"] // 8
        self.assertEqual((offset, length), (51, 1024))
        self.assertEqual(self.data[offset:offset + length], b"0123456789ABCDEF" * 64)
        self.assertLess(offset, 512)
        self.assertGreater(offset + length, 1024)
        unknown = self.tags[5]
        self.assertEqual(self.data[unknown["payloadOffset"]:unknown["payloadOffset"] + unknown["payloadBytes"]], bytes(range(256)) * 8)
        diagnostic = self.tags[6]["expectedDiagnostic"]
        # The parser reports the first unavailable ASC bit, not the beginning
        # of the partially available field; FLV PreviousTagSize follows here.
        self.assertEqual(diagnostic["offset"], self.tags[6]["payloadOffset"] + self.tags[6]["payloadBytes"])
        self.assertEqual(diagnostic["offset"], 3282)
        self.assertEqual(diagnostic["bitOffset"], 0)

    def test_raw_windows_and_incomplete_container(self):
        self.assertEqual(self.files["hex-change.flv"], self.files["hex-details.flv"])
        partial, records = self.files["hex-truncated.flv"]
        self.assertEqual(partial[:len(self.data)], self.data)
        self.assertEqual(len(records), 107)
        self.assertEqual(records[-1]["offset"], len(self.data))
        self.assertEqual(partial[records[-1]["payloadOffset"]:], bytes.fromhex("af00"))
        self.assertEqual(fixture.window(partial, records[-1]["payloadOffset"], 512)["hex"], "af00")
        for offset in (0, 512, 1024, self.tags[105]["offset"], len(self.data) - 5, len(self.data)):
            result = fixture.window(self.data, offset, 512)
            actual = bytes.fromhex(result["hex"])
            self.assertEqual(actual, self.data[offset:offset + 512])
            self.assertEqual(len(actual), result["returnedBytes"])
            self.assertEqual(result["eof"], offset + len(actual) == len(self.data))
        self.assertEqual(fixture.window(self.data, 0, 4096)["returnedBytes"], 4096)
        self.assertEqual(fixture.window(self.data, len(self.data), 512)["returnedBytes"], 0)


if __name__ == "__main__":
    unittest.main()
