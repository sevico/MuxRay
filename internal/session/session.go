// Package session manages cancellable background container scans. Open owns a
// local file; Start borrows its source. Index queries require a finished worker
// and, for Open, a completed source close attempt.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"sync"

	"github.com/sevico/muxray/internal/flv"
)

var (
	ErrNotReady    = errors.New("session: scan has not finished")
	ErrClosed      = errors.New("session: session is closed")
	ErrTagNotFound = errors.New("session: tag was not indexed")
)

// State describes task execution, not FLV validity. Finished can have structural
// errors; check the final snapshot's Complete/HasErrors or Result diagnostics.
type State string

const (
	StateRunning   State = "running"
	StateCanceling State = "canceling"
	StateFinished  State = "finished"
	StateCanceled  State = "canceled"
	StateFailed    State = "failed"
)

// Terminal reports whether scanning and any owned-source close have ended.
func (s State) Terminal() bool {
	return s == StateFinished || s == StateCanceled || s == StateFailed
}

// Snapshot is a constant-size independent value. Complete and HasErrors mirror
// ScanResult only in terminal states. Closed means Close was requested and new
// result/page access is disabled; it does not mean a blocked worker has exited.
type Snapshot struct {
	State     State
	Progress  flv.ScanProgress
	Complete  bool
	HasErrors bool
	Closed    bool
}

// Session has one worker and one bounded result. Use Open to construct it; the
// zero value is not usable. Its methods are safe for concurrent callers. Do not
// copy a Session. Call Close when its retained result is no longer needed.
type Session struct {
	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	done     chan struct{}
	snapshot Snapshot
	result   flv.ScanResult
	err      error
}

// Open opens and stats a regular file synchronously, then starts its scan. The
// session owns the file and closes it when the worker ends, including failure
// and cancellation. Option validation is part of the worker; consult Wait.
// The parent context cancels the scan; nil uses context.Background().
func Open(ctx context.Context, path string, options flv.Options) (*Session, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("session: open source: %w", err)
	}
	info, err := source.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() < 0) {
		err = errors.New("source must be a regular file with a nonnegative size")
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("session: stat source: %w", err), closeSource(source))
	}
	return start(ctx, source, uint64(info.Size()), options), nil
}

type ownedSource interface {
	io.ReaderAt
	io.Closer
}

// Start borrows source for a background scan. The caller retains ownership and
// must keep it readable until Wait finishes, then close it itself. Unlike Open,
// this entry point never closes the supplied source; Close still waits for the
// scanner to stop before returning. It permits a document owner to retain one
// file for later, independently canceled detail reads. A nil source is reported
// as a scan failure by Wait, including when size is zero.
func Start(ctx context.Context, source io.ReaderAt, size uint64, options flv.Options) *Session {
	return StartWithTagObserver(ctx, source, size, options, nil)
}

// StartWithTagObserver borrows source like Start and synchronously observes each
// committed tag on the scan worker, using the worker's cancelable context. The
// observer may perform bounded reads from source until Wait finishes. Observer
// errors retain the committed prefix; Wait and Close include observer completion.
// Neither this entry point nor Close closes the borrowed source.
func StartWithTagObserver(ctx context.Context, source io.ReaderAt, size uint64, options flv.Options, observe flv.TagObserver) *Session {
	return startWithTagObserver(ctx, borrowedSource{ReaderAt: source}, size, options, observe)
}

type borrowedSource struct{ io.ReaderAt }

func (borrowedSource) Close() error { return nil }

// start transfers exclusive ownership of an already opened source to the
// worker. Keeping this seam private permits deterministic I/O lifecycle tests
// without making caller-supplied source ownership another public API.
func start(ctx context.Context, source ownedSource, size uint64, options flv.Options) *Session {
	return startWithTagObserver(ctx, source, size, options, nil)
}

func startWithTagObserver(ctx context.Context, source ownedSource, size uint64, options flv.Options, observe flv.TagObserver) *Session {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	s := &Session{
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
		snapshot: Snapshot{State: StateRunning, Progress: flv.ScanProgress{SourceSize: size}},
	}
	go s.run(source, size, options, observe)
	return s
}

