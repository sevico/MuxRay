package analysis

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/flv"
)

const (
	analysisAVCC320x180 = "0164000cffe1001a6764000cacd941419f9f011000000300100000030320f142996001000668ebe3cb22c0fdf8f800"
	analysisAVCC176x144 = "0142c00bffe100166742c00bd902c4ec0440000003004000000c83c50a9201000568cb83cb20"
)

func TestAnalyzeMediaSummary(t *testing.T) {
	aacConfig := []byte{0x12, 0x10}
	avcConfig := analysisHex(t, analysisAVCC320x180)
	tags := []analysisFixtureTag{
		{rawTypeFlags: byte(flv.TagTypeAudio), dts: 0, payload: append([]byte{0xaf, 0x00}, aacConfig...)},
		{rawTypeFlags: byte(flv.TagTypeAudio), dts: 100, payload: []byte{0xaf, 0x01, 0xde, 0xad}},
		{rawTypeFlags: byte(flv.TagTypeAudio), dts: 100, payload: []byte{0xaf, 0x01, 0xbe}},
		{rawTypeFlags: byte(flv.TagTypeAudio), dts: 150, payload: []byte{0xaf, 0x01, 0xef}},
		{rawTypeFlags: byte(flv.TagTypeAudio), dts: 400, payload: []byte{0xaf, 0x01, 0x01}},
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 0, payload: append([]byte{0x17, 0x00, 0, 0, 0}, avcConfig...)},
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 0x01, 0xff, 0xff, 0xec}}, // PTS 80.
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 200, payload: []byte{0x27, 0x01, 0, 0, 0}},
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1_100, payload: []byte{0x17, 0x01, 0, 0, 30}}, // PTS 1130.
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1_500, payload: []byte{0x17, 0x01, 0, 0, 0}},
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1_600, payload: []byte{0x17, 0x02, 0, 0, 0}},
		{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1_700, payload: []byte{0x57}},
		{rawTypeFlags: byte(flv.TagTypeScript), dts: 0, payload: []byte{0x05}},
		{rawTypeFlags: 7, dts: 0, payload: nil},
	}
	data, scan := scanAnalysisFixture(t, tags...)

	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !got.Complete || got.HasErrors() || len(got.Diagnostics) != 0 {
		t.Fatalf("unexpected completion/diagnostics: complete=%v diagnostics=%+v", got.Complete, got.Diagnostics)
	}
	if got.Tags.Total != 14 || got.Tags.Audio != 5 || got.Tags.Video != 7 || got.Tags.Script != 1 || got.Tags.Other != 1 {
		t.Fatalf("unexpected tag counts: %+v", got.Tags)
	}
	wantDataBytes := uint64(0)
	for _, tag := range tags {
		wantDataBytes += uint64(len(tag.payload))
	}
	if got.Tags.DataSizeBytes != wantDataBytes {
		t.Fatalf("data bytes = %d, want %d", got.Tags.DataSizeBytes, wantDataBytes)
	}

	assertTrack(t, got.Audio, trackExpectation{
		tags: 5, indexedBytes: 17, mediaTags: 4, mediaBytes: 13, excluded: 1,
		timing:       Timeline{Present: true, SampleCount: 4, FirstMS: 100, LastMS: 400, MinMS: 100, MaxMS: 400, RangeMS: 300, ForwardDeltaTotalMS: 300, DuplicateCount: 1, LargestForwardGapMS: 250},
		bitrateBytes: 10, bitrateDuration: 300, bitrateBPS: 266,
	})
	videoIndexed := uint64(5+5+5+5+5+1) + uint64(5+len(avcConfig))
	assertTrack(t, got.Video, trackExpectation{
		tags: 7, indexedBytes: videoIndexed, mediaTags: 4, mediaBytes: 20, excluded: 3,
		timing:       Timeline{Present: true, SampleCount: 4, FirstMS: 100, LastMS: 1_500, MinMS: 100, MaxMS: 1_500, RangeMS: 1_400, ForwardDeltaTotalMS: 1_400, LargestForwardGapMS: 900},
		bitrateBytes: 15, bitrateDuration: 1_400, bitrateBPS: 85,
	})
	if got.VideoPTS != (Timeline{Present: true, SampleCount: 4, FirstMS: 80, LastMS: 1_500, MinMS: 80, MaxMS: 1_500, RangeMS: 1_420, ForwardDeltaTotalMS: 1_420, LargestForwardGapMS: 930}) {
		t.Fatalf("unexpected video PTS timeline: %+v", got.VideoPTS)
	}
	if got.Composition != (CompositionTime{Present: true, SampleCount: 4, NegativeCount: 1, ZeroCount: 2, PositiveCount: 1, MinMS: -20, MaxMS: 30}) {
		t.Fatalf("unexpected composition summary: %+v", got.Composition)
	}

	if got.Keyframes.DeclaredCount != 3 || got.Keyframes.Truncated || len(got.Keyframes.Entries) != 3 {
		t.Fatalf("unexpected keyframes: %+v", got.Keyframes)
	}
	wantKeyframes := []Keyframe{
		{TagID: 6, Offset: scan.Tags[6].Offset, DTS: 100, PTS: 80, PTSPresent: true, DataSize: 5, CodecID: flv.CodecIDAVC, GOPDurationMS: 1_000, GOPDurationPresent: true},
		{TagID: 8, Offset: scan.Tags[8].Offset, DTS: 1_100, PTS: 1_130, PTSPresent: true, DataSize: 5, CodecID: flv.CodecIDAVC, GOPDurationMS: 400, GOPDurationPresent: true},
		{TagID: 9, Offset: scan.Tags[9].Offset, DTS: 1_500, PTS: 1_500, PTSPresent: true, DataSize: 5, CodecID: flv.CodecIDAVC},
	}
	if !reflect.DeepEqual(got.Keyframes.Entries, wantKeyframes) {
		t.Fatalf("keyframes = %+v, want %+v", got.Keyframes.Entries, wantKeyframes)
	}
	if got.GOP != (GOP{Count: 3, ClosedDurationCount: 2, MinDurationMS: 400, MaxDurationMS: 1_000, AverageDurationMS: 700}) {
		t.Fatalf("unexpected GOP summary: %+v", got.GOP)
	}

	if got.AAC.EventCount != 1 || got.AAC.ChangeCount != 0 || got.AAC.Truncated || len(got.AAC.Events) != 1 {
		t.Fatalf("unexpected AAC events: %+v", got.AAC)
	}
	aacEvent := got.AAC.Events[0]
	if !aacEvent.Complete || aacEvent.FingerprintSHA256 != analysisSHA256(aacConfig) || !aacEvent.AudioObjectTypePresent || aacEvent.AudioObjectType != 2 || aacEvent.AudioObjectTypeName != "aacLC" ||
		!aacEvent.SampleRatePresent || aacEvent.SamplingFrequencyHz != 44_100 || aacEvent.ExtensionSamplingFrequencyPresent || !aacEvent.ChannelConfigurationPresent || aacEvent.ChannelConfiguration != 2 || aacEvent.ChannelConfigurationName != "stereo" || !aacEvent.ChannelCountPresent || aacEvent.ChannelCount != 2 {
		t.Fatalf("unexpected AAC configuration: %+v", aacEvent)
	}
	if got.AVC.EventCount != 1 || got.AVC.ChangeCount != 0 || got.AVC.Truncated || len(got.AVC.Events) != 1 {
		t.Fatalf("unexpected AVC events: %+v", got.AVC)
	}
	avcEvent := got.AVC.Events[0]
	if !avcEvent.Complete || avcEvent.FingerprintSHA256 != analysisSHA256(avcConfig) || !avcEvent.ProfilePresent || avcEvent.ProfileIDC != 100 || avcEvent.LevelIDC != 12 ||
		!avcEvent.LengthSizePresent || avcEvent.LengthSize != 4 || !avcEvent.FirstCompleteSPSPresent || avcEvent.FirstCompleteSPSIndex != 0 || avcEvent.FirstCompleteSPSID != 0 || avcEvent.CompleteSPSCount != 1 || avcEvent.DistinctSPSDimensions != 1 || avcEvent.CodedWidth != 320 || avcEvent.CodedHeight != 192 || avcEvent.DisplayWidth != 320 || avcEvent.DisplayHeight != 180 {
		t.Fatalf("unexpected AVC configuration: %+v", avcEvent)
	}
	if got.AVSync != (AVSync{Present: true, Basis: avSyncBasis, StartExtremaSkewMS: -20, EndExtremaSkewMS: 1_100, RangesOverlap: true, OverlapMS: 300}) {
		t.Fatalf("unexpected A/V sync: %+v", got.AVSync)
	}

	repeated, err := Analyze(nil, bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("repeat Analyze: %v", err)
	}
	if !reflect.DeepEqual(repeated, got) {
		t.Fatalf("analysis is not deterministic\nfirst=%+v\nrepeat=%+v", got, repeated)
	}
}

