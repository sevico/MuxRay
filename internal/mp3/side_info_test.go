package mp3

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/sevico/muxray/internal/cursor"
)

type sideBits struct {
	data []byte
	pos  uint32
}

func (b *sideBits) put(value uint32, width uint32) {
	for i := int(width) - 1; i >= 0; i-- {
		if b.pos%8 == 0 {
			b.data = append(b.data, 0)
		}
		b.data[b.pos/8] |= byte(value>>uint(i)&1) << (7 - b.pos%8)
		b.pos++
	}
}

type sideFixture struct {
	version                   Version
	mono, protected, switched bool
	back, part, big, block    uint32
}

func sideFixtureHeader(f sideFixture) [4]byte {
	raw := uint32(0x7ff<<21) | uint32(f.version)<<19 | uint32(LayerIII)<<17 | uint32(9<<12)
	if !f.protected {
		raw |= 1 << 16
	}
	if f.mono {
		raw |= uint32(ChannelModeSingleChannel) << 6
	}
	var h [4]byte
	binary.BigEndian.PutUint32(h[:], raw)
	return h
}
func sideFixtureFrame(f sideFixture) []byte {
	header := sideFixtureHeader(f)
	frame := mpegFrame(header)
	channels, granules := uint32(2), uint32(2)
	if f.mono {
		channels = 1
	}
	var b sideBits
	if f.version == VersionMPEG1 {
		b.put(f.back, 9)
		if f.mono {
			b.put(31, 5)
		} else {
			b.put(7, 3)
		}
		for ch := uint32(0); ch < channels; ch++ {
			b.put(0xa+ch, 4)
		}
	} else {
		b.put(f.back, 8)
		b.put((1<<channels)-1, channels)
		granules = 1
	}
	for gr := uint32(0); gr < granules; gr++ {
		for ch := uint32(0); ch < channels; ch++ {
			b.put(f.part, 12)
			b.put(f.big, 9)
			b.put(190+gr*2+ch, 8)
			if f.version == VersionMPEG1 {
				b.put(10, 4)
			} else {
				b.put(511, 9)
			}
			if f.switched {
				b.put(1, 1)
				b.put(f.block, 2)
				b.put(1, 1)
				b.put(1, 5)
				b.put(2, 5)
				for i := uint32(0); i < 3; i++ {
					b.put(3+i, 3)
				}
			} else {
				b.put(0, 1)
				b.put(1, 5)
				b.put(2, 5)
				b.put(3, 5)
				b.put(6, 4)
				b.put(4, 3)
			}
			if f.version == VersionMPEG1 {
				b.put(1, 1)
			}
			b.put(1, 1)
			b.put(0, 1)
		}
	}
	at := 4
	if f.protected {
		at = 6
	}
	copy(frame[at:], b.data)
	if f.protected {
		message := append(append([]byte{}, header[2:]...), b.data...)
		binary.BigEndian.PutUint16(frame[4:6], referenceCRC(message))
	}
	return frame
}

