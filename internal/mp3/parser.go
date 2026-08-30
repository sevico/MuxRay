package mp3

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/cursor"
)

const frameHeaderBytes = 4

var (
	mpeg1Layer3BitrateKbps = [...]uint16{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	mpeg2Layer3BitrateKbps = [...]uint16{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
	baseSamplingFrequency  = [...]uint32{44100, 48000, 32000}
)

// ParseFrameHeader parses the first fixed MPEG Audio Layer III header in the
// supplied byte-aligned range. Malformed syntax returns a partial Result;
// invalid arguments and ReaderAt failures return Go errors.
func ParseFrameHeader(source io.ReaderAt, payload cursor.Span, options Options) (Result, error) {
	result := Result{Span: payload}
	if source == nil {
		return result, errors.New("mp3: nil source")
	}
	if err := payload.Validate(); err != nil {
		return result, fmt.Errorf("mp3: invalid payload span: %w", err)
	}
	if !payload.IsByteAligned() {
		return result, errors.New("mp3: payload span must be byte-aligned")
	}
	start, end, err := payload.ByteRange()
	if err != nil {
		return result, fmt.Errorf("mp3: payload byte range: %w", err)
	}
	payloadBytes := end - start
	if payloadBytes > HardMaxPayloadBytes {
		return result, fmt.Errorf("mp3: payload length %d exceeds hard limit %d", payloadBytes, HardMaxPayloadBytes)
	}
	normalized, err := options.Normalized()
	if err != nil {
		return result, err
	}
	diagnostics := newDiagnosticCollector(normalized.MaxDiagnostics, end)
	if payloadBytes < frameHeaderBytes {
		diagnostics.add(Diagnostic{
			Code: CodeFrameHeaderTruncated, Severity: SeverityError,
			Offset: start, Message: "MPEG Audio frame header requires four bytes",
			Expected: number(frameHeaderBytes), Actual: number(payloadBytes), RecoveryOffset: number(end),
		})
		result.Diagnostics = diagnostics.items
		return result, nil
	}
	if start > math.MaxInt64-(frameHeaderBytes-1) {
		return result, fmt.Errorf("mp3: frame header offset: %w", &cursor.OffsetError{Offset: start, Length: frameHeaderBytes})
	}

	var raw [frameHeaderBytes]byte
	n, readErr := source.ReadAt(raw[:], int64(start))
	if n != len(raw) {
		if readErr == nil {
			readErr = io.ErrUnexpectedEOF
		}
		return result, fmt.Errorf("mp3: read frame header at byte %d: %w", start, readErr)
	}

	header := decodeFrameHeader(start, raw)
	result.Header = header
	result.HeaderPresent = true
	result.BitsConsumed = frameHeaderBytes * 8
	validateFrameHeader(&result.Header, diagnostics, end)
	deriveFrameHeader(&result.Header)
	result.Header.Complete = !hasError(diagnostics.items)
	result.Diagnostics = diagnostics.items
	result.Complete = result.Header.Complete
	return result, nil
}

func decodeFrameHeader(start uint64, raw [frameHeaderBytes]byte) FrameHeader {
	value := binary.BigEndian.Uint32(raw[:])
	header := FrameHeader{
		Raw: value, Span: frameBitSpan(start, 0, 32),
		SyncWord: uint16(value >> 21), SyncWordSpan: frameBitSpan(start, 0, 11),
		Version: Version((value >> 19) & 3), VersionSpan: frameBitSpan(start, 11, 2),
		Layer: Layer((value >> 17) & 3), LayerSpan: frameBitSpan(start, 13, 2),
		ProtectionBit: uint8((value >> 16) & 1), ProtectionBitSpan: frameBitSpan(start, 15, 1),
		BitrateIndex: uint8((value >> 12) & 15), BitrateIndexSpan: frameBitSpan(start, 16, 4),
		SamplingFrequencyIndex: uint8((value >> 10) & 3), SamplingFrequencyIndexSpan: frameBitSpan(start, 20, 2),
		Padding: value&(1<<9) != 0, PaddingSpan: frameBitSpan(start, 22, 1),
		Private: value&(1<<8) != 0, PrivateSpan: frameBitSpan(start, 23, 1),
		ChannelMode: ChannelMode((value >> 6) & 3), ChannelModeSpan: frameBitSpan(start, 24, 2),
		ModeExtension: uint8((value >> 4) & 3), ModeExtensionSpan: frameBitSpan(start, 26, 2),
		Copyright: value&(1<<3) != 0, CopyrightSpan: frameBitSpan(start, 28, 1),
		Original: value&(1<<2) != 0, OriginalSpan: frameBitSpan(start, 29, 1),
		Emphasis: Emphasis(value & 3), EmphasisSpan: frameBitSpan(start, 30, 2),
	}
	header.CRCProtected = header.ProtectionBit == 0
	if header.ChannelMode == ChannelModeSingleChannel {
		header.Channels = 1
	} else {
		header.Channels = 2
	}
	switch header.BitrateIndex {
	case 0:
		header.BitrateMode = BitrateModeFree
	case 15:
		header.BitrateMode = BitrateModeInvalid
	default:
		header.BitrateMode = BitrateModeIndexed
	}
	return header
}

func validateFrameHeader(header *FrameHeader, diagnostics *diagnosticCollector, recovery uint64) {
	fail := func(code string, span cursor.Span, message string, expected, actual *uint64) {
		diagnostics.add(Diagnostic{
			Code: code, Severity: SeverityError,
			Offset: span.ByteOffset, BitOffset: span.BitOffset, Message: message,
			Expected: expected, Actual: actual, RecoveryOffset: number(recovery),
		})
	}
	warn := func(code string, span cursor.Span, message string, actual *uint64) {
		diagnostics.add(Diagnostic{
			Code: code, Severity: SeverityWarning,
			Offset: span.ByteOffset, BitOffset: span.BitOffset, Message: message, Actual: actual,
		})
	}

	if header.SyncWord != 0x7ff {
		fail(CodeSyncInvalid, header.SyncWordSpan, "MPEG Audio sync word is not eleven set bits", number(0x7ff), number(uint64(header.SyncWord)))
	}
	if header.Version == VersionReserved {
		fail(CodeVersionReserved, header.VersionSpan, "MPEG Audio version ID is reserved", nil, number(uint64(header.Version)))
	}
	switch header.Layer {
	case LayerReserved:
		fail(CodeLayerReserved, header.LayerSpan, "MPEG Audio layer value is reserved", nil, number(uint64(header.Layer)))
	case LayerI, LayerII:
		fail(CodeLayerNotIII, header.LayerSpan, "FLV MP3 SoundData must contain MPEG Audio Layer III", number(uint64(LayerIII)), number(uint64(header.Layer)))
	}
	if header.BitrateIndex == 15 {
		fail(CodeBitrateIndexInvalid, header.BitrateIndexSpan, "MPEG Audio bitrate index 15 is invalid", nil, number(uint64(header.BitrateIndex)))
	} else if header.BitrateIndex == 0 {
		warn(CodeFreeFormatUnresolved, header.BitrateIndexSpan, "free-format bitrate and frame length cannot be derived from one header", number(0))
	}
	if header.SamplingFrequencyIndex == 3 {
		fail(CodeSamplingFrequencyReserved, header.SamplingFrequencyIndexSpan, "MPEG Audio sampling-frequency index is reserved", nil, number(uint64(header.SamplingFrequencyIndex)))
	}
	if header.Emphasis == EmphasisReserved {
		warn(CodeEmphasisReserved, header.EmphasisSpan, "MPEG Audio emphasis value is reserved", number(uint64(header.Emphasis)))
	}
}

func deriveFrameHeader(header *FrameHeader) {
	versionValid := header.Version != VersionReserved
	layerValid := header.Layer == LayerIII
	samplingValid := header.SamplingFrequencyIndex < 3

	if versionValid && samplingValid {
		frequency := baseSamplingFrequency[header.SamplingFrequencyIndex]
		switch header.Version {
		case VersionMPEG2:
			frequency /= 2
		case VersionMPEG25:
			frequency /= 4
		}
		header.SamplingFrequencyHz = frequency
		header.SamplingFrequencyPresent = true
	}
	if versionValid && layerValid {
		if header.Version == VersionMPEG1 {
			header.SamplesPerFrame = 1152
		} else {
			header.SamplesPerFrame = 576
		}
		header.SamplesPerFramePresent = true
	}
	if versionValid && layerValid && header.BitrateIndex > 0 && header.BitrateIndex < 15 {
		if header.Version == VersionMPEG1 {
			header.BitrateKbps = mpeg1Layer3BitrateKbps[header.BitrateIndex]
		} else {
			header.BitrateKbps = mpeg2Layer3BitrateKbps[header.BitrateIndex]
		}
		header.BitratePresent = true
	}
	if header.BitratePresent && header.SamplingFrequencyPresent {
		coefficient := uint64(144000)
		if header.Version != VersionMPEG1 {
			coefficient = 72000
		}
		length := coefficient * uint64(header.BitrateKbps) / uint64(header.SamplingFrequencyHz)
		if header.Padding {
			length++
		}
		header.FrameLengthBytes = uint32(length)
		header.FrameLengthPresent = true
	}
}

func frameBitSpan(start uint64, bitOffset uint8, bitLength uint32) cursor.Span {
	return cursor.Span{
		ByteOffset: start + uint64(bitOffset/8),
		BitOffset:  bitOffset % 8,
		BitLength:  bitLength,
	}
}
