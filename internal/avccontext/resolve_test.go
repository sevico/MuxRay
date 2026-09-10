package avccontext

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"testing"

	"github.com/sevico/muxray/internal/flv"
)

type packet struct {
	kind    byte
	payload []byte
}

func config(width byte) packet {
	return packet{9, []byte{0x17, 0, 0, 0, 0, 1, 0x42, 0, 0x1e, 0xfc | (width - 1), 0xe0, 0}}
}
func nalu() packet { return packet{9, []byte{0x27, 1, 0, 0, 0, 0, 0, 0, 1, 0x65}} }

func fixture(t *testing.T, packets ...packet) ([]byte, flv.TagIndex) {
	t.Helper()
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, packet := range packets {
		header := make([]byte, 11)
		header[0] = packet.kind
		header[1], header[2], header[3] = byte(len(packet.payload)>>16), byte(len(packet.payload)>>8), byte(len(packet.payload))
		data = append(data, header...)
		data = append(data, packet.payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(11+len(packet.payload)))
		data = append(data, previous[:]...)
	}
	scan, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return data, scan.Index()
}

func lookup(index flv.TagIndex) Lookup {
	return func(id uint64) (flv.TagRecord, error) {
		tag, ok := index.Get(id)
		if !ok {
			return tag, ErrInvalidInput
		}
		return tag, nil
	}
}
func selected(index flv.TagIndex) flv.TagRecord { tag, _ := index.Get(index.Len() - 1); return tag }

func TestNearestUsableSequenceHeaderAndSourceSpan(t *testing.T) {
	for _, width := range []byte{1, 2, 4} {
		data, index := fixture(t, config(4), packet{18, []byte{5}}, config(width), nalu())
		got, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{})
		origin, _ := index.Get(2)
		if err != nil || got.State != StateFound || got.NALLengthSize != width || got.SourceTagID != 2 || got.ScannedTags != 1 || got.Candidates != 1 || got.BytesRead == 0 {
			t.Fatalf("width=%d got=%+v err=%v", width, got, err)
		}
		if got.SourceLengthSpan.ByteOffset != origin.PayloadSpan().ByteOffset+9 || got.SourceLengthSpan.BitOffset != 6 || got.SourceLengthSpan.BitLength != 2 {
			t.Fatalf("declaration span: %+v", got.SourceLengthSpan)
		}
	}
	// Incomplete newer framing may be skipped, but a framed declaration with
	// damaged SPS syntax remains usable, exactly as the CLI already permits.
	bad := packet{9, []byte{0x17, 0, 0, 0, 0, 1, 0x42}}
	semantic := packet{9, []byte{0x17, 0, 0, 0, 0, 1, 0x42, 0, 0x1e, 0xfd, 0xe1, 0, 1, 0x67, 0}}
	for _, tc := range []struct {
		middle     packet
		sourceID   uint64
		width      byte
		candidates uint32
	}{{bad, 0, 1, 2}, {semantic, 1, 2, 1}} {
		data, index := fixture(t, config(1), tc.middle, nalu())
		got, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{})
		if err != nil || got.State != StateFound || got.NALLengthSize != tc.width || got.SourceTagID != tc.sourceID || got.Candidates != tc.candidates {
			t.Fatalf("usable framing: %+v %v", got, err)
		}
	}
}

func TestMissingAndNotNeededRemainDistinct(t *testing.T) {
	for _, p := range []packet{config(4), {9, []byte{0x17, 2, 0, 0, 0}}, {9, []byte{0x52, 1}}, {9, []byte{0x12, 1}}, {18, []byte{5}}, {0x29, []byte{0x27, 1, 0, 0, 0}}} {
		data, index := fixture(t, p)
		got, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), func(uint64) (flv.TagRecord, error) { t.Fatal("unneeded lookup"); return flv.TagRecord{}, nil }, flv.AVCVideoOptions{}, Limits{})
		if err != nil || got.State != StateNotNeeded || got.NALLengthSize != 0 || got.ScannedTags != 0 {
			t.Fatalf("not needed: %+v %v", got, err)
		}
	}
	data, index := fixture(t, packet{18, []byte{5}}, nalu())
	got, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{MaxTags: 1})
	if err != nil || got.State != StateMissing || got.ScannedTags != 1 || got.Candidates != 0 || got.BytesRead != 2 {
		t.Fatalf("missing: %+v %v", got, err)
	}
}