func TestSideInfoVersionsChannelsAndBlockBranches(t *testing.T) {
	for _, version := range []Version{VersionMPEG1, VersionMPEG2, VersionMPEG25} {
		for _, mono := range []bool{false, true} {
			for _, protected := range []bool{false, true} {
				for _, switched := range []bool{false, true} {
					f := sideFixture{version: version, mono: mono, protected: protected, switched: switched, back: 0, part: 100, big: 37, block: 2}
					t.Run(fmt.Sprintf("%s/mono%t/crc%t/block%t", version, mono, protected, switched), func(t *testing.T) {
						frame := sideFixtureFrame(f)
						r := sequence(t, frame, Options{InspectSideInfo: true})
						if !r.Complete || len(r.Frames) != 1 {
							t.Fatalf("parse %+v", r.Diagnostics)
						}
						s := r.Frames[0].SideInfo
						if s == nil || !s.Complete || s.BitsConsumed != s.DeclaredBytes*8 || s.Span.BitLength != s.BitsConsumed || s.MainDataBegin.Value != 0 {
							t.Fatalf("side %+v", s)
						}
						channels, granules := 2, 2
						if mono {
							channels = 1
						}
						if version != VersionMPEG1 {
							granules = 1
						}
						if len(s.Granules) != granules || len(s.Granules[0].Channels) != channels {
							t.Fatal("granule/channel layout")
						}
						if version == VersionMPEG1 {
							if len(s.SCFSIChannels) != channels || len(s.SCFSIChannels[0].Flags) != 4 || !s.SCFSIChannels[0].Flags[0].Value || s.SCFSIChannels[0].Flags[1].Value {
								t.Fatal("SCFSI bits")
							}
						} else if len(s.SCFSIChannels) != 0 {
							t.Fatal("invented LSF SCFSI")
						}
						for gi, g := range s.Granules {
							for ci, c := range g.Channels {
								if g.Index != uint32(gi) || c.Index != uint32(ci) || c.Part23Length.Value != 100 || c.BigValues.Value != 37 || c.GlobalGain.Value != uint32(190+gi*2+ci) {
									t.Fatalf("channel %+v", c)
								}
								if version == VersionMPEG1 {
									if c.Preflag == nil || !c.Preflag.Value || c.ScalefacCompress.Value != 10 {
										t.Fatal("MPEG1 fields")
									}
								} else if c.Preflag != nil || c.ScalefacCompress.Value != 511 {
									t.Fatal("LSF fields")
								}
								if switched {
									if c.BlockType.Value != 2 || !c.MixedBlockFlag.Value || len(c.TableSelect) != 2 || len(c.SubblockGain) != 3 || c.Region0Count != nil {
										t.Fatal("switched block")
									}
								} else if c.BlockType != nil || c.MixedBlockFlag != nil || len(c.TableSelect) != 3 || len(c.SubblockGain) != 0 || c.Region0Count.Value != 6 || c.Region1Count.Value != 4 {
									t.Fatal("long block")
								}
							}
						}
						checkSideFieldSpans(t, reflect.ValueOf(s), frame)
						d := r.Frames[0].Dependency
						if d.State != "none" || *d.Part23Bits != uint32(granules*channels*100) || d.KnownPreviousBytes != 0 || d.MissingPreviousBytes != nil {
							t.Fatalf("dependency %+v", d)
						}
						legacy := sequence(t, frame, Options{})
						if legacy.Frames[0].SideInfo != nil || legacy.Frames[0].Dependency != nil {
							t.Fatal("side info leaked into legacy")
						}
						wantExtra := uint64(s.DeclaredBytes)
						if protected {
							wantExtra = 0
						}
						if r.BytesRead-legacy.BytesRead != wantExtra {
							t.Fatalf("unexpected side reads: deep %d legacy %d", r.BytesRead, legacy.BytesRead)
						}
					})
				}
			}
		}
	}
}

func checkSideFieldSpans(t testing.TB, value reflect.Value, data []byte) {
	t.Helper()
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		checkSideFieldSpans(t, value.Elem(), data)
		return
	}
	if value.Type() == reflect.TypeOf(UnsignedField{}) {
		f := value.Interface().(UnsignedField)
		if extractSideBits(t, data, f.Span) != f.Value {
			t.Fatalf("encoded uint span %+v", f)
		}
		return
	}
	if value.Type() == reflect.TypeOf(BooleanField{}) {
		f := value.Interface().(BooleanField)
		if (extractSideBits(t, data, f.Span) != 0) != f.Value {
			t.Fatalf("encoded flag span %+v", f)
		}
		return
	}
	switch value.Kind() {
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			checkSideFieldSpans(t, value.Field(i), data)
		}
	case reflect.Slice:
		for i := 0; i < value.Len(); i++ {
			checkSideFieldSpans(t, value.Index(i), data)
		}
	}
}
func extractSideBits(t testing.TB, data []byte, s cursor.Span) uint32 {
	t.Helper()
	if s.BitLength == 0 || s.BitLength > 32 || s.BitOffset > 7 || s.ByteOffset*8+uint64(s.BitOffset)+uint64(s.BitLength) > uint64(len(data))*8 {
		t.Fatalf("invalid field span %+v", s)
	}
	var v uint32
	for i := uint32(0); i < s.BitLength; i++ {
		bit := s.ByteOffset*8 + uint64(s.BitOffset) + uint64(i)
		v = v<<1 | uint32(data[bit/8]>>(7-bit%8)&1)
	}
	return v
}

