package document

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

// These fixtures contain video throughout, so a resolver that still probes
// every preceding video record cannot accidentally pass a script-only gap.
func indexedAVCData(payloads ...[]byte) ([]byte, []int64) {
	data := []byte{'F', 'L', 'V', 1, 1, 0, 0, 0, 9, 0, 0, 0, 0}
	offsets := make([]int64, 0, len(payloads))
	for _, payload := range payloads {
		header := make([]byte, 11)
		header[0] = byte(flv.TagTypeVideo)
		header[1], header[2], header[3] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
		data = append(data, header...)
		offsets = append(offsets, int64(len(data)))
		data = append(data, payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(11+len(payload)))
		data = append(data, previous[:]...)
	}
	return data, offsets
}

func indexedAVCConfig(width byte) []byte {
	return []byte{0x17, 0, 0, 0, 0, 1, 0x42, 0, 0x1e, 0xfc | (width - 1), 0xe0, 0}
}

func indexedAVCNAL(width byte) []byte {
	payload := []byte{0x27, 1, 0, 0, 0}
	length := make([]byte, width)
	length[len(length)-1] = 1
	payload = append(payload, length...)
	return append(payload, 0x65)
}

func indexedAVCBadConfig() []byte { return []byte{0x17, 0, 0, 0, 0, 1} }

func requireIndexedAVCContext(t *testing.T, got DetailResult, sourceID uint64, width byte, candidates uint32) {
	t.Helper()
	if got.Err != nil || got.State != "ready" || got.Detail == nil || !got.Detail.Complete || got.Detail.Kind != "avcVideo" {
		t.Fatalf("indexed AVC detail: %+v", got)
	}
	want := map[string]string{
		"AVC context": "found", "lookupMethod": "candidate_index",
		"sourceTagId":   strconv.FormatUint(sourceID, 10),
		"NALLengthSize": fmt.Sprintf("%d (source lengthSizeMinusOne + 1)", width),
		"scannedTags":   strconv.FormatUint(uint64(candidates), 10),
		"candidates":    strconv.FormatUint(uint64(candidates), 10),
	}
	for name, value := range want {
		if actual := field(t, got.Detail, name).Value; actual != value {
			t.Errorf("%s = %q, want %q", name, actual, value)
		}
	}
	if method := field(t, got.Detail, "lookupMethod"); method.Span != nil {
		t.Fatal("derived lookup method claims an encoded source span")
	}
}

func TestAVCIndexFirstDetailSkipsLongVideoDistance(t *testing.T) {
	const nalCount = 9000
	payloads := make([][]byte, nalCount+1)
	payloads[0] = indexedAVCConfig(4)
	for i := 1; i < len(payloads); i++ {
		payloads[i] = indexedAVCNAL(4)
	}
	data, _ := indexedAVCData(payloads...)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	reads := s.reads.Load()
	// No earlier detail request has parsed or cached the distant configuration.
	if _, err := d.StartDetail(1, nalCount); err != nil {
		t.Fatal(err)
	}
	requireIndexedAVCContext(t, result(t, d, 1), 0, 4, 1)
	if actual := s.reads.Load() - reads; actual >= 100 {
		t.Fatalf("tail detail made %d reads; traversed intervening video payloads", actual)
	}
}

func TestAVCIndexConfigurationChangesIgnoreRequestOrder(t *testing.T) {
	data, offsets := indexedAVCData(
		indexedAVCConfig(1), indexedAVCNAL(1),
		indexedAVCConfig(2), indexedAVCNAL(2),
		indexedAVCBadConfig(), indexedAVCNAL(2),
		indexedAVCConfig(4), indexedAVCNAL(4),
	)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	for i, tc := range []struct {
		tag, config uint64
		width       byte
		candidates  uint32
	}{{7, 6, 4, 1}, {1, 0, 1, 1}, {5, 2, 2, 2}, {3, 2, 2, 1}, {7, 6, 4, 1}, {5, 2, 2, 2}, {1, 0, 1, 1}} {
		requestID := uint64(i + 1)
		if _, err := d.StartDetail(requestID, tc.tag); err != nil {
			t.Fatal(err)
		}
		got := result(t, d, requestID)
		requireIndexedAVCContext(t, got, tc.config, tc.width, tc.candidates)
		origin := field(t, got.Detail, "sourceLengthSizeMinusOne")
		if origin.Span == nil || origin.Span.ByteOffset != strconv.FormatInt(offsets[tc.config]+9, 10) || origin.Span.BitOffset != 6 || origin.Span.BitLength != 2 {
			t.Fatalf("request %d selected %d: wrong configuration origin %+v", requestID, tc.tag, origin)
		}
	}
}