func TestAnalyzeConfigurationChangesAndRetentionLimits(t *testing.T) {
	aacA := []byte{0x12, 0x10}
	aacB := []byte{0x11, 0x90}
	avcA := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
	avcB := []byte{0x01, 0x42, 0x00, 0x1f, 0xfc, 0xe0, 0x00}
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 0, payload: append([]byte{0xaf, 0}, aacA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 1, payload: append([]byte{0xaf, 0}, aacA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 2, payload: append([]byte{0xaf, 0}, aacB...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 0, payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1, payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 2, payload: append([]byte{0x17, 0, 0, 0, 0}, avcB...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 0x01, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 0x01, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 200, payload: []byte{0x17, 0x01, 0, 0, 0}},
	)
	full, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze full configurations: %v", err)
	}
	assertConfigurationChangeEvents(t, full.AAC.Events, full.AAC.EventCount, full.AAC.AnalyzedCount, full.AAC.ChangeCount, analysisSHA256(aacA), analysisSHA256(aacB))
	assertAVCConfigurationChangeEvents(t, full.AVC.Events, full.AVC.EventCount, full.AVC.AnalyzedCount, full.AVC.ChangeCount, analysisSHA256(avcA), analysisSHA256(avcB))

	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{MaxKeyframes: 1, MaxConfigurationEvents: 1})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !got.Complete || got.HasErrors() {
		t.Fatalf("retention truncation should preserve complete aggregate counts: %+v", got)
	}
	if got.AAC.EventCount != 3 || got.AAC.AnalyzedCount != 3 || got.AAC.SkippedByBudgetCount != 0 || got.AAC.ChangeCount != 1 || len(got.AAC.Events) != 1 || !got.AAC.Truncated || got.AAC.Events[0].FingerprintSHA256 != analysisSHA256(aacA) {
		t.Fatalf("unexpected bounded AAC events: %+v", got.AAC)
	}
	if got.AVC.EventCount != 3 || got.AVC.AnalyzedCount != 3 || got.AVC.SkippedByBudgetCount != 0 || got.AVC.ChangeCount != 1 || len(got.AVC.Events) != 1 || !got.AVC.Truncated || got.AVC.Events[0].FingerprintSHA256 != analysisSHA256(avcA) {
		t.Fatalf("unexpected bounded AVC events: %+v", got.AVC)
	}
	if got.Keyframes.DeclaredCount != 3 || len(got.Keyframes.Entries) != 1 || !got.Keyframes.Truncated {
		t.Fatalf("unexpected bounded keyframes: %+v", got.Keyframes)
	}
	if got.Keyframes.Entries[0].GOPDurationPresent {
		t.Fatalf("retained first keyframe was assigned a later keyframe's GOP duration: %+v", got.Keyframes.Entries[0])
	}
	if got.GOP.Count != 3 || got.GOP.ClosedDurationCount != 1 || got.GOP.NonPositiveDurationCount != 1 || got.GOP.MinDurationMS != 100 || got.GOP.MaxDurationMS != 100 || got.GOP.AverageDurationMS != 100 {
		t.Fatalf("unexpected GOP aggregate under retention limit: %+v", got.GOP)
	}
}

