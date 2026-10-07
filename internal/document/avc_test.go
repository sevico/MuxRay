package document

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func avcData(width byte, padding int, header bool) []byte {
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	appendTag := func(kind byte, payload []byte) {
		framing := make([]byte, 11)
		framing[0] = kind
		framing[1], framing[2], framing[3] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
		data = append(data, framing...)
		data = append(data, payload...)
		var previous [4]byte
		binary.BigEndian.PutUint32(previous[:], uint32(11+len(payload)))
		data = append(data, previous[:]...)
	}
	if header {
		appendTag(9, []byte{0x17, 0, 0, 0, 0, 1, 0x42, 0, 0x1e, 0xfc | (width - 1), 0xe0, 0})
	}
	for range padding {
		appendTag(18, []byte{5})
	}
	payload := []byte{0x27, 1, 0xff, 0xff, 0xfe}
	length := make([]byte, int(width))
	length[len(length)-1] = 1
	payload = append(payload, length...)
	payload = append(payload, 0x65)
	appendTag(9, payload)
	return data
}

func field(t *testing.T, detail *detailview.Detail, name string) detailview.Field {
	t.Helper()
	if detail == nil {
		t.Fatal("no projected detail")
	}
	for _, field := range detail.Fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("field %q missing", name)
	return detailview.Field{}
}

// These framing fixtures deliberately contain only an IDR NAL header. Deep
// parsing must retain valid container/NAL framing without calling its absent
// slice prefix complete.
func requireTruncatedAVCSlice(t *testing.T, detail *detailview.Detail) {
	t.Helper()
	if detail.Complete || field(t, detail, "prefixComplete").Value != "false" {
		t.Fatalf("header-only IDR marked complete: %+v", detail)
	}
	for _, diagnostic := range detail.Diagnostics {
		if diagnostic.Code == "AVC_RBSP_SYNTAX_TRUNCATED" {
			return
		}
	}
	t.Fatal("missing slice prefix was not diagnosed")
}

func TestDefaultDocumentAVCResolvesUnderSourceOwner(t *testing.T) {
	for _, width := range []byte{1, 2, 4} {
		s := source(t, avcData(width, 0, true))
		d := startTest(t, s, nil, DetailTimeout)
		if err := scanDone(t, d); err != nil {
			t.Fatal(err)
		}
		if _, err := d.StartDetail(1, 1); err != nil {
			t.Fatal(err)
		}
		got := result(t, d, 1)
		if got.Err != nil || got.Detail == nil || got.Detail.Kind != "avcVideo" {
			t.Fatalf("width %d: %+v", width, got)
		}
		requireTruncatedAVCSlice(t, got.Detail)
		if field(t, got.Detail, "AVC context").Value != "found" || field(t, got.Detail, "sourceTagId").Value != "0" {
			t.Fatalf("context: %+v", got.Detail.Fields)
		}
		origin := field(t, got.Detail, "sourceLengthSizeMinusOne")
		if origin.Span == nil || origin.Span.ByteOffset != "33" || origin.Span.BitOffset != 6 || origin.Span.BitLength != 2 {
			t.Fatalf("configuration origin: %+v", origin)
		}
		if s.closes.Load() != 0 {
			t.Fatal("AVC lookup closed retained source")
		}
	}
}

func TestDesktopAVCContextIndexCrossesFormerDistanceBudget(t *testing.T) {
	for _, tc := range []struct {
		padding        int
		header         bool
		state          string
		tagID          uint64
		parameterState string
	}{{0, false, "missing", 0, "missing"}, {8192, true, "found", 8193, "budget_exhausted"}, {8191, true, "found", 8192, "missing"}} {
		s := source(t, avcData(4, tc.padding, tc.header))
		d := startTest(t, s, nil, DetailTimeout)
		if err := scanDone(t, d); err != nil {
			t.Fatal(err)
		}
		if _, err := d.StartDetail(1, tc.tagID); err != nil {
			t.Fatal(err)
		}
		got := result(t, d, 1)
		if got.Err != nil || got.Detail == nil || field(t, got.Detail, "AVC context").Value != tc.state {
			t.Fatalf("%s: %+v", tc.state, got)
		}
		if got.Detail.Complete || field(t, got.Detail, "Parameter context").Value != tc.parameterState {
			t.Fatalf("independent parameter context: %+v", got.Detail)
		}
		if tc.state == "found" {
			requireTruncatedAVCSlice(t, got.Detail)
		}
	}
}

func TestAVCContextReadSharesCancellationAndCloseLifetime(t *testing.T) {
	s := source(t, avcData(4, 0, true))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 24 // retained sequence-header payload, not the selected NALU
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 1); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := d.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked context read close: %v", err)
	}
	if d.Snapshot().State.Terminal() || s.closes.Load() != 0 {
		t.Fatal("configuration read escaped document ownership")
	}
	close(s.release)
	if err := d.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.closes.Load() != 1 {
		t.Fatal("source not closed exactly once")
	}
}

func TestAVCContextMutationDiscardsResolvedDetail(t *testing.T) {
	s := source(t, avcData(4, 0, true))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.blockOffset = 24
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 1); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	close(s.release)
	if got := result(t, d, 1); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil {
		t.Fatalf("changed context published detail: %+v", got)
	}
}

func TestAVCCanceledScanPrefixRetainsContext(t *testing.T) {
	data := avcData(4, 0, true)
	// Append one more tag and block its header, preserving configuration+NALU.
	offset := len(data)
	data = append(data, 18, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 11)
	s := source(t, data)
	s.blockOffset = int64(offset)
	s.armed.Store(true)
	d := startTest(t, s, nil, DetailTimeout)
	receive(t, s.entered)
	d.Cancel()
	close(s.release)
	if err := scanDone(t, d); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan outcome: %v", err)
	}
	if _, err := d.StartDetail(1, 1); err != nil {
		t.Fatal(err)
	}
	got := result(t, d, 1)
	if got.Err != nil || got.Detail == nil || field(t, got.Detail, "AVC context").Value != "found" {
		t.Fatalf("partial context: %+v", got)
	}
	requireTruncatedAVCSlice(t, got.Detail)
	if d.Snapshot().State != session.StateCanceled {
		t.Fatal("detail altered scan outcome")
	}
}

func TestAVCDefaultSourceValidationKeepsScanContract(t *testing.T) {
	s := source(t, avcData(4, 0, true))
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := start(t.Context(), s, info, flv.Options{})
	t.Cleanup(func() { _ = d.Close(context.Background()) })
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil || !got.Detail.Complete || field(t, got.Detail, "AVC context").Value != "not_needed" {
		t.Fatalf("self-contained configuration: %+v", got)
	}
}
