package report

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	AACTagDetailSchemaVersion = 1
	AACTagDetailDocumentType  = "flv-aac-tag-detail"
	AACAudioKind              = "aacAudio"
)

var ErrUnsupportedTagDetail = errors.New("report: unsupported parsed tag detail")

// ParsedTagDetailDocument is the strongly typed report union produced by the
// generic FLV detail dispatcher. Each member retains an independently
// versioned JSON contract.
type ParsedTagDetailDocument interface {
	parsedTagDetailDocument()
}

func (TagDetailDocument) parsedTagDetailDocument() {}

// AACTagDetailDocument is the bounded report for one FLV AAC audio tag. Its
// contract is intentionally independent from the existing AMF0 detail v1
// document so adding AAC cannot invalidate a pinned AMF0 validator.
type AACTagDetailDocument struct {
	SchemaVersion int                   `json:"schemaVersion"`
	DocumentType  string                `json:"documentType"`
	Source        Source                `json:"source"`
	Tag           Tag                   `json:"tag"`
	Detail        AACAudioDetail        `json:"detail"`
	Diagnostics   []TagDetailDiagnostic `json:"diagnostics"`
}

func (AACTagDetailDocument) parsedTagDetailDocument() {}

type AACAudioDetail struct {
	Kind                string                     `json:"kind"`
	Span                Span                       `json:"span"`
	BitsConsumed        uint64                     `json:"bitsConsumed"`
	Complete            bool                       `json:"complete"`
	AudioTagHeader      *FLVAudioTagHeaderDetail   `json:"audioTagHeader,omitempty"`
	PacketType          *AACPacketTypeDetail       `json:"packetType,omitempty"`
	DataSpan            *Span                      `json:"dataSpan,omitempty"`
	RawDataSpan         *Span                      `json:"rawDataSpan,omitempty"`
	AudioSpecificConfig *AudioSpecificConfigDetail `json:"audioSpecificConfig,omitempty"`
}

type FLVAudioTagHeaderDetail struct {
	Raw         uint8         `json:"raw"`
	Span        Span          `json:"span"`
	SoundFormat AACNamedField `json:"soundFormat"`
	SoundRate   AACNamedField `json:"soundRate"`
	SoundSize   AACNamedField `json:"soundSize"`
	SoundType   AACNamedField `json:"soundType"`
}

type AACNamedField struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
	Span  Span   `json:"span"`
}

type AACPacketTypeDetail struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
	Span  Span   `json:"span"`
}

type AudioSpecificConfigDetail struct {
	Span                          Span                           `json:"span"`
	SyntaxSpan                    Span                           `json:"syntaxSpan"`
	TrailingSpan                  Span                           `json:"trailingSpan"`
	BitsConsumed                  uint64                         `json:"bitsConsumed"`
	TrailingBits                  uint64                         `json:"trailingBits"`
	Complete                      bool                           `json:"complete"`
	InitialAudioObjectType        *AACObjectTypeFieldDetail      `json:"initialAudioObjectType,omitempty"`
	AudioObjectType               *AACObjectTypeFieldDetail      `json:"audioObjectType,omitempty"`
	SamplingFrequency             *AACFrequencyFieldDetail       `json:"samplingFrequency,omitempty"`
	ChannelConfiguration          *AACChannelConfigurationDetail `json:"channelConfiguration,omitempty"`
	ExplicitSBR                   bool                           `json:"explicitSbr"`
	ExplicitPS                    bool                           `json:"explicitPs"`
	ExtensionAudioObjectType      *AACNamedValue                 `json:"extensionAudioObjectType,omitempty"`
	ExtensionSamplingFrequency    *AACFrequencyFieldDetail       `json:"extensionSamplingFrequency,omitempty"`
	ExtensionChannelConfiguration *AACChannelConfigurationDetail `json:"extensionChannelConfiguration,omitempty"`
	GASpecific                    *AACGASpecificConfigDetail     `json:"gaSpecificConfig,omitempty"`
	EPConfig                      *AACUnsignedField              `json:"epConfig,omitempty"`
}

type AACObjectTypeFieldDetail struct {
	Raw      AACUnsignedField  `json:"raw"`
	Escaped  bool              `json:"escaped"`
	Escape   *AACUnsignedField `json:"escape,omitempty"`
	Value    *uint8            `json:"value,omitempty"`
	Name     *string           `json:"name,omitempty"`
	Span     Span              `json:"span"`
	Complete bool              `json:"complete"`
}

