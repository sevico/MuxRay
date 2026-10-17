//go:build windows

package picker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Helpers are test executables with no COM or GUI initialization.
func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == ChildArg && os.Getenv("PICKER_TEST_CHILD_MODE") != "" {
		os.Exit(testChild())
	}
	os.Exit(m.Run())
}

func testChild() int {
	mode := os.Getenv("PICKER_TEST_CHILD_MODE")
	dir := os.Getenv("PICKER_TEST_DIR")
	if mode == "parent" {
		_, err := run(context.Background(), os.Args[0], 1, []string{"PICKER_TEST_CHILD_MODE=parentdeath_child"})
		if err != nil {
			return 41
		}
		return 0
	}
	if mode == "grandchild" {
		os.WriteFile(filepath.Join(dir, "grandchild.pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
		time.Sleep(time.Hour)
		return 0
	}
	os.WriteFile(filepath.Join(dir, "child.pid"), []byte(strconv.Itoa(os.Getpid())), 0600)
	req, err := ReadRequest(os.Stdin)
	if err != nil {
		return 31
	}
	os.WriteFile(filepath.Join(dir, "request-received"), []byte("ok"), 0600)
	out, err := openResponsePipe(req.ResponseHandle)
	if err != nil {
		return 32
	}
	defer out.Close()
	response := Response{Version: Version, Nonce: req.Nonce, Status: "selected", Path: `C:\fixture\sample.flv`}
	switch mode {
	case "noisy_selected", "noisy_canceled":
		// Use the native standard handle, as a Shell DLL with cached CRT stdout
		// would; this must never become part of the protocol response.
		h, err := windows.GetStdHandle(windows.STD_OUTPUT_HANDLE)
		if err != nil {
			return 38
		}
		warning := []byte("[WARNING] Ignore caches that are heterogeneous\r\n")
		var n uint32
		if err := windows.WriteFile(h, warning, &n, nil); err != nil || int(n) != len(warning) {
			return 38
		}
		if mode == "noisy_canceled" {
			response.Status, response.Path = "canceled", ""
		}
		WriteResponse(out, response)
		if n, err := os.Stdout.Write([]byte{0xff, 0xfe, '\n'}); err != nil || n != 3 {
			return 38
		}
		diagnostic := []byte("native diagnostic\n")
		if n, err := os.Stderr.Write(diagnostic); err != nil || n != len(diagnostic) {
			return 38
		}
	case "stdout_boundary":
		if n, err := os.Stdout.Write([]byte(strings.Repeat("x", MaxMessageBytes))); err != nil || n != MaxMessageBytes {
			return 38
		}
		WriteResponse(out, response)
	case "stdout_oversize":
		os.Stdout.Write([]byte(strings.Repeat("x", MaxMessageBytes+1)))
		time.Sleep(time.Hour)
	case "stdout_response_only":
		WriteResponse(os.Stdout, response)
	case "response_prefix":
		out.Write([]byte("[WARNING] Ignore caches that are heterogeneous\r\n"))
		WriteResponse(out, response)
	case "response_extra_json":
		WriteResponse(out, response)
		WriteResponse(out, response)
	case "selected":
		WriteResponse(out, response)
		time.Sleep(150 * time.Millisecond)
	case "canceled":
		response.Status = "canceled"
		response.Path = ""
		WriteResponse(out, response)
	case "error":
		response.Status = "error"
		response.Path = ""
		response.Error = "dialog_show"
		WriteResponse(out, response)
	case "crash":
		return 17
	case "valid_then_crash":
		WriteResponse(out, response)
		return 17
	case "mismatch":
		response.Nonce = strings.Repeat("0", 32)
		WriteResponse(out, response)
	case "utf8":
		out.Write([]byte{0xff, 0xfe})
		time.Sleep(time.Hour)
	case "oversize":
		out.Write([]byte(strings.Repeat("x", MaxMessageBytes+1)))
		time.Sleep(time.Hour)
	case "stderr_oversize":
		os.Stderr.Write([]byte(strings.Repeat("x", MaxMessageBytes+1)))
		time.Sleep(time.Hour)
	case "hang", "parentdeath_child":
		time.Sleep(time.Hour)
	case "grandchild_hold_pipe", "grandchild_hold_response":
		child := exec.Command(os.Args[0], ChildArg)
		configureChild(child, nil)
		if mode == "grandchild_hold_response" {
			// Deliberately leak a duplicate into a descendant to exercise the
			// Job boundary even if an extension retains our protocol pipe.
			var leaked windows.Handle
			if windows.DuplicateHandle(windows.CurrentProcess(), windows.Handle(out.(*os.File).Fd()), windows.CurrentProcess(), &leaked, 0, true, windows.DUPLICATE_SAME_ACCESS) != nil {
				return 37
			}
			defer windows.CloseHandle(leaked)
			child.SysProcAttr.AdditionalInheritedHandles = []syscall.Handle{syscall.Handle(leaked)}
		}
		child.Env = append(os.Environ(), "PICKER_TEST_CHILD_MODE=grandchild")
		child.Stdout = os.Stdout
		child.Stderr = os.Stderr
		if child.Start() != nil {
			return 35
		}
		if !waitFile(filepath.Join(dir, "grandchild.pid"), time.Second) {
			return 36
		}
		WriteResponse(out, response)
	case "handshake":
		response.Status = "canceled"
		response.Path = ""
		WriteResponse(out, response)
	default:
		return 39
	}
	return 0
}

func waitFile(path string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func processHandle(t *testing.T, path string) windows.Handle {
	t.Helper()
	if !waitFile(path, 3*time.Second) {
		t.Fatal("helper identity not published")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(string(b), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { windows.CloseHandle(h) })
	return h
}

func assertExited(t *testing.T, h windows.Handle) {
	t.Helper()
	status, err := windows.WaitForSingleObject(h, 2000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("process not actually exited: %x %v", status, err)
	}
}

func TestRunnerSubprocesses(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want error
	}{{"selected", nil}, {"canceled", ErrCanceled}, {"noisy_selected", nil}, {"noisy_canceled", ErrCanceled}, {"stdout_boundary", nil}, {"stdout_oversize", ErrProtocol}, {"stdout_response_only", ErrProtocol}, {"response_prefix", ErrProtocol}, {"response_extra_json", ErrProtocol}, {"error", ErrChildFailed}, {"crash", ErrChildFailed}, {"valid_then_crash", ErrChildFailed}, {"mismatch", ErrProtocol}, {"utf8", context.DeadlineExceeded}, {"oversize", ErrProtocol}, {"stderr_oversize", ErrProtocol}, {"hang", context.DeadlineExceeded}, {"grandchild_hold_pipe", nil}, {"grandchild_hold_response", nil}} {
		t.Run(tc.mode, func(t *testing.T) {
			dir := t.TempDir()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			start := time.Now()
			path, err := run(ctx, os.Args[0], 1, []string{"PICKER_TEST_CHILD_MODE=" + tc.mode, "PICKER_TEST_DIR=" + dir})
			// A crashing child can close the request/output pipe concurrently;
			// either fail-closed error is valid, but no path may escape.
			if tc.mode == "crash" {
				if err == nil {
					t.Fatal("crash accepted")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("error=%v want=%v", err, tc.want)
			}
			if tc.want == nil {
				if path != `C:\fixture\sample.flv` {
					t.Fatal("path mismatch")
				}
			} else if path != "" {
				t.Fatal("failed operation returned path")
			}
			if tc.mode == "selected" && time.Since(start) < 150*time.Millisecond {
				t.Fatal("returned before child exit")
			}
			if time.Since(start) > 4*time.Second {
				t.Fatal("unbounded cleanup")
			}
			// Probe retained OS identity where available; PID absence also means exited.
			for _, name := range []string{"child.pid", "grandchild.pid"} {
				b, e := os.ReadFile(filepath.Join(dir, name))
				if e != nil {
					continue
				}
				pid, _ := strconv.ParseUint(string(b), 10, 32)
				h, e := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
				if e == windows.ERROR_INVALID_PARAMETER {
					continue
				}
				if e != nil {
					t.Fatal(e)
				}
				assertExited(t, h)
				windows.CloseHandle(h)
			}
		})
	}
}

func TestChildNoRequestNoWork(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], ChildArg)
	configureChild(cmd, nil)
	cmd.Env = append(os.Environ(), "PICKER_TEST_CHILD_MODE=handshake", "PICKER_TEST_DIR="+dir)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Start() != nil {
		t.Fatal("start")
	}
	defer cmd.Process.Kill()
	if !waitFile(filepath.Join(dir, "child.pid"), time.Second) {
		t.Fatal("child not idle")
	}
	if _, err = os.Stat(filepath.Join(dir, "request-received")); !os.IsNotExist(err) {
		t.Fatal("worked before request")
	}
	pipe.Close()
	if cmd.Wait() == nil {
		t.Fatal("EOF without request accepted")
	}
}

func TestParentDeathKillsContainedChild(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command(os.Args[0], ChildArg)
	configureChild(cmd, nil)
	cmd.Env = append(os.Environ(), "PICKER_TEST_CHILD_MODE=parent", "PICKER_TEST_DIR="+dir)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	h := processHandle(t, filepath.Join(dir, "child.pid"))
	if !waitFile(filepath.Join(dir, "request-received"), time.Second) {
		t.Fatal("child handshake absent")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	cmd.Wait()
	assertExited(t, h)
}

func TestAlreadyCanceledAndInvalidOwner(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, os.Args[0], 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := Run(context.Background(), os.Args[0], 0); !errors.Is(err, ErrProtocol) {
		t.Fatal(fmt.Sprint(err))
	}
}

func TestCancelRunningChildReapsBeforeReturn(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type outcome struct {
		path string
		err  error
	}
	done := make(chan outcome, 1)
	go func() {
		path, err := run(ctx, os.Args[0], 1, []string{"PICKER_TEST_CHILD_MODE=hang", "PICKER_TEST_DIR=" + dir})
		done <- outcome{path, err}
	}()
	h := processHandle(t, filepath.Join(dir, "child.pid"))
	if !waitFile(filepath.Join(dir, "request-received"), time.Second) {
		t.Fatal("request not received")
	}
	cancel()
	select {
	case got := <-done:
		if got.path != "" || !errors.Is(got.err, context.Canceled) {
			t.Fatalf("cancel outcome: %v", got.err)
		}
		assertExited(t, h)
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not reap promptly")
	}
}
