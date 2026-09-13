package document

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func hexResult(t *testing.T, d *Document, id uint64) HexResult {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		got, err := d.GetHex(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != "pending" {
			return got
		}
		select {
		case <-deadline:
			t.Fatal("hex read did not finish")
		default:
			runtime.Gosched()
		}
	}
}

func TestHexWindowsBoundsCopiesAndIdempotency(t *testing.T) {
	data := fixture(bytes.Repeat([]byte{0x00, 0x7f, 0x80, 0xff}, 1536))
	s := source(t, data)
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		offset uint64
		count  uint32
		length int
		eof    bool
	}{{0, 0, 512, false}, {25, 4, 4, false}, {0, MaxHexBytes, 4096, false}, {uint64(len(data) - 3), MaxHexBytes, 3, true}, {uint64(len(data)), 0, 0, true}}
	for i, tc := range cases {
		id := uint64(i + 1)
		before := s.reads.Load()
		if _, err := d.StartHex(id, tc.offset, tc.count); err != nil {
			t.Fatal(err)
		}
		got := hexResult(t, d, id)
		count := tc.count
		if count == 0 {
			count = DefaultHexBytes
		}
		if got.Err != nil || got.State != "ready" || got.RequestID != id || got.Offset != tc.offset || got.Count != count || got.Window == nil {
			t.Fatalf("window %d: %+v", id, got)
		}
		w := got.Window
		if w.Offset != tc.offset || w.SourceSize != uint64(len(data)) || w.EOF != tc.eof || w.Bytes == nil || !bytes.Equal(w.Bytes, data[int(tc.offset):int(tc.offset)+tc.length]) {
			t.Fatalf("window %d: %+v", id, w)
		}
		wantReads := before
		if tc.length != 0 {
			wantReads++
		}
		if s.reads.Load() != wantReads {
			t.Fatal("window performed more than one bounded read or read at EOF")
		}
		if _, err := d.StartHex(id, tc.offset, count); err != nil || s.reads.Load() != wantReads {
			t.Fatalf("duplicate reread: %v", err)
		}
		if _, err := d.StartHex(id, tc.offset, count-1); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("changed count accepted: %v", err)
		}
		if _, err := d.StartHex(id, tc.offset+1, count); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("changed offset accepted: %v", err)
		}
		if len(w.Bytes) > 0 {
			w.Bytes[0] ^= 0xff
		}
		w.Offset, w.SourceSize, w.EOF = 999, 1, !tc.eof
		again, err := d.GetHex(id)
		if err != nil || again.Window.Offset != tc.offset || again.Window.SourceSize != uint64(len(data)) || again.Window.EOF != tc.eof || !bytes.Equal(again.Window.Bytes, data[int(tc.offset):int(tc.offset)+tc.length]) {
			t.Fatal("query exposed retained window storage")
		}
	}
	for _, tc := range []struct {
		id, offset uint64
		count      uint32
	}{{0, 0, 1}, {100, uint64(len(data)) + 1, 1}, {100, math.MaxUint64, 1}, {100, 0, MaxHexBytes + 1}} {
		if _, err := d.StartHex(tc.id, tc.offset, tc.count); !errors.Is(err, ErrInvalidRequest) {
			t.Fatalf("invalid request %+v: %v", tc, err)
		}
	}
	if _, err := d.StartHex(6, 0, 1); err != nil {
		t.Fatalf("invalid request consumed a generation: %v", err)
	}
	if got := hexResult(t, d, 6); got.Err != nil || s.closes.Load() != 0 {
		t.Fatal("hex lost retained source")
	}
	if _, err := d.GetHex(0); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal(err)
	}
	if _, err := d.GetHex(7); !errors.Is(err, ErrUnknownRequest) {
		t.Fatal(err)
	}
}

type hexSizedInfo struct {
	os.FileInfo
	size int64
}

func (s hexSizedInfo) Size() int64 { return s.size }

// Admission is tested independently of filesystem maximum sizes. No worker is
// started for this synthetic size; pending offsets must remain uint64-exact.
func TestHexAdmissionPreservesLargeOffsets(t *testing.T) {
	s := source(t, fixture())
	defer s.Close()
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	scan := session.Start(t.Context(), bytes.NewReader(fixture()), uint64(len(fixture())), flv.Options{})
	defer scan.Close(t.Context())
	if err := scan.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	d := &Document{ctx: t.Context(), initial: hexSizedInfo{info, math.MaxInt64}, scan: scan, wake: make(chan struct{}, 1)}
	for i, offset := range []uint64{1<<53 + 1, math.MaxInt64 - 1, math.MaxInt64} {
		got, err := d.StartHex(uint64(i+1), offset, MaxHexBytes)
		if err != nil || got.Offset != offset || d.pending.offset != offset || got.Count != MaxHexBytes {
			t.Fatalf("large offset %d: %+v %v", offset, got, err)
		}
	}
	if _, err := d.StartHex(4, uint64(math.MaxInt64)+1, 1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("unrepresentable offset: %v", err)
	}
}