func (s *Session) run(source ownedSource, size uint64, options flv.Options, observe flv.TagObserver) {
	reader := io.ReaderAt(source)
	if borrowed, ok := source.(borrowedSource); ok {
		reader = borrowed.ReaderAt // preserve the scanner's nil-source validation
	}
	result, scanErr := flv.ScanContextWithObserver(s.ctx, reader, size, options, func(progress flv.ScanProgress) {
		s.mu.Lock()
		s.snapshot.Progress = progress
		s.mu.Unlock()
	}, observe)
	closeErr := closeSource(source)
	// Classify the scanner's returned outcome, not a late cancellation while
	// closing the source. Close failures take precedence but retain both errors.
	state := StateFinished
	switch {
	case closeErr != nil:
		state = StateFailed
	case errors.Is(scanErr, context.Canceled), errors.Is(scanErr, context.DeadlineExceeded):
		state = StateCanceled
	case scanErr != nil:
		state = StateFailed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = errors.Join(scanErr, closeErr)
	s.snapshot.State = state
	s.snapshot.Complete = result.Complete
	s.snapshot.HasErrors = result.HasErrors()
	if !s.snapshot.Closed {
		s.result = result
	}
	s.cancel() // release the parent attachment without a spurious canceling snapshot
	close(s.done)
}

func closeSource(source io.Closer) error {
	if err := source.Close(); err != nil {
		return fmt.Errorf("session: close source: %w", err)
	}
	return nil
}

func (s *Session) Snapshot() Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snapshot := s.snapshot
	if snapshot.State == StateRunning && s.ctx.Err() != nil {
		snapshot.State = StateCanceling
	}
	return snapshot
}

// Cancel requests cooperative cancellation without waiting. A scan which has
// already returned can still finish successfully if it wins the race.
func (s *Session) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelLocked()
}

func (s *Session) cancelLocked() {
	if !s.snapshot.State.Terminal() {
		s.snapshot.State = StateCanceling
		s.cancel()
	}
}

// Wait waits for scanning and any owned-source close, returning their recorded
// error(s). For Start, completion permits the caller to close its borrowed source.
// Structural diagnostics do not produce a Go error. The wait context controls
// only this call and never cancels the scan; nil means no deadline. A finished
// task's error takes precedence over the wait context's cancellation.
func (s *Session) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-s.done:
		return s.taskError()
	default:
	}
	select {
	case <-s.done:
		return s.taskError()
	case <-ctx.Done():
		select {
		case <-s.done:
			return s.taskError()
		default:
			return ctx.Err()
		}
	}
}

func (s *Session) taskError() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

func (s *Session) finishedResult() (flv.ScanResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.snapshot.Closed {
		return flv.ScanResult{}, ErrClosed
	}
	if !s.snapshot.State.Terminal() {
		return flv.ScanResult{}, ErrNotReady
	}
	return s.result, nil
}

// Page returns independent records with the usual FLV page bounds. Partial
// indexes are accessible after cancellation/failure until Close is requested.
func (s *Session) Page(start uint64, count uint32) (flv.TagPage, error) {
	result, err := s.finishedResult()
	if err != nil {
		return flv.TagPage{}, err
	}
	return result.Index().Page(start, count)
}

// Tag returns one independent record from the final committed index without
// allocating a page or copying diagnostics. It shares Page's access lifetime.
func (s *Session) Tag(id uint64) (flv.TagRecord, error) {
	result, err := s.finishedResult()
	if err != nil {
		return flv.TagRecord{}, err
	}
	tag, found := result.Index().Get(id)
	if !found {
		return flv.TagRecord{}, ErrTagNotFound
	}
	return tag, nil
}

// Result returns a final result with independent diagnostics and a shared
// immutable chunked index. The access error is separate from the task error
// returned by Wait. Returned results can outlive Close and retain index memory.
func (s *Session) Result() (flv.ScanResult, error) {
	result, err := s.finishedResult()
	if err != nil {
		return flv.ScanResult{}, err
	}
	result.Diagnostics = cloneDiagnostics(result.Diagnostics)
	return result, nil
}

func cloneDiagnostics(diagnostics []flv.Diagnostic) []flv.Diagnostic {
	diagnostics = slices.Clone(diagnostics)
	for i := range diagnostics {
		diagnostic := &diagnostics[i]
		if diagnostic.Expected != nil {
			value := *diagnostic.Expected
			diagnostic.Expected = &value
		}
		if diagnostic.Actual != nil {
			value := *diagnostic.Actual
			diagnostic.Actual = &value
		}
	}
	return diagnostics
}

// Close disables new result/tag/page access, cancels the worker, and releases the
// session's result references once scanning ends. It waits using ctx and returns
// the same task/wait error as Wait. If the wait expires, the worker still owns
// cleanup; no source is forcibly closed while a read is active. Close is
// idempotent. Previously captured results/pages remain valid.
func (s *Session) Close(ctx context.Context) error {
	s.mu.Lock()
	s.snapshot.Closed = true
	s.result = flv.ScanResult{}
	s.cancelLocked()
	s.mu.Unlock()
	return s.Wait(ctx)
}
