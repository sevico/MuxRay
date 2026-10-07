package analysis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

type curveRead struct {
	offset int64
	length int
}

type curveReader struct {
	io.ReaderAt
	reads []curveRead
}

func (r *curveReader) ReadAt(p []byte, offset int64) (int, error) {
	r.reads = append(r.reads, curveRead{offset, len(p)})
	return r.ReaderAt.ReadAt(p, offset)
}

// Compare the existing summary, progress and exact source read sequence, in
// addition to checking the new curves. A collector must not reparse payloads.
func analyzeCurveFixture(t *testing.T, data []byte, scan flv.ScanResult, options OptionsV2) (ResultV2, *MediaCurves) {
	t.Helper()
	oldReader, newReader := &curveReader{ReaderAt: bytes.NewReader(data)}, &curveReader{ReaderAt: bytes.NewReader(data)}
	var oldProgress, newProgress []Progress
	want, err := AnalyzeV2WithProgress(t.Context(), oldReader, scan, options, func(p Progress) { oldProgress = append(oldProgress, p) })
	if err != nil {
		t.Fatal(err)
	}
	got, curves, err := AnalyzeV2WithCurves(t.Context(), newReader, scan, options, func(p Progress) { newProgress = append(newProgress, p) })
	if err != nil || curves == nil {
		t.Fatalf("curves: %v", err)
	}
	if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(oldProgress, newProgress) || !reflect.DeepEqual(oldReader.reads, newReader.reads) {
		t.Fatal("curve collection changed summary, progress, or source reads")
	}
	assertCurveTotals(t, got, curves)
	return got, curves
}

func assertCurveTotals(t *testing.T, summary ResultV2, curves *MediaCurves) {
	t.Helper()
	if curves.IndexedTags != summary.Tags.Total || uint64(len(curves.Points)) != (curves.IndexedTags+CurveWindowTags-1)/CurveWindowTags {
		t.Fatalf("curve coverage: indexed=%d points=%d tags=%d", curves.IndexedTags, len(curves.Points), summary.Tags.Total)
	}
	var rates [3]curveRateTotals
	var declared, closed, duration, minimum, maximum, unclassified uint64
	for i, point := range curves.Points {
		if point.Index != uint64(i) || point.StartTag != uint64(i)*CurveWindowTags || point.NextTag != min(point.StartTag+CurveWindowTags, curves.IndexedTags) {
			t.Fatalf("window bounds: %+v", point)
		}
		for j, rate := range []MediaCurveRate{point.Audio, point.Video, point.MP3} {
			if rate.Present {
				if rate.CoveredIntervalMS == 0 || rate.TagID < point.StartTag || rate.TagID >= point.NextTag || rate.BitsPerSecond != rate.CoveredBytes*8000/rate.CoveredIntervalMS {
					t.Fatalf("invalid covered rate: %+v", rate)
				}
			} else if rate != (MediaCurveRate{}) {
				t.Fatalf("uncovered rate was not unknown: %+v", rate)
			}
			rates[j].bytes += rate.CoveredBytes
			rates[j].intervalMS += rate.CoveredIntervalMS
		}
		gop := point.GOP
		if gop.Present {
			if gop.ClosedCount == 0 || gop.TagID < point.StartTag || gop.TagID >= point.NextTag || gop.AverageDurationMS != gop.TotalDurationMS/gop.ClosedCount {
				t.Fatalf("invalid closed GOPs: %+v", gop)
			}
			if closed == 0 || gop.MinDurationMS < minimum {
				minimum = gop.MinDurationMS
			}
			maximum = max(maximum, gop.MaxDurationMS)
		} else if gop.ClosedCount != 0 || gop.TotalDurationMS != 0 || gop.MinDurationMS != 0 || gop.MaxDurationMS != 0 || gop.AverageDurationMS != 0 || gop.TagID != 0 {
			t.Fatalf("unclosed GOP gained a duration: %+v", gop)
		}
		declared += gop.DeclaredCount
		closed += gop.ClosedCount
		duration += gop.TotalDurationMS
		unclassified += point.UnclassifiedTags
	}
	for i, track := range []Track{summary.Audio, summary.Video, summary.MP3.Track} {
		if rates[i].bytes != track.Bitrate.Bytes || rates[i].intervalMS != track.Bitrate.CoveredIntervalMS {
			t.Fatalf("track %d coverage differs: %+v versus %+v", i, rates[i], track.Bitrate)
		}
	}
	if declared != summary.GOP.Count || closed != summary.GOP.ClosedDurationCount || minimum != summary.GOP.MinDurationMS || maximum != summary.GOP.MaxDurationMS ||
		closed != 0 && duration/closed != summary.GOP.AverageDurationMS || unclassified != summary.Audio.UnclassifiedTagCount+summary.Video.UnclassifiedTagCount {
		t.Fatal("curve GOP or unclassified aggregates differ from summary")
	}
}