func TestAnalyzeAVCConfigurationDimensionChange(t *testing.T) {
	first := analysisHex(t, analysisAVCC320x180)
	second := analysisHex(t, analysisAVCC176x144)
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, first...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1, payload: append([]byte{0x17, 0, 0, 0, 0}, second...)},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !got.Complete || got.AVC.ChangeCount != 1 || len(got.AVC.Events) != 2 || !got.AVC.Events[1].ComparedToPrevious || !got.AVC.Events[1].ChangedFromPrevious {
		t.Fatalf("unexpected AVC dimension-change aggregate: %+v", got.AVC)
	}
	wantDimensions := [][2]uint64{{320, 180}, {176, 144}}
	for index, event := range got.AVC.Events {
		if !event.Complete || event.CompleteSPSCount != 1 || event.DistinctSPSDimensions != 1 || !event.FirstCompleteSPSPresent || event.FirstCompleteSPSIndex != 0 || event.FirstCompleteSPSID != 0 || event.DisplayWidth != wantDimensions[index][0] || event.DisplayHeight != wantDimensions[index][1] {
			t.Fatalf("AVC configuration[%d] dimensions = %+v, want %dx%d", index, event, wantDimensions[index][0], wantDimensions[index][1])
		}
	}
}

func TestAnalyzeUnsupportedVideoStartsNewAVCConfigurationEpoch(t *testing.T) {
	avcA := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x12}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || got.AVC.EventCount != 2 || got.AVC.AnalyzedCount != 2 || got.AVC.ChangeCount != 0 || len(got.AVC.Events) != 2 {
		t.Fatalf("unexpected AVC configuration aggregates: %+v", got.AVC)
	}
	wantFingerprint := analysisSHA256(avcA)
	for index, event := range got.AVC.Events {
		if !event.Complete || event.FingerprintSHA256 != wantFingerprint || event.ComparedToPrevious || event.ChangedFromPrevious {
			t.Fatalf("AVC configuration[%d] crossed codec epoch: %+v", index, event)
		}
	}
	if !hasAnalysisDiagnostic(got.Diagnostics, CodeVideoCodecUnsupported, 1) {
		t.Fatalf("unsupported codec diagnostic missing: %+v", got.Diagnostics)
	}
}

func TestAnalyzeUnsupportedAudioStartsNewAACConfigurationEpoch(t *testing.T) {
	aacA := []byte{0x12, 0x10}
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: append([]byte{0xaf, 0}, aacA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: []byte{0x2f}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: append([]byte{0xaf, 0}, aacA...)},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || got.AAC.EventCount != 2 || got.AAC.AnalyzedCount != 2 || got.AAC.ChangeCount != 0 || len(got.AAC.Events) != 2 {
		t.Fatalf("unexpected AAC configuration aggregates: %+v", got.AAC)
	}
	wantFingerprint := analysisSHA256(aacA)
	for index, event := range got.AAC.Events {
		if !event.Complete || event.FingerprintSHA256 != wantFingerprint || event.ComparedToPrevious || event.ChangedFromPrevious {
			t.Fatalf("AAC configuration[%d] crossed codec epoch: %+v", index, event)
		}
	}
	if !hasAnalysisDiagnostic(got.Diagnostics, CodeAudioCodecUnsupported, 1) {
		t.Fatalf("unsupported codec diagnostic missing: %+v", got.Diagnostics)
	}
}

func TestAnalyzeAVCFramingDiagnostics(t *testing.T) {
	t.Run("end of sequence starts a new configuration epoch", func(t *testing.T) {
		avcA := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x17, 2, 0, 0, 1}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if !got.Complete || got.HasErrors() || got.AVC.ChangeCount != 0 || len(got.AVC.Events) != 2 || got.AVC.Events[1].ComparedToPrevious {
			t.Fatalf("AVC EOS did not start a clean configuration epoch: %+v", got)
		}
		if !hasAnalysisDiagnostic(got.Diagnostics, flv.CodeAVCCompositionTimeNonZero, 1) {
			t.Fatalf("non-zero EOS composition-time warning missing: %+v", got.Diagnostics)
		}
	})

	t.Run("end of sequence payload is invalid", func(t *testing.T) {
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x17, 2, 0, 0, 0, 0xaa}},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if got.Complete || !got.HasErrors() || got.Video.ExcludedTagCount != 1 || got.Video.MediaTagCount != 0 {
			t.Fatalf("unexpected EOS payload result: %+v", got)
		}
		diagnostic := findAnalysisDiagnostic(got.Diagnostics, flv.CodeAVCEndOfSequencePayloadUnexpected, 0)
		payloadEnd := scan.Tags[0].PayloadSpan().ByteOffset + uint64(scan.Tags[0].DataSize)
		if diagnostic == nil || diagnostic.Expected == nil || *diagnostic.Expected != 0 || diagnostic.Actual == nil || *diagnostic.Actual != 1 || diagnostic.RecoveryOffset == nil || *diagnostic.RecoveryOffset != payloadEnd {
			t.Fatalf("unexpected EOS payload diagnostic: %+v", diagnostic)
		}
	})

	t.Run("reserved frame types retain warnings", func(t *testing.T) {
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x07, 1, 0, 0, 0}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x37, 1, 0, 0, 0}},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if !got.Complete || got.HasErrors() || got.Video.MediaTagCount != 2 ||
			!hasAnalysisDiagnostic(got.Diagnostics, flv.CodeAVCFrameTypeReserved, 0) ||
			!hasAnalysisDiagnostic(got.Diagnostics, flv.CodeAVCFrameTypeUnexpected, 1) {
			t.Fatalf("AVC frame-type warnings were not retained: %+v", got)
		}
	})
}

