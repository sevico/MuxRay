package analysis

import (
	"context"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/flv"
)

// CurveWindowTags groups consecutive source Tags, not fixed time intervals or
// decoded frames. The scanner's hard limit permits at most 3,907 points.
const CurveWindowTags uint64 = 256

// MediaCurves covers the entire supplied index, including a partial scan's
// committed prefix. Scan and analysis completeness remain in ResultV2.
type MediaCurves struct {
	IndexedTags uint64
	Points      []MediaCurvePoint
}

// MediaCurvePoint assigns covered intervals to the window containing their
// closing Tag. Their bytes or starting keyframe may belong to an earlier window.
// NextTag is exclusive; empty-duration metrics are unknown, not zero rates.
type MediaCurvePoint struct {
	Index, StartTag, NextTag uint64
	Audio, Video, MP3        MediaCurveRate
	GOP                      MediaCurveGOP
	UnclassifiedTags         uint64
}

// MediaCurveRate uses the same covered FLV DataSize bytes and forward DTS
// intervals as the summary. Audio includes MP3; MP3 is its codec-specific subset.
// TagID is the first closing Tag in this window and is valid only when Present.
type MediaCurveRate struct {
	CoveredBytes, CoveredIntervalMS uint64
	BitsPerSecond                   uint64
	Present                         bool
	TagID                           uint64
}

// MediaCurveGOP counts FLV-declared keyframes, not verified IDR pictures. TagID
// closes the longest positive GOP in this window (first on ties), and is valid
// only when Present. The last, unclosed GOP never gains an inferred duration.
type MediaCurveGOP struct {
	DeclaredCount, ClosedCount, TotalDurationMS uint64
	MinDurationMS, MaxDurationMS                uint64
	AverageDurationMS                           uint64
	Present                                     bool
	TagID                                       uint64
}

// AnalyzeV2WithCurves collects bounded curves during the existing analysis pass.
// It performs no additional source reads and preserves all classification and
// coverage barriers. Errors, including cancellation, never publish partial curves.
func AnalyzeV2WithCurves(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options OptionsV2, progress func(Progress)) (ResultV2, *MediaCurves, error) {
	normalized, err := options.Normalized()
	if err != nil {
		return ResultV2{}, nil, err
	}
	total := scan.Index().Len()
	if total > flv.HardMaxTags {
		return ResultV2{}, nil, fmt.Errorf("analysis: curve index exceeds hard tag limit %d", flv.HardMaxTags)
	}
	collector := mediaCurveCollector{result: MediaCurves{
		IndexedTags: total,
		Points:      make([]MediaCurvePoint, 0, int((total+CurveWindowTags-1)/CurveWindowTags)),
	}}
	state := summaryV2State{headerLimit: normalized.MaxMP3HeaderEvents, curves: &collector}
	base, err := analyze(ctx, source, scan, normalized.Options, &state, progress)
	result := ResultV2{Result: base, AudioCodecs: state.audioCodecs, MP3: state.mp3}
	// The final progress callback may cancel after the last visit has completed.
	if err == nil && ctx != nil && ctx.Err() != nil {
		result.Complete = false
		err = ctx.Err()
	}
	if err != nil {
		return result, nil, err
	}
	collector.finish()
	return result, &collector.result, nil
}

type curveRateTotals struct {
	bytes, intervalMS uint64
}

type mediaCurveCollector struct {
	result                       MediaCurves
	audio, video, mp3            curveRateTotals
	declared, closed, durationMS uint64
	unclassified                 uint64
}

func (c *mediaCurveCollector) observe(a *analyzer, index, tagID uint64) {
	if index%CurveWindowTags == 0 {
		c.result.Points = append(c.result.Points, MediaCurvePoint{Index: index / CurveWindowTags, StartTag: index})
	}
	point := &c.result.Points[len(c.result.Points)-1]
	point.NextTag = index + 1
	point.Audio.observe(a.audioBitrate, &c.audio, tagID)
	point.Video.observe(a.videoBitrate, &c.video, tagID)
	point.MP3.observe(a.v2.mp3Bitrate, &c.mp3, tagID)
	point.GOP.DeclaredCount += a.result.GOP.Count - c.declared
	if count := a.result.GOP.ClosedDurationCount - c.closed; count != 0 {
		// One successful Tag visit can close at most one declared GOP.
		duration := a.gopDurationTotal - c.durationMS
		gop := &point.GOP
		if !gop.Present || duration < gop.MinDurationMS {
			gop.MinDurationMS = duration
		}
		if !gop.Present || duration > gop.MaxDurationMS {
			gop.MaxDurationMS, gop.TagID = duration, tagID
		}
		gop.Present = true
		gop.ClosedCount += count
		gop.TotalDurationMS += duration
	}
	c.declared, c.closed, c.durationMS = a.result.GOP.Count, a.result.GOP.ClosedDurationCount, a.gopDurationTotal
	unclassified := a.result.Audio.UnclassifiedTagCount + a.result.Video.UnclassifiedTagCount
	point.UnclassifiedTags += unclassified - c.unclassified
	c.unclassified = unclassified
}

func (r *MediaCurveRate) observe(total bitrateAccumulator, previous *curveRateTotals, tagID uint64) {
	interval := total.coveredIntervalMS - previous.intervalMS
	if interval != 0 {
		if !r.Present {
			r.Present, r.TagID = true, tagID
		}
		r.CoveredBytes += total.committedBytes - previous.bytes
		r.CoveredIntervalMS += interval
	}
	previous.bytes, previous.intervalMS = total.committedBytes, total.coveredIntervalMS
}

func (c *mediaCurveCollector) finish() {
	for i := range c.result.Points {
		point := &c.result.Points[i]
		for _, rate := range []*MediaCurveRate{&point.Audio, &point.Video, &point.MP3} {
			if rate.Present {
				rate.BitsPerSecond = rate.CoveredBytes * 8_000 / rate.CoveredIntervalMS
			}
		}
		if point.GOP.Present {
			point.GOP.AverageDurationMS = point.GOP.TotalDurationMS / point.GOP.ClosedCount
		}
	}
}