type AACFrequencyFieldDetail struct {
	Index         AACUnsignedField `json:"index"`
	Explicit      bool             `json:"explicit"`
	ExplicitValue *uint32          `json:"explicitValue,omitempty"`
	ExplicitSpan  *Span            `json:"explicitSpan,omitempty"`
	Hertz         *uint32          `json:"hertz,omitempty"`
	Complete      bool             `json:"complete"`
}

type AACChannelConfigurationDetail struct {
	Value        uint8  `json:"value"`
	Name         string `json:"name"`
	Span         Span   `json:"span"`
	ChannelCount *uint8 `json:"channelCount,omitempty"`
}

type AACNamedValue struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
}

type AACUnsignedField struct {
	Value uint64 `json:"value"`
	Span  Span   `json:"span"`
}

type AACBooleanField struct {
	Value bool `json:"value"`
	Span  Span `json:"span"`
}

type AACGASpecificConfigDetail struct {
	Span                          Span              `json:"span"`
	Complete                      bool              `json:"complete"`
	FrameLengthFlag               *AACBooleanField  `json:"frameLengthFlag,omitempty"`
	DependsOnCoreCoder            *AACBooleanField  `json:"dependsOnCoreCoder,omitempty"`
	CoreCoderDelay                *AACUnsignedField `json:"coreCoderDelay,omitempty"`
	ExtensionFlag                 *AACBooleanField  `json:"extensionFlag,omitempty"`
	LayerNr                       *AACUnsignedField `json:"layerNr,omitempty"`
	NumOfSubFrame                 *AACUnsignedField `json:"numOfSubFrame,omitempty"`
	LayerLength                   *AACUnsignedField `json:"layerLength,omitempty"`
	SectionDataResilienceFlag     *AACBooleanField  `json:"sectionDataResilienceFlag,omitempty"`
	ScalefactorDataResilienceFlag *AACBooleanField  `json:"scalefactorDataResilienceFlag,omitempty"`
	SpectralDataResilienceFlag    *AACBooleanField  `json:"spectralDataResilienceFlag,omitempty"`
	ExtensionFlag3                *AACBooleanField  `json:"extensionFlag3,omitempty"`
}

// NewTagDetail maps a generic parser result to its strongly typed public
// contract. Existing ScriptData output goes through the original constructor.
func NewTagDetail(sourceSize uint64, tag flv.TagRecord, detail flv.ParsedTagDetail) (ParsedTagDetailDocument, error) {
	switch parsed := detail.(type) {
	case flv.ScriptData:
		return NewScriptTagDetail(sourceSize, tag, parsed), nil
	case *flv.ScriptData:
		if parsed == nil {
			return nil, fmt.Errorf("%w: nil *flv.ScriptData", ErrUnsupportedTagDetail)
		}
		return NewScriptTagDetail(sourceSize, tag, *parsed), nil
	case flv.AACAudio:
		return NewAACTagDetail(sourceSize, tag, parsed), nil
	case *flv.AACAudio:
		if parsed == nil {
			return nil, fmt.Errorf("%w: nil *flv.AACAudio", ErrUnsupportedTagDetail)
		}
		return NewAACTagDetail(sourceSize, tag, *parsed), nil
	default:
		return nil, fmt.Errorf("%w: %T", ErrUnsupportedTagDetail, detail)
	}
}

