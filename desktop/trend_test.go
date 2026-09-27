package main

import (
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/sevico/muxray/internal/desktop"
)

func TestNativeTagTrendsWindowContextDetailHexAndReopen(t *testing.T) {
	data := []byte{'F', 'L', 'V', 1, 4, 0, 0, 0, 9, 0, 0, 0, 0}
	for i := range 512 {
		tag := make([]byte, 15)
		tag[0], tag[4], tag[5], tag[6] = 18, byte(i>>16), byte(i>>8), byte(i)
		binary.BigEndian.PutUint32(tag[11:], 11)
		data = append(data, tag...)
	}
	for _, dts := range []byte{100, 0} {
		data = append(data, 8, 0, 0, 4, 0, 0, dts, 0, 0, 0, 0, 0xaf, 0, 0x12, 0x10, 0, 0, 0, 15)
	}
	path := filepath.Join(t.TempDir(), "trend.flv")
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
	page := b.GetTagTrends(desktop.PageRequest{SessionID: id, Start: "256", Count: 256})
	if page.Error != nil || page.Page == nil || len(page.Page.Points) != 256 || page.Page.ContextStart != "255" || page.Page.ContextScanned != 1 || *page.Page.Points[0].PreviousTagID != "255" || *page.Page.Points[0].DeltaMS != "1" {
		t.Fatalf("cross-window: %+v", page)
	}
	q := desktop.PageRequest{SessionID: id, Start: page.Page.Bounds.Next, Count: 256}
	last := b.GetTagTrends(q)
	if last.Error != nil || last.Page == nil || len(last.Page.Points) != 2 || last.Page.ContextScanned != 512 || last.Page.Points[0].Context != "start" || *last.Page.Points[1].DeltaMS != "-100" {
		t.Fatalf("last window: %+v", last)
	}
	selected := last.Page.Points[1]
	if reply := b.StartDetail(desktop.DetailRequest{SessionID: id, RequestID: "1", TagID: selected.TagID}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if ready := waitNativeDetail(t, h, id, "1"); ready.Error != nil || ready.Detail == nil || ready.Detail.Kind != "aacAudio" {
		t.Fatalf("point detail: %+v", ready)
	}
	if reply := b.StartHex(desktop.HexRequest{SessionID: id, RequestID: "2", Offset: selected.Offset, Count: 512}); reply.Error != nil {
		t.Fatal(reply.Error)
	}
	if ready := waitNativeHex(t, h, id, "2"); ready.Error != nil || ready.Window == nil || ready.Window.Offset != selected.Offset || len(ready.Window.Hex)/2 != 19 || !ready.Window.EOF {
		t.Fatalf("point Hex: %+v", ready)
	}
	b.CloseFile(id)
	waitNativeScan(t, h, id)
	if reply := b.GetTagTrends(q); reply.Error == nil || reply.Error.Code != desktop.CodeSessionClosed {
		t.Fatalf("closed: %+v", reply)
	}
	reopened := h.native.OpenFLV("2")
	if reopened.Error != nil || reopened.Session == nil || reopened.Session.SessionID == id {
		t.Fatalf("reopen: %+v", reopened)
	}
	waitNativeScan(t, h, reopened.Session.SessionID)
	if reply := b.GetTagTrends(q); reply.Error == nil || reply.Error.Code != desktop.CodeSessionNotFound {
		t.Fatalf("stale session: %+v", reply)
	}
	q.SessionID = reopened.Session.SessionID
	if reply := b.GetTagTrends(q); reply.Error != nil || reply.Page == nil || reply.Page.Points[1].TagID != selected.TagID {
		t.Fatalf("reopened query: %+v", reply)
	}
	h.beforeClose(context.Background())
	if reply := b.GetTagTrends(q); reply.Error == nil || reply.Error.Code != desktop.CodeSessionClosed {
		t.Fatalf("shutdown: %+v", reply)
	}
}
