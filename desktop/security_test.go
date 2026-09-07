package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProductionSurface(t *testing.T) {
	h := newHost(nil)
	defer h.shutdown(context.Background())
	opt := appOptions(h)
	if len(opt.Bind) != 2 || opt.Bind[0] != h.app.Bindings() || opt.Bind[1] != h.native {
		t.Fatal("unexpected binding objects")
	}
	if opt.BindingsAllowedOrigins != "" || opt.EnableDefaultContextMenu || opt.EnableFraudulentWebsiteDetection || opt.DragAndDrop.EnableFileDrop || !opt.DragAndDrop.DisableWebViewDrop {
		t.Fatal("unsafe production defaults")
	}
	w := httptest.NewRecorder()
	securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })).ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 || w.Header().Get("Content-Security-Policy") != contentSecurityPolicy || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing")
	}
}
