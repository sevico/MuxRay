package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestRecoverOpenAfterLostReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "恢复测试.flv")
	if err := os.WriteFile(path, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := newHost(func(context.Context) (string, error) { calls++; return path, nil })
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	lost := h.native.OpenFLV("1")
	if lost.Error != nil || lost.Session == nil {
		t.Fatalf("open: %+v", lost)
	}
	waitNativeScan(t, h, lost.Session.SessionID)
	recovered := h.native.GetOpenResult("1")
	if recovered.RequestID != "1" || recovered.Pending || recovered.Session == nil || !recovered.Session.Terminal || recovered.Session.SessionID != lost.Session.SessionID || recovered.FileName != "恢复测试.flv" {
		t.Fatalf("recover: %+v", recovered)
	}
	if duplicate := h.native.OpenFLV("1"); duplicate.Session.SessionID != lost.Session.SessionID || calls != 1 {
		t.Fatal("duplicate created another dialog/session")
	}
	h.app.Bindings().CloseFile(lost.Session.SessionID)
	if closed := h.native.GetOpenResult("1"); !closed.Session.Closed {
		t.Fatal("recovery resurrected closed session")
	}
	waitNativeScan(t, h, lost.Session.SessionID) // retained source must finish closing
	if next := h.native.OpenFLV("2"); next.Session == nil || next.Session.SessionID == lost.Session.SessionID || calls != 2 {
		t.Fatalf("next open: %+v", next)
	}
	if stale := h.native.OpenFLV("1"); stale.Error == nil || stale.Error.Code != "stale_request" || calls != 2 {
		t.Fatal("stale request admitted")
	}
}

func TestRecoverUnseenOpenSealsLateDelivery(t *testing.T) {
	calls := 0
	h := newHost(func(context.Context) (string, error) { calls++; return "", nil })
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	for _, id := range []string{"1", "2"} {
		result := h.native.GetOpenResult(id)
		if !result.Canceled || result.Pending || result.Error != nil || result.RequestID != id {
			t.Fatalf("seal: %+v", result)
		}
	}
	if late := h.native.OpenFLV("2"); !late.Canceled {
		t.Fatalf("late: %+v", late)
	}
	if stale := h.native.OpenFLV("1"); stale.Error == nil || stale.Error.Code != "stale_request" {
		t.Fatalf("stale: %+v", stale)
	}
	if calls != 0 {
		t.Fatal("query or late delivery opened a dialog")
	}
	if next := h.native.OpenFLV("3"); !next.Canceled || calls != 1 {
		t.Fatal("new request did not open normally")
	}
}

func TestRecoverWhilePickerPending(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	h := newHost(func(context.Context) (string, error) { close(entered); <-release; return "", nil })
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	done := make(chan PickResponse, 1)
	go func() { done <- h.native.OpenFLV("1") }()
	<-entered
	for _, result := range []PickResponse{h.native.GetOpenResult("1"), h.native.OpenFLV("1")} {
		if !result.Pending || result.Canceled || result.Session != nil || result.RequestID != "1" {
			t.Errorf("pending: %+v", result)
		}
	}
	if newer := h.native.GetOpenResult("2"); newer.Error == nil || newer.Error.Code != "busy" {
		t.Errorf("newer: %+v", newer)
	}
	close(release)
	<-done
	if result := h.native.GetOpenResult("1"); !result.Canceled || result.Pending {
		t.Fatalf("canceled: %+v", result)
	}
}

func TestInvalidOpenRequestIDs(t *testing.T) {
	h := newHost(func(context.Context) (string, error) { t.Fatal("invalid request opened dialog"); return "", nil })
	defer h.shutdown(context.Background())
	h.startup(context.Background())
	for _, id := range []string{"", "0", "01", "+1", "-1", " 1", "18446744073709551616"} {
		for _, result := range []PickResponse{h.native.OpenFLV(id), h.native.GetOpenResult(id)} {
			if result.Error == nil || result.Error.Code != "invalid_argument" {
				t.Errorf("%q: %+v", id, result)
			}
		}
	}
	if result := h.native.GetOpenResult("18446744073709551615"); !result.Canceled {
		t.Fatal("uint64 maximum rejected")
	}
}