func TestAnalyzeTimelineRegressionAndCoveredIntervalBitrate(t *testing.T) {
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 100, payload: []byte{0xaf, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 200, payload: []byte{0xaf, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 200, payload: []byte{0xaf, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 50, payload: []byte{0xaf, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 400, payload: []byte{0xaf, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 0x01, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 400, payload: []byte{0x27, 0x01, 0, 0, 0}},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	wantTimeline := Timeline{
		Present: true, SampleCount: 5, FirstMS: 100, LastMS: 400, MinMS: 50, MaxMS: 400,
		RangeMS: 350, ForwardDeltaTotalMS: 450, RegressionCount: 1, DuplicateCount: 1, LargestForwardGapMS: 350,
	}
	if got.Audio.Timing != wantTimeline {
		t.Fatalf("audio timeline = %+v, want %+v", got.Audio.Timing, wantTimeline)
	}
	if got.Audio.Bitrate != (Bitrate{Present: true, Basis: bitrateBasis, Bytes: 4, CoveredIntervalMS: 450, AverageBitsPerSecond: 71}) {
		t.Fatalf("audio bitrate did not use covered forward-DTS intervals: %+v", got.Audio.Bitrate)
	}
	if got.AVSync.Present {
		t.Fatalf("A/V skew must be suppressed when an input timeline regresses: %+v", got.AVSync)
	}
}

func TestAnalyzeTimestampBoundariesAndBFrameReordering(t *testing.T) {
	t.Run("raw DTS wrap candidate remains a regression", func(t *testing.T) {
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 0xfffffff0, payload: []byte{0x17, 1, 0, 0, 0}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 0x00000010, payload: []byte{0x17, 1, 0, 0, 0}},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		want := Timeline{
			Present: true, SampleCount: 2, FirstMS: 0xfffffff0, LastMS: 0x10,
			MinMS: 0x10, MaxMS: 0xfffffff0, RangeMS: 0xffffffe0, RegressionCount: 1,
		}
		if !got.Complete || got.Video.Timing != want || got.Video.Bitrate.Present || got.GOP != (GOP{Count: 2}) || got.Keyframes.Entries[0].GOPDurationPresent {
			t.Fatalf("raw DTS was unwrapped or crossed a segment boundary: %+v", got)
		}
	})

	t.Run("signed composition-time extrema", func(t *testing.T) {
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x27, 1, 0x80, 0x00, 0x00}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x27, 1, 0x7f, 0xff, 0xff}},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if !got.Complete || got.Composition.MinMS != -8_388_608 || got.Composition.MaxMS != 8_388_607 || got.VideoPTS.MinMS != -8_388_608 || got.VideoPTS.MaxMS != 8_388_607 {
			t.Fatalf("signed int24 extrema were decoded incorrectly: composition=%+v pts=%+v", got.Composition, got.VideoPTS)
		}
	})

	t.Run("PTS may be negative", func(t *testing.T) {
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1, payload: []byte{0x17, 1, 0xff, 0xff, 0xfe}},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		if !got.Complete || got.VideoPTS.FirstMS != -1 || len(got.Keyframes.Entries) != 1 || got.Keyframes.Entries[0].PTS != -1 {
			t.Fatalf("negative PTS was not preserved: %+v", got)
		}
	})

	t.Run("B-frame PTS regression preserves DTS GOP and AV sync", func(t *testing.T) {
		data, scan := scanAnalysisFixture(t,
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 100, payload: []byte{0xaf, 1}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 1, 0, 0, 100}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 150, payload: []byte{0x27, 1, 0xff, 0xff, 0x9c}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 200, payload: []byte{0xaf, 1}},
			analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 200, payload: []byte{0x17, 1, 0, 0, 0}},
		)
		got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
		if err != nil {
			t.Fatalf("Analyze: %v", err)
		}
		wantSync := AVSync{
			Present: true, Basis: avSyncBasis, StartExtremaSkewMS: -50,
			EndExtremaSkewMS: 0, RangesOverlap: true, OverlapMS: 100,
		}
		if !got.Complete || got.Video.Timing.RegressionCount != 0 || got.VideoPTS.RegressionCount != 1 || got.GOP.ClosedDurationCount != 1 || got.GOP.AverageDurationMS != 100 || got.AVSync != wantSync {
			t.Fatalf("B-frame reordering altered DTS continuity or suppressed A/V extrema: %+v", got)
		}
	})
}

func TestAnalyzeConfigurationByteBudget(t *testing.T) {
	aacConfig := []byte{0x12, 0x10}
	avcConfig := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: append([]byte{0xaf, 0}, aacConfig...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcConfig...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 200, payload: []byte{0x17, 1, 0, 0, 0}},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{MaxConfigurationBytes: uint64(len(aacConfig))})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || !got.HasErrors() {
		t.Fatalf("configuration-byte truncation was not surfaced: %+v", got)
	}
	if got.AAC.EventCount != 1 || got.AAC.AnalyzedCount != 1 || got.AAC.SkippedByBudgetCount != 0 || len(got.AAC.Events) != 1 {
		t.Fatalf("AAC configuration inside the shared budget was not analyzed: %+v", got.AAC)
	}
	if got.AVC.EventCount != 1 || got.AVC.AnalyzedCount != 0 || got.AVC.SkippedByBudgetCount != 1 || len(got.AVC.Events) != 0 {
		t.Fatalf("AVC configuration outside the shared budget was not skipped: %+v", got.AVC)
	}
	if got.GOP != (GOP{Count: 2}) || got.Keyframes.Entries[0].GOPDurationPresent {
		t.Fatalf("budget-skipped AVC configuration did not break the GOP: GOP=%+v keyframes=%+v", got.GOP, got.Keyframes)
	}
	if !hasAnalysisDiagnostic(got.Diagnostics, CodeConfigurationByteLimit, 2) {
		t.Fatalf("configuration-byte budget diagnostic missing: %+v", got.Diagnostics)
	}
}

