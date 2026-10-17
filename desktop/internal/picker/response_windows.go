//go:build windows

package picker

import (
	"io"
	"os"
	"strconv"

	"golang.org/x/sys/windows"
)

// The protocol uses its own inherited pipe. Redirecting a standard handle is
// insufficient because native libraries can retain a cached stdout handle.
func newResponsePipe() (*os.File, *os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	if err = windows.SetHandleInformation(windows.Handle(r.Fd()), windows.HANDLE_FLAG_INHERIT, 0); err == nil {
		err = windows.SetHandleInformation(windows.Handle(w.Fd()), windows.HANDLE_FLAG_INHERIT, windows.HANDLE_FLAG_INHERIT)
	}
	if err != nil {
		r.Close()
		w.Close()
		return nil, nil, err
	}
	return r, w, nil
}

func openResponsePipe(value string) (io.WriteCloser, error) {
	if !decimal(value, strconv.IntSize) {
		return nil, ErrProtocol
	}
	n, _ := strconv.ParseUint(value, 10, strconv.IntSize)
	h := windows.Handle(n)
	if h == windows.InvalidHandle {
		return nil, ErrProtocol
	}
	for _, stream := range []uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE} {
		if standard, err := windows.GetStdHandle(stream); err == nil && h == standard {
			return nil, ErrProtocol
		}
	}
	if kind, err := windows.GetFileType(h); err != nil || kind != windows.FILE_TYPE_PIPE {
		return nil, ErrProtocol
	}
	// Take ownership only after validating the pipe, then prevent Shell-created
	// descendants from keeping its writer open after this child returns.
	out := os.NewFile(uintptr(h), "picker-response")
	if err := windows.SetHandleInformation(h, windows.HANDLE_FLAG_INHERIT, 0); err != nil {
		out.Close()
		return nil, ErrProtocol
	}
	return out, nil
}
