package flv

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
)

func TestScanTagObserverSequenceAndTruncation(t *testing.T) {
	data := makeFLV(t, 5, nil,
		fixtureTag{rawTypeFlags: byte(TagTypeScript), payload: []byte{1, 2}},
		fixtureTag{rawTypeFlags: byte(TagTypeVideo) | 0x20, payload: []byte{3, 4, 5}},
		fixtureTag{rawTypeFlags: byte(TagTypeAudio), payload: []byte{6}},
	)
	// Every cut includes complete records, partial payloads, partial trailers,
	// and headers which must not yet be committed or observed.
	for size := 0; size <= len(data); size++ {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			var records []TagRecord
			var updates []ScanProgress
			got, err := ScanContextWithObserver(nil, bytes.NewReader(data[:size]), uint64(size), Options{}, func(p ScanProgress) {
				updates = append(updates, p)
			}, func(ctx context.Context, record TagRecord) error {
				if ctx == nil || ctx.Err() != nil || record.ID != uint64(len(records)) {
					t.Fatalf("observer context or sequence: ctx=%v record=%+v", ctx, record)
				}
				records = append(records, record)
				record.Offset++ // The callback receives an independent value.
				return nil
			})
			want, wantErr := Scan(bytes.NewReader(data[:size]), uint64(size), Options{})
			if err != nil || wantErr != nil {
				t.Fatalf("scan: %v / %v", err, wantErr)
			}
			assertScanEquivalent(t, want, got)
			assertProgress(t, uint64(size), got, updates)
			if len(records) != len(want.Tags) {
				t.Fatalf("observed %d records, committed %d", len(records), len(want.Tags))
			}
			for id, record := range records {
				if record != want.Tags[id] {
					t.Fatalf("observer saw unfinished or altered record: %+v want %+v", record, want.Tags[id])
				}
			}
		})
	}
}

func TestScanTagObserverStopsAfterCommit(t *testing.T) {
	data := makeFLV(t, 5, nil,
		fixtureTag{rawTypeFlags: byte(TagTypeScript)},
		fixtureTag{rawTypeFlags: byte(TagTypeVideo), payload: []byte{1, 2, 3}},
		fixtureTag{rawTypeFlags: byte(TagTypeAudio)},
	)
	observerErr := errors.New("observer failed")
	for _, branch := range []struct {
		name   string
		size   int
		offset uint64
	}{
		{name: "complete", size: len(data), offset: 46},
		{name: "partial_payload", size: 40, offset: 40},
		{name: "truncated_trailer", size: 44, offset: 42},
	} {
		for _, mode := range []string{"error", "cancel_and_error", "cancel"} {
			t.Run(branch.name+"/"+mode, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				source := &interruptedReader{source: bytes.NewReader(data[:branch.size])}
				var updates []ScanProgress
				var observed []TagRecord
				callsAtStop := 0
				result, err := ScanContextWithObserver(ctx, source, uint64(branch.size), Options{}, func(p ScanProgress) {
					updates = append(updates, p)
				}, func(observeCtx context.Context, record TagRecord) error {
					if observeCtx != ctx {
						t.Fatal("observer received a different scan context")
					}
					observed = append(observed, record)
					if record.ID != 1 {
						return nil
					}
					callsAtStop = source.calls
					if mode != "error" {
						cancel()
					}
					if mode == "cancel" {
						return nil
					}
					return observerErr
				})
				wantErr := observerErr
				if mode != "error" {
					wantErr = context.Canceled
				}
				if !errors.Is(err, wantErr) || mode == "error" && err == observerErr || result.Complete || result.Index().Len() != 2 || len(observed) != 2 {
					t.Fatalf("observer outcome: tags=%d observed=%d complete=%v err=%v", result.Index().Len(), len(observed), result.Complete, err)
				}
				// A successful observer may allow the truncated trailer's remaining
				// logical bytes to be consumed before cancellation is returned.
				wantOffset := branch.offset
				if mode == "cancel" && branch.name == "truncated_trailer" {
					wantOffset = uint64(branch.size)
				}
				if result.BytesScanned != wantOffset || source.calls != callsAtStop {
					t.Fatalf("scan continued I/O after observer stopped: offset=%d calls=%d want=%d/%d", result.BytesScanned, source.calls, wantOffset, callsAtStop)
				}
				for id, record := range result.Index().All() {
					if record != observed[id] {
						t.Fatal("observer failure discarded or changed committed record")
					}
				}
				if branch.name == "partial_payload" {
					assertDiagnostic(t, result, CodeTagBodyTruncated)
				}
				if branch.name == "truncated_trailer" {
					assertDiagnostic(t, result, CodePreviousTagSizeTruncated)
				}
				assertProgress(t, uint64(branch.size), result, updates)
			})
		}
	}
}

func TestScanDefaultEntriesDoNotReadPayload(t *testing.T) {
	entries := map[string]func(io.ReaderAt, uint64, Options) (ScanResult, error){
		"scan": Scan,
		"context": func(source io.ReaderAt, size uint64, options Options) (ScanResult, error) {
			return ScanContext(nil, source, size, options)
		},
		"progress": func(source io.ReaderAt, size uint64, options Options) (ScanResult, error) {
			return ScanContextWithProgress(nil, source, size, options, func(ScanProgress) {})
		},
		"nil_observer": func(source io.ReaderAt, size uint64, options Options) (ScanResult, error) {
			return ScanContextWithObserver(nil, source, size, options, nil, nil)
		},
	}
	for name, entry := range entries {
		t.Run(name, func(t *testing.T) {
			// This source rejects any access to its multi-megabyte payloads.
			source := &sparseFLV{tags: 3, payloadSize: 0xffffff, dataOffset: 9}
			result, err := entry(source, source.size(), Options{})
			if err != nil || !result.Complete || result.Index().Len() != source.tags || source.bytesRead != 13+15*source.tags {
				t.Fatalf("default scan changed I/O: complete=%v tags=%d bytes=%d err=%v", result.Complete, result.Index().Len(), source.bytesRead, err)
			}
		})
	}
}