func TestHexReadsOutsideCanceledAndDamagedIndex(t *testing.T) {
	data := fixture(script(), script())
	s := source(t, data)
	s.blockOffset = int64(13 + 15 + len(script()))
	s.armed.Store(true)
	d := startTest(t, s, nil, DetailTimeout)
	receive(t, s.entered)
	if _, err := d.StartHex(1, 0, 1); !errors.Is(err, session.ErrNotReady) {
		t.Fatalf("live scan admitted hex: %v", err)
	}
	d.Cancel()
	close(s.release)
	if err := scanDone(t, d); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 1); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("expected unindexed second tag: %v", err)
	}
	offset := uint64(len(data) - 4)
	if _, err := d.StartHex(1, offset, 4); err != nil {
		t.Fatal(err)
	}
	if got := hexResult(t, d, 1); got.Err != nil || !bytes.Equal(got.Window.Bytes, data[offset:]) || !got.Window.EOF || s.closes.Load() != 0 {
		t.Fatalf("unindexed bytes unavailable: %+v", got)
	}
	for _, damaged := range [][]byte{{'F', 'L'}, data[:len(data)-8]} {
		other := source(t, damaged)
		doc := startTest(t, other, nil, DetailTimeout)
		if err := scanDone(t, doc); err != nil {
			t.Fatal(err)
		}
		if _, err := doc.StartHex(1, 0, 0); err != nil {
			t.Fatal(err)
		}
		if got := hexResult(t, doc, 1); got.Err != nil || !bytes.Equal(got.Window.Bytes, damaged) || !got.Window.EOF {
			t.Fatalf("damaged file unavailable: %+v", got)
		}
	}
}

func TestHexAndDetailsShareLatestWorker(t *testing.T) {
	for _, hexFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "detail-to-hex", true: "hex-to-detail"}[hexFirst], func(t *testing.T) {
			s := source(t, fixture(script(), script()))
			var parses atomic.Int32
			parse := func(ctx context.Context, r io.ReaderAt, tag flv.TagRecord) (*detailview.Detail, error) {
				parses.Add(1)
				return detailview.Parse(ctx, r, tag)
			}
			d := startTest(t, s, parse, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			s.armed.Store(true)
			if hexFirst {
				_, err := d.StartHex(1, 0, 8)
				if err != nil {
					t.Fatal(err)
				}
			} else {
				_, err := d.StartDetail(1, 0)
				if err != nil {
					t.Fatal(err)
				}
			}
			receive(t, s.entered)
			reads := s.reads.Load()
			if _, err := d.StartHex(2, 0, 8); err != nil {
				t.Fatal(err)
			}
			if _, err := d.StartDetail(2, 0); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("reused ID changed kind: %v", err)
			}
			if _, err := d.GetDetail(2); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("wrong query kind: %v", err)
			}
			if _, err := d.StartDetail(3, 1); err != nil {
				t.Fatal(err)
			}
			if _, err := d.StartHex(3, 0, 8); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("reused ID changed kind: %v", err)
			}
			if _, err := d.GetHex(3); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf("wrong query kind: %v", err)
			}
			if hexFirst {
				if _, err := d.StartDetail(4, 0); err != nil {
					t.Fatal(err)
				}
			} else {
				if _, err := d.StartHex(4, 0, 8); err != nil {
					t.Fatal(err)
				}
			}
			if s.reads.Load() != reads {
				t.Fatal("a newer task bypassed blocked I/O")
			}
			if _, err := d.GetHex(1); !errors.Is(err, ErrStaleRequest) {
				t.Fatal(err)
			}
			if _, err := d.GetDetail(1); !errors.Is(err, ErrStaleRequest) {
				t.Fatal(err)
			}
			if _, err := d.StartHex(2, 0, 8); !errors.Is(err, ErrStaleRequest) {
				t.Fatal(err)
			}
			if _, err := d.StartDetail(2, 0); !errors.Is(err, ErrStaleRequest) {
				t.Fatal(err)
			}
			close(s.release)
			if hexFirst {
				got := result(t, d, 4)
				if got.Err != nil || got.Detail == nil || got.TagID != 0 {
					t.Fatalf("latest detail: %+v", got)
				}
			} else {
				got := hexResult(t, d, 4)
				if got.Err != nil || !bytes.Equal(got.Window.Bytes, fixture()[:8]) || s.reads.Load() != reads+1 {
					t.Fatalf("latest hex: %+v", got)
				}
			}
			if parses.Load() != 1 {
				t.Fatalf("superseded pending parser ran: %d", parses.Load())
			}
		})
	}
}

