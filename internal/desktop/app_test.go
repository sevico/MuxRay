package desktop

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func TestRealFileWorkflow(t *testing.T) {
	data := fixture(2050)
	data[12] = 1
	path := filepath.Join(t.TempDir(), "sample.flv")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	app := New(nil)
	cleanupApp(t, app)
	b := app.Bindings()
	opened := b.OpenFile(OpenRequest{Path: path})
	checkSession(t, opened, "opening", false)
	id := opened.Session.SessionID
	finished := finishReal(t, app, id)
	checkSession(t, finished, "finished", true)
	if finished.Session.Scan == nil || !finished.Session.Scan.Complete || finished.Session.Scan.HasErrors || finished.Session.Progress.TagCount != "2050" || finished.Session.Progress.DiagnosticCount != 1 {
		t.Fatalf("scan metadata: %+v", finished.Session)
	}
	page := b.GetTags(PageRequest{SessionID: id, Start: "1023", Count: 3})
	if page.Error != nil || page.Page.SessionID != id || len(page.Page.Tags) != 3 || page.Page.Bounds != (PageBounds{Start: "1023", Limit: 3, Total: "2050", Next: "1026"}) || page.Page.Tags[0].ID != "1023" || page.Page.Tags[0].Offset != decimal(13+1023*15) || !page.Page.Tags[0].PreviousTagSize.Present || page.Page.Tags[0].PreviousTagSize.Span == nil {
		t.Fatalf("tag page: %+v", page)
	}
	diagnostics := b.GetDiagnostics(PageRequest{SessionID: id, Start: "0", Count: 256})
	if diagnostics.Error != nil || len(diagnostics.Page.Diagnostics) != 1 || *diagnostics.Page.Diagnostics[0].Actual != "1" {
		t.Fatalf("diagnostics: %+v", diagnostics)
	}
	page.Page.Tags[0].ID = "changed"
	*diagnostics.Page.Diagnostics[0].Actual = "changed"
	if b.GetTags(PageRequest{id, "1023", 1}).Page.Tags[0].ID != "1023" || *b.GetDiagnostics(PageRequest{id, "0", 1}).Page.Diagnostics[0].Actual != "1" {
		t.Fatal("DTO mutation changed core storage")
	}
	empty := b.GetTags(PageRequest{id, "2050", 4096})
	if empty.Error != nil || empty.Page.Tags == nil || len(empty.Page.Tags) != 0 || empty.Page.Bounds.Next != "2050" {
		t.Fatalf("empty page: %+v", empty)
	}
	checkFault(t, b.OpenFile(OpenRequest{Path: path}).Error, CodeBusy)
	closed := b.CloseFile(id)
	if closed.Error != nil || !closed.Session.Closed {
		t.Fatalf("close: %+v", closed)
	}
	closeReal(t, app)
	checkFault(t, b.GetTags(PageRequest{id, "0", 1}).Error, CodeSessionClosed)
	checkFault(t, b.GetDiagnostics(PageRequest{id, "0", 1}).Error, CodeSessionClosed)
	checkFault(t, b.CancelScan(id).Error, CodeSessionClosed)
	if b.CloseFile(id).Error != nil {
		t.Fatal("close is not idempotent")
	}
	next := b.OpenFile(OpenRequest{Path: path})
	if next.Error != nil || next.Session.SessionID == id {
		t.Fatal("slot not reusable with a new identity")
	}
	newID := next.Session.SessionID
	for _, reply := range []SessionResponse{b.GetSession(id), b.CancelScan(id), b.CloseFile(id)} {
		checkFault(t, reply.Error, CodeSessionNotFound)
	}
	checkFault(t, b.GetTags(PageRequest{id, "0", 1}).Error, CodeSessionNotFound)
	finishReal(t, app, newID)
	if snapshot := b.GetSession(newID).Session; snapshot.Closed || snapshot.Failure != nil {
		t.Fatal("stale request changed new session")
	}
	b.CloseFile(newID)
	closeReal(t, app)
	if err := os.Remove(path); err != nil {
		t.Fatalf("real source still open: %v", err)
	}
}

func TestOpenFailuresAndMalformedSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.flv")
	if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		code string
	}{{filepath.Join(dir, "private-missing.flv"), CodeNotFound}, {dir, CodeOpenFailed}, {path, ""}} {
		app := New(nil)
		cleanupApp(t, app)
		opened := app.Bindings().OpenFile(OpenRequest{Path: tc.path})
		id := opened.Session.SessionID
		view := finishReal(t, app, id)
		if view.Error != nil || !view.Session.Terminal {
			t.Fatalf("outcome: %+v", view)
		}
		if tc.code != "" {
			checkFault(t, view.Session.Failure, tc.code)
			if view.Session.Progress != nil || view.Session.Scan != nil || view.Session.State != "failed" {
				t.Fatal("open failure disguised as scan")
			}
			checkFault(t, app.Bindings().GetTags(PageRequest{id, "0", 1}).Error, CodeOpenFailed)
		} else {
			if view.Session.State != "finished" || view.Session.Failure != nil || view.Session.Scan.Complete || !view.Session.Scan.HasErrors {
				t.Fatalf("structural error lost: %+v", view.Session)
			}
			page := app.Bindings().GetDiagnostics(PageRequest{id, "0", 1})
			if page.Error != nil || page.Page.Diagnostics[0].Code != flv.CodeHeaderTruncated {
				t.Fatal("malformed source has no diagnostics")
			}
		}
		encoded, _ := json.Marshal(view)
		if bytes.Contains(encoded, []byte("private-missing.flv")) || bytes.Contains(encoded, []byte(dir)) {
			t.Fatal("response leaked source path")
		}
	}
}

func TestOpeningAdmissionCancelAndLateClose(t *testing.T) {
	for _, action := range []string{"cancel", "close", "shutdown"} {
		t.Run(action, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			defer unblock()
			scan := newFakeScan(t, true)
			var calls atomic.Int32
			app := newApp(nil, func(ctx context.Context, _ string, _ flv.Options) (scanSession, error) {
				calls.Add(1)
				close(entered)
				<-release // model an open call which cannot be preempted
				if ctx.Err() == nil {
					t.Error("opening context was not canceled")
				}
				return scan, nil // even a late successful open must be owned/closed
			})
			defer func() {
				unblock()
				scan.finish(context.Canceled)
				_ = app.Shutdown(context.Background())
			}()
			b := app.Bindings()
			id := b.OpenFile(OpenRequest{Path: "sample.flv"}).Session.SessionID
			await(t, entered)
			checkFault(t, b.OpenFile(OpenRequest{Path: "second.flv"}).Error, CodeBusy)
			checkFault(t, b.GetTags(PageRequest{id, "0", 1}).Error, CodeNotReady)
			ctx, stop := expiredContext()
			defer stop()
			switch action {
			case "cancel":
				b.CancelScan(id)
			case "close":
				b.CloseFile(id)
			case "shutdown":
				if err := app.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("shutdown ignored blocked open: %v", err)
				}
			}
			view := b.GetSession(id)
			if view.Session.State != "canceling" || view.Session.Terminal || view.Session.Progress != nil || view.Session.Closed != (action != "cancel") {
				t.Fatalf("opening cancel: %+v", view.Session)
			}
			unblock()
			waitOpened(t, app)
			if action != "cancel" {
				if !scan.Snapshot().Closed {
					t.Fatal("late source escaped closed owner")
				}
			} else if scan.Snapshot().Closed {
				t.Fatal("cancel unexpectedly discarded partial result")
			}
			code := CodeBusy
			if action == "shutdown" {
				code = CodeShuttingDown
			}
			checkFault(t, b.OpenFile(OpenRequest{Path: "second.flv"}).Error, code)
			scan.finish(context.Canceled)
			view = b.GetSession(id)
			checkSession(t, view, "canceled", true)
			if calls.Load() != 1 {
				t.Fatal("opening work exceeded single-slot limit")
			}
			if action == "shutdown" {
				if err := app.Shutdown(ctx); !errors.Is(err, context.Canceled) {
					t.Fatalf("repeat shutdown lost final outcome: %v", err)
				}
			}
		})
	}
}

