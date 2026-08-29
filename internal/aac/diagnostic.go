package aac

// Severity is the machine-stable severity of an AAC diagnostic.
type Severity string

const (
	SeverityWarning Severity = "warning"
	SeverityError   Severity = "error"
)

const (
	CodeAudioObjectTypeTruncated       = "AAC_AUDIO_OBJECT_TYPE_TRUNCATED"
	CodeAudioObjectTypeEscapeTruncated = "AAC_AUDIO_OBJECT_TYPE_ESCAPE_TRUNCATED"
	CodeAudioObjectTypeReserved        = "AAC_AUDIO_OBJECT_TYPE_RESERVED"
	CodeAudioObjectTypeUnsupported     = "AAC_AUDIO_OBJECT_TYPE_UNSUPPORTED"

	CodeSamplingFrequencyIndexTruncated = "AAC_SAMPLING_FREQUENCY_INDEX_TRUNCATED"
	CodeSamplingFrequencyIndexReserved  = "AAC_SAMPLING_FREQUENCY_INDEX_RESERVED"
	CodeSamplingFrequencyTruncated      = "AAC_SAMPLING_FREQUENCY_TRUNCATED"
	CodeSamplingFrequencyZero           = "AAC_SAMPLING_FREQUENCY_ZERO"
	CodeChannelConfigurationTruncated   = "AAC_CHANNEL_CONFIGURATION_TRUNCATED"
	CodeChannelConfigurationReserved    = "AAC_CHANNEL_CONFIGURATION_RESERVED"
	CodeProgramConfigElementUnsupported = "AAC_PROGRAM_CONFIG_ELEMENT_UNSUPPORTED"

	CodeFrameLengthFlagTruncated           = "AAC_FRAME_LENGTH_FLAG_TRUNCATED"
	CodeDependsOnCoreCoderTruncated        = "AAC_DEPENDS_ON_CORE_CODER_TRUNCATED"
	CodeCoreCoderDelayTruncated            = "AAC_CORE_CODER_DELAY_TRUNCATED"
	CodeExtensionFlagTruncated             = "AAC_EXTENSION_FLAG_TRUNCATED"
	CodeLayerNrTruncated                   = "AAC_LAYER_NR_TRUNCATED"
	CodeNumOfSubFrameTruncated             = "AAC_NUM_OF_SUB_FRAME_TRUNCATED"
	CodeLayerLengthTruncated               = "AAC_LAYER_LENGTH_TRUNCATED"
	CodeSectionDataResilienceTruncated     = "AAC_SECTION_DATA_RESILIENCE_FLAG_TRUNCATED"
	CodeScalefactorDataResilienceTruncated = "AAC_SCALEFACTOR_DATA_RESILIENCE_FLAG_TRUNCATED"
	CodeSpectralDataResilienceTruncated    = "AAC_SPECTRAL_DATA_RESILIENCE_FLAG_TRUNCATED"
	CodeExtensionFlag3Truncated            = "AAC_EXTENSION_FLAG3_TRUNCATED"
	CodeExtensionFlag3Unsupported          = "AAC_EXTENSION_FLAG3_UNSUPPORTED"
	CodeEPConfigTruncated                  = "AAC_EP_CONFIG_TRUNCATED"
	CodeErrorProtectionConfigUnsupported   = "AAC_ERROR_PROTECTION_CONFIG_UNSUPPORTED"
	CodeDiagnosticLimitReached             = "AAC_DIAGNOSTIC_LIMIT_REACHED"
)

// Diagnostic is deterministic and directly mappable to public reports.
// RecoveryOffset is the byte offset at which the outer container can safely
// resume after a fatal AudioSpecificConfig condition.
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
	if c.limit == 0 || c.truncated {
		return
	}
	c.truncated = true
	markerIndex := len(c.items) - 1
	c.items[markerIndex] = Diagnostic{
		Code:           CodeDiagnosticLimitReached,
		Severity:       SeverityError,
		Offset:         diagnostic.Offset,
		BitOffset:      diagnostic.BitOffset,
		Message:        "diagnostic limit reached; additional diagnostics were omitted",
		RecoveryOffset: diagnostic.RecoveryOffset,
	}
	if diagnostic.RecoveryOffset != nil && markerIndex > 0 {
		c.items[markerIndex-1] = diagnostic
	}
}
