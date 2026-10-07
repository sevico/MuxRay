// Package document owns a read-only file across scanning, details and hex reads.
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

	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
	"github.com/sevico/muxray/internal/summaryview"
)

const (
	DetailTimeout                  = 3 * time.Second
	AVCContextMaxCandidates uint32 = 32
	AVCContextMaxBytes      uint64 = 1 << 20
)

var (
	ErrSourceChanged  = errors.New("document: source changed")
	ErrStaleRequest   = errors.New("document: request superseded")
	ErrUnknownRequest = errors.New("document: request not found")
	ErrInvalidRequest = errors.New("document: invalid request")
	ErrTagNotFound    = errors.New("document: tag not indexed")
)

type sourceFile interface {
	io.ReaderAt
	io.Closer
	Stat() (os.FileInfo, error)
}

type parseDetail func(context.Context, io.ReaderAt, flv.TagRecord) (*detailview.Detail, error)

type workKind uint8

const (
	detailWork workKind = iota
	hexWork
	summaryWork
)

type work struct {
	id     uint64
	kind   workKind
	tag    flv.TagRecord
	offset uint64
	count  uint32
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
	mu             sync.Mutex
	ctx            context.Context
	cancel         context.CancelFunc
	scan           *session.Session
	avcIndex       *avccontext.CandidateIndex
	source         sourceFile
	initial        os.FileInfo
	parse          parseDetail
	timeout        time.Duration
	summaryTimeout time.Duration
	wake           chan struct{}
	done           chan struct{}
	closed         bool
	released       bool
	closeErr       error
	changed        bool
	request        work
	latest         DetailResult
	hex            HexResult
	snapshotDetail DetailResult
	snapshotHex    HexResult
	summary        SummaryResult
	summaryReport  *summaryview.Report
	pending        *work
	active         *work
	activeCancel   context.CancelFunc
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
	return startWithParser(ctx, source, info, options, nil, DetailTimeout)
}

func startWithParser(ctx context.Context, source sourceFile, info os.FileInfo, options flv.Options, parse parseDetail, timeout time.Duration) *Document {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	d := &Document{ctx: ctx, cancel: cancel, source: source, initial: info, parse: parse, timeout: timeout, summaryTimeout: SummaryTimeout, wake: make(chan struct{}, 1), done: make(chan struct{})}
	index := &avccontext.CandidateIndex{}
	d.avcIndex = index
	d.scan = session.StartWithTagObserver(ctx, source, uint64(info.Size()), options, func(scanCtx context.Context, tag flv.TagRecord) error {
		return index.Observe(scanCtx, source, tag)
	})
	if d.parse == nil {
		d.parse = d.parseWithContext
	}
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
			d.hex.Window = nil
			d.clearSnapshotReadsLocked()
			d.summary.Overview, d.summaryReport = nil, nil
			d.mu.Unlock()
			// This single worker owns closure. No read or parse remains active.
			_ = d.scan.Close(context.Background())
			d.avcIndex = nil
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
		timeout := d.timeout
		if selected.kind == summaryWork {
			timeout = d.summaryTimeout
		}
		ctx, cancel := context.WithTimeout(d.ctx, timeout)
		d.active = selected
		d.activeCancel = cancel
		d.mu.Unlock()

		var detail *detailview.Detail
		var window *HexWindow
		var summary *summaryview.Report
		var err error
		if selected.kind == hexWork {
			window, err = d.readHex(ctx, selected.offset, selected.count)
		} else if selected.kind == summaryWork {
			summary, err = d.readSummary(ctx, selected.id)
		} else {
			detail, err = d.readDetail(ctx, selected.tag)
		}
		cancel()
		d.mu.Lock()
		d.activeCancel = nil
		d.active = nil
		if !d.closed && selected.kind == summaryWork && d.summary.RequestID == selected.id {
			// A superseded active Summary remains canceling until its actual
			// read returns, even though a newer task owns the request namespace.
			if d.changed {
				err = ErrSourceChanged
			} else if d.summary.State == "canceling" {
				err = context.Canceled
			}
			d.summary.State, d.summary.Err = "ready", err
			d.summaryReport = summary
			if err != nil {
				d.summary.State = "failed"
				d.summary.Overview, d.summaryReport = nil, nil
			} else {
				d.summary.Overview = &summary.Overview
			}
		} else if !d.closed && d.request.id == selected.id {
			if selected.kind == hexWork {
				d.hex.Window, d.hex.Err, d.hex.State = window, err, "ready"
				if err != nil {
					d.hex.Window, d.hex.State = nil, "failed"
				} else {
					d.snapshotHex = d.hex
				}
			} else {
				d.latest.Detail, d.latest.Err, d.latest.State = detail, err, "ready"
				if err != nil {
					d.latest.Detail, d.latest.State = nil, "failed"
				} else {
					d.snapshotDetail = d.latest
				}
			}
		}
		d.mu.Unlock()
	}
}

