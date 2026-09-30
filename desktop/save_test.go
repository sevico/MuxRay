package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/sevico/muxray/internal/desktop"
)

func saveTestHost(t *testing.T) (*host, SaveRequest, string) {
	t.Helper()
	dir := t.TempDir()
	source := filepath.Join(dir, "source.flv")
	if err := os.WriteFile(source, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}, 0600); err != nil {
		t.Fatal(err)
	}
	h := newHost(func(context.Context) (string, error) { return source, nil })
	h.startup(context.Background())
	t.Cleanup(func() { h.shutdown(context.Background()) })
	opened := h.native.OpenFLV("1")
	if opened.Error != nil {
		t.Fatal(opened.Error)
	}
	waitNativeScan(t, h, opened.Session.SessionID)
	return h, SaveRequest{RequestID: "1", Snapshot: desktop.SnapshotRequest{SessionID: opened.Session.SessionID}}, dir
}

func TestSaveSuccessRecoveryIsExactlyOnce(t *testing.T) {
	h, request, dir := saveTestHost(t)
	path := filepath.Join(dir, "诊断.json")
	var dialogs atomic.Int32
	h.native.saveDialog = func(_ context.Context, name string) (string, error) {
		dialogs.Add(1)
		if name != "source.flv.muxray.json" {
			t.Errorf("suggested name %q", name)
		}
		return path, nil
	}
	result := h.native.SaveSnapshot(request)
	if result.Error != nil || result.Pending || result.Canceled || result.FileName != "诊断.json" || result.BytesWritten == "0" {
		t.Fatalf("save %+v", result)
	}
	data, err := os.ReadFile(path)
	if err != nil || !strings.Contains(string(data), "desktopDiagnosticSnapshot") {
		t.Fatalf("report: %s, %v", data, err)
	}
	// Discarding the first IPC reply does not cause another dialog/write.
	if got := h.native.GetSaveResult("1"); !reflect.DeepEqual(got, result) {
		t.Fatalf("query %+v", got)
	}
	if err := os.WriteFile(path, []byte("later owner content"), 0600); err != nil {
		t.Fatal(err)
	}
	request.Snapshot.SessionID = "different-session"
	if got := h.native.SaveSnapshot(request); !reflect.DeepEqual(got, result) {
		t.Fatalf("repeat %+v", got)
	}
	data, _ = os.ReadFile(path)
	if dialogs.Load() != 1 || string(data) != "later owner content" {
		t.Fatal("replayed save caused a side effect")
	}
}

func TestSaveUnseenQuerySealsLateInvocation(t *testing.T) {
	h, request, _ := saveTestHost(t)
	h.native.saveDialog = func(context.Context, string) (string, error) { t.Fatal("unexpected dialog"); return "", nil }
	sealed := h.native.GetSaveResult("1")
	if !sealed.Canceled || sealed.Error != nil {
		t.Fatalf("sealed %+v", sealed)
	}
	if got := h.native.SaveSnapshot(request); !reflect.DeepEqual(got, sealed) {
		t.Fatalf("late %+v", got)
	}
	if got := h.native.GetSaveResult("2"); !got.Canceled {
		t.Fatalf("next %+v", got)
	}
	if got := h.native.SaveSnapshot(request); got.Error == nil || got.Error.Code != "stale_request" {
		t.Fatalf("stale %+v", got)
	}
}

func TestSaveDialogOutcomesAndErrorsAreRetained(t *testing.T) {
	for _, tc := range []struct {
		name, path, code string
		err              error
		canceled         bool
	}{
		{name: "cancel", canceled: true}, {name: "dialog", err: errors.New("private host path"), code: "dialog_failed"},
		{name: "relative", path: "report.json", code: "invalid_argument"}, {name: "extension", path: "report.txt", code: "invalid_argument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h, request, dir := saveTestHost(t)
			path := tc.path
			if tc.name == "extension" {
				path = filepath.Join(dir, path)
			}
			h.native.saveDialog = func(context.Context, string) (string, error) { return path, tc.err }
			result := h.native.SaveSnapshot(request)
			if result.Canceled != tc.canceled || result.Pending || result.FileName != "" || result.BytesWritten != "0" {
				t.Fatalf("result %+v", result)
			}
			if tc.code != "" && (result.Error == nil || result.Error.Code != tc.code || strings.Contains(result.Error.Message, "private")) {
				t.Fatalf("error %+v", result.Error)
			}
			if tc.code == "" && result.Error != nil {
				t.Fatal(result.Error)
			}
			if got := h.native.GetSaveResult("1"); !reflect.DeepEqual(got, result) {
				t.Fatalf("recovery %+v", got)
			}
		})
	}
}

