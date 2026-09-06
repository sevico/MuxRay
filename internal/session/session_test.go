package session

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
)

func TestOpenAndPage(t *testing.T) {
	data := fixture(2050)
	path := filepath.Join(t.TempDir(), "sample.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	s, err := Open(nil, path, flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(t.Context()) })
	if err := wait(t, s); err != nil {
		t.Fatal(err)
	}
	if snapshot := s.Snapshot(); snapshot.State != StateFinished || !snapshot.Complete || snapshot.HasErrors || snapshot.Closed || snapshot.Progress.TagCount != 2050 || snapshot.Progress.BytesScanned != uint64(len(data)) {
		t.Fatalf("finished: %+v", snapshot)
	}
	for _, bounds := range []struct {
		start uint64
		count uint32
	}{{0, 1}, {1023, 3}, {1024, 1025}, {2049, flv.MaxPageSize}, {2050, 1}} {
		page, err := s.Page(bounds.start, bounds.count)
		if err != nil || page.Start != bounds.start || page.Total != 2050 || page.Limit != bounds.count || page.Next != min(bounds.start+uint64(bounds.count), 2050) || page.Records == nil {
			t.Fatalf("page: %+v %v", page, err)
		}
		for i, record := range page.Records {
			id := bounds.start + uint64(i)
			if record.ID != id || record.Offset != 13+15*id {
				t.Fatalf("absolute IDs/offsets lost: %+v", record)
			}
		}
		if len(page.Records) > 0 {
			page.Records[0].ID = math.MaxUint64
		}
	}
	for _, bounds := range []struct {
		start uint64
		count uint32
	}{{0, 0}, {0, flv.MaxPageSize + 1}, {2051, 1}, {math.MaxUint64, 1}} {
		if _, err := s.Page(bounds.start, bounds.count); err == nil {
			t.Fatalf("accepted invalid page: %+v", bounds)
		}
	}
	result, err := s.Result()
	if err != nil || result.Tags != nil || result.Index().Len() != 2050 {
		t.Fatalf("result: %v", err)
	}
	first, _ := result.Index().Get(0)
	if first.ID != 0 {
		t.Fatal("page exposed backing storage")
	}
	// The scan no longer needs the source for paging. This also checks real-file
	// cleanup on Windows, where an open file may prevent removal.
	if err := os.Remove(path); err != nil {
		t.Fatalf("source still held after Wait: %v", err)
	}
	if err := s.Close(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Page(0, 1); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed page: %v", err)
	}
	if _, err := result.Index().Page(1023, 3); err != nil {
		t.Fatalf("returned index invalidated by Close: %v", err)
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{dir, filepath.Join(dir, "missing.flv")} {
		if s, err := Open(nil, path, flv.Options{}); err == nil || s != nil {
			t.Fatalf("Open(%q)=%v, %v", path, s, err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s, err := Open(ctx, filepath.Join(dir, "missing.flv"), flv.Options{}); !errors.Is(err, context.Canceled) || s != nil {
		t.Fatalf("pre-canceled Open: %v %v", s, err)
	}
}

func TestSessionTerminalOutcomes(t *testing.T) {
	readErr, closeErr := errors.New("injected read failure"), errors.New("injected close failure")
	warning := fixture(2)
	warning[12] = 1
	noisy := fixture(2)
	noisy[13] = 0xff
	badSignature := fixture(2)
	badSignature[0] = 'X'
	for _, tc := range []struct {
		name        string
		data        []byte
		options     flv.Options
		failAt      int64
		readErr     error
		closeErr    error
		state       State
		tags        uint64
		complete    bool
		hasErrors   bool
		diagnostics uint32
	}{
		{name: "empty", data: fixture(0), state: StateFinished, complete: true},
		{name: "valid", data: fixture(2), state: StateFinished, tags: 2, complete: true},
		{name: "warning", data: warning, state: StateFinished, tags: 2, complete: true, diagnostics: 1},
		{name: "short_header", data: []byte{1, 2}, state: StateFinished, hasErrors: true, diagnostics: 1},
		{name: "invalid_signature", data: badSignature, state: StateFinished, hasErrors: true, diagnostics: 1},
		{name: "short_trailer", data: fixture(2)[:40], state: StateFinished, tags: 2, hasErrors: true, diagnostics: 1},
		{name: "tag_limit", data: fixture(2), options: flv.Options{MaxTags: 1}, state: StateFinished, tags: 1, hasErrors: true, diagnostics: 1},
		{name: "diagnostic_limit", data: noisy, options: flv.Options{MaxDiagnostics: 2}, state: StateFinished, tags: 2, complete: true, hasErrors: true, diagnostics: 2},
		{name: "invalid_options", data: fixture(2), options: flv.Options{MaxDiagnostics: 1}, state: StateFailed, hasErrors: true},
		{name: "read_failure", data: fixture(2), failAt: 28, readErr: readErr, state: StateFailed, tags: 1, hasErrors: true},
		{name: "close_failure", data: fixture(2), closeErr: closeErr, state: StateFailed, tags: 2, complete: true},
		{name: "read_and_close_failure", data: fixture(2), failAt: 28, readErr: readErr, closeErr: closeErr, state: StateFailed, tags: 1, hasErrors: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := newSource(tc.data)
			source.failAt, source.readErr, source.closeErr = tc.failAt, tc.readErr, tc.closeErr
			s := startTest(t, nil, source, uint64(len(tc.data)), tc.options)
			err := wait(t, s)
			if (err != nil) != (tc.state == StateFailed) || tc.readErr != nil && !errors.Is(err, tc.readErr) || tc.closeErr != nil && !errors.Is(err, tc.closeErr) {
				t.Fatalf("task error: %v", err)
			}
			snapshot := s.Snapshot()
			if snapshot.State != tc.state || !snapshot.State.Terminal() || snapshot.Complete != tc.complete || snapshot.HasErrors != tc.hasErrors || snapshot.Progress.TagCount != tc.tags || snapshot.Progress.DiagnosticCount != tc.diagnostics {
				t.Fatalf("snapshot: %+v", snapshot)
			}
			result, accessErr := s.Result()
			if accessErr != nil || result.Tags != nil || result.Index().Len() != tc.tags || result.BytesScanned != snapshot.Progress.BytesScanned || uint32(len(result.Diagnostics)) != snapshot.Progress.DiagnosticCount {
				t.Fatalf("final result: %+v %v", result, accessErr)
			}
			page, accessErr := s.Page(0, flv.MaxPageSize)
			if accessErr != nil || uint64(len(page.Records)) != tc.tags {
				t.Fatalf("partial page: %+v %v", page, accessErr)
			}
			if source.closes.Load() != 1 || source.closeDuringRead.Load() {
				t.Fatal("source not closed exactly once after reading")
			}
			if tc.name == "invalid_options" && source.reads.Load() != 0 {
				t.Fatal("invalid options performed reads")
			}
			s.Cancel()
			if s.Snapshot() != snapshot {
				t.Fatal("late cancel changed terminal result")
			}
			_ = s.Close(nil)
			_ = s.Close(nil)
			if !s.Snapshot().Closed || s.result.Index().Len() != 0 || s.result.Diagnostics != nil || source.closes.Load() != 1 {
				t.Fatal("Close did not release owned result or closed source twice")
			}
		})
	}
}

func TestWaitTimeoutDoesNotCancelScan(t *testing.T) {
	source := newSource(fixture(2050))
	source.gateRead(13 + 1024*15)
	s := startTest(t, nil, source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	want := Snapshot{State: StateRunning, Progress: flv.ScanProgress{SourceSize: uint64(source.reader.Len()), BytesScanned: 13 + 1024*15, TagCount: 1024}}
	if got := s.Snapshot(); got != want {
		t.Fatalf("live snapshot: %+v want %+v", got, want)
	}
	copy := s.Snapshot()
	copy.Progress.TagCount = 0
	copy.Closed = true
	if s.Snapshot() != want {
		t.Fatal("snapshot exposed mutable storage")
	}
	if _, err := s.Page(0, 1); !errors.Is(err, ErrNotReady) {
		t.Fatalf("active page: %v", err)
	}
	if _, err := s.Result(); !errors.Is(err, ErrNotReady) {
		t.Fatalf("active result: %v", err)
	}
	ctx, cancel := expiredContext()
	defer cancel()
	if err := s.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) || s.Snapshot() != want || source.closes.Load() != 0 {
		t.Fatalf("wait deadline affected task: %v %+v", err, s.Snapshot())
	}
	source.releaseRead()
	if err := wait(t, s); err != nil {
		t.Fatal(err)
	}
	if err := s.Wait(ctx); err != nil {
		t.Fatalf("finished outcome lost to wait deadline: %v", err)
	}
}

func TestCancellationAndCloseWhileReadBlocked(t *testing.T) {
	for _, action := range []string{"cancel", "parent", "close"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := newSource(fixture(2))
			source.gateRead(39) // second tag trailer; first tag alone is committed
			s := startTest(t, ctx, source, uint64(source.reader.Len()), flv.Options{})
			await(t, source.readEntered)
			waitCtx, stop := expiredContext()
			defer stop()
			switch action {
			case "cancel":
				s.Cancel()
				s.Cancel()
			case "parent":
				cancel()
			case "close":
				if err := s.Close(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("close wait deadline: %v", err)
				}
			}
			if snapshot := s.Snapshot(); snapshot.State != StateCanceling || snapshot.Closed != (action == "close") || source.closes.Load() != 0 {
				t.Fatalf("blocked cancellation: %+v closes=%d", snapshot, source.closes.Load())
			}
			accessErr := ErrNotReady
			if action == "close" {
				accessErr = ErrClosed
			}
			if _, err := s.Result(); !errors.Is(err, accessErr) {
				t.Fatalf("active access: %v", err)
			}
			if err := s.Wait(waitCtx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("blocked worker falsely finished: %v", err)
			}
			source.releaseRead()
			if err := wait(t, s); !errors.Is(err, context.Canceled) {
				t.Fatalf("cancellation: %v", err)
			}
			snapshot := s.Snapshot()
			if snapshot.State != StateCanceled || snapshot.Complete || !snapshot.HasErrors || snapshot.Progress.TagCount != 1 || snapshot.Progress.BytesScanned != 39 || snapshot.Progress.DiagnosticCount != 0 || source.closes.Load() != 1 || source.closeDuringRead.Load() {
				t.Fatalf("canceled prefix/cleanup: %+v", snapshot)
			}
			if action != "close" {
				page, err := s.Page(0, 2)
				if err != nil || len(page.Records) != 1 || page.Records[0].ID != 0 {
					t.Fatalf("canceled page: %+v %v", page, err)
				}
			} else if s.result.Index().Len() != 0 {
				t.Fatal("closed worker published an owned index")
			}
		})
	}
}

func TestPreCanceledWorkerAndCloseFailure(t *testing.T) {
	deadline, stop := expiredContext()
	defer stop()
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	closeFailure := errors.New("cannot close")
	for _, ctx := range []context.Context{deadline, canceled} {
		for _, closeErr := range []error{nil, closeFailure} {
			source := newSource(fixture(2))
			source.closeErr = closeErr
			s := startTest(t, ctx, source, uint64(source.reader.Len()), flv.Options{})
			err := wait(t, s)
			state := StateCanceled
			if closeErr != nil {
				state = StateFailed
			}
			if !errors.Is(err, ctx.Err()) || closeErr != nil && !errors.Is(err, closeErr) || s.Snapshot().State != state || source.reads.Load() != 0 || source.closes.Load() != 1 {
				t.Fatalf("pre-canceled worker: %+v %v", s.Snapshot(), err)
			}
		}
	}
}

func TestWaitIncludesSourceCloseAndCompletionWinsLateCancel(t *testing.T) {
	source := newSource(fixture(1))
	source.gateClose()
	s := startTest(t, nil, source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.closeEntered)
	if snapshot := s.Snapshot(); snapshot.State.Terminal() || snapshot.Progress.TagCount != 1 || snapshot.Progress.BytesScanned != uint64(source.reader.Len()) {
		t.Fatalf("published before close finished: %+v", snapshot)
	}
	s.Cancel() // the scanner already returned nil; this cannot undo it
	ctx, cancel := expiredContext()
	defer cancel()
	if err := s.Wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Wait ignored source close: %v", err)
	}
	if _, err := s.Page(0, 1); !errors.Is(err, ErrNotReady) {
		t.Fatalf("Page ignored source close: %v", err)
	}
	source.releaseClose()
	if err := wait(t, s); err != nil || s.Snapshot().State != StateFinished || !s.Snapshot().Complete {
		t.Fatalf("late cancel rewrote scan outcome: %+v %v", s.Snapshot(), err)
	}
}

func TestResultDiagnosticOwnership(t *testing.T) {
	data := fixture(2)
	data[12] = 1 // Expected and Actual pointers in a warning
	source := newSource(data)
	s := startTest(t, nil, source, uint64(len(data)), flv.Options{})
	if err := wait(t, s); err != nil {
		t.Fatal(err)
	}
	want, _ := s.Result()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				got, err := s.Result()
				if err != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("result ownership: %v", err)
					return
				}
				got.Diagnostics[0].Code = "modified"
				*got.Diagnostics[0].Expected = 100
				*got.Diagnostics[0].Actual = 100
				got.Header.Version = 255
				page, err := s.Page(0, 1)
				if err != nil || page.Records[0].ID != 0 {
					t.Errorf("page ownership: %v", err)
					return
				}
				page.Records[0].ID = 100
			}
		})
	}
	wg.Wait()
	if err := s.Close(nil); err != nil {
		t.Fatal(err)
	}
	if *want.Diagnostics[0].Expected != 0 || *want.Diagnostics[0].Actual != 1 || want.Index().Len() != 2 || s.result.Diagnostics != nil || s.result.Index().Len() != 0 {
		t.Fatal("caller-owned result invalidated or retained by closed session")
	}
	if _, err := s.Result(); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed result: %v", err)
	}
}

