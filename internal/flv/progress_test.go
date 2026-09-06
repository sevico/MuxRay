package flv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"sync"
	"testing"
)

func TestScanProgressBoundaries(t *testing.T) {
	for _, count := range []uint64{0, 1, 1023, 1024, 1025, 2049, HardMaxTags} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source := &sparseFLV{tags: count, dataOffset: 9}
			var updates []ScanProgress
			result, err := ScanContextWithProgress(nil, source, source.size(), Options{}, func(p ScanProgress) {
				updates = append(updates, p)
			})
			if err != nil || result.HasErrors() || result.Tags != nil {
				t.Fatalf("scan: complete=%v err=%v", result.Complete, err)
			}
			assertProgress(t, source.size(), result, updates)
			if got, want := len(updates), int(count/tagBlockSize)+3; got != want {
				t.Fatalf("updates=%d want=%d", got, want)
			}
			if updates[1].BytesScanned != 13 || updates[1].TagCount != 0 {
				t.Fatalf("container progress: %+v", updates[1])
			}
			for i, update := range updates[2 : len(updates)-1] {
				want := uint64(i+1) * tagBlockSize
				if update.TagCount != want || update.BytesScanned != 13+want*15 {
					t.Fatalf("block progress: %+v want tags=%d", update, want)
				}
			}
			if source.bytesRead != 13+15*count {
				t.Fatalf("unexpected I/O: %d", source.bytesRead)
			}
		})
	}
}

func TestScanProgressMalformedEquivalence(t *testing.T) {
	data := makeFLV(t, 5, []byte{1, 2}, fixtureTag{rawTypeFlags: 0xf2, payload: []byte{1, 2, 3}, streamID: 1}, fixtureTag{rawTypeFlags: 8})
	for size := 0; size <= len(data); size++ {
		for _, options := range []Options{{}, {MaxTags: 1, MaxDiagnostics: 2}} {
			var updates []ScanProgress
			got, err := ScanContextWithProgress(nil, bytes.NewReader(data[:size]), uint64(size), options, func(p ScanProgress) {
				updates = append(updates, p)
			})
			want, wantErr := Scan(bytes.NewReader(data[:size]), uint64(size), options)
			if err != nil || wantErr != nil {
				t.Fatalf("scan: %v / %v", err, wantErr)
			}
			assertScanEquivalent(t, want, got)
			assertProgress(t, uint64(size), got, updates)
		}
	}
}

func TestScanProgressEarlyErrors(t *testing.T) {
	for _, options := range []Options{{MaxTags: HardMaxTags + 1}, {MaxDiagnostics: 1}, {MaxDiagnostics: HardMaxDiagnostics + 1}} {
		source := &interruptedReader{source: bytes.NewReader(nil)}
		var updates []ScanProgress
		result, err := ScanContextWithProgress(nil, source, 123, options, func(p ScanProgress) { updates = append(updates, p) })
		if err == nil || source.calls != 0 || len(updates) != 2 {
			t.Fatalf("validation: err=%v calls=%d updates=%v", err, source.calls, updates)
		}
		assertProgress(t, 123, result, updates)
	}
	var updates []ScanProgress
	result, err := ScanContextWithProgress(nil, nil, 123, Options{}, func(p ScanProgress) { updates = append(updates, p) })
	if err == nil || len(updates) != 2 {
		t.Fatalf("nil source: %v %v", err, updates)
	}
	assertProgress(t, 123, result, updates)
}

func TestScanProgressInterruptedReads(t *testing.T) {
	data := makeFLV(t, 0, nil, fixtureTag{rawTypeFlags: 18, streamID: 1}, fixtureTag{rawTypeFlags: 18})
	for _, cancelRead := range []bool{false, true} {
		for call := 1; call <= 6; call++ {
			ctx, cancel := context.WithCancel(context.Background())
			wantErr := io.ErrUnexpectedEOF
			source := &interruptedReader{source: bytes.NewReader(data), at: call, err: wantErr}
			if cancelRead {
				source.cancel = cancel
				source.err = nil
				wantErr = context.Canceled
			}
			var updates []ScanProgress
			result, err := ScanContextWithProgress(ctx, source, uint64(len(data)), Options{}, func(p ScanProgress) { updates = append(updates, p) })
			cancel()
			if !errors.Is(err, wantErr) || result.Complete || source.calls != call {
				t.Fatalf("interrupted call %d: %+v %v", call, result, err)
			}
			assertProgress(t, uint64(len(data)), result, updates)
			wantTags := uint64(0)
			if call >= 5 {
				wantTags = 1
			}
			if result.Index().Len() != wantTags {
				t.Fatalf("interrupted tag committed at call %d", call)
			}
		}
	}
}