func curvePadding(n int) []analysisFixtureTag {
	tags := make([]analysisFixtureTag, n)
	for i := range tags {
		tags[i].rawTypeFlags = byte(flv.TagTypeScript)
	}
	return tags
}

func curveAAC(dts uint32, payload ...byte) analysisFixtureTag {
	return analysisFixtureTag{rawTypeFlags: 8, dts: dts, payload: append([]byte{0xaf, 1}, payload...)}
}

func curveKeyframe(dts uint32) analysisFixtureTag {
	return analysisFixtureTag{rawTypeFlags: 9, dts: dts, payload: []byte{0x17, 1, 0, 0, 0}}
}

func TestMediaCurveWindowAndPartialIndexBounds(t *testing.T) {
	for _, count := range []int{0, 1, 255, 256, 257} {
		data, scan := scanAnalysisFixture(t, curvePadding(count)...)
		_, curves := analyzeCurveFixture(t, data, scan, OptionsV2{})
		for _, point := range curves.Points {
			if point.Audio.Present || point.Video.Present || point.MP3.Present || point.GOP.Present {
				t.Fatal("script-only window acquired media coverage")
			}
		}
	}
	data, _ := scanAnalysisFixture(t, curvePadding(300)...)
	scan, err := flv.ScanContext(t.Context(), bytes.NewReader(data), uint64(len(data)), flv.Options{MaxTags: 257})
	if err != nil || scan.Complete || scan.Index().Len() != 257 {
		t.Fatalf("partial fixture: %+v %v", scan, err)
	}
	got, curves := analyzeCurveFixture(t, data, scan, OptionsV2{})
	if got.Complete || len(curves.Points) != 2 || curves.Points[1].NextTag != 257 {
		t.Fatal("partial-index coverage was promoted to a full source")
	}
}

func TestMediaCurveDuplicateDTSAndRegressionAcrossWindow(t *testing.T) {
	tags := append(curvePadding(254), curveAAC(100, 1), curveAAC(100, 1, 2), curveAAC(100, 1, 2, 3), curveAAC(200, 1), curveAAC(50, 1), curveAAC(75, 1))
	data, scan := scanAnalysisFixture(t, tags...)
	_, curves := analyzeCurveFixture(t, data, scan, OptionsV2{})
	if curves.Points[0].Audio.Present {
		t.Fatal("unclosed duplicate-DTS group was committed at a window boundary")
	}
	want := MediaCurveRate{CoveredBytes: 15, CoveredIntervalMS: 125, BitsPerSecond: 960, Present: true, TagID: 257}
	if curves.Points[1].Audio != want {
		t.Fatalf("cross-window closing ownership: %+v, want %+v", curves.Points[1].Audio, want)
	}
}

func TestMediaCurveGOPMaximumRepresentativeAndTie(t *testing.T) {
	tags := append(curvePadding(254), curveKeyframe(0), curveKeyframe(10), curveKeyframe(40), curveKeyframe(70), curveKeyframe(75))
	data, scan := scanAnalysisFixture(t, tags...)
	_, curves := analyzeCurveFixture(t, data, scan, OptionsV2{Options: Options{MaxKeyframes: 1}})
	want := MediaCurveGOP{DeclaredCount: 3, ClosedCount: 3, TotalDurationMS: 65, MinDurationMS: 5, MaxDurationMS: 30, AverageDurationMS: 21, Present: true, TagID: 256}
	if curves.Points[1].GOP != want {
		t.Fatalf("maximum-GOP closing Tag and first tie: %+v, want %+v", curves.Points[1].GOP, want)
	}
}

