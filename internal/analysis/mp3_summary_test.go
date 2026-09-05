package analysis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/mp3"
)

func mp3Fixture(dts uint32, header ...byte) analysisFixtureTag {
	return analysisFixtureTag{rawTypeFlags: 8, dts: dts, payload: append([]byte{0x2f}, header...)}
}

func analyzeV2Fixture(t testing.TB, options OptionsV2, tags ...analysisFixtureTag) ResultV2 {
	t.Helper()
	data, scan := scanAnalysisFixture(t, tags...)
	result, err := AnalyzeV2(context.Background(), bytes.NewReader(data), scan, options)
	if err != nil {
		t.Fatalf("AnalyzeV2: %v", err)
	}
	return result
}

func TestAnalyzeV2MP3TimingAndVersionIsolation(t *testing.T) {
	var tags []analysisFixtureTag
	for _, dts := range []uint32{0, 26, 26, 52, 10, 36} {
		tags = append(tags, mp3Fixture(dts, 0xff, 0xfb, 0x90, 0x64))
	}
	data, scan := scanAnalysisFixture(t, tags...)
	legacy, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil || legacy.Complete || legacy.Audio.UnclassifiedTagCount != 6 || legacy.Audio.MediaTagCount != 0 {
		t.Fatalf("v1 MP3 scope changed: %+v, %v", legacy, err)
	}
	got := analyzeV2Fixture(t, OptionsV2{}, tags...)
	if got.HasErrors() || len(got.Diagnostics) != 0 || got.AudioCodecs != (AudioCodecCounts{MP3TagCount: 6}) {
		t.Fatalf("v2 failed MP3 classification: %+v", got)
	}
	want := trackExpectation{
		tags: 6, indexedBytes: 30, mediaTags: 6, mediaBytes: 30,
		timing: Timeline{Present: true, SampleCount: 6, FirstMS: 0, LastMS: 36, MinMS: 0, MaxMS: 52, RangeMS: 52,
			ForwardDeltaTotalMS: 78, RegressionCount: 1, DuplicateCount: 1, LargestForwardGapMS: 26},
		bitrateBytes: 20, bitrateDuration: 78, bitrateBPS: 2051,
	}
	assertTrack(t, got.Audio, want)
	assertTrack(t, got.MP3.Track, want)
	if got.MP3.Headers.EventCount != 6 || got.MP3.Headers.ValidCount != 6 || got.MP3.Headers.Events[4].ComparedToPrevious {
		t.Fatalf("regression did not start a new header baseline: %+v", got.MP3.Headers)
	}
}

func TestAnalyzeV2MP3ParameterBitrateChangesAndRetention(t *testing.T) {
	tags := []analysisFixtureTag{
		mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64),  // MPEG-1, 128 kb/s.
		mp3Fixture(26, 0xff, 0xfb, 0xa0, 0x64), // 160 kb/s, same parameters.
		mp3Fixture(52, 0xff, 0xfb, 0xa2, 0x64), // Padding does not change either tuple.
		mp3Fixture(78, 0xff, 0xf3, 0x80, 0xc4), // MPEG-2, 22050 Hz, mono, 64 kb/s.
		mp3Fixture(104, 0xff, 0xf3, 0x80, 0xc4),
		mp3Fixture(130, 0xff, 0xf3, 0x00, 0xc4), // Valid free-format.
		mp3Fixture(156, 0xff, 0xf3, 0x8c, 0xc4), // Reserved sampling-frequency index.
		mp3Fixture(182, 0xff, 0xfb, 0x90, 0x64), // New baseline after malformed data.
		mp3Fixture(208, 0xff, 0xfb, 0x90, 0x64),
	}
	got := analyzeV2Fixture(t, OptionsV2{}, tags...)
	headers := got.MP3.Headers
	if got.Complete || headers.EventCount != 9 || headers.ValidCount != 8 || headers.MalformedCount != 1 ||
		headers.FreeFormatCount != 1 || headers.ParameterChangeCount != 1 || headers.BitrateChangeCount != 3 {
		t.Fatalf("wrong aggregates: %+v", headers)
	}
	if headers.Events[1].ParametersChangedFromPrevious || !headers.Events[1].BitrateChangedFromPrevious ||
		headers.Events[2].ParametersChangedFromPrevious || headers.Events[2].BitrateChangedFromPrevious ||
		!headers.Events[3].ParametersChangedFromPrevious || headers.Events[7].ComparedToPrevious {
		t.Fatalf("wrong comparisons: %+v", headers.Events)
	}
	if got.Audio.Bitrate.Bytes != 25 || got.Audio.Bitrate.CoveredIntervalMS != 130 || got.Audio.Bitrate.AverageBitsPerSecond != 1538 ||
		got.MP3.Track.Bitrate != got.Audio.Bitrate {
		t.Fatalf("bitrate crossed parameter/malformed boundaries: %+v", got.Audio.Bitrate)
	}
	bounded := analyzeV2Fixture(t, OptionsV2{MaxMP3HeaderEvents: 1}, tags...)
	if !bounded.MP3.Headers.Truncated || len(bounded.MP3.Headers.Events) != 1 || bounded.MP3.Headers.Events[0] != headers.Events[0] {
		t.Fatalf("retention is not a source-order prefix: %+v", bounded.MP3.Headers)
	}
	bounded.MP3.Headers.Events, bounded.MP3.Headers.Truncated = got.MP3.Headers.Events, got.MP3.Headers.Truncated
	if !reflect.DeepEqual(got, bounded) {
		t.Fatal("retention altered aggregate results or diagnostics")
	}
}

