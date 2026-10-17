//go:build windows && pickerprocess

package main

import (
	"context"
	"errors"
	"testing"

	"github.com/sevico/muxray/desktop/internal/picker"
)

func TestPickerOwnerModalFocusRestoration(t *testing.T) {
	// Model the intended ordering: native cleanup restores the entry enabled
	// state before giving focus back. This checks our admission/recovery policy;
	// the real Windows modal ordering requires separate native validation.
	for _, canceled := range []bool{false, true} {
		t.Run(map[bool]string{false: "selected", true: "canceled"}[canceled], func(t *testing.T) {
			enabled := true
			restores := 0
			var resultErr error
			path := `C:\sample.flv`
			if canceled {
				path, resultErr = "", picker.ErrCanceled
			}
			got, err := runWithPickerOwner(7, 12, pickerOwnerAccess{
				processID: func(uintptr) uint32 { return 12 },
				enabled:   func(uintptr) bool { return enabled },
				restore:   func(uintptr) { restores++; enabled = true },
			}, func() (string, error) {
				enabledOnEntry := enabled
				enabled = false // The native dialog establishes modality.
				if enabledOnEntry {
					enabled = true // Native cleanup precedes focus restoration.
				}
				if !enabled {
					t.Fatal("dialog returned focus to a disabled owner")
				}
				return path, resultErr
			})
			if got != path || err != resultErr || !enabled || restores != 1 {
				t.Fatalf("result=(%q, %v) enabled=%t restores=%d", got, err, enabled, restores)
			}
		})
	}
}

func TestPickerOwnerRecoversInterruptedDialog(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, context.Canceled, picker.ErrChildFailed, picker.ErrProtocol} {
		t.Run(cause.Error(), func(t *testing.T) {
			enabled := true
			restores := 0
			_, err := runWithPickerOwner(7, 12, pickerOwnerAccess{
				processID: func(uintptr) uint32 { return 12 },
				enabled:   func(uintptr) bool { return enabled },
				restore:   func(uintptr) { restores++; enabled = true },
			}, func() (string, error) {
				enabled = false // Child terminated before native modal cleanup.
				return "", cause
			})
			if !errors.Is(err, cause) || !enabled || restores != 1 {
				t.Fatalf("err=%v enabled=%t restores=%d", err, enabled, restores)
			}
		})
	}
}

func TestPickerOwnerRejectsUnavailableWindow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		pid     uint32
		enabled bool
	}{
		{"disabled", 12, false},
		{"foreign", 99, true},
		{"destroyed", 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := runWithPickerOwner(7, 12, pickerOwnerAccess{
				processID: func(uintptr) uint32 { return tc.pid },
				enabled:   func(uintptr) bool { return tc.enabled },
				restore:   func(uintptr) { t.Fatal("changed an unavailable window") },
			}, func() (string, error) {
				t.Fatal("admitted a dialog with an unavailable owner")
				return "", nil
			})
			if err == nil {
				t.Fatal("expected owner admission failure")
			}
		})
	}
}

func TestPickerOwnerDoesNotRestoreChangedWindow(t *testing.T) {
	for _, replacementPID := range []uint32{0, 99} {
		pid := uint32(12)
		_, err := runWithPickerOwner(7, 12, pickerOwnerAccess{
			processID: func(uintptr) uint32 { return pid },
			enabled:   func(uintptr) bool { return true },
			restore:   func(uintptr) { t.Fatal("restored a destroyed or foreign window") },
		}, func() (string, error) {
			pid = replacementPID
			return "", picker.ErrChildFailed
		})
		if !errors.Is(err, picker.ErrChildFailed) {
			t.Fatalf("changed the picker error: %v", err)
		}
	}
}
