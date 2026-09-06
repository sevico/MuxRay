package flv

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/sevico/muxray/internal/cursor"
)

// Scan reads only container structure and builds a compact tag index. It does
// not retain payload bytes and therefore keeps memory use independent of input
// payload size apart from the bounded tag index.
func Scan(source io.ReaderAt, size uint64, options Options) (ScanResult, error) {
	return scan(context.Background(), source, size, options, false, nil)
}

// ScanContext builds an immutable chunked index without populating Tags. On
// cancellation or I/O failure it returns the committed prefix and an error.
// Cancellation cannot interrupt a blocked underlying ReaderAt call.
func ScanContext(ctx context.Context, source io.ReaderAt, size uint64, options Options) (ScanResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return scan(ctx, source, size, options, true, nil)
}

func scan(ctx context.Context, source io.ReaderAt, size uint64, options Options, chunked bool, progress func(ScanProgress)) (ScanResult, error) {
	if source == nil {
		return ScanResult{}, errors.New("flv: nil source")
	}

	result := ScanResult{SourceSize: size}
	options, err := options.normalized()
	if err != nil {
		return result, err
	}
	if chunked {
		result.index = &chunkedTagIndex{limit: options.MaxTags}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	diagnostics := newDiagnosticCollector(options.MaxDiagnostics)
	if chunked {
		source = cursor.WithContext(ctx, source)
	}
	reader := cursor.NewByteReader(source, size)

	finishError := func(complete bool, err error) (ScanResult, error) {
		if canceled := ctx.Err(); canceled != nil {
			err = canceled
		}
		result.BytesScanned = reader.Offset()
		result.Complete = complete && err == nil
		result.Diagnostics = diagnostics.items
		return result, err
	}
	finish := func(complete bool) (ScanResult, error) { return finishError(complete, nil) }
	count := func() uint64 {
		if chunked {
			return result.index.Len()
		}
		return uint64(len(result.Tags))
	}
	publishProgress := func() {
		progress(ScanProgress{
			SourceSize: size, BytesScanned: reader.Offset(),
			TagCount: count(), DiagnosticCount: uint32(len(diagnostics.items)),
		})
	}
	appendRecord := func(record TagRecord) {
		if chunked {
			result.index.append(record)
		} else {
			result.Tags = append(result.Tags, record)
		}
		if progress != nil && count()%tagBlockSize == 0 {
			publishProgress()
		}
	}

	if size < HeaderSize {
		diagnostics.add(Diagnostic{
			Code: CodeHeaderTruncated, Severity: SeverityError, Offset: 0,
			Message:  "FLV header is shorter than 9 bytes",
			Expected: number(HeaderSize), Actual: number(size),
		})
		return finish(false)
	}

	var fixedHeader [9]byte
	if err := reader.ReadInto(fixedHeader[:]); err != nil {
		return finishError(false, fmt.Errorf("flv: read fixed header: %w", err))
	}
	signature := fixedHeader[:3]
	version := fixedHeader[3]
	typeFlags := fixedHeader[4]
	dataOffset := binary.BigEndian.Uint32(fixedHeader[5:9])

	result.HeaderPresent = true
	result.Header = Header{
		Span:       cursor.Span{ByteOffset: 0, BitLength: uint32(HeaderSize * 8)},
		Version:    version,
		TypeFlags:  typeFlags,
		HasAudio:   typeFlags&0x04 != 0,
		HasVideo:   typeFlags&0x01 != 0,
		DataOffset: dataOffset,
	}

	if !bytes.Equal(signature, []byte{'F', 'L', 'V'}) {
		diagnostics.add(Diagnostic{
			Code: CodeSignatureInvalid, Severity: SeverityError, Offset: 0,
			Message: "file does not begin with the FLV signature",
		})
		return finish(false)
	}
	if version != 1 {
		diagnostics.add(Diagnostic{
			Code: CodeVersionUnsupported, Severity: SeverityWarning, Offset: 3,
			Message:  "FLV version is not the currently supported version 1",
			Expected: number(1), Actual: number(uint64(version)),
		})
	}
	if reserved := typeFlags &^ uint8(0x05); reserved != 0 {
		diagnostics.add(Diagnostic{
			Code: CodeTypeFlagsReserved, Severity: SeverityWarning, Offset: 4,
			Message:  "reserved FLV type flag bits are non-zero",
			Expected: number(0), Actual: number(uint64(reserved)),
		})
	}
	if dataOffset < uint32(HeaderSize) {
		diagnostics.add(Diagnostic{
			Code: CodeDataOffsetInvalid, Severity: SeverityError, Offset: 5,
			Message:  "FLV data offset points inside the fixed header",
			Expected: number(HeaderSize), Actual: number(uint64(dataOffset)),
		})
		return finish(false)
	}
	if uint64(dataOffset) > size {
		diagnostics.add(Diagnostic{
			Code: CodeDataOffsetOutOfBounds, Severity: SeverityError, Offset: 5,
			Message:  "FLV data offset is beyond the end of the source",
			Expected: number(size), Actual: number(uint64(dataOffset)),
		})
		return finish(false)
	}

	result.Header.ExtraDataSize = uint64(dataOffset) - HeaderSize
	if err := reader.Seek(uint64(dataOffset)); err != nil {
		return finishError(false, fmt.Errorf("flv: seek to data offset: %w", err))
	}
	if reader.Remaining() < 4 {
		diagnostics.add(Diagnostic{
			Code: CodePreviousTagSize0Truncated, Severity: SeverityError, Offset: reader.Offset(),
			Message:  "initial PreviousTagSize is truncated",
			Expected: number(4), Actual: number(reader.Remaining()),
		})
		return finish(false)
	}

	previousTagSize0Offset := reader.Offset()
	var uint32Bytes [4]byte
	if err := reader.ReadInto(uint32Bytes[:]); err != nil {
		return finishError(false, fmt.Errorf("flv: read initial previous tag size: %w", err))
	}
	previousTagSize0 := binary.BigEndian.Uint32(uint32Bytes[:])
	result.PreviousTagSize0 = previousTagSize0
	if previousTagSize0 != 0 {
		diagnostics.add(Diagnostic{
			Code: CodePreviousTagSize0NonZero, Severity: SeverityWarning, Offset: previousTagSize0Offset,
			Message:  "initial PreviousTagSize should be zero",
			Expected: number(0), Actual: number(uint64(previousTagSize0)),
		})
	}
	if !chunked {
		result.Tags = make([]TagRecord, 0, initialTagCapacity(reader.Remaining(), options.MaxTags))
	}
	if progress != nil {
		publishProgress()
	}

	var tagHeader [11]byte
	for reader.Remaining() > 0 {
		if err := ctx.Err(); err != nil {
			return finishError(false, err)
		}
		if count() >= options.MaxTags {
			diagnostics.add(Diagnostic{
				Code: CodeTagLimitExceeded, Severity: SeverityError, Offset: reader.Offset(),
				Message:  "configured tag limit reached",
				Expected: number(options.MaxTags), Actual: number(count() + 1),
			})
			return finish(false)
		}

		tagOffset := reader.Offset()
		if reader.Remaining() < TagHeaderSize {
			diagnostics.add(Diagnostic{
				Code: CodeTagHeaderTruncated, Severity: SeverityError, Offset: tagOffset,
				Message:  "FLV tag header is truncated",
				Expected: number(TagHeaderSize), Actual: number(reader.Remaining()),
			})
			return finish(false)
		}

		if err := reader.ReadInto(tagHeader[:]); err != nil {
			return finishError(false, fmt.Errorf("flv: read tag header at %d: %w", tagOffset, err))
		}
		rawTypeFlags := tagHeader[0]
		dataSize := uint24(tagHeader[1:4])
		timestampLow := uint24(tagHeader[4:7])
		timestampExtended := tagHeader[7]
		streamID := uint24(tagHeader[8:11])

		tagType := TagType(rawTypeFlags & 0x1f)
		filter := rawTypeFlags&0x20 != 0
		record := TagRecord{
			ID:           count(),
			Offset:       tagOffset,
			RawTypeFlags: rawTypeFlags,
			DataSize:     dataSize,
			DTS:          int64(uint32(timestampLow) | uint32(timestampExtended)<<24),
			StreamID:     streamID,
		}

		if reserved := rawTypeFlags & 0xc0; reserved != 0 {
			diagnostics.add(Diagnostic{
				Code: CodeTagReservedBitsNonZero, Severity: SeverityWarning, Offset: tagOffset,
				Message:  "reserved FLV tag header bits are non-zero",
				Expected: number(0), Actual: number(uint64(reserved)),
			})
		}
		if filter {
			diagnostics.add(Diagnostic{
				Code: CodeTagFilterUnsupported, Severity: SeverityWarning, Offset: tagOffset,
				Message: "filtered/encrypted FLV tag payload is not decoded",
			})
		}
		if !tagType.Known() {
			diagnostics.add(Diagnostic{
				Code: CodeTagTypeUnknown, Severity: SeverityWarning, Offset: tagOffset,
				Message: "unknown FLV tag type is indexed but not decoded",
				Actual:  number(uint64(tagType)),
			})
		}
		if streamID != 0 {
			diagnostics.add(Diagnostic{
				Code: CodeStreamIDNonZero, Severity: SeverityWarning, Offset: tagOffset + 8,
				Message:  "FLV stream ID should be zero",
				Expected: number(0), Actual: number(uint64(streamID)),
			})
		}

		availablePayload := reader.Remaining()
		if availablePayload > uint64(dataSize) {
			availablePayload = uint64(dataSize)
		}
		record.AvailableDataSize = uint32(availablePayload)

		if reader.Remaining() < uint64(dataSize) {
			if err := reader.Skip(availablePayload); err != nil {
				return finishError(false, fmt.Errorf("flv: skip partial payload at %d: %w", tagOffset, err))
			}
			if err := ctx.Err(); err != nil {
				return finishError(false, err)
			}
			appendRecord(record)
			diagnostics.add(Diagnostic{
				Code: CodeTagBodyTruncated, Severity: SeverityError, Offset: reader.Offset(),
				Message:  "FLV tag payload is shorter than DataSize",
				Expected: number(uint64(dataSize)), Actual: number(availablePayload),
			})
			return finish(false)
		}

		if err := reader.Skip(uint64(dataSize)); err != nil {
			return finishError(false, fmt.Errorf("flv: skip payload at %d: %w", tagOffset, err))
		}
		record.flags |= tagFlagPayloadComplete
		record.AvailableDataSize = dataSize

		previousTagSizeOffset := reader.Offset()
		if reader.Remaining() < 4 {
			if err := ctx.Err(); err != nil {
				return finishError(false, err)
			}
			appendRecord(record)
			diagnostics.add(Diagnostic{
				Code: CodePreviousTagSizeTruncated, Severity: SeverityError, Offset: previousTagSizeOffset,
				Message:  "PreviousTagSize following the tag is truncated",
				Expected: number(4), Actual: number(reader.Remaining()),
			})
			if err := reader.Skip(reader.Remaining()); err != nil {
				return finishError(false, fmt.Errorf("flv: consume trailing previous tag size at %d: %w", previousTagSizeOffset, err))
			}
			return finish(false)
		}

		if err := reader.ReadInto(uint32Bytes[:]); err != nil {
			return finishError(false, fmt.Errorf("flv: read previous tag size at %d: %w", previousTagSizeOffset, err))
		}
		previousTagSize := binary.BigEndian.Uint32(uint32Bytes[:])
		record.flags |= tagFlagPreviousTagSizePresent
		record.PreviousTagSize = previousTagSize
		if previousTagSize == record.ExpectedPreviousTagSize() {
			record.flags |= tagFlagPreviousTagSizeValid
		} else {
			diagnostics.add(Diagnostic{
				Code: CodePreviousTagSizeMismatch, Severity: SeverityWarning, Offset: previousTagSizeOffset,
				Message:  "PreviousTagSize does not match the preceding tag",
				Expected: number(uint64(record.ExpectedPreviousTagSize())), Actual: number(uint64(previousTagSize)),
			})
		}

		if err := ctx.Err(); err != nil {
			return finishError(false, err)
		}
		appendRecord(record)
	}

	return finish(true)
}

func uint24(value []byte) uint32 {
	return uint32(value[0])<<16 | uint32(value[1])<<8 | uint32(value[2])
}

func initialTagCapacity(remainingBytes, maxTags uint64) int {
	// An FLV tag occupies at least its 11-byte header plus a 4-byte
	// PreviousTagSize. Using that structural upper bound avoids repeated large
	// slice reallocations while maxTags keeps the eager allocation bounded.
	estimate := remainingBytes / 15
	if remainingBytes%15 != 0 {
		estimate++
	}
	if estimate > maxTags {
		estimate = maxTags
	}
	return int(estimate)
}