func NewAACTagDetail(sourceSize uint64, tag flv.TagRecord, detail flv.AACAudio) AACTagDetailDocument {
	document := AACTagDetailDocument{
		SchemaVersion: AACTagDetailSchemaVersion,
		DocumentType:  AACTagDetailDocumentType,
		Source:        Source{SizeBytes: sourceSize},
		Tag:           mapTag(tag),
		Detail: AACAudioDetail{
			Kind: AACAudioKind, Span: mapSpan(detail.Span),
			BitsConsumed: detail.BitsConsumed, Complete: detail.Complete,
		},
		Diagnostics: make([]TagDetailDiagnostic, 0, len(detail.Diagnostics)),
	}

	if detail.HeaderPresent {
		document.Detail.AudioTagHeader = &FLVAudioTagHeaderDetail{
			Raw: detail.Header.Raw, Span: mapSpan(detail.Header.Span),
			SoundFormat: AACNamedField{Value: detail.Header.SoundFormat, Name: soundFormatName(detail.Header.SoundFormat), Span: mapSpan(detail.Header.SoundFormatSpan)},
			SoundRate:   AACNamedField{Value: detail.Header.SoundRate, Name: soundRateName(detail.Header.SoundRate), Span: mapSpan(detail.Header.SoundRateSpan)},
			SoundSize:   AACNamedField{Value: detail.Header.SoundSize, Name: soundSizeName(detail.Header.SoundSize), Span: mapSpan(detail.Header.SoundSizeSpan)},
			SoundType:   AACNamedField{Value: detail.Header.SoundType, Name: soundTypeName(detail.Header.SoundType), Span: mapSpan(detail.Header.SoundTypeSpan)},
		}
	}
	if detail.PacketTypePresent {
		document.Detail.PacketType = &AACPacketTypeDetail{
			Value: uint8(detail.PacketType), Name: detail.PacketType.String(), Span: mapSpan(detail.PacketTypeSpan),
		}
		span := mapSpan(detail.DataSpan)
		document.Detail.DataSpan = &span
	}
	if detail.RawDataPresent {
		span := mapSpan(detail.DataSpan)
		document.Detail.RawDataSpan = &span
	}
	if detail.ConfigPresent {
		config := mapAudioSpecificConfig(detail.Config)
		document.Detail.AudioSpecificConfig = &config
	}
	for _, diagnostic := range detail.Diagnostics {
		document.Diagnostics = append(document.Diagnostics, TagDetailDiagnostic{
			Code: diagnostic.Code, Severity: amf0.Severity(diagnostic.Severity),
			Offset: diagnostic.Offset, BitOffset: diagnostic.BitOffset,
			Message: diagnostic.Message, Expected: cloneNumber(diagnostic.Expected),
			Actual: cloneNumber(diagnostic.Actual), RecoveryOffset: cloneNumber(diagnostic.RecoveryOffset),
		})
	}
	return document
}

// WriteParsedTagDetailJSON writes either supported detail document while the
// original WriteTagDetailJSON signature remains source compatible.
func WriteParsedTagDetailJSON(output io.Writer, document ParsedTagDetailDocument, pretty bool) error {
	return writeTagDetailJSON(output, document, pretty)
}

func WriteAACTagDetailJSON(output io.Writer, document AACTagDetailDocument, pretty bool) error {
	return writeTagDetailJSON(output, document, pretty)
}

func mapAudioSpecificConfig(config aac.Result) AudioSpecificConfigDetail {
	mapped := AudioSpecificConfigDetail{
		Span: mapSpan(config.Span), SyntaxSpan: mapSpan(config.SyntaxSpan), TrailingSpan: mapSpan(config.TrailingSpan),
		BitsConsumed: config.BitsConsumed, TrailingBits: config.TrailingBits, Complete: config.Complete,
		ExplicitSBR: config.ExplicitSBR, ExplicitPS: config.ExplicitPS,
	}
	mapped.InitialAudioObjectType = mapAACObjectTypeField(config.InitialAudioObjectType)
	mapped.AudioObjectType = mapAACObjectTypeField(config.AudioObjectType)
	mapped.SamplingFrequency = mapAACFrequencyField(config.SamplingFrequency)
	mapped.ChannelConfiguration = mapAACChannelConfiguration(config.ChannelConfiguration)

	if config.ExplicitSBR || config.ExplicitPS {
		mapped.ExtensionAudioObjectType = &AACNamedValue{
			Value: uint8(config.ExtensionAudioObjectType), Name: config.ExtensionAudioObjectType.String(),
		}
		mapped.ExtensionSamplingFrequency = mapAACFrequencyField(config.ExtensionSamplingFrequency)
	}
	if config.ExtensionChannelConfigurationPresent {
		mapped.ExtensionChannelConfiguration = mapAACChannelConfiguration(config.ExtensionChannelConfiguration)
	}
	if config.GASpecificPresent {
		ga := mapAACGASpecific(config.GASpecific)
		mapped.GASpecific = &ga
	}
	if config.EPConfigPresent && config.EPConfigComplete {
		mapped.EPConfig = &AACUnsignedField{Value: uint64(config.EPConfig), Span: mapSpan(config.EPConfigSpan)}
	}
	return mapped
}

func mapAACObjectTypeField(field aac.ObjectTypeField) *AACObjectTypeFieldDetail {
	if field.RawSpan.BitLength != 5 {
		return nil
	}
	mapped := &AACObjectTypeFieldDetail{
		Raw:     AACUnsignedField{Value: uint64(field.Raw), Span: mapSpan(field.RawSpan)},
		Escaped: field.Escaped, Span: mapSpan(field.Span), Complete: field.Complete,
	}
	if field.EscapeSpan.BitLength == 6 {
		mapped.Escape = &AACUnsignedField{Value: uint64(field.Escape), Span: mapSpan(field.EscapeSpan)}
	}
	if mapped.Complete {
		value := uint8(field.Value)
		name := field.Name
		mapped.Value, mapped.Name = &value, &name
	}
	return mapped
}