func TestAnalyzeIncompleteConfigurationDoesNotResetChangeBaseline(t *testing.T) {
	aacA := []byte{0x12, 0x10}
	aacB := []byte{0x11, 0x90}
	avcA := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
	avcB := []byte{0x01, 0x42, 0x00, 0x1f, 0xfc, 0xe0, 0x00}
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: append([]byte{0xaf, 0}, aacA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: []byte{0xaf, 0, 0x12}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: append([]byte{0xaf, 0}, aacB...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x17, 0, 0, 0, 0, 1, 0x64}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcB...)},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || got.AAC.ChangeCount != 1 || got.AVC.ChangeCount != 1 || len(got.AAC.Events) != 3 || len(got.AVC.Events) != 3 {
		t.Fatalf("unexpected configuration aggregates: AAC=%+v AVC=%+v", got.AAC, got.AVC)
	}
	for name, states := range map[string][]struct {
		complete bool
		compared bool
		changed  bool
	}{
		"AAC": {
			{complete: true},
			{},
			{complete: true, compared: true, changed: true},
		},
		"AVC": {
			{complete: true},
			{},
			{complete: true, compared: true, changed: true},
		},
	} {
		for index, state := range states {
			var complete, compared, changed bool
			if name == "AAC" {
				complete, compared, changed = got.AAC.Events[index].Complete, got.AAC.Events[index].ComparedToPrevious, got.AAC.Events[index].ChangedFromPrevious
			} else {
				complete, compared, changed = got.AVC.Events[index].Complete, got.AVC.Events[index].ComparedToPrevious, got.AVC.Events[index].ChangedFromPrevious
			}
			if complete != state.complete || compared != state.compared || changed != state.changed {
				t.Fatalf("%s configuration[%d] state = complete=%v compared=%v changed=%v, want %+v", name, index, complete, compared, changed, state)
			}
		}
	}
}

func TestAnalyzeAVCConfigurationRecoveryBreaksGOP(t *testing.T) {
	avcA := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
	tests := []struct {
		name       string
		uncertain  []byte
		options    Options
		wantEvents uint64
		wantParsed uint64
		wantSkip   uint64
	}{
		{
			name:       "malformed configuration",
			uncertain:  []byte{0x01, 0x64},
			wantEvents: 3,
			wantParsed: 3,
		},
		{
			name:       "budget-skipped configuration",
			uncertain:  append(append([]byte(nil), avcA...), 0xff),
			options:    Options{MaxConfigurationBytes: uint64(len(avcA) * 2)},
			wantEvents: 3,
			wantParsed: 2,
			wantSkip:   1,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			data, scan := scanAnalysisFixture(t,
				analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
				analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: append([]byte{0x17, 0, 0, 0, 0}, test.uncertain...)},
				analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 200, payload: []byte{0x17, 1, 0, 0, 0}},
				analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 250, payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
				analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 300, payload: []byte{0x17, 1, 0, 0, 0}},
				analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 400, payload: []byte{0x17, 1, 0, 0, 0}},
			)
			got, err := Analyze(context.Background(), bytes.NewReader(data), scan, test.options)
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			if got.Complete || got.AVC.EventCount != test.wantEvents || got.AVC.AnalyzedCount != test.wantParsed || got.AVC.SkippedByBudgetCount != test.wantSkip || got.AVC.ChangeCount != 0 {
				t.Fatalf("unexpected AVC uncertainty aggregates: %+v", got.AVC)
			}
			lastEvent := got.AVC.Events[len(got.AVC.Events)-1]
			if !lastEvent.Complete || !lastEvent.ComparedToPrevious || lastEvent.ChangedFromPrevious {
				t.Fatalf("recovery config should compare with the last complete baseline without reporting a change: %+v", lastEvent)
			}
			if got.GOP != (GOP{Count: 3, ClosedDurationCount: 1, MinDurationMS: 100, MaxDurationMS: 100, AverageDurationMS: 100}) ||
				got.Keyframes.Entries[0].GOPDurationPresent || !got.Keyframes.Entries[1].GOPDurationPresent || got.Keyframes.Entries[1].GOPDurationMS != 100 {
				t.Fatalf("GOP crossed an unknown-to-known configuration recovery: GOP=%+v keyframes=%+v", got.GOP, got.Keyframes)
			}
		})
	}
}

func TestAnalyzeGOPBoundaries(t *testing.T) {
	avcA := []byte{0x01, 0x42, 0x00, 0x1e, 0xff, 0xe0, 0x00}
	avcB := []byte{0x01, 0x42, 0x00, 0x1f, 0xfc, 0xe0, 0x00}
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 50, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 100, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 150, payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 200, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 250, payload: []byte{0x17, 0, 0, 0, 0, 1, 0x64}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 275, payload: append([]byte{0x17, 0, 0, 0, 0}, avcA...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 300, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 350, payload: append([]byte{0x17, 0, 0, 0, 0}, avcB...)},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 400, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 500, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 550, payload: []byte{0x12}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 600, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 700, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 700, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 800, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 850, payload: []byte{0x17, 2, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 900, payload: []byte{0x17, 1, 0, 0, 0}},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || got.Keyframes.DeclaredCount != 11 || len(got.Keyframes.Entries) != 11 {
		t.Fatalf("unexpected keyframe summary: %+v", got.Keyframes)
	}
	if got.GOP != (GOP{Count: 11, ClosedDurationCount: 4, NonPositiveDurationCount: 1, MinDurationMS: 100, MaxDurationMS: 100, AverageDurationMS: 100}) {
		t.Fatalf("unexpected GOP boundaries: %+v", got.GOP)
	}
	wantDurations := map[int]uint64{1: 100, 4: 100, 6: 100, 8: 100}
	for index, keyframe := range got.Keyframes.Entries {
		want, present := wantDurations[index]
		if keyframe.GOPDurationPresent != present || keyframe.GOPDurationMS != want {
			t.Fatalf("keyframe[%d] GOP = %d/%v, want %d/%v", index, keyframe.GOPDurationMS, keyframe.GOPDurationPresent, want, present)
		}
	}
	if got.AVC.EventCount != 5 || got.AVC.AnalyzedCount != 5 || got.AVC.ChangeCount != 1 || got.AVC.Events[1].ChangedFromPrevious || got.AVC.Events[2].Complete || got.AVC.Events[2].ComparedToPrevious || got.AVC.Events[3].ChangedFromPrevious || !got.AVC.Events[4].ChangedFromPrevious {
		t.Fatalf("configuration epochs were misclassified: %+v", got.AVC)
	}
	if !hasAnalysisDiagnostic(got.Diagnostics, CodeVideoCodecUnsupported, 11) {
		t.Fatalf("codec-switch boundary diagnostic missing: %+v", got.Diagnostics)
	}
}

