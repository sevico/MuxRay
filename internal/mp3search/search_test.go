package mp3search

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
)

type fixtureTag struct {
	kind    byte
	dts     uint32
	payload []byte
}

func frame(length int, header ...byte) []byte {
	b := make([]byte, length)
	copy(b, header)
	return b
}

func audio(dts uint32, frames ...[]byte) fixtureTag {
	p := []byte{0x2f}
	for _, f := range frames {
		p = append(p, f...)
	}
	return fixtureTag{8, dts, p}
}

func fixture(t testing.TB, tags ...fixtureTag) ([]byte, flv.ScanResult, Lookup) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, tag := range tags {
		h := make([]byte, 11)
		h[0] = tag.kind
		h[1], h[2], h[3] = byte(len(tag.payload)>>16), byte(len(tag.payload)>>8), byte(len(tag.payload))
		h[4], h[5], h[6], h[7] = byte(tag.dts>>16), byte(tag.dts>>8), byte(tag.dts), byte(tag.dts>>24)
		data = append(data, h...)
		data = append(data, tag.payload...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(tag.payload)))
		data = append(data, size[:]...)
	}
	scan, err := flv.Scan(bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	lookup := func(id uint64) (flv.TagRecord, error) {
		if id >= uint64(len(scan.Tags)) {
			return flv.TagRecord{}, ErrInvalidInput
		}
		return scan.Tags[id], nil
	}
	return data, scan, lookup
}

func run(t testing.TB, data []byte, scan flv.ScanResult, lookup Lookup, state State, filter string) *Page {
	t.Helper()
	p, err := Search(t.Context(), bytes.NewReader(data), lookup, state, uint64(len(scan.Tags)), scan.Complete, filter)
	if err != nil {
		t.Fatal(err)
	}
	if p.Next != p.NextState.Position() || p.Start != state.Position() || p.ScannedFrames > MaxScanFrames || p.ScannedTags > MaxScanTags || p.BytesRead > MaxBytes || len(p.Rows) > MaxResults {
		t.Fatalf("page limits/identity: %+v", p)
	}
	return p
}

func collect(t testing.TB, data []byte, scan flv.ScanResult, lookup Lookup, filter string) ([]Row, []*Page) {
	t.Helper()
	state := Initial()
	var rows []Row
	var pages []*Page
	for range 100 {
		p := run(t, data, scan, lookup, state, filter)
		rows, pages = append(rows, p.Rows...), append(pages, p)
		if p.Exhausted {
			return rows, pages
		}
		if p.Next == p.Start {
			t.Fatalf("no progress: %+v", p)
		}
		state = p.NextState
	}
	t.Fatal("did not finish bounded fixture")
	return nil, nil
}

func TestLongTagFindsChangesAfterRetainedSummaryPrefix(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64) // 128 kbps, 44.1 kHz, joint stereo
	mono := frame(417, 0xff, 0xfb, 0x90, 0xc4)
	frames := make([][]byte, 3000)
	for i := range frames {
		frames[i] = base
	}
	for _, i := range []int{1024, 2048, 2600} {
		frames[i] = mono
	}
	data, scan, lookup := fixture(t, audio(10, frames...))
	before := append([]byte(nil), data...)
	rows, pages := collect(t, data, scan, lookup, "parameters")
	if len(pages) != 3 || pages[0].Next != (Position{0, 1024}) || len(pages[0].Rows) != 0 || pages[1].Next != (Position{0, 2048}) || !pages[2].Exhausted || len(rows) != 6 {
		t.Fatalf("long tag pages=%d rows=%+v", len(pages), rows)
	}
	for i, want := range []uint32{1024, 1025, 2048, 2049, 2600, 2601} {
		r := rows[i]
		if r.FrameIndex != want || r.TagID != 0 || r.Status != "complete" || r.DTS != 10 || !r.ComparedToPrevious || !r.ParametersChangedFromPrevious || r.BitrateChangedFromPrevious || r.Span.ByteOffset != 25+uint64(want)*417 || r.Span.BitLength != 417*8 || r.Parameters.Channels == r.PreviousParameters.Channels {
			t.Fatalf("row%d %+v", i, r)
		}
	}
	if pages[0].BytesRead != 1+1024*4 || pages[1].BytesRead != 1024*4 || pages[2].BytesRead != 952*4 {
		t.Fatal("reread prefix or frame bodies")
	}
	// Copying and retrying a server-owned cursor neither mutates it nor changes
	// the first comparison at a frame-budget boundary.
	retry := run(t, data, scan, lookup, pages[0].NextState, "parameters")
	if !reflect.DeepEqual(retry, pages[1]) || !bytes.Equal(before, data) {
		t.Fatal("non-idempotent state/source")
	}
	summary, err := analysis.AnalyzeV3(t.Context(), bytes.NewReader(data), scan, analysis.OptionsV3{})
	if err != nil || summary.MP3Frames.ParameterChangeCount != uint64(len(rows)) || len(summary.MP3Frames.Events) != 256 || !summary.MP3Frames.EventsTruncated {
		t.Fatalf("summary mismatch: %+v %v", summary.MP3Frames, err)
	}
}

