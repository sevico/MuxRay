package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

func (b *builder) aac(p flv.AACAudio) {
	root := b.add("", "AAC audio", "aacAudio", "FLV AAC framing; raw access units are not decoded", span(p.Span))
	if p.HeaderPresent {
		h := p.Header
		id := b.add(root, "AudioTagHeader", "bitfield", fmt.Sprintf("0x%02x", h.Raw), span(h.Span))
		b.encoded(id, "soundFormat", "uint4", fmt.Sprintf("%d (AAC)", h.SoundFormat), h.SoundFormatSpan)
		rates := [...]string{"5.5 kHz", "11 kHz", "22 kHz", "44 kHz"}
		sizes := [...]string{"8-bit", "16-bit"}
		channels := [...]string{"mono", "stereo"}
		b.encoded(id, "soundRate", "uint2", fmt.Sprintf("%d (%s FLV hint; use ASC frequency)", h.SoundRate, rates[h.SoundRate]), h.SoundRateSpan)
		b.encoded(id, "soundSize", "uint1", fmt.Sprintf("%d (%s FLV hint)", h.SoundSize, sizes[h.SoundSize]), h.SoundSizeSpan)
		b.encoded(id, "soundType", "uint1", fmt.Sprintf("%d (%s FLV hint; use ASC channels)", h.SoundType, channels[h.SoundType]), h.SoundTypeSpan)
	}
	if p.PacketTypePresent {
		b.encoded(root, "AACPacketType", "uint8", fmt.Sprintf("%d (%s)", p.PacketType, p.PacketType), p.PacketTypeSpan)
	}
	if p.RawDataPresent {
		b.add(root, "rawAAC", "opaque", fmt.Sprintf("%d bytes; AAC access unit decoding is not supported", p.DataSpan.BitLength/8), span(p.DataSpan))
	}
	if !p.ConfigPresent {
		return
	}
	c := p.Config
	id := b.add(root, "AudioSpecificConfig", "ASC", "MPEG-4 audio decoder configuration", span(c.Span))
	b.objectType(id, "initialAudioObjectType", c.InitialAudioObjectType)
	b.frequency(id, "samplingFrequency", c.SamplingFrequency)
	if c.ChannelConfiguration.Complete {
		b.encoded(id, "channelConfiguration", "uint4", fmt.Sprintf("%d (%s)", c.ChannelConfiguration.Value, c.ChannelConfiguration.Name), c.ChannelConfiguration.Span)
	}
	if c.ExplicitSBR || c.ExplicitPS {
		b.add(id, "explicitExtension", "flags", fmt.Sprintf("SBR=%t, PS=%t", c.ExplicitSBR, c.ExplicitPS), nil)
		b.frequency(id, "extensionSamplingFrequency", c.ExtensionSamplingFrequency)
		b.objectType(id, "coreAudioObjectType", c.AudioObjectType)
	}
	if c.ExtensionChannelConfigurationPresent && c.ExtensionChannelConfiguration.Complete {
		b.encoded(id, "extensionChannelConfiguration", "uint4", fmt.Sprintf("%d (%s)", c.ExtensionChannelConfiguration.Value, c.ExtensionChannelConfiguration.Name), c.ExtensionChannelConfiguration.Span)
	}
	if c.GASpecificPresent {
		g := c.GASpecific
		ga := b.add(id, "GASpecificConfig", "bitfield", "General Audio configuration", span(g.Span))
		b.flag(ga, "frameLengthFlag", g.FrameLengthFlag, g.FrameLengthFlagSpan)
		b.flag(ga, "dependsOnCoreCoder", g.DependsOnCoreCoder, g.DependsOnCoreCoderSpan)
		if g.CoreCoderDelayPresent {
			b.encoded(ga, "coreCoderDelay", "uint14", decimal(uint64(g.CoreCoderDelay)), g.CoreCoderDelaySpan)
		}
		b.flag(ga, "extensionFlag", g.ExtensionFlag, g.ExtensionFlagSpan)
		if g.LayerNrPresent {
			b.encoded(ga, "layerNr", "uint3", decimal(uint64(g.LayerNr)), g.LayerNrSpan)
		}
		if g.NumOfSubFramePresent {
			b.encoded(ga, "numOfSubFrame", "uint5", decimal(uint64(g.NumOfSubFrame)), g.NumOfSubFrameSpan)
			b.encoded(ga, "layerLength", "uint11", decimal(uint64(g.LayerLength)), g.LayerLengthSpan)
		}
		if g.ResilienceFlagsPresent {
			b.flag(ga, "sectionDataResilienceFlag", g.SectionDataResilienceFlag, g.SectionDataResilienceFlagSpan)
			b.flag(ga, "scalefactorDataResilienceFlag", g.ScalefactorDataResilienceFlag, g.ScalefactorDataResilienceSpan)
			b.flag(ga, "spectralDataResilienceFlag", g.SpectralDataResilienceFlag, g.SpectralDataResilienceFlagSpan)
		}
		if g.ExtensionFlag3Present {
			b.flag(ga, "extensionFlag3", g.ExtensionFlag3, g.ExtensionFlag3Span)
		}
	}
	if c.EPConfigPresent && c.EPConfigComplete {
		b.encoded(id, "epConfig", "uint2", decimal(uint64(c.EPConfig)), c.EPConfigSpan)
	}
	if c.TrailingBits > 0 {
		b.add(id, "trailingBits", "opaque", decimal(c.TrailingBits)+" bits not interpreted", span(c.TrailingSpan))
	}
}

func (b *builder) flag(parent, name string, value bool, s cursor.Span) {
	b.encoded(parent, name, "boolean", strconv.FormatBool(value), s)
}
func (b *builder) objectType(parent, name string, o aac.ObjectTypeField) {
	if o.RawSpan.BitLength != 5 {
		return
	}
	value := "incomplete audio object type"
	if o.Complete {
		value = fmt.Sprintf("%d (%s)", o.Value, o.Name)
	}
	id := b.add(parent, name, "audioObjectType", value, span(o.Span))
	b.encoded(id, "raw", "uint5", decimal(uint64(o.Raw)), o.RawSpan)
	if o.Escaped {
		b.encoded(id, "escape", "uint6", decimal(uint64(o.Escape)), o.EscapeSpan)
	}
}
func (b *builder) frequency(parent, name string, f aac.FrequencyField) {
	if f.IndexSpan.BitLength != 4 {
		return
	}
	value := fmt.Sprintf("index %d; incomplete", f.Index)
	if f.Complete {
		value = fmt.Sprintf("index %d (%d Hz)", f.Index, f.Hertz)
		if f.Index == 13 || f.Index == 14 {
			value = fmt.Sprintf("index %d (reserved)", f.Index)
		}
	}
	id := b.add(parent, name, "samplingFrequency", value, span(f.IndexSpan))
	b.encoded(id, "samplingFrequencyIndex", "uint4", decimal(uint64(f.Index)), f.IndexSpan)
	if f.Explicit {
		b.encoded(id, "explicitFrequencyHz", "uint24", decimal(uint64(f.ExplicitValue)), f.ExplicitSpan)
	}
}
