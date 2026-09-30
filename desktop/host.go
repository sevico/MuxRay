package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/sevico/muxray/internal/desktop"
)

type fileDialog func(context.Context) (string, error)

type host struct {
	app    *desktop.App
	native *Native
	stop   sync.Once
}

// Native exposes only native dialogs and their bounded recovery results.
// Host lifecycle and the Wails context must never become callable from
// JavaScript through binding reflection.
type Native struct {
	mu            sync.Mutex
	ctx           context.Context
	closing       bool
	picking       bool
	dialog        fileDialog
	api           *desktop.Bindings
	openID        uint64
	opened        PickResponse
	saveDialog    snapshotDialog
	capture       func(desktop.SnapshotRequest) (desktop.SnapshotCapture, *desktop.Fault)
	writeSnapshot snapshotWriter
	saveID        uint64
	saved         SaveResponse
}

type PickResponse struct {
	APIVersion uint8                    `json:"apiVersion"`
	RequestID  string                   `json:"requestId"`
	Pending    bool                     `json:"pending"`
	Canceled   bool                     `json:"canceled"`
	FileName   string                   `json:"fileName"`
	Session    *desktop.SessionSnapshot `json:"session"`
	Error      *desktop.Fault           `json:"error"`
}

func newHost(dialog fileDialog) *host {
	app := desktop.New(context.Background())
	return &host{app: app, native: &Native{dialog: dialog, api: app.Bindings(), capture: app.CaptureSnapshot, writeSnapshot: writeSnapshotFile}}
}

func (h *host) startup(ctx context.Context) {
	h.native.mu.Lock()
	defer h.native.mu.Unlock()
	if !h.native.closing {
		h.native.ctx = ctx
	}
}

func (h *host) beforeClose(context.Context) bool {
	h.native.mu.Lock()
	h.native.closing = true
	h.native.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = h.app.Shutdown(ctx) // Stop admission/cancel immediately, never block the UI.
	return false
}

func (h *host) shutdown(context.Context) {
	h.stop.Do(func() {
		h.beforeClose(context.Background())
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if message := shutdownMessage(h.app.Shutdown(ctx)); message != "" {
			log.Print(message)
		}
	})
}

func shutdownMessage(err error) string {
	if err == nil {
		return ""
	}
	// errors.Is(err, context.Canceled) also matches a joined cancellation and
	// source-close failure. Suppress only context-only trees, never real errors.
	if !shutdownContextOnly(err) {
		return "MuxRay scan cleanup reported an error."
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "MuxRay cleanup timed out; pending I/O will be reclaimed on process exit."
	}
	return ""
}

func shutdownContextOnly(err error) bool {
	if err == context.Canceled || err == context.DeadlineExceeded {
		return true
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := wrapped.Unwrap()
		for _, cause := range causes {
			if !shutdownContextOnly(cause) {
				return false
			}
		}
		return len(causes) > 0
	case interface{ Unwrap() error }:
		return shutdownContextOnly(wrapped.Unwrap())
	default:
		return false
	}
}

func pickerError(code, message string) PickResponse {
	return PickResponse{APIVersion: desktop.APIVersion, Error: &desktop.Fault{Code: code, Message: message}}
}

func openRequestID(value string) (uint64, bool) {
	id, err := strconv.ParseUint(value, 10, 64)
	return id, err == nil && id != 0 && strconv.FormatUint(id, 10) == value
}

func (n *Native) openResultLocked() PickResponse {
	result := n.opened
	if result.Session != nil {
		current := n.api.GetSession(result.Session.SessionID)
		result.Session, result.Error = current.Session, current.Error
	}
	return result
}

// GetOpenResult never opens a dialog or source. An unseen request is sealed as
// canceled, so an OpenFLV invocation delivered after this query cannot create
// an orphan session. Only the latest monotonically numbered request is kept.
func (n *Native) GetOpenResult(requestID string) PickResponse {
	id, valid := openRequestID(requestID)
	if !valid {
		return pickerError("invalid_argument", "Invalid open request ID.")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if id < n.openID {
		result := pickerError("stale_request", "The open request has been superseded.")
		result.RequestID = requestID
		return result
	}
	if id > n.openID {
		if n.picking {
			result := pickerError("busy", "A file dialog is already open.")
			result.RequestID = requestID
			return result
		}
		n.openID = id
		n.opened = PickResponse{APIVersion: desktop.APIVersion, RequestID: requestID, Canceled: true}
	}
	return n.openResultLocked()
}

func (n *Native) OpenFLV(requestID string) PickResponse {
	id, valid := openRequestID(requestID)
	if !valid {
		return pickerError("invalid_argument", "Invalid open request ID.")
	}
	n.mu.Lock()
	if id <= n.openID {
		defer n.mu.Unlock()
		if id == n.openID {
			return n.openResultLocked()
		}
		result := pickerError("stale_request", "The open request has been superseded.")
		result.RequestID = requestID
		return result
	}
	fail := func(code, message string) PickResponse {
		result := pickerError(code, message)
		result.RequestID = requestID
		n.mu.Unlock()
		return result
	}
	if n.closing {
		return fail("shutting_down", "The application is shutting down.")
	}
	if n.ctx == nil {
		return fail("not_ready", "The desktop window is not ready.")
	}
	if n.picking {
		return fail("busy", "A file dialog is already open.")
	}
	n.openID = id
	n.opened = PickResponse{APIVersion: desktop.APIVersion, RequestID: requestID, Pending: true}
	n.picking = true
	ctx := n.ctx
	n.mu.Unlock()

	path, err := n.dialog(ctx)
	n.mu.Lock()
	defer n.mu.Unlock()
	finish := func(result PickResponse) PickResponse {
		result.RequestID = requestID
		n.opened, n.picking = result, false
		return result
	}
	if n.closing {
		return finish(pickerError("shutting_down", "The application is shutting down."))
	}
	if err != nil {
		return finish(pickerError("dialog_failed", "The file dialog could not be opened."))
	}
	if path == "" {
		return finish(PickResponse{APIVersion: desktop.APIVersion, Canceled: true})
	}
	// Native pickers return absolute paths. Reject a surprising relative result
	// instead of making its meaning depend on the process working directory.
	if !filepath.IsAbs(path) {
		return finish(pickerError("invalid_argument", "The selected path must be absolute."))
	}
	result := n.api.OpenFile(desktop.OpenRequest{Path: path})
	response := PickResponse{APIVersion: result.APIVersion, Session: result.Session, Error: result.Error}
	if result.Error == nil {
		response.FileName = filepath.Base(path)
	}
	return finish(response)
}
