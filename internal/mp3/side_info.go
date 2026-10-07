package mp3

import "github.com/sevico/muxray/internal/cursor"

const (
	CodeSideInfoTruncated     = "MP3_SIDE_INFO_TRUNCATED"
	CodeSideInfoValueInvalid  = "MP3_SIDE_INFO_VALUE_INVALID"
	CodeMainDataLengthInvalid = "MP3_MAIN_DATA_LENGTH_INVALID"
)

type UnsignedField struct {
	Value uint32
	Span  cursor.Span
}
type BooleanField struct {
	Value bool
	Span  cursor.Span
}

// SideInfo is fixed Layer III side information, never decoded scale factors or
// Huffman data. Complete describes syntax and checked structural constraints.
type SideInfo struct {
	Span                        cursor.Span
	DeclaredBytes, BitsConsumed uint32
	MainDataBegin, PrivateBits  *UnsignedField
	SCFSIChannels               []SCFSIChannel
	Granules                    []SideInfoGranule
	Complete                    bool
}

type SCFSIChannel struct {
	Channel uint32
	Flags   []*BooleanField // Four encoded flags, in source order; MPEG-1 only.
}

type SideInfoGranule struct {
	Index    uint32
	Channels []SideInfoChannel
}

type SideInfoChannel struct {
	Index                                                 uint32
	Part23Length, BigValues, GlobalGain, ScalefacCompress *UnsignedField
	WindowSwitchingFlag                                   *BooleanField
	BlockType                                             *UnsignedField // Absent when window_switching_flag is zero.
	MixedBlockFlag                                        *BooleanField
	TableSelect                                           []*UnsignedField
	SubblockGain                                          []*UnsignedField
	Region0Count, Region1Count                            *UnsignedField
	Preflag                                               *BooleanField // MPEG-1 only; LSF does not encode this field.
	ScalefacScale, Count1TableSelect                      *BooleanField
}

// MainDataDependency describes the physical main-data slot window implied by
// main_data_begin. It does not assert unused reservoir capacity, valid Huffman
// data, or independent decoding. No main-data bytes are read or reconstructed.
type MainDataDependency struct {
	State        string // none, within_tag, before_tag, or unknown
	MainDataSpan cursor.Span
	// KnownPreviousBytes counts physical main-data slots in this Tag's
	// uninterrupted compatible prefix, including potentially consumed bytes.
	KnownPreviousBytes   uint32
	MissingPreviousBytes *uint32
	// AvailableBits counts the visible suffix of the declared backward window
	// plus current slots. When before_tag, it is not a contiguous known prefix
	// of the audio data and does not establish that Part23Bits are available.
	Part23Bits, AvailableBits *uint32
}

func parseSideInfo(data []byte, start uint64, header FrameHeader, diagnostics *diagnosticCollector, recovery uint64) *SideInfo {
	s := &SideInfo{Span: byteSpan(start, uint64(len(data))), DeclaredBytes: uint32(sideInfoBytes(header)), SCFSIChannels: []SCFSIChannel{}, Granules: []SideInfoGranule{}}
	p := sideInfoReader{data: data, start: start, diagnostics: diagnostics, recovery: recovery}
	if uint32(len(data)) < s.DeclaredBytes {
		diagnostics.add(Diagnostic{Code: CodeSideInfoTruncated, Severity: SeverityError, Offset: start + uint64(len(data)), Message: "fixed Layer III side information is truncated", Expected: number(uint64(s.DeclaredBytes)), Actual: number(uint64(len(data))), RecoveryOffset: number(recovery)})
	}
	lsf := header.Version != VersionMPEG1
	granules := uint32(2)
	if lsf {
		s.MainDataBegin = p.uint(8)
		s.PrivateBits = p.uint(uint32(header.Channels))
		granules = 1
	} else {
		s.MainDataBegin = p.uint(9)
		privateWidth := uint32(3)
		if header.Channels == 1 {
			privateWidth = 5
		}
		s.PrivateBits = p.uint(privateWidth)
		for ch := uint32(0); ch < uint32(header.Channels) && !p.truncated; ch++ {
			entry := SCFSIChannel{Channel: ch, Flags: []*BooleanField{}}
			for i := 0; i < 4; i++ {
				if flag := p.flag(); flag != nil {
					entry.Flags = append(entry.Flags, flag)
				}
			}
			s.SCFSIChannels = append(s.SCFSIChannels, entry)
		}
	}
	for gr := uint32(0); gr < granules && !p.truncated; gr++ {
		g := SideInfoGranule{Index: gr, Channels: []SideInfoChannel{}}
		for ch := uint32(0); ch < uint32(header.Channels) && !p.truncated; ch++ {
			c := SideInfoChannel{Index: ch, TableSelect: []*UnsignedField{}, SubblockGain: []*UnsignedField{}}
			c.Part23Length = p.uint(12)
			c.BigValues = p.uint(9)
			if c.BigValues != nil && c.BigValues.Value > 288 {
				p.invalid(c.BigValues.Span, "big_values exceeds 288", 288, uint64(c.BigValues.Value))
			}
			c.GlobalGain = p.uint(8)
			if lsf {
				c.ScalefacCompress = p.uint(9)
			} else {
				c.ScalefacCompress = p.uint(4)
			}
			c.WindowSwitchingFlag = p.flag()
			if c.WindowSwitchingFlag != nil && c.WindowSwitchingFlag.Value {
				c.BlockType = p.uint(2)
				if c.BlockType != nil && c.BlockType.Value == 0 {
					p.invalid(c.BlockType.Span, "block_type must be nonzero when window_switching_flag is set", 1, 0)
				}
				c.MixedBlockFlag = p.flag()
				for i := 0; i < 2; i++ {
					if field := p.uint(5); field != nil {
						c.TableSelect = append(c.TableSelect, field)
					}
				}
				for i := 0; i < 3; i++ {
					if field := p.uint(3); field != nil {
						c.SubblockGain = append(c.SubblockGain, field)
					}
				}
			} else {
				for i := 0; i < 3; i++ {
					if field := p.uint(5); field != nil {
						c.TableSelect = append(c.TableSelect, field)
					}
				}
				c.Region0Count = p.uint(4)
				c.Region1Count = p.uint(3)
			}
			if !lsf {
				c.Preflag = p.flag()
			}
			c.ScalefacScale = p.flag()
			c.Count1TableSelect = p.flag()
			g.Channels = append(g.Channels, c)
		}
		s.Granules = append(s.Granules, g)
	}
	s.BitsConsumed = p.position
	s.Complete = !p.truncated && !p.bad && p.position == s.DeclaredBytes*8
	return s
}

