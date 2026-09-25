// Package desktop adapts owned scans to a bounded, JSON-friendly desktop API.
// It contains no GUI runtime dependency; only Bindings is a transport surface.
package desktop

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

const MaxPathBytes = 32768

const (
	CodeInvalidArgument = "invalid_argument"
	CodeBusy            = "busy"
	CodeShuttingDown    = "shutting_down"
	CodeSessionNotFound = "session_not_found"
	CodeSessionClosed   = "session_closed"
	CodeNotReady        = "not_ready"
	CodeIDExhausted     = "id_exhausted"
	CodeOpenFailed      = "open_failed"
	CodeNotFound        = "source_not_found"
	CodePermission      = "source_permission_denied"
	CodeCanceled        = "canceled"
	CodeDeadline        = "deadline_exceeded"
	CodeScanFailed      = "scan_failed"
)

// App owns a single slot and its lifetime. Construct it with New; do not copy it.
// Bind only Bindings(), not App or the lower-level session object.
type App struct {
	mu       sync.Mutex
	ctx      context.Context
	cancel   context.CancelFunc
	pollCtx  context.Context
	closed   bool
	nextID   uint64
	current  *slot
	bindings *Bindings
	open     opener
}

type scanSession interface {
	Snapshot() session.Snapshot
	Cancel()
	Wait(context.Context) error
	Close(context.Context) error
	Page(uint64, uint32) (flv.TagPage, error)
	FilteredPage(uint64, uint32, flv.TagFilter) (flv.FilteredTagPage, error)
	DiagnosticPage(uint64, uint32) (session.DiagnosticPage, error)
}

type opener func(context.Context, string, flv.Options) (scanSession, error)

type slot struct {
	id       string
	ctx      context.Context
	cancel   context.CancelFunc
	opening  bool
	openDone chan struct{}
	openErr  error
	closed   bool
	scan     scanSession
}

// New uses ctx as the application lifetime; nil means Background. Canceling
// that lifetime stops work but the host must still call Shutdown for cleanup.
func New(ctx context.Context) *App {
	return newApp(ctx, func(ctx context.Context, path string, options flv.Options) (scanSession, error) {
		scan, err := document.Open(ctx, path, options)
		if err != nil {
			return nil, err // do not publish a typed-nil session interface
		}
		return scan, nil
	})
}

func newApp(ctx context.Context, open opener) *App {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	pollCtx, stop := context.WithCancel(context.Background())
	stop()
	app := &App{ctx: ctx, cancel: cancel, pollCtx: pollCtx, open: open}
	app.bindings = &Bindings{app: app}
	return app
}

// Bindings returns a stable object exposing only DTO-based application calls.
func (a *App) Bindings() *Bindings { return a.bindings }

// Bindings is the desktop transport surface. Methods are concurrency-safe and
// never return a context, Go error, session pointer, full index, or payload.
type Bindings struct{ app *App }

// OpenFile reserves the only slot before starting asynchronous file opening.
// A closed slot is reusable only after all its opening/scanning work has ended.
func (b *Bindings) OpenFile(request OpenRequest) SessionResponse {
	if !validOpen(request) {
		return failedSession(CodeInvalidArgument)
	}
	a := b.app
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.ctx.Err() != nil {
		return failedSession(CodeShuttingDown)
	}
	if a.current != nil {
		a.refreshClosedLocked(a.current)
	}
	if a.current != nil && (!a.current.closed || !a.terminalLocked(a.current)) {
		return failedSession(CodeBusy)
	}
	if a.nextID == math.MaxUint64 {
		return failedSession(CodeIDExhausted)
	}
	a.nextID++
	ctx, cancel := context.WithCancel(a.ctx)
	e := &slot{id: "scan-" + decimal(a.nextID), ctx: ctx, cancel: cancel, opening: true, openDone: make(chan struct{})}
	a.current = e
	go a.openSlot(e, request)
	return a.responseLocked(e)
}

func validOpen(request OpenRequest) bool {
	return request.Path != "" && len(request.Path) <= MaxPathBytes && utf8.ValidString(request.Path) &&
		!strings.ContainsRune(request.Path, 0) && uint64(request.MaxTags) <= flv.HardMaxTags &&
		(request.MaxDiagnostics == 0 || request.MaxDiagnostics >= flv.MinDiagnostics && request.MaxDiagnostics <= flv.HardMaxDiagnostics)
}

func (a *App) openSlot(e *slot, request OpenRequest) {
	scan, err := a.open(e.ctx, request.Path, flv.Options{MaxTags: uint64(request.MaxTags), MaxDiagnostics: request.MaxDiagnostics})
	a.mu.Lock()
	defer a.mu.Unlock()
	e.scan, e.openErr = scan, err
	if err != nil {
		e.cancel()
	}
	a.refreshClosedLocked(e)
	if e.closed && scan != nil {
		// Close uses an already canceled wait context: ownership is released by
		// the session when its worker ends, never by a per-request goroutine.
		_ = scan.Close(a.pollCtx)
	}
	e.opening = false
	close(e.openDone)
}

func (b *Bindings) GetSession(id string) SessionResponse {
	a := b.app
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.findLocked(id)
	if e == nil {
		return failedSession(CodeSessionNotFound)
	}
	return a.responseLocked(e)
}