func mapAACFrequencyField(field aac.FrequencyField) *AACFrequencyFieldDetail {
	if field.IndexSpan.BitLength != 4 {
		return nil
	}
	mapped := &AACFrequencyFieldDetail{
		Index:    AACUnsignedField{Value: uint64(field.Index), Span: mapSpan(field.IndexSpan)},
		Explicit: field.Explicit, Complete: field.Complete,
	}
	if field.ExplicitSpan.BitLength == 24 {
		value := field.ExplicitValue
		span := mapSpan(field.ExplicitSpan)
		mapped.ExplicitValue, mapped.ExplicitSpan = &value, &span
	}
	if field.Hertz != 0 {
		hertz := field.Hertz
		mapped.Hertz = &hertz
	}
	return mapped
}

func mapAACChannelConfiguration(field aac.ChannelConfigurationField) *AACChannelConfigurationDetail {
	if !field.Complete {
		return nil
	}
	mapped := &AACChannelConfigurationDetail{Value: field.Value, Name: field.Name, Span: mapSpan(field.Span)}
	if count, ok := aacChannelCount(field.Value); ok {
		mapped.ChannelCount = &count
	}
	return mapped
}

func mapAACGASpecific(config aac.GASpecificConfig) AACGASpecificConfigDetail {
	mapped := AACGASpecificConfigDetail{Span: mapSpan(config.Span), Complete: config.Complete}
	mapped.FrameLengthFlag = aacBoolField(config.FrameLengthFlag, config.FrameLengthFlagSpan)
	mapped.DependsOnCoreCoder = aacBoolField(config.DependsOnCoreCoder, config.DependsOnCoreCoderSpan)
	if config.CoreCoderDelayPresent {
		mapped.CoreCoderDelay = aacUintField(uint64(config.CoreCoderDelay), config.CoreCoderDelaySpan, 14)
	}
	mapped.ExtensionFlag = aacBoolField(config.ExtensionFlag, config.ExtensionFlagSpan)
	if config.LayerNrPresent {
		mapped.LayerNr = aacUintField(uint64(config.LayerNr), config.LayerNrSpan, 3)
	}
	if config.NumOfSubFramePresent {
		mapped.NumOfSubFrame = aacUintField(uint64(config.NumOfSubFrame), config.NumOfSubFrameSpan, 5)
		mapped.LayerLength = aacUintField(uint64(config.LayerLength), config.LayerLengthSpan, 11)
	}
	if config.ResilienceFlagsPresent {
		mapped.SectionDataResilienceFlag = aacBoolField(config.SectionDataResilienceFlag, config.SectionDataResilienceFlagSpan)
		mapped.ScalefactorDataResilienceFlag = aacBoolField(config.ScalefactorDataResilienceFlag, config.ScalefactorDataResilienceSpan)
		mapped.SpectralDataResilienceFlag = aacBoolField(config.SpectralDataResilienceFlag, config.SpectralDataResilienceFlagSpan)
	}
	if config.ExtensionFlag3Present {
		mapped.ExtensionFlag3 = aacBoolField(config.ExtensionFlag3, config.ExtensionFlag3Span)
	}
	return mapped
}

func aacBoolField(value bool, span cursor.Span) *AACBooleanField {
	if span.BitLength != 1 {
		return nil
	}
	return &AACBooleanField{Value: value, Span: mapSpan(span)}
}

func aacUintField(value uint64, span cursor.Span, width uint32) *AACUnsignedField {
	if span.BitLength != width {
		return nil
	}
	return &AACUnsignedField{Value: value, Span: mapSpan(span)}
}

func aacChannelCount(configuration uint8) (uint8, bool) {
	switch configuration {
	case 1, 2, 3, 4, 5, 6:
		return configuration, true
	case 7:
		return 8, true
	default:
		return 0, false
	}
}

func soundFormatName(value uint8) string {
	if value == flv.SoundFormatAAC {
		return "AAC"
	}
	return "unknown"
}

func soundRateName(value uint8) string {
	return [...]string{"5.5kHz", "11kHz", "22kHz", "44kHz"}[value&3]
}

func soundSizeName(value uint8) string {
	if value == 0 {
		return "8-bit"
	}
	return "16-bit"
}

func soundTypeName(value uint8) string {
	if value == 0 {
		return "mono"
	}
	return "stereo"
}

var (
	_ ParsedTagDetailDocument = TagDetailDocument{}
	_ ParsedTagDetailDocument = AACTagDetailDocument{}
)
