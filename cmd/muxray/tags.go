package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/report"
)

func runTags(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("muxray tags", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pretty := flags.Bool("pretty", false, "indent JSON output")
	start := flags.Uint64("start", 0, "global tag ID at which to start the page")
	count := flags.Uint64("count", uint64(flv.DefaultPageSize), fmt.Sprintf("page size (1-%d)", flv.MaxPageSize))
	maxTags := flags.Uint64("max-tags", 0, fmt.Sprintf("maximum tags to index (0 uses %d; hard maximum %d)", flv.DefaultMaxTags, flv.HardMaxTags))
	maxDiagnostics := flags.Uint64("max-diagnostics", 0, fmt.Sprintf("maximum scan diagnostics (0 uses %d; range %d-%d)", flv.DefaultMaxDiagnostics, flv.MinDiagnostics, flv.HardMaxDiagnostics))
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: muxray tags [--start <id>] [--count <n>] [options] <file.flv>")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return exitUsage
	}
	if *count == 0 || *count > uint64(flv.MaxPageSize) {
		_, _ = fmt.Fprintf(stderr, "muxray: --count must be between 1 and %d\n", flv.MaxPageSize)
		return exitUsage
	}
	if *start > flv.HardMaxTags || *maxTags > flv.HardMaxTags {
		_, _ = fmt.Fprintf(stderr, "muxray: --start and --max-tags cannot exceed %d\n", flv.HardMaxTags)
		return exitUsage
	}
	if *maxDiagnostics > uint64(flv.HardMaxDiagnostics) || (*maxDiagnostics != 0 && *maxDiagnostics < uint64(flv.MinDiagnostics)) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics must be 0 or between %d and %d\n", flv.MinDiagnostics, flv.HardMaxDiagnostics)
		return exitUsage
	}
	file, err := os.Open(flags.Arg(0))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: open input: %v\n", err)
		return exitFailure
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: stat input: %v\n", err)
		return exitFailure
	}
	if info.Size() < 0 {
		_, _ = fmt.Fprintln(stderr, "muxray: input reports a negative size")
		return exitFailure
	}
	scan, err := flv.ScanContext(ctx, file, uint64(info.Size()), flv.Options{MaxTags: *maxTags, MaxDiagnostics: uint32(*maxDiagnostics)})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: scan input: %v\n", err)
		return exitFailure
	}
	document, err := report.NewTagPage(scan, *start, uint32(*count))
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: query tags: %v\n", err)
		return exitUsage
	}
	if err := report.WriteTagPageJSON(stdout, document, *pretty); err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: write tag page: %v\n", err)
		return exitFailure
	}
	if scan.HasErrors() {
		return exitDiagnostics
	}
	return exitOK
}
