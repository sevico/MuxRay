package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/report"
)

func runSummary(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("muxray summary", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pretty := flags.Bool("pretty", false, "indent JSON output")
	schemaVersion := flags.Uint64("schema-version", 1, "summary schema version: 1 (AAC/AVC) or 2 (AAC/AVC/MP3)")
	maxMP3HeaderEvents := flags.Uint64("max-mp3-header-events", 0, fmt.Sprintf("v2 only: maximum MP3 header events retained (0 uses %d; hard maximum %d)", analysis.DefaultMaxMP3HeaderEvents, analysis.HardMaxMP3HeaderEvents))
	maxTags := flags.Uint64("max-tags", 0, fmt.Sprintf("maximum tags to index (0 uses %d; hard maximum %d)", flv.DefaultMaxTags, flv.HardMaxTags))
	maxScanDiagnostics := flags.Uint64("max-diagnostics", 0, fmt.Sprintf("maximum scan diagnostics to retain (0 uses %d; range %d-%d)", flv.DefaultMaxDiagnostics, flv.MinDiagnostics, flv.HardMaxDiagnostics))
	maxKeyframes := flags.Uint64("max-keyframes", 0, fmt.Sprintf("maximum keyframe index entries to retain (0 uses %d; hard maximum %d)", analysis.DefaultMaxKeyframes, analysis.HardMaxKeyframes))
	maxConfigurationEvents := flags.Uint64("max-configuration-events", 0, fmt.Sprintf("maximum AAC/AVC configuration events retained per codec (0 uses %d; hard maximum %d)", analysis.DefaultMaxConfigurationEvents, analysis.HardMaxConfigurationEvents))
	maxConfigurationBytes := flags.Uint64("max-configuration-bytes", 0, fmt.Sprintf("maximum cumulative AAC/AVC configuration bytes to parse (0 uses %d; hard maximum %d)", analysis.DefaultMaxConfigurationBytes, analysis.HardMaxConfigurationBytes))
	maxAnalysisDiagnostics := flags.Uint64("max-analysis-diagnostics", 0, fmt.Sprintf("maximum media-analysis diagnostics (0 uses %d; range %d-%d)", analysis.DefaultMaxDiagnostics, analysis.MinDiagnostics, analysis.HardMaxDiagnostics))
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: muxray summary [options] <file.flv>")
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
	if *schemaVersion != 1 && *schemaVersion != 2 {
		_, _ = fmt.Fprintln(stderr, "muxray: --schema-version must be 1 or 2")
		return exitUsage
	}
	mp3LimitSet := false
	flags.Visit(func(option *flag.Flag) {
		if option.Name == "max-mp3-header-events" {
			mp3LimitSet = true
		}
	})
	if mp3LimitSet && *schemaVersion != 2 {
		_, _ = fmt.Fprintln(stderr, "muxray: --max-mp3-header-events requires --schema-version 2")
		return exitUsage
	}
	if *maxMP3HeaderEvents > uint64(analysis.HardMaxMP3HeaderEvents) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-mp3-header-events exceeds hard maximum %d\n", analysis.HardMaxMP3HeaderEvents)
		return exitUsage
	}
	if *maxTags > flv.HardMaxTags {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-tags exceeds hard maximum %d\n", flv.HardMaxTags)
		return exitUsage
	}
	if *maxScanDiagnostics > uint64(flv.HardMaxDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics exceeds hard maximum %d\n", flv.HardMaxDiagnostics)
		return exitUsage
	}
	if *maxScanDiagnostics != 0 && *maxScanDiagnostics < uint64(flv.MinDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics is below minimum %d\n", flv.MinDiagnostics)
		return exitUsage
	}
	if *maxKeyframes > uint64(analysis.HardMaxKeyframes) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-keyframes exceeds hard maximum %d\n", analysis.HardMaxKeyframes)
		return exitUsage
	}
	if *maxConfigurationEvents > uint64(analysis.HardMaxConfigurationEvents) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-configuration-events exceeds hard maximum %d\n", analysis.HardMaxConfigurationEvents)
		return exitUsage
	}
	if *maxConfigurationBytes > analysis.HardMaxConfigurationBytes {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-configuration-bytes exceeds hard maximum %d\n", analysis.HardMaxConfigurationBytes)
		return exitUsage
	}
	if *maxAnalysisDiagnostics > uint64(analysis.HardMaxDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-analysis-diagnostics exceeds hard maximum %d\n", analysis.HardMaxDiagnostics)
		return exitUsage
	}
	if *maxAnalysisDiagnostics != 0 && *maxAnalysisDiagnostics < uint64(analysis.MinDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-analysis-diagnostics is below minimum %d\n", analysis.MinDiagnostics)
		return exitUsage
	}
	analysisOptions, err := (analysis.Options{
		MaxKeyframes:           uint32(*maxKeyframes),
		MaxConfigurationEvents: uint32(*maxConfigurationEvents),
		MaxConfigurationBytes:  *maxConfigurationBytes,
		MaxDiagnostics:         uint32(*maxAnalysisDiagnostics),
	}).Normalized()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: invalid media-analysis options: %v\n", err)
		return exitUsage
	}

	path := flags.Arg(0)
	file, err := os.Open(path)
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

	scan, err := flv.Scan(file, uint64(info.Size()), flv.Options{
		MaxTags: *maxTags, MaxDiagnostics: uint32(*maxScanDiagnostics),
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: scan input: %v\n", err)
		return exitFailure
	}
	if *schemaVersion == 2 {
		summary, err := analysis.AnalyzeV2(context.Background(), file, scan, analysis.OptionsV2{
			Options: analysisOptions, MaxMP3HeaderEvents: uint32(*maxMP3HeaderEvents),
		})
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "muxray: analyze input: %v\n", err)
			return exitFailure
		}
		if err := report.WriteMediaSummaryV2JSON(stdout, report.NewMediaSummaryV2(scan, summary), *pretty); err != nil {
			_, _ = fmt.Fprintf(stderr, "muxray: write media summary: %v\n", err)
			return exitFailure
		}
		if scan.HasErrors() || summary.HasErrors() {
			return exitDiagnostics
		}
		return exitOK
	}
	summary, err := analysis.Analyze(context.Background(), file, scan, analysisOptions)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: analyze input: %v\n", err)
		return exitFailure
	}
	document := report.NewMediaSummary(scan, summary)
	if err := report.WriteMediaSummaryJSON(stdout, document, *pretty); err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: write media summary: %v\n", err)
		return exitFailure
	}
	if scan.HasErrors() || summary.HasErrors() {
		return exitDiagnostics
	}
	return exitOK
}
