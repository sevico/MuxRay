package detailview

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3context"
)

func dependencyFixture(t *testing.T, payloads ...[]byte) ([]byte, []flv.TagRecord) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, payload := range payloads {
		header := make([]byte, 11)
		header[0], header[1], header[2], header[3] = 8, byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
		data = append(data, header...)
		data = append(data, payload...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(payload)))
		data = append(data, size[:]...)
	}
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || !scan.Complete {
		t.Fatalf("scan: %+v %v", scan, err)
	}
	return data, scan.Tags
}

type dependencyReads struct {
	io.ReaderAt
	calls, bytes int
}

func (r *dependencyReads) ReadAt(p []byte, off int64) (int, error) {
	n, err := r.ReaderAt.ReadAt(p, off)
	r.calls++
	r.bytes += n
	return n, err
}

func TestMP3DependencyProjectionReusesSelectedParseAndKeepsSources(t *testing.T) {
	frame := sideInfoViewFrame(t)
	payload := append([]byte{0x2f}, frame...)
	data, tags := dependencyFixture(t, payload, payload)
	lookup := func(id uint64) (flv.TagRecord, error) { return tags[id], nil }
	coreReads := &dependencyReads{ReaderAt: bytes.NewReader(data)}
	r, err := mp3context.Resolve(t.Context(), coreReads, tags[1], 0, lookup, mp3context.Limits{})
	if err != nil || r.State != "resolved" {
		t.Fatalf("resolve: %+v %v", r, err)
	}
	detailReads := &dependencyReads{ReaderAt: bytes.NewReader(data)}
	d, err := ParseWithMP3Dependency(t.Context(), detailReads, tags[1], 0, lookup, mp3context.Limits{})
	if err != nil || d == nil || detailReads.calls != coreReads.calls || detailReads.bytes != coreReads.bytes {
		t.Fatalf("repeated source parse: %v core=%+v detail=%+v", err, coreReads, detailReads)
	}
	group := find(t, d, "MP3 dependency sources")
	if group.Type != "bounded cross-tag dependency" || group.Value != "resolved" || find(t, d, "targetTagId").Value != "1" || find(t, d, "selectedFrameIndex").Value != "0" || find(t, d, "sourceCount").Value != "2" || find(t, d, "requestedPreviousBytes").Value != "200" || find(t, d, "missingPreviousBytes").Value != "0" {
		t.Fatalf("identity or state: %+v", d)
	}
	rows := 0
	for _, f := range d.Fields {
		if f.Type != "physical main-data source" {
			continue
		}
		if f.ParentID != group.ID {
			t.Fatal("source escaped its group")
		}
		values := map[string]Field{}
		for _, child := range d.Fields {
			if child.ParentID == f.ID {
				values[child.Name] = child
			}
		}
		want := r.Sources[rows]
		if values["sourceTagId"].Value != strconv.FormatUint(want.TagID, 10) || values["sourceFrameIndex"].Value != "0" || values["sourceCurrent"].Value != strconv.FormatBool(want.Current) || values["sourceSpan"].Span == nil || *values["sourceSpan"].Span != *span(want.Span) {
			t.Fatalf("wrong source row: %+v want %+v", values, want)
		}
		rows++
	}
	if rows != len(r.Sources) {
		t.Fatal("omitted sources")
	}
	assertBounded(t, d)
	for _, oldParse := range []func() (*Detail, error){
		func() (*Detail, error) { return Parse(t.Context(), bytes.NewReader(data), tags[1]) },
		func() (*Detail, error) { return ParseWithMP3Frame(t.Context(), bytes.NewReader(data), tags[1], 0) },
	} {
		old, err := oldParse()
		if err != nil || optionalField(old, "MP3 dependency sources") != nil {
			t.Fatal("cross-Tag query changed an old entry point")
		}
	}
}

