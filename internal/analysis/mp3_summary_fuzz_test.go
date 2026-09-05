package analysis

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func FuzzAnalyzeV2(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		{0x2f, 0xff, 0xfb, 0x90, 0x64, 0x2f, 0xff, 0xfb, 0xa0, 0x64},
		{0x2f, 0xff, 0xe3, 0x08, 0xc4, 0x2f, 0xff},
		{0xaf, 1, 0, 0, 0, 0x2f, 0xff, 0xf3, 0x80, 0xc4, 0xaf, 1, 0, 0, 0},
		{0xef, 0, 0, 0, 0, 0x2f, 0, 0, 0, 0},
	} {
		f.Add(uint32(0), seed)
	}
	f.Fuzz(func(t *testing.T, dts uint32, payloads []byte) {
		if len(payloads) > 160 {
			t.Skip()
		}
		var tags []analysisFixtureTag
		for start := 0; start < len(payloads); start += 5 {
			end := start + 5
			if end > len(payloads) {
				end = len(payloads)
			}
			// Alternating delta directions exercise regression/duplicate epochs.
			stamp := dts + uint32(start%15)
			tags = append(tags, analysisFixtureTag{rawTypeFlags: 8, dts: stamp, payload: payloads[start:end]})
		}
		if len(tags) == 0 {
			tags = append(tags, analysisFixtureTag{rawTypeFlags: 8})
		}
		data, scan := scanAnalysisFixture(t, tags...)
		options := OptionsV2{Options: Options{MaxDiagnostics: 8}, MaxMP3HeaderEvents: 2}
		got, err := AnalyzeV2(context.Background(), bytes.NewReader(data), scan, options)
		if err != nil {
			t.Fatalf("in-memory analysis failed: %v", err)
		}
		repeat, err := AnalyzeV2(context.Background(), bytes.NewReader(data), scan, options)
		if err != nil || !reflect.DeepEqual(got, repeat) {
			t.Fatalf("non-deterministic v2 result: %v", err)
		}
		c, h, track := got.AudioCodecs, got.MP3.Headers, got.MP3.Track
		if c.AACTagCount+c.MP3TagCount+c.OtherTagCount+c.UnknownTagCount != got.Audio.TagCount ||
			c.MP3TagCount != track.TagCount || h.EventCount != track.TagCount || h.ValidCount+h.MalformedCount != h.EventCount ||
			h.ValidCount != track.MediaTagCount || h.MalformedCount != track.UnclassifiedTagCount || h.FreeFormatCount > h.ValidCount ||
			track.MediaTagCount > got.Audio.MediaTagCount || track.MediaDataSizeBytes > got.Audio.MediaDataSizeBytes ||
			len(h.Events) > 2 || len(got.Diagnostics) > 8 || h.Truncated != (h.EventCount > uint64(len(h.Events))) {
			t.Fatalf("counter/retention invariant failed: %+v", got)
		}
		if got.Audio.TagCount != got.Audio.MediaTagCount+got.Audio.ExcludedTagCount+got.Audio.UnclassifiedTagCount ||
			track.Bitrate.Bytes > track.MediaDataSizeBytes || got.Audio.Bitrate.Bytes > got.Audio.MediaDataSizeBytes {
			t.Fatalf("classification/coverage invariant failed: %+v", got)
		}
		for _, diagnostic := range got.Diagnostics {
			if diagnostic.Offset > uint64(len(data)) || diagnostic.BitOffset > 7 ||
				(diagnostic.TagID != nil && *diagnostic.TagID >= uint64(len(tags))) ||
				(diagnostic.RecoveryOffset != nil && *diagnostic.RecoveryOffset > uint64(len(data))) {
				t.Fatalf("diagnostic outside input: %+v", diagnostic)
			}
		}
	})
}
