package aac

import (
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/cursor"
)

// ParseAudioSpecificConfig parses one MPEG-4 AudioSpecificConfig. All spans
// and diagnostic offsets are absolute source positions. Malformed syntax is
// returned as a partial Result with diagnostics; invalid arguments and
// ReaderAt failures are returned as Go errors.
func ParseAudioSpecificConfig(source io.ReaderAt, payload cursor.Span, options Options) (Result, error) {
	result := Result{Span: payload}
	if source == nil {
		return result, errors.New("aac: nil source")
	}
	if err := payload.Validate(); err != nil {
		return result, fmt.Errorf("aac: invalid payload span: %w", err)
	}
	if !payload.IsByteAligned() {
		return result, errors.New("aac: payload span must be byte-aligned")
	}
	start, end, err := payload.ByteRange()
	if err != nil {
		return result, fmt.Errorf("aac: payload byte range: %w", err)
	}
	payloadBytes := end - start
	if payloadBytes > HardMaxPayloadBytes {
		return result, fmt.Errorf("aac: payload length %d exceeds hard limit %d", payloadBytes, HardMaxPayloadBytes)
	}
	options, err = options.Normalized()
	if err != nil {
		return result, err
	}
	reader, err := cursor.NewBitReaderAt(source, start, payloadBytes)
	if err != nil {
		return result, fmt.Errorf("aac: create payload reader: %w", err)
	}
	p := parser{
		reader:      reader,
		start:       start,
		end:         end,
		result:      result,
		diagnostics: newDiagnosticCollector(options.MaxDiagnostics),
	}
	p.parse()
	p.finish()
	if p.err != nil {
		return p.result, p.err
	}
	return p.result, nil
}

type parser struct {
	reader      *cursor.BitReader
	start       uint64
	end         uint64
	result      Result
	diagnostics *diagnosticCollector
	fatal       bool
	err         error
}

func (p *parser) parse() {
	initial, ok := p.readObjectType("initial")
	p.result.InitialAudioObjectType = initial
	p.result.AudioObjectType = initial
	if !ok || !p.validateObjectType(initial, initial.Value == AudioObjectTypeSBR || initial.Value == AudioObjectTypePS, "initial") {
		return
	}

	frequency, ok := p.readFrequency("sampling")
	p.result.SamplingFrequency = frequency
	if !ok {
		return
	}
	channel, ok := p.readChannelConfiguration("channel")
	p.result.ChannelConfiguration = channel
	if !ok {
		return
	}

	if initial.Value == AudioObjectTypeSBR || initial.Value == AudioObjectTypePS {
		p.result.ExplicitSBR = true
		p.result.ExplicitPS = initial.Value == AudioObjectTypePS
		p.result.ExtensionAudioObjectType = AudioObjectTypeSBR

		extensionFrequency, parsed := p.readFrequency("extension sampling")
		p.result.ExtensionSamplingFrequency = extensionFrequency
		if !parsed {
			return
		}
		core, parsed := p.readObjectType("core")
		p.result.AudioObjectType = core
		if !parsed || !p.validateObjectType(core, false, "core") {
			return
		}
		if core.Value == AudioObjectTypeERBSAC {
			extensionChannel, parsed := p.readChannelConfiguration("extension channel")
			p.result.ExtensionChannelConfiguration = extensionChannel
			p.result.ExtensionChannelConfigurationPresent = true
			if !parsed {
				return
			}
		}
	}

	effective := p.result.AudioObjectType.Value
	if !gaAudioObjectType(effective) {
		p.failAtSpan(Diagnostic{
			Code: CodeAudioObjectTypeUnsupported, Severity: SeverityError,
			Offset: p.result.AudioObjectType.Span.ByteOffset, BitOffset: p.result.AudioObjectType.Span.BitOffset,
			Message: "audio object type specific configuration is not implemented",
			Actual:  number(uint64(effective)),
		})
		return
	}
	p.result.GASpecificPresent = true
	p.parseGASpecific(effective)
	if p.stopped() {
		return
	}
	if errorResilientAudioObjectType(effective) {
		value, span, ok := p.readField(2, CodeEPConfigTruncated, "error-protection configuration is truncated")
		p.result.EPConfigPresent = true
		p.result.EPConfig = uint8(value)
		p.result.EPConfigSpan = span
		if !ok {
			return
		}
		p.result.EPConfigComplete = true
		if value == 2 || value == 3 {
			p.failAtSpan(Diagnostic{
				Code: CodeErrorProtectionConfigUnsupported, Severity: SeverityError,
				Offset: span.ByteOffset, BitOffset: span.BitOffset,
				Message: "ErrorProtectionSpecificConfig is not implemented",
				Actual:  number(value),
			})
		}
	}
}

