package report

import (
	"io"

	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

const (
	MP3TagDetailSchemaVersion = 1
	MP3TagDetailDocumentType  = "flv-mp3-tag-detail"
	MP3AudioKind              = "mp3Audio"
)

// MP3TagDetailDocument is the bounded report for the first MPEG Audio frame
// header in one FLV MP3 audio tag. It is independent from all existing detail
// and media-summary v1 contracts.
type MP3TagDetailDocument struct {
	SchemaVersion int                   `json:"schemaVersion"`
	DocumentType  string                `json:"documentType"`
	Source        Source                `json:"source"`
	Tag           Tag                   `json:"tag"`
	Detail        MP3AudioDetail        `json:"detail"`
	Diagnostics   []TagDetailDiagnostic `json:"diagnostics"`
}

func (MP3TagDetailDocument) parsedTagDetailDocument() {}

type MP3AudioDetail struct {
	Kind             string                   `json:"kind"`
	Span             Span                     `json:"span"`
	BitsConsumed     uint64                   `json:"bitsConsumed"`
	Complete         bool                     `json:"complete"`
	AudioTagHeader   *MP3AudioTagHeaderDetail `json:"audioTagHeader,omitempty"`
	AudioDataSpan    *Span                    `json:"audioDataSpan,omitempty"`
	FirstFrameHeader *MP3FrameHeaderDetail    `json:"firstFrameHeader,omitempty"`
	UnparsedDataSpan *Span                    `json:"unparsedDataSpan,omitempty"`
}

type MP3AudioTagHeaderDetail struct {
	Raw         uint8         `json:"raw"`
	Span        Span          `json:"span"`
	SoundFormat MP3NamedField `json:"soundFormat"`
	SoundRate   MP3NamedField `json:"soundRate"`
	SoundSize   MP3NamedField `json:"soundSize"`
	SoundType   MP3NamedField `json:"soundType"`
}

type MP3FrameHeaderDetail struct {
	Raw  uint32 `json:"raw"`
	Span Span   `json:"span"`

	SyncWord      MP3UnsignedField `json:"syncWord"`
	Version       MP3NamedField    `json:"version"`
	Layer         MP3NamedField    `json:"layer"`
	ProtectionBit MP3NamedField    `json:"protectionBit"`
	CRCProtected  bool             `json:"crcProtected"`

	BitrateIndex MP3UnsignedField `json:"bitrateIndex"`
	BitrateMode  string           `json:"bitrateMode"`
	BitrateKbps  *uint16          `json:"bitrateKbps,omitempty"`

	SamplingFrequencyIndex MP3UnsignedField `json:"samplingFrequencyIndex"`
	SamplingFrequencyHz    *uint32          `json:"samplingFrequencyHz,omitempty"`

	Padding          MP3BooleanField  `json:"padding"`
	Private          MP3BooleanField  `json:"private"`
	ChannelMode      MP3NamedField    `json:"channelMode"`
	ModeExtension    MP3UnsignedField `json:"modeExtension"`
	Channels         uint8            `json:"channels"`
	Copyright        MP3BooleanField  `json:"copyright"`
	Original         MP3BooleanField  `json:"original"`
	Emphasis         MP3NamedField    `json:"emphasis"`
	SamplesPerFrame  *uint16          `json:"samplesPerFrame,omitempty"`
	FrameLengthBytes *uint32          `json:"frameLengthBytes,omitempty"`
	Complete         bool             `json:"complete"`
}

type MP3NamedField struct {
	Value uint8  `json:"value"`
	Name  string `json:"name"`
	Span  Span   `json:"span"`
}

type MP3UnsignedField struct {
	Value uint64 `json:"value"`
	Span  Span   `json:"span"`
}

type MP3BooleanField struct {
	Value bool `json:"value"`
	Span  Span `json:"span"`
}

func NewMP3TagDetail(sourceSize uint64, tag flv.TagRecord, detail flv.MP3Audio) MP3TagDetailDocument {
	document := MP3TagDetailDocument{
		SchemaVersion: MP3TagDetailSchemaVersion,
		DocumentType:  MP3TagDetailDocumentType,
		Source:        Source{SizeBytes: sourceSize},
		Tag:           mapTag(tag),
		Detail: MP3AudioDetail{
			Kind: MP3AudioKind, Span: mapSpan(detail.Span),
			BitsConsumed: detail.BitsConsumed, Complete: detail.Complete,
		},
		Diagnostics: make([]TagDetailDiagnostic, 0, len(detail.Diagnostics)),
	}
	if detail.HeaderPresent {
		document.Detail.AudioTagHeader = &MP3AudioTagHeaderDetail{
			Raw: detail.Header.Raw, Span: mapSpan(detail.Header.Span),
			SoundFormat: MP3NamedField{Value: detail.Header.SoundFormat, Name: mp3SoundFormatName(detail.Header.SoundFormat), Span: mapSpan(detail.Header.SoundFormatSpan)},
			SoundRate:   MP3NamedField{Value: detail.Header.SoundRate, Name: soundRateName(detail.Header.SoundRate), Span: mapSpan(detail.Header.SoundRateSpan)},
			SoundSize:   MP3NamedField{Value: detail.Header.SoundSize, Name: soundSizeName(detail.Header.SoundSize), Span: mapSpan(detail.Header.SoundSizeSpan)},
			SoundType:   MP3NamedField{Value: detail.Header.SoundType, Name: soundTypeName(detail.Header.SoundType), Span: mapSpan(detail.Header.SoundTypeSpan)},
		}
		span := mapSpan(detail.DataSpan)
		document.Detail.AudioDataSpan = &span
	}
	if detail.FrameHeader.HeaderPresent {
		header := mapMP3FrameHeader(detail.FrameHeader.Header)
		document.Detail.FirstFrameHeader = &header
		if detail.DataSpan.BitLength >= 32 {
			span := mapSpan(detail.DataSpan)
			span.ByteOffset += 4
			span.BitLength -= 32
			document.Detail.UnparsedDataSpan = &span
		}
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

func WriteMP3TagDetailJSON(output io.Writer, document MP3TagDetailDocument, pretty bool) error {
	return writeTagDetailJSON(output, document, pretty)
}

func mapMP3FrameHeader(header mp3.FrameHeader) MP3FrameHeaderDetail {
	mapped := MP3FrameHeaderDetail{
		Raw: header.Raw, Span: mapSpan(header.Span),
		SyncWord:               MP3UnsignedField{Value: uint64(header.SyncWord), Span: mapSpan(header.SyncWordSpan)},
		Version:                MP3NamedField{Value: uint8(header.Version), Name: header.Version.String(), Span: mapSpan(header.VersionSpan)},
		Layer:                  MP3NamedField{Value: uint8(header.Layer), Name: header.Layer.String(), Span: mapSpan(header.LayerSpan)},
		ProtectionBit:          MP3NamedField{Value: header.ProtectionBit, Name: mp3ProtectionName(header.ProtectionBit), Span: mapSpan(header.ProtectionBitSpan)},
		CRCProtected:           header.CRCProtected,
		BitrateIndex:           MP3UnsignedField{Value: uint64(header.BitrateIndex), Span: mapSpan(header.BitrateIndexSpan)},
		BitrateMode:            string(header.BitrateMode),
		SamplingFrequencyIndex: MP3UnsignedField{Value: uint64(header.SamplingFrequencyIndex), Span: mapSpan(header.SamplingFrequencyIndexSpan)},
		Padding:                MP3BooleanField{Value: header.Padding, Span: mapSpan(header.PaddingSpan)},
		Private:                MP3BooleanField{Value: header.Private, Span: mapSpan(header.PrivateSpan)},
		ChannelMode:            MP3NamedField{Value: uint8(header.ChannelMode), Name: header.ChannelMode.String(), Span: mapSpan(header.ChannelModeSpan)},
		ModeExtension:          MP3UnsignedField{Value: uint64(header.ModeExtension), Span: mapSpan(header.ModeExtensionSpan)},
		Channels:               header.Channels,
		Copyright:              MP3BooleanField{Value: header.Copyright, Span: mapSpan(header.CopyrightSpan)},
		Original:               MP3BooleanField{Value: header.Original, Span: mapSpan(header.OriginalSpan)},
		Emphasis:               MP3NamedField{Value: uint8(header.Emphasis), Name: header.Emphasis.String(), Span: mapSpan(header.EmphasisSpan)},
		Complete:               header.Complete,
	}
	if header.BitratePresent {
		value := header.BitrateKbps
		mapped.BitrateKbps = &value
	}
	if header.SamplingFrequencyPresent {
		value := header.SamplingFrequencyHz
		mapped.SamplingFrequencyHz = &value
	}
	if header.SamplesPerFramePresent {
		value := header.SamplesPerFrame
		mapped.SamplesPerFrame = &value
	}
	if header.FrameLengthPresent {
		value := header.FrameLengthBytes
		mapped.FrameLengthBytes = &value
	}
	return mapped
}

func mp3SoundFormatName(value uint8) string {
	if value == flv.SoundFormatMP3 {
		return "MP3"
	}
	return "unknown"
}

func mp3ProtectionName(value uint8) string {
	if value == 0 {
		return "crc16"
	}
	return "none"
}

var _ ParsedTagDetailDocument = MP3TagDetailDocument{}
