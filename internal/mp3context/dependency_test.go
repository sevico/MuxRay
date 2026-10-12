package mp3context

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

func frame(back uint32) []byte {
	f := make([]byte, 417)
	copy(f, []byte{0xff, 0xfb, 0x90, 0x64})
	f[4], f[5] = byte(back>>1), byte(back&1)<<7
	for i := 36; i < len(f); i++ {
		f[i] = byte(i)
	}
	return f
}

type tagData struct {
	kind    byte
	payload []byte
}

func fixture(t *testing.T, tags ...tagData) ([]byte, []flv.TagRecord) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, tag := range tags {
		header := make([]byte, 11)
		header[0] = tag.kind
		n := len(tag.payload)
		header[1], header[2], header[3] = byte(n>>16), byte(n>>8), byte(n)
		data = append(data, header...)
		data = append(data, tag.payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(11+n))
		data = append(data, previous[:]...)
	}
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil || !scan.Complete {
		t.Fatalf("scan: %+v %v", scan, err)
	}
	return data, scan.Tags
}

func audio(frames ...[]byte) tagData {
	payload := []byte{0x2f}
	for _, f := range frames {
		payload = append(payload, f...)
	}
	return tagData{8, payload}
}

func resolve(t *testing.T, data []byte, tags []flv.TagRecord, index uint32, limits Limits) Resolution {
	t.Helper()
	r, err := Resolve(t.Context(), bytes.NewReader(data), tags[len(tags)-1], index, func(id uint64) (flv.TagRecord, error) { return tags[id], nil }, limits)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestResolvePhysicalWindowAcrossTagsAndSameTagOracle(t *testing.T) {
	first, second, target := frame(0), frame(10), frame(511)
	data, tags := fixture(t, audio(first), tagData{9, []byte{0x17, 2, 0, 0, 0}}, tagData{18, []byte{5}}, audio(second), audio(target))
	r := resolve(t, data, tags, 0, Limits{})
	if r.State != "resolved" || r.Reason != "across_tags" || *r.RequestedPreviousBytes != 511 || r.ResolvedPreviousBytes != 511 || *r.MissingPreviousBytes != 0 ||
		*r.AvailableBits != (511+381)*8 || r.ScannedTags != 4 || r.AudioTags != 2 || r.FramesInspected != 3 || r.CoverageBytes != 417*3 || r.BytesRead >= r.CoverageBytes {
		t.Fatalf("resolution: %+v", r)
	}
	want := []Source{
		{0, 0, false, cursor.Span{ByteOffset: tags[0].Offset + 11 + 1 + 36 + 251, BitLength: 130 * 8}},
		{3, 0, false, cursor.Span{ByteOffset: tags[3].Offset + 11 + 1 + 36, BitLength: 381 * 8}},
		{4, 0, true, cursor.Span{ByteOffset: tags[4].Offset + 11 + 1 + 36, BitLength: 381 * 8}},
	}
	if !reflect.DeepEqual(r.Sources, want) {
		t.Fatalf("source ranges: %+v", r.Sources)
	}
	var physical []byte
	for _, s := range r.Sources {
		physical = append(physical, data[s.Span.ByteOffset:s.Span.ByteOffset+uint64(s.Span.BitLength/8)]...)
	}
	wantBytes := append(append(append([]byte{}, first[len(first)-130:]...), second[36:]...), target[36:]...)
	if !bytes.Equal(physical, wantBytes) {
		t.Fatal("physical window includes headers or FLV bytes")
	}
	joined := append(append(append([]byte{}, first...), second...), target...)
	oracle, err := mp3.ParseFrameSequence(bytes.NewReader(joined), cursor.Span{BitLength: uint32(len(joined) * 8)}, mp3.Options{InspectSideInfo: true})
	if err != nil || oracle.Frames[2].Dependency.State != "within_tag" || *oracle.Frames[2].Dependency.AvailableBits != uint32(*r.AvailableBits) {
		t.Fatalf("same-tag geometry: %+v %v", oracle, err)
	}
	data, tags = fixture(t, audio(first, second, target))
	r = resolve(t, data, tags, 2, Limits{})
	if r.State != "resolved" || r.Reason != "within_tag" || r.ScannedTags != 0 || r.Sources[0].FrameIndex != 0 || r.Sources[1].FrameIndex != 1 || r.Sources[2].FrameIndex != 2 {
		t.Fatalf("same-tag selection: %+v", r)
	}
}

func TestResolveDoesNotCrossBarriers(t *testing.T) {
	badCRC := make([]byte, 417)
	copy(badCRC, []byte{0xff, 0xfa, 0x90, 0x64})
	badSide := frame(0)
	badSide[8] = 0xff
	badSide[9] = 0x80 // big_values=511 exceeds288.
	changedChannels := make([]byte, 417)
	copy(changedChannels, []byte{0xff, 0xfb, 0x90, 0xc4})
	changedRate := make([]byte, 384)
	copy(changedRate, []byte{0xff, 0xfb, 0x94, 0x64})
	changedVersion := make([]byte, 261)
	copy(changedVersion, []byte{0xff, 0xf3, 0x90, 0x64})
	for _, tc := range []struct {
		name   string
		tag    tagData
		reason string
	}{
		{"CRC", audio(badCRC), "crc_invalid"}, {"side info", audio(badSide), "invalid_side_info"},
		{"channels", audio(changedChannels), "parameters_changed"}, {"rate", audio(changedRate), "parameters_changed"}, {"version", audio(changedVersion), "parameters_changed"},
		{"codec", tagData{8, []byte{0xaf, 1, 0}}, "audio_codec_changed"},
		{"filtered", tagData{0x28, append([]byte{0x2f}, frame(0)...)}, "filtered_audio"},
		{"empty", tagData{8, nil}, "audio_header_missing"},
		{"prefix", audio(append([]byte{0}, frame(0)...)), "unparsed_audio_suffix"},
		{"truncated frame", audio(frame(0)[:400]), "unparsed_audio_suffix"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, tags := fixture(t, audio(frame(0)), tc.tag, audio(frame(10)))
			r := resolve(t, data, tags, 0, Limits{})
			if r.State != "unknown" || r.Reason != tc.reason || r.ScannedTags != 1 || r.ResolvedPreviousBytes != 0 || len(r.Sources) != 1 || !r.Sources[0].Current {
				t.Fatalf("crossed barrier: %+v", r)
			}
		})
	}
	// A barrier before an adequate compatible suffix is irrelevant. It is
	// still never crossed to claim an earlier physical slot.
	data, tags := fixture(t, audio(badCRC, frame(0)), audio(frame(10)))
	r := resolve(t, data, tags, 0, Limits{})
	if r.State != "resolved" || r.Sources[0].FrameIndex != 1 {
		t.Fatalf("valid suffix lost: %+v", r)
	}
	data, tags = fixture(t, audio(frame(0)), audio(badSide, frame(500)))
	r = resolve(t, data, tags, 1, Limits{})
	if r.Reason != "invalid_side_info" || r.ScannedTags != 0 {
		t.Fatalf("selected prefix barrier crossed: %+v", r)
	}
}

