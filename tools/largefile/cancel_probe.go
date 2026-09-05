package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/trace"

	"github.com/sevico/muxray/internal/flv"
)

// This instrumented experiment is deliberately separate from the historical
// measure command. Observer calls can warm shared runtime/OS paths: compare
// against the unchanged historical binary, not just a primed measurement.
type cancelProbe struct {
	ToolVersion           int                `json:"toolVersion"`
	GoVersion             string             `json:"goVersion"`
	OS                    string             `json:"os"`
	Arch                  string             `json:"arch"`
	GOMAXPROCS            int                `json:"gomaxprocs"`
	SizeBytes             uint64             `json:"sizeBytes"`
	TagCount              uint64             `json:"tagCount"`
	PreScans              int                `json:"preScans"`
	ClockPrimed           bool               `json:"clockPrimed"`
	ClockSource           string             `json:"clockSource"`
	ClockCreateMS         float64            `json:"clockCreateMs"`
	ClockPrimeMS          float64            `json:"clockPrimeMs"`
	TraceEnabled          bool               `json:"traceEnabled"`
	BoundaryToWorkerMS    float64            `json:"boundaryToWorkerMs"`
	StartClockCallMS      float64            `json:"startClockCallMs"`
	CancelCallMS          float64            `json:"cancelCallMs"`
	CancelToScanReturnMS  float64            `json:"cancelToScanReturnMs"`
	AfterCancelToReaderMS float64            `json:"afterCancelToReaderMs"`
	ReaderToScanReturnMS  float64            `json:"readerToScanReturnMs"`
	ReturnClockCallMS     float64            `json:"returnClockCallMs"`
	LegacyWindowMS        float64            `json:"legacyWindowMs"`
	ReadCalls             uint64             `json:"readCalls"`
	ReaderAtBytes         uint64             `json:"readerAtBytes"`
	IndexedTags           uint64             `json:"indexedTags"`
	Diagnostics           int                `json:"diagnostics"`
	Canceled              bool               `json:"canceled"`
	TimelineMS            map[string]float64 `json:"timelineMs"`
}

// Timestamps are observed on two goroutines. Only after the worker is joined
// does the scanner goroutine examine its timestamps or reader state.
type cancelReader struct {
	source             io.ReaderAt
	clock              benchmarkClock
	ready              chan struct{}
	ack                chan struct{}
	reads, bytes       uint64
	boundary, released int64
	err                error
}

func (r *cancelReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.source.ReadAt(p, offset)
	r.reads++
	r.bytes += uint64(max(n, 0))
	if r.reads == 100 {
		r.boundary, r.err = r.clock.read()
		close(r.ready)
		<-r.ack
		var clockErr error
		r.released, clockErr = r.clock.read()
		r.err = errors.Join(r.err, clockErr)
	}
	return n, err
}

type workerTimes struct {
	woken, testStart, beforeCancel, afterCancel int64
	err                                         error
}

func cancellationTrial(ctx context.Context, input io.ReaderAt, size uint64, observer, clock benchmarkClock) (cancelProbe, error) {
	if err := ctx.Err(); err != nil {
		return cancelProbe{}, err
	}
	cancelCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	r := &cancelReader{source: input, clock: observer, ready: make(chan struct{}), ack: make(chan struct{})}
	worker := make(chan workerTimes, 1)
	start, err := observer.read()
	if err != nil {
		return cancelProbe{}, err
	}
	go func() {
		select {
		case <-r.ready:
		case <-cancelCtx.Done():
		}
		var w workerTimes
		var err error
		var errorsSeen []error
		w.woken, err = observer.read()
		errorsSeen = append(errorsSeen, err)
		w.testStart, err = clock.read()
		errorsSeen = append(errorsSeen, err)
		w.beforeCancel, err = observer.read()
		errorsSeen = append(errorsSeen, err)
		cancel()
		w.afterCancel, err = observer.read()
		errorsSeen = append(errorsSeen, err)
		w.err = errors.Join(errorsSeen...)
		close(r.ack)
		worker <- w
	}()
	scan, scanErr := flv.ScanContext(cancelCtx, r, size, flv.Options{})
	returned, returnErr := observer.read()
	testEnd, testErr := clock.read()
	end, endErr := observer.read()
	cancel() // Also joins the worker if an early I/O error prevented read #100.
	w := <-worker
	if err := errors.Join(r.err, w.err, returnErr, testErr, endErr); err != nil {
		return cancelProbe{}, err
	}
	if err := ctx.Err(); err != nil {
		return cancelProbe{}, err
	}
	if r.reads != 100 || !errors.Is(scanErr, context.Canceled) || scan.Complete || scan.Index().Len() == 0 || len(scan.Diagnostics) != 0 {
		return cancelProbe{}, fmt.Errorf("expected clean partial scan canceled at read #100, reads=%d: %v", r.reads, scanErr)
	}
	ticks := []int64{start, r.boundary, w.woken, w.beforeCancel, w.afterCancel, r.released, returned, end}
	for i := 1; i < len(ticks); i++ {
		if ticks[i] < ticks[i-1] {
			return cancelProbe{}, errors.New("non-monotonic observer clock")
		}
	}
	if testEnd < w.testStart {
		return cancelProbe{}, errors.New("non-monotonic measured clock")
	}
	ms := func(a, b int64) float64 { return float64(b-a) * 1000 / observer.frequency }
	return cancelProbe{
		BoundaryToWorkerMS: ms(r.boundary, w.woken), StartClockCallMS: ms(w.woken, w.beforeCancel),
		CancelCallMS: ms(w.beforeCancel, w.afterCancel), CancelToScanReturnMS: ms(w.beforeCancel, returned),
		AfterCancelToReaderMS: ms(w.afterCancel, r.released), ReaderToScanReturnMS: ms(r.released, returned),
		ReturnClockCallMS: ms(returned, end), LegacyWindowMS: float64(testEnd-w.testStart) * 1000 / clock.frequency,
		ReadCalls: r.reads, ReaderAtBytes: r.bytes, IndexedTags: scan.Index().Len(), Diagnostics: len(scan.Diagnostics), Canceled: true,
		TimelineMS: map[string]float64{
			"scanStart": 0, "readBoundary": ms(start, r.boundary), "workerAwake": ms(start, w.woken),
			"beforeCancel": ms(start, w.beforeCancel), "afterCancel": ms(start, w.afterCancel),
			"readerReleased": ms(start, r.released), "scanReturned": ms(start, returned), "afterReturnClock": ms(start, end),
		},
	}, nil
}