func (p *parser) readObjectType(role string) (ObjectTypeField, bool) {
	startByte, startBit := p.reader.Offset()
	raw, rawSpan, ok := p.readField(5, CodeAudioObjectTypeTruncated, role+" audio object type is truncated")
	field := ObjectTypeField{Raw: uint8(raw), RawSpan: rawSpan, Value: AudioObjectType(raw)}
	if !ok {
		field.Value = 0
		field.Span = p.spanFrom(startByte, startBit)
		return field, false
	}
	if raw == uint64(AudioObjectTypeEscape) {
		field.Escaped = true
		extension, extensionSpan, parsed := p.readField(6, CodeAudioObjectTypeEscapeTruncated, role+" audio object type escape value is truncated")
		field.Escape = uint8(extension)
		field.EscapeSpan = extensionSpan
		field.Span = p.spanFrom(startByte, startBit)
		if !parsed {
			field.Value = 0
			return field, false
		}
		field.Value = AudioObjectType(32 + extension)
		field.Name = field.Value.String()
		field.Complete = true
		return field, true
	}
	field.Name = field.Value.String()
	field.Span = p.spanFrom(startByte, startBit)
	field.Complete = true
	return field, true
}

func (p *parser) validateObjectType(field ObjectTypeField, allowExtension bool, role string) bool {
	if allowExtension || gaAudioObjectType(field.Value) {
		return true
	}
	if field.Value.Known() {
		p.failAtSpan(Diagnostic{
			Code: CodeAudioObjectTypeUnsupported, Severity: SeverityError,
			Offset: field.Span.ByteOffset, BitOffset: field.Span.BitOffset,
			Message: role + " audio object type specific configuration is not implemented",
			Actual:  number(uint64(field.Value)),
		})
		return false
	}
	p.failAtSpan(Diagnostic{
		Code: CodeAudioObjectTypeReserved, Severity: SeverityError,
		Offset: field.Span.ByteOffset, BitOffset: field.Span.BitOffset,
		Message: role + " audio object type is reserved",
		Actual:  number(uint64(field.Value)),
	})
	return false
}

func (p *parser) readFrequency(role string) (FrequencyField, bool) {
	index, indexSpan, ok := p.readField(4, CodeSamplingFrequencyIndexTruncated, role+" frequency index is truncated")
	field := FrequencyField{Index: uint8(index), IndexSpan: indexSpan}
	if !ok {
		return field, false
	}
	if index == 13 || index == 14 {
		field.Complete = true
		p.failAtSpan(Diagnostic{
			Code: CodeSamplingFrequencyIndexReserved, Severity: SeverityError,
			Offset: indexSpan.ByteOffset, BitOffset: indexSpan.BitOffset,
			Message: role + " frequency index is reserved", Actual: number(index),
		})
		return field, false
	}
	if index == 15 {
		field.Explicit = true
		value, valueSpan, parsed := p.readField(24, CodeSamplingFrequencyTruncated, role+" explicit frequency is truncated")
		field.ExplicitValue = uint32(value)
		field.ExplicitSpan = valueSpan
		field.Hertz = uint32(value)
		if !parsed {
			return field, false
		}
		field.Complete = true
		if value == 0 {
			p.failAtSpan(Diagnostic{
				Code: CodeSamplingFrequencyZero, Severity: SeverityError,
				Offset: valueSpan.ByteOffset, BitOffset: valueSpan.BitOffset,
				Message: role + " explicit frequency must be non-zero", Actual: number(0),
			})
			return field, false
		}
		return field, true
	}
	field.Hertz = samplingFrequencies[index]
	field.Complete = true
	return field, true
}

func (p *parser) readChannelConfiguration(role string) (ChannelConfigurationField, bool) {
	value, span, ok := p.readField(4, CodeChannelConfigurationTruncated, role+" configuration is truncated")
	field := ChannelConfigurationField{Value: uint8(value), Name: channelConfigurationName(uint8(value)), Span: span}
	if !ok {
		return field, false
	}
	field.Complete = true
	if value == 0 {
		p.failAtSpan(Diagnostic{
			Code: CodeProgramConfigElementUnsupported, Severity: SeverityError,
			Offset: span.ByteOffset, BitOffset: span.BitOffset,
			Message: "channelConfiguration 0 requires an unsupported program_config_element",
			Actual:  number(0),
		})
		return field, false
	}
	if value > 7 {
		p.failAtSpan(Diagnostic{
			Code: CodeChannelConfigurationReserved, Severity: SeverityError,
			Offset: span.ByteOffset, BitOffset: span.BitOffset,
			Message: role + " configuration is reserved", Actual: number(value),
		})
		return field, false
	}
	return field, true
}