func TestConcurrentLifecycle(t *testing.T) {
	source := newSource(fixture(2050))
	source.gateRead(13 + 1024*15)
	s := startTest(t, nil, source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	ctx, cancel := expiredContext()
	defer cancel()
	var wg sync.WaitGroup
	for worker := range 24 {
		wg.Go(func() {
			for range 20 {
				switch worker % 6 {
				case 0:
					s.Cancel()
				case 1:
					_ = s.Close(ctx)
				case 2:
					_ = s.Wait(ctx)
				case 3:
					_, err := s.Page(0, 1)
					if err != nil && !errors.Is(err, ErrNotReady) && !errors.Is(err, ErrClosed) {
						t.Errorf("concurrent page: %v", err)
					}
				case 4:
					_, err := s.Result()
					if err != nil && !errors.Is(err, ErrNotReady) && !errors.Is(err, ErrClosed) {
						t.Errorf("concurrent result: %v", err)
					}
				case 5:
					p := s.Snapshot().Progress
					if p.TagCount < 1024 || p.TagCount > 2050 || p.BytesScanned > p.SourceSize {
						t.Errorf("invalid concurrent progress: %+v", p)
					}
				}
			}
		})
	}
	source.releaseRead()
	wg.Wait()
	_ = wait(t, s) // completion or cancellation can win this deliberate race
	if snapshot := s.Snapshot(); !snapshot.State.Terminal() || !snapshot.Closed || source.closes.Load() != 1 || source.closeDuringRead.Load() || s.result.Index().Len() != 0 {
		t.Fatalf("concurrent cleanup: %+v", snapshot)
	}
}

func TestMultipleWaiters(t *testing.T) {
	source := newSource(fixture(2))
	source.gateRead(28)
	s := startTest(t, nil, source, uint64(source.reader.Len()), flv.Options{})
	await(t, source.readEntered)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := wait(t, s); !errors.Is(err, context.Canceled) || source.closes.Load() != 1 {
				t.Errorf("waiter: %v closes=%d", err, source.closes.Load())
			}
		})
	}
	s.Cancel()
	source.releaseRead()
	wg.Wait()
}

