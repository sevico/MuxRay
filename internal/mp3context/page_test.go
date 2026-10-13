package mp3context

import (
	"bytes"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func TestResolvePageAcrossPriorLongTagRetainsSourceFrameIndices(t *testing.T) {
	prior := make([][]byte, 120)
	for i := range prior {
		prior[i] = frame(0)
	}
	data, tags := fixture(t, audio(prior...), tagData{9, []byte{0x17, 2, 0, 0, 0}}, audio(frame(511)))
	reader := bytes.NewReader(data)
	p, page, err := flv.ParseMP3AudioPage(t.Context(), reader, tags[2], nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ResolvePage(t.Context(), reader, tags[2], 0, func(id uint64) (flv.TagRecord, error) { return tags[id], nil }, Limits{}, p, page)
	if err != nil || r.State != "resolved" || r.Reason != "across_tags" || len(r.Sources) != 3 || r.Sources[0].FrameIndex != 118 || r.Sources[1].FrameIndex != 119 || r.Sources[0].TagID != 0 || r.FramesInspected != 121 {
		t.Fatalf("long prior tag %+v %v", r, err)
	}
}

func TestResolvePageDoesNotSkipUnknownPriorTagTail(t *testing.T) {
	for _, test := range []struct {
		name   string
		count  int
		tail   []byte
		reason string
	}{
		{"budget", 300, nil, "frame_budget"}, {"truncated", 70, frame(0)[:100], "unparsed_audio_suffix"},
		{"free", 70, []byte{0xff, 0xfb, 0, 0x64}, "unparsed_audio_suffix"},
	} {
		t.Run(test.name, func(t *testing.T) {
			prior := append(bytes.Repeat(frame(0), test.count), test.tail...)
			data, tags := fixture(t, tagData{8, append([]byte{0x2f}, prior...)}, audio(frame(511)))
			reader := bytes.NewReader(data)
			p, page, err := flv.ParseMP3AudioPage(t.Context(), reader, tags[1], nil)
			if err != nil {
				t.Fatal(err)
			}
			r, err := ResolvePage(t.Context(), reader, tags[1], 0, func(id uint64) (flv.TagRecord, error) { return tags[id], nil }, Limits{}, p, page)
			if err != nil || r.State != "unknown" || r.Reason != test.reason || r.ResolvedPreviousBytes != 0 || len(r.Sources) != 1 || r.FramesInspected > MaxFrames {
				t.Fatalf("tail %+v %v", r, err)
			}
		})
	}
}

func TestResolvePageRetainsParameterBarrierAcrossPage(t *testing.T) {
	frames := make([][]byte, 66)
	for i := range frames {
		frames[i] = frame(0)
	}
	frames[63][3] = 0xc4 // Mono immediately before stereo target at page start.
	frames[64] = frame(511)
	data, tags := fixture(t, audio(frames...))
	reader := bytes.NewReader(data)
	_, first, err := flv.ParseMP3AudioPage(t.Context(), reader, tags[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	p, page, err := flv.ParseMP3AudioPage(t.Context(), reader, tags[0], first.Next)
	if err != nil {
		t.Fatal(err)
	}
	r, err := ResolvePage(t.Context(), reader, tags[0], 64, func(id uint64) (flv.TagRecord, error) { return tags[id], nil }, Limits{}, p, page)
	if err != nil || r.State != "unknown" || r.Reason != "parameters_changed" || r.ResolvedPreviousBytes != 0 {
		t.Fatalf("barrier %+v %v", r, err)
	}
	if p.Frames.Frames[0].Dependency.State != "unknown" {
		t.Fatal("page main-data history crossed parameter barrier")
	}
	if first.Next.NextFrameIndex() != mp3.FramePageSize {
		t.Fatal("checkpoint mutated")
	}
}