func TestRowLimitAndFiltersPreserveOriginalIndices(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64)
	fast := frame(522, 0xff, 0xfb, 0xa0, 0x64)
	frames := make([][]byte, 201)
	for i := range frames {
		frames[i] = base
		if i%2 == 1 {
			frames[i] = fast
		}
	}
	data, scan, lookup := fixture(t, audio(0, frames...))
	rows, pages := collect(t, data, scan, lookup, "bitrate")
	if len(rows) != 200 || len(pages) != 4 || pages[0].Next != (Position{0, 65}) || pages[1].Next != (Position{0, 129}) || pages[2].Next != (Position{0, 193}) {
		t.Fatalf("rows %d pages %+v", len(rows), pages)
	}
	var offset uint64 = 25 + 417
	for i, r := range rows {
		if r.FrameIndex != uint32(i+1) || r.Span.ByteOffset != offset || r.Status != "complete" || !r.ComparedToPrevious || !r.BitrateChangedFromPrevious || r.ParametersChangedFromPrevious || r.BitrateKbps == r.PreviousBitrateKbps {
			t.Fatalf("row%d %+v", i, r)
		}
		offset += uint64(r.Span.BitLength / 8)
	}
	all, _ := collect(t, data, scan, lookup, "all")
	if !reflect.DeepEqual(all, rows) {
		t.Fatal("all lost bitrate changes")
	}
	for _, filter := range []string{"parameters", "stops"} {
		got, _ := collect(t, data, scan, lookup, filter)
		if len(got) != 0 {
			t.Fatalf("unexpected %s hits: %+v", filter, got)
		}
	}
}

func TestStopsPreserveSuffixAndResumeOnlyAtNextTag(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64)
	fast := frame(522, 0xff, 0xfb, 0xa0, 0x64)
	for _, test := range []struct {
		name string
		tail []byte
	}{
		{"truncatedHeader", []byte{0xff, 0xfb}},
		{"malformedHeader", append([]byte{0, 0, 0, 0}, fast...)},
		{"unsupportedLayer", []byte{0xff, 0xfd, 0x90, 0x64}},
		{"freeFormat", []byte{0xff, 0xfb, 0x00, 0x64}},
		{"truncatedFrame", base[:40]},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, scan, lookup := fixture(t, audio(0, base, test.tail), audio(1, fast))
			rows, _ := collect(t, data, scan, lookup, "all")
			if len(rows) != 1 || rows[0].Status != test.name || rows[0].TagID != 0 || rows[0].FrameIndex != 1 || rows[0].Span.ByteOffset != 25+417 || rows[0].Span.BitLength != uint32(len(test.tail)*8) || rows[0].ComparedToPrevious {
				t.Fatalf("stop %+v", rows)
			}
		})
	}
	data, scan, lookup := fixture(t, audio(0))
	p := run(t, data, scan, lookup, State{}, "stops")
	if len(p.Rows) != 1 || p.Rows[0].Status != "truncatedHeader" || p.Rows[0].Span.BitLength != 0 || p.Rows[0].Span.ByteOffset != 25 || p.ScannedFrames != 1 || p.BytesRead != 1 {
		t.Fatalf("empty SoundData %+v", p)
	}
}

func TestLateDamageRemainsDiscoverableAndDoesNotResynchronize(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64)
	fast := frame(522, 0xff, 0xfb, 0xa0, 0x64)
	frames := [][]byte{bytes.Repeat(base, 1500), {0, 0, 0, 0}, fast}
	data, scan, lookup := fixture(t, audio(0, frames...), audio(1, fast))
	rows, pages := collect(t, data, scan, lookup, "stops")
	if len(pages) != 2 || len(rows) != 1 || rows[0].FrameIndex != 1500 || rows[0].Status != "malformedHeader" || rows[0].Span.ByteOffset != 25+1500*417 || rows[0].Span.BitLength != uint32((4+len(fast))*8) {
		t.Fatalf("late stop %+v", rows)
	}
	all, _ := collect(t, data, scan, lookup, "all")
	if !reflect.DeepEqual(all, rows) {
		t.Fatal("resynchronized or compared across malformed suffix")
	}
}

