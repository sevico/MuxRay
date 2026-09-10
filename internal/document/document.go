// Package document owns a read-only file across scanning and selected details.
// Scan completion leaves the source open; Close ends all work before closing it.
package document

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

const DetailTimeout = 3 * time.Second

var (
	ErrSourceChanged  = errors.New("document: source changed")
	ErrStaleRequest   = errors.New("document: detail request superseded")
	ErrUnknownRequest = errors.New("document: detail request not found")
	ErrInvalidRequest = errors.New("document: invalid detail request")
	ErrTagNotFound    = errors.New("document: tag not indexed")
)

type sourceFile interface {
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

type parseDetail func(context.Context, io.ReaderAt, flv.TagRecord) (*detailview.Detail, error)

type work struct {
	id  uint64
	tag flv.TagRecord
}

// DetailResult is the latest selected request. Its value is copied on return;
// consumers cannot mutate the single retained result in the document.
type DetailResult struct {
	RequestID uint64
	TagID     uint64
	State     string
	Detail    *detailview.Detail
	Err       error
}

type Document struct {
	mu           sync.Mutex
	ctx          context.Context
	cancel       context.CancelFunc
	scan         *session.Session
	source       sourceFile
	initial      os.FileInfo
	parse        parseDetail
	timeout      time.Duration
	wake         chan struct{}
	done         chan struct{}
	closed       bool
	released     bool
	closeErr     error
	changed      bool
	latest       DetailResult
	pending      *work
	activeCancel context.CancelFunc
}

// Open opens one regular file. The same handle serves the scan and all details
// until Close; stat checks detect observable changes, not a byte snapshot.
func Open(ctx context.Context, path string, options flv.Options) (*Document, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("document: open source: %w", err)
	}
	info, err := source.Stat()
	if err == nil && (!info.Mode().IsRegular() || info.Size() < 0) {
		err = errors.New("source must be a regular file with a nonnegative size")
	}
	if err != nil {
		return nil, errors.Join(fmt.Errorf("document: stat source: %w", err), source.Close())
	}
	return start(ctx, source, info, options), nil
}

func start(ctx context.Context, source sourceFile, info os.FileInfo, options flv.Options) *Document {
	return startWithParser(ctx, source, info, options, detailview.Parse, DetailTimeout)
}

func startWithParser(ctx context.Context, source sourceFile, info os.FileInfo, options flv.Options, parse parseDetail, timeout time.Duration) *Document {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	d := &Document{ctx: ctx, cancel: cancel, source: source, initial: info, parse: parse, timeout: timeout, wake: make(chan struct{}, 1), done: make(chan struct{})}
	d.scan = session.Start(ctx, source, uint64(info.Size()), options)
	go d.run()
	return d
}

func (d *Document) run() {
	_ = d.scan.Wait(context.Background())
	for {
		d.mu.Lock()
		if d.closed || d.ctx.Err() != nil {
			d.closed = true
			d.pending = nil
			d.latest.Detail = nil
			d.mu.Unlock()
			// This single worker owns closure. No read or parse remains active.
			_ = d.scan.Close(context.Background())
			err := d.source.Close()
			d.mu.Lock()
			d.closeErr, d.released = err, true
			close(d.done)
			d.mu.Unlock()
			return
		}
		selected := d.pending
		if selected == nil {
			d.mu.Unlock()
			select {
			case <-d.wake:
			case <-d.ctx.Done():
			}
			continue
		}
		d.pending = nil
		ctx, cancel := context.WithTimeout(d.ctx, d.timeout)
		d.activeCancel = cancel
		d.mu.Unlock()

		detail, err := d.readDetail(ctx, selected.tag)
		cancel()
		d.mu.Lock()
		d.activeCancel = nil
		if !d.closed && d.latest.RequestID == selected.id {
			d.latest.Detail, d.latest.Err, d.latest.State = detail, err, "ready"
			if err != nil {
				d.latest.Detail, d.latest.State = nil, "failed"
			}
		}
		d.mu.Unlock()
	}
}

func (d *Document) sourceUnchanged() error {
	if d.changed {
		return ErrSourceChanged
	}
	info, err := d.source.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(d.initial, info) || d.initial.Size() != info.Size() || !d.initial.ModTime().Equal(info.ModTime()) || d.initial.Mode() != info.Mode() {
		d.changed = true
		return ErrSourceChanged
	}
	return nil
}