type sideInfoReader struct {
	data           []byte
	start          uint64
	position       uint32
	truncated, bad bool
	diagnostics    *diagnosticCollector
	recovery       uint64
}

func (p *sideInfoReader) uint(width uint32) *UnsignedField {
	if p.truncated {
		return nil
	}
	if width > uint32(len(p.data)*8)-p.position {
		p.position = uint32(len(p.data) * 8)
		p.truncated = true
		return nil
	}
	span := frameBitSpan(p.start, uint8(p.position), width)
	value := uint32(0)
	for i := uint32(0); i < width; i++ {
		value = value<<1 | uint32(p.data[p.position/8]>>(7-p.position%8)&1)
		p.position++
	}
	return &UnsignedField{Value: value, Span: span}
}
func (p *sideInfoReader) flag() *BooleanField {
	field := p.uint(1)
	if field == nil {
		return nil
	}
	return &BooleanField{Value: field.Value != 0, Span: field.Span}
}
func (p *sideInfoReader) invalid(span cursor.Span, message string, expected, actual uint64) {
	p.bad = true
	p.diagnostics.add(Diagnostic{Code: CodeSideInfoValueInvalid, Severity: SeverityError, Offset: span.ByteOffset, BitOffset: span.BitOffset, Message: message, Expected: number(expected), Actual: number(actual), RecoveryOffset: number(p.recovery)})
}

type mainDataHistory struct {
	known        uint32
	fromTagStart bool
	header       *FrameHeader
}

func (h *mainDataHistory) inspect(frame *Frame, diagnostics *diagnosticCollector, recovery uint64) {
	current := frame.Header
	if h.header != nil && (h.header.Version != current.Version || h.header.SamplingFrequencyHz != current.SamplingFrequencyHz || h.header.Channels != current.Channels) {
		h.known = 0
		h.fromTagStart = false
	}
	copy := current
	h.header = &copy
	prefix := uint64(4 + sideInfoBytes(current))
	if current.CRCProtected {
		prefix += 2
	}
	frameBytes := uint64(frame.Span.BitLength / 8)
	mainStart := frame.Span.ByteOffset + min(prefix, frameBytes)
	mainBytes := frameBytes - min(prefix, frameBytes)
	d := &MainDataDependency{State: "unknown", MainDataSpan: byteSpan(mainStart, mainBytes), KnownPreviousBytes: h.known}
	frame.Dependency = d
	if frame.SideInfo == nil || !frame.SideInfo.Complete || !frame.BoundaryComplete || (frame.CRC.Status != "not_protected" && frame.CRC.Status != "match") {
		h.known = 0
		h.fromTagStart = false
		return
	}
	var bits uint32
	for _, granule := range frame.SideInfo.Granules {
		for _, channel := range granule.Channels {
			bits += channel.Part23Length.Value
		}
	}
	back := frame.SideInfo.MainDataBegin.Value
	available := (min(back, h.known) + uint32(mainBytes)) * 8
	d.Part23Bits, d.AvailableBits = &bits, &available
	switch {
	case back == 0:
		d.State = "none"
	case back <= h.known:
		d.State = "within_tag"
	case h.fromTagStart:
		d.State = "before_tag"
		missing := back - h.known
		d.MissingPreviousBytes = &missing
	}
	declaredWindow := (uint64(back) + mainBytes) * 8
	if uint64(bits) > declaredWindow {
		span := frame.SideInfo.MainDataBegin.Span
		diagnostics.add(Diagnostic{Code: CodeMainDataLengthInvalid, Severity: SeverityError, Offset: span.ByteOffset, BitOffset: span.BitOffset, Message: "part2_3_length total exceeds the declared backward distance plus this frame's main-data slots", Expected: number(declaredWindow), Actual: number(uint64(bits)), RecoveryOffset: number(recovery)})
		d.State = "unknown"
		h.known = 0
		h.fromTagStart = false
		return
	}
	h.known += uint32(mainBytes)
}