func TestSideInfoPartialAndInvalidValues(t *testing.T) {
	f := sideFixture{version: VersionMPEG1, part: 20, big: 1}
	frame := sideFixtureFrame(f)
	for cut := 4; cut < 36; cut++ {
		r := sequence(t, frame[:cut], Options{InspectSideInfo: true})
		s := r.Frames[0].SideInfo
		if r.Complete || s == nil || s.Complete || !hasCode(r, CodeSideInfoTruncated) || s.Span.BitLength != uint32((cut-4)*8) || s.BitsConsumed > s.Span.BitLength || r.Frames[0].Dependency.State != "unknown" {
			t.Fatalf("partial %d %+v", cut, r)
		}
		if cut == 5 && s.MainDataBegin != nil {
			t.Fatal("invented partial main_data_begin")
		}
		checkSideFieldSpans(t, reflect.ValueOf(s), frame[:cut])
	}
	for _, fixture := range []sideFixture{{version: VersionMPEG1, big: 289}, {version: VersionMPEG2, switched: true, block: 0}} {
		r := sequence(t, sideFixtureFrame(fixture), Options{InspectSideInfo: true})
		if r.Complete || r.Frames[0].SideInfo.Complete || !hasCode(r, CodeSideInfoValueInvalid) || r.Frames[0].Dependency.State != "unknown" {
			t.Fatalf("invalid side accepted %+v", r)
		}
		if legacy := sequence(t, sideFixtureFrame(fixture), Options{}); !legacy.Complete {
			t.Fatal("new syntax changed opt-out")
		}
	}
	// A full side-info block can still describe an impossible main-data size.
	f.part = 4095
	r := sequence(t, sideFixtureFrame(f), Options{InspectSideInfo: true})
	if r.Complete || !r.Frames[0].SideInfo.Complete || !hasCode(r, CodeMainDataLengthInvalid) || r.Frames[0].Dependency.State != "unknown" {
		t.Fatalf("impossible part lengths: %+v", r)
	}
}

func TestSideInfoMainDataDependencyGeometry(t *testing.T) {
	f := sideFixture{version: VersionMPEG1, part: 100, big: 10}
	first := sideFixtureFrame(f)
	f.back = 200
	second := sideFixtureFrame(f)
	f.back = 511
	third := sideFixtureFrame(f)
	r := sequence(t, bytes.Join([][]byte{first, second, third}, nil), Options{InspectSideInfo: true})
	if !r.Complete {
		t.Fatalf("dependency: %+v", r.Diagnostics)
	}
	for i, want := range []string{"none", "within_tag", "within_tag"} {
		if r.Frames[i].Dependency.State != want {
			t.Fatalf("frame %d dependency %+v", i, r.Frames[i].Dependency)
		}
	}
	if r.Frames[1].Dependency.KnownPreviousBytes != 381 || r.Frames[2].Dependency.KnownPreviousBytes != 762 || *r.Frames[2].Dependency.AvailableBits != (511+381)*8 {
		t.Fatal("header/side-info incorrectly included in history")
	}
	// A Tag can start in the middle of an elementary stream. A declared
	// predecessor outside this Tag is missing context, not malformed audio.
	r = sequence(t, third, Options{InspectSideInfo: true})
	d := r.Frames[0].Dependency
	if !r.Complete || d.State != "before_tag" || *d.MissingPreviousBytes != 511 || *d.AvailableBits != 381*8 {
		t.Fatalf("before-tag %+v %+v", d, r.Diagnostics)
	}
	// Format changes break the history, but a later independently specified
	// frame can establish a known same-format physical range again.
	f.version = VersionMPEG2
	f.back = 20
	changed := sideFixtureFrame(f)
	f.back = 0
	independent := sideFixtureFrame(f)
	f.back = 10
	after := sideFixtureFrame(f)
	r = sequence(t, bytes.Join([][]byte{first, changed, independent, after}, nil), Options{InspectSideInfo: true})
	for i, want := range []string{"none", "unknown", "none", "within_tag"} {
		if r.Frames[i].Dependency.State != want {
			t.Fatalf("changed frame %d %+v", i, r.Frames[i].Dependency)
		}
	}
	// Corrupted side info cannot supply history for the following backpointer.
	bad := sideFixtureFrame(sideFixture{version: VersionMPEG1, big: 300})
	r = sequence(t, append(bad, second...), Options{InspectSideInfo: true})
	if r.Frames[1].Dependency.State != "unknown" || r.Frames[1].Dependency.KnownPreviousBytes != 0 {
		t.Fatal("invalid preceding frame supplied history")
	}
}