func TestBlockedScanShutdownAndPartialCancel(t *testing.T) {
	scan := newFakeScan(t, true)
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	defer func() { scan.finish(context.Canceled); _ = app.Shutdown(nil) }()
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "sample.flv"}).Session.SessionID
	waitOpened(t, app)
	checkFault(t, b.GetDiagnostics(PageRequest{id, "0", 1}).Error, CodeNotReady)
	view := b.CancelScan(id)
	checkSession(t, view, "canceling", false)
	scan.finish(context.Canceled)
	view = b.GetSession(id)
	checkSession(t, view, "canceled", true)
	if view.Session.Scan.Complete || !view.Session.Scan.HasErrors || view.Session.Closed {
		t.Fatal("canceled scan outcome")
	}
	if page := b.GetTags(PageRequest{id, "0", 1}); page.Error != nil || len(page.Page.Tags) != 1 {
		t.Fatalf("partial result unavailable: %+v", page)
	}
	// A new active fake makes shutdown wait on the scanner rather than opening.
	b.CloseFile(id)
	active := newFakeScan(t, true)
	app.open = func(context.Context, string, flv.Options) (scanSession, error) { return active, nil }
	defer func() { active.finish(context.Canceled); _ = app.Shutdown(nil) }()
	id = b.OpenFile(OpenRequest{Path: "other.flv"}).Session.SessionID
	waitOpened(t, app)
	ctx, cancel := expiredContext()
	defer cancel()
	if err := app.Shutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("blocked shutdown: %v", err)
	}
	if view := b.GetSession(id).Session; !view.Closed || view.Terminal {
		t.Fatal("timeout pretended cleanup was complete")
	}
	checkFault(t, b.OpenFile(OpenRequest{Path: "third.flv"}).Error, CodeShuttingDown)
	active.finish(errors.New("cleanup failed"))
	checkFault(t, b.GetSession(id).Session.Failure, CodeScanFailed)
	if err := app.Shutdown(nil); err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("cleanup failure hidden: %v", err)
	}
}

func TestConcurrentOpenAndStalePage(t *testing.T) {
	scan := newFakeScan(t, false)
	var calls atomic.Int32
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) {
		calls.Add(1)
		return scan, nil
	})
	cleanupApp(t, app)
	b := app.Bindings()
	var wg sync.WaitGroup
	var winner SessionResponse
	var wins atomic.Int32
	var winnerMu sync.Mutex
	for range 16 {
		wg.Go(func() {
			response := b.OpenFile(OpenRequest{Path: "sample.flv"})
			if response.Error == nil {
				wins.Add(1)
				winnerMu.Lock()
				winner = response
				winnerMu.Unlock()
			} else if response.Error.Code != CodeBusy {
				t.Errorf("concurrent open: %+v", response.Error)
			}
		})
	}
	wg.Wait()
	waitOpened(t, app)
	if wins.Load() != 1 || calls.Load() != 1 {
		t.Fatal("concurrent opening created multiple sessions")
	}
	id := winner.Session.SessionID
	scan.pageEntered, scan.pageRelease = make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(scan.pageRelease) }) }
	defer release()
	oldReply := make(chan TagPageResponse, 1)
	go func() { oldReply <- b.GetTags(PageRequest{id, "0", 1}) }()
	await(t, scan.pageEntered)
	b.CloseFile(id)
	newScan := newFakeScan(t, false)
	app.open = func(context.Context, string, flv.Options) (scanSession, error) { return newScan, nil }
	newID := b.OpenFile(OpenRequest{Path: "second.flv"}).Session.SessionID
	waitOpened(t, app)
	checkFault(t, b.CancelScan(id).Error, CodeSessionNotFound)
	release()
	select {
	case page := <-oldReply:
		if page.Error != nil || page.Page.SessionID != id || page.Page.SessionID == newID {
			t.Fatal("in-flight reply lost original identity")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("old page did not finish")
	}
	if b.GetSession(newID).Session.Closed {
		t.Fatal("old call closed new session")
	}
}

func TestConcurrentLifecycle(t *testing.T) {
	scan := newFakeScan(t, true)
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
	defer func() { scan.finish(nil); _ = app.Shutdown(nil) }()
	b := app.Bindings()
	id := b.OpenFile(OpenRequest{Path: "sample.flv"}).Session.SessionID
	waitOpened(t, app)
	ctx, cancel := expiredContext()
	defer cancel()
	var wg sync.WaitGroup
	for worker := range 24 {
		wg.Go(func() {
			for range 30 {
				switch worker % 6 {
				case 0:
					b.GetSession(id)
				case 1:
					b.CancelScan(id)
				case 2:
					b.CloseFile(id)
				case 3:
					b.GetTags(PageRequest{id, "0", 1})
				case 4:
					b.GetDiagnostics(PageRequest{id, "0", 1})
				case 5:
					_ = app.Shutdown(ctx)
				}
			}
		})
	}
	scan.finish(context.Canceled)
	wg.Wait()
	view := b.GetSession(id).Session
	if !view.Closed || !view.Terminal || !scan.Snapshot().Closed {
		t.Fatal("concurrent lifecycle lost cleanup")
	}
}