func TestMP3DependencyMaximumSourcesSurviveProjectionBudget(t *testing.T) {
	// MPEG-2, 8 kbit/s, 24 kHz stereo has three physical main-data bytes
	// per 24-byte frame. 189 backward bytes need exactly 63 prior slots.
	frame := make([]byte, 24)
	copy(frame, []byte{0xff, 0xf3, 0x14, 0x64})
	prior := append([]byte{0x2f}, bytes.Repeat(frame, 63)...)
	frame[4] = 189
	current := append([]byte{0x2f}, frame...)
	data, tags := dependencyFixture(t, prior, current)
	lookup := func(id uint64) (flv.TagRecord, error) { return tags[id], nil }
	d, err := ParseWithMP3Dependency(t.Context(), bytes.NewReader(data), tags[1], 0, lookup, mp3context.Limits{})
	if err != nil || find(t, d, "sourceCount").Value != "64" || find(t, d, "dependencyState").Value != "resolved" || find(t, d, "resolvedPreviousBytes").Value != "189" {
		t.Fatalf("max sources: %+v %v", d, err)
	}
	rows, spans := 0, 0
	for _, f := range d.Fields {
		if f.Type == "physical main-data source" {
			rows++
		}
		if f.Name == "sourceSpan" && f.Span != nil && f.Span.BitLength == 24 {
			spans++
		}
	}
	if rows != 64 || spans != 64 || find(t, d, "selectedFrameIndex").Value != "0" || find(t, d, "dependencyScope").Value == "" {
		t.Fatal("field budget omitted mandatory source metadata")
	}
	assertBounded(t, d)
	// A 190-byte window would need 65 total rows. Keep a known suffix and
	// expose source_limit instead of dropping an unreported physical range.
	prior = append([]byte{0x2f}, bytes.Repeat(append([]byte{0xff, 0xf3, 0x14, 0x64}, make([]byte, 20)...), 64)...)
	frame[4] = 190
	data, tags = dependencyFixture(t, prior, append([]byte{0x2f}, frame...))
	d, err = ParseWithMP3Dependency(t.Context(), bytes.NewReader(data), tags[1], 0, lookup, mp3context.Limits{})
	if err != nil || find(t, d, "sourceCount").Value != "64" || find(t, d, "dependencyReason").Value != "source_limit" || find(t, d, "missingPreviousBytes").Value != "1" || !d.Truncated {
		t.Fatalf("source limit: %+v %v", d, err)
	}
	assertBounded(t, d)
}

func TestMP3DependencyBudgetBeforeParseAndCancellation(t *testing.T) {
	data, tag := fixture(t, 8, append([]byte{0x2f}, sideInfoViewFrame(t)...))
	lookup := func(uint64) (flv.TagRecord, error) { t.Fatal("budget queried history"); return flv.TagRecord{}, nil }
	d, err := ParseWithMP3Dependency(t.Context(), bytes.NewReader(data), tag, 0, lookup, mp3context.Limits{MaxBytes: 1})
	if err != nil || d.Complete || !d.Truncated || find(t, d, "dependencyReason").Value != "coverage_budget" || find(t, d, "targetFrameIndex").Value != "0" || find(t, d, "requestedPreviousBytes").Value != "unknown" || optionalField(d, "Frame inspection") != nil {
		t.Fatalf("pre-parse budget: %+v %v", d, err)
	}
	assertBounded(t, d)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got, err := ParseWithMP3Dependency(ctx, bytes.NewReader(data), tag, 0, lookup, mp3context.Limits{}); got != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled projection: %+v %v", got, err)
	}
}

func TestMP3DependencyRejectsUnselectableTargets(t *testing.T) {
	frame := sideInfoViewFrame(t)
	free := append([]byte(nil), frame...)
	free[2] = 0
	for _, tc := range []struct {
		name  string
		body  []byte
		index uint32
	}{
		{"out of range", frame, 2},
		{"free format", free, 0},
		{"truncated frame", frame[:400], 0},
		{"invalid header", []byte{0, 0, 0, 0}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, tag := fixture(t, 8, append([]byte{0x2f}, tc.body...))
			lookup := func(uint64) (flv.TagRecord, error) { return tag, nil }
			got, err := ParseWithMP3Dependency(t.Context(), bytes.NewReader(data), tag, tc.index, lookup, mp3context.Limits{})
			if !errors.Is(err, ErrInvalidInput) || got != nil {
				t.Fatalf("published unselectable frame: %+v %v", got, err)
			}
		})
	}
	data, tag := fixture(t, 8, append([]byte{0x2f}, bytes.Repeat(frame, 3)...))
	lookup := func(uint64) (flv.TagRecord, error) { return tag, nil }
	got, err := ParseWithMP3Dependency(t.Context(), bytes.NewReader(data), tag, 2, lookup, mp3context.Limits{MaxFrames: 1})
	if err != nil || !got.Truncated || find(t, got, "dependencyReason").Value != "frame_budget" || optionalField(got, "selectedFrameIndex") != nil {
		t.Fatalf("budget projected an uninspected selected frame: %+v %v", got, err)
	}
}
