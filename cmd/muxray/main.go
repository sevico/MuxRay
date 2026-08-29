package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/report"
)

const (
	exitOK          = 0
	exitFailure     = 1
	exitDiagnostics = 2
	exitUsage       = 64
)

var version = "dev"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		writeUsage(stderr)
		return exitUsage
	}

	switch args[0] {
	case "help", "-h", "--help":
		writeUsage(stdout)
		return exitOK
	case "version", "--version":
		_, _ = fmt.Fprintf(stdout, "muxray %s\n", version)
		return exitOK
	case "inspect":
		return runInspect(args[1:], stdout, stderr)
	case "detail":
		return runDetail(args[1:], stdout, stderr)
	default:
		_, _ = fmt.Fprintf(stderr, "muxray: unknown command %q\n", args[0])
		writeUsage(stderr)
		return exitUsage
	}
}

func runDetail(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("muxray detail", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pretty := flags.Bool("pretty", false, "indent JSON output")
	tagID := flags.Uint64("tag", ^uint64(0), "zero-based tag ID to parse (required)")
	maxTags := flags.Uint64("max-tags", 0, fmt.Sprintf("maximum tags to index (0 uses %d; hard maximum %d)", flv.DefaultMaxTags, flv.HardMaxTags))
	maxDiagnostics := flags.Uint64("max-diagnostics", 0, fmt.Sprintf("maximum scan diagnostics to retain (0 uses %d; range %d-%d)", flv.DefaultMaxDiagnostics, flv.MinDiagnostics, flv.HardMaxDiagnostics))
	maxAMFDepth := flags.Uint64("max-amf-depth", 0, fmt.Sprintf("maximum AMF0 nesting depth (0 uses %d; hard maximum %d)", amf0.DefaultMaxDepth, amf0.HardMaxDepth))
	maxAMFNodes := flags.Uint64("max-amf-nodes", 0, fmt.Sprintf("maximum AMF0 value nodes (0 uses %d; hard maximum %d)", amf0.DefaultMaxNodes, amf0.HardMaxNodes))
	maxAMFEntries := flags.Uint64("max-amf-entries", 0, fmt.Sprintf("maximum entries in one AMF0 container (0 uses %d; hard maximum %d)", amf0.DefaultMaxContainerEntries, amf0.HardMaxContainerEntries))
	maxAMFStringBytes := flags.Uint64("max-amf-string-bytes", 0, fmt.Sprintf("maximum retained bytes for one AMF0 string (0 uses %d; hard maximum %d)", amf0.DefaultMaxStringBytes, amf0.HardMaxStringBytes))
	maxAMFTotalStringBytes := flags.Uint64("max-amf-total-string-bytes", 0, fmt.Sprintf("maximum retained AMF0 string bytes (0 uses %d; hard maximum %d)", amf0.DefaultMaxTotalStringBytes, amf0.HardMaxTotalStringBytes))
	maxAMFDiagnostics := flags.Uint64("max-amf-diagnostics", 0, fmt.Sprintf("maximum AMF0 diagnostics (0 uses %d; range %d-%d)", amf0.DefaultMaxDiagnostics, amf0.MinDiagnostics, amf0.HardMaxDiagnostics))
	maxAACDiagnostics := flags.Uint64("max-aac-diagnostics", 0, fmt.Sprintf("maximum AAC diagnostics (0 uses %d; range %d-%d)", aac.DefaultMaxDiagnostics, aac.MinDiagnostics, aac.HardMaxDiagnostics))
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: muxray detail --tag <id> [options] <file.flv>")
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if flags.NArg() != 1 || *tagID == ^uint64(0) {
		flags.Usage()
		return exitUsage
	}
	if *tagID >= flv.HardMaxTags {
		_, _ = fmt.Fprintf(stderr, "muxray: --tag must be less than %d\n", flv.HardMaxTags)
		return exitUsage
	}
	if *maxTags > flv.HardMaxTags {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-tags exceeds hard maximum %d\n", flv.HardMaxTags)
		return exitUsage
	}
	if *maxDiagnostics > uint64(flv.HardMaxDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics exceeds hard maximum %d\n", flv.HardMaxDiagnostics)
		return exitUsage
	}
	if *maxDiagnostics != 0 && *maxDiagnostics < uint64(flv.MinDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics is below minimum %d\n", flv.MinDiagnostics)
		return exitUsage
	}

	amfOptions, err := makeAMFOptions(
		*maxAMFDepth,
		*maxAMFNodes,
		*maxAMFEntries,
		*maxAMFStringBytes,
		*maxAMFTotalStringBytes,
		*maxAMFDiagnostics,
	)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: invalid AMF0 options: %v\n", err)
		return exitUsage
	}
	aacOptions, err := makeAACOptions(*maxAACDiagnostics)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: invalid AAC options: %v\n", err)
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

	result, err := flv.Scan(file, uint64(info.Size()), flv.Options{
		MaxTags: *maxTags, MaxDiagnostics: uint32(*maxDiagnostics),
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: scan input: %v\n", err)
		return exitFailure
	}
	if !result.Complete || result.HasErrors() {
		_, _ = fmt.Fprintf(stderr, "muxray: cannot produce tag detail from an incomplete FLV index (%d scan diagnostics); run inspect for details\n", len(result.Diagnostics))
		return exitDiagnostics
	}
	if *tagID >= uint64(len(result.Tags)) {
		_, _ = fmt.Fprintf(stderr, "muxray: tag %d was not indexed (indexed tags: %d)\n", *tagID, len(result.Tags))
		return exitDiagnostics
	}

	tag := result.Tags[*tagID]
	detail, err := flv.ParseTagDetail(file, tag, flv.DetailOptions{AMF0: amfOptions, AAC: aacOptions})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: parse tag %d detail: %v\n", *tagID, err)
		if isDetailSelectionError(err) {
			return exitDiagnostics
		}
		return exitFailure
	}
	document, err := report.NewTagDetail(uint64(info.Size()), tag, detail)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: map tag %d detail report: %v\n", *tagID, err)
		return exitFailure
	}
	if err := report.WriteParsedTagDetailJSON(stdout, document, *pretty); err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: write detail report: %v\n", err)
		return exitFailure
	}
	if !detail.IsComplete() {
		return exitDiagnostics
	}
	return exitOK
}