func TestAnalyzeV2AudioCodecSwitchesBreakCoverage(t *testing.T) {
	aac := func(dts uint32) analysisFixtureTag {
		return analysisFixtureTag{rawTypeFlags: 8, dts: dts, payload: []byte{0xaf, 1, 0xaa}}
	}
	got := analyzeV2Fixture(t, OptionsV2{},
		aac(0), mp3Fixture(100, 0xff, 0xfb, 0x90, 0x64), mp3Fixture(120, 0xff, 0xfb, 0x90, 0x64),
		aac(200), aac(220), mp3Fixture(300, 0xff, 0xfb, 0xa0, 0x64), mp3Fixture(320, 0xff, 0xfb, 0xa0, 0x64),
		analysisFixtureTag{rawTypeFlags: 9, dts: 0, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: 9, dts: 320, payload: []byte{0x17, 1, 0, 0, 0}},
	)
	if got.HasErrors() || got.AudioCodecs != (AudioCodecCounts{AACTagCount: 3, MP3TagCount: 4, SwitchCount: 3}) {
		t.Fatalf("codec totals: %+v", got)
	}
	if got.Audio.Bitrate.Bytes != 13 || got.Audio.Bitrate.CoveredIntervalMS != 60 || got.Audio.Bitrate.AverageBitsPerSecond != 1733 ||
		got.MP3.Track.Bitrate.Bytes != 10 || got.MP3.Track.Bitrate.CoveredIntervalMS != 40 {
		t.Fatalf("coverage crossed a codec switch: audio=%+v mp3=%+v", got.Audio.Bitrate, got.MP3.Track.Bitrate)
	}
	if got.Audio.Timing.ForwardDeltaTotalMS != 320 || got.MP3.Headers.BitrateChangeCount != 0 || got.MP3.Headers.Events[2].ComparedToPrevious {
		t.Fatalf("timeline/header epoch semantics: %+v", got)
	}
	if got.AVSync.Present || got.GOP.ClosedDurationCount != 1 {
		t.Fatalf("mixed-codec A/V skew or unrelated video GOP: %+v", got)
	}
}

func TestAnalyzeV2MP3SwitchResetsAACConfigurationBaseline(t *testing.T) {
	got := analyzeV2Fixture(t, OptionsV2{},
		analysisFixtureTag{rawTypeFlags: 8, payload: []byte{0xaf, 0, 0x12, 0x10}},
		mp3Fixture(20, 0xff, 0xfb, 0x90, 0x64),
		analysisFixtureTag{rawTypeFlags: 8, dts: 40, payload: []byte{0xaf, 0, 0x11, 0x90}},
	)
	if got.HasErrors() || got.AAC.ChangeCount != 0 || len(got.AAC.Events) != 2 || got.AAC.Events[1].ComparedToPrevious {
		t.Fatalf("AAC compared across MP3: %+v", got.AAC)
	}
}

func TestAnalyzeV2UnknownAndOtherAudio(t *testing.T) {
	data, scan := scanAnalysisFixture(t,
		mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64),
		analysisFixtureTag{rawTypeFlags: 8, dts: 10},
		mp3Fixture(20, 0xff, 0xfb, 0xa0, 0x64),
		analysisFixtureTag{rawTypeFlags: 8, dts: 30, payload: []byte{0xef}},
		mp3Fixture(40, 0xff, 0xfb, 0x90, 0x64),
		analysisFixtureTag{rawTypeFlags: 0x28, dts: 50, payload: []byte{0x2f}},
		mp3Fixture(60, 0xff, 0xfb, 0xa0, 0x64),
		mp3Fixture(70, 0xff, 0xfb, 0x90, 0x64),
	)
	scan.Tags[7].AvailableDataSize-- // Trusted index can explicitly contain an incomplete payload.
	got, err := AnalyzeV2(context.Background(), bytes.NewReader(data), scan, OptionsV2{})
	if err != nil || got.Complete || got.AudioCodecs != (AudioCodecCounts{MP3TagCount: 4, OtherTagCount: 1, UnknownTagCount: 3, SwitchCount: 2}) {
		t.Fatalf("unknown/other partition: %+v, %v", got, err)
	}
	if got.Audio.UnclassifiedTagCount != 4 || got.MP3.Track.TagCount != 4 || got.MP3.Headers.BitrateChangeCount != 0 || got.Audio.Bitrate.Present {
		t.Fatalf("unsafe comparison/coverage after unknown audio: %+v", got)
	}
	for _, e := range got.MP3.Headers.Events {
		if e.ComparedToPrevious {
			t.Fatalf("unknown/unsupported identity did not reset baseline: %+v", e)
		}
	}
	if !hasAnalysisDiagnostic(got.Diagnostics, CodeAudioCodecUnsupported, 3) {
		t.Fatal("reserved SoundFormat=14 was accepted")
	}
}

