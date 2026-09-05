// Command largefile creates materialized FLV framing fixtures or measures the
// scanner against a caller-selected file. It is a developer tool, not CLI API.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"runtime"
	"sort"
	"time"

	"github.com/sevico/muxray/internal/flv"
)

const maxFixtureSize = 12 << 30

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	err := run(ctx, os.Args[1:], os.Stdout)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "largefile:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: largefile generate|measure --file <path> [options]")
	}
	if args[0] == "cancel-probe" {
		return runCancelProbe(ctx, args[1:], output)
	}
	flags := flag.NewFlagSet("largefile "+args[0], flag.ContinueOnError)
	path := flags.String("file", "", "explicit input/output file; generation refuses overwrite")
	size := flags.Uint64("size-bytes", 0, "exact generated size, at most 12 GiB")
	tags := flags.Uint64("tags", 100_000, "exact generated/expected tag count (1-1000000)")
	runs := flags.Int("runs", 10, "measured scans and cancellations after one warmup (1-100)")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	if flags.NArg() != 0 || *path == "" || *tags == 0 || *tags > flv.HardMaxTags || *runs < 1 || *runs > 100 {
		return errors.New("invalid file, tags, runs, or positional argument")
	}
	switch args[0] {
	case "generate":
		if err := validateShape(*size, *tags); err != nil {
			return err
		}
		file, err := os.OpenFile(*path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		hash := sha256.New()
		// All payload bytes pass through Write: no Seek, Truncate, sparse-file API,
		// or uninitialized file allocation is used by generation.
		err = generate(ctx, io.MultiWriter(file, hash), *size, *tags)
		if err == nil {
			err = file.Sync()
		}
		closeErr := file.Close()
		if err != nil {
			return fmt.Errorf("generation left a partial file for inspection: %w", err)
		}
		if closeErr != nil {
			return closeErr
		}
		return json.NewEncoder(output).Encode(struct {
			SizeBytes    uint64 `json:"sizeBytes"`
			Tags         uint64 `json:"tags"`
			SHA256       string `json:"sha256"`
			Materialized bool   `json:"materialized"`
		}{*size, *tags, hex.EncodeToString(hash.Sum(nil)), true})
	case "measure":
		return measure(ctx, *path, *tags, *runs, output)
	default:
		return errors.New("unknown command")
	}
}

func validateShape(size, tags uint64) error {
	if tags == 0 || tags > flv.HardMaxTags || size > maxFixtureSize || size < 13+15*tags || size-(13+15*tags) > tags*0xffffff {
		return errors.New("fixture size/tag count cannot be represented within bounded FLV framing")
	}
	return nil
}

func payloadSize(size, tags, id uint64) uint32 {
	bytes := size - 13 - tags*15
	n := bytes / tags
	if id < bytes%tags {
		n++
	}
	return uint32(n)
}

func tagOffset(size, tags, id uint64) uint64 {
	bytes := size - 13 - tags*15
	return 13 + id*(15+bytes/tags) + min(id, bytes%tags)
}

func generate(ctx context.Context, output io.Writer, size, tags uint64) error {
	if err := validateShape(size, tags); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := write(output, []byte{'F', 'L', 'V', 1, 0, 0, 0, 0, 9, 0, 0, 0, 0}); err != nil {
		return err
	}
	// Repeated non-zero deterministic stress bytes, not encoded media.
	payload := make([]byte, 1<<20)
	for i := range payload {
		payload[i] = byte((i*31+17)%251 + 1)
	}
	var header [11]byte
	var previous [4]byte
	for id := uint64(0); id < tags; id++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		n := payloadSize(size, tags, id)
		header[0] = 18
		header[1] = byte(n >> 16)
		header[2] = byte(n >> 8)
		header[3] = byte(n)
		dts := uint32(id * 40)
		header[4] = byte(dts >> 16)
		header[5] = byte(dts >> 8)
		header[6] = byte(dts)
		header[7] = byte(dts >> 24)
		if err := write(output, header[:]); err != nil {
			return err
		}
		for remaining := n; remaining > 0; {
			if err := ctx.Err(); err != nil {
				return err
			}
			chunk := min(remaining, uint32(len(payload)))
			if err := write(output, payload[:chunk]); err != nil {
				return err
			}
			remaining -= chunk
		}
		binary.BigEndian.PutUint32(previous[:], 11+n)
		if err := write(output, previous[:]); err != nil {
			return err
		}
	}
	return nil
}

func write(output io.Writer, p []byte) error {
	n, err := output.Write(p)
	if err == nil && n != len(p) {
		return io.ErrShortWrite
	}
	return err
}

type distribution struct {
	Median float64   `json:"median"`
	P95    float64   `json:"p95"`
	Values []float64 `json:"values"`
}

func stats(values []float64) distribution {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	n := len(sorted)
	median := sorted[n/2]
	if n%2 == 0 {
		median = (sorted[n/2-1] + median) / 2
	}
	return distribution{Median: median, P95: sorted[int(math.Ceil(float64(n)*0.95))-1], Values: values}
}

type measurement struct {
	ToolVersion           int          `json:"toolVersion"`
	GoVersion             string       `json:"goVersion"`
	OS                    string       `json:"os"`
	Arch                  string       `json:"arch"`
	SizeBytes             uint64       `json:"sizeBytes"`
	TagCount              uint64       `json:"tagCount"`
	Warmups               int          `json:"warmups"`
	Runs                  int          `json:"runs"`
	ReaderAtBytes         uint64       `json:"readerAtBytesPerScan"`
	ScanMS                distribution `json:"scanMs"`
	AllocatedBytes        distribution `json:"allocatedBytesPerScan"`
	Allocations           distribution `json:"allocationsPerScan"`
	RetainedHeapBytes     distribution `json:"retainedHeapBytes"`
	WorkingSetBytes       distribution `json:"workingSetBytes"`
	PeakWorkingSetBytes   uint64       `json:"peakWorkingSetBytes"`
	WorkingSetSource      string       `json:"workingSetSource"`
	CancelMS              distribution `json:"cancelMs"`
	CancelClock           string       `json:"cancelClock"`
	CancelWarmups         int          `json:"cancelWarmups"`
	ColdCancelMS          float64      `json:"coldCancelMs"`
	Page256AllocatedBytes float64      `json:"page256AllocatedBytes"`
	Page256Allocations    float64      `json:"page256Allocations"`
}

