package analysis

import (
	"bytes"
	"context"
	"reflect"
	"testing"
)

func FuzzAnalyzeTag(f *testing.F) {
	for _, seed := range []struct {
		rawType byte
		dts     uint32
		payload []byte
	}{
		{rawType: 8, payload: nil},
		{rawType: 8, dts: 100, payload: []byte{0xaf, 0x00, 0x12, 0x10}},
		{rawType: 8, dts: 200, payload: []byte{0xaf, 0x01, 0xde, 0xad}},
		{rawType: 9, payload: []byte{0x17, 0, 0, 0, 0, 1, 0x42}},
		{rawType: 9, dts: 300, payload: []byte{0x17, 1, 0xff, 0xff, 0xfe}},
		{rawType: 0x29, payload: []byte{0x17, 1, 0, 0, 0}},
		{rawType: 18, payload: []byte{5}},
		{rawType: 7, payload: []byte{1, 2, 3}},
	} {
		f.Add(seed.rawType, seed.dts, seed.payload)
	}

	f.Fuzz(func(t *testing.T, rawType byte, dts uint32, payload []byte) {
		if len(payload) > 4_096 {
			t.Skip()
		}
		data, scan := scanAnalysisFixture(t, analysisFixtureTag{rawTypeFlags: rawType, dts: dts, payload: payload})
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{MaxDiagnostics: 8})
		if err != nil {
			t.Fatalf("Analyze returned an in-memory source error: %v", err)
		}
		if got.Tags.Total != 1 {
			t.Fatalf("tag count = %d, want 1", got.Tags.Total)
		}
		repeated, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{MaxDiagnostics: 8})
		if err != nil {
			t.Fatalf("repeat Analyze: %v", err)
		}
		if !reflect.DeepEqual(got, repeated) {
			t.Fatalf("non-deterministic result\nfirst=%+v\nrepeat=%+v", got, repeated)
		}
		for _, diagnostic := range got.Diagnostics {
			if diagnostic.TagID != nil && *diagnostic.TagID != 0 {
				t.Fatalf("diagnostic has invalid Tag ID: %+v", diagnostic)
			}
		}
	})
}
