package mp3

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/cursor"
)

const (
	CodeFrameBodyTruncated      = "MP3_FRAME_BODY_TRUNCATED"
	CodeFrameLimitReached       = "MP3_FRAME_LIMIT_REACHED"
	CodeFrameByteLimitReached   = "MP3_FRAME_BYTE_LIMIT_REACHED"
	CodeFrameBoundaryUnresolved = "MP3_FRAME_BOUNDARY_UNRESOLVED"
	CodeCRCTruncated            = "MP3_CRC_TRUNCATED"
	CodeCRCMismatch             = "MP3_CRC_MISMATCH"
	CodeVBRHeaderTruncated      = "MP3_VBR_HEADER_TRUNCATED"
	CodeVBRFlagsUnsupported     = "MP3_VBR_FLAGS_UNSUPPORTED"
	CodeVBRIVersionUnsupported  = "MP3_VBRI_VERSION_UNSUPPORTED"
)

// FrameSequence inspects declared boundaries in one SoundData span, without
// decoding audio, searching for sync words, or crossing into another FLV tag.
// BytesCovered includes skipped frame bodies; BytesRead counts bounded reads.
// Complete does not assert Huffman, reservoir, or full audio integrity.
type FrameSequence struct {
	Span            cursor.Span
	Frames          []Frame
	MaxFrames       uint32
	MaxBytes        uint64
	BytesCovered    uint64
	BytesRead       uint64
	CompleteFrames  uint32
	UninspectedSpan cursor.Span
	StopReason      string
	Complete        bool
	Diagnostics     []Diagnostic
	VBR             *VBRHeader
}

type Frame struct {
	Index            uint32
	Span             cursor.Span
	Header           FrameHeader
	BoundaryComplete bool
	CRC              FrameCRC
}

// FrameCRC covers header bytes 2..3 and Layer III side information only.
// Main data, ancillary bytes, and encoder-specific music CRCs are excluded.
type FrameCRC struct {
	Status        string
	Span          cursor.Span
	Stored        uint16
	Computed      uint16
	ValuesPresent bool
}

// VBRHeader contains whole-stream declarations, not the number of frames or
// bytes inspected inside this FLV tag. TOC is retained as a source span only.
type VBRHeader struct {
	Kind           string
	Span           cursor.Span
	Complete       bool
	Flags          *uint32
	Version        *uint16
	Delay          *uint16
	Quality        *uint32
	DeclaredFrames *uint32
	DeclaredBytes  *uint32
	TOCSpan        *cursor.Span
}

