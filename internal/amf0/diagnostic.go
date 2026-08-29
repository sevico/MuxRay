package amf0

// Severity is the machine-stable severity of an AMF0 diagnostic.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	CodeMarkerTruncated             = "AMF0_MARKER_TRUNCATED"
	CodeMarkerUnknown               = "AMF0_MARKER_UNKNOWN"
	CodeTypeUnsupported             = "AMF0_TYPE_UNSUPPORTED"
	CodeObjectEndUnexpected         = "AMF0_OBJECT_END_UNEXPECTED"
	CodeNumberTruncated             = "AMF0_NUMBER_TRUNCATED"
	CodeBooleanTruncated            = "AMF0_BOOLEAN_TRUNCATED"
	CodeBooleanNonCanonical         = "AMF0_BOOLEAN_NONCANONICAL"
	CodeStringLengthTruncated       = "AMF0_STRING_LENGTH_TRUNCATED"
	CodeStringDataTruncated         = "AMF0_STRING_DATA_TRUNCATED"
	CodeStringLimitExceeded         = "AMF0_STRING_LIMIT_EXCEEDED"
	CodeStringBudgetExceeded        = "AMF0_STRING_BUDGET_EXCEEDED"
	CodeUTF8Invalid                 = "AMF0_UTF8_INVALID"
	CodeObjectKeyLengthTruncated    = "AMF0_OBJECT_KEY_LENGTH_TRUNCATED"
	CodeObjectKeyDataTruncated      = "AMF0_OBJECT_KEY_DATA_TRUNCATED"
	CodeObjectEndMissing            = "AMF0_OBJECT_END_MISSING"
	CodeArrayCountTruncated         = "AMF0_ARRAY_COUNT_TRUNCATED"
	CodeECMAArrayCountMismatch      = "AMF0_ECMA_ARRAY_COUNT_MISMATCH"
	CodeReferenceTruncated          = "AMF0_REFERENCE_TRUNCATED"
	CodeReferenceOutOfRange         = "AMF0_REFERENCE_OUT_OF_RANGE"
	CodeDateMillisecondsTruncated   = "AMF0_DATE_MILLISECONDS_TRUNCATED"
	CodeDateTimezoneTruncated       = "AMF0_DATE_TIMEZONE_TRUNCATED"
	CodeDateTimezoneNonZero         = "AMF0_DATE_TIMEZONE_NONZERO"
	CodeTypedClassLengthTruncated   = "AMF0_TYPED_CLASS_LENGTH_TRUNCATED"
	CodeTypedClassDataTruncated     = "AMF0_TYPED_CLASS_DATA_TRUNCATED"
	CodeDepthLimitExceeded          = "AMF0_DEPTH_LIMIT_EXCEEDED"
	CodeNodeLimitExceeded           = "AMF0_NODE_LIMIT_EXCEEDED"
	CodeContainerEntryLimitExceeded = "AMF0_CONTAINER_ENTRY_LIMIT_EXCEEDED"
	CodeDiagnosticLimitReached      = "AMF0_DIAGNOSTIC_LIMIT_REACHED"
)

// Diagnostic is deterministic and directly mappable to public reports.
// RecoveryOffset is the byte offset at which the outer container can safely
// resume after a fatal AMF0 condition.
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

type diagnosticCollector struct {
	items     []Diagnostic
	limit     uint32
	truncated bool
}

func newDiagnosticCollector(limit uint32) *diagnosticCollector {
	return &diagnosticCollector{limit: limit}
}

func (c *diagnosticCollector) add(diagnostic Diagnostic) {
	if uint32(len(c.items)) < c.limit {
		c.items = append(c.items, diagnostic)
		return
	}
	if c.limit == 0 {
		return
	}
	if !c.truncated {
		c.truncated = true
		c.items[len(c.items)-1] = Diagnostic{
			Code:      CodeDiagnosticLimitReached,
			Severity:  SeverityError,
			Offset:    diagnostic.Offset,
			BitOffset: diagnostic.BitOffset,
			Message:   "diagnostic limit reached; additional diagnostics were omitted",
		}
	}
	if diagnostic.RecoveryOffset == nil {
		return
	}

	// If warnings exhausted the budget before the fatal condition, preserve
	// both the fatal cause and the fact that diagnostics were truncated. The
	// package minimum of two slots guarantees room for that pair.
	markerIndex := len(c.items) - 1
	if markerIndex > 0 {
		c.items[markerIndex-1] = diagnostic
	}
	c.items[markerIndex].Offset = diagnostic.Offset
	c.items[markerIndex].BitOffset = diagnostic.BitOffset
	c.items[markerIndex].RecoveryOffset = diagnostic.RecoveryOffset
}
