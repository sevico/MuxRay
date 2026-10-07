package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func main() {
	if handled, code := runPickerChild(); handled {
		os.Exit(code)
	}
	if err := runDesktop(); err != nil {
		// Avoid leaking host-specific paths in UI/binding errors.
		log.Print("MuxRay desktop could not start; check the WebView2 runtime and desktop build.")
		os.Exit(1)
	}
}

func runDesktop() error {
	host := newHost(openFileDialog)
	host.native.saveDialog = func(ctx context.Context, name string) (string, error) {
		return runtime.SaveFileDialog(ctx, runtime.SaveDialogOptions{
			Title: "导出诊断快照 JSON", DefaultFilename: name,
			Filters: []runtime.FileFilter{{DisplayName: "JSON 诊断快照 (*.json)", Pattern: "*.json"}},
		})
	}
	defer host.shutdown(context.Background())
	return wails.Run(appOptions(host))
}

func appOptions(host *host) *options.App {
	return &options.App{
		Title: "MuxRay · 码流透镜", Width: 1200, Height: 800,
		MinWidth: 860, MinHeight: 560,
		BackgroundColour: options.NewRGB(245, 247, 250),
		AssetServer:      &assetserver.Options{Assets: assets, Middleware: securityHeaders},
		OnStartup:        host.startup, OnBeforeClose: host.beforeClose, OnShutdown: host.shutdown,
		Bind:        []interface{}{host.app.Bindings(), host.native},
		DragAndDrop: &options.DragAndDrop{DisableWebViewDrop: true},
	}
}

const contentSecurityPolicy = "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'none'; frame-src 'none'; frame-ancestors 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