func TestAnalyzeVideoDTSRegressionBreaksGOP(t *testing.T) {
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 1_000, payload: []byte{0x17, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 0, payload: []byte{0x27, 1, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 2_000, payload: []byte{0x17, 1, 0, 0, 0}},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !got.Complete || got.Video.Timing.RegressionCount != 1 {
		t.Fatalf("video DTS regression was not recorded: %+v", got.Video)
	}
	if got.GOP != (GOP{Count: 2}) {
		t.Fatalf("non-keyframe DTS regression incorrectly closed a GOP: %+v", got.GOP)
	}
	if len(got.Keyframes.Entries) != 2 || got.Keyframes.Entries[0].GOPDurationPresent || got.Keyframes.Entries[1].GOPDurationPresent {
		t.Fatalf("GOP duration crossed the regression boundary: %+v", got.Keyframes)
	}
}

func TestAnalyzeMalformedFilteredAndIncompleteTags(t *testing.T) {
	complete := analysisFLVBytes(
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: nil},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: []byte{0xaf}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: []byte{0xaf, 0x02}},
		analysisFixtureTag{rawTypeFlags: 0x20 | byte(flv.TagTypeAudio), payload: []byte{0xaf, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: []byte{0x2f}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: nil},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x17, 0x01}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x17, 0x03, 0, 0, 0}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), payload: []byte{0x12}},
	)
	data := appendIncompleteAnalysisTag(complete, byte(flv.TagTypeVideo), 0, 5, []byte{0x17})
	scan := scanAnalysisBytes(t, data)
	if scan.Complete || len(scan.Tags) != 10 {
		t.Fatalf("fixture did not produce an indexed partial tag: %+v", scan)
	}

	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	wantCodes := []string{
		CodeAudioHeaderTruncated,
		CodeAACPacketTypeTruncated,
		CodeAACPacketTypeUnknown,
		CodeFilteredTagSkipped,
		CodeAudioCodecUnsupported,
		CodeVideoHeaderTruncated,
		CodeAVCFramingTruncated,
		CodeAVCPacketTypeUnknown,
		CodeVideoCodecUnsupported,
		CodeIncompleteTagSkipped,
	}
	if got.Complete || !got.HasErrors() || got.Audio.UnclassifiedTagCount != 5 || got.Video.UnclassifiedTagCount != 5 || len(got.Diagnostics) != len(wantCodes) {
		t.Fatalf("unexpected malformed summary: %+v", got)
	}
	for index, wantCode := range wantCodes {
		diagnostic := got.Diagnostics[index]
		if diagnostic.Code != wantCode || diagnostic.TagID == nil || *diagnostic.TagID != uint64(index) {
			t.Fatalf("diagnostic[%d] = %+v, want code=%s tag=%d", index, diagnostic, wantCode, index)
		}
	}
	for _, index := range []int{3, 4, 8} {
		if got.Diagnostics[index].Severity != SeverityWarning {
			t.Fatalf("diagnostic[%d] should be a warning: %+v", index, got.Diagnostics[index])
		}
	}

	bounded, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{MaxDiagnostics: 2})
	if err != nil {
		t.Fatalf("Analyze bounded diagnostics: %v", err)
	}
	if bounded.Complete || len(bounded.Diagnostics) != 2 || bounded.Diagnostics[0].Code != CodeAudioHeaderTruncated || bounded.Diagnostics[1].Code != CodeDiagnosticLimitReached || bounded.Diagnostics[1].Severity != SeverityError {
		t.Fatalf("unexpected diagnostic truncation: %+v", bounded.Diagnostics)
	}
}

func TestAnalyzeInheritsScanDiagnosticIncompleteness(t *testing.T) {
	data := analysisFLVBytes(
		analysisFixtureTag{rawTypeFlags: 7},
		analysisFixtureTag{rawTypeFlags: 7},
		analysisFixtureTag{rawTypeFlags: 7},
	)
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{MaxDiagnostics: 2})
	if err != nil {
		t.Fatalf("flv.Scan: %v", err)
	}
	if !scan.Complete || !scan.HasErrors() || len(scan.Diagnostics) != 2 || scan.Diagnostics[1].Code != flv.CodeDiagnosticLimitReached {
		t.Fatalf("fixture did not produce a complete scan with truncated diagnostics: %+v", scan)
	}
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || !got.HasErrors() || got.Tags.Total != 3 {
		t.Fatalf("scan diagnostic incompleteness was not inherited: %+v", got)
	}
}