type countingReader struct {
	source  io.ReaderAt
	bytes   uint64
	reads   uint64
	started chan struct{}
	ack     chan struct{}
}

func (r *countingReader) ReadAt(p []byte, offset int64) (int, error) {
	n, err := r.source.ReadAt(p, offset)
	r.bytes += uint64(max(0, n))
	r.reads++
	if r.started != nil && r.reads == 100 {
		close(r.started)
		<-r.ack // Deterministic cross-goroutine cancellation at a read boundary.
	}
	return n, err
}

func measure(ctx context.Context, path string, tags uint64, runs int, output io.Writer) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 0 {
		return errors.New("input must be a regular file")
	}
	size := uint64(info.Size())
	if err := validateShape(size, tags); err != nil {
		return err
	}
	if tags < 100 {
		return errors.New("measurement needs at least 100 tags to exercise cancellation")
	}
	result := measurement{ToolVersion: 1, GoVersion: runtime.Version(), OS: runtime.GOOS, Arch: runtime.GOARCH, SizeBytes: size, TagCount: tags, Warmups: 1, Runs: runs}
	var durations, allocBytes, allocs, heaps, workingSets, cancels []float64
	for iteration := -1; iteration < runs; iteration++ {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		source := &countingReader{source: file}
		start := time.Now()
		scan, err := flv.ScanContext(ctx, source, size, flv.Options{})
		elapsed := time.Since(start)
		runtime.ReadMemStats(&after)
		if err != nil {
			return err
		}
		if scan.HasErrors() || scan.Index().Len() != tags || scan.BytesScanned != size {
			return errors.New("scan did not match generated framing facts")
		}
		for _, id := range []uint64{0, tags / 2, tags - 1} {
			record, ok := scan.Index().Get(id)
			if !ok || record.Offset != tagOffset(size, tags, id) || record.DataSize != payloadSize(size, tags, id) || record.DTS != int64(id*40) {
				return fmt.Errorf("tag %d differs from generator oracle", id)
			}
		}
		if source.bytes != 13+15*tags {
			return fmt.Errorf("unexpected payload reads: %d bytes", source.bytes)
		}
		if iteration >= 0 {
			durations = append(durations, float64(elapsed)/float64(time.Millisecond))
			allocBytes = append(allocBytes, float64(after.TotalAlloc-before.TotalAlloc))
			allocs = append(allocs, float64(after.Mallocs-before.Mallocs))
		}
		runtime.GC()
		runtime.ReadMemStats(&after)
		if iteration >= 0 {
			heaps = append(heaps, float64(after.HeapAlloc))
			current, peak, source, err := processMemory()
			if err != nil {
				return err
			}
			workingSets = append(workingSets, float64(current))
			result.PeakWorkingSetBytes = max(result.PeakWorkingSetBytes, peak)
			result.WorkingSetSource = source
		}
		if iteration == runs-1 {
			runtime.ReadMemStats(&before)
			page, pageErr := scan.Index().Page(min(uint64(1023), tags), 256)
			runtime.ReadMemStats(&after)
			if pageErr != nil || page.Total != tags || page.Next > tags {
				return errors.New("page failed")
			}
			result.Page256AllocatedBytes = float64(after.TotalAlloc - before.TotalAlloc)
			result.Page256Allocations = float64(after.Mallocs - before.Mallocs)
			runtime.KeepAlive(page)
		}
		result.ReaderAtBytes = source.bytes
		runtime.KeepAlive(scan)
	}
	clock, err := newBenchmarkClock()
	if err != nil {
		return err
	}
	result.CancelClock = clock.source
	result.CancelWarmups = 1
	for iteration := -1; iteration < runs; iteration++ {
		cancelCtx, cancel := context.WithCancel(ctx)
		source := &countingReader{source: file, started: make(chan struct{}), ack: make(chan struct{})}
		requested := make(chan clockReading, 1)
		go func() {
			select {
			case <-source.started:
			case <-cancelCtx.Done():
			}
			at, err := clock.read()
			cancel()
			requested <- clockReading{ticks: at, err: err}
			close(source.ack)
		}()
		scan, scanErr := flv.ScanContext(cancelCtx, source, size, flv.Options{})
		end, clockErr := clock.read()
		cancel()
		at := <-requested
		if clockErr != nil {
			return clockErr
		}
		if at.err != nil {
			return at.err
		}
		if !errors.Is(scanErr, context.Canceled) || scan.Complete || scan.Index().Len() >= tags || end < at.ticks {
			return fmt.Errorf("mid-scan cancellation failed: %v", scanErr)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		elapsed := float64(end-at.ticks) * 1000 / clock.frequency
		if iteration < 0 {
			result.ColdCancelMS = elapsed
		} else {
			cancels = append(cancels, elapsed)
		}
	}
	result.ScanMS = stats(durations)
	result.AllocatedBytes = stats(allocBytes)
	result.Allocations = stats(allocs)
	result.RetainedHeapBytes = stats(heaps)
	result.WorkingSetBytes = stats(workingSets)
	result.CancelMS = stats(cancels)
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}