func TestSavePendingBlocksDialogsAndSurvivesSourceClose(t *testing.T) {
	h, request, dir := saveTestHost(t)
	entered, release := make(chan struct{}), make(chan struct{})
	h.native.saveDialog = func(context.Context, string) (string, error) {
		close(entered)
		<-release
		return filepath.Join(dir, "frozen.json"), nil
	}
	done := make(chan SaveResponse, 1)
	go func() { done <- h.native.SaveSnapshot(request) }()
	<-entered
	if got := h.native.GetSaveResult("1"); !got.Pending {
		t.Fatalf("pending %+v", got)
	}
	if got := h.native.SaveSnapshot(request); !got.Pending {
		t.Fatalf("repeat %+v", got)
	}
	if got := h.native.GetSaveResult("2"); got.Error == nil || got.Error.Code != "busy" {
		t.Fatalf("query busy %+v", got)
	}
	if got := h.native.OpenFLV("2"); got.Error == nil || got.Error.Code != "busy" {
		t.Fatalf("open busy %+v", got)
	}
	other := request
	other.RequestID = "2"
	if got := h.native.SaveSnapshot(other); got.Error == nil || got.Error.Code != "busy" {
		t.Fatalf("save busy %+v", got)
	}
	h.app.Bindings().CloseFile(request.Snapshot.SessionID)
	close(release)
	if got := <-done; got.Error != nil || got.BytesWritten == "0" {
		t.Fatalf("frozen save %+v", got)
	}
	if got := h.app.Bindings().GetSession(request.Snapshot.SessionID); !got.Session.Closed {
		t.Fatal("save reopened source")
	}
}

