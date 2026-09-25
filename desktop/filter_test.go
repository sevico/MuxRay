package main

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sevico/muxray/internal/desktop"
)

func TestNativeFilteredTagsSparseNavigationAndReopen(t *testing.T) {
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	for range 4097 {
		tag := make([]byte, 15)
		tag[0] = 18
		binary.BigEndian.PutUint32(tag[11:], 11)
		data = append(data, tag...)
	}
	audioOffset := len(data)
	data = append(data, 8, 0, 0, 4, 0, 0, 100, 0, 0, 0, 0, 0xaf, 0, 0x12, 0x10, 0, 0, 0, 15)
	path := filepath.Join(t.TempDir(), "filter.flv")
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
	waitNativeScan(t, h, id)
	b := h.app.Bindings()
	bound := "100"
	request := desktop.FilteredTagRequest{SessionID: id, Start: "0", Count: 100, Filter: desktop.TagFilter{Type: "audio", Structure: "all", DTSMin: &bound, DTSMax: &bound}}
	first := b.GetFilteredTags(request)
	if first.Error != nil || first.Page == nil || len(first.Page.Tags) != 0 || first.Page.Exhausted || first.Page.Next != "4096" {
		t.Fatalf("first empty range: %+v", first)
	}
	request.Start = first.Page.Next
	next := b.GetFilteredTags(request)
	if next.Error != nil || next.Page == nil || len(next.Page.Tags) != 1 || !next.Page.Exhausted || next.Page.Tags[0].ID != "4097" {
		t.Fatalf("sparse match: %+v", next)
	}
	selected := next.Page.Tags[0]
	if response := b.StartDetail(desktop.DetailRequest{SessionID: id, RequestID: "1", TagID: selected.ID}); response.Error != nil {
		t.Fatal(response.Error)
	}
	if ready := waitNativeDetail(t, h, id, "1"); ready.Error != nil || ready.Detail == nil || ready.Detail.Kind != "aacAudio" {
		t.Fatalf("filtered detail: %+v", ready)
	}
	if response := b.StartHex(desktop.HexRequest{SessionID: id, RequestID: "2", Offset: selected.Offset, Count: 512}); response.Error != nil {
		t.Fatal(response.Error)
	}
	if ready := waitNativeHex(t, h, id, "2"); ready.Error != nil || ready.Window == nil || ready.Window.Offset != selected.Offset || len(ready.Window.Hex)/2 != len(data)-audioOffset || !ready.Window.EOF {
		t.Fatalf("filtered Hex: %+v", ready)
	}
	b.CloseFile(id)
	waitNativeScan(t, h, id)
	if response := b.GetFilteredTags(request); response.Error == nil || response.Error.Code != desktop.CodeSessionClosed {
		t.Fatalf("closed query: %+v", response)
	}
	reopened := h.native.OpenFLV("2")
	if reopened.Error != nil || reopened.Session == nil || reopened.Session.SessionID == id {
		t.Fatalf("reopen: %+v", reopened)
	}
	waitNativeScan(t, h, reopened.Session.SessionID)
	if response := b.GetFilteredTags(request); response.Error == nil || response.Error.Code != desktop.CodeSessionNotFound {
		t.Fatalf("stale query: %+v", response)
	}
	request.SessionID = reopened.Session.SessionID
	if response := b.GetFilteredTags(request); response.Error != nil || response.Page.Tags[0].ID != "4097" {
		t.Fatalf("reopened query: %+v", response)
	}
	h.beforeClose(context.Background())
	if response := b.GetFilteredTags(request); response.Error == nil || response.Error.Code != desktop.CodeSessionClosed {
		t.Fatalf("shutdown query: %+v", response)
	}
}

func waitNativeHex(t *testing.T, h *host, id, requestID string) desktop.HexResponse {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		result := h.app.Bindings().GetHex(desktop.HexQuery{SessionID: id, RequestID: requestID})
		if result.State != "pending" {
			return result
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("Hex did not finish")
		}
	}
}