func TestMediaCurvesOutliveRetainedEventPrefixes(t *testing.T) {
	tags := make([]analysisFixtureTag, 0, 2400)
	for i := uint32(0); i < 1200; i++ {
		tags = append(tags, curveKeyframe(i*40), mp3Fixture(i*26, 0xff, 0xfb, 0x90, 0x64))
	}
	data, scan := scanAnalysisFixture(t, tags...)
	got, curves := analyzeCurveFixture(t, data, scan, OptionsV2{Options: Options{MaxKeyframes: 1024}, MaxMP3HeaderEvents: 1})
	last := curves.Points[len(curves.Points)-1]
	if !got.Keyframes.Truncated || !got.MP3.Headers.Truncated || len(curves.Points) != 10 || !last.GOP.Present || !last.MP3.Present || last.GOP.TagID < 2304 || last.MP3.TagID < 2304 {
		t.Fatalf("retained event prefix truncated curves: %+v", last)
	}
	if got.GOP.ClosedDurationCount != 1199 || got.MP3.Track.Bitrate.CoveredIntervalMS != 1199*26 {
		t.Fatal("unexpected full-index aggregate")
	}
}

func TestMediaCurvesPreserveConfigurationAndCodecBarriers(t *testing.T) {
	config := func(level byte) analysisFixtureTag {
		return analysisFixtureTag{rawTypeFlags: 9, payload: []byte{0x17, 0, 0, 0, 0, 1, 0x42, 0, level, 0xff, 0xe0, 0}}
	}
	for _, tc := range []struct {
		name             string
		tags             []analysisFixtureTag
		options          OptionsV2
		audioBytes       uint64
		audioMS          uint64
		videoBytes       uint64
		videoMS          uint64
		closedGOPs       uint64
		unclassifiedTags uint64
	}{
		{name: "identical AVC config", tags: []analysisFixtureTag{config(30), curveKeyframe(0), config(30), curveKeyframe(10)}, videoBytes: 5, videoMS: 10, closedGOPs: 1},
		{name: "changed AVC config", tags: []analysisFixtureTag{config(30), curveKeyframe(0), config(31), curveKeyframe(10), curveKeyframe(20)}, videoBytes: 10, videoMS: 20, closedGOPs: 1},
		{name: "malformed then restored AVC config", tags: []analysisFixtureTag{config(30), curveKeyframe(0), {rawTypeFlags: 9, payload: []byte{0x17, 0, 0, 0, 0, 1}}, curveKeyframe(10), config(30), curveKeyframe(20), curveKeyframe(30)}, videoBytes: 15, videoMS: 30, closedGOPs: 1},
		{name: "AVC config budget", tags: []analysisFixtureTag{config(30), curveKeyframe(0), curveKeyframe(10), config(30), curveKeyframe(20), curveKeyframe(30)}, options: OptionsV2{Options: Options{MaxConfigurationBytes: 7}}, videoBytes: 15, videoMS: 30, closedGOPs: 2},
		{name: "AVC EOS", tags: []analysisFixtureTag{curveKeyframe(0), curveKeyframe(10), {rawTypeFlags: 9, payload: []byte{0x17, 2, 0, 0, 0}}, curveKeyframe(20), curveKeyframe(30)}, videoBytes: 15, videoMS: 30, closedGOPs: 2},
		{name: "GOP duplicate and regression", tags: []analysisFixtureTag{curveKeyframe(0), curveKeyframe(10), curveKeyframe(10), curveKeyframe(20), curveKeyframe(5), curveKeyframe(15)}, videoBytes: 20, videoMS: 30, closedGOPs: 3},
		{name: "AAC config does not reset rate", tags: []analysisFixtureTag{curveAAC(0, 1), {rawTypeFlags: 8, payload: []byte{0xaf, 0, 0x12, 0x10}}, curveAAC(20, 1), {rawTypeFlags: 8, payload: []byte{0xaf, 0, 0x11, 0x90}}, curveAAC(40, 1)}, audioBytes: 6, audioMS: 40},
		{name: "audio codec switch", tags: []analysisFixtureTag{curveAAC(0, 1), mp3Fixture(100, 0xff, 0xfb, 0x90, 0x64), mp3Fixture(120, 0xff, 0xfb, 0x90, 0x64), curveAAC(200, 1), curveAAC(220, 1)}, audioBytes: 8, audioMS: 40},
		{name: "MP3 parameters bitrate free-format malformed", tags: []analysisFixtureTag{mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64), mp3Fixture(26, 0xff, 0xfb, 0xa0, 0x64), mp3Fixture(52, 0xff, 0xfb, 0xa2, 0x64), mp3Fixture(78, 0xff, 0xf3, 0x80, 0xc4), mp3Fixture(104, 0xff, 0xf3, 0x80, 0xc4), mp3Fixture(130, 0xff, 0xf3, 0x00, 0xc4), mp3Fixture(156, 0xff, 0xf3, 0x8c, 0xc4), mp3Fixture(182, 0xff, 0xfb, 0x90, 0x64), mp3Fixture(208, 0xff, 0xfb, 0x90, 0x64)}, audioBytes: 25, audioMS: 130, unclassifiedTags: 1},
		{name: "unclassified tags", tags: []analysisFixtureTag{{rawTypeFlags: 8}, {rawTypeFlags: 8, payload: []byte{0xef}}, {rawTypeFlags: 0x28, payload: []byte{0xaf, 1}}, {rawTypeFlags: 9, payload: []byte{0x12}}}, unclassifiedTags: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, scan := scanAnalysisFixture(t, append(curvePadding(254), tc.tags...)...)
			got, _ := analyzeCurveFixture(t, data, scan, tc.options)
			if got.Audio.Bitrate.Bytes != tc.audioBytes || got.Audio.Bitrate.CoveredIntervalMS != tc.audioMS || got.Video.Bitrate.Bytes != tc.videoBytes || got.Video.Bitrate.CoveredIntervalMS != tc.videoMS || got.GOP.ClosedDurationCount != tc.closedGOPs || got.Audio.UnclassifiedTagCount+got.Video.UnclassifiedTagCount != tc.unclassifiedTags {
				t.Fatalf("unexpected fixture coverage: audio=%+v video=%+v GOP=%+v", got.Audio, got.Video, got.GOP)
			}
		})
	}
}

