//go:build windows && pickerprocess

package main

import "errors"

// pickerOwnerAccess keeps the owner admission and recovery policy independent
// of Win32 calls. Only the dialog owns the transition into a disabled state.
type pickerOwnerAccess struct {
	processID func(uintptr) uint32
	enabled   func(uintptr) bool
	restore   func(uintptr)
}

func runWithPickerOwner(owner uintptr, processID uint32, access pickerOwnerAccess, pick func() (string, error)) (string, error) {
	if access.processID(owner) != processID || !access.enabled(owner) {
		return "", errors.New("picker owner unavailable")
	}
	// Leave modality to IFileDialog instead of pre-disabling its owner. The
	// native reproduction reached WebView2 focus restoration with a disabled
	// owner; avoiding that state requires separate native validation.
	// Retain recovery for a terminated child, which cannot finish modal cleanup.
	defer func() {
		if access.processID(owner) == processID {
			access.restore(owner)
		}
	}()
	return pick()
}
