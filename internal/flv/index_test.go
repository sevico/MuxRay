package flv

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestChunkedIndexBoundaries(t *testing.T) {
	for _, count := range []uint64{0, 1, 1023, 1024, 1025, 2049} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source := &sparseFLV{tags: count, dataOffset: 9}
			got, err := ScanContext(nil, source, source.size(), Options{MaxTags: 2049})
			if err != nil || got.HasErrors() || got.Index().Len() != count || got.Tags != nil {
				t.Fatalf("scan: count=%d complete=%v err=%v", got.Index().Len(), got.Complete, err)
			}
			if uint64(len(got.index.blocks)) != (count+tagBlockSize-1)/tagBlockSize {
				t.Fatal("unexpected block count")
			}
			for i, block := range got.index.blocks {
				want := int(min(uint64(tagBlockSize), 2049-uint64(i*tagBlockSize)))
				if cap(block) != want {
					t.Fatalf("block %d cap=%d want=%d", i, cap(block), want)
				}
			}
			seen := uint64(0)
			for id, record := range got.Index().All() {
				byID, ok := got.Index().Get(id)
				if !ok || id != seen || record != byID || record.ID != id || record.Offset != 13+id*15 {
					t.Fatal("iteration/Get mismatch")
				}
				seen++
			}
			if seen != count {
				t.Fatal("iteration count mismatch")
			}
			if _, ok := got.Index().Get(count); ok {
				t.Fatal("Get past EOF")
			}
			if _, ok := got.Index().Get(math.MaxUint64); ok {
				t.Fatal("Get overflow")
			}
			if count > 0 {
				seen = 0
				for range got.Index().All() {
					seen++
					break
				}
				if seen != 1 {
					t.Fatal("iterator did not stop")
				}
			}
			page, err := got.Index().Page(count, MaxPageSize)
			if err != nil || page.Next != count || page.Total != count || page.Records == nil || len(page.Records) != 0 {
				t.Fatal("empty EOF page")
			}
		})
	}
}