func TestHexSourceChangeIsStickyAcrossKinds(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "during"}[during], func(t *testing.T) {
			s := source(t, fixture(script()))
			d := startTest(t, s, nil, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			initial, err := s.Stat()
			if err != nil {
				t.Fatal(err)
			}
			mutate := func() {
				if err := os.Chtimes(s.Name(), initial.ModTime(), initial.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			if during {
				s.armed.Store(true)
			} else {
				mutate()
			}
			if _, err := d.StartHex(1, 0, 8); err != nil {
				t.Fatal(err)
			}
			if during {
				receive(t, s.entered)
				mutate()
				close(s.release)
			}
			if got := hexResult(t, d, 1); got.State != "failed" || got.Window != nil || !errors.Is(got.Err, ErrSourceChanged) {
				t.Fatalf("changed read: %+v", got)
			}
			if err := os.Chtimes(s.Name(), initial.ModTime(), initial.ModTime()); err != nil {
				t.Fatal(err)
			}
			reads := s.reads.Load()
			if _, err := d.StartDetail(2, 0); err != nil {
				t.Fatal(err)
			}
			if got := result(t, d, 2); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil {
				t.Fatalf("change not shared with detail: %+v", got)
			}
			if _, err := d.StartHex(3, uint64(initial.Size()), 0); err != nil {
				t.Fatal(err)
			}
			if got := hexResult(t, d, 3); !errors.Is(got.Err, ErrSourceChanged) || got.Window != nil || s.reads.Load() != reads {
				t.Fatalf("restored EOF cleared source latch: %+v", got)
			}
		})
	}
}

type hexFaultSource struct {
	*testSource
	fail atomic.Bool
	read func([]byte) (int, error)
}

func (s *hexFaultSource) ReadAt(p []byte, offset int64) (int, error) {
	if s.fail.Load() {
		return s.read(p)
	}
	return s.testSource.ReadAt(p, offset)
}

func TestHexReadErrorsNeverPublishPartialBytes(t *testing.T) {
	failure := errors.New("synthetic hex I/O failure")
	cases := []struct {
		name string
		read func([]byte) (int, error)
		want error
	}{
		{"short-nil", func(p []byte) (int, error) { return len(p) - 1, nil }, io.ErrUnexpectedEOF},
		{"short-eof", func(p []byte) (int, error) { return len(p) - 1, io.EOF }, io.ErrUnexpectedEOF},
		{"short-error", func(p []byte) (int, error) { return len(p) - 1, failure }, failure},
		{"full-error", func(p []byte) (int, error) { return len(p), failure }, failure},
		{"full-eof", func(p []byte) (int, error) { copy(p, []byte("FLV")); return len(p), io.EOF }, nil},
		{"negative-count", func([]byte) (int, error) { return -1, nil }, ErrInvalidRequest},
		{"over-count", func(p []byte) (int, error) { return len(p) + 1, nil }, ErrInvalidRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := &hexFaultSource{testSource: source(t, fixture()), read: tc.read}
			info, err := s.Stat()
			if err != nil {
				t.Fatal(err)
			}
			d := start(t.Context(), s, info, flv.Options{})
			t.Cleanup(func() { _ = d.Close(context.Background()) })
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			s.fail.Store(true)
			if _, err := d.StartHex(1, 0, 3); err != nil {
				t.Fatal(err)
			}
			got := hexResult(t, d, 1)
			if tc.want == nil {
				if got.Err != nil || got.State != "ready" || got.Window == nil || string(got.Window.Bytes) != "FLV" {
					t.Fatalf("legal full EOF: %+v", got)
				}
			} else if !errors.Is(got.Err, tc.want) || got.State != "failed" || got.Window != nil {
				t.Fatalf("read failure: %+v", got)
			}
		})
	}
}

func TestHexDeadlineCannotBypassBlockedRead(t *testing.T) {
	s := source(t, fixture(script()))
	d := startTest(t, s, nil, 10*time.Millisecond)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(true)
	if _, err := d.StartHex(1, 0, 8); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	// The OS read ignores cancellation. Let its deadline expire while the gate
	// stays closed, then require pending until the actual read has returned.
	time.Sleep(30 * time.Millisecond)
	if got, err := d.GetHex(1); err != nil || got.State != "pending" || got.Window != nil {
		t.Fatalf("deadline faked completed I/O: %+v %v", got, err)
	}
	close(s.release)
	if got := hexResult(t, d, 1); !errors.Is(got.Err, context.DeadlineExceeded) || got.Window != nil || got.State != "failed" {
		t.Fatalf("deadline published bytes: %+v", got)
	}
}

func TestHexCloseRetainsBlockedSourceAndClearsAllResults(t *testing.T) {
	s := source(t, fixture(script()))
	d := startTest(t, s, nil, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	s.armed.Store(true)
	if _, err := d.StartHex(2, 0, 8); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := d.Close(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("close wait: %v", err)
	}
	if snapshot := d.Snapshot(); !snapshot.Closed || snapshot.State.Terminal() || s.closes.Load() != 0 {
		t.Fatalf("blocked source released: %+v", snapshot)
	}
	if _, err := d.StartHex(3, 0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.GetHex(2); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(3, 0); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	if _, err := d.Page(0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatal(err)
	}
	d.mu.Lock()
	cleared := d.latest.Detail == nil && d.hex.Window == nil && d.pending == nil
	d.mu.Unlock()
	if !cleared {
		t.Fatal("Close retained detail, hex bytes, or pending work")
	}
	close(s.release)
	if err := d.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(t.Context()); err != nil || s.closes.Load() != 1 || !d.Snapshot().State.Terminal() {
		t.Fatalf("final close: %v count=%d", err, s.closes.Load())
	}
}