func TestSideInfoCRCAndReadBounds(t *testing.T) {
	f := sideFixture{version: VersionMPEG1, protected: true, part: 100, big: 10}
	frame := sideFixtureFrame(f)
	frame[4] ^= 1
	r := sequence(t, frame, Options{InspectSideInfo: true})
	if !r.Frames[0].SideInfo.Complete || r.Frames[0].Dependency.State != "unknown" || r.Frames[0].Dependency.Part23Bits != nil {
		t.Fatal("CRC failure trusted for dependency")
	}
	for _, cut := range []int{4, 5, 6, 7, 37} {
		r = sequence(t, frame[:cut], Options{InspectSideInfo: true})
		s := r.Frames[0].SideInfo
		_, end, err := s.Span.ByteRange()
		if err != nil || end > uint64(cut) || s.Complete || !hasCode(r, CodeSideInfoTruncated) {
			t.Fatalf("protected prefix cut%d: %+v", cut, s)
		}
	}
	for _, h := range [][4]byte{stereoHeader, {0xff, 0xfa, 0x90, 0x64}} {
		frame := mpegFrame(h)
		for _, full := range []bool{false, true} {
			failure := errors.New("cancelled")
			_, err := ParseFrameSequence(sequenceErrorReader{data: frame, at: 4, full: full, err: failure}, byteSpan(0, uint64(len(frame))), Options{InspectSideInfo: true})
			if !errors.Is(err, failure) {
				t.Fatalf("source failure swallowed %v", err)
			}
		}
	}
	f.protected = false
	frame = sideFixtureFrame(f)
	r = sequence(t, bytes.Repeat(frame, 20), Options{InspectSideInfo: true, MaxFrames: 3})
	if len(r.Frames) != 3 || r.Complete || !hasCode(r, CodeFrameLimitReached) || r.BytesRead > 3*(4+32)+8 {
		t.Fatalf("side budget %+v", r)
	}
	options, err := (Options{InspectSideInfo: true}).Normalized()
	if err != nil || !options.InspectFrames {
		t.Fatal("side option did not enable sequence")
	}
}

func FuzzSideInformation(f *testing.F) {
	for _, v := range []Version{VersionMPEG1, VersionMPEG2, VersionMPEG25} {
		f.Add(sideFixtureFrame(sideFixture{version: v, part: 100, big: 10}))
		f.Add(sideFixtureFrame(sideFixture{version: v, mono: true, protected: true, switched: true, block: 2, back: 10}))
	}
	f.Add([]byte{0xff, 0xfb, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}
		opts := Options{InspectSideInfo: true, MaxFrames: 8, MaxDiagnostics: 8}
		r, err := ParseFrameSequence(bytes.NewReader(data), byteSpan(0, uint64(len(data))), opts)
		if err != nil {
			t.Fatal(err)
		}
		again, e := ParseFrameSequence(bytes.NewReader(data), byteSpan(0, uint64(len(data))), opts)
		if e != nil || !reflect.DeepEqual(r, again) {
			t.Fatal("non-deterministic")
		}
		if len(r.Frames) > 8 || len(r.Diagnostics) > 8 || r.BytesRead > 8*(4+2+32)+256 {
			t.Fatal("side-info read budget")
		}
		for _, frame := range r.Frames {
			if s := frame.SideInfo; s != nil {
				if s.BitsConsumed > s.DeclaredBytes*8 || s.Span.BitLength > s.DeclaredBytes*8 || len(s.Granules) > 2 {
					t.Fatal("side-info extent")
				}
				checkSideFieldSpans(t, reflect.ValueOf(s), data)
			}
		}
	})
}

func TestSideInfoAbsoluteSourceSpans(t *testing.T) {
	frame := sideFixtureFrame(sideFixture{version: VersionMPEG1, part: 100, big: 10, back: 40})
	data := append(make([]byte, 101), frame...)
	r, err := ParseFrameSequence(bytes.NewReader(data), byteSpan(101, uint64(len(frame))), Options{InspectSideInfo: true})
	if err != nil || !r.Complete {
		t.Fatalf("absolute source: %v %+v", err, r.Diagnostics)
	}
	s := r.Frames[0].SideInfo
	if s.Span.ByteOffset != 105 || s.MainDataBegin.Span.ByteOffset != 105 || r.Frames[0].Dependency.MainDataSpan.ByteOffset != 137 {
		t.Fatal("source base lost")
	}
	checkSideFieldSpans(t, reflect.ValueOf(s), data)
}

func TestMainDataUnknownKeepsComputableBeforeTagGap(t *testing.T) {
	frame := sideFixtureFrame(sideFixture{version: VersionMPEG1, back: 200, part: 4095, big: 10})
	r := sequence(t, frame, Options{InspectSideInfo: true})
	d := r.Frames[0].Dependency
	if r.Complete || !hasCode(r, CodeMainDataLengthInvalid) || d.State != "unknown" || d.MissingPreviousBytes == nil || *d.MissingPreviousBytes != 200 || d.Part23Bits == nil || *d.Part23Bits != 16380 || d.AvailableBits == nil || *d.AvailableBits != 3048 {
		t.Fatalf("invalid data length discarded independently computable geometry: %+v %+v", d, r.Diagnostics)
	}
}
