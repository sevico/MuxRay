package avcsearch

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

func config(width byte) []byte {
	return []byte{0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xfc | width - 1, 0xe1, 0, 1, 0x67, 1, 0, 1, 0x68}
}
func packet(frame byte, width byte, headers ...byte) []byte {
	p := []byte{frame<<4 | 7, 1, 0, 0, 0}
	for _, h := range headers {
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], 3)
		p = append(p, size[4-width:]...)
		p = append(p, h, 0xde, 0xad)
	}
	return p
}
func fixture(payloads ...[]byte) []byte {
	data := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, p := range payloads {
		h := make([]byte, 11)
		h[0] = 9
		h[1], h[2], h[3] = byte(len(p)>>16), byte(len(p)>>8), byte(len(p))
		data = append(data, h...)
		data = append(data, p...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(p)))
		data = append(data, size[:]...)
	}
	return data
}
func prepare(t *testing.T, data []byte) (*avccontext.CandidateIndex, Lookup, flv.ScanResult) {
	t.Helper()
	source := bytes.NewReader(data)
	index := &avccontext.CandidateIndex{}
	var records []flv.TagRecord
	scan, err := flv.ScanContextWithObserver(t.Context(), source, uint64(len(data)), flv.Options{}, nil, func(ctx context.Context, tag flv.TagRecord) error {
		records = append(records, tag)
		return index.Observe(ctx, source, tag)
	})
	if err != nil {
		t.Fatal(err)
	}
	scan.Tags = records
	lookup := func(id uint64) (flv.TagRecord, error) {
		if id >= uint64(len(scan.Tags)) {
			return flv.TagRecord{}, ErrInvalidInput
		}
		return scan.Tags[id], nil
	}
	return index, lookup, scan
}
func search(t *testing.T, data []byte, start uint64, filter string) *Page {
	t.Helper()
	index, lookup, scan := prepare(t, data)
	page, err := Search(t.Context(), bytes.NewReader(data), lookup, index, start, uint64(len(scan.Tags)), scan.Complete, filter)
	if err != nil {
		t.Fatal(err)
	}
	return page
}
func TestHeaderOnlyClassificationAndSource(t *testing.T) {
	data := fixture(config(4), packet(1, 4, 0x65), packet(1, 4, 0x41), packet(2, 4, 0x65), packet(2, 4, 0x41), packet(1, 4, 0x74), packet(1, 4, 0xe5), packet(2, 4, 0x65, 0x74))
	page := search(t, data, 0, "all")
	want := []string{"yes", "no", "yes", "no", "unknown", "unknown", "yes"}
	if !page.Exhausted || !page.ScanComplete || page.Next != 8 || page.Scanned != 8 || len(page.Rows) != len(want) {
		t.Fatalf("page %+v", page)
	}
	for i, row := range page.Rows {
		if row.ObservedIDR != want[i] {
			t.Fatalf("row%d %+v", i, row)
		}
		if row.Span == nil {
			t.Fatal("missing span")
		}
		span := row.Span
		value := data[span.ByteOffset] >> (8 - span.BitOffset - uint8(span.BitLength)) & byte((1<<span.BitLength)-1)
		if span.BitLength == 5 && value != 1 && value != 5 {
			t.Fatalf("unusable span %+v", span)
		}
	}
	if page.Rows[1].DeclarationRelation != "keyframe_without_idr" || page.Rows[2].DeclarationRelation != "inter_with_idr" {
		t.Fatal(page.Rows)
	}
	if page.BytesRead > 100 {
		t.Fatalf("NAL bodies were read: %d", page.BytesRead)
	}
	mismatch := search(t, data, 0, "mismatches")
	if len(mismatch.Rows) != 3 {
		t.Fatal(mismatch.Rows)
	}
	unknown := search(t, data, 0, "unknown")
	if len(unknown.Rows) != 2 {
		t.Fatal(unknown.Rows)
	}
}
func TestConfigurationBarriersAndWidths(t *testing.T) {
	for _, width := range []byte{1, 2, 4} {
		data := fixture(config(width), packet(2, width, 0x41), []byte{0x17, 2, 0, 0, 0}, packet(1, width, 0x65), config(width), packet(1, width, 0x65))
		for _, start := range []uint64{0, 1, 3, 5} {
			p := search(t, data, start, "all")
			for _, r := range p.Rows {
				want := "yes"
				if r.TagID == 1 {
					want = "no"
				}
				if r.TagID == 3 {
					want = "unknown"
				}
				if r.ObservedIDR != want {
					t.Fatalf("width%d start%d %+v", width, start, r)
				}
			}
		}
	}
	for _, barrier := range [][]byte{{0x12, 1}, {0x17, 3}, {0x17}, {0x17, 1}, {0x17, 1, 0}, {0x17, 1, 0, 0}, {0x17, 0, 0, 0, 0, 1, 66, 0, 30, 0xfe, 0xe0, 0}, config(4)[:len(config(4))-1]} {
		for _, start := range []uint64{0, 2} {
			p := search(t, fixture(config(4), barrier, packet(1, 4, 0x65)), start, "all")
			if len(p.Rows) == 0 || p.Rows[len(p.Rows)-1].TagID != 2 || p.Rows[len(p.Rows)-1].ObservedIDR != "unknown" {
				t.Fatalf("barrier %x start%d: %+v", barrier, start, p)
			}
		}
	}
}
func TestPaginationLimitsAndMalformedCoverage(t *testing.T) {
	payloads := [][]byte{config(4)}
	for i := 0; i < 200; i++ {
		payloads = append(payloads, packet(2, 4, 0x65))
	}
	data := fixture(payloads...)
	index, lookup, scan := prepare(t, data)
	cursor := uint64(0)
	seen := map[uint64]bool{}
	for {
		p, err := Search(t.Context(), bytes.NewReader(data), lookup, index, cursor, uint64(len(scan.Tags)), true, "all")
		if err != nil {
			t.Fatal(err)
		}
		if p.Next <= cursor || len(p.Rows) > 64 || p.Scanned != uint32(p.Next-cursor) {
			t.Fatalf("bad page %+v", p)
		}
		for _, r := range p.Rows {
			if seen[r.TagID] {
				t.Fatal("repeat")
			}
			seen[r.TagID] = true
		}
		cursor = p.Next
		if p.Exhausted {
			break
		}
	}
	if len(seen) != 200 {
		t.Fatal(len(seen))
	}
	payloads = [][]byte{config(4)}
	for i := 0; i < 300; i++ {
		payloads = append(payloads, packet(2, 4, 0x41))
	}
	page := search(t, fixture(payloads...), 0, "mismatches")
	if page.Next != 256 || page.Exhausted || len(page.Rows) != 0 {
		t.Fatalf("scan bound %+v", page)
	}
	headers := bytes.Repeat([]byte{0x41}, MaxNALHeaders+1)
	p := search(t, fixture(config(4), packet(1, 4, headers...)), 0, "all")
	if p.Rows[0].ObservedIDR != "unknown" || p.Rows[0].Reason != "nal_limit" {
		t.Fatal(p.Rows)
	}
	headers[0] = 0x65
	p = search(t, fixture(config(4), packet(2, 4, headers...)), 0, "all")
	if p.Rows[0].ObservedIDR != "yes" {
		t.Fatal(p.Rows)
	}
	for _, payload := range [][]byte{packet(1, 4), append(packet(1, 4, 0x41), 1), append(packet(1, 4, 0x41), 0, 0, 0, 0), {0x17, 1, 0}} {
		p := search(t, fixture(config(4), payload), 0, "all")
		if p.Rows[0].ObservedIDR != "unknown" {
			t.Fatal(p.Rows)
		}
	}
}
func TestPartialIndexAndTruncatedVideo(t *testing.T) {
	data := fixture(config(4), packet(1, 4, 0x65))
	data = data[:len(data)-5]
	p := search(t, data, 0, "all")
	if p.ScanComplete || !p.Exhausted || p.Rows[0].ObservedIDR != "unknown" || p.Rows[0].Reason != "truncated_payload" {
		t.Fatalf("%+v %+v", p, p.Rows)
	}
}