// The source gates I/O with channels so cancellation tests do not depend on
// scheduling sleeps or a fast/slow filesystem. Only the scan worker reads it.
type controlledSource struct {
	reader          *bytes.Reader
	blockAt         int64
	readEntered     chan struct{}
	readRelease     chan struct{}
	readOnce        sync.Once
	closeEntered    chan struct{}
	closeRelease    chan struct{}
	closeOnce       sync.Once
	failAt          int64
	readErr         error
	closeErr        error
	reads           atomic.Int32
	closes          atomic.Int32
	activeReads     atomic.Int32
	closeDuringRead atomic.Bool
}

func newSource(data []byte) *controlledSource {
	return &controlledSource{reader: bytes.NewReader(data), blockAt: -1, failAt: -1}
}

func (r *controlledSource) gateRead(offset int64) {
	r.blockAt = offset
	r.readEntered, r.readRelease = make(chan struct{}), make(chan struct{})
}

func (r *controlledSource) gateClose() {
	r.closeEntered, r.closeRelease = make(chan struct{}), make(chan struct{})
}

func (r *controlledSource) releaseRead() {
	if r.readRelease != nil {
		r.readOnce.Do(func() { close(r.readRelease) })
	}
}

func (r *controlledSource) releaseClose() {
	if r.closeRelease != nil {
		r.closeOnce.Do(func() { close(r.closeRelease) })
	}
}