// ParseFrameSequence reads at most MaxFrames fixed headers over a 1 MiB
// source range, plus first-frame VBR metadata and protected-frame side info.
// Free-format stops without guessing a frame length.
func ParseFrameSequence(source io.ReaderAt, payload cursor.Span, options Options) (FrameSequence, error) {
	out := FrameSequence{Span: payload, Frames: []Frame{}, StopReason: "end"}
	options, err := options.Normalized()
	if err != nil {
		return out, err
	}
	options.InspectFrames = true // Deep inspection preserves full-count non-EOF source errors.
	out.MaxFrames, out.MaxBytes = options.MaxFrames, MaxFrameScanBytes
	// Reuse the fixed-header parser's argument checks, including the 16 MiB cap.
	first, err := ParseFrameHeader(source, payload, options)
	if err != nil {
		return out, err
	}
	start, end, _ := payload.ByteRange()
	limit := end
	if end-start > MaxFrameScanBytes {
		limit = start + MaxFrameScanBytes
	}
	d := newDiagnosticCollector(options.MaxDiagnostics, end)
	at := start
	fail := func(code, message, reason string, off uint64) {
		d.add(Diagnostic{Code: code, Severity: SeverityError, Offset: off, Message: message, RecoveryOffset: number(end)})
		out.StopReason = reason
	}
	for at < end || (at == start && end == start) {
		if uint32(len(out.Frames)) >= options.MaxFrames {
			fail(CodeFrameLimitReached, "same-tag frame inspection limit reached", "frame_limit", at)
			break
		}
		if at < end && (at >= limit || limit-at < 4 && end-at >= 4) {
			fail(CodeFrameByteLimitReached, "same-tag frame byte coverage limit reached", "byte_limit", at)
			break
		}
		parsed := first
		if at != start {
			parsed, err = ParseFrameHeader(source, byteSpan(at, end-at), options)
			if err != nil {
				return out, err
			}
		}
		out.BytesRead += parsed.BitsConsumed / 8
		for _, diagnostic := range parsed.Diagnostics {
			d.add(diagnostic)
		}
		if !parsed.HeaderPresent {
			out.StopReason = "header_truncated"
			break
		}
		h := parsed.Header
		f := Frame{Index: uint32(len(out.Frames)), Span: byteSpan(at, 4), Header: h, CRC: FrameCRC{Status: "not_checked"}}
		if !h.CRCProtected {
			f.CRC.Status = "not_protected"
		}
		if !parsed.Complete {
			out.Frames = append(out.Frames, f)
			out.StopReason = "invalid_header"
			break
		}
		if !h.FrameLengthPresent {
			out.Frames = append(out.Frames, f)
			fail(CodeFrameBoundaryUnresolved, "free-format frame length is unknown; no sync search is performed", "free_format", at)
			break
		}
		length := uint64(h.FrameLengthBytes)
		available := min(length, end-at)
		f.Span = byteSpan(at, available)
		if length > limit-at && limit < end {
			f.Span = byteSpan(at, 4)
			out.Frames = append(out.Frames, f)
			fail(CodeFrameByteLimitReached, "declared frame exceeds same-tag byte coverage limit", "byte_limit", at)
			break
		}
		// A truncated frame can still expose a complete bounded CRC prefix.
		read := func(offset uint64, count uint64) ([]byte, error) {
			if offset < at || offset > at+available || count > at+available-offset || offset > limit || count > limit-offset {
				return nil, io.ErrUnexpectedEOF
			}
			if offset > math.MaxInt64 || count > uint64(math.MaxInt64)-offset {
				return nil, fmt.Errorf("mp3: metadata offset exceeds ReaderAt range")
			}
			buf := make([]byte, count)
			n, e := source.ReadAt(buf, int64(offset))
			if n > 0 && n <= len(buf) {
				out.BytesRead += uint64(n)
			}
			if n != len(buf) {
				if e == nil {
					e = io.ErrUnexpectedEOF
				}
				return nil, fmt.Errorf("mp3: source read at byte %d: %w", offset, e)
			}
			if e != nil && e != io.EOF {
				return nil, fmt.Errorf("mp3: source read at byte %d: %w", offset, e)
			}
			return buf, nil
		}
		if h.CRCProtected {
			f.CRC.Span = byteSpan(at+4, min(uint64(2), available-4))
			count := uint64(2 + sideInfoBytes(h))
			if available < 4+count {
				f.CRC.Status = "truncated"
				d.add(Diagnostic{Code: CodeCRCTruncated, Severity: SeverityError, Offset: at + 4, Message: "CRC word and Layer III side information do not fit in this frame", RecoveryOffset: number(end)})
			} else {
				prefix, e := read(at+4, count)
				if e != nil {
					return out, fmt.Errorf("mp3: read CRC side information: %w", e)
				}
				f.CRC.Stored = binary.BigEndian.Uint16(prefix[:2])
				f.CRC.Computed = crc16([]byte{byte(h.Raw >> 8), byte(h.Raw)}, prefix[2:])
				f.CRC.ValuesPresent = true
				f.CRC.Status = "match"
				if f.CRC.Stored != f.CRC.Computed {
					f.CRC.Status = "mismatch"
					d.add(Diagnostic{Code: CodeCRCMismatch, Severity: SeverityError, Offset: at + 4, Message: "CRC differs for MPEG header bits and Layer III side information; audio main data is not covered", Expected: number(uint64(f.CRC.Computed)), Actual: number(uint64(f.CRC.Stored)), RecoveryOffset: number(end)})
				}
			}
		}
		if at == start {
			vbr, e := parseVBR(h, available, read, d, end)
			if e != nil {
				return out, e
			}
			out.VBR = vbr
		}
		f.BoundaryComplete = length <= end-at
		out.Frames = append(out.Frames, f)
		if !f.BoundaryComplete {
			fail(CodeFrameBodyTruncated, "declared MPEG frame extends beyond this FLV tag", "body_truncated", at)
			break
		}
		out.CompleteFrames++
		at += length
		out.BytesCovered = at - start
	}
	out.UninspectedSpan = byteSpan(at, end-at)
	out.Diagnostics = d.items
	out.Complete = at == end && !hasError(out.Diagnostics)
	if at == end && !out.Complete && out.StopReason == "end" {
		out.StopReason = "diagnostics"
	}
	return out, nil
}

