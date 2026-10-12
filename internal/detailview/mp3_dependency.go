package detailview

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3context"
)

// ParseWithMP3Dependency is an explicitly requested cross-Tag query. It reuses
// the resolver's selected-Tag parse, so source data is not parsed twice. The
// caller owns source identity validation and the lifetime of the lookup.
func ParseWithMP3Dependency(ctx context.Context, source io.ReaderAt, tag flv.TagRecord, index uint32, lookup mp3context.Lookup, limits mp3context.Limits) (*Detail, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	r, err := mp3context.Resolve(ctx, source, tag, index, lookup, limits)
	if errors.Is(err, mp3context.ErrInvalidInput) {
		return nil, ErrInvalidInput
	}
	if err != nil {
		return nil, err
	}
	if r.Selected != nil && !selectableMP3Frame(r.Selected.Frames, index) {
		if r.Reason != "frame_budget" {
			return nil, ErrInvalidInput
		}
		// The bounded parser did not reach this target. Publish only the
		// dependency query identity, never a nonexistent selected frame.
		r.Selected = nil
	}
	if r.Selected == nil {
		switch r.Reason {
		case "audio_codec_changed", "audio_header_missing":
			return nil, ErrInvalidInput
		case "filtered_audio":
			return nil, ErrFiltered
		case "truncated_audio":
			return nil, ErrTruncated
		}
	}
	b := builder{detail: Detail{Kind: "mp3Audio", Fields: []Field{}, Diagnostics: []Diagnostic{}}}
	if r.Selected != nil {
		b.detail.Complete = r.Selected.Complete
		for _, d := range r.Selected.Diagnostics {
			b.diagnostic(d.Code, string(d.Severity), d.Offset, d.BitOffset, d.Message, d.Expected, d.Actual)
		}
	}
	b.add("", "tagId", "uint64", decimal(tag.ID), nil)
	// The bounded source table is the requested result. Keep it ahead of the
	// ordinary frame summaries, which can consume the remaining field budget.
	b.mp3Dependency(r)
	if r.Selected != nil {
		b.mp3(*r.Selected, &index)
	}
	if strings.HasSuffix(r.Reason, "budget") || r.Reason == "source_limit" {
		b.detail.Truncated = true
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &b.detail, nil
}

func (b *builder) mp3Dependency(r mp3context.Resolution) {
	id := b.add("", "MP3 dependency sources", "bounded cross-tag dependency", r.State, nil)
	b.add(id, "dependencyState", "derived enum", r.State, nil)
	b.add(id, "dependencyReason", "derived enum", r.Reason, nil)
	for _, item := range []struct {
		name  string
		value uint64
	}{
		{"targetTagId", r.TargetTagID}, {"targetFrameIndex", uint64(r.TargetFrameIndex)},
		{"resolvedPreviousBytes", uint64(r.ResolvedPreviousBytes)}, {"sourceCount", uint64(len(r.Sources))},
		{"scannedTags", r.ScannedTags}, {"audioTags", r.AudioTags}, {"framesInspected", uint64(r.FramesInspected)},
		{"coverageBytes", r.CoverageBytes}, {"bytesRead", r.BytesRead},
		{"tagBudget", r.Limits.MaxTags}, {"audioTagBudget", r.Limits.MaxAudioTags},
		{"frameBudget", uint64(r.Limits.MaxFrames)}, {"coverageBudget", r.Limits.MaxBytes},
		{"readBudget", r.Limits.MaxReadBytes}, {"sourceBudget", uint64(r.Limits.MaxSources)},
	} {
		b.add(id, item.name, "derived count", decimal(item.value), nil)
	}
	for _, item := range []struct {
		name  string
		value *uint32
	}{
		{"requestedPreviousBytes", r.RequestedPreviousBytes}, {"missingPreviousBytes", r.MissingPreviousBytes}, {"part23Bits", r.Part23Bits},
	} {
		value := "unknown"
		if item.value != nil {
			value = decimal(uint64(*item.value))
		}
		b.add(id, item.name, "derived count or unknown", value, nil)
	}
	value := "unknown"
	if r.AvailableBits != nil {
		value = decimal(*r.AvailableBits)
	}
	b.add(id, "availableBits", "derived count or unknown", value, nil)
	for i, source := range r.Sources {
		row := b.add(id, "Source "+decimal(uint64(i)), "physical main-data source", "Physical slot range; may include previously consumed or ancillary bytes", nil)
		b.add(row, "sourceTagId", "derived reference", decimal(source.TagID), nil)
		b.add(row, "sourceFrameIndex", "derived reference", decimal(uint64(source.FrameIndex)), nil)
		b.add(row, "sourceCurrent", "derived boolean", strconv.FormatBool(source.Current), nil)
		b.add(row, "sourceSpan", "physical source bytes", decimal(uint64(source.Span.BitLength/8))+" bytes", span(source.Span))
	}
	b.add(id, "dependencyScope", "scope", "Physical main_data_begin window plus current slots; unknown retains only a known suffix. No reservoir consumption or audio decoding is verified. Coverage budget charges whole SoundData ranges; read bytes count actual I/O.", nil)
}
