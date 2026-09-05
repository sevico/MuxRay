package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

func testClock() benchmarkClock {
	var ticks atomic.Int64
	return benchmarkClock{read: func() (int64, error) { return ticks.Add(1), nil }, frequency: 1000, source: "test"}
}

func TestCancellationTrialTimelineAndErrors(t *testing.T) {
	var input bytes.Buffer
	if err := generate(context.Background(), &input, 10000, 100); err != nil {
		t.Fatal(err)
	}
	clock := testClock()
	result, err := cancellationTrial(context.Background(), bytes.NewReader(input.Bytes()), 10000, clock, clock)
	if err != nil || !result.Canceled || result.ReadCalls != 100 || result.IndexedTags != 48 || result.Diagnostics != 0 {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if result.CancelCallMS != 1 || result.ReaderToScanReturnMS != 1 || result.CancelToScanReturnMS != 3 || result.LegacyWindowMS != 5 {
		t.Fatalf("wrong phase arithmetic: %+v", result)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cancellationTrial(ctx, bytes.NewReader(nil), 10000, clock, clock); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := cancellationTrial(context.Background(), bytes.NewReader(nil), 10000, clock, clock); err == nil {
		t.Fatal("early I/O failure accepted")
	}
	want := errors.New("clock failed")
	failed := benchmarkClock{read: func() (int64, error) { return 0, want }, frequency: 1}
	for _, pair := range [][2]benchmarkClock{{failed, clock}, {clock, failed}} {
		if _, err := cancellationTrial(context.Background(), bytes.NewReader(input.Bytes()), 10000, pair[0], pair[1]); !errors.Is(err, want) {
			t.Fatal(err)
		}
	}
}

func TestCancelProbeCommandAndTraceLimits(t *testing.T) {
	directory := t.TempDir()
	fixture := filepath.Join(directory, "fixture.flv")
	if err := run(context.Background(), []string{"generate", "--file", fixture, "--size-bytes", "10000", "--tags", "100"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	for _, prime := range []string{"false", "true"} {
		var output bytes.Buffer
		args := []string{"cancel-probe", "--file", fixture, "--tags", "100", "--pre-scans", "1", "--prime-clock=" + prime}
		if err := run(context.Background(), args, &output); err != nil {
			t.Fatal(err)
		}
		var result cancelProbe
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if result.PreScans != 1 || result.ClockPrimed != (prime == "true") || result.ReadCalls != 100 || result.ClockSource == "" {
			t.Fatal(result)
		}
	}
	for _, args := range [][]string{{"--file", fixture, "--pre-scans", "12"}, {"--file", fixture, "--tags", "99"}, {"--file", fixture, "--tags", "1000001"}, {"--file", fixture, "unexpected"}, {}} {
		if err := runCancelProbe(context.Background(), args, io.Discard); err == nil {
			t.Fatal("invalid arguments accepted")
		}
	}
	tracePath := filepath.Join(directory, "cancel.trace")
	args := []string{"cancel-probe", "--file", fixture, "--tags", "100", "--trace", tracePath}
	if err := run(context.Background(), args, io.Discard); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tracePath)
	if err != nil || info.Size() == 0 || info.Size() > 8<<20 {
		t.Fatalf("trace=%v err=%v", info, err)
	}
	if err := run(context.Background(), args, io.Discard); err == nil {
		t.Fatal("trace overwrite accepted")
	}
	limited := traceWriter{output: io.Discard, remaining: 1}
	if _, err := limited.Write([]byte{1, 2}); err == nil {
		t.Fatal("trace cap ignored")
	}
	short := traceWriter{output: shortWriter{}, remaining: 2}
	if _, err := short.Write([]byte{1, 2}); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
}

func TestClockReturnDelayIsNotAttributedToCoreCancellation(t *testing.T) {
	var input bytes.Buffer
	if err := generate(context.Background(), &input, 10000, 100); err != nil {
		t.Fatal(err)
	}
	var ticks, calls atomic.Int64
	observer := benchmarkClock{read: func() (int64, error) { return ticks.Add(1), nil }, frequency: 1000}
	measured := benchmarkClock{read: func() (int64, error) {
		stamp := ticks.Add(1)
		if calls.Add(1) == 1 {
			// Controlled simulated delay AFTER the starting timestamp but BEFORE
			// cancel(), not a claim about the historical real-machine outlier.
			ticks.Add(607)
		}
		return stamp, nil
	}, frequency: 1000}
	result, err := cancellationTrial(context.Background(), bytes.NewReader(input.Bytes()), 10000, observer, measured)
	if err != nil {
		t.Fatal(err)
	}
	if result.LegacyWindowMS < 607 || result.StartClockCallMS < 607 || result.CancelToScanReturnMS != 3 {
		t.Fatalf("clock delay not isolated: %+v", result)
	}
}