func runCancelProbe(ctx context.Context, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("largefile cancel-probe", flag.ContinueOnError)
	path := flags.String("file", "", "existing materialized fixture; read only")
	tags := flags.Uint64("tags", 100_000, "expected fixture tag count")
	preScans := flags.Int("pre-scans", 0, "complete scans before the first cancellation (0-11)")
	prime := flags.Bool("prime-clock", false, "read the measured clock once before cancellation")
	tracePath := flags.String("trace", "", "optional exclusive-create Go runtime trace (8 MiB cap)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" || flags.NArg() != 0 || *preScans < 0 || *preScans > 11 || *tags < 100 {
		return errors.New("invalid cancel-probe arguments")
	}
	file, err := os.Open(*path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return errors.New("fixture must be a regular file")
	}
	size := uint64(info.Size())
	if err := validateShape(size, *tags); err != nil {
		return err
	}
	for i := 0; i < *preScans; i++ {
		runtime.GC()
		scan, err := flv.ScanContext(ctx, file, size, flv.Options{})
		if err != nil {
			return err
		}
		if scan.HasErrors() || len(scan.Diagnostics) != 0 || scan.Index().Len() != *tags || scan.BytesScanned != size {
			return errors.New("pre-scan did not match fixture")
		}
		runtime.KeepAlive(scan)
	}
	observer, err := newBenchmarkClock()
	if err != nil {
		return err
	}
	begin, err := observer.read() // Always resolve/prime the observer, not the measured clock.
	if err != nil {
		return err
	}
	clock, err := newBenchmarkClock()
	if err != nil {
		return err
	}
	created, err := observer.read()
	if err != nil {
		return err
	}
	if *prime {
		if _, err := clock.read(); err != nil {
			return err
		}
	}
	primed, err := observer.read()
	if err != nil {
		return err
	}
	var traceFile *os.File
	var traceOutput *traceWriter
	if *tracePath != "" {
		traceFile, err = os.OpenFile(*tracePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return err
		}
		defer traceFile.Close()
		traceOutput = &traceWriter{output: traceFile, remaining: 8 << 20}
		if err := trace.Start(traceOutput); err != nil {
			return err
		}
	}
	result, probeErr := cancellationTrial(ctx, file, size, observer, clock)
	if traceFile != nil {
		trace.Stop()
		if err := errors.Join(traceOutput.err, traceFile.Sync()); err != nil {
			return err
		}
	}
	if probeErr != nil {
		return probeErr
	}
	if result.IndexedTags >= *tags {
		return errors.New("canceled index is not partial")
	}
	result.ToolVersion, result.GoVersion, result.OS, result.Arch = 1, runtime.Version(), runtime.GOOS, runtime.GOARCH
	result.GOMAXPROCS, result.SizeBytes, result.TagCount = runtime.GOMAXPROCS(0), size, *tags
	result.PreScans, result.ClockPrimed, result.ClockSource = *preScans, *prime, clock.source
	result.ClockCreateMS = float64(created-begin) * 1000 / observer.frequency
	result.ClockPrimeMS = float64(primed-created) * 1000 / observer.frequency
	result.TraceEnabled = traceFile != nil
	return json.NewEncoder(output).Encode(result)
}

type traceWriter struct {
	output    io.Writer
	remaining int
	err       error
}

func (w *traceWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		w.err = errors.New("trace exceeded 8 MiB; partial trace retained")
		return 0, w.err
	}
	n, err := w.output.Write(p)
	w.remaining -= n
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	w.err = errors.Join(w.err, err)
	return n, err
}