type failureReader struct {
	io.ReaderAt
	err    error
	cancel context.CancelFunc
}

func (f failureReader) ReadAt(p []byte, off int64) (int, error) {
	n, _ := f.ReaderAt.ReadAt(p, off)
	if f.cancel != nil {
		f.cancel()
	}
	return n, f.err
}
func TestSourceFailuresCancellationAndBudget(t *testing.T) {
	data := fixture(config(4), packet(1, 4, 0x65))
	index, lookup, scan := prepare(t, data)
	sentinel := errors.New("source read failed")
	for _, err := range []error{sentinel, io.ErrUnexpectedEOF} {
		p, e := Search(t.Context(), failureReader{ReaderAt: bytes.NewReader(data), err: err}, lookup, index, 0, uint64(len(scan.Tags)), true, "all")
		if p != nil || !errors.Is(e, err) {
			t.Fatalf("%+v %v", p, e)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	p, err := Search(ctx, failureReader{ReaderAt: bytes.NewReader(data), cancel: cancel}, lookup, index, 0, uint64(len(scan.Tags)), true, "all")
	if p != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("%+v %v", p, err)
	}
	r := reader{ctx: t.Context(), source: bytes.NewReader(data), bytes: MaxBytes}
	if err := r.read(0, make([]byte, 1)); !errors.Is(err, errBudget) || r.bytes != MaxBytes {
		t.Fatal(err, r.bytes)
	}
}

func TestNoBodyReads(t *testing.T) {
	data := fixture(config(4), packet(1, 4, 0x65, 0x41))
	index, lookup, scan := prepare(t, data)
	allowed := make(map[uint64]bool)
	for _, tag := range scan.Tags {
		base := tag.PayloadSpan().ByteOffset
		allowed[base], allowed[base+1] = true, true
	}
	c := scan.Tags[0].PayloadSpan().ByteOffset
	for _, off := range []uint64{5, 6, 7, 8, 9, 10, 11, 12, 14, 15, 16} {
		allowed[c+off] = true
	}
	n := scan.Tags[1].PayloadSpan().ByteOffset
	for _, off := range []uint64{5, 6, 7, 8, 9, 12, 13, 14, 15, 16} {
		allowed[n+off] = true
	}
	source := &headerReader{ReaderAt: bytes.NewReader(data), allowed: allowed}
	p, err := Search(t.Context(), source, lookup, index, 1, 2, true, "all")
	if err != nil || p.Rows[0].ObservedIDR != "yes" {
		t.Fatalf("%+v %v", p, err)
	}
}

type headerReader struct {
	io.ReaderAt
	allowed map[uint64]bool
}

func (r *headerReader) ReadAt(p []byte, off int64) (int, error) {
	for i := range p {
		if !r.allowed[uint64(off)+uint64(i)] {
			return 0, errors.New("body read")
		}
	}
	return r.ReaderAt.ReadAt(p, off)
}