func TestBudgetsStopWorkWithoutInventingMissingContext(t *testing.T) {
	data, index := fixture(t, config(4), packet{18, []byte{5}}, nalu())
	last := selected(index)
	full, err := Resolve(t.Context(), bytes.NewReader(data), last, lookup(index), flv.AVCVideoOptions{}, Limits{})
	if err != nil || full.State != StateFound {
		t.Fatalf("baseline: %+v %v", full, err)
	}
	for _, tc := range []struct {
		limits Limits
		state  string
	}{{Limits{MaxTags: 1}, StateBudgetExhausted}, {Limits{MaxTags: 2}, StateFound}, {Limits{MaxCandidates: 1}, StateFound}, {Limits{MaxBytes: 1}, StateBudgetExhausted}, {Limits{MaxBytes: full.BytesRead - 1}, StateBudgetExhausted}, {Limits{MaxBytes: full.BytesRead}, StateFound}} {
		got, err := Resolve(t.Context(), bytes.NewReader(data), last, lookup(index), flv.AVCVideoOptions{}, tc.limits)
		if err != nil || got.State != tc.state || tc.limits.MaxBytes != 0 && got.BytesRead > tc.limits.MaxBytes || tc.limits.MaxTags != 0 && got.ScannedTags > tc.limits.MaxTags || tc.limits.MaxCandidates != 0 && got.Candidates > tc.limits.MaxCandidates {
			t.Fatalf("limits=%+v result=%+v error=%v", tc.limits, got, err)
		}
	}
	bad := packet{9, []byte{0x17, 0, 0, 0, 0, 1, 0x42}}
	data, index = fixture(t, config(4), bad, nalu())
	got, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{MaxCandidates: 1})
	if err != nil || got.State != StateBudgetExhausted || got.Candidates != 1 || got.ScannedTags != 2 || got.NALLengthSize != 0 {
		t.Fatalf("candidate budget: %+v %v", got, err)
	}
}

type readerFunc func([]byte, int64) (int, error)

func (f readerFunc) ReadAt(p []byte, offset int64) (int, error) { return f(p, offset) }