func TestAVCIndexKeepsCandidateBudget(t *testing.T) {
	for _, badCount := range []int{31, 32} {
		t.Run(strconv.Itoa(badCount), func(t *testing.T) {
			payloads := [][]byte{indexedAVCConfig(4)}
			for range badCount {
				payloads = append(payloads, indexedAVCBadConfig())
			}
			payloads = append(payloads, indexedAVCNAL(4))
			data, _ := indexedAVCData(payloads...)
			s := source(t, data)
			d := startTest(t, s, nil, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			if _, err := d.StartDetail(1, uint64(len(payloads)-1)); err != nil {
				t.Fatal(err)
			}
			got := result(t, d, 1)
			if badCount == 31 {
				requireIndexedAVCContext(t, got, 0, 4, 32)
				return
			}
			if got.Err != nil || got.Detail == nil || got.Detail.Complete {
				t.Fatalf("candidate budget did not retain partial detail: %+v", got)
			}
			for name, want := range map[string]string{"AVC context": "budget_exhausted", "lookupMethod": "candidate_index", "candidates": "32"} {
				if actual := field(t, got.Detail, name).Value; actual != want {
					t.Errorf("%s = %q, want %q", name, actual, want)
				}
			}
			if field(t, got.Detail, "NALLengthSize").Type != "unavailable" {
				t.Fatal("exhausted lookup guessed a NAL length size")
			}
			for _, diagnostic := range got.Detail.Diagnostics {
				if diagnostic.Code == detailview.CodeAVCContextLookupLimitReached {
					return
				}
			}
			t.Fatal("candidate exhaustion was not diagnosed")
		})
	}
}

func TestAVCIndexAllCandidatesExaminedMeansMissing(t *testing.T) {
	payloads := make([][]byte, AVCContextMaxCandidates, AVCContextMaxCandidates+1)
	for i := range payloads {
		payloads[i] = indexedAVCBadConfig()
	}
	payloads = append(payloads, indexedAVCNAL(4))
	data, _ := indexedAVCData(payloads...)
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, uint64(len(payloads)-1)); err != nil {
		t.Fatal(err)
	}
	got := result(t, d, 1)
	if got.Err != nil || got.Detail == nil || got.Detail.Complete {
		t.Fatalf("missing configuration: %+v", got)
	}
	for name, want := range map[string]string{"AVC context": "missing", "lookupMethod": "candidate_index", "scannedTags": "32", "candidates": "32"} {
		if actual := field(t, got.Detail, name).Value; actual != want {
			t.Errorf("%s = %q, want %q", name, actual, want)
		}
	}
	for _, diagnostic := range got.Detail.Diagnostics {
		if diagnostic.Code == detailview.CodeAVCContextLookupLimitReached {
			t.Fatal("all candidates were examined; lookup incorrectly claims budget exhaustion")
		}
	}
}

type avcIndexReadSource struct {
	*testSource
	mu      sync.Mutex
	offsets []int64
}

func (s *avcIndexReadSource) ReadAt(p []byte, offset int64) (int, error) {
	s.mu.Lock()
	s.offsets = append(s.offsets, offset)
	s.mu.Unlock()
	return s.testSource.ReadAt(p, offset)
}

func TestAVCIndexCanceledProbeRetainsOnlyCommittedPrefix(t *testing.T) {
	data, offsets := indexedAVCData(indexedAVCConfig(1), indexedAVCNAL(1), indexedAVCConfig(4), indexedAVCNAL(4))
	s := source(t, data)
	s.blockOffset = offsets[1] // This tag is committed; its observer is still blocked.
	s.armed.Store(true)
	tracked := &avcIndexReadSource{testSource: s}
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := startWithParser(t.Context(), tracked, info, flv.Options{}, nil, DetailTimeout)
	t.Cleanup(func() {
		s.armed.Store(false)
		select {
		case <-s.release:
		default:
			close(s.release)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = d.Close(ctx)
	})
	receive(t, s.entered)
	d.Cancel()
	close(s.release)
	if err := scanDone(t, d); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled probe: %v", err)
	}
	page, err := d.Page(0, 10)
	if err != nil || len(page.Records) != 2 {
		t.Fatalf("committed scan prefix: %+v %v", page, err)
	}
	if _, err := d.StartDetail(1, 1); err != nil {
		t.Fatal(err)
	}
	requireIndexedAVCContext(t, result(t, d, 1), 0, 1, 1)
	if _, err := d.StartDetail(2, 2); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("uncommitted configuration was selectable: %v", err)
	}
	tracked.mu.Lock()
	defer tracked.mu.Unlock()
	for _, offset := range tracked.offsets {
		if offset >= offsets[2]-int64(flv.TagHeaderSize) {
			t.Fatalf("read uncommitted future configuration at %d", offset)
		}
	}
	if d.Snapshot().State != session.StateCanceled {
		t.Fatal("context lookup changed the canceled scan outcome")
	}
}

func TestAVCIndexScanProbeRemainsOwnedUntilCloseCompletes(t *testing.T) {
	data, offsets := indexedAVCData(indexedAVCConfig(4), indexedAVCNAL(4))
	s := source(t, data)
	s.blockOffset = offsets[0]
	s.armed.Store(true)
	d := startTest(t, s, nil, DetailTimeout)
	receive(t, s.entered)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := d.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked scan probe close: %v", err)
	}
	if d.Snapshot().State.Terminal() || s.closes.Load() != 0 {
		t.Fatal("blocked scan probe outlived source ownership")
	}
	if _, err := d.StartDetail(1, 0); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed document admitted a detail: %v", err)
	}
	close(s.release)
	if err := d.Close(t.Context()); !errors.Is(err, context.Canceled) {
		t.Fatalf("close must retain the canceled scan outcome: %v", err)
	}
	if s.closes.Load() != 1 {
		t.Fatal("source not closed exactly once after probe returned")
	}
	if d.avcIndex != nil {
		t.Fatal("closed document retained the AVC candidate index")
	}
}

func TestAVCIndexScanMutationCannotPublishAContext(t *testing.T) {
	data, offsets := indexedAVCData(indexedAVCConfig(4), indexedAVCNAL(4))
	s := source(t, data)
	s.blockOffset = offsets[0]
	s.armed.Store(true)
	d := startTest(t, s, nil, DetailTimeout)
	receive(t, s.entered)
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	close(s.release)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	reads := s.reads.Load()
	if _, err := d.StartDetail(1, 1); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil {
		t.Fatalf("mutated scan published context: %+v", got)
	}
	if s.reads.Load() != reads {
		t.Fatal("invalidated source was read for indexed context lookup")
	}
}