func TestInputValidationAndIdentityExhaustion(t *testing.T) {
	var calls atomic.Int32
	app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) {
		calls.Add(1)
		return nil, fs.ErrNotExist
	})
	cleanupApp(t, app)
	b := app.Bindings()
	for _, request := range []OpenRequest{{}, {Path: "\x00"}, {Path: string([]byte{255})}, {Path: strings.Repeat("x", MaxPathBytes+1)}, {Path: "x", MaxTags: uint32(flv.HardMaxTags + 1)}, {Path: "x", MaxDiagnostics: 1}, {Path: "x", MaxDiagnostics: flv.HardMaxDiagnostics + 1}} {
		checkFault(t, b.OpenFile(request).Error, CodeInvalidArgument)
	}
	if calls.Load() != 0 || app.current != nil {
		t.Fatal("invalid request started work")
	}
	for _, request := range []PageRequest{{"missing", "", 1}, {"missing", "+1", 1}, {"missing", "01", 1}, {"missing", "0", 0}, {"missing", "0", 4097}} {
		checkFault(t, b.GetTags(request).Error, CodeInvalidArgument)
		checkFault(t, b.GetDiagnostics(request).Error, CodeInvalidArgument)
	}
	for _, response := range []SessionResponse{b.GetSession(""), b.CancelScan("missing"), b.CloseFile("missing")} {
		checkFault(t, response.Error, CodeSessionNotFound)
	}
	checkFault(t, b.GetDiagnostics(PageRequest{"missing", "0", 1}).Error, CodeSessionNotFound)
	app.nextID = math.MaxUint64
	checkFault(t, b.OpenFile(OpenRequest{Path: "x"}).Error, CodeIDExhausted)
	if err := app.Shutdown(nil); err != nil {
		t.Fatal(err)
	}
	checkFault(t, b.OpenFile(OpenRequest{Path: "x"}).Error, CodeShuttingDown)
}

func TestTaskErrorClassificationAndOptions(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{{context.Canceled, CodeCanceled}, {context.DeadlineExceeded, CodeDeadline}, {fs.ErrPermission, CodePermission}, {fs.ErrNotExist, CodeNotFound}, {errors.New("private path"), CodeOpenFailed}} {
		app := newApp(nil, func(_ context.Context, path string, options flv.Options) (scanSession, error) {
			if path != "x" || options != (flv.Options{MaxTags: 2, MaxDiagnostics: 3}) {
				t.Error("scan options were changed")
			}
			return nil, tc.err
		})
		cleanupApp(t, app)
		id := app.Bindings().OpenFile(OpenRequest{Path: "x", MaxTags: 2, MaxDiagnostics: 3}).Session.SessionID
		waitOpened(t, app)
		checkFault(t, app.Bindings().GetSession(id).Session.Failure, tc.code)
	}
	ctx, cancel := context.WithCancel(context.Background())
	app := New(ctx)
	cancel()
	checkFault(t, app.Bindings().OpenFile(OpenRequest{Path: "x"}).Error, CodeShuttingDown)
	_ = app.Shutdown(nil)
	for _, err := range []error{context.DeadlineExceeded, errors.Join(context.Canceled, errors.New("close failed"))} {
		scan := newFakeScan(t, true)
		if errors.Is(err, context.DeadlineExceeded) {
			scan.finish(err)
		} else {
			scan.finish(errors.New("close failed"))
		}
		app := newApp(nil, func(context.Context, string, flv.Options) (scanSession, error) { return scan, nil })
		cleanupApp(t, app)
		id := app.Bindings().OpenFile(OpenRequest{Path: "x"}).Session.SessionID
		waitOpened(t, app)
		code := CodeScanFailed
		if errors.Is(err, context.DeadlineExceeded) {
			code = CodeDeadline
		}
		checkFault(t, app.Bindings().GetSession(id).Session.Failure, code)
	}
}

// This fake controls task termination independently of open/close requests.
// Its immutable result is produced by the real parser; no I/O timing is assumed.
type fakeScan struct {
	mu          sync.Mutex
	snapshot    session.Snapshot
	result      flv.ScanResult
	err         error
	done        chan struct{}
	pageEntered chan struct{}
	pageRelease chan struct{}
}

func newFakeScan(t *testing.T, active bool) *fakeScan {
	t.Helper()
	data := fixture(2)
	data[12] = 1
	result, err := flv.ScanContext(nil, bytes.NewReader(data), uint64(len(data)), flv.Options{})
	if err != nil {
		t.Fatal(err)
	}
	scan := &fakeScan{result: result, done: make(chan struct{}), snapshot: session.Snapshot{State: session.StateRunning, Progress: flv.ScanProgress{SourceSize: result.SourceSize, BytesScanned: result.BytesScanned, TagCount: result.Index().Len(), DiagnosticCount: uint32(len(result.Diagnostics))}}}
	if !active {
		scan.finish(nil)
	}
	return scan
}