func TestResolveBudgetsAndStartBoundary(t *testing.T) {
	data, tags := fixture(t, audio(frame(0)), tagData{18, []byte{5}}, audio(frame(0)), audio(frame(511)))
	for _, tc := range []struct {
		limits Limits
		reason string
		known  uint32
	}{
		{Limits{MaxTags: 1}, "tag_budget", 381}, {Limits{MaxAudioTags: 1}, "audio_tag_budget", 381},
		{Limits{MaxBytes: 833}, "coverage_budget", 0}, {Limits{MaxReadBytes: 5}, "read_budget", 0},
		{Limits{MaxFrames: 1}, "frame_budget", 0}, {Limits{MaxSources: 1}, "source_limit", 0},
	} {
		r := resolve(t, data, tags, 0, tc.limits)
		if r.State != "unknown" || r.Reason != tc.reason || r.ResolvedPreviousBytes != tc.known || r.BytesRead > r.Limits.MaxReadBytes || r.CoverageBytes > r.Limits.MaxBytes || r.FramesInspected > r.Limits.MaxFrames {
			t.Fatalf("budget %+v: %+v", tc, r)
		}
	}
	data, tags = fixture(t, audio(frame(0), frame(0)), audio(frame(10)))
	r := resolve(t, data, tags, 0, Limits{MaxFrames: 2})
	if r.Reason != "frame_budget" || r.ResolvedPreviousBytes != 0 {
		t.Fatalf("used unparsed prior suffix: %+v", r)
	}
	data, tags = fixture(t, audio(frame(50)))
	r = resolve(t, data, tags, 0, Limits{})
	if r.State != "unknown" || r.Reason != "start_of_file" || *r.MissingPreviousBytes != 50 {
		t.Fatalf("invented beginning history: %+v", r)
	}
	data, tags = fixture(t, audio(frame(0)))
	r = resolve(t, data, tags, 0, Limits{})
	if r.State != "resolved" || r.Reason != "no_backward_reference" || len(r.Sources) != 1 || *r.MissingPreviousBytes != 0 {
		t.Fatalf("zero backpointer: %+v", r)
	}
}