func TestReadErrorsCancellationAndInvalidLookups(t *testing.T) {
	data, index := fixture(t, config(4), nalu())
	last := selected(index)
	sourceError := errors.New("source unavailable")
	for _, tc := range []struct {
		name   string
		reader io.ReaderAt
		want   error
	}{{"failure", readerFunc(func([]byte, int64) (int, error) { return 0, sourceError }), sourceError}, {"short nil", readerFunc(func(p []byte, _ int64) (int, error) { return len(p) - 1, nil }), io.ErrUnexpectedEOF}, {"invalid count", readerFunc(func(p []byte, _ int64) (int, error) { return len(p) + 1, nil }), ErrInvalidInput}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(t.Context(), tc.reader, last, lookup(index), flv.AVCVideoOptions{}, Limits{})
			if !errors.Is(err, tc.want) || got.State == StateBudgetExhausted {
				t.Fatalf("result=%+v err=%v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	reader := readerFunc(func(p []byte, off int64) (int, error) {
		calls++
		n, err := bytes.NewReader(data).ReadAt(p, off)
		cancel()
		return n, err
	})
	got, err := Resolve(ctx, reader, last, lookup(index), flv.AVCVideoOptions{}, Limits{})
	if !errors.Is(err, context.Canceled) || calls != 1 || got.BytesRead != 2 {
		t.Fatalf("cancellation: %+v %v calls=%d", got, err, calls)
	}
	for _, get := range []Lookup{nil, func(uint64) (flv.TagRecord, error) { return flv.TagRecord{}, sourceError }, func(id uint64) (flv.TagRecord, error) { tag, _ := index.Get(id); tag.ID++; return tag, nil }} {
		if _, err := Resolve(t.Context(), bytes.NewReader(data), last, get, flv.AVCVideoOptions{}, Limits{}); err == nil {
			t.Fatal("accepted invalid lookup")
		}
	}
}

func TestSourceSpansAndIntegerBounds(t *testing.T) {
	data, index := fixture(t, config(2), nalu())
	first, _ := index.Get(0)
	last := selected(index)
	base := uint64(1<<53) + 17
	first.Offset += base
	last.Offset += base
	first.ID, last.ID = math.MaxUint64-1, math.MaxUint64
	reader := readerFunc(func(p []byte, off int64) (int, error) {
		if off < 0 || uint64(off) < base {
			t.Fatal("invalid source offset")
		}
		start := uint64(off)
		end := start + uint64(len(p))
		if !(start >= first.PayloadSpan().ByteOffset && end <= first.PayloadSpan().ByteOffset+uint64(first.DataSize)) && !(start >= last.PayloadSpan().ByteOffset && end <= last.PayloadSpan().ByteOffset+2) {
			t.Fatalf("read escaped permitted payload: %d..%d", start, end)
		}
		return bytes.NewReader(data).ReadAt(p, int64(start-base))
	})
	got, err := Resolve(t.Context(), reader, last, func(id uint64) (flv.TagRecord, error) {
		if id != first.ID {
			t.Fatal("searched past nearest config")
		}
		return first, nil
	}, flv.AVCVideoOptions{}, Limits{MaxTags: 1})
	if err != nil || got.State != StateFound || got.SourceTagID != math.MaxUint64-1 || got.SourceLengthSpan.ByteOffset != first.PayloadSpan().ByteOffset+9 {
		t.Fatalf("large source coordinates: %+v %v", got, err)
	}
	last.Offset = math.MaxUint64
	if _, err := Resolve(t.Context(), reader, last, lookup(index), flv.AVCVideoOptions{}, Limits{}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("offset overflow: %v", err)
	}
	if _, _, _, err := ReadFramingPrefix(nil, first); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("nil source: %v", err)
	}
	work := workReader{ctx: t.Context(), source: bytes.NewReader(data), result: &Resolution{BytesRead: math.MaxUint64}}
	if _, err := work.ReadAt(make([]byte, 1), 0); !errors.Is(err, ErrCounterOverflow) {
		t.Fatalf("counter overflow: %v", err)
	}
}

func TestFullReadWithEOFAccepted(t *testing.T) {
	data, index := fixture(t, config(4), nalu())
	reader := readerFunc(func(p []byte, off int64) (int, error) {
		n, err := bytes.NewReader(data).ReadAt(p, off)
		if n == len(p) {
			err = io.EOF
		}
		return n, err
	})
	got, err := Resolve(t.Context(), reader, selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{})
	if err != nil || got.State != StateFound || got.NALLengthSize != 4 {
		t.Fatalf("full EOF: %+v %v", got, err)
	}
}

func TestSearchAcrossIndexBlocks(t *testing.T) {
	packets := []packet{config(4)}
	for range 1024 {
		packets = append(packets, packet{18, []byte{5}})
	}
	packets = append(packets, nalu())
	data, index := fixture(t, packets...)
	got, err := Resolve(t.Context(), bytes.NewReader(data), selected(index), lookup(index), flv.AVCVideoOptions{}, Limits{})
	if err != nil || got.State != StateFound || got.ScannedTags != 1025 || got.Candidates != 1 {
		t.Fatalf("cross-block: %+v %v", got, err)
	}
}
