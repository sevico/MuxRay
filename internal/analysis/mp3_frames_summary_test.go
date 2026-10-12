package analysis

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/big"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func fullMP3Frame(t testing.TB, raw ...byte) []byte {
	t.Helper()
	parsed, err := mp3.ParseFrameHeader(bytes.NewReader(raw), cursor.Span{BitLength: uint32(len(raw)) * 8}, mp3.Options{})
	if err != nil || !parsed.Complete || !parsed.Header.FrameLengthPresent {
		t.Fatalf("frame fixture: %+v %v", parsed, err)
	}
	frame := make([]byte, parsed.Header.FrameLengthBytes)
	copy(frame, raw)
	return frame
}
func analyzeV3Fixture(t testing.TB, options OptionsV3, tags ...analysisFixtureTag) ResultV3 {
	t.Helper()
	data, scan := scanAnalysisFixture(t, tags...)
	result, err := AnalyzeV3(context.Background(), bytes.NewReader(data), scan, options)
	if err != nil {
		t.Fatal(err)
	}
	return result
}
func TestMP3FramesMergedSplitAndRateDuration(t *testing.T) {
	a := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64) // 417 bytes, 1152/44100 seconds.
	b := fullMP3Frame(t, 0xff, 0xfb, 0xa4, 0x64) // 480 bytes, 1152/48000 seconds.
	c := fullMP3Frame(t, 0xff, 0xf3, 0x80, 0xc4) // 208 bytes, 576/22050 seconds.
	merged := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(0, bytes.Join([][]byte{a, b, c}, nil)...))
	split := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(0, a...), mp3Fixture(26, b...), mp3Fixture(50, c...))
	for _, got := range []ResultV3{merged, split} {
		s := got.MP3Frames
		if !got.Complete || !s.Complete || s.CompleteFrameCount != 3 || s.FrameBytes != 1105 || s.SampleCount != 2880 || s.UnresolvedBytes != 0 || s.ParameterChangeCount != 2 || s.BitrateChangeCount != 2 || s.AttemptedHeaderCount != 3 || s.HeaderBytesRead != 12 {
			t.Fatalf("all-frame facts: %+v", s)
		}
		// 1152/44100 + 1152/48000 + 576/22050 = 934/12250 = 467/6125.
		if s.DurationSeconds != (MP3Duration{Numerator: 467, Denominator: 6125}) {
			t.Fatalf("duration changed rates incorrectly: %+v", s.DurationSeconds)
		}
		if len(s.Rates) != 3 || s.Rates[2].SamplingFrequencyHz != 22050 || s.Rates[2].SampleCount != 576 {
			t.Fatalf("rate partition: %+v", s.Rates)
		}
	}
	if merged.MP3.Headers.EventCount != 1 || merged.MP3.Headers.ParameterChangeCount != 0 || split.MP3.Headers.EventCount != 3 {
		t.Fatal("v2 first-header semantics changed")
	}
	for i, event := range merged.MP3Frames.Events {
		if event.TagID != 0 || event.FrameIndex != uint32(i) || event.DTS != 0 || event.SourceSpan.ByteOffset != uint64(25+[]int{0, len(a), len(a) + len(b)}[i]) {
			t.Fatalf("frame source coordinates: %+v", event)
		}
	}
}

func TestMP3FramesRetainedLimitDoesNotLimitAggregation(t *testing.T) {
	frame := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	tag := mp3Fixture(0, bytes.Repeat(frame, 3000)...)
	got := analyzeV3Fixture(t, OptionsV3{MaxMP3FrameEvents: 1}, tag).MP3Frames
	if !got.Complete || got.CompleteFrameCount != 3000 || got.HeaderBytesRead != 12000 || got.EventCount != 3000 || !got.EventsTruncated || len(got.Events) != 1 || got.FrameBytes != uint64(len(frame)*3000) {
		t.Fatalf("retention capped scan: %+v", got)
	}
}

