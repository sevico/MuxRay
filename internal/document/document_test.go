package document

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func fixture(payloads ...[]byte) []byte {
	data := []byte{'F', 'L', 'V', 1, 5, 0, 0, 0, 9, 0, 0, 0, 0}
	for _, payload := range payloads {
		header := make([]byte, 11)
		header[0] = byte(flv.TagTypeScript)
		header[1], header[2], header[3] = byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload))
		data = append(data, header...)
		data = append(data, payload...)
		var size [4]byte
		binary.BigEndian.PutUint32(size[:], uint32(11+len(payload)))
		data = append(data, size[:]...)
	}
	return data
}

func script() []byte { return []byte{2, 0, 1, 'x', 0, 0x3f, 0xf0, 0, 0, 0, 0, 0, 0} }

type testSource struct {
	*os.File
	closes      atomic.Int32
	reads       atomic.Int32
	armed       atomic.Bool
	blockOffset int64
	entered     chan struct{}
	release     chan struct{}
	enterOnce   sync.Once
	closeErr    error
}

func (s *testSource) ReadAt(p []byte, offset int64) (int, error) {
	s.reads.Add(1)
	if s.armed.Load() && (s.blockOffset < 0 || s.blockOffset == offset) {
		s.enterOnce.Do(func() { close(s.entered) })
		<-s.release
	}
	return s.File.ReadAt(p, offset)
}

func (s *testSource) Close() error {
	s.closes.Add(1)
	return errors.Join(s.File.Close(), s.closeErr)
}

func source(t *testing.T, data []byte) *testSource {
	t.Helper()
	path := filepath.Join(t.TempDir(), "detail.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return &testSource{File: file, blockOffset: -1, entered: make(chan struct{}), release: make(chan struct{})}
}

func startTest(t *testing.T, s *testSource, parse parseDetail, timeout time.Duration) *Document {
	t.Helper()
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	d := startWithParser(t.Context(), s, info, flv.Options{}, parse, timeout)
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
	return d
}

func scanDone(t *testing.T, d *Document) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	return d.Wait(ctx)
}

func receive(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not reach gate")
	}
}

func result(t *testing.T, d *Document, id uint64) DetailResult {
	t.Helper()
	deadline := time.After(3 * time.Second)
	for {
		value, err := d.GetDetail(id)
		if err != nil {
			t.Fatal(err)
		}
		if value.State != "pending" {
			return value
		}
		select {
		case <-deadline:
			t.Fatal("detail did not finish")
		default:
			runtime.Gosched()
		}
	}
}

func TestRetainedSourceIndependentDetailAndCopies(t *testing.T) {
	s := source(t, fixture(script()))
	d := startTest(t, s, detailview.Parse, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if s.closes.Load() != 0 {
		t.Fatal("scan closed the retained source")
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	first := result(t, d, 1)
	if first.Err != nil || first.State != "ready" || first.Detail == nil || first.Detail.Kind != "amf0ScriptData" || !first.Detail.Complete {
		t.Fatalf("detail: %+v", first)
	}
	first.Detail.Fields[0].Value = "mutated"
	if second, _ := d.GetDetail(1); second.Detail.Fields[0].Value == "mutated" {
		t.Fatal("query exposed retained DTO")
	}
	reads := s.reads.Load()
	if duplicate, err := d.StartDetail(1, 0); err != nil || duplicate.State != "ready" || s.reads.Load() != reads {
		t.Fatalf("idempotent start: %+v %v", duplicate, err)
	}
	if _, err := d.StartDetail(1, 1); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("same ID changed selection: %v", err)
	}
	d.Cancel() // a late scan cancellation must not cancel the document.
	if _, err := d.StartDetail(2, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 2); got.Err != nil {
		t.Fatal(got.Err)
	}
	if err := d.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(t.Context()); err != nil || s.closes.Load() != 1 {
		t.Fatalf("close count=%d err=%v", s.closes.Load(), err)
	}
	if _, err := d.StartDetail(3, 0); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed start: %v", err)
	}
}

func TestCanceledPrefixCanReadDetails(t *testing.T) {
	s := source(t, fixture(script(), script()))
	s.blockOffset = int64(13 + 15 + len(script()))
	s.armed.Store(true)
	d := startTest(t, s, detailview.Parse, DetailTimeout)
	receive(t, s.entered)
	if _, err := d.StartDetail(1, 0); !errors.Is(err, session.ErrNotReady) {
		t.Fatalf("live detail: %v", err)
	}
	d.Cancel()
	close(s.release)
	if err := scanDone(t, d); !errors.Is(err, context.Canceled) {
		t.Fatalf("scan error: %v", err)
	}
	page, err := d.Page(0, 10)
	if err != nil || page.Total != 1 {
		t.Fatalf("prefix: %+v %v", page, err)
	}
	if d.ctx.Err() != nil || s.closes.Load() != 0 {
		t.Fatal("CancelScan ended file lifetime")
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil || !got.Detail.Complete {
		t.Fatalf("prefix detail: %+v", got)
	}
	if _, err := d.StartDetail(2, 1); !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("uncommitted tag: %v", err)
	}
}

