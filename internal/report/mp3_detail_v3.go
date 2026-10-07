package report

import (
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

// The shared DTO gains optional members, but only the v3 constructor publishes
// them. Earlier constructors and their closed schemas retain their exact shape.
func NewMP3TagDetailV3(size uint64, tag flv.TagRecord, p flv.MP3Audio) MP3TagDetailDocumentV2 {
	d := NewMP3TagDetailV2(size, tag, p)
	d.SchemaVersion = 3
	if p.Frames != nil {
		for i, f := range p.Frames.Frames {
			d.Detail.Frames.Items[i].SideInfo = mapMP3SideInfo(f.SideInfo)
			if x := f.Dependency; x != nil {
				d.Detail.Frames.Items[i].Dependency = &MP3DependencyDetail{
					State: x.State, MainDataSpan: mapSpan(x.MainDataSpan), KnownPreviousBytes: x.KnownPreviousBytes,
					MissingPreviousBytes: x.MissingPreviousBytes, Part23Bits: x.Part23Bits, AvailableBits: x.AvailableBits,
				}
			}
		}
	}
	return d
}

type MP3SideInfoDetail struct {
	Span          Span                 `json:"span"`
	DeclaredBytes uint32               `json:"declaredBytes"`
	BitsConsumed  uint32               `json:"bitsConsumed"`
	MainDataBegin *MP3UnsignedField    `json:"mainDataBegin,omitempty"`
	PrivateBits   *MP3UnsignedField    `json:"privateBits,omitempty"`
	SCFSIChannels []MP3SCFSIChannel    `json:"scfsiChannels"`
	Granules      []MP3SideInfoGranule `json:"granules"`
	Complete      bool                 `json:"complete"`
}
type MP3SCFSIChannel struct {
	Channel uint32            `json:"channel"`
	Flags   []MP3BooleanField `json:"flags"`
}
type MP3SideInfoGranule struct {
	Index    uint32               `json:"index"`
	Channels []MP3SideInfoChannel `json:"channels"`
}
type MP3SideInfoChannel struct {
	Index               uint32             `json:"index"`
	Part23Length        *MP3UnsignedField  `json:"part23Length,omitempty"`
	BigValues           *MP3UnsignedField  `json:"bigValues,omitempty"`
	GlobalGain          *MP3UnsignedField  `json:"globalGain,omitempty"`
	ScalefacCompress    *MP3UnsignedField  `json:"scalefacCompress,omitempty"`
	WindowSwitchingFlag *MP3BooleanField   `json:"windowSwitchingFlag,omitempty"`
	BlockType           *MP3UnsignedField  `json:"blockType,omitempty"`
	MixedBlockFlag      *MP3BooleanField   `json:"mixedBlockFlag,omitempty"`
	TableSelect         []MP3UnsignedField `json:"tableSelect"`
	SubblockGain        []MP3UnsignedField `json:"subblockGain"`
	Region0Count        *MP3UnsignedField  `json:"region0Count,omitempty"`
	Region1Count        *MP3UnsignedField  `json:"region1Count,omitempty"`
	Preflag             *MP3BooleanField   `json:"preflag,omitempty"`
	ScalefacScale       *MP3BooleanField   `json:"scalefacScale,omitempty"`
	Count1TableSelect   *MP3BooleanField   `json:"count1TableSelect,omitempty"`
}
type MP3DependencyDetail struct {
	State                string  `json:"state"`
	MainDataSpan         Span    `json:"mainDataSpan"`
	KnownPreviousBytes   uint32  `json:"knownPreviousBytes"`
	MissingPreviousBytes *uint32 `json:"missingPreviousBytes,omitempty"`
	Part23Bits           *uint32 `json:"part23Bits,omitempty"`
	AvailableBits        *uint32 `json:"availableBits,omitempty"`
}

func mapMP3SideInfo(p *mp3.SideInfo) *MP3SideInfoDetail {
	if p == nil {
		return nil
	}
	r := &MP3SideInfoDetail{Span: mapSpan(p.Span), DeclaredBytes: p.DeclaredBytes, BitsConsumed: p.BitsConsumed,
		MainDataBegin: mapMP3SideUint(p.MainDataBegin), PrivateBits: mapMP3SideUint(p.PrivateBits),
		SCFSIChannels: []MP3SCFSIChannel{}, Granules: []MP3SideInfoGranule{}, Complete: p.Complete}
	for _, scfsi := range p.SCFSIChannels {
		c := MP3SCFSIChannel{Channel: scfsi.Channel, Flags: []MP3BooleanField{}}
		for _, flag := range scfsi.Flags {
			c.Flags = append(c.Flags, *mapMP3SideBool(flag))
		}
		r.SCFSIChannels = append(r.SCFSIChannels, c)
	}
	for _, granule := range p.Granules {
		g := MP3SideInfoGranule{Index: granule.Index, Channels: []MP3SideInfoChannel{}}
		for _, ch := range granule.Channels {
			c := MP3SideInfoChannel{Index: ch.Index, Part23Length: mapMP3SideUint(ch.Part23Length),
				BigValues: mapMP3SideUint(ch.BigValues), GlobalGain: mapMP3SideUint(ch.GlobalGain), ScalefacCompress: mapMP3SideUint(ch.ScalefacCompress),
				WindowSwitchingFlag: mapMP3SideBool(ch.WindowSwitchingFlag), BlockType: mapMP3SideUint(ch.BlockType), MixedBlockFlag: mapMP3SideBool(ch.MixedBlockFlag),
				TableSelect: []MP3UnsignedField{}, SubblockGain: []MP3UnsignedField{}, Region0Count: mapMP3SideUint(ch.Region0Count), Region1Count: mapMP3SideUint(ch.Region1Count),
				Preflag: mapMP3SideBool(ch.Preflag), ScalefacScale: mapMP3SideBool(ch.ScalefacScale), Count1TableSelect: mapMP3SideBool(ch.Count1TableSelect)}
			for _, field := range ch.TableSelect {
				c.TableSelect = append(c.TableSelect, *mapMP3SideUint(field))
			}
			for _, field := range ch.SubblockGain {
				c.SubblockGain = append(c.SubblockGain, *mapMP3SideUint(field))
			}
			g.Channels = append(g.Channels, c)
		}
		r.Granules = append(r.Granules, g)
	}
	return r
}

func mapMP3SideUint(p *mp3.UnsignedField) *MP3UnsignedField {
	if p == nil {
		return nil
	}
	return &MP3UnsignedField{Value: uint64(p.Value), Span: mapSpan(p.Span)}
}
func mapMP3SideBool(p *mp3.BooleanField) *MP3BooleanField {
	if p == nil {
		return nil
	}
	return &MP3BooleanField{Value: p.Value, Span: mapSpan(p.Span)}
}
