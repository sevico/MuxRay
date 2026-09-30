package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sevico/muxray/internal/desktop"
)

type snapshotDialog func(context.Context, string) (string, error)
type snapshotWriter func(string, []byte, snapshotSource, func(func() error) error) error

type SaveRequest struct {
	RequestID string                  `json:"requestId"`
	Snapshot  desktop.SnapshotRequest `json:"snapshot"`
}

type SaveResponse struct {
	APIVersion   uint8          `json:"apiVersion"`
	RequestID    string         `json:"requestId"`
	Pending      bool           `json:"pending"`
	Canceled     bool           `json:"canceled"`
	FileName     string         `json:"fileName"`
	BytesWritten string         `json:"bytesWritten"`
	Error        *desktop.Fault `json:"error"`
}

func saveResponse(id string) SaveResponse {
	if len(id) > 20 {
		id = ""
	}
	return SaveResponse{APIVersion: desktop.APIVersion, RequestID: id, BytesWritten: "0"}
}

func saveFailure(id, code, message string) SaveResponse {
	r := saveResponse(id)
	r.Error = &desktop.Fault{Code: code, Message: message}
	return r
}

// GetSaveResult never opens a dialog or writes a file. Sealing unseen IDs also
// makes a SaveSnapshot call delivered after recovery harmless.
func (n *Native) GetSaveResult(requestID string) SaveResponse {
	id, valid := openRequestID(requestID)
	if !valid {
		return saveFailure(requestID, "invalid_argument", "Invalid save request ID.")
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if id < n.saveID {
		return saveFailure(requestID, "stale_request", "The save request has been superseded.")
	}
	if id > n.saveID {
		if n.picking {
			return saveFailure(requestID, "busy", "A native file operation is already running.")
		}
		n.saveID = id
		n.saved = saveResponse(requestID)
		n.saved.Canceled = true
	}
	return n.saved
}

// SaveSnapshot freezes bounded, already available facts before showing a native
// dialog. The frozen document may finish saving after its source view is closed.
func (n *Native) SaveSnapshot(request SaveRequest) SaveResponse {
	id, valid := openRequestID(request.RequestID)
	if !valid {
		return saveFailure(request.RequestID, "invalid_argument", "Invalid save request ID.")
	}
	n.mu.Lock()
	if id <= n.saveID {
		defer n.mu.Unlock()
		if id == n.saveID {
			return n.saved
		}
		return saveFailure(request.RequestID, "stale_request", "The save request has been superseded.")
	}
	fail := func(code, message string) SaveResponse {
		n.mu.Unlock()
		return saveFailure(request.RequestID, code, message)
	}
	if n.closing {
		return fail("shutting_down", "The application is shutting down.")
	}
	if n.ctx == nil || n.saveDialog == nil || n.capture == nil {
		return fail("not_ready", "Snapshot saving is not ready.")
	}
	if n.picking {
		return fail("busy", "A native file operation is already running.")
	}
	n.saveID = id
	n.saved = saveResponse(request.RequestID)
	n.saved.Pending = true
	n.picking = true
	ctx := n.ctx
	n.mu.Unlock()
	finish := func(result SaveResponse) SaveResponse {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.saved, n.picking = result, false
		return result
	}
	captured, failure := n.capture(request.Snapshot)
	if failure != nil {
		result := saveResponse(request.RequestID)
		result.Error = failure
		return finish(result)
	}
	source, err := protectSnapshotSource(captured.SourcePath)
	if err != nil {
		return finish(saveFailure(request.RequestID, "source_unavailable", "The source identity could not be checked for safe saving."))
	}
	if captured.SourceInfo == nil {
		return finish(saveFailure(request.RequestID, "source_unavailable", "The original source identity is unavailable."))
	}
	// Protect the retained handle even if the pathname was replaced since open.
	source.info = captured.SourceInfo
	n.mu.Lock()
	closing := n.closing
	n.mu.Unlock()
	if closing {
		return finish(saveFailure(request.RequestID, "shutting_down", "The application is shutting down."))
	}
	name := captured.FileName + ".muxray.json"
	path, err := n.saveDialog(ctx, name)
	if err != nil {
		return finish(saveFailure(request.RequestID, "dialog_failed", "The save dialog could not be opened."))
	}
	if path == "" {
		result := saveResponse(request.RequestID)
		result.Canceled = true
		return finish(result)
	}
	if !validSnapshotPath(path) {
		return finish(saveFailure(request.RequestID, "invalid_argument", "Choose an absolute path with a .json extension."))
	}
	// Shutdown and committing the completed temporary file are serialized.
	commit := func(action func() error) error {
		n.mu.Lock()
		defer n.mu.Unlock()
		if n.closing {
			return errSaveShutdown
		}
		return action()
	}
	writer := n.writeSnapshot
	if writer == nil {
		writer = writeSnapshotFile
	}
	if err := writer(path, captured.Data, source, commit); err != nil {
		code, message := "write_failed", "The snapshot could not be saved; the destination was not replaced."
		switch {
		case errors.Is(err, errSaveSource):
			code, message = "source_overwrite", "The snapshot cannot replace the source file or one of its aliases."
		case errors.Is(err, errSaveShutdown):
			code, message = "shutting_down", "The application is shutting down."
		case errors.Is(err, errSaveTargetChanged):
			code, message = "target_changed", "The destination changed while saving. Choose it again."
		}
		return finish(saveFailure(request.RequestID, code, message))
	}
	result := saveResponse(request.RequestID)
	result.FileName = filepath.Base(path)
	result.BytesWritten = strconv.Itoa(len(captured.Data))
	return finish(result)
}

func validSnapshotPath(path string) bool {
	return len(path) <= desktop.MaxPathBytes && utf8.ValidString(path) && !strings.ContainsRune(path, 0) && filepath.IsAbs(path) &&
		strings.EqualFold(filepath.Ext(path), ".json") && !strings.Contains(filepath.Base(path), ":")
}

var (
	errSaveSource        = errors.New("save: protected source")
	errSaveShutdown      = errors.New("save: shutdown")
	errSaveTargetChanged = errors.New("save: destination changed")
)

type snapshotSource struct {
	path, resolved string
	info           os.FileInfo
}

func protectSnapshotSource(path string) (snapshotSource, error) {
	info, err := os.Stat(path)
	if err != nil {
		return snapshotSource{}, err
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return snapshotSource{}, err
	}
	return snapshotSource{path: filepath.Clean(path), resolved: filepath.Clean(resolved), info: info}, nil
}

func snapshotDestination(path string, source snapshotSource) (os.FileInfo, error) {
	if strings.EqualFold(filepath.Clean(path), source.path) || strings.EqualFold(filepath.Clean(path), source.resolved) {
		return nil, errSaveSource
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Never follow a target link. SameFile also catches source hard links.
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errSaveSource
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("save: destination is not a regular file")
	}
	if os.SameFile(source.info, info) {
		return nil, errSaveSource
	}
	return info, nil
}

func writeSnapshotFile(path string, data []byte, source snapshotSource, commit func(func() error) error) error {
	before, err := snapshotDestination(path, source)
	if err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".muxray-snapshot-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	_, writeErr := temp.Write(data)
	if writeErr == nil {
		writeErr = temp.Sync()
	}
	closeErr := temp.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return commit(func() error {
		after, err := snapshotDestination(path, source)
		if err != nil {
			return err
		}
		if (before == nil) != (after == nil) {
			return errSaveTargetChanged
		}
		if before != nil && (!os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime())) {
			return errSaveTargetChanged
		}
		return os.Rename(temp.Name(), path)
	})
}