func TestAnalyzeV2FreeFormatAndAVRange(t *testing.T) {
	got := analyzeV2Fixture(t, OptionsV2{Options: Options{MaxConfigurationBytes: 1}, MaxMP3HeaderEvents: 1},
		mp3Fixture(100, 0xff, 0xe3, 0x08, 0xc4),
		mp3Fixture(125, 0xff, 0xe3, 0x08, 0xc4),
		analysisFixtureTag{rawTypeFlags: 9, dts: 110, payload: []byte{0x17, 1, 0xff, 0xff, 0xf6}},
		analysisFixtureTag{rawTypeFlags: 9, dts: 130, payload: []byte{0x27, 1, 0, 0, 0}},
	)
	if got.HasErrors() || got.MP3.Headers.FreeFormatCount != 2 || !got.MP3.Headers.Truncated || got.MP3.Track.Bitrate.AverageBitsPerSecond != 1600 {
		t.Fatalf("valid free format or retention marked incomplete: %+v", got)
	}
	event := got.MP3.Headers.Events[0]
	if event.BitrateKbps != 0 || event.BitrateMode != mp3.BitrateModeFree || event.Parameters.Version != mp3.VersionMPEG25 ||
		event.Parameters.SamplingFrequencyHz != 8000 || event.Parameters.SamplesPerFrame != 576 {
		t.Fatalf("free-format facts: %+v", event)
	}
	if !got.AVSync.Present || got.AVSync.StartExtremaSkewMS != 0 || got.AVSync.EndExtremaSkewMS != 5 || got.AVSync.OverlapMS != 25 {
		t.Fatalf("MP3 A/V range: %+v", got.AVSync)
	}
}

func TestAnalyzeV2MalformedRecoveryAndDiagnosticLimit(t *testing.T) {
	data, scan := scanAnalysisFixture(t,
		mp3Fixture(0, 0xff, 0xfb, 0x00, 0x64),
		mp3Fixture(10, 0xff, 0xfb, 0x00, 0x64),
		mp3Fixture(20, 0xff),
		mp3Fixture(30, 0, 0, 0xfc, 0),
	)
	got, err := AnalyzeV2(context.Background(), bytes.NewReader(data), scan, OptionsV2{Options: Options{MaxDiagnostics: 2}})
	if err != nil || !got.HasErrors() || got.MP3.Headers.MalformedCount != 2 || len(got.Diagnostics) != 2 {
		t.Fatalf("malformed/limited result: %+v, %v", got, err)
	}
	first := got.Diagnostics[0]
	end := scan.Tags[2].PayloadSpan().ByteOffset + uint64(scan.Tags[2].DataSize)
	if first.Code != mp3.CodeFrameHeaderTruncated || first.TagID == nil || *first.TagID != 2 ||
		first.Offset != scan.Tags[2].PayloadSpan().ByteOffset+1 || first.RecoveryOffset == nil || *first.RecoveryOffset != end {
		t.Fatalf("first actionable error was lost: %+v", first)
	}
	if got.Diagnostics[1].Code != CodeDiagnosticLimitReached || got.AVSync.Present {
		t.Fatalf("missing truncation marker: %+v", got)
	}
}

