package summaryview

import (
	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

func DesktopOptionsV3() analysis.OptionsV3 {
	return analysis.OptionsV3{OptionsV2: DesktopOptions(), MaxMP3FrameEvents: 256, MaxMP3Frames: analysis.DefaultMaxMP3Frames}
}

// ProjectV3 retains the existing interval/curve facts and replaces the desktop
// MP3 event list with frame-level observations. Its generic wire shape stays
// compatible; the mp3Frames section identifies the newer analysis explicitly.
func ProjectV3(scan flv.ScanResult, result analysis.ResultV3) (*Report, error) {
	p, err := Project(scan, result.ResultV2)
	if err != nil {
		return nil, err
	}
	f := result.MP3Frames
	if len(f.Events) > int(DesktopOptionsV3().MaxMP3FrameEvents) {
		return nil, ErrInvalidProjection
	}
	metrics := p.Overview.Metrics[:0]
	for _, m := range p.Overview.Metrics {
		if m.Section != "mp3Headers" {
			metrics = append(metrics, m)
		}
	}
	for _, v := range []struct {
		name  string
		value uint64
		unit  string
	}{
		{"completeFrameCount", f.CompleteFrameCount, "frames"},
		{"attemptedHeaderCount", f.AttemptedHeaderCount, "headers"},
		{"sampleCount", f.SampleCount, "samples"},
		{"frameBytes", f.FrameBytes, "byte"}, {"unresolvedBytes", f.UnresolvedBytes, "byte"},
		{"parameterChangeCount", f.ParameterChangeCount, "count"}, {"bitrateChangeCount", f.BitrateChangeCount, "count"},
		{"completeTagCount", f.CompleteTagCount, "tags"}, {"partialTagCount", f.PartialTagCount, "tags"},
		{"freeFormatCount", f.FreeFormatCount, "headers"}, {"truncatedHeaderCount", f.TruncatedHeaderCount, "headers"},
		{"malformedHeaderCount", f.MalformedHeaderCount, "headers"}, {"truncatedFrameCount", f.TruncatedFrameCount, "frames"},
		{"unsupportedLayerCount", f.UnsupportedLayerCount, "headers"}, {"budgetStoppedTagCount", f.BudgetStoppedTagCount, "tags"},
		{"maxFrames", f.MaxFrames, "headers"},
	} {
		metrics = append(metrics, count("mp3Frames", v.name, v.value, v.unit))
	}
	metrics = append(metrics, metric("mp3Frames", "complete", pointer(boolean(f.Complete)), ""),
		metric("mp3Frames", "scanComplete", pointer(boolean(f.ScanComplete)), ""),
		metric("mp3Frames", "durationSeconds", pointer(number(f.DurationSeconds.Numerator)+"/"+number(f.DurationSeconds.Denominator)), "s"))
	p.Overview.Metrics = metrics
	p.Overview.Complete = result.Complete && f.Complete
	p.Overview.HasErrors = result.HasErrors()
	p.Events["mp3"] = []Event{}
	for i, frame := range f.Events {
		e := event(i, frame.TagID, frame.SourceSpan.ByteOffset, frame.DTS, "mp3Frame")
		e.BitOffset = frame.SourceSpan.BitOffset
		e.Message = "Sequential frame observation; a stop spans the unresolved Tag suffix. DTS belongs to its FLV Tag. Payload is not decoded."
		e.Values = []Metric{
			count("", "frameIndex", uint64(frame.FrameIndex), "index"), metric("", "frameStatus", pointer(frame.Status), ""),
			count("", "sourceBitLength", uint64(frame.SourceSpan.BitLength), "bit"),
			metric("", "version", optional(frame.ParametersPresent, frame.Parameters.Version.String()), ""),
			metric("", "channelMode", optional(frame.ParametersPresent, frame.Parameters.ChannelMode.String()), ""),
			metric("", "samplingFrequencyHz", optional(frame.ParametersPresent, number(uint64(frame.Parameters.SamplingFrequencyHz))), "Hz"),
			metric("", "channels", optional(frame.ParametersPresent, number(uint64(frame.Parameters.Channels))), "count"),
			metric("", "samplesPerFrame", optional(frame.ParametersPresent, number(uint64(frame.Parameters.SamplesPerFrame))), "samples"),
			metric("", "bitrateKbps", optional(frame.BitratePresent, number(uint64(frame.BitrateKbps))), "kbps"),
			metric("", "parametersChangedFromPrevious", optional(frame.ComparedToPrevious, boolean(frame.ParametersChangedFromPrevious)), ""),
			metric("", "bitrateChangedFromPrevious", optional(frame.ComparedToPrevious, boolean(frame.BitrateChangedFromPrevious)), ""),
		}
		p.Events["mp3"] = append(p.Events["mp3"], e)
	}
	for i, list := range p.Overview.Lists {
		if list.Kind == "mp3" {
			p.Overview.Lists[i] = ListInfo{Kind: "mp3", Observed: pointer(number(f.EventCount)), Retained: number(uint64(len(f.Events))), Truncated: f.EventsTruncated}
		}
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}
