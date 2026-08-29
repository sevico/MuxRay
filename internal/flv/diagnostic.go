package flv

// Severity is the machine-stable severity of a diagnostic.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	CodeHeaderTruncated           = "FLV_HEADER_TRUNCATED"
	CodeSignatureInvalid          = "FLV_SIGNATURE_INVALID"
	CodeVersionUnsupported        = "FLV_VERSION_UNSUPPORTED"
	CodeTypeFlagsReserved         = "FLV_TYPE_FLAGS_RESERVED"
	CodeDataOffsetInvalid         = "FLV_DATA_OFFSET_INVALID"
	CodeDataOffsetOutOfBounds     = "FLV_DATA_OFFSET_OUT_OF_BOUNDS"
	CodePreviousTagSize0Truncated = "FLV_PREVIOUS_TAG_SIZE0_TRUNCATED"
	CodePreviousTagSize0NonZero   = "FLV_PREVIOUS_TAG_SIZE0_NONZERO"
	CodeTagHeaderTruncated        = "FLV_TAG_HEADER_TRUNCATED"
	CodeTagTypeUnknown            = "FLV_TAG_TYPE_UNKNOWN"
	CodeTagReservedBitsNonZero    = "FLV_TAG_RESERVED_BITS_NONZERO"
	CodeTagFilterUnsupported      = "FLV_TAG_FILTER_UNSUPPORTED"
	CodeStreamIDNonZero           = "FLV_STREAM_ID_NONZERO"
	CodeTagBodyTruncated          = "FLV_TAG_BODY_TRUNCATED"
	CodePreviousTagSizeTruncated  = "FLV_PREVIOUS_TAG_SIZE_TRUNCATED"
	CodePreviousTagSizeMismatch   = "FLV_PREVIOUS_TAG_SIZE_MISMATCH"
	CodeTagLimitExceeded          = "FLV_TAG_LIMIT_EXCEEDED"
	CodeDiagnosticLimitReached    = "FLV_DIAGNOSTIC_LIMIT_REACHED"
)

// Diagnostic is safe to persist in Golden output. Code is the compatibility
// contract; Message is explanatory text for humans.
type Diagnostic struct {
	Code      string
	Severity  Severity
	Offset    uint64
	BitOffset uint8
	Message   string
	Expected  *uint64
	Actual    *uint64
}

func number(value uint64) *uint64 {
	return &value
}

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
	// A one-entry collector is not exposed by Scan (Options requires at least
	// two), but keep its internal behavior safe by promoting a structural error.
	if c.limit == 1 {
		if diagnostic.Severity == SeverityError && c.items[0].Severity != SeverityError {
			c.items[0] = diagnostic
		}
		c.truncated = true
		return
	}
	if c.truncated {
		return
	}

	c.truncated = true
	marker := Diagnostic{
		Code:     CodeDiagnosticLimitReached,
		Severity: SeverityError,
		Offset:   diagnostic.Offset,
		Message:  "diagnostic limit reached; additional diagnostics were omitted",
	}
	// Preserve the hard limit by replacing only the final retained diagnostic.
	c.items[len(c.items)-1] = marker
}