func TestDiagnosticCollectorRetainsErrorAfterWarningFlood(t *testing.T) {
	collector := diagnosticCollector{limit: 2}
	for offset := uint64(1); offset <= 3; offset++ {
		collector.add(Diagnostic{
			Code: "TEST_WARNING", Severity: SeverityWarning, Offset: offset,
		})
	}
	firstError := Diagnostic{
		Code: "TEST_FIRST_ERROR", Severity: SeverityError, TagID: tagID(9),
		Offset: 90, BitOffset: 3, RecoveryOffset: number(99),
	}
	collector.add(firstError)
	collector.add(Diagnostic{Code: "TEST_LATER_ERROR", Severity: SeverityError, Offset: 100})

	if !collector.truncated || len(collector.items) != 2 || collector.items[0].Code != firstError.Code {
		t.Fatalf("late actionable error was not retained: %+v", collector)
	}
	marker := collector.items[1]
	if marker.Code != CodeDiagnosticLimitReached || marker.Severity != SeverityError || marker.TagID == nil || *marker.TagID != 9 || marker.Offset != 90 || marker.BitOffset != 3 || marker.RecoveryOffset == nil || *marker.RecoveryOffset != 99 {
		t.Fatalf("diagnostic limit marker did not retain error recovery context: %+v", marker)
	}

	warningsOnly := diagnosticCollector{limit: 2}
	warningsOnly.add(Diagnostic{Code: "TEST_WARNING_1", Severity: SeverityWarning, Offset: 1})
	warningsOnly.add(Diagnostic{Code: "TEST_WARNING_2", Severity: SeverityWarning, Offset: 2})
	warningsOnly.add(Diagnostic{Code: "TEST_WARNING_3", Severity: SeverityWarning, Offset: 3})
	if !warningsOnly.truncated || len(warningsOnly.items) != 2 || warningsOnly.items[0].Code != "TEST_WARNING_1" || warningsOnly.items[1].Code != CodeDiagnosticLimitReached || warningsOnly.items[1].Offset != 3 {
		t.Fatalf("warning-only diagnostic truncation is unstable: %+v", warningsOnly)
	}
}

func TestAnalyzeMalformedConfigurationsKeepPartialEvents(t *testing.T) {
	data, scan := scanAnalysisFixture(t,
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), dts: 10, payload: []byte{0xaf, 0x00, 0x12}},
		analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeVideo), dts: 20, payload: []byte{0x17, 0x00, 0, 0, 0, 0x01, 0x64}},
	)
	got, err := Analyze(context.Background(), bytes.NewReader(data), scan, Options{})
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Complete || len(got.AAC.Events) != 1 || got.AAC.Events[0].Complete || len(got.AVC.Events) != 1 || got.AVC.Events[0].Complete {
		t.Fatalf("malformed configurations lost partial events: %+v", got)
	}
	if got.AAC.Events[0].FingerprintSHA256 != analysisSHA256([]byte{0x12}) || got.AVC.Events[0].FingerprintSHA256 != analysisSHA256([]byte{0x01, 0x64}) {
		t.Fatalf("malformed configuration fingerprints cover the wrong span: AAC=%+v AVC=%+v", got.AAC.Events[0], got.AVC.Events[0])
	}
	if !hasAnalysisDiagnostic(got.Diagnostics, aac.CodeSamplingFrequencyIndexTruncated, 0) || !hasAnalysisDiagnostic(got.Diagnostics, avc.CodeRecordHeaderTruncated, 1) {
		t.Fatalf("nested diagnostics were not retained with Tag IDs: %+v", got.Diagnostics)
	}
}