func (b *Bindings) CancelScan(id string) SessionResponse {
	a := b.app
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.findLocked(id)
	if e == nil {
		return failedSession(CodeSessionNotFound)
	}
	if e.closed {
		return failedSession(CodeSessionClosed)
	}
	// Opening has no scan handle yet. Once admitted, cancellation belongs to
	// the scan alone; the document source must survive for partial details.
	if e.opening {
		e.cancel()
	}
	if e.scan != nil {
		e.scan.Cancel()
	}
	return a.responseLocked(e)
}

func (b *Bindings) CloseFile(id string) SessionResponse {
	a := b.app
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.findLocked(id)
	if e == nil {
		return failedSession(CodeSessionNotFound)
	}
	a.closeLocked(e)
	return a.responseLocked(e)
}

func (a *App) closeLocked(e *slot) {
	e.closed = true
	e.cancel()
	if e.scan != nil {
		_ = e.scan.Close(a.pollCtx)
	}
}

func (a *App) findLocked(id string) *slot {
	if a.current == nil || id != a.current.id {
		return nil
	}
	a.refreshClosedLocked(a.current)
	return a.current
}

func (a *App) refreshClosedLocked(e *slot) {
	// Cancellation can finish while an opener is returning its document. Its
	// own logical closure must not be lost merely because publication was late.
	if e.scan != nil && e.scan.Snapshot().Closed {
		e.closed = true
	}
}

func (a *App) terminalLocked(e *slot) bool {
	return !e.opening && (e.scan == nil || e.scan.Snapshot().State.Terminal())
}

func (a *App) responseLocked(e *slot) SessionResponse {
	a.refreshClosedLocked(e)
	view := &SessionSnapshot{SessionID: e.id, State: "opening", Closed: e.closed}
	if e.opening {
		if e.ctx.Err() != nil {
			view.State = string(session.StateCanceling)
		}
	} else if e.scan == nil {
		view.Terminal = true
		view.State = string(session.StateFailed)
		code := openFailure(e.openErr)
		if code == CodeCanceled || code == CodeDeadline {
			view.State = string(session.StateCanceled)
		}
		view.Failure = fault(code)
	} else {
		snapshot := e.scan.Snapshot()
		view.State, view.Terminal = string(snapshot.State), snapshot.State.Terminal()
		p := snapshot.Progress
		view.Progress = &Progress{SourceSize: decimal(p.SourceSize), BytesScanned: decimal(p.BytesScanned), TagCount: decimal(p.TagCount), DiagnosticCount: p.DiagnosticCount}
		if view.Terminal {
			view.Scan = &ScanOutcome{Complete: snapshot.Complete, HasErrors: snapshot.HasErrors}
			switch snapshot.State {
			case session.StateFailed:
				view.Failure = fault(CodeScanFailed) // cleanup failure must not look like cancellation
			case session.StateCanceled:
				code := CodeCanceled
				if errors.Is(e.scan.Wait(a.pollCtx), context.DeadlineExceeded) {
					code = CodeDeadline
				}
				view.Failure = fault(code)
			}
		}
	}
	return SessionResponse{APIVersion: APIVersion, Session: view}
}

// Shutdown permanently disables opens, closes the slot and waits for opening
// plus scan cleanup. A wait timeout leaves ownership intact. Nil waits without
// a deadline. Repeated calls can wait again; cancellation errors are expected
// when this shutdown cancels work. This method is deliberately not on Bindings.
func (a *App) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	a.closed = true
	a.cancel()
	e := a.current
	if e != nil {
		a.closeLocked(e)
	}
	a.mu.Unlock()
	if e == nil {
		return nil
	}
	if err := waitOpen(ctx, e.openDone); err != nil {
		return err
	}
	a.mu.Lock()
	scan, openErr := e.scan, e.openErr
	a.mu.Unlock()
	if scan != nil {
		return scan.Close(ctx)
	}
	return openErr
}

func waitOpen(ctx context.Context, done <-chan struct{}) error {
	select {
	case <-done:
		return nil
	default:
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		select {
		case <-done:
			return nil
		default:
			return ctx.Err()
		}
	}
}

func parseStart(value string) (uint64, bool) {
	if value == "" || len(value) > 20 || len(value) > 1 && value[0] == '0' {
		return 0, false
	}
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return 0, false
		}
	}
	start, err := strconv.ParseUint(value, 10, 64)
	return start, err == nil
}

func openFailure(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return CodeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return CodeDeadline
	case errors.Is(err, fs.ErrNotExist):
		return CodeNotFound
	case errors.Is(err, fs.ErrPermission):
		return CodePermission
	default:
		return CodeOpenFailed
	}
}

func failedSession(code string) SessionResponse {
	return SessionResponse{APIVersion: APIVersion, Error: fault(code)}
}

func fault(code string) *Fault {
	message := "Invalid request."
	switch code {
	case CodeBusy:
		message = "Close the current file and wait for cleanup before opening another."
	case CodeShuttingDown:
		message = "The application is shutting down."
	case CodeSessionNotFound:
		message = "The session is no longer current."
	case CodeSessionClosed:
		message = "The session is closed."
	case CodeNotReady:
		message = "The scan has not finished."
	case CodeIDExhausted:
		message = "The application cannot allocate another session ID."
	case CodeOpenFailed:
		message = "Could not open a regular source file."
	case CodeNotFound:
		message = "The source file does not exist."
	case CodePermission:
		message = "Permission to open the source file was denied."
	case CodeCanceled:
		message = "The task was canceled."
	case CodeDeadline:
		message = "The task deadline was exceeded."
	case CodeScanFailed:
		message = "Scanning or source cleanup failed."
	}
	return &Fault{Code: code, Message: message}
}