func (d *Document) readDetail(ctx context.Context, tag flv.TagRecord) (*detailview.Detail, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := d.sourceUnchanged(); err != nil {
		return nil, err
	}
	detail, err := d.parse(ctx, d.source, tag)
	// Detect changes during a read before publishing any parsed fields.
	if changed := d.sourceUnchanged(); changed != nil {
		return nil, changed
	}
	if canceled := ctx.Err(); canceled != nil {
		return nil, canceled
	}
	return detail, err
}

// StartDetail fences request generations. There is one active parser and one
// replaceable pending selection, including while an old ReaderAt is blocked.
func (d *Document) StartDetail(id, tagID uint64) (DetailResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return DetailResult{}, session.ErrClosed
	}
	if id == 0 {
		return DetailResult{}, ErrInvalidRequest
	}
	if id < d.latest.RequestID {
		return DetailResult{}, ErrStaleRequest
	}
	if id == d.latest.RequestID {
		if tagID != d.latest.TagID {
			return DetailResult{}, ErrInvalidRequest
		}
		return cloneResult(d.latest), nil
	}
	page, err := d.scan.Page(tagID, 1)
	if err != nil {
		if errors.Is(err, session.ErrClosed) || errors.Is(err, session.ErrNotReady) {
			return DetailResult{}, err
		}
		return DetailResult{}, ErrTagNotFound
	}
	if len(page.Records) != 1 {
		return DetailResult{}, ErrTagNotFound
	}
	tag := page.Records[0]
	d.latest = DetailResult{RequestID: id, TagID: tagID, State: "pending"}
	d.pending = &work{id: id, tag: tag}
	if d.activeCancel != nil {
		d.activeCancel()
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
	return cloneResult(d.latest), nil
}

func (d *Document) GetDetail(id uint64) (DetailResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return DetailResult{}, session.ErrClosed
	}
	if id == 0 {
		return DetailResult{}, ErrInvalidRequest
	}
	if id < d.latest.RequestID {
		return DetailResult{}, ErrStaleRequest
	}
	if id != d.latest.RequestID {
		return DetailResult{}, ErrUnknownRequest
	}
	return cloneResult(d.latest), nil
}

func cloneResult(result DetailResult) DetailResult {
	if result.Detail != nil {
		// Detail projection guarantees a <=256 KiB JSON value. Deep copying this
		// bounded DTO also copies every optional span/diagnostic numeric pointer.
		encoded, err := json.Marshal(result.Detail)
		if err != nil {
			panic("document: invalid detail DTO")
		}
		var detail detailview.Detail
		if err := json.Unmarshal(encoded, &detail); err != nil {
			panic("document: invalid detail DTO")
		}
		result.Detail = &detail
	}
	return result
}

func (d *Document) Snapshot() session.Snapshot {
	d.mu.Lock()
	defer d.mu.Unlock()
	snapshot := d.scan.Snapshot()
	snapshot.Closed = d.closed || d.ctx.Err() != nil
	if snapshot.Closed && !d.released {
		snapshot.State = session.StateCanceling
	}
	if d.released && d.closeErr != nil {
		snapshot.State = session.StateFailed
	}
	return snapshot
}

func (d *Document) Cancel()                        { d.scan.Cancel() }
func (d *Document) Wait(ctx context.Context) error { return d.scan.Wait(ctx) }
func (d *Document) Page(start uint64, count uint32) (flv.TagPage, error) {
	d.mu.Lock()
	closed := d.closed || d.ctx.Err() != nil
	d.mu.Unlock()
	if closed {
		return flv.TagPage{}, session.ErrClosed
	}
	return d.scan.Page(start, count)
}
func (d *Document) DiagnosticPage(start uint64, count uint32) (session.DiagnosticPage, error) {
	d.mu.Lock()
	closed := d.closed || d.ctx.Err() != nil
	d.mu.Unlock()
	if closed {
		return session.DiagnosticPage{}, session.ErrClosed
	}
	return d.scan.DiagnosticPage(start, count)
}

// Close immediately ends admission and requests cancellation. A wait timeout
// never releases the source or permits a second worker to bypass blocked I/O.
func (d *Document) Close(ctx context.Context) error {
	d.mu.Lock()
	d.closed = true
	d.cancel()
	d.pending = nil
	d.latest.Detail = nil
	if d.activeCancel != nil {
		d.activeCancel()
	}
	d.mu.Unlock()
	pollCtx, stop := context.WithCancel(context.Background())
	stop()
	_ = d.scan.Close(pollCtx)
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-d.done:
	default:
		select {
		case <-d.done:
		case <-ctx.Done():
			select {
			case <-d.done:
			default:
				return ctx.Err()
			}
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return errors.Join(d.scan.Wait(context.Background()), d.closeErr)
}