func TestTagBarriersMatchSummaryComparison(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64)
	fast := frame(522, 0xff, 0xfb, 0xa0, 0x64)
	for _, test := range []struct {
		name     string
		middle   fixtureTag
		compared bool
		stop     string
	}{
		{"video", fixtureTag{9, 15, []byte{0x17, 2, 0, 0, 0}}, true, ""},
		{"script", fixtureTag{18, 15, nil}, true, ""},
		{"aac", fixtureTag{8, 15, []byte{0xaf, 1, 0}}, false, ""},
		{"unknown", fixtureTag{8, 15, nil}, false, "missingAudioHeader"},
		{"filtered", fixtureTag{0x28, 15, []byte{0x2f}}, false, "filteredAudio"},
		{"other", fixtureTag{8, 15, []byte{0x7f}}, false, "unsupportedAudio"},
	} {
		t.Run(test.name, func(t *testing.T) {
			data, scan, lookup := fixture(t, audio(10, base), test.middle, audio(20, fast))
			rows, _ := collect(t, data, scan, lookup, "all")
			changes := 0
			for _, row := range rows {
				if row.Status == "complete" {
					changes++
					if row.TagID != 2 || row.FrameIndex != 0 || !row.ComparedToPrevious || !row.BitrateChangedFromPrevious {
						t.Fatal(row)
					}
				} else if row.TagID != 1 || row.Status != test.stop || row.ParametersPresent {
					t.Fatal(row)
				}
			}
			if (changes == 1) != test.compared || len(rows) != changes+boolInt(test.stop != "") {
				t.Fatalf("barrier rows %+v", rows)
			}
			summary, err := analysis.AnalyzeV3(t.Context(), bytes.NewReader(data), scan, analysis.OptionsV3{})
			if err != nil || summary.MP3Frames.BitrateChangeCount != uint64(changes) {
				t.Fatalf("summary mismatch %+v %v", summary.MP3Frames, err)
			}
		})
	}
	data, scan, lookup := fixture(t, audio(10, base), audio(9, fast), audio(10, base))
	rows, _ := collect(t, data, scan, lookup, "all")
	if len(rows) != 1 || rows[0].TagID != 2 || rows[0].PreviousBitrateKbps != 160 {
		t.Fatalf("DTS barrier %+v", rows)
	}
	// A partial indexed audio payload is not read or claimed to be MP3.
	scan.Tags[1].AvailableDataSize--
	rows, _ = collect(t, data, scan, lookup, "all")
	if len(rows) != 1 || rows[0].Status != "truncatedAudio" || rows[0].ParametersPresent || rows[0].Span.BitLength != scan.Tags[1].AvailableDataSize*8 {
		t.Fatal(rows)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func TestTagBudgetCarriesComparisonAcrossVideoInterleaving(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64)
	fast := frame(522, 0xff, 0xfb, 0xa0, 0x64)
	tags := []fixtureTag{audio(0, base)}
	for range 255 {
		tags = append(tags, fixtureTag{9, 0, nil})
	}
	tags = append(tags, audio(1, fast))
	data, scan, lookup := fixture(t, tags...)
	scan.Complete = false
	rows, pages := collect(t, data, scan, lookup, "all")
	if len(pages) != 2 || pages[0].ScannedTags != 256 || pages[0].Next != (Position{256, 0}) || len(rows) != 1 || rows[0].TagID != 256 || !rows[0].ComparedToPrevious || pages[1].ScanComplete || !pages[1].Exhausted {
		t.Fatalf("tag budget rows=%+v pages=%+v", rows, pages)
	}
}

type readerFunc func([]byte, int64) (int, error)

func (f readerFunc) ReadAt(b []byte, offset int64) (int, error) { return f(b, offset) }

func TestReadFailuresAndCancellationNeverPublishPage(t *testing.T) {
	data, scan, lookup := fixture(t, audio(0, frame(417, 0xff, 0xfb, 0x90, 0x64)))
	sentinel := errors.New("source failed")
	for _, test := range []struct {
		name string
		n    int
		err  error
	}{
		{"short", 0, nil}, {"EOF", 0, io.EOF}, {"error", 0, sentinel}, {"full-error", 1, sentinel}, {"invalid-count", 100, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			source := readerFunc(func(b []byte, off int64) (int, error) {
				copy(b, data[off:])
				return test.n, test.err
			})
			p, err := Search(t.Context(), source, lookup, State{}, 1, true, "all")
			if err == nil || p != nil || test.err == sentinel && !errors.Is(err, sentinel) {
				t.Fatalf("page=%+v err=%v", p, err)
			}
		})
	}
	// Full reads with EOF are legal, including the strict frame-header read.
	fullEOF := readerFunc(func(b []byte, off int64) (int, error) { copy(b, data[off:]); return len(b), io.EOF })
	if p, err := Search(nil, fullEOF, lookup, State{}, 1, true, "all"); err != nil || !p.Exhausted || p.BytesRead != 5 {
		t.Fatalf("full EOF %+v %v", p, err)
	}
	for _, when := range []string{"before", "read", "lookup"} {
		t.Run("cancel-"+when, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if when == "before" {
				cancel()
			}
			source := readerFunc(func(b []byte, off int64) (int, error) {
				n, e := bytes.NewReader(data).ReadAt(b, off)
				if when == "read" {
					cancel()
				}
				return n, e
			})
			get := func(id uint64) (flv.TagRecord, error) {
				if when == "lookup" {
					cancel()
				}
				return lookup(id)
			}
			p, err := Search(ctx, source, get, State{}, uint64(len(scan.Tags)), true, "all")
			if p != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("page=%+v err=%v", p, err)
			}
		})
	}
}