func TestScanProgressObserverCancellation(t *testing.T) {
	for _, at := range []int{1, 2, 3} { // initial, container, first full block
		source := &sparseFLV{tags: 2049, dataOffset: 9}
		ctx, cancel := context.WithCancel(context.Background())
		var updates []ScanProgress
		result, err := ScanContextWithProgress(ctx, source, source.size(), Options{}, func(p ScanProgress) {
			updates = append(updates, p)
			if len(updates) == at {
				cancel()
			}
		})
		cancel()
		if !errors.Is(err, context.Canceled) || result.Complete || len(updates) != at+1 || len(result.Diagnostics) != 0 {
			t.Fatalf("cancel at update %d: %+v %v", at, result, err)
		}
		assertProgress(t, source.size(), result, updates)
		if at == 1 && source.bytesRead != 0 {
			t.Fatal("pre-canceled scan performed I/O")
		}
		if at == 3 && result.Index().Len() != tagBlockSize {
			t.Fatal("block callback preceded tag commitment")
		}
	}
}

func TestScanProgressLargeOffsetsAndNilObserver(t *testing.T) {
	source := &sparseFLV{tags: 2049, payloadSize: 0xffffff, dataOffset: math.MaxUint32}
	var updates []ScanProgress
	got, err := ScanContextWithProgress(nil, source, source.size(), Options{MaxTags: 1025}, func(p ScanProgress) { updates = append(updates, p) })
	if err != nil || got.Complete || got.Index().Len() != 1025 || got.BytesScanned <= math.MaxUint32 {
		t.Fatalf("large progress: %+v %v", got, err)
	}
	assertProgress(t, source.size(), got, updates)
	want, err := ScanContextWithProgress(nil, source, source.size(), Options{MaxTags: 1025}, nil)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("nil observer changes scan: %v", err)
	}
}

func assertProgress(t *testing.T, size uint64, result ScanResult, updates []ScanProgress) {
	t.Helper()
	if len(updates) < 2 || updates[0] != (ScanProgress{SourceSize: size}) {
		t.Fatalf("missing initial/final progress: %v", updates)
	}
	var previous ScanProgress
	for _, p := range updates {
		if p.SourceSize != size || p.BytesScanned > size || p.BytesScanned < previous.BytesScanned || p.TagCount < previous.TagCount || p.DiagnosticCount < previous.DiagnosticCount || p.TagCount > result.Index().Len() || int(p.DiagnosticCount) > len(result.Diagnostics) {
			t.Fatalf("invalid progress: %+v -> %+v", previous, p)
		}
		previous = p
	}
	want := ScanProgress{SourceSize: size, BytesScanned: result.BytesScanned, TagCount: result.Index().Len(), DiagnosticCount: uint32(len(result.Diagnostics))}
	if previous != want {
		t.Fatalf("final progress: %+v want %+v", previous, want)
	}
	if uint64(len(updates)) > result.Index().Len()/tagBlockSize+3 {
		t.Fatal("too many progress updates")
	}
}

func FuzzScanProgress(f *testing.F) {
	f.Add([]byte{}, uint16(0), uint8(0))
	f.Add([]byte{18, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 11}, uint16(1), uint8(2))
	f.Fuzz(func(t *testing.T, body []byte, tagLimit uint16, diagnosticLimit uint8) {
		data := append([]byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}, body...)
		options := Options{MaxTags: uint64(tagLimit)%2049 + 1, MaxDiagnostics: uint32(diagnosticLimit)%15 + 2}
		var updates []ScanProgress
		result, err := ScanContextWithProgress(nil, bytes.NewReader(data), uint64(len(data)), options, func(p ScanProgress) { updates = append(updates, p) })
		legacy, legacyErr := Scan(bytes.NewReader(data), uint64(len(data)), options)
		if err != nil || legacyErr != nil {
			t.Fatalf("scan: %v / %v", err, legacyErr)
		}
		assertProgress(t, uint64(len(data)), result, updates)
		assertScanEquivalent(t, legacy, result)
	})
}

func BenchmarkScanProgress(b *testing.B) {
	for _, observe := range []bool{false, true} {
		b.Run(fmt.Sprintf("mutex_observer=%t", observe), func(b *testing.B) {
			source := &sparseFLV{tags: 100_000, dataOffset: 9}
			var mu sync.Mutex
			var latest ScanProgress
			var callback func(ScanProgress)
			if observe {
				callback = func(p ScanProgress) {
					mu.Lock()
					latest = p
					mu.Unlock()
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				result, err := ScanContextWithProgress(nil, source, source.size(), Options{}, callback)
				if err != nil || !result.Complete || observe && latest.TagCount != source.tags {
					b.Fatalf("scan: %v", err)
				}
			}
		})
	}
}
