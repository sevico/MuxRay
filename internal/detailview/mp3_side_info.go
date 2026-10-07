package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/mp3"
)

func (b *builder) mp3SideInfo(parent string, frame mp3.Frame) {
	p := frame.SideInfo
	id := b.add(parent, fmt.Sprintf("Side info Frame %d", frame.Index), "Layer III side information", fmt.Sprintf("complete=%t; %d of %d bits inspected", p.Complete, p.BitsConsumed, p.DeclaredBytes*8), span(p.Span))
	b.mp3SideUint(id, "mainDataBegin", p.MainDataBegin)
	b.mp3SideUint(id, "privateBits", p.PrivateBits)
	for _, scfsi := range p.SCFSIChannels {
		ch := b.add(id, fmt.Sprintf("SCFSI channel %d", scfsi.Channel), "scale-factor selection", "encoded reuse flags; scale factors not decoded", nil)
		for i, flag := range scfsi.Flags {
			b.mp3SideBool(ch, fmt.Sprintf("scfsi[%d]", i), flag)
		}
	}
	for _, granule := range p.Granules {
		g := b.add(id, fmt.Sprintf("Granule %d", granule.Index), "granule", "encoded side information", nil)
		for _, channel := range granule.Channels {
			c := b.add(g, fmt.Sprintf("Channel %d", channel.Index), "channel", "encoded side information", nil)
			b.mp3SideUint(c, "part23Length", channel.Part23Length)
			b.mp3SideUint(c, "bigValues", channel.BigValues)
			b.mp3SideUint(c, "globalGain", channel.GlobalGain)
			b.mp3SideUint(c, "scalefacCompress", channel.ScalefacCompress)
			b.mp3SideBool(c, "windowSwitchingFlag", channel.WindowSwitchingFlag)
			b.mp3SideUint(c, "blockType", channel.BlockType)
			b.mp3SideBool(c, "mixedBlockFlag", channel.MixedBlockFlag)
			for i, field := range channel.TableSelect {
				b.mp3SideUint(c, fmt.Sprintf("tableSelect[%d]", i), field)
			}
			for i, field := range channel.SubblockGain {
				b.mp3SideUint(c, fmt.Sprintf("subblockGain[%d]", i), field)
			}
			b.mp3SideUint(c, "region0Count", channel.Region0Count)
			b.mp3SideUint(c, "region1Count", channel.Region1Count)
			b.mp3SideBool(c, "preflag", channel.Preflag)
			b.mp3SideBool(c, "scalefacScale", channel.ScalefacScale)
			b.mp3SideBool(c, "count1TableSelect", channel.Count1TableSelect)
		}
	}
}

func (b *builder) mp3SideUint(parent, name string, p *mp3.UnsignedField) {
	if p != nil {
		b.add(parent, name, "encoded unsigned", decimal(uint64(p.Value)), span(p.Span))
	}
}
func (b *builder) mp3SideBool(parent, name string, p *mp3.BooleanField) {
	if p != nil {
		b.add(parent, name, "encoded boolean", strconv.FormatBool(p.Value), span(p.Span))
	}
}