func makeAACOptions(diagnostics uint64) (aac.Options, error) {
	if diagnostics > uint64(aac.HardMaxDiagnostics) {
		return aac.Options{}, fmt.Errorf("max diagnostics exceeds package hard limit %d", aac.HardMaxDiagnostics)
	}
	return (aac.Options{MaxDiagnostics: uint32(diagnostics)}).Normalized()
}

func isDetailSelectionError(err error) bool {
	return errors.Is(err, flv.ErrTagDetailUnsupported) ||
		errors.Is(err, flv.ErrScriptDataWrongTagType) ||
		errors.Is(err, flv.ErrScriptDataFiltered) ||
		errors.Is(err, flv.ErrScriptDataPayloadIncomplete) ||
		errors.Is(err, flv.ErrAACAudioWrongTagType) ||
		errors.Is(err, flv.ErrAACAudioFiltered) ||
		errors.Is(err, flv.ErrAACAudioPayloadIncomplete) ||
		errors.Is(err, flv.ErrAACAudioWrongSoundFormat)
}

func makeAMFOptions(depth, nodes, entries, stringBytes, totalStringBytes, diagnostics uint64) (amf0.Options, error) {
	if depth > uint64(amf0.HardMaxDepth) || nodes > uint64(amf0.HardMaxNodes) ||
		entries > uint64(amf0.HardMaxContainerEntries) || stringBytes > uint64(amf0.HardMaxStringBytes) ||
		totalStringBytes > amf0.HardMaxTotalStringBytes || diagnostics > uint64(amf0.HardMaxDiagnostics) {
		return amf0.Options{}, fmt.Errorf("one or more values exceed package hard limits")
	}
	options := amf0.Options{
		MaxDepth:            uint32(depth),
		MaxNodes:            uint32(nodes),
		MaxContainerEntries: uint32(entries),
		MaxStringBytes:      uint32(stringBytes),
		MaxTotalStringBytes: totalStringBytes,
		MaxDiagnostics:      uint32(diagnostics),
	}
	return options.Normalized()
}

func runInspect(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("muxray inspect", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pretty := flags.Bool("pretty", false, "indent JSON output")
	maxTags := flags.Uint64("max-tags", 0, fmt.Sprintf("maximum tags to index (0 uses %d; hard maximum %d)", flv.DefaultMaxTags, flv.HardMaxTags))
	maxDiagnostics := flags.Uint64("max-diagnostics", 0, fmt.Sprintf("maximum diagnostics to retain (0 uses %d; range %d-%d)", flv.DefaultMaxDiagnostics, flv.MinDiagnostics, flv.HardMaxDiagnostics))
	flags.Usage = func() {
		_, _ = fmt.Fprintln(stderr, "Usage: muxray inspect [options] <file.flv>")
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
	if *maxTags > flv.HardMaxTags {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-tags exceeds hard maximum %d\n", flv.HardMaxTags)
		return exitUsage
	}
	if *maxDiagnostics > uint64(flv.HardMaxDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics exceeds hard maximum %d\n", flv.HardMaxDiagnostics)
		return exitUsage
	}
	if *maxDiagnostics != 0 && *maxDiagnostics < uint64(flv.MinDiagnostics) {
		_, _ = fmt.Fprintf(stderr, "muxray: --max-diagnostics is below minimum %d\n", flv.MinDiagnostics)
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

	result, err := flv.Scan(file, uint64(info.Size()), flv.Options{
		MaxTags: *maxTags, MaxDiagnostics: uint32(*maxDiagnostics),
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: scan input: %v\n", err)
		return exitFailure
	}
	if err := report.WriteScanJSON(stdout, result, *pretty); err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: write report: %v\n", err)
		return exitFailure
	}
	if result.HasErrors() {
		return exitDiagnostics
	}
	return exitOK
}

func writeUsage(output io.Writer) {
	_, _ = fmt.Fprintln(output, "Usage: muxray <command> [arguments]")
	_, _ = fmt.Fprintln(output, "")
	_, _ = fmt.Fprintln(output, "Commands:")
	_, _ = fmt.Fprintln(output, "  inspect    scan an FLV file and emit schemaVersion 1 JSON")
	_, _ = fmt.Fprintln(output, "  detail     parse one supported indexed tag and emit versioned detail JSON")
	_, _ = fmt.Fprintln(output, "  version    print the build version")
}
