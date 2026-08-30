package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/avc"
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
	case "summary":
		return runSummary(args[1:], stdout, stderr)
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
	maxAVCDiagnostics := flags.Uint64("max-avc-diagnostics", 0, fmt.Sprintf("maximum AVC diagnostics (0 uses %d; range %d-%d)", avc.DefaultMaxDiagnostics, avc.MinDiagnostics, avc.HardMaxDiagnostics))
	maxAVCParameterSets := flags.Uint64("max-avc-parameter-sets", 0, fmt.Sprintf("maximum AVC parameter sets (0 uses %d; hard maximum %d)", avc.DefaultMaxParameterSets, avc.HardMaxParameterSets))
	maxAVCNALUnits := flags.Uint64("max-avc-nal-units", 0, fmt.Sprintf("maximum AVC NAL units (0 uses %d; hard maximum %d)", avc.DefaultMaxNALUnits, avc.HardMaxNALUnits))
	maxAVCParameterSetBytes := flags.Uint64("max-avc-parameter-set-bytes", 0, fmt.Sprintf("maximum bytes summarized for one AVC parameter-set NAL (0 uses %d; hard maximum %d)", avc.DefaultMaxParameterSetBytes, avc.HardMaxParameterSetBytes))
	maxAVCRBSPMappingEntries := flags.Uint64("max-avc-rbsp-mapping-entries", 0, fmt.Sprintf("maximum cumulative AVC RBSP source-map entries (0 uses %d; hard maximum %d)", avc.DefaultMaxRBSPMappingEntries, avc.HardMaxRBSPMappingEntries))
	avcNALLengthSize := flags.Uint64("avc-nal-length-size", 0, "AVC NAL length field bytes (0 auto; allowed 1, 2, or 4)")
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
	avcOptions, err := makeAVCOptions(*maxAVCDiagnostics, *maxAVCParameterSets, *maxAVCNALUnits, *maxAVCParameterSetBytes, *maxAVCRBSPMappingEntries, *avcNALLengthSize)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "muxray: invalid AVC options: %v\n", err)
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
	if tag.Type() == flv.TagTypeVideo && avcOptions.NALLengthSize == 0 {
		resolved, resolveErr := resolveAVCNALLengthSize(file, result.Tags, *tagID, avcOptions)
		if resolveErr != nil {
			_, _ = fmt.Fprintf(stderr, "muxray: resolve AVC NAL length size: %v\n", resolveErr)
			return exitFailure
		}
		avcOptions.NALLengthSize = resolved
	}
	detail, err := flv.ParseTagDetail(file, tag, flv.DetailOptions{AMF0: amfOptions, AAC: aacOptions, AVC: avcOptions})
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

func makeAVCOptions(diagnostics, parameterSets, nalUnits, parameterSetBytes, rbspMappingEntries, nalLengthSize uint64) (flv.AVCVideoOptions, error) {
	if diagnostics > uint64(avc.HardMaxDiagnostics) || parameterSets > uint64(avc.HardMaxParameterSets) ||
		nalUnits > uint64(avc.HardMaxNALUnits) || parameterSetBytes > uint64(avc.HardMaxParameterSetBytes) ||
		rbspMappingEntries > uint64(avc.HardMaxRBSPMappingEntries) {
		return flv.AVCVideoOptions{}, fmt.Errorf("one or more values exceed package hard limits")
	}
	if nalLengthSize != 0 && nalLengthSize != 1 && nalLengthSize != 2 && nalLengthSize != 4 {
		return flv.AVCVideoOptions{}, fmt.Errorf("NAL length size must be 0, 1, 2, or 4 bytes")
	}
	options, err := (avc.Options{
		MaxDiagnostics:        uint32(diagnostics),
		MaxParameterSets:      uint32(parameterSets),
		MaxNALUnits:           uint32(nalUnits),
		MaxParameterSetBytes:  uint32(parameterSetBytes),
		MaxRBSPMappingEntries: uint32(rbspMappingEntries),
	}).Normalized()
	if err != nil {
		return flv.AVCVideoOptions{}, err
	}
	return flv.AVCVideoOptions{AVC: options, NALLengthSize: uint8(nalLengthSize)}, nil
}

