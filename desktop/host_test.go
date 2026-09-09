package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/desktop"
)

func TestShutdownMessage(t *testing.T) {
	const failed = "MuxRay scan cleanup reported an error."
	const timedOut = "MuxRay cleanup timed out; pending I/O will be reclaimed on process exit."
	closeErr := errors.New("private path and OS close details")
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{name: "success"},
		{name: "canceled", err: context.Canceled},
		{name: "wrapped-canceled", err: fmt.Errorf("session: %w", context.Canceled)},
		{name: "joined-cancellations", err: errors.Join(context.Canceled, fmt.Errorf("scan: %w", context.Canceled))},
		{name: "deadline", err: context.DeadlineExceeded, want: timedOut},
		{name: "wrapped-deadline", err: fmt.Errorf("shutdown: %w", context.DeadlineExceeded), want: timedOut},
		{name: "canceled-and-deadline", err: errors.Join(context.Canceled, context.DeadlineExceeded), want: timedOut},
		{name: "close-failed", err: closeErr, want: failed},
		{name: "canceled-and-close-failed", err: errors.Join(context.Canceled, fmt.Errorf("session: close source: %w", closeErr)), want: failed},
		{name: "nested-canceled-and-close-failed", err: fmt.Errorf("shutdown: %w", errors.Join(fmt.Errorf("scan: %w", context.Canceled), fmt.Errorf("session: close source: %w", closeErr))), want: failed},
		{name: "deadline-and-close-failed", err: errors.Join(context.DeadlineExceeded, closeErr), want: failed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := shutdownMessage(tc.err); got != tc.want {
				t.Fatalf("message = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNativeSurface(t *testing.T) {
	typ := reflect.TypeOf((*Native)(nil))
	if typ.NumMethod() != 2 || typ.Method(0).Name != "GetOpenResult" || typ.Method(1).Name != "OpenFLV" {
		t.Fatalf("unexpected native surface: %v", typ)
	}
}

func TestPickerOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, path, code string
		err              error
		canceled         bool
	}{
		{name: "cancel", canceled: true},
		{name: "failure", err: errors.New("private path and OS details"), code: "dialog_failed"},
		{name: "relative", path: "relative.flv", code: "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHost(func(context.Context) (string, error) { return tc.path, tc.err })
			defer h.shutdown(context.Background())
			if result := h.native.OpenFLV("1"); result.Error == nil || result.Error.Code != "not_ready" {
				t.Fatalf("before startup: %+v", result)
			}
			h.startup(context.Background())
			result := h.native.OpenFLV("1")
			if result.APIVersion != 1 || result.Canceled != tc.canceled || result.Session != nil || result.FileName != "" {
				t.Fatalf("unexpected outcome: %+v", result)
			}
			if tc.code != "" && (result.Error == nil || result.Error.Code != tc.code || strings.Contains(result.Error.Message, "private")) {
				t.Fatalf("unexpected error: %+v", result.Error)
			}
			if tc.code == "" && result.Error != nil {
				t.Fatal(result.Error)
			}
		})
	}
}

func TestPickerOpensOnlyOwnedSlot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "测试.flv")
	if err := os.WriteFile(path, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	h := newHost(func(context.Context) (string, error) { return path, nil })
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	result := h.native.OpenFLV("1")
	if result.Error != nil || result.Session == nil || result.FileName != "测试.flv" || result.Canceled {
		t.Fatalf("open: %+v", result)
	}
	if second := h.native.OpenFLV("2"); second.Error == nil || second.Error.Code != "busy" || second.FileName != "" {
		t.Fatalf("second open: %+v", second)
	}
	if h.beforeClose(context.Background()) {
		t.Fatal("window close unexpectedly prevented")
	}
	if snapshot := h.app.Bindings().GetSession(result.Session.SessionID); !snapshot.Session.Closed {
		t.Fatal("window close did not close the owned session")
	}
	if response := h.app.Bindings().OpenFile(desktop.OpenRequest{Path: path}); response.Error.Code != "shutting_down" {
		t.Fatal("window close did not stop adapter admission")
	}
}