func (s *fakeScan) Snapshot() session.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshot
}

func (s *fakeScan) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.snapshot.State.Terminal() {
		s.snapshot.State = session.StateCanceling
	}
}

func (s *fakeScan) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := waitOpen(ctx, s.done); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.err
}

func (s *fakeScan) Close(ctx context.Context) error {
	s.mu.Lock()
	s.snapshot.Closed = true
	s.mu.Unlock()
	s.Cancel()
	return s.Wait(ctx)
}

func (s *fakeScan) finish(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.State.Terminal() {
		return
	}
	s.err = err
	s.snapshot.State = session.StateFinished
	s.snapshot.Complete, s.snapshot.HasErrors = s.result.Complete, s.result.HasErrors()
	if err != nil {
		s.snapshot.State = session.StateFailed
		s.snapshot.Complete, s.snapshot.HasErrors = false, true
		if err == context.Canceled || err == context.DeadlineExceeded {
			s.snapshot.State = session.StateCanceled
		}
	}
	close(s.done)
}

func (s *fakeScan) Page(start uint64, count uint32) (flv.TagPage, error) {
	s.mu.Lock()
	if s.snapshot.Closed {
		s.mu.Unlock()
		return flv.TagPage{}, session.ErrClosed
	}
	if !s.snapshot.State.Terminal() {
		s.mu.Unlock()
		return flv.TagPage{}, session.ErrNotReady
	}
	index := s.result.Index()
	s.mu.Unlock()
	if s.pageEntered != nil {
		close(s.pageEntered)
		<-s.pageRelease
	}
	return index.Page(start, count)
}

func (s *fakeScan) DiagnosticPage(start uint64, count uint32) (session.DiagnosticPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.snapshot.Closed {
		return session.DiagnosticPage{}, session.ErrClosed
	}
	if !s.snapshot.State.Terminal() {
		return session.DiagnosticPage{}, session.ErrNotReady
	}
	total := uint64(len(s.result.Diagnostics))
	if start > total || count == 0 || count > flv.MaxPageSize {
		return session.DiagnosticPage{}, errors.New("bounds")
	}
	next := start + min(uint64(count), total-start)
	return session.DiagnosticPage{Start: start, Limit: count, Total: total, Next: next, Records: s.result.Diagnostics[start:next]}, nil
}

func waitOpened(t *testing.T, app *App) {
	t.Helper()
	app.mu.Lock()
	done := app.current.openDone
	app.mu.Unlock()
	await(t, done)
}

func finishReal(t *testing.T, app *App, id string) SessionResponse {
	t.Helper()
	waitOpened(t, app)
	app.mu.Lock()
	scan := app.current.scan
	app.mu.Unlock()
	if scan != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = scan.Wait(ctx)
	}
	return app.Bindings().GetSession(id)
}

func closeReal(t *testing.T, app *App) {
	t.Helper()
	waitOpened(t, app)
	app.mu.Lock()
	scan := app.current.scan
	app.mu.Unlock()
	if scan != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := scan.Close(ctx); err != nil && !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
}

func cleanupApp(t *testing.T, app *App) {
	t.Helper()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Shutdown(ctx)
		app.mu.Lock()
		defer app.mu.Unlock()
		if app.current != nil && !app.terminalLocked(app.current) {
			t.Error("application still owns running work")
		}
	})
}

func checkFault(t *testing.T, got *Fault, code string) {
	t.Helper()
	if got == nil || got.Code != code || got.Message == "" {
		t.Fatalf("fault=%+v want=%s", got, code)
	}
}

func checkSession(t *testing.T, got SessionResponse, state string, terminal bool) {
	t.Helper()
	if got.APIVersion != APIVersion || got.Error != nil || got.Session == nil || got.Session.State != state || got.Session.Terminal != terminal {
		t.Fatalf("response=%+v want=%s terminal=%v", got, state, terminal)
	}
}

func await(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not reach gate")
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

func TestBindingSurface(t *testing.T) {
	typeOf := reflect.TypeFor[*Bindings]()
	var methods []string
	for i := range typeOf.NumMethod() {
		methods = append(methods, typeOf.Method(i).Name)
	}
	want := []string{"CancelScan", "CloseFile", "GetDetail", "GetDiagnostics", "GetHex", "GetSession", "GetTags", "OpenFile", "StartDetail", "StartHex"}
	if !reflect.DeepEqual(methods, want) {
		t.Fatalf("unexpected binding exposure: %v", methods)
	}
}