func (d *Document) sourceUnchanged() error {
	d.mu.Lock()
	if d.changed {
		d.mu.Unlock()
		return ErrSourceChanged
	}
	d.mu.Unlock()
	info, err := d.source.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(d.initial, info) || d.initial.Size() != info.Size() || !d.initial.ModTime().Equal(info.ModTime()) || d.initial.Mode() != info.Mode() {
		d.mu.Lock()
		d.changed = true
		d.clearSnapshotReadsLocked()
		d.invalidateSummaryLocked(ErrSourceChanged)
		d.mu.Unlock()
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

// parseWithContext runs lookup and projection within the caller's single task
// deadline and before/after source checks. Each candidate remains payload-bound.
func (d *Document) parseWithContext(ctx context.Context, source io.ReaderAt, tag flv.TagRecord) (*detailview.Detail, error) {
	resolution, err := avccontext.ResolveIndexed(ctx, source, tag, d.scan.Tag, d.avcIndex, detailview.DesktopAVCOptions(), avccontext.Limits{MaxCandidates: AVCContextMaxCandidates, MaxBytes: AVCContextMaxBytes})
	if err != nil {
		return nil, err
	}
	parameters, err := avccontext.ResolveParameters(ctx, source, tag, d.scan.Tag, resolution, detailview.DesktopAVCOptions(), avccontext.ParameterLimits{})
	if err != nil {
		return nil, err
	}
	return detailview.ParseWithAVCParameters(ctx, source, tag, resolution, parameters)
}

// StartDetail shares request generations with StartHex. There is one active
// read task and one replaceable pending selection, even during blocked I/O.
func (d *Document) StartDetail(id, tagID uint64) (DetailResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed || d.ctx.Err() != nil {
		return DetailResult{}, session.ErrClosed
	}
	if id == 0 {
		return DetailResult{}, ErrInvalidRequest
	}
	if id < d.request.id {
		return DetailResult{}, ErrStaleRequest
	}
	if id == d.request.id {
		if d.request.kind != detailWork || tagID != d.request.tag.ID {
			return DetailResult{}, ErrInvalidRequest
		}
		return cloneResult(d.latest), nil
	}
	tag, err := d.scan.Tag(tagID)
	if err != nil {
		if errors.Is(err, session.ErrTagNotFound) {
			return DetailResult{}, ErrTagNotFound
		}
		return DetailResult{}, err
	}
	d.admitLocked(work{id: id, kind: detailWork, tag: tag})
	return cloneResult(d.latest), nil
}

// admitLocked replaces the pending task without bypassing the active worker.
// Superseded results are dropped; a caller already holding a copy may keep it.
func (d *Document) admitLocked(task work) {
	d.cancelSummaryLocked()
	d.request = task
	d.pending = &task
	d.latest, d.hex = DetailResult{}, HexResult{}
	if task.kind == hexWork {
		d.snapshotHex = HexResult{}
		d.hex = HexResult{RequestID: task.id, Offset: task.offset, Count: task.count, State: "pending"}
	} else if task.kind == detailWork {
		d.snapshotDetail = DetailResult{}
		d.latest = DetailResult{RequestID: task.id, TagID: task.tag.ID, State: "pending"}
	} else {
		d.clearSnapshotReadsLocked()
		d.summary = SummaryResult{RequestID: task.id, State: "pending", Total: d.scan.Snapshot().Progress.TagCount}
		d.summaryReport = nil
	}
	if d.activeCancel != nil {
		d.activeCancel()
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
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
	if id < d.request.id {
		return DetailResult{}, ErrStaleRequest
	}
	if id != d.request.id {
		return DetailResult{}, ErrUnknownRequest
	}
	if d.request.kind != detailWork {
		return DetailResult{}, ErrInvalidRequest
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
	d.hex.Window = nil
	d.clearSnapshotReadsLocked()
	d.summary.Overview, d.summaryReport = nil, nil
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