func TestResolvePriorTagFrameSelectionBoundary(t *testing.T) {
	for _, count := range []int{64, 65} {
		data, tags := fixture(t, audio(bytes.Repeat(frame(0), count)), audio(frame(10)))
		r := resolve(t, data, tags, 0, Limits{})
		if r.FramesInspected != 65 {
			t.Fatalf("per-Tag cap for %d frames: %+v", count, r)
		}
		if count == 64 {
			if r.State != "resolved" || len(r.Sources) != 2 || r.Sources[0].FrameIndex != 63 || r.ResolvedPreviousBytes != 10 {
				t.Fatalf("last selectable frame not retained: %+v", r)
			}
		} else if r.State != "unknown" || r.Reason != "frame_budget" || len(r.Sources) != 1 || r.ResolvedPreviousBytes != 0 {
			t.Fatalf("unselectable tail used as a source: %+v", r)
		}
		for _, source := range r.Sources {
			if source.FrameIndex >= 64 {
				t.Fatalf("source cannot be selected: %+v", source)
			}
		}
	}
}

type interceptReader struct {
	io.ReaderAt
	call func() error
}

func (r interceptReader) ReadAt(p []byte, off int64) (int, error) {
	n, e := r.ReaderAt.ReadAt(p, off)
	if r.call != nil {
		if err := r.call(); err != nil {
			return n, err
		}
	}
	return n, e
}

func TestResolveCancellationIOAndLookupErrorsPublishNothing(t *testing.T) {
	data, tags := fixture(t, audio(frame(0)), audio(frame(10)))
	lookup := func(id uint64) (flv.TagRecord, error) { return tags[id], nil }
	boom := errors.New("source broke")
	for _, mode := range []string{"cancel", "full read error", "late read error", "lookup", "bad lookup"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			reader := interceptReader{ReaderAt: bytes.NewReader(data)}
			find := lookup
			want := boom
			switch mode {
			case "cancel":
				reader.call = func() error { cancel(); return nil }
				want = context.Canceled
			case "full read error":
				reader.call = func() error { return boom }
			case "late read error":
				calls := 0
				reader.call = func() error {
					calls++
					if calls == 5 {
						return boom
					}
					return nil
				}
			case "lookup":
				find = func(uint64) (flv.TagRecord, error) { return flv.TagRecord{}, boom }
			case "bad lookup":
				find = func(uint64) (flv.TagRecord, error) { return tags[1], nil }
				want = ErrInvalidInput
			}
			r, err := Resolve(ctx, reader, tags[1], 0, find, Limits{})
			if !errors.Is(err, want) || !reflect.DeepEqual(r, Resolution{}) {
				t.Fatalf("published failed work: %+v %v", r, err)
			}
		})
	}
	for _, limits := range []Limits{{MaxTags: MaxTags + 1}, {MaxFrames: MaxFrames + 1}, {MaxBytes: MaxBytes + 1}, {MaxSources: MaxSources + 1}} {
		if _, err := Resolve(t.Context(), bytes.NewReader(data), tags[1], 0, lookup, limits); !errors.Is(err, ErrInvalidInput) {
			t.Fatal("raised hard limit")
		}
	}
}