func TestMediaCurvesNeverPublishOnErrorsOrCancellation(t *testing.T) {
	data, scan := scanAnalysisFixture(t, mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64), mp3Fixture(26, 0xff, 0xfb, 0x90, 0x64))
	second, _ := scan.Index().Get(1)
	failure := errors.New("injected MPEG header failure")
	reader := progressErrorReader{bytes.NewReader(data), int64(second.PayloadSpan().ByteOffset + 1), failure}
	got, curves, err := AnalyzeV2WithCurves(t.Context(), reader, scan, OptionsV2{}, nil)
	if !errors.Is(err, failure) || curves != nil || got.Complete || got.Tags.Total != 2 {
		t.Fatalf("failed half-visited Tag published curves: %v %+v", err, curves)
	}
	if _, curves, err := AnalyzeV2WithCurves(t.Context(), nil, scan, OptionsV2{}, nil); err == nil || curves != nil {
		t.Fatal("nil source published curves")
	}
	if _, curves, err := AnalyzeV2WithCurves(t.Context(), bytes.NewReader(data), scan, OptionsV2{Options: Options{MaxKeyframes: HardMaxKeyframes + 1}}, nil); err == nil || curves != nil {
		t.Fatal("invalid options published curves")
	}
	data, scan = scanAnalysisFixture(t, curvePadding(2050)...)
	for _, stopAt := range []uint64{0, 1024, 2050} {
		ctx, cancel := context.WithCancel(t.Context())
		got, curves, err := AnalyzeV2WithCurves(ctx, bytes.NewReader(data), scan, OptionsV2{}, func(p Progress) {
			if p.Processed == stopAt {
				cancel()
			}
		})
		cancel()
		if !errors.Is(err, context.Canceled) || curves != nil || got.Complete {
			t.Fatalf("cancel at %d published curves: %v %+v", stopAt, err, curves)
		}
	}
}

func TestMediaCurvesHardPointLimit(t *testing.T) {
	tags := make([]flv.TagRecord, flv.HardMaxTags+1)
	// Script Tags require no source reads; this exercises the maximum permitted
	// index without allocating a million payloads or retained media events.
	for i := range tags {
		tags[i] = flv.TagRecord{ID: uint64(i), RawTypeFlags: byte(flv.TagTypeScript)}
	}
	scan := flv.ScanResult{Tags: tags[:flv.HardMaxTags], Complete: true}
	got, curves, err := AnalyzeV2WithCurves(nil, bytes.NewReader(nil), scan, OptionsV2{}, nil)
	if err != nil || curves == nil || len(curves.Points) != 3907 || curves.Points[3906].NextTag != flv.HardMaxTags {
		t.Fatalf("maximum curve index: %v", err)
	}
	assertCurveTotals(t, got, curves)
	scan.Tags = tags
	if _, curves, err := AnalyzeV2WithCurves(t.Context(), bytes.NewReader(nil), scan, OptionsV2{}, nil); err == nil || curves != nil {
		t.Fatal("oversized caller-supplied index escaped curve point limit")
	}
}
