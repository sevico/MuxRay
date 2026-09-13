package analysis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

func TestAnalyzeV2ProgressUsesCompletedSharedIndexVisits(t *testing.T) {
	tags := make([]analysisFixtureTag, 2050)
	for i := range tags {
		tags[i] = mp3Fixture(uint32(i*26), 0xff, 0xfb, 0x90, 0x64)
	}
	data, _ := scanAnalysisFixture(t, tags...)
	scan, err := flv.ScanContext(t.Context(), bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || scan.Tags != nil {
		t.Fatalf("shared index scan: %v", err)
	}
	index := scan.Index()
	options := OptionsV2{MaxMP3HeaderEvents: 1}
	want, err := AnalyzeV2(t.Context(), bytes.NewReader(data), scan, options)
	if err != nil {
		t.Fatal(err)
	}
	var updates []Progress
	got, err := AnalyzeV2WithProgress(t.Context(), bytes.NewReader(data), scan, options, func(p Progress) { updates = append(updates, p) })
	if err != nil || !reflect.DeepEqual(got, want) || scan.Index() != index || scan.Tags != nil {
		t.Fatalf("progress changed analysis or index ownership: %v", err)
	}
	if expected := []Progress{{0, 2050}, {1024, 2050}, {2048, 2050}, {2050, 2050}}; !reflect.DeepEqual(updates, expected) {
		t.Fatalf("updates: %+v", updates)
	}
}

type progressErrorReader struct {
	io.ReaderAt
	offset int64
	err    error
}

func (r progressErrorReader) ReadAt(p []byte, offset int64) (int, error) {
	if offset == r.offset {
		return 0, r.err
	}
	return r.ReaderAt.ReadAt(p, offset)
}

func TestAnalyzeV2ProgressExcludesFailedTag(t *testing.T) {
	data, scan := scanAnalysisFixture(t, mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64), mp3Fixture(26, 0xff, 0xfb, 0x90, 0x64))
	second, _ := scan.Index().Get(1)
	failure := errors.New("injected MPEG-header failure")
	r := progressErrorReader{bytes.NewReader(data), int64(second.PayloadSpan().ByteOffset + 1), failure}
	var updates []Progress
	got, err := AnalyzeV2WithProgress(t.Context(), r, scan, OptionsV2{}, func(p Progress) { updates = append(updates, p) })
	if !errors.Is(err, failure) || got.Complete || got.Tags.Total != 2 || got.MP3.Headers.EventCount != 1 {
		t.Fatalf("expected a failed half-visited Tag: %+v %v", got, err)
	}
	if !reflect.DeepEqual(updates, []Progress{{0, 2}, {1, 2}}) {
		t.Fatalf("failed Tag counted as processed: %+v", updates)
	}
}

func TestAnalyzeV2ProgressCancellationAndEmptyIndex(t *testing.T) {
	data, scan := scanAnalysisFixture(t, mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := &mp3SummaryCancelReader{source: bytes.NewReader(data), cancel: cancel}
	var updates []Progress
	got, err := AnalyzeV2WithProgress(ctx, cursor.WithContext(ctx, r), scan, OptionsV2{}, func(p Progress) { updates = append(updates, p) })
	if !errors.Is(err, context.Canceled) || got.Complete || !reflect.DeepEqual(updates, []Progress{{0, 1}}) {
		t.Fatalf("cancel during first read published progress: %+v %v", updates, err)
	}
	updates = nil
	_, err = AnalyzeV2WithProgress(nil, bytes.NewReader(nil), flv.ScanResult{}, OptionsV2{}, func(p Progress) { updates = append(updates, p) })
	if err != nil || !reflect.DeepEqual(updates, []Progress{{0, 0}}) {
		t.Fatalf("empty progress: %+v %v", updates, err)
	}
}