func byteSpan(offset, length uint64) cursor.Span {
	return cursor.Span{ByteOffset: offset, BitLength: uint32(length * 8)}
}
func sideInfoBytes(h FrameHeader) int {
	if h.Version == VersionMPEG1 {
		if h.Channels == 1 {
			return 17
		}
		return 32
	}
	if h.Channels == 1 {
		return 9
	}
	return 17
}
func crc16(header, side []byte) uint16 {
	crc := uint16(0xffff)
	for _, part := range [][]byte{header, side} {
		for _, value := range part {
			for bit := 7; bit >= 0; bit-- {
				top := crc >> 15
				crc <<= 1
				if top^uint16(value>>bit&1) != 0 {
					crc ^= 0x8005
				}
			}
		}
	}
	return crc
}

func parseVBR(h FrameHeader, available uint64, read func(uint64, uint64) ([]byte, error), d *diagnosticCollector, end uint64) (*VBRHeader, error) {
	// Xing's canonical offset does not move by two bytes for CRC protection.
	// VBRI is always at frame header + 32 bytes, independent of channel mode.
	for _, candidate := range []struct {
		offset uint64
		vbri   bool
	}{{uint64(4 + sideInfoBytes(h)), false}, {36, true}} {
		offset := candidate.offset
		if available < offset+4 {
			continue
		}
		marker, err := read(h.Span.ByteOffset+offset, 4)
		if err != nil {
			return nil, fmt.Errorf("mp3: read VBR signature: %w", err)
		}
		kind := string(marker)
		if candidate.vbri && kind != "VBRI" || !candidate.vbri && kind != "Xing" && kind != "Info" {
			continue
		}
		v := &VBRHeader{Kind: kind, Span: byteSpan(h.Span.ByteOffset+offset, 4)}
		next := offset + 4
		take := func(count uint64) ([]byte, error) {
			if count > available-next {
				return nil, io.ErrUnexpectedEOF
			}
			b, e := read(h.Span.ByteOffset+next, count)
			if e != nil {
				return nil, e
			}
			next += count
			v.Span = byteSpan(h.Span.ByteOffset+offset, next-offset)
			return b, nil
		}
		truncated := func() (*VBRHeader, error) {
			d.add(Diagnostic{Code: CodeVBRHeaderTruncated, Severity: SeverityError, Offset: h.Span.ByteOffset + next, Message: kind + " fields extend beyond the first MPEG frame", RecoveryOffset: number(end)})
			return v, nil
		}
		field := func() (*uint32, error) {
			b, e := take(4)
			if e != nil {
				return nil, e
			}
			n := binary.BigEndian.Uint32(b)
			return &n, nil
		}
		var e error
		if candidate.vbri {
			word := func() (*uint16, error) {
				b, e := take(2)
				if e != nil {
					return nil, e
				}
				value := binary.BigEndian.Uint16(b)
				return &value, nil
			}
			v.Version, e = word()
			if e == io.ErrUnexpectedEOF {
				return truncated()
			}
			if e != nil {
				return nil, e
			}
			if *v.Version != 1 {
				d.add(Diagnostic{Code: CodeVBRIVersionUnsupported, Severity: SeverityError, Offset: v.Span.ByteOffset + 4, Message: "only VBRI version 1 fixed fields are supported", Actual: number(uint64(*v.Version)), RecoveryOffset: number(end)})
				return v, nil
			}
			v.Delay, e = word()
			if e == nil {
				var quality *uint16
				quality, e = word()
				if e == nil {
					value := uint32(*quality)
					v.Quality = &value
				}
			}
			if e == io.ErrUnexpectedEOF {
				return truncated()
			}
			if e != nil {
				return nil, e
			}
			v.DeclaredBytes, e = field()
			if e == nil {
				v.DeclaredFrames, e = field()
			}
		} else {
			v.Flags, e = field()
			if e == nil && *v.Flags&1 != 0 {
				v.DeclaredFrames, e = field()
			}
			if e == nil && *v.Flags&2 != 0 {
				v.DeclaredBytes, e = field()
			}
			if e == nil && *v.Flags&4 != 0 {
				off := h.Span.ByteOffset + next
				_, e = take(100)
				if e == nil {
					s := byteSpan(off, 100)
					v.TOCSpan = &s
				}
			}
			if e == nil && *v.Flags&8 != 0 {
				v.Quality, e = field()
			}
			if e == nil && *v.Flags&^uint32(15) != 0 {
				d.add(Diagnostic{Code: CodeVBRFlagsUnsupported, Severity: SeverityError, Offset: v.Span.ByteOffset + 4, Message: "unknown Xing/Info flags; only standard optional fields are inspected", Actual: number(uint64(*v.Flags)), RecoveryOffset: number(end)})
				return v, nil
			}
		}
		if e == io.ErrUnexpectedEOF {
			return truncated()
		}
		if e != nil {
			return nil, e
		}
		v.Complete = true
		return v, nil
	}
	return nil, nil
}
