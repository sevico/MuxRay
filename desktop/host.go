package main

import (
	"context"
	"errors"
	"log"
	"path/filepath"
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

// Native exposes exactly one method. Host lifecycle and the Wails context must
// never become callable from JavaScript through binding reflection.
type Native struct {
	mu      sync.Mutex
	ctx     context.Context
	closing bool
	picking bool
	dialog  fileDialog
	api     *desktop.Bindings
}

type PickResponse struct {
	APIVersion uint8                    `json:"apiVersion"`
	Canceled   bool                     `json:"canceled"`
	FileName   string                   `json:"fileName"`
	Session    *desktop.SessionSnapshot `json:"session"`
	Error      *desktop.Fault           `json:"error"`
}

func newHost(dialog fileDialog) *host {
	app := desktop.New(context.Background())
	return &host{app: app, native: &Native{dialog: dialog, api: app.Bindings()}}
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
		if err := h.app.Shutdown(ctx); err != nil && !errors.Is(err, context.Canceled) {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Print("MuxRay cleanup timed out; pending I/O will be reclaimed on process exit.")
			} else {
				log.Print("MuxRay scan cleanup reported an error.")
			}
		}
	})
}

func pickerError(code, message string) PickResponse {
	return PickResponse{APIVersion: desktop.APIVersion, Error: &desktop.Fault{Code: code, Message: message}}
}

func (n *Native) OpenFLV() PickResponse {
	n.mu.Lock()
	if n.closing {
		n.mu.Unlock()
		return pickerError("shutting_down", "The application is shutting down.")
	}
	if n.ctx == nil {
		n.mu.Unlock()
		return pickerError("not_ready", "The desktop window is not ready.")
	}
	if n.picking {
		n.mu.Unlock()
		return pickerError("busy", "A file dialog is already open.")
	}
	n.picking = true
	ctx := n.ctx
	n.mu.Unlock()
	defer func() {
		n.mu.Lock()
		n.picking = false
		n.mu.Unlock()
	}()

	path, err := n.dialog(ctx)
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closing {
		return pickerError("shutting_down", "The application is shutting down.")
	}
	if err != nil {
		return pickerError("dialog_failed", "The file dialog could not be opened.")
	}
	if path == "" {
		return PickResponse{APIVersion: desktop.APIVersion, Canceled: true}
	}
	// Native pickers return absolute paths. Reject a surprising relative result
	// instead of making its meaning depend on the process working directory.
	if !filepath.IsAbs(path) {
		return pickerError("invalid_argument", "The selected path must be absolute.")
	}
	result := n.api.OpenFile(desktop.OpenRequest{Path: path})
	response := PickResponse{APIVersion: result.APIVersion, Session: result.Session, Error: result.Error}
	if result.Error == nil {
		response.FileName = filepath.Base(path)
	}
	return response
}
