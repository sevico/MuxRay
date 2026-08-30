package mp3

// Severity is the machine-stable severity of an MP3 diagnostic.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	CodeFrameHeaderTruncated      = "MP3_FRAME_HEADER_TRUNCATED"
	CodeSyncInvalid               = "MP3_SYNC_INVALID"
	CodeVersionReserved           = "MP3_VERSION_RESERVED"
	CodeLayerReserved             = "MP3_LAYER_RESERVED"
	CodeLayerNotIII               = "MP3_LAYER_NOT_III"
	CodeBitrateIndexInvalid       = "MP3_BITRATE_INDEX_INVALID"
	CodeSamplingFrequencyReserved = "MP3_SAMPLING_FREQUENCY_RESERVED"
	CodeFreeFormatUnresolved      = "MP3_FREE_FORMAT_UNRESOLVED"
	CodeEmphasisReserved          = "MP3_EMPHASIS_RESERVED"
	CodeDiagnosticLimitReached    = "MP3_DIAGNOSTIC_LIMIT_REACHED"
)

// Diagnostic is deterministic and directly mappable to public reports.
// RecoveryOffset is the enclosing SoundData end after a fatal condition.
type Diagnostic struct {
	Code           string
	Severity       Severity
	Offset         uint64
	BitOffset      uint8
	Message        string
	Expected       *uint64
	Actual         *uint64
	RecoveryOffset *uint64
}

func number(value uint64) *uint64 { return &value }

func hasError(items []Diagnostic) bool {
	for _, item := range items {
		if item.Severity == SeverityError {
			return true
		}
	}
	return false
}

type diagnosticCollector struct {
	items          []Diagnostic
	limit          uint32
	truncated      bool
	recoveryOffset *uint64
}

func newDiagnosticCollector(limit uint32, recoveryOffset uint64) *diagnosticCollector {
	return &diagnosticCollector{limit: limit, recoveryOffset: number(recoveryOffset)}
}

func (collector *diagnosticCollector) add(item Diagnostic) {
	if uint32(len(collector.items)) < collector.limit {
		collector.items = append(collector.items, item)
		return
	}
	if collector.limit == 0 || collector.truncated || len(collector.items) == 0 {
		return
	}
	collector.truncated = true
	index := len(collector.items) - 1
	collector.items[index] = Diagnostic{
		Code: CodeDiagnosticLimitReached, Severity: SeverityError,
		Offset: item.Offset, BitOffset: item.BitOffset,
		Message:        "diagnostic limit reached; additional diagnostics were omitted",
		RecoveryOffset: item.RecoveryOffset,
	}
	if collector.items[index].RecoveryOffset == nil {
		collector.items[index].RecoveryOffset = collector.recoveryOffset
	}
	if item.Severity == SeverityError && index > 0 {
		collector.items[index-1] = item
	}
}
