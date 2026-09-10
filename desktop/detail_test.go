package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/desktop"
)

func TestNativeDetailIsolationAcrossReopenAndShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "details.flv")
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0, 8, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0, 0xaf, 0, 0x12, 0x10, 0, 0, 0, 15}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	h := newHost(func(context.Context) (string, error) { return path, nil })
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	first := h.native.OpenFLV("1")
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	id := first.Session.SessionID
	waitNativeScan(t, h, id)
	b := h.app.Bindings()
	if response := b.StartDetail(desktop.DetailRequest{SessionID: id, RequestID: "1", TagID: "0"}); response.Error != nil {
		t.Fatal(response.Error)
	}
	ready := waitNativeDetail(t, h, id, "1")
	if ready.Detail == nil || ready.Detail.Kind != "aacAudio" || !ready.Detail.Complete {
		t.Fatalf("detail: %+v", ready)
	}
	b.CloseFile(id)
	waitNativeScan(t, h, id)
	second := h.native.OpenFLV("2")
	if second.Error != nil || second.Session.SessionID == id {
		t.Fatalf("reopen: %+v", second)
	}
	newID := second.Session.SessionID
	waitNativeScan(t, h, newID)
	for _, reply := range []desktop.DetailResponse{b.GetDetail(desktop.DetailQuery{SessionID: id, RequestID: "1"}), b.StartDetail(desktop.DetailRequest{SessionID: id, RequestID: "2", TagID: "0"})} {
		if reply.Error == nil || reply.Error.Code != desktop.CodeSessionNotFound || reply.SessionID != id {
			t.Fatalf("stale reply: %+v", reply)
		}
	}
	if reply := b.StartDetail(desktop.DetailRequest{SessionID: newID, RequestID: "1", TagID: "0"}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if reply := waitNativeDetail(t, h, newID, "1"); reply.Error != nil || reply.SessionID != newID {
		t.Fatalf("current reply: %+v", reply)
	}
	h.beforeClose(context.Background())
	if reply := b.GetDetail(desktop.DetailQuery{SessionID: newID, RequestID: "1"}); reply.Error == nil || reply.Error.Code != desktop.CodeSessionClosed {
		t.Fatalf("shutdown detail: %+v", reply)
	}
}

func waitNativeDetail(t *testing.T, h *host, id, requestID string) desktop.DetailResponse {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		result := h.app.Bindings().GetDetail(desktop.DetailQuery{SessionID: id, RequestID: requestID})
		if result.State != "pending" {
			return result
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("detail did not finish")
		}
	}
}