func TestAnalyzeCancellationReaderFailuresAndOptions(t *testing.T) {
	data, scan := scanAnalysisFixture(t, analysisFixtureTag{rawTypeFlags: byte(flv.TagTypeAudio), payload: []byte{0xaf, 0x01}})

	t.Run("nil source", func(t *testing.T) {
		_, err := Analyze(context.Background(), nil, scan, Options{})
		if !errors.Is(err, ErrNilSource) {
			t.Fatalf("error = %v, want ErrNilSource", err)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		result, err := Analyze(ctx, bytes.NewReader(data), scan, Options{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
		if result.Complete || result.Tags.Total != 0 {
			t.Fatalf("cancelled partial analysis reports complete or visited tags: %+v", result)
		}
	})

	t.Run("reader failure", func(t *testing.T) {
		readErr := errors.New("analysis reader failed")
		result, err := Analyze(context.Background(), analysisFailingReaderAt{err: readErr}, scan, Options{})
		if !errors.Is(err, readErr) {
			t.Fatalf("error = %v, want wrapped reader error", err)
		}
		if result.Complete || result.Tags.Total != 1 || result.Audio.TagCount != 1 || result.Audio.MediaTagCount != 0 {
			t.Fatalf("reader failure returned an invalid partial result: %+v", result)
		}
	})

	t.Run("full read with EOF", func(t *testing.T) {
		result, err := Analyze(context.Background(), analysisFullReadEOFReaderAt{data: data}, scan, Options{})
		if err != nil || !result.Complete || result.Audio.MediaTagCount != 1 {
			t.Fatalf("full ReaderAt read with EOF was rejected: result=%+v err=%v", result, err)
		}
	})

	for _, test := range []struct {
		name    string
		options Options
	}{
		{name: "keyframes", options: Options{MaxKeyframes: HardMaxKeyframes + 1}},
		{name: "configuration events", options: Options{MaxConfigurationEvents: HardMaxConfigurationEvents + 1}},
		{name: "configuration bytes", options: Options{MaxConfigurationBytes: HardMaxConfigurationBytes + 1}},
		{name: "diagnostics below minimum", options: Options{MaxDiagnostics: 1}},
		{name: "diagnostics above maximum", options: Options{MaxDiagnostics: HardMaxDiagnostics + 1}},
		{name: "AAC options", options: Options{AAC: aac.Options{MaxDiagnostics: 1}}},
		{name: "AVC options", options: Options{AVC: avc.Options{MaxDiagnostics: 1}}},
	} {
		t.Run("invalid option "+test.name, func(t *testing.T) {
			if _, err := Analyze(context.Background(), bytes.NewReader(data), scan, test.options); err == nil {
				t.Fatalf("invalid options accepted: %+v", test.options)
			}
		})
	}
}

type trackExpectation struct {
	tags, indexedBytes, mediaTags, mediaBytes, excluded, unclassified uint64
	timing                                                            Timeline
	bitrateBytes, bitrateDuration, bitrateBPS                         uint64
}

func assertConfigurationChangeEvents(t testing.TB, events []AACConfiguration, eventCount, analyzedCount, changeCount uint64, firstFingerprint, changedFingerprint string) {
	t.Helper()
	if eventCount != 3 || analyzedCount != 3 || changeCount != 1 || len(events) != 3 {
		t.Fatalf("unexpected AAC configuration aggregates: event=%d analyzed=%d changes=%d events=%+v", eventCount, analyzedCount, changeCount, events)
	}
	wantFingerprints := []string{firstFingerprint, firstFingerprint, changedFingerprint}
	wantCompared := []bool{false, true, true}
	wantChanged := []bool{false, false, true}
	for index := range events {
		if !events[index].Complete || events[index].FingerprintSHA256 != wantFingerprints[index] || events[index].ComparedToPrevious != wantCompared[index] || events[index].ChangedFromPrevious != wantChanged[index] {
			t.Fatalf("AAC configuration[%d] = %+v", index, events[index])
		}
	}
}

func assertAVCConfigurationChangeEvents(t testing.TB, events []AVCConfiguration, eventCount, analyzedCount, changeCount uint64, firstFingerprint, changedFingerprint string) {
	t.Helper()
	if eventCount != 3 || analyzedCount != 3 || changeCount != 1 || len(events) != 3 {
		t.Fatalf("unexpected AVC configuration aggregates: event=%d analyzed=%d changes=%d events=%+v", eventCount, analyzedCount, changeCount, events)
	}
	wantFingerprints := []string{firstFingerprint, firstFingerprint, changedFingerprint}
	wantCompared := []bool{false, true, true}
	wantChanged := []bool{false, false, true}
	for index := range events {
		if !events[index].Complete || events[index].FingerprintSHA256 != wantFingerprints[index] || events[index].ComparedToPrevious != wantCompared[index] || events[index].ChangedFromPrevious != wantChanged[index] {
			t.Fatalf("AVC configuration[%d] = %+v", index, events[index])
		}
	}
}

func assertTrack(t testing.TB, got Track, want trackExpectation) {
	t.Helper()
	if got.TagCount != want.tags || got.IndexedDataSizeBytes != want.indexedBytes || got.MediaTagCount != want.mediaTags || got.MediaDataSizeBytes != want.mediaBytes || got.ExcludedTagCount != want.excluded || got.UnclassifiedTagCount != want.unclassified {
		t.Fatalf("unexpected track counters: %+v", got)
	}
	if got.Timing != want.timing {
		t.Fatalf("timing = %+v, want %+v", got.Timing, want.timing)
	}
	wantBitrate := Bitrate{}
	if want.bitrateDuration > 0 {
		wantBitrate = Bitrate{Present: true, Basis: bitrateBasis, Bytes: want.bitrateBytes, CoveredIntervalMS: want.bitrateDuration, AverageBitsPerSecond: want.bitrateBPS}
	}
	if got.Bitrate != wantBitrate {
		t.Fatalf("bitrate = %+v, want %+v", got.Bitrate, wantBitrate)
	}
}

func hasAnalysisDiagnostic(items []Diagnostic, code string, wantTagID uint64) bool {
	return findAnalysisDiagnostic(items, code, wantTagID) != nil
}

func findAnalysisDiagnostic(items []Diagnostic, code string, wantTagID uint64) *Diagnostic {
	for _, item := range items {
		if item.Code == code && item.TagID != nil && *item.TagID == wantTagID {
			copy := item
			return &copy
		}
	}
	return nil
}

type analysisFixtureTag struct {
	rawTypeFlags byte
	dts          uint32
	payload      []byte
}

func scanAnalysisFixture(t testing.TB, tags ...analysisFixtureTag) ([]byte, flv.ScanResult) {
	t.Helper()
	data := analysisFLVBytes(tags...)
	return data, scanAnalysisBytes(t, data)
}

func scanAnalysisBytes(t testing.TB, data []byte) flv.ScanResult {
	t.Helper()
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatalf("flv.Scan: %v", err)
	}
	return scan
}

func analysisFLVBytes(tags ...analysisFixtureTag) []byte {
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, tag := range tags {
		data = appendAnalysisTag(data, tag.rawTypeFlags, tag.dts, uint32(len(tag.payload)), tag.payload, true)
	}
	return data
}

func appendIncompleteAnalysisTag(data []byte, rawTypeFlags byte, dts uint32, declared uint32, available []byte) []byte {
	return appendAnalysisTag(data, rawTypeFlags, dts, declared, available, false)
}

func appendAnalysisTag(data []byte, rawTypeFlags byte, dts, declared uint32, payload []byte, previousTagSize bool) []byte {
	headerOffset := len(data)
	data = append(data, make([]byte, flv.TagHeaderSize)...)
	header := data[headerOffset : headerOffset+int(flv.TagHeaderSize)]
	header[0] = rawTypeFlags
	header[1], header[2], header[3] = byte(declared>>16), byte(declared>>8), byte(declared)
	header[4], header[5], header[6], header[7] = byte(dts>>16), byte(dts>>8), byte(dts), byte(dts>>24)
	data = append(data, payload...)
	if previousTagSize {
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(flv.TagHeaderSize)+declared)
		data = append(data, previous[:]...)
	}
	return data
}

func analysisHex(t testing.TB, value string) []byte {
	t.Helper()
	decoded, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("hex.DecodeString: %v", err)
	}
	return decoded
}

func analysisSHA256(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

type analysisFailingReaderAt struct{ err error }

func (reader analysisFailingReaderAt) ReadAt([]byte, int64) (int, error) { return 0, reader.err }

type analysisFullReadEOFReaderAt struct{ data []byte }

func (reader analysisFullReadEOFReaderAt) ReadAt(output []byte, offset int64) (int, error) {
	if offset < 0 || offset >= int64(len(reader.data)) {
		return 0, io.EOF
	}
	n := copy(output, reader.data[offset:])
	return n, io.EOF
}