func TestStructuralDamageDoesNotHideCompletePrefix(t *testing.T) {
	data := fixture(script(), script())
	data = data[:len(data)-8] // complete first tag, incomplete second payload
	s := source(t, data)
	d := startTest(t, s, detailview.Parse, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if got := d.Snapshot(); got.Complete || !got.HasErrors {
		t.Fatalf("scan: %+v", got)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.Err != nil {
		t.Fatal(got.Err)
	}
	if _, err := d.StartDetail(2, 1); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 2); !errors.Is(got.Err, detailview.ErrTruncated) || got.Detail != nil {
		t.Fatalf("truncated tag: %+v", got)
	}
}

func TestSourceChangesBeforeAndDuringRead(t *testing.T) {
	for _, during := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "during"}[during], func(t *testing.T) {
			s := source(t, fixture(script()))
			var calls atomic.Int32
			mutate := func() {
				info, err := os.Stat(s.Name())
				if err != nil {
					t.Fatal(err)
				}
				if err := os.Chtimes(s.Name(), info.ModTime(), info.ModTime().Add(time.Second)); err != nil {
					t.Fatal(err)
				}
			}
			parse := func(ctx context.Context, r io.ReaderAt, tag flv.TagRecord) (*detailview.Detail, error) {
				calls.Add(1)
				parsed, err := detailview.Parse(ctx, r, tag)
				if during {
					mutate()
				}
				return parsed, err
			}
			d := startTest(t, s, parse, DetailTimeout)
			if err := scanDone(t, d); err != nil {
				t.Fatal(err)
			}
			if !during {
				mutate()
			}
			if _, err := d.StartDetail(1, 0); err != nil {
				t.Fatal(err)
			}
			if got := result(t, d, 1); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil {
				t.Fatalf("changed detail: %+v", got)
			}
			previousCalls := calls.Load()
			if _, err := d.StartDetail(2, 0); err != nil {
				t.Fatal(err)
			}
			if got := result(t, d, 2); !errors.Is(got.Err, ErrSourceChanged) || got.Detail != nil || calls.Load() != previousCalls {
				t.Fatalf("change was not sticky: %+v", got)
			}
		})
	}
}

func TestLatestSelectionDoesNotBypassBlockedRead(t *testing.T) {
	s := source(t, fixture(script(), script(), script()))
	var active, maximum, calls atomic.Int32
	parse := func(ctx context.Context, r io.ReaderAt, tag flv.TagRecord) (*detailview.Detail, error) {
		n := active.Add(1)
		defer active.Add(-1)
		calls.Add(1)
		if n > maximum.Load() {
			maximum.Store(n)
		}
		return detailview.Parse(ctx, r, tag)
	}
	d := startTest(t, s, parse, DetailTimeout)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	if _, err := d.StartDetail(2, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(3, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := d.GetDetail(1); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old query: %v", err)
	}
	if _, err := d.StartDetail(2, 0); !errors.Is(err, ErrStaleRequest) {
		t.Fatalf("old start: %v", err)
	}
	if _, err := d.GetDetail(4); !errors.Is(err, ErrUnknownRequest) {
		t.Fatalf("unknown query: %v", err)
	}
	if calls.Load() != 1 || maximum.Load() != 1 {
		t.Fatal("blocked read was bypassed")
	}
	close(s.release)
	got := result(t, d, 3)
	if got.Err != nil || got.TagID != 2 || calls.Load() != 2 || maximum.Load() != 1 {
		t.Fatalf("latest: %+v calls=%d max=%d", got, calls.Load(), maximum.Load())
	}
}

func TestDeadlineAndCloseWaitForBlockedDetail(t *testing.T) {
	s := source(t, fixture(script()))
	deadlineSeen := make(chan struct{})
	parse := func(ctx context.Context, r io.ReaderAt, tag flv.TagRecord) (*detailview.Detail, error) {
		go func() { <-ctx.Done(); close(deadlineSeen) }()
		return detailview.Parse(ctx, r, tag)
	}
	d := startTest(t, s, parse, 10*time.Millisecond)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	s.armed.Store(true)
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	receive(t, s.entered)
	receive(t, deadlineSeen)
	if got, _ := d.GetDetail(1); got.State != "pending" {
		t.Fatalf("deadline pretended underlying read ended: %+v", got)
	}
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	if err := d.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("close wait: %v", err)
	}
	if s.closes.Load() != 0 || d.Snapshot().State.Terminal() {
		t.Fatal("closed active source or advertised reusable slot")
	}
	if _, err := d.Page(0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("closed page: %v", err)
	}
	close(s.release)
	if err := d.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.closes.Load() != 1 || !d.Snapshot().State.Terminal() {
		t.Fatal("cleanup never completed")
	}
}

func TestDeadlineReturnsFailureAfterReaderReturns(t *testing.T) {
	s := source(t, fixture(script()))
	parse := func(ctx context.Context, _ io.ReaderAt, _ flv.TagRecord) (*detailview.Detail, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	d := startTest(t, s, parse, time.Millisecond)
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	if _, err := d.StartDetail(1, 0); err != nil {
		t.Fatal(err)
	}
	if got := result(t, d, 1); got.State != "failed" || !errors.Is(got.Err, context.DeadlineExceeded) {
		t.Fatalf("deadline: %+v", got)
	}
}

func TestParentCancellationAndCloseFailure(t *testing.T) {
	s := source(t, fixture(script()))
	s.closeErr = errors.New("synthetic close failure")
	info, err := s.Stat()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	d := start(ctx, s, info, flv.Options{})
	if err := scanDone(t, d); err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := d.Page(0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("parent-canceled page: %v", err)
	}
	if _, err := d.DiagnosticPage(0, 1); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("parent-canceled diagnostics: %v", err)
	}
	if err := d.Close(t.Context()); !errors.Is(err, s.closeErr) {
		t.Fatalf("close failure lost: %v", err)
	}
	if snapshot := d.Snapshot(); snapshot.State != session.StateFailed || !snapshot.Closed {
		t.Fatalf("cleanup outcome: %+v", snapshot)
	}
}
