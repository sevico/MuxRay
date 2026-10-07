package picker

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"os"
	"os/exec"
	"strconv"
	"time"
)

const maxDuration = 10 * time.Minute
const cleanupDuration = 5 * time.Second

type containment interface {
	assign(*os.Process) error
	close() error
}

// Run returns a path only after the child has actually exited successfully and
// its kill-on-close Job has been closed. It never falls back to an in-process UI.
// A caller should map ErrCanceled to its normal canceled-dialog behavior.
func Run(ctx context.Context, exe string, owner uintptr) (string, error) {
	return run(ctx, exe, owner, nil)
}

// extraEnv is private and only used by subprocess tests; requests never use argv.
func run(ctx context.Context, exe string, owner uintptr, extraEnv []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, maxDuration)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	created, err := parentCreated()
	if err != nil {
		return "", err
	}
	nonce := make([]byte, 16)
	if _, err = rand.Read(nonce); err != nil {
		return "", ErrStart
	}
	req := Request{Version: Version, Nonce: hex.EncodeToString(nonce), Owner: strconv.FormatUint(uint64(owner), 10), Kind: "open", ParentPID: uint32(os.Getpid()), ParentCreated: created}
	if !validRequest(req) {
		return "", ErrProtocol
	}
	job, err := newContainment()
	if err != nil {
		return "", err
	}
	jobClosed := false
	closeJob := func() error {
		if jobClosed {
			return nil
		}
		jobClosed = true
		return job.close()
	}
	defer closeJob()
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return "", ErrStart
	}
	defer stdinR.Close()
	defer stdinW.Close()
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", ErrStart
	}
	defer stdoutR.Close()
	defer stdoutW.Close()
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		return "", ErrStart
	}
	defer stderrR.Close()
	defer stderrW.Close()
	cmd := exec.Command(exe, ChildArg)
	configureChild(cmd)
	cmd.Env = append(os.Environ(), extraEnv...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdinR, stdoutW, stderrW
	if err = cmd.Start(); err != nil {
		return "", ErrStart
	}
	stdinR.Close()
	stdoutW.Close()
	stderrW.Close()
	wait := make(chan error, 1)
	// Before this succeeds the child has no request and must remain UI/COM idle.
	// Keep exec's process handle until assignment completes: an early Wait could
	// release an already-exited child's last handle before opening its PID.
	if err = job.assign(cmd.Process); err != nil {
		cmd.Process.Kill()
		closeJob()
		go func() { wait <- cmd.Wait() }()
		select {
		case <-wait:
			return "", ErrContainment
		case <-time.After(cleanupDuration):
			return "", ErrCleanup
		}
	}
	go func() { wait <- cmd.Wait() }()
	type responseResult struct {
		response Response
		err      error
	}
	output := make(chan responseResult, 1)
	go func() { r, e := readResponse(stdoutR, req.Nonce); output <- responseResult{r, e} }()
	stderr := make(chan error, 1)
	go func() {
		n, e := io.Copy(io.Discard, io.LimitReader(stderrR, MaxMessageBytes+1))
		if e != nil || n > MaxMessageBytes {
			stderr <- ErrProtocol
		} else {
			stderr <- nil
		}
	}()
	input := make(chan error, 1)
	go func() { e := WriteRequest(stdinW, req); stdinW.Close(); input <- e }()
	var response Response
	var failure error
	var cleanupTimer *time.Timer
	var cleanupDeadline <-chan time.Time
	stop := func(cause error) {
		if failure == nil {
			failure = cause
		}
		if cleanupTimer != nil {
			return
		}
		if e := closeJob(); e != nil {
			failure = ErrCleanup
		}
		cmd.Process.Kill() // Covers a failed Job close; harmless after actual exit.
		stdinW.Close()
		stdoutR.Close()
		stderrR.Close()
		cleanupTimer = time.NewTimer(cleanupDuration)
		cleanupDeadline = cleanupTimer.C
	}
	defer func() {
		if cleanupTimer != nil {
			cleanupTimer.Stop()
		}
	}()
	ctxDone := ctx.Done()
	for wait != nil || output != nil || stderr != nil || input != nil {
		select {
		case e := <-wait:
			wait = nil
			// Descendants can hold pipe ends after the direct child exits. Closing
			// the Job now ends them before waiting for EOF or delivering a path.
			if e := closeJob(); e != nil {
				stop(ErrCleanup)
			}
			if e != nil {
				stop(ErrChildFailed)
			}
		case r := <-output:
			output = nil
			response = r.response
			if r.err != nil {
				stop(ErrProtocol)
			}
		case e := <-stderr:
			stderr = nil
			if e != nil {
				stop(ErrProtocol)
			}
		case e := <-input:
			input = nil
			if e != nil {
				stop(ErrProtocol)
			}
		case <-ctxDone:
			ctxDone = nil
			stop(ctx.Err())
		case <-cleanupDeadline:
			return "", ErrCleanup // Wait goroutine retains responsibility to reap.
		}
	}
	if err := ctx.Err(); err != nil && failure == nil {
		failure = err
	}
	if failure != nil {
		return "", failure
	}
	switch response.Status {
	case "selected":
		return response.Path, nil
	case "canceled":
		return "", ErrCanceled
	default:
		return "", ErrChildFailed // Never expose child error text or paths.
	}
}