func TestReaderBudgetChargesOnlyActualReadBytes(t *testing.T) {
	reads := 0
	r := reader{ctx: t.Context(), bytes: MaxBytes - 4, source: readerFunc(func(b []byte, _ int64) (int, error) {
		reads++
		return len(b), nil
	})}
	if n, err := r.ReadAt(make([]byte, 4), 0); n != 4 || err != nil || r.bytes != MaxBytes || reads != 1 {
		t.Fatalf("exact budget %d %v %+v", n, err, r)
	}
	if n, err := r.ReadAt(make([]byte, 1), 0); n != 0 || !errors.Is(err, errBudget) || r.bytes != MaxBytes || reads != 1 {
		t.Fatalf("budget read %d %v %+v", n, err, r)
	}
	// The public Tag/frame limits are currently tighter than the byte limit;
	// the separate reader guard still protects later parser changes.
	if 4*uint64(MaxScanFrames)+uint64(MaxScanTags) > MaxBytes {
		t.Fatal("update the public worst-case read expectation")
	}
}

func TestInvalidInputsAndCompletedCursor(t *testing.T) {
	data, scan, lookup := fixture(t, audio(0, frame(417, 0xff, 0xfb, 0x90, 0x64)))
	for _, filter := range []string{"", "unknown", "ALL"} {
		if p, err := Search(t.Context(), bytes.NewReader(data), lookup, State{}, 1, true, filter); p != nil || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("filter %q %+v %v", filter, p, err)
		}
	}
	for _, total := range []uint64{flv.HardMaxTags + 1, 0} {
		state := State{position: Position{TagID: 1}}
		if p, err := Search(t.Context(), bytes.NewReader(data), lookup, state, total, true, "all"); p != nil || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("total %d %+v %v", total, p, err)
		}
	}
	bad := func(uint64) (flv.TagRecord, error) { return flv.TagRecord{ID: 99}, nil }
	if p, err := Search(t.Context(), bytes.NewReader(data), bad, State{}, 1, true, "all"); p != nil || !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("lookup %+v %v", p, err)
	}
	for _, mutate := range []func(*flv.TagRecord){
		func(tag *flv.TagRecord) { tag.DataSize = 0x1000000 },
		func(tag *flv.TagRecord) { tag.AvailableDataSize = tag.DataSize + 1 },
		func(tag *flv.TagRecord) { tag.Offset = ^uint64(0) },
	} {
		get := func(id uint64) (flv.TagRecord, error) { tag, _ := lookup(id); mutate(&tag); return tag, nil }
		if p, err := Search(t.Context(), bytes.NewReader(data), get, State{}, 1, true, "all"); p != nil || !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("invalid tag %+v %v", p, err)
		}
	}
	p := run(t, data, scan, lookup, State{}, "all")
	end := run(t, data, scan, lookup, p.NextState, "all")
	if !end.Exhausted || end.ScannedTags != 0 || end.ScannedFrames != 0 || end.BytesRead != 0 || len(end.Rows) != 0 || end.Next != p.Next {
		t.Fatalf("completed cursor %+v", end)
	}
}

func TestFailedContinuationDoesNotPublishPriorHits(t *testing.T) {
	base := frame(417, 0xff, 0xfb, 0x90, 0x64)
	fast := frame(522, 0xff, 0xfb, 0xa0, 0x64)
	data, _, lookup := fixture(t, audio(0, base, fast), audio(1, base))
	sentinel := errors.New("late source failure")
	second, _ := lookup(1)
	source := readerFunc(func(b []byte, off int64) (int, error) {
		if uint64(off) >= second.Offset {
			return 0, sentinel
		}
		return bytes.NewReader(data).ReadAt(b, off)
	})
	p, err := Search(t.Context(), source, lookup, State{}, 2, true, "all")
	if p != nil || !errors.Is(err, sentinel) {
		t.Fatalf("partial page escaped: %+v %v", p, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	get := func(id uint64) (flv.TagRecord, error) {
		if id == 1 {
			cancel()
		}
		return lookup(id)
	}
	p, err = Search(ctx, bytes.NewReader(data), get, State{}, 2, true, "all")
	if p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled page escaped: %+v %v", p, err)
	}
}