func TestAnalyzeV2NonParameterBitsAndTimestampWrapCandidate(t *testing.T) {
	got := analyzeV2Fixture(t, OptionsV2{},
		mp3Fixture(0xfffffff0, 0xff, 0xfb, 0x90, 0x64),
		mp3Fixture(0xffffffff, 0xff, 0xfa, 0x93, 0x7e), // CRC, padding, private, mode extension, copyright, emphasis.
		mp3Fixture(5, 0xff, 0xfb, 0xa0, 0x64),
	)
	if got.HasErrors() || got.MP3.Headers.ParameterChangeCount != 0 || got.MP3.Headers.BitrateChangeCount != 0 ||
		got.MP3.Headers.Events[2].ComparedToPrevious || !hasAnalysisDiagnostic(got.Diagnostics, mp3.CodeEmphasisReserved, 1) {
		t.Fatalf("unpromised flags or wrap candidate changed header comparisons: %+v", got)
	}
	if got.Audio.Timing.RegressionCount != 1 || got.Audio.Timing.LastMS != 5 || got.Audio.Bitrate.CoveredIntervalMS != 15 || got.Audio.Bitrate.Bytes != 5 {
		t.Fatalf("timestamp wrap candidate was unwrapped or covered: %+v", got.Audio)
	}
}

func TestAnalyzeV2CancellationDuringTraversal(t *testing.T) {
	tags := make([]analysisFixtureTag, 2048)
	for i := range tags {
		tags[i] = mp3Fixture(uint32(i*26), 0xff, 0xfb, 0x90, 0x64)
	}
	data, scan := scanAnalysisFixture(t, tags...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader := &mp3SummaryCancelReader{source: bytes.NewReader(data), cancel: cancel}
	got, err := AnalyzeV2(ctx, reader, scan, OptionsV2{MaxMP3HeaderEvents: 1})
	if !errors.Is(err, context.Canceled) || got.Complete || got.Tags.Total == 0 || got.Tags.Total > 1024 ||
		got.MP3.Headers.ValidCount != got.Tags.Total || len(got.MP3.Headers.Events) != 1 {
		t.Fatalf("mid-traversal cancellation failed to preserve bounded partial results: %+v, %v", got, err)
	}
}

type mp3SummaryCancelReader struct {
	source io.ReaderAt
	cancel context.CancelFunc
}

func (r *mp3SummaryCancelReader) ReadAt(dst []byte, offset int64) (int, error) {
	n, err := r.source.ReadAt(dst, offset)
	r.cancel()
	return n, err
}

func TestAnalyzeV2BoundedReadsCancellationAndFailures(t *testing.T) {
	fixture := mp3Fixture(0, 0xff, 0xfb, 0x90, 0x64)
	fixture.payload = append(fixture.payload, make([]byte, 1<<20)...)
	data, scan := scanAnalysisFixture(t, fixture)
	start := scan.Tags[0].PayloadSpan().ByteOffset
	reader := &mp3SummaryGuardReader{data: data, start: int64(start)}
	got, err := AnalyzeV2(nil, reader, scan, OptionsV2{})
	if err != nil || got.HasErrors() || reader.bytesRead != 5 || reader.calls != 2 {
		t.Fatalf("read beyond the fixed framing: calls=%d bytes=%d err=%v", reader.calls, reader.bytesRead, err)
	}
	reader.fail = errors.New("MPEG header unavailable")
	got, err = AnalyzeV2(nil, reader, scan, OptionsV2{})
	if !errors.Is(err, reader.fail) || got.Complete {
		t.Fatalf("ReaderAt failure was swallowed: %+v, %v", got, err)
	}
	got, err = AnalyzeV2(nil, analysisFullReadEOFReaderAt{data: data}, scan, OptionsV2{})
	if err != nil || !got.Complete {
		t.Fatalf("full read with EOF: %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got, err = AnalyzeV2(ctx, bytes.NewReader(data), scan, OptionsV2{})
	if !errors.Is(err, context.Canceled) || got.Complete || got.Tags.Total != 0 {
		t.Fatalf("cancellation: %+v, %v", got, err)
	}
	if _, err := AnalyzeV2(nil, nil, scan, OptionsV2{}); !errors.Is(err, ErrNilSource) {
		t.Fatalf("nil source: %v", err)
	}
	for _, options := range []OptionsV2{{MaxMP3HeaderEvents: HardMaxMP3HeaderEvents + 1}, {Options: Options{MaxDiagnostics: 1}}} {
		if _, err := AnalyzeV2(nil, bytes.NewReader(data), scan, options); err == nil {
			t.Fatalf("invalid option accepted: %+v", options)
		}
	}
}

type mp3SummaryGuardReader struct {
	data      []byte
	start     int64
	calls     int
	bytesRead int
	fail      error
}

func (r *mp3SummaryGuardReader) ReadAt(dst []byte, offset int64) (int, error) {
	r.calls++
	if !((offset == r.start && len(dst) == 1) || (offset == r.start+1 && len(dst) == 4)) {
		return 0, errors.New("read outside MP3 fixed header")
	}
	if offset == r.start+1 && r.fail != nil {
		return 0, r.fail
	}
	n, err := bytes.NewReader(r.data).ReadAt(dst, offset)
	r.bytesRead += n
	return n, err
}

var _ io.ReaderAt = (*mp3SummaryGuardReader)(nil)