func TestSaveShutdownBeforeCommitLeavesDestination(t *testing.T) {
	h, request, dir := saveTestHost(t)
	path := filepath.Join(dir, "prior.json")
	if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	h.native.saveDialog = func(context.Context, string) (string, error) { h.beforeClose(context.Background()); return path, nil }
	result := h.native.SaveSnapshot(request)
	if result.Error == nil || result.Error.Code != "shutting_down" || result.FileName != "" {
		t.Fatalf("shutdown %+v", result)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "original" {
		t.Fatal("shutdown replaced destination")
	}
	assertNoSnapshotTemps(t, dir)
}

func TestSaveCaptureFailureDoesNotOpenDialog(t *testing.T) {
	h, request, _ := saveTestHost(t)
	h.native.saveDialog = func(context.Context, string) (string, error) { t.Fatal("dialog after failed capture"); return "", nil }
	h.app.Bindings().CloseFile(request.Snapshot.SessionID)
	result := h.native.SaveSnapshot(request)
	if result.Error == nil || result.Error.Code != "session_closed" {
		t.Fatalf("closed capture %+v", result)
	}
}

func TestSnapshotWriterPreservesSourceAndAliases(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "source.json")
	if err := os.WriteFile(path, []byte("source media"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := protectSnapshotSource(path)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{path}
	alias := filepath.Join(dir, "alias.json")
	if err := os.Link(path, alias); err != nil {
		t.Fatal(err)
	}
	paths = append(paths, alias)
	for _, target := range paths {
		if err := writeSnapshotFile(target, []byte("report"), source, func(fn func() error) error { return fn() }); !errors.Is(err, errSaveSource) {
			t.Fatalf("target %s: %v", target, err)
		}
	}
	data, _ := os.ReadFile(path)
	if string(data) != "source media" {
		t.Fatal("source overwritten")
	}
	assertNoSnapshotTemps(t, dir)
}

func TestSnapshotWriterCommitFailuresAndReplacement(t *testing.T) {
	dir := t.TempDir()
	sourcePath := filepath.Join(dir, "source.flv")
	if err := os.WriteFile(sourcePath, []byte("media"), 0600); err != nil {
		t.Fatal(err)
	}
	source, err := protectSnapshotSource(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(dir, "report.json")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	want := errors.New("commit failure")
	if err := writeSnapshotFile(target, []byte("new report"), source, func(func() error) error { return want }); !errors.Is(err, want) {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(target)
	if string(data) != "original" {
		t.Fatal("failed save truncated target")
	}
	assertNoSnapshotTemps(t, dir)
	if err := writeSnapshotFile(target, []byte("new report"), source, func(fn func() error) error { return fn() }); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "new report" {
		t.Fatal("replacement failed")
	}
	err = writeSnapshotFile(target, []byte("should not publish"), source, func(fn func() error) error {
		if err := os.WriteFile(target, []byte("external change"), 0600); err != nil {
			return err
		}
		return fn()
	})
	if !errors.Is(err, errSaveTargetChanged) {
		t.Fatalf("target race: %v", err)
	}
	data, _ = os.ReadFile(target)
	if string(data) != "external change" {
		t.Fatal("concurrent content replaced")
	}
	assertNoSnapshotTemps(t, dir)
}

func TestSaveProtectsOriginalIdentityAfterSourcePathReplacement(t *testing.T) {
	h, request, dir := saveTestHost(t)
	sourcePath, alias := filepath.Join(dir, "source.flv"), filepath.Join(dir, "source-alias.json")
	captured, failure := h.app.CaptureSnapshot(request.Snapshot)
	if failure != nil {
		t.Fatal(failure)
	}
	// Model a source view being closed after capture, releasing Windows' deny-
	// delete handle while the independent frozen save is still in progress.
	if err := h.app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.native.capture = func(desktop.SnapshotRequest) (desktop.SnapshotCapture, *desktop.Fault) { return captured, nil }
	if err := os.Link(sourcePath, alias); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement.flv")
	if err := os.WriteFile(replacement, []byte("replacement source pathname"), 0600); err != nil {
		t.Fatal(err)
	}
	// Replacing its pathname must not cause Native to forget the old identity.
	if err := os.Rename(replacement, sourcePath); err != nil {
		t.Fatal(err)
	}
	h.native.saveDialog = func(context.Context, string) (string, error) { return alias, nil }
	result := h.native.SaveSnapshot(request)
	if result.Error == nil || result.Error.Code != "source_overwrite" {
		t.Fatalf("alias %+v", result)
	}
	data, err := os.ReadFile(alias)
	if err != nil || len(data) != 13 || string(data[:3]) != "FLV" {
		t.Fatalf("original alias changed: %q %v", data, err)
	}
}

func TestSaveWriteFailureRetainsOutcomeAndCurrentView(t *testing.T) {
	h, request, dir := saveTestHost(t)
	path := filepath.Join(dir, "failed.json")
	h.native.saveDialog = func(context.Context, string) (string, error) { return path, nil }
	h.native.writeSnapshot = func(string, []byte, snapshotSource, func(func() error) error) error {
		return errors.New("private path and filesystem error")
	}
	result := h.native.SaveSnapshot(request)
	if result.Error == nil || result.Error.Code != "write_failed" || strings.Contains(result.Error.Message, "private") || result.BytesWritten != "0" {
		t.Fatalf("failure %+v", result)
	}
	if got := h.native.GetSaveResult("1"); !reflect.DeepEqual(got, result) {
		t.Fatalf("retained %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unexpected destination: %v", err)
	}
	if got := h.app.Bindings().GetSession(request.Snapshot.SessionID); got.Error != nil || got.Session.Closed {
		t.Fatal("save failure altered source lifecycle")
	}
}

func assertNoSnapshotTemps(t *testing.T, dir string) {
	t.Helper()
	names, err := filepath.Glob(filepath.Join(dir, ".muxray-snapshot-*.tmp"))
	if err != nil || len(names) != 0 {
		t.Fatalf("temporary files: %v %v", names, err)
	}
}
