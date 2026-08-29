package avc

// Severity is the machine-stable severity of an AVC diagnostic.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	CodeRecordHeaderTruncated          = "AVC_CONFIGURATION_RECORD_HEADER_TRUNCATED"
	CodeConfigurationVersionUnexpected = "AVC_CONFIGURATION_VERSION_UNEXPECTED"
	CodeReservedBitsUnexpected         = "AVC_RESERVED_BITS_UNEXPECTED"
	CodeLengthSizeReserved             = "AVC_LENGTH_SIZE_RESERVED"
	CodeConfigurationBitDepthInvalid   = "AVC_CONFIGURATION_BIT_DEPTH_INVALID"
	CodeParameterSetCountTruncated     = "AVC_PARAMETER_SET_COUNT_TRUNCATED"
	CodeParameterSetLengthTruncated    = "AVC_PARAMETER_SET_LENGTH_TRUNCATED"
	CodeParameterSetDataTruncated      = "AVC_PARAMETER_SET_DATA_TRUNCATED"
	CodeParameterSetLimitReached       = "AVC_PARAMETER_SET_LIMIT_REACHED"
	CodeParameterSetTooLarge           = "AVC_PARAMETER_SET_TOO_LARGE"
	CodeParameterSetNALTypeUnexpected  = "AVC_PARAMETER_SET_NAL_TYPE_UNEXPECTED"
	CodeHighProfileExtensionTruncated  = "AVC_HIGH_PROFILE_EXTENSION_TRUNCATED"

	CodeNALLengthTruncated  = "AVC_NAL_LENGTH_TRUNCATED"
	CodeNALPayloadTruncated = "AVC_NAL_PAYLOAD_TRUNCATED"
	CodeNALUnitLimitReached = "AVC_NAL_UNIT_LIMIT_REACHED"
	CodeNALUnitZeroLength   = "AVC_NAL_UNIT_ZERO_LENGTH"
	CodeNALHeaderTruncated  = "AVC_NAL_HEADER_TRUNCATED"
	CodeForbiddenZeroBitSet = "AVC_FORBIDDEN_ZERO_BIT_SET"

	CodeEmulationPreventionInvalid = "AVC_EMULATION_PREVENTION_INVALID"
	CodeRBSPMappingLimitReached    = "AVC_RBSP_MAPPING_LIMIT_REACHED"
	CodeRBSPSyntaxTruncated        = "AVC_RBSP_SYNTAX_TRUNCATED"
	CodeExpGolombOverflow          = "AVC_EXP_GOLOMB_OVERFLOW"
	CodeSPSIDOutOfRange            = "AVC_SPS_ID_OUT_OF_RANGE"
	CodePPSIDOutOfRange            = "AVC_PPS_ID_OUT_OF_RANGE"
	CodePPSReferencedSPSOutOfRange = "AVC_PPS_REFERENCED_SPS_ID_OUT_OF_RANGE"
	CodeSPSReservedBitsUnexpected  = "AVC_SPS_RESERVED_BITS_UNEXPECTED"
	CodeSPSChromaFormatInvalid     = "AVC_SPS_CHROMA_FORMAT_INVALID"
	CodeSPSBitDepthInvalid         = "AVC_SPS_BIT_DEPTH_INVALID"
	CodeSPSLog2MaxFrameNumInvalid  = "AVC_SPS_LOG2_MAX_FRAME_NUM_INVALID"
	CodeSPSLog2MaxPicOrderInvalid  = "AVC_SPS_LOG2_MAX_PIC_ORDER_CNT_INVALID"
	CodeSPSPicOrderCntTypeInvalid  = "AVC_SPS_PIC_ORDER_CNT_TYPE_INVALID"
	CodeSPSReferenceCycleTooLarge  = "AVC_SPS_REFERENCE_CYCLE_TOO_LARGE"
	CodeSPSCropInvalid             = "AVC_SPS_CROP_INVALID"

	CodeDiagnosticLimitReached = "AVC_DIAGNOSTIC_LIMIT_REACHED"
)

// Diagnostic is deterministic and directly mappable to public reports.
// RecoveryOffset is the absolute byte at which the enclosing bounded payload
// can safely resume after an error.
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

func newDiagnosticCollector(limit uint32, recoveryOffset ...uint64) *diagnosticCollector {
	collector := &diagnosticCollector{limit: limit}
	if len(recoveryOffset) != 0 {
		collector.recoveryOffset = number(recoveryOffset[0])
	}
	return collector
}

func (c *diagnosticCollector) add(item Diagnostic) {
	if uint32(len(c.items)) < c.limit {
		c.items = append(c.items, item)
		return
	}
	if c.limit == 0 || c.truncated || len(c.items) == 0 {
		return
	}
	c.truncated = true
	index := len(c.items) - 1
	c.items[index] = Diagnostic{
		Code: CodeDiagnosticLimitReached, Severity: SeverityError,
		Offset: item.Offset, BitOffset: item.BitOffset,
		Message:        "diagnostic limit reached; additional diagnostics were omitted",
		RecoveryOffset: item.RecoveryOffset,
	}
	if c.items[index].RecoveryOffset == nil {
		c.items[index].RecoveryOffset = c.recoveryOffset
	}
	if item.Severity == SeverityError && index > 0 {
		c.items[index-1] = item
	}
}
