package summaryview

import (
	"strconv"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func number(n uint64) string   { return strconv.FormatUint(n, 10) }
func signed(n int64) string    { return strconv.FormatInt(n, 10) }
func pointer(s string) *string { return &s }
func optional(ok bool, value string) *string {
	if !ok {
		return nil
	}
	return pointer(value)
}
func boolean(ok bool) string { return strconv.FormatBool(ok) }
func metric(section, name string, value *string, unit string) Metric {
	return Metric{section, name, value, unit}
}
func count(section, name string, n uint64, unit string) Metric {
	return metric(section, name, pointer(number(n)), unit)
}

// Project accepts a successfully completed analysis, including a complete
// analysis of a partial scan. Callers must discard ResultV2 when Analyze errs.
func Project(scan flv.ScanResult, r analysis.ResultV2) (*Report, error) {
	o := DesktopOptions()
	if len(r.Keyframes.Entries) > int(o.Options.MaxKeyframes) || len(r.AAC.Events) > int(o.Options.MaxConfigurationEvents) || len(r.AVC.Events) > int(o.Options.MaxConfigurationEvents) || len(r.MP3.Headers.Events) > int(o.MaxMP3HeaderEvents) || len(r.Diagnostics) > int(o.Options.MaxDiagnostics) {
		return nil, ErrInvalidProjection
	}
	p := &Report{Overview: Overview{SourceSize: number(scan.SourceSize), IndexedTags: number(scan.Index().Len()), ScanComplete: scan.Complete, ScanHasErrors: scan.HasErrors(), Complete: r.Complete, HasErrors: r.HasErrors(), Metrics: []Metric{}, Lists: []ListInfo{}}, Events: map[string][]Event{}}
	m := &p.Overview.Metrics
	*m = append(*m, count("tags", "total", r.Tags.Total, "tags"), count("tags", "audio", r.Tags.Audio, "tags"), count("tags", "video", r.Tags.Video, "tags"), count("tags", "script", r.Tags.Script, "tags"), count("tags", "other", r.Tags.Other, "tags"), count("tags", "dataSizeBytes", r.Tags.DataSizeBytes, "byte"))
	*m = append(*m, count("audioCodecs", "aacTagCount", r.AudioCodecs.AACTagCount, "tags"), count("audioCodecs", "mp3TagCount", r.AudioCodecs.MP3TagCount, "tags"), count("audioCodecs", "otherTagCount", r.AudioCodecs.OtherTagCount, "tags"), count("audioCodecs", "unknownTagCount", r.AudioCodecs.UnknownTagCount, "tags"), count("audioCodecs", "switchCount", r.AudioCodecs.SwitchCount, "count"))
	track(m, "audio", r.Audio)
	track(m, "video", r.Video)
	track(m, "mp3", r.MP3.Track)
	timeline(m, "videoPts", r.VideoPTS)
	*m = append(*m, count("keyframes", "declaredCount", r.Keyframes.DeclaredCount, "tags"), count("gop", "count", r.GOP.Count, "count"), count("gop", "closedDurationCount", r.GOP.ClosedDurationCount, "count"), count("gop", "nonPositiveDurationCount", r.GOP.NonPositiveDurationCount, "count"))
	for _, v := range []struct {
		name string
		n    uint64
	}{{"minDurationMs", r.GOP.MinDurationMS}, {"maxDurationMs", r.GOP.MaxDurationMS}, {"averageDurationMs", r.GOP.AverageDurationMS}} {
		*m = append(*m, metric("gop", v.name, optional(r.GOP.ClosedDurationCount > 0, number(v.n)), "ms"))
	}
	*m = append(*m, count("aac", "eventCount", r.AAC.EventCount, "count"), count("aac", "analyzedCount", r.AAC.AnalyzedCount, "count"), count("aac", "skippedByBudgetCount", r.AAC.SkippedByBudgetCount, "count"), count("aac", "changeCount", r.AAC.ChangeCount, "count"), count("avc", "eventCount", r.AVC.EventCount, "count"), count("avc", "analyzedCount", r.AVC.AnalyzedCount, "count"), count("avc", "skippedByBudgetCount", r.AVC.SkippedByBudgetCount, "count"), count("avc", "changeCount", r.AVC.ChangeCount, "count"))
	h := r.MP3.Headers
	*m = append(*m, count("mp3Headers", "eventCount", h.EventCount, "count"), count("mp3Headers", "validCount", h.ValidCount, "count"), count("mp3Headers", "malformedCount", h.MalformedCount, "count"), count("mp3Headers", "freeFormatCount", h.FreeFormatCount, "count"), count("mp3Headers", "parameterChangeCount", h.ParameterChangeCount, "count"), count("mp3Headers", "bitrateChangeCount", h.BitrateChangeCount, "count"))
	c := r.Composition
	for _, v := range []struct {
		name string
		n    uint64
	}{{"sampleCount", c.SampleCount}, {"negativeCount", c.NegativeCount}, {"zeroCount", c.ZeroCount}, {"positiveCount", c.PositiveCount}} {
		*m = append(*m, count("composition", v.name, v.n, "tags"))
	}
	*m = append(*m, metric("composition", "minMs", optional(c.Present, signed(int64(c.MinMS))), "ms"), metric("composition", "maxMs", optional(c.Present, signed(int64(c.MaxMS))), "ms"))
	a := r.AVSync
	*m = append(*m, metric("avSync", "startExtremaSkewMs", optional(a.Present, signed(a.StartExtremaSkewMS)), "ms"), metric("avSync", "endExtremaSkewMs", optional(a.Present, signed(a.EndExtremaSkewMS)), "ms"), metric("avSync", "rangesOverlap", optional(a.Present, boolean(a.RangesOverlap)), ""), metric("avSync", "overlapMs", optional(a.Present, number(a.OverlapMS)), "ms"))
	for _, kind := range []string{"keyframes", "aac", "avc", "mp3", "diagnostics"} {
		p.Events[kind] = []Event{}
	}
	for i, v := range r.Keyframes.Entries {
		e := event(i, v.TagID, v.Offset, v.DTS, "declaredKeyframe")
		e.Values = []Metric{metric("", "pts", optional(v.PTSPresent, signed(v.PTS)), "ms"), count("", "dataSizeBytes", uint64(v.DataSize), "byte"), metric("", "gopDurationMs", optional(v.GOPDurationPresent, number(v.GOPDurationMS)), "ms")}
		p.Events["keyframes"] = append(p.Events["keyframes"], e)
	}
	for i, v := range r.AAC.Events {
		e := event(i, v.TagID, v.Offset, v.DTS, "aacConfiguration")
		e.Values = []Metric{metric("", "complete", pointer(boolean(v.Complete)), ""), metric("", "fingerprintSHA256", pointer(v.FingerprintSHA256), ""), metric("", "changedFromPrevious", optional(v.ComparedToPrevious, boolean(v.ChangedFromPrevious)), ""), metric("", "audioObjectType", optional(v.AudioObjectTypePresent, v.AudioObjectTypeName), ""), metric("", "samplingFrequencyHz", optional(v.SampleRatePresent, number(uint64(v.SamplingFrequencyHz))), "Hz"), metric("", "extensionSamplingFrequencyHz", optional(v.ExtensionSamplingFrequencyPresent, number(uint64(v.ExtensionSamplingFrequencyHz))), "Hz"), metric("", "channelConfiguration", optional(v.ChannelConfigurationPresent, v.ChannelConfigurationName), ""), metric("", "channels", optional(v.ChannelCountPresent, number(uint64(v.ChannelCount))), "count")}
		p.Events["aac"] = append(p.Events["aac"], e)
	}
	for i, v := range r.AVC.Events {
		e := event(i, v.TagID, v.Offset, v.DTS, "avcConfiguration")
		e.Values = []Metric{metric("", "complete", pointer(boolean(v.Complete)), ""), metric("", "fingerprintSHA256", pointer(v.FingerprintSHA256), ""), metric("", "changedFromPrevious", optional(v.ComparedToPrevious, boolean(v.ChangedFromPrevious)), ""), metric("", "profileIDC", optional(v.ProfilePresent, number(uint64(v.ProfileIDC))), ""), metric("", "levelIDC", optional(v.ProfilePresent, number(uint64(v.LevelIDC))), ""), metric("", "lengthSize", optional(v.LengthSizePresent, number(uint64(v.LengthSize))), "byte"), count("", "completeSPSCount", uint64(v.CompleteSPSCount), "count"), count("", "distinctSPSDimensions", uint64(v.DistinctSPSDimensions), "count"), metric("", "displayWidth", optional(v.FirstCompleteSPSPresent, number(v.DisplayWidth)), "px"), metric("", "displayHeight", optional(v.FirstCompleteSPSPresent, number(v.DisplayHeight)), "px")}
		p.Events["avc"] = append(p.Events["avc"], e)
	}
	for i, v := range h.Events {
		e := event(i, v.TagID, v.Offset, v.DTS, "mp3Header")
		e.Values = []Metric{metric("", "complete", pointer(boolean(v.Complete)), ""), metric("", "version", optional(v.Complete, v.Parameters.Version.String()), ""), metric("", "channelMode", optional(v.Complete, v.Parameters.ChannelMode.String()), ""), metric("", "samplingFrequencyHz", optional(v.Complete, number(uint64(v.Parameters.SamplingFrequencyHz))), "Hz"), metric("", "channels", optional(v.Complete, number(uint64(v.Parameters.Channels))), "count"), metric("", "samplesPerFrame", optional(v.Complete, number(uint64(v.Parameters.SamplesPerFrame))), "count"), metric("", "bitrateMode", optional(v.Complete, string(v.BitrateMode)), ""), metric("", "bitrateKbps", optional(v.Complete && v.BitrateMode == mp3.BitrateModeIndexed, number(uint64(v.BitrateKbps))), "kbps"), metric("", "parametersChangedFromPrevious", optional(v.ComparedToPrevious, boolean(v.ParametersChangedFromPrevious)), ""), metric("", "bitrateChangedFromPrevious", optional(v.ComparedToPrevious, boolean(v.BitrateChangedFromPrevious)), "")}
		p.Events["mp3"] = append(p.Events["mp3"], e)
	}
	diagnosticsTruncated := false
	for i, v := range r.Diagnostics {
		e := Event{ID: number(uint64(i)), Offset: number(v.Offset), BitOffset: v.BitOffset, Title: v.Code, Severity: string(v.Severity), Message: v.Message, Values: []Metric{}}
		if v.TagID != nil {
			e.TagID = pointer(number(*v.TagID))
		}
		for _, x := range []struct {
			name string
			n    *uint64
		}{{"expected", v.Expected}, {"actual", v.Actual}, {"recoveryOffset", v.RecoveryOffset}} {
			var value *string
			if x.n != nil {
				value = pointer(number(*x.n))
			}
			e.Values = append(e.Values, metric("", x.name, value, ""))
		}
		p.Events["diagnostics"] = append(p.Events["diagnostics"], e)
		diagnosticsTruncated = diagnosticsTruncated || v.Code == analysis.CodeDiagnosticLimitReached
	}
	for _, l := range []struct {
		kind      string
		observed  uint64
		truncated bool
	}{{"keyframes", r.Keyframes.DeclaredCount, r.Keyframes.Truncated}, {"aac", r.AAC.EventCount, r.AAC.Truncated}, {"avc", r.AVC.EventCount, r.AVC.Truncated}, {"mp3", h.EventCount, h.Truncated}, {"diagnostics", uint64(len(r.Diagnostics)), diagnosticsTruncated}} {
		observed := pointer(number(l.observed))
		if l.kind == "diagnostics" && l.truncated {
			observed = nil
		}
		p.Overview.Lists = append(p.Overview.Lists, ListInfo{l.kind, observed, number(uint64(len(p.Events[l.kind]))), l.truncated})
	}
	if err := p.validate(); err != nil {
		return nil, err
	}
	return p, nil
}

func event(i int, tag, offset uint64, dts int64, title string) Event {
	return Event{ID: number(uint64(i)), TagID: pointer(number(tag)), Offset: number(offset), DTS: pointer(signed(dts)), Title: title, Values: []Metric{}}
}
func track(out *[]Metric, section string, t analysis.Track) {
	*out = append(*out, count(section, "tagCount", t.TagCount, "tags"), count(section, "indexedDataSizeBytes", t.IndexedDataSizeBytes, "byte"), count(section, "mediaTagCount", t.MediaTagCount, "tags"), count(section, "mediaDataSizeBytes", t.MediaDataSizeBytes, "byte"), count(section, "excludedTagCount", t.ExcludedTagCount, "tags"), count(section, "unclassifiedTagCount", t.UnclassifiedTagCount, "tags"))
	timeline(out, section+"Timing", t.Timing)
	b := t.Bitrate
	*out = append(*out, metric(section+"Bitrate", "basis", optional(b.Present, b.Basis), ""), metric(section+"Bitrate", "coveredBytes", optional(b.Present, number(b.Bytes)), "byte"), metric(section+"Bitrate", "coveredIntervalMs", optional(b.Present, number(b.CoveredIntervalMS)), "ms"), metric(section+"Bitrate", "averageBitsPerSecond", optional(b.Present, number(b.AverageBitsPerSecond)), "bps"))
}
func timeline(out *[]Metric, section string, t analysis.Timeline) {
	*out = append(*out, count(section, "sampleCount", t.SampleCount, "tags"))
	for _, v := range []struct {
		name string
		n    int64
	}{{"firstMs", t.FirstMS}, {"lastMs", t.LastMS}, {"minMs", t.MinMS}, {"maxMs", t.MaxMS}} {
		*out = append(*out, metric(section, v.name, optional(t.Present, signed(v.n)), "ms"))
	}
	for _, v := range []struct {
		name string
		n    uint64
		unit string
	}{{"rangeMs", t.RangeMS, "ms"}, {"forwardDeltaTotalMs", t.ForwardDeltaTotalMS, "ms"}, {"regressionCount", t.RegressionCount, "count"}, {"duplicateCount", t.DuplicateCount, "count"}, {"largestForwardGapMs", t.LargestForwardGapMS, "ms"}} {
		*out = append(*out, metric(section, v.name, optional(t.Present, number(v.n)), v.unit))
	}
}