func (p *parser) parseGASpecific(objectType AudioObjectType) {
	startByte, startBit := p.reader.Offset()
	defer func() {
		p.result.GASpecific.Span = p.spanFrom(startByte, startBit)
		p.result.GASpecific.Complete = !p.stopped()
	}()

	value, span, ok := p.readField(1, CodeFrameLengthFlagTruncated, "frameLengthFlag is truncated")
	p.result.GASpecific.FrameLengthFlag = value != 0
	p.result.GASpecific.FrameLengthFlagSpan = span
	if !ok {
		return
	}
	value, span, ok = p.readField(1, CodeDependsOnCoreCoderTruncated, "dependsOnCoreCoder is truncated")
	p.result.GASpecific.DependsOnCoreCoder = value != 0
	p.result.GASpecific.DependsOnCoreCoderSpan = span
	if !ok {
		return
	}
	if value != 0 {
		p.result.GASpecific.CoreCoderDelayPresent = true
		value, span, ok = p.readField(14, CodeCoreCoderDelayTruncated, "coreCoderDelay is truncated")
		p.result.GASpecific.CoreCoderDelay = uint16(value)
		p.result.GASpecific.CoreCoderDelaySpan = span
		if !ok {
			return
		}
	}
	value, span, ok = p.readField(1, CodeExtensionFlagTruncated, "extensionFlag is truncated")
	p.result.GASpecific.ExtensionFlag = value != 0
	p.result.GASpecific.ExtensionFlagSpan = span
	if !ok {
		return
	}

	if objectType == AudioObjectTypeAACScalable || objectType == AudioObjectTypeERAACScalable {
		p.result.GASpecific.LayerNrPresent = true
		value, span, ok = p.readField(3, CodeLayerNrTruncated, "layerNr is truncated")
		p.result.GASpecific.LayerNr = uint8(value)
		p.result.GASpecific.LayerNrSpan = span
		if !ok {
			return
		}
	}
	if !p.result.GASpecific.ExtensionFlag {
		return
	}
	if objectType == AudioObjectTypeERBSAC {
		p.result.GASpecific.NumOfSubFramePresent = true
		value, span, ok = p.readField(5, CodeNumOfSubFrameTruncated, "numOfSubFrame is truncated")
		p.result.GASpecific.NumOfSubFrame = uint8(value)
		p.result.GASpecific.NumOfSubFrameSpan = span
		if !ok {
			return
		}
		value, span, ok = p.readField(11, CodeLayerLengthTruncated, "layer_length is truncated")
		p.result.GASpecific.LayerLength = uint16(value)
		p.result.GASpecific.LayerLengthSpan = span
		if !ok {
			return
		}
	}
	if resilienceFlagsPresent(objectType) {
		p.result.GASpecific.ResilienceFlagsPresent = true
		value, span, ok = p.readField(1, CodeSectionDataResilienceTruncated, "aacSectionDataResilienceFlag is truncated")
		p.result.GASpecific.SectionDataResilienceFlag = value != 0
		p.result.GASpecific.SectionDataResilienceFlagSpan = span
		if !ok {
			return
		}
		value, span, ok = p.readField(1, CodeScalefactorDataResilienceTruncated, "aacScalefactorDataResilienceFlag is truncated")
		p.result.GASpecific.ScalefactorDataResilienceFlag = value != 0
		p.result.GASpecific.ScalefactorDataResilienceSpan = span
		if !ok {
			return
		}
		value, span, ok = p.readField(1, CodeSpectralDataResilienceTruncated, "aacSpectralDataResilienceFlag is truncated")
		p.result.GASpecific.SpectralDataResilienceFlag = value != 0
		p.result.GASpecific.SpectralDataResilienceFlagSpan = span
		if !ok {
			return
		}
	}
	p.result.GASpecific.ExtensionFlag3Present = true
	value, span, ok = p.readField(1, CodeExtensionFlag3Truncated, "extensionFlag3 is truncated")
	p.result.GASpecific.ExtensionFlag3 = value != 0
	p.result.GASpecific.ExtensionFlag3Span = span
	if !ok {
		return
	}
	if value != 0 {
		p.failAtSpan(Diagnostic{
			Code: CodeExtensionFlag3Unsupported, Severity: SeverityError,
			Offset: span.ByteOffset, BitOffset: span.BitOffset,
			Message: "extensionFlag3 syntax is not implemented",
			Actual:  number(value),
		})
	}
}