func TestCloseDuringPickerAndConcurrentAdmission(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	path := filepath.Join(t.TempDir(), "never-open.flv")
	h := newHost(func(context.Context) (string, error) {
		close(entered)
		<-release
		return path, nil
	})
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	done := make(chan PickResponse, 1)
	go func() { done <- h.native.OpenFLV("1") }()
	<-entered
	if second := h.native.OpenFLV("2"); second.Error == nil || second.Error.Code != "busy" {
		t.Fatalf("concurrent dialog: %+v", second)
	}
	h.beforeClose(context.Background())
	h.startup(context.Background()) // Late startup cannot reopen shutdown admission.
	close(release)
	if result := <-done; result.Error == nil || result.Error.Code != "shutting_down" || result.Session != nil {
		t.Fatalf("late picker: %+v", result)
	}
	if result := h.native.OpenFLV("1"); result.Error == nil || result.Error.Code != "shutting_down" {
		t.Fatalf("shutdown admission: %+v", result)
	}
}

func TestNativeRealScanAndBoundedPages(t *testing.T) {
	for _, name := range []string{"complete", "mismatch-warning", "truncated-error"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "pages.flv")
			data := []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}
			for i := 0; i < 205; i++ {
				data = append(data, 18, 0, 0, 0, 0, 0, byte(i), 0, 0, 0, 0, 0, 0, 0, 11)
			}
			if name == "mismatch-warning" {
				data[len(data)-1] = 99
			} else if name == "truncated-error" {
				data = data[:len(data)-1]
			}
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			h := newHost(func(context.Context) (string, error) { return path, nil })
			defer h.shutdown(context.Background())
			h.startup(context.Background())
			opened := h.native.OpenFLV("1")
			if opened.Error != nil || opened.Session == nil {
				t.Fatalf("open: %+v", opened)
			}
			id := opened.Session.SessionID
			snapshot := waitNativeScan(t, h, id)
			if snapshot.Failure != nil || snapshot.Scan == nil || snapshot.Scan.HasErrors != (name == "truncated-error") || snapshot.Scan.Complete != (name != "truncated-error") {
				t.Fatalf("outcome: %+v, scan: %+v", snapshot, snapshot.Scan)
			}
			start := "0"
			for _, count := range []int{100, 100, 5} {
				result := h.app.Bindings().GetTags(desktop.PageRequest{SessionID: id, Start: start, Count: 100})
				if result.Error != nil || result.Page == nil || len(result.Page.Tags) != count || result.Page.Bounds.Total != "205" || result.Page.Tags[0].ID != start {
					t.Fatalf("page at %s: %+v", start, result)
				}
				start = result.Page.Bounds.Next
			}
			diagnostics := h.app.Bindings().GetDiagnostics(desktop.PageRequest{SessionID: id, Start: "0", Count: 100})
			if diagnostics.Error != nil || diagnostics.Page == nil || (len(diagnostics.Page.Diagnostics) != 0) != (name != "complete") {
				t.Fatalf("diagnostics: %+v", diagnostics)
			}
			h.app.Bindings().CloseFile(id)
			if result := h.app.Bindings().GetTags(desktop.PageRequest{SessionID: id, Start: "0", Count: 100}); result.Error == nil || result.Error.Code != "session_closed" {
				t.Fatal("closed host leaked page access")
			}
		})
	}
}

func waitNativeScan(t *testing.T, h *host, id string) *desktop.SessionSnapshot {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		result := h.app.Bindings().GetSession(id)
		if result.Error != nil || result.Session == nil {
			t.Fatalf("status: %+v", result)
		}
		if result.Session.Terminal {
			return result.Session
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("scan did not terminate")
		}
	}
}