func TestMP3FramesStopsAndRecoversOnlyAtNextTag(t *testing.T) {
	frame := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	tests := []struct {
		name, status string
		tail         []byte
	}{
		{"header", "truncatedHeader", []byte{0xff}},
		{"body", "truncatedFrame", frame[:40]},
		{"malformed", "malformedHeader", []byte{0, 0, 0, 0}},
		{"layer", "unsupportedLayer", []byte{0xff, 0xfd, 0x90, 0x64}},
		{"free", "freeFormat", []byte{0xff, 0xfb, 0x00, 0x64}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			tail := append([]byte{}, test.tail...)
			if test.name != "header" && test.name != "body" {
				tail = append(tail, frame...)
			} // No sync scanning.
			got := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(0, append(append([]byte{}, frame...), tail...)...), mp3Fixture(100, frame...))
			s := got.MP3Frames
			if got.Complete || s.Complete || s.CompleteFrameCount != 2 || s.PartialTagCount != 1 || s.CompleteTagCount != 1 || s.UnresolvedBytes != uint64(len(tail)) || s.FrameBytes+s.UnresolvedBytes != s.SoundDataBytes || len(s.Events) != 3 || s.Events[1].Status != test.status || s.Events[2].ComparedToPrevious {
				t.Fatalf("stop/recovery: %+v", s)
			}
			if s.Events[1].SourceSpan.ByteOffset != uint64(25+len(frame)) || s.Events[1].SourceSpan.BitLength != uint32(len(tail)*8) {
				t.Fatal("unresolved source span")
			}
		})
	}
	empty := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(0)).MP3Frames
	if empty.Complete || empty.TruncatedHeaderCount != 1 || empty.Events[0].SourceSpan.BitLength != 0 {
		t.Fatalf("empty sound data: %+v", empty)
	}
}

func TestMP3FramesGlobalBudgetIsIndependentFromEvents(t *testing.T) {
	frame := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	got := analyzeV3Fixture(t, OptionsV3{MaxMP3Frames: 2, MaxMP3FrameEvents: 1}, mp3Fixture(0, bytes.Repeat(frame, 3)...), mp3Fixture(100, frame...)).MP3Frames
	if got.Complete || got.AttemptedHeaderCount != 2 || got.CompleteFrameCount != 2 || got.BudgetStoppedTagCount != 2 || got.PartialTagCount != 2 || got.UnresolvedBytes != uint64(len(frame)*2) || got.HeaderBytesRead != 8 || got.EventCount != 4 || len(got.Events) != 1 || !got.EventsTruncated {
		t.Fatalf("budget: %+v", got)
	}
	exact := analyzeV3Fixture(t, OptionsV3{MaxMP3Frames: 2}, mp3Fixture(0, bytes.Repeat(frame, 2)...)).MP3Frames
	if !exact.Complete || exact.BudgetStoppedTagCount != 0 {
		t.Fatalf("exact budget match: %+v", exact)
	}
}

func TestMP3FramesComparisonBarriers(t *testing.T) {
	a := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	b := fullMP3Frame(t, 0xff, 0xfb, 0xa0, 0x64)
	video := analysisFixtureTag{rawTypeFlags: 9, payload: []byte{0x27, 1, 0, 0, 0}}
	tests := []struct {
		name                 string
		middle               analysisFixtureTag
		compared             bool
		unknown, unsupported uint64
	}{
		{"video", video, true, 0, 0},
		{"aac", analysisFixtureTag{rawTypeFlags: 8, payload: []byte{0xaf, 1, 0}}, false, 0, 0},
		{"unknown", analysisFixtureTag{rawTypeFlags: 8}, false, 1, 0},
		{"filtered", analysisFixtureTag{rawTypeFlags: 0x28, payload: []byte{0x2f}}, false, 1, 0},
		{"other", analysisFixtureTag{rawTypeFlags: 8, payload: []byte{0x7f}}, false, 0, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			s := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(10, a...), test.middle, mp3Fixture(20, b...)).MP3Frames
			if s.Events[1].ComparedToPrevious != test.compared || s.UnknownAudioTagCount != test.unknown || s.UnsupportedAudioTagCount != test.unsupported {
				t.Fatalf("barrier: %+v", s)
			}
		})
	}
	s := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(10, a...), mp3Fixture(9, b...)).MP3Frames
	if s.BitrateChangeCount != 0 || s.Events[1].ComparedToPrevious {
		t.Fatal("backward DTS comparison")
	}
	data, scan := scanAnalysisFixture(t, mp3Fixture(10, a...), mp3Fixture(15, b...), mp3Fixture(20, b...))
	scan.Tags[1].AvailableDataSize--
	got, err := AnalyzeV3(nil, bytes.NewReader(data), scan, OptionsV3{})
	if err != nil || got.MP3Frames.Complete || got.MP3Frames.UnknownAudioTagCount != 1 || got.MP3Frames.Events[1].ComparedToPrevious {
		t.Fatalf("incomplete tag barrier: %+v %v", got.MP3Frames, err)
	}
}