func (r *controlledSource) ReadAt(p []byte, offset int64) (int, error) {
	r.reads.Add(1)
	r.activeReads.Add(1)
	defer r.activeReads.Add(-1)
	if offset == r.blockAt {
		close(r.readEntered)
		<-r.readRelease
	}
	if offset == r.failAt && r.readErr != nil {
		return 0, r.readErr
	}
	return r.reader.ReadAt(p, offset)
}

func (r *controlledSource) Close() error {
	r.closes.Add(1)
	if r.activeReads.Load() != 0 {
		r.closeDuringRead.Store(true)
	}
	if r.closeEntered != nil {
		close(r.closeEntered)
		<-r.closeRelease
	}
	return r.closeErr
}

func startTest(t *testing.T, ctx context.Context, source *controlledSource, size uint64, options flv.Options) *Session {
	t.Helper()
	s := start(ctx, source, size, options)
	t.Cleanup(func() {
		source.releaseRead()
		source.releaseClose()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = s.Close(ctx)
		if !s.Snapshot().State.Terminal() || source.closes.Load() != 1 {
			t.Error("test worker did not release its source")
		}
	})
	return s
}

func wait(t *testing.T, s *Session) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.Wait(ctx)
}

func await(t *testing.T, entered <-chan struct{}) {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach I/O gate")
	}
}

func expiredContext() (context.Context, context.CancelFunc) {
	return context.WithDeadline(context.Background(), time.Unix(1, 0))
}

func fixture(tags int) []byte {
	data := make([]byte, 13+15*tags)
	copy(data, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9})
	for i := range tags {
		offset := 13 + 15*i
		data[offset] = 18
		binary.BigEndian.PutUint32(data[offset+11:offset+15], 11)
	}
	return data
}

var _ io.ReaderAt = (*controlledSource)(nil)
