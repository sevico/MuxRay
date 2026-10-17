//go:build windows

package picker

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func handleFlags(t *testing.T, h windows.Handle) uint32 {
	t.Helper()
	var flags uint32
	get := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetHandleInformation")
	if ok, _, err := get.Call(uintptr(h), uintptr(unsafe.Pointer(&flags))); ok == 0 {
		t.Fatal(err)
	}
	return flags
}

func TestResponsePipeOwnershipAndInheritance(t *testing.T) {
	r, w, err := newResponsePipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	if handleFlags(t, windows.Handle(r.Fd()))&windows.HANDLE_FLAG_INHERIT != 0 {
		t.Fatal("parent response reader can be inherited")
	}
	if handleFlags(t, windows.Handle(w.Fd()))&windows.HANDLE_FLAG_INHERIT == 0 {
		t.Fatal("response writer cannot reach child")
	}
	// Simulate the distinct handle table ownership in the child process without
	// creating two os.File owners for one handle in this test process.
	var inherited windows.Handle
	if err := windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(w.Fd()), windows.CurrentProcess(), &inherited, 0, true, windows.DUPLICATE_SAME_ACCESS); err != nil {
		t.Fatal(err)
	}
	w.Close()
	out, err := openResponsePipe(strconv.FormatUint(uint64(inherited), 10))
	if err != nil {
		windows.CloseHandle(inherited)
		t.Fatal(err)
	}
	defer out.Close()
	if handleFlags(t, inherited)&windows.HANDLE_FLAG_INHERIT != 0 {
		t.Fatal("child response writer can leak into Shell descendants")
	}
	want := []byte("private response")
	if _, err := out.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("response pipe contents/EOF: %q %v", got, err)
	}
}

func TestResponsePipeRejectsInvalidHandlesBeforeCOM(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "not-a-pipe"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	values := []string{"", "0", "01", "-1", "18446744073709551616", strconv.FormatUint(uint64(windows.InvalidHandle), 10), strconv.FormatUint(uint64(f.Fd()), 10)}
	for _, standard := range []uint32{windows.STD_INPUT_HANDLE, windows.STD_OUTPUT_HANDLE, windows.STD_ERROR_HANDLE} {
		if h, err := windows.GetStdHandle(standard); err == nil {
			values = append(values, strconv.FormatUint(uint64(h), 10))
		}
	}
	for _, value := range values {
		if out, err := openResponsePipe(value); out != nil || !errors.Is(err, ErrProtocol) {
			t.Fatalf("invalid response handle accepted: %v", err)
		}
		req := childTestRequest()
		req.ResponseHandle = value
		var input bytes.Buffer
		// Some values pass request syntax but must still fail the OS pipe check.
		if err := writeMessage(&input, req); err != nil {
			t.Fatal(err)
		}
		if exit := childMainWithOperations(&input, openResponsePipe, childOperations{}); exit != 2 {
			t.Fatalf("invalid pipe reached native calls or returned %d", exit)
		}
	}
	if _, err := f.Write([]byte("still owned by caller")); err != nil {
		t.Fatal("rejecting a non-pipe took ownership of its handle", err)
	}
}

type responseWriteCloser struct {
	writeError error
	closed     bool
}

func (w *responseWriteCloser) Write(p []byte) (int, error) {
	if w.writeError != nil {
		return 0, w.writeError
	}
	return len(p), nil
}

func (w *responseWriteCloser) Close() error { w.closed = true; return nil }

func TestChildClosesResponseAfterSuccessAndWriteFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		var input bytes.Buffer
		if err := WriteRequest(&input, childTestRequest()); err != nil {
			t.Fatal(err)
		}
		out := &responseWriteCloser{}
		want := 0
		if fail {
			out.writeError, want = errors.New("broken response pipe"), 3
		}
		fake := newFakeChildCOM()
		fake.showHR = hresultCanceled
		if exit := childMainWithOperations(&input, func(string) (io.WriteCloser, error) { return out, nil }, fake.operations()); exit != want || !out.closed {
			t.Fatalf("exit=%d want=%d closed=%v", exit, want, out.closed)
		}
	}
}