func TestMP3FramesCurvesAndLegacyIsolation(t *testing.T) {
	a := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	data, scan := scanAnalysisFixture(t, mp3Fixture(0, bytes.Repeat(a, 3)...), mp3Fixture(100, a...))
	v2, c2, err := AnalyzeV2WithCurves(nil, bytes.NewReader(data), scan, OptionsV2{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	v3, c3, err := AnalyzeV3WithCurves(nil, bytes.NewReader(data), scan, OptionsV3{}, nil)
	if err != nil || !reflect.DeepEqual(v2, v3.ResultV2) || !reflect.DeepEqual(c2, c3) {
		t.Fatalf("v2 facts/curves changed: %v", err)
	}
}

type mp3FrameReadGuard struct {
	data    []byte
	allowed map[int64]int
	reads   int
	failAt  int64
	fail    error
	cancel  context.CancelFunc
}

func (r *mp3FrameReadGuard) ReadAt(dst []byte, off int64) (int, error) {
	if r.allowed[off] != len(dst) {
		return 0, errors.New("read escaped fixed headers")
	}
	r.reads++
	n, err := bytes.NewReader(r.data).ReadAt(dst, off)
	if off == r.failAt && r.fail != nil {
		return n, r.fail
	}
	if r.cancel != nil && r.reads == 3 {
		r.cancel()
	}
	return n, err
}
func TestMP3FramesReadBudgetCancelAndErrors(t *testing.T) {
	frame := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	data, scan := scanAnalysisFixture(t, mp3Fixture(0, bytes.Repeat(frame, 100)...))
	allowed := map[int64]int{24: 1}
	for i := 0; i < 100; i++ {
		allowed[int64(25+i*len(frame))] = 4
	}
	reader := &mp3FrameReadGuard{data: data, allowed: allowed}
	got, err := AnalyzeV3(nil, reader, scan, OptionsV3{MaxMP3Frames: 2})
	if err != nil || reader.reads != 3 || got.MP3Frames.HeaderBytesRead != 8 {
		t.Fatalf("read budget %d %+v %v", reader.reads, got.MP3Frames, err)
	}
	sentinel := errors.New("source gone")
	for _, offset := range []int64{25, 25 + int64(len(frame))} {
		reader = &mp3FrameReadGuard{data: data, allowed: allowed, failAt: offset, fail: sentinel}
		got, err = AnalyzeV3(nil, reader, scan, OptionsV3{})
		if !errors.Is(err, sentinel) || got.Complete || got.MP3Frames.Complete {
			t.Fatalf("source error lost: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader = &mp3FrameReadGuard{data: data, allowed: allowed, cancel: cancel}
	got, err = AnalyzeV3(ctx, reader, scan, OptionsV3{})
	if !errors.Is(err, context.Canceled) || got.Complete || got.MP3Frames.Complete || reader.reads != 3 {
		t.Fatalf("per-frame cancellation: reads=%d err=%v", reader.reads, err)
	}
	// A complete fixed header accompanied by io.EOF remains usable.
	reader = &mp3FrameReadGuard{data: data, allowed: allowed, failAt: 25, fail: io.EOF}
	got, err = AnalyzeV3(nil, reader, scan, OptionsV3{})
	if err != nil || !got.Complete {
		t.Fatalf("full-read EOF: %+v %v", got.MP3Frames, err)
	}
}
func TestMP3FramesOptionsAndEmpty(t *testing.T) {
	data, scan := scanAnalysisFixture(t)
	for _, o := range []OptionsV3{{MaxMP3Frames: HardMaxMP3Frames + 1}, {MaxMP3FrameEvents: HardMaxMP3FrameEvents + 1}, {OptionsV2: OptionsV2{MaxMP3HeaderEvents: HardMaxMP3HeaderEvents + 1}}} {
		if _, err := AnalyzeV3(nil, bytes.NewReader(data), scan, o); err == nil {
			t.Fatal("accepted invalid options")
		}
	}
	got, err := AnalyzeV3(nil, bytes.NewReader(data), scan, OptionsV3{})
	if err != nil || !got.Complete || got.MP3Frames.Events == nil || got.MP3Frames.Rates == nil || got.MP3Frames.DurationSeconds != (MP3Duration{Denominator: 1}) {
		t.Fatalf("empty: %+v %v", got, err)
	}
	if _, err := AnalyzeV3(nil, nil, scan, OptionsV3{}); !errors.Is(err, ErrNilSource) {
		t.Fatal(err)
	}
}

func TestMP3FramesAllSupportedRatesHaveExactDuration(t *testing.T) {
	var frames [][]byte
	want := new(big.Rat)
	for _, version := range []byte{0xfb, 0xf3, 0xe3} {
		for index := byte(0); index < 3; index++ {
			frame := fullMP3Frame(t, 0xff, version, 0x90|index<<2, 0x64)
			parsed, err := mp3.ParseFrameHeader(bytes.NewReader(frame), cursor.Span{BitLength: 32}, mp3.Options{})
			if err != nil {
				t.Fatal(err)
			}
			want.Add(want, new(big.Rat).SetFrac64(int64(parsed.Header.SamplesPerFrame), int64(parsed.Header.SamplingFrequencyHz)))
			frames = append(frames, frame)
		}
	}
	got := analyzeV3Fixture(t, OptionsV3{}, mp3Fixture(0, bytes.Join(frames, nil)...)).MP3Frames
	actual := new(big.Rat).SetFrac(new(big.Int).SetUint64(got.DurationSeconds.Numerator), new(big.Int).SetUint64(got.DurationSeconds.Denominator))
	if !got.Complete || len(got.Rates) != 9 || actual.Cmp(want) != 0 {
		t.Fatalf("all-rate duration: got %s want %s: %+v", actual, want, got)
	}
}

func TestMP3FramesScanCompletionIsSeparateFromScanErrors(t *testing.T) {
	frame := fullMP3Frame(t, 0xff, 0xfb, 0x90, 0x64)
	data, scan := scanAnalysisFixture(t, mp3Fixture(0, frame...))
	// ScanResult can describe a complete indexed source with an error diagnostic.
	// Preserve that completion fact while refusing a complete media verdict.
	scan.Diagnostics = append(scan.Diagnostics, flv.Diagnostic{Code: "TEST_SCAN_ERROR", Severity: flv.SeverityError})
	got, err := AnalyzeV3(nil, bytes.NewReader(data), scan, OptionsV3{})
	if err != nil || !got.MP3Frames.ScanComplete || got.MP3Frames.Complete || got.Complete || got.MP3Frames.CompleteFrameCount != 1 {
		t.Fatalf("scan completion conflated with errors: %+v %v", got.MP3Frames, err)
	}
	scan.Complete = false
	got, err = AnalyzeV3(nil, bytes.NewReader(data), scan, OptionsV3{})
	if err != nil || got.MP3Frames.ScanComplete || got.MP3Frames.Complete {
		t.Fatalf("partial scan promoted to complete: %+v %v", got.MP3Frames, err)
	}
}

func FuzzMP3FramesSummary(f *testing.F) {
	f.Add([]byte{0xff, 0xfb, 0x90, 0x64})
	f.Add([]byte{0xff, 0xfb, 0, 0x64})
	f.Add(bytes.Repeat([]byte{0xff, 0xfb, 0x90, 0x64}, 200))
	f.Fuzz(func(t *testing.T, payload []byte) {
		if len(payload) > 8192 {
			t.Skip()
		}
		data, scan := scanAnalysisFixture(t, mp3Fixture(0, payload...), mp3Fixture(26, payload...))
		options := OptionsV3{OptionsV2: OptionsV2{Options: Options{MaxDiagnostics: 8}}, MaxMP3FrameEvents: 2, MaxMP3Frames: 32}
		got, err := AnalyzeV3(nil, bytes.NewReader(data), scan, options)
		if err != nil {
			t.Fatal(err)
		}
		repeated, err := AnalyzeV3(nil, bytes.NewReader(data), scan, options)
		if err != nil || !reflect.DeepEqual(got, repeated) {
			t.Fatal("nondeterministic summary")
		}
		s := got.MP3Frames
		if s.FrameBytes+s.UnresolvedBytes != s.SoundDataBytes || s.TagCount != s.CompleteTagCount+s.PartialTagCount || s.AttemptedHeaderCount > 32 || s.HeaderBytesRead > 4*s.AttemptedHeaderCount || len(s.Events) > 2 || s.EventCount != s.CompleteFrameCount+s.PartialTagCount || s.EventsTruncated != (s.EventCount > uint64(len(s.Events))) || s.DurationSeconds.Denominator == 0 {
			t.Fatalf("summary invariant: %+v", s)
		}
		var frames, samples uint64
		for _, rate := range s.Rates {
			frames += rate.FrameCount
			samples += rate.SampleCount
		}
		if frames != s.CompleteFrameCount || samples != s.SampleCount {
			t.Fatal("rate totals")
		}
		for _, event := range s.Events {
			start, end, err := event.SourceSpan.ByteRange()
			tag := scan.Tags[event.TagID]
			tagStart, tagEnd, _ := tag.PayloadSpan().ByteRange()
			if err != nil || start < tagStart+1 || end > tagEnd {
				t.Fatalf("source span outside SoundData: %+v", event)
			}
		}
	})
}