func (p *parser) readField(bits uint8, code, message string) (uint64, cursor.Span, bool) {
	if p.stopped() {
		return 0, cursor.Span{}, false
	}
	startByte, startBit := p.reader.Offset()
	if p.reader.RemainingBits() < uint64(bits) {
		available := p.reader.RemainingBits()
		if err := p.reader.Skip(available); err != nil {
			byteOffset, bitOffset := p.reader.Offset()
			p.err = fmt.Errorf("aac: consume truncated field at byte %d bit %d: %w", byteOffset, bitOffset, err)
			return 0, cursor.Span{}, false
		}
		byteOffset, bitOffset := p.reader.Offset()
		p.fail(Diagnostic{
			Code: code, Severity: SeverityError, Offset: byteOffset, BitOffset: bitOffset,
			Message: message, Expected: number(uint64(bits)), Actual: number(available),
		})
		return 0, cursor.Span{ByteOffset: startByte, BitOffset: startBit, BitLength: uint32(available)}, false
	}
	byteOffset, bitOffset := startByte, startBit
	value, span, err := p.reader.ReadBitsWithSpan(bits)
	if err != nil {
		p.err = fmt.Errorf("aac: read %d-bit field at byte %d bit %d: %w", bits, byteOffset, bitOffset, err)
		return 0, cursor.Span{}, false
	}
	return value, span, true
}

func (p *parser) finish() {
	byteOffset, bitOffset := p.reader.Offset()
	p.result.BitsConsumed = bitDistance(p.start, 0, byteOffset, bitOffset)
	p.result.SyntaxSpan = cursor.Span{ByteOffset: p.start, BitLength: uint32(p.result.BitsConsumed)}
	p.result.TrailingBits = p.reader.RemainingBits()
	p.result.TrailingSpan = cursor.Span{ByteOffset: byteOffset, BitOffset: bitOffset, BitLength: uint32(p.result.TrailingBits)}
	p.result.Diagnostics = p.diagnostics.items
	p.result.Complete = p.err == nil && !p.fatal && p.result.GASpecificPresent && p.result.GASpecific.Complete
}

func (p *parser) failAtSpan(diagnostic Diagnostic) {
	p.fail(diagnostic)
}

func (p *parser) fail(diagnostic Diagnostic) {
	recovery := p.end
	diagnostic.RecoveryOffset = &recovery
	p.diagnostics.add(diagnostic)
	p.fatal = true
}

func (p *parser) stopped() bool { return p.fatal || p.err != nil }

func (p *parser) spanFrom(startByte uint64, startBit uint8) cursor.Span {
	endByte, endBit := p.reader.Offset()
	return cursor.Span{ByteOffset: startByte, BitOffset: startBit, BitLength: uint32(bitDistance(startByte, startBit, endByte, endBit))}
}

func bitDistance(startByte uint64, startBit uint8, endByte uint64, endBit uint8) uint64 {
	return (endByte-startByte)*8 + uint64(endBit) - uint64(startBit)
}

func gaAudioObjectType(objectType AudioObjectType) bool {
	switch objectType {
	case AudioObjectTypeAACMain, AudioObjectTypeAACLC, AudioObjectTypeAACSSR,
		AudioObjectTypeAACLTP, AudioObjectTypeAACScalable, AudioObjectTypeTwinVQ,
		AudioObjectTypeERAACLC, AudioObjectTypeERAACLTP, AudioObjectTypeERAACScalable,
		AudioObjectTypeERTwinVQ, AudioObjectTypeERBSAC, AudioObjectTypeERAACLD:
		return true
	default:
		return false
	}
}

func errorResilientAudioObjectType(objectType AudioObjectType) bool {
	switch objectType {
	case AudioObjectTypeERAACLC, AudioObjectTypeERAACLTP, AudioObjectTypeERAACScalable,
		AudioObjectTypeERTwinVQ, AudioObjectTypeERBSAC, AudioObjectTypeERAACLD:
		return true
	default:
		return false
	}
}

func resilienceFlagsPresent(objectType AudioObjectType) bool {
	switch objectType {
	case AudioObjectTypeERAACLC, AudioObjectTypeERAACLTP,
		AudioObjectTypeERAACScalable, AudioObjectTypeERAACLD:
		return true
	default:
		return false
	}
}

var samplingFrequencies = [...]uint32{
	96_000, 88_200, 64_000, 48_000, 44_100, 32_000, 24_000,
	22_050, 16_000, 12_000, 11_025, 8_000, 7_350, 0, 0,
}