func TestTagPagesBoundsAndOwnership(t *testing.T) {
	source := &sparseFLV{tags: 2050, dataOffset: 9}
	chunked, err := ScanContext(context.Background(), source, source.size(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := Scan(source, source.size(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, index := range []TagIndex{chunked.Index(), legacy.Index(), (ScanResult{}).Index()} {
		for _, bounds := range []struct {
			start uint64
			count uint32
		}{{0, 0}, {0, MaxPageSize + 1}, {math.MaxUint64, 1}, {index.Len() + 1, 1}} {
			if _, err := index.Page(bounds.start, bounds.count); err == nil {
				t.Fatalf("accepted %+v", bounds)
			}
		}
		if index.Len() == 0 {
			continue
		}
		for _, bounds := range []struct {
			start uint64
			count uint32
		}{{0, 1}, {1023, 3}, {1024, 1025}, {2049, 4096}, {0, 4096}} {
			page, err := index.Page(bounds.start, bounds.count)
			wantLength := min(uint64(bounds.count), index.Len()-bounds.start)
			if err != nil || uint64(len(page.Records)) != wantLength || page.Next != bounds.start+wantLength || page.Start != bounds.start || page.Limit != bounds.count || page.Total != index.Len() {
				t.Fatalf("page: %+v err=%v", page, err)
			}
			for i, record := range page.Records {
				original, _ := index.Get(bounds.start + uint64(i))
				if record != original {
					t.Fatal("page/Get mismatch")
				}
			}
			original, _ := index.Get(bounds.start)
			page.Records[0].Offset++
			again, _ := index.Get(bounds.start)
			if again != original {
				t.Fatal("page exposed backing records")
			}
		}
	}
}

func TestChunkedIndexConcurrentReaders(t *testing.T) {
	source := &sparseFLV{tags: 4097, dataOffset: 9}
	result, err := ScanContext(nil, source, source.size(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for worker := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id, record := range result.Index().All() {
				got, ok := result.Index().Get(id)
				if !ok || got != record {
					t.Error("concurrent Get mismatch")
					return
				}
			}
			for range 10 {
				page, err := result.Index().Page(uint64(worker*512), 1024)
				if err != nil {
					t.Error(err)
					return
				}
				page.Records[0].ID = math.MaxUint64
			}
		}()
	}
	wg.Wait()
}

func TestScanContextCancellationBoundaries(t *testing.T) {
	data := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: 18}, fixtureTag{rawTypeFlags: 18})
	for call, offset := range []uint64{0, 9, 13, 24, 28, 39} {
		t.Run(fmt.Sprint(call+1), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &interruptedReader{source: bytes.NewReader(data), at: call + 1, cancel: cancel}
			got, err := ScanContext(ctx, source, uint64(len(data)), Options{})
			wantTags := uint64(0)
			if call >= 4 {
				wantTags = 1
			}
			if !errors.Is(err, context.Canceled) || got.Complete || got.BytesScanned != offset || got.Index().Len() != wantTags || len(got.Diagnostics) != 0 || source.calls != call+1 {
				t.Fatalf("cancel at %d: %+v err=%v calls=%d", call+1, got, err, source.calls)
			}
			page, pageErr := got.Index().Page(0, 2)
			if pageErr != nil || uint64(len(page.Records)) != wantTags {
				t.Fatal("partial index not queryable")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	deadline, stop := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer stop()
	for _, ctx := range []context.Context{ctx, deadline} {
		source := &interruptedReader{source: bytes.NewReader(data)}
		got, err := ScanContext(ctx, source, uint64(len(data)), Options{})
		if !errors.Is(err, ctx.Err()) || got.Complete || got.BytesScanned != 0 || got.Index().Len() != 0 || len(got.Diagnostics) != 0 || source.calls != 0 {
			t.Fatalf("pre-canceled scan: %+v, %v", got, err)
		}
	}
}

func TestScanContextIOFailurePreservesPrefix(t *testing.T) {
	data := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: 18, streamID: 1}, fixtureTag{rawTypeFlags: 18})
	wantErr := errors.New("source unavailable")
	source := &interruptedReader{source: bytes.NewReader(data), at: 5, err: wantErr}
	got, err := ScanContext(nil, source, uint64(len(data)), Options{})
	if !errors.Is(err, wantErr) || got.Complete || got.BytesScanned != 28 || got.Index().Len() != 1 {
		t.Fatalf("I/O partial result: %+v err=%v", got, err)
	}
	assertDiagnostic(t, got, CodeStreamIDNonZero)
	if _, err := ScanContext(nil, nil, 0, Options{}); err == nil {
		t.Fatal("nil source accepted")
	}
	if _, err := ScanContext(nil, bytes.NewReader(nil), 0, Options{MaxTags: HardMaxTags + 1}); err == nil {
		t.Fatal("oversized budget accepted")
	}
}

func TestScanContextSparseLargeFileAndHardLimit(t *testing.T) {
	// More than 10 GiB of declared source, but only structural bytes exist here.
	source := &sparseFLV{tags: 641, payloadSize: 0xffffff, dataOffset: math.MaxUint32}
	got, err := ScanContext(nil, source, source.size(), Options{})
	if err != nil || got.HasErrors() || got.BytesScanned != source.size() || got.Index().Len() != source.tags {
		t.Fatalf("sparse scan: %+v %v", got, err)
	}
	first, _ := got.Index().Get(0)
	last, _ := got.Index().Get(source.tags - 1)
	if first.Offset <= math.MaxUint32 || last.PayloadSpan().ByteOffset <= math.MaxUint32 || source.bytesRead != 13+source.tags*15 || len(got.index.blocks) != 1 || cap(got.index.blocks[0]) != tagBlockSize {
		t.Fatal("large offsets, bounded reads or lazy allocation failed")
	}
	source = &sparseFLV{tags: HardMaxTags + 1, dataOffset: 9}
	got, err = ScanContext(nil, source, source.size(), Options{})
	if err != nil || got.Complete || got.Index().Len() != HardMaxTags || got.BytesScanned != 13+HardMaxTags*15 {
		t.Fatalf("hard cap: complete=%v count=%d offset=%d err=%v", got.Complete, got.Index().Len(), got.BytesScanned, err)
	}
	assertDiagnostic(t, got, CodeTagLimitExceeded)
	var capacity uint64
	for _, block := range got.index.blocks {
		capacity += uint64(cap(block))
	}
	if capacity != HardMaxTags {
		t.Fatalf("allocated %d records beyond budget", capacity)
	}
}

func TestScanContextLegacyEquivalence(t *testing.T) {
	data := makeFLV(t, 5, nil, fixtureTag{rawTypeFlags: 0xf2, payload: []byte{1, 2, 3}, streamID: 1}, fixtureTag{rawTypeFlags: 8, payload: []byte{0xaf, 1}})
	for size := 0; size <= len(data); size++ {
		for _, options := range []Options{{}, {MaxTags: 1, MaxDiagnostics: 2}} {
			legacy, err := Scan(bytes.NewReader(data[:size]), uint64(size), options)
			chunked, contextErr := ScanContext(nil, bytes.NewReader(data[:size]), uint64(size), options)
			if err != nil || contextErr != nil {
				t.Fatalf("scan: %v %v", err, contextErr)
			}
			assertScanEquivalent(t, legacy, chunked)
		}
	}
}

func assertScanEquivalent(t *testing.T, legacy, chunked ScanResult) {
	t.Helper()
	if chunked.Tags != nil || legacy.Index().Len() != chunked.Index().Len() {
		t.Fatal("storage/count mismatch")
	}
	for id, want := range legacy.Index().All() {
		got, ok := chunked.Index().Get(id)
		if !ok || got != want {
			t.Fatalf("record %d differs", id)
		}
	}
	chunked.Tags = legacy.Tags
	chunked.index = nil
	if !reflect.DeepEqual(legacy, chunked) {
		t.Fatalf("scan metadata differs: %+v / %+v", legacy, chunked)
	}
}

type interruptedReader struct {
	source    io.ReaderAt
	calls, at int
	cancel    context.CancelFunc
	err       error
}

func (r *interruptedReader) ReadAt(p []byte, offset int64) (int, error) {
	r.calls++
	if r.calls == r.at && r.err != nil {
		return 0, r.err
	}
	n, err := r.source.ReadAt(p, offset)
	if r.calls == r.at && r.cancel != nil {
		r.cancel()
	}
	return n, err
}

// sparseFLV refuses payload/extension reads and synthesizes only framing.
type sparseFLV struct {
	tags                    uint64
	payloadSize, dataOffset uint32
	bytesRead               uint64
}

func (r *sparseFLV) size() uint64 {
	return uint64(r.dataOffset) + 4 + r.tags*(15+uint64(r.payloadSize))
}
func (r *sparseFLV) ReadAt(p []byte, offset int64) (int, error) {
	clear(p)
	start := uint64(r.dataOffset) + 4
	switch {
	case offset == 0 && len(p) == 9:
		copy(p, []byte{'F', 'L', 'V', 1, 0})
		binary.BigEndian.PutUint32(p[5:], r.dataOffset)
	case uint64(offset) == uint64(r.dataOffset) && len(p) == 4:
	case uint64(offset) >= start && uint64(offset) < r.size():
		relative := (uint64(offset) - start) % (15 + uint64(r.payloadSize))
		if relative == 0 && len(p) == 11 {
			p[0] = 18
			p[1] = byte(r.payloadSize >> 16)
			p[2] = byte(r.payloadSize >> 8)
			p[3] = byte(r.payloadSize)
		} else if relative == 11+uint64(r.payloadSize) && len(p) == 4 {
			binary.BigEndian.PutUint32(p, 11+r.payloadSize)
		} else {
			return 0, fmt.Errorf("unexpected payload read at %d length %d", offset, len(p))
		}
	default:
		return 0, io.EOF
	}
	r.bytesRead += uint64(len(p))
	if uint64(offset)+uint64(len(p)) == r.size() {
		return len(p), io.EOF
	}
	return len(p), nil
}

func FuzzScanContextPage(f *testing.F) {
	f.Add([]byte{18, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 11}, uint64(0), uint32(1))
	f.Add([]byte{}, uint64(math.MaxUint64), uint32(math.MaxUint32))
	f.Fuzz(func(t *testing.T, body []byte, start uint64, count uint32) {
		data := append([]byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}, body...)
		opts := Options{MaxTags: 2049, MaxDiagnostics: 8}
		legacy, err := Scan(bytes.NewReader(data), uint64(len(data)), opts)
		chunked, contextErr := ScanContext(nil, bytes.NewReader(data), uint64(len(data)), opts)
		if err != nil || contextErr != nil {
			t.Fatalf("unexpected I/O error: %v %v", err, contextErr)
		}
		assertScanEquivalent(t, legacy, chunked)
		page, pageErr := chunked.Index().Page(start, count)
		if count == 0 || count > MaxPageSize || start > chunked.Index().Len() {
			if pageErr == nil {
				t.Fatal("invalid page accepted")
			}
			return
		}
		if pageErr != nil || page.Next > page.Total || page.Next < page.Start || uint64(len(page.Records)) != page.Next-page.Start {
			t.Fatalf("bad page: %+v %v", page, pageErr)
		}
		for i, record := range page.Records {
			want, _ := legacy.Index().Get(start + uint64(i))
			if record != want {
				t.Fatal("page differs")
			}
		}
	})
}

func BenchmarkScanContextTags(b *testing.B) {
	for _, count := range []uint64{10_000, HardMaxTags} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			source := &sparseFLV{tags: count, dataOffset: 9}
			b.ReportAllocs()
			for range b.N {
				got, err := ScanContext(nil, source, source.size(), Options{})
				if err != nil || got.HasErrors() {
					b.Fatal(err)
				}
				benchmarkScanResult = got
			}
		})
	}
}

var benchmarkPage TagPage

func BenchmarkTagPage(b *testing.B) {
	for _, count := range []uint64{10_000, HardMaxTags} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			source := &sparseFLV{tags: count, dataOffset: 9}
			got, err := ScanContext(nil, source, source.size(), Options{})
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				benchmarkPage, err = got.Index().Page(1023, DefaultPageSize)
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