// resolveAVCNALLengthSize supplies the stream context omitted by an isolated
// AVC NALU Tag. It only parses sequence-header candidates and never guesses a
// default when no usable declaration exists.
func resolveAVCNALLengthSize(source io.ReaderAt, tags []flv.TagRecord, selected uint64, options flv.AVCVideoOptions) (uint8, error) {
	if selected >= uint64(len(tags)) {
		return 0, nil
	}
	header, packetType, present, err := readAVCFramingPrefix(source, tags[selected])
	if err != nil {
		return 0, err
	}
	if !present || header&0x0f != flv.CodecIDAVC || header>>4 == 5 || packetType != uint8(flv.AVCPacketTypeNALU) {
		return 0, nil
	}

	options.NALLengthSize = 0
	for index := int64(selected) - 1; index >= 0; index-- {
		candidate := tags[index]
		header, candidateType, candidatePresent, readErr := readAVCFramingPrefix(source, candidate)
		if readErr != nil {
			return 0, readErr
		}
		if !candidatePresent || header&0x0f != flv.CodecIDAVC || header>>4 == 5 || candidateType != uint8(flv.AVCPacketTypeSequenceHeader) {
			continue
		}
		parsed, parseErr := flv.ParseAVCVideo(source, candidate, options)
		if parseErr != nil {
			if isDetailSelectionError(parseErr) {
				continue
			}
			return 0, parseErr
		}
		if parsed.ConfigPresent && parsed.Config.FramingComplete && (parsed.Config.LengthSize == 1 || parsed.Config.LengthSize == 2 || parsed.Config.LengthSize == 4) {
			return parsed.Config.LengthSize, nil
		}
	}
	return 0, nil
}

func readAVCFramingPrefix(source io.ReaderAt, tag flv.TagRecord) (header uint8, packetType uint8, present bool, err error) {
	if tag.Type() != flv.TagTypeVideo || tag.Filter() || !tag.PayloadComplete() || tag.DataSize < 2 {
		return 0, 0, false, nil
	}
	var prefix [2]byte
	n, readErr := source.ReadAt(prefix[:], int64(tag.PayloadSpan().ByteOffset))
	if n == len(prefix) {
		return prefix[0], prefix[1], true, nil
	}
	if readErr != nil {
		return 0, 0, false, readErr
	}
	return 0, 0, false, io.ErrUnexpectedEOF
}

func isDetailSelectionError(err error) bool {
	return errors.Is(err, flv.ErrTagDetailUnsupported) ||
		errors.Is(err, flv.ErrScriptDataWrongTagType) ||
		errors.Is(err, flv.ErrScriptDataFiltered) ||
		errors.Is(err, flv.ErrScriptDataPayloadIncomplete) ||
		errors.Is(err, flv.ErrAACAudioWrongTagType) ||
		errors.Is(err, flv.ErrAACAudioFiltered) ||
		errors.Is(err, flv.ErrAACAudioPayloadIncomplete) ||
		errors.Is(err, flv.ErrAACAudioWrongSoundFormat) ||
		errors.Is(err, flv.ErrAVCVideoWrongTagType) ||
		errors.Is(err, flv.ErrAVCVideoFiltered) ||
		errors.Is(err, flv.ErrAVCVideoPayloadIncomplete) ||
		errors.Is(err, flv.ErrAVCVideoWrongCodecID) ||
		errors.Is(err, flv.ErrAVCVideoCommandFrame)
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
	_, _ = fmt.Fprintln(output, "  summary    derive bounded cross-tag media timing and codec summaries")
	_, _ = fmt.Fprintln(output, "  detail     parse one supported indexed tag and emit versioned detail JSON")
	_, _ = fmt.Fprintln(output, "  version    print the build version")
}
