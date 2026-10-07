//go:build windows && pickerprocess

package main

import (
	"context"
	"errors"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"github.com/sevico/muxray/desktop/internal/picker"
	"golang.org/x/sys/windows"
)

var (
	pickerUser32       = windows.NewLazySystemDLL("user32.dll")
	pickerEnumWindows  = pickerUser32.NewProc("EnumWindows")
	pickerWindowPID    = pickerUser32.NewProc("GetWindowThreadProcessId")
	pickerVisible      = pickerUser32.NewProc("IsWindowVisible")
	pickerGetWindow    = pickerUser32.NewProc("GetWindow")
	pickerEnabled      = pickerUser32.NewProc("IsWindowEnabled")
	pickerEnable       = pickerUser32.NewProc("EnableWindow")
	pickerOwnerMu      sync.Mutex
	pickerOwnerWindows []uintptr
	// NewCallback slots are retained by the Go runtime. Allocate this once,
	// not once per dialog, so repeated opens do not accumulate callbacks.
	pickerEnumCallback = syscall.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		pickerWindowPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		visible, _, _ := pickerVisible.Call(hwnd)
		owner, _, _ := pickerGetWindow.Call(hwnd, 4) // GW_OWNER
		if pid == uint32(os.Getpid()) && visible != 0 && owner == 0 {
			pickerOwnerWindows = append(pickerOwnerWindows, hwnd)
		}
		return 1
	})
)

func runPickerChild() (bool, int) {
	if len(os.Args) > 1 && os.Args[1] == picker.ChildArg {
		if len(os.Args) != 2 {
			return true, 64
		}
		return true, picker.ChildMain(os.Stdin, os.Stdout)
	}
	return false, 0
}

func openFileDialog(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	owner, err := pickerOwner()
	if err != nil {
		return "", err
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	// Restore the original enabled state even if the child crashes or is killed.
	// Native Show may change the cross-process owner state while it is modal.
	var ownerPID uint32
	pickerWindowPID.Call(owner, uintptr(unsafe.Pointer(&ownerPID)))
	if ownerPID != uint32(os.Getpid()) {
		return "", errors.New("picker owner unavailable")
	}
	enabled, _, _ := pickerEnabled.Call(owner)
	if enabled == 0 {
		return "", errors.New("picker owner unavailable")
	}
	pickerEnable.Call(owner, 0)
	defer func() {
		var pid uint32
		pickerWindowPID.Call(owner, uintptr(unsafe.Pointer(&pid)))
		if pid == uint32(os.Getpid()) {
			pickerEnable.Call(owner, 1)
		}
	}()
	path, err := picker.Run(ctx, exe, owner)
	if errors.Is(err, picker.ErrCanceled) {
		return "", nil
	}
	return path, err
}

// Discover the unique visible, unowned top-level window belonging to this
// process. Never use another app's foreground window or a title-only match.
func pickerOwner() (uintptr, error) {
	pickerOwnerMu.Lock()
	defer pickerOwnerMu.Unlock()
	pickerOwnerWindows = pickerOwnerWindows[:0]
	ok, _, err := pickerEnumWindows.Call(pickerEnumCallback, 0)
	if ok == 0 {
		return 0, err
	}
	if len(pickerOwnerWindows) != 1 {
		return 0, errors.New("picker owner is not unique")
	}
	return pickerOwnerWindows[0], nil
}
