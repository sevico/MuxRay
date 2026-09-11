package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

// mp3 projects only the fixed headers already parsed by the core. A complete
// result does not establish any compressed frame boundary or validate its body.
func (b *builder) mp3(p flv.MP3Audio) {
	root := b.add("", "MP3 audio", "mp3Audio", "FLV framing and first MPEG frame header only; audio and frame bodies are not decoded or validated", span(p.Span))
	if p.HeaderPresent {
		h := p.Header
		id := b.add(root, "AudioTagHeader", "bitfield", fmt.Sprintf("0x%02x", h.Raw), span(h.Span))
		b.encoded(id, "soundFormat", "uint4", fmt.Sprintf("%d (MP3)", h.SoundFormat), h.SoundFormatSpan)
		rates := [...]string{"5.5 kHz", "11 kHz", "22 kHz", "44 kHz"}
		sizes := [...]string{"8-bit", "16-bit"}
		channels := [...]string{"mono", "stereo"}
		b.encoded(id, "soundRate", "uint2", fmt.Sprintf("%d (%s FLV hint; see MPEG sampling frequency)", h.SoundRate, rates[h.SoundRate]), h.SoundRateSpan)
		b.encoded(id, "soundSize", "uint1", fmt.Sprintf("%d (%s FLV hint)", h.SoundSize, sizes[h.SoundSize]), h.SoundSizeSpan)
		b.encoded(id, "soundType", "uint1", fmt.Sprintf("%d (%s FLV hint; see MPEG channels)", h.SoundType, channels[h.SoundType]), h.SoundTypeSpan)
	}
	b.add(root, "SoundData", "opaque", fmt.Sprintf("%d bytes; only the first four-byte MPEG header is inspected", p.DataSpan.BitLength/8), span(p.DataSpan))
	if !p.FrameHeader.HeaderPresent {
		return
	}
	h := p.FrameHeader.Header
	id := b.add(root, "MPEG frame header", "bitfield", fmt.Sprintf("0x%08x", h.Raw), span(h.Span))
	b.encoded(id, "syncWord", "uint11", fmt.Sprintf("0x%03x", h.SyncWord), h.SyncWordSpan)
	b.encoded(id, "version", "uint2", fmt.Sprintf("%d (%s)", h.Version, h.Version), h.VersionSpan)
	b.encoded(id, "layer", "uint2", fmt.Sprintf("%d (%s)", h.Layer, h.Layer), h.LayerSpan)
	b.encoded(id, "protectionBit", "uint1", decimal(uint64(h.ProtectionBit)), h.ProtectionBitSpan)
	b.add(id, "crcProtected", "derived boolean", strconv.FormatBool(h.CRCProtected)+" (header declaration; CRC data is not checked)", nil)
	b.encoded(id, "bitrateIndex", "uint4", decimal(uint64(h.BitrateIndex)), h.BitrateIndexSpan)
	b.add(id, "bitrateMode", "derived enum", string(h.BitrateMode), nil)
	b.encoded(id, "samplingFrequencyIndex", "uint2", decimal(uint64(h.SamplingFrequencyIndex)), h.SamplingFrequencyIndexSpan)
	b.flag(id, "padding", h.Padding, h.PaddingSpan)
	b.flag(id, "private", h.Private, h.PrivateSpan)
	b.encoded(id, "channelMode", "uint2", fmt.Sprintf("%d (%s)", h.ChannelMode, h.ChannelMode), h.ChannelModeSpan)
	b.encoded(id, "modeExtension", "uint2", decimal(uint64(h.ModeExtension)), h.ModeExtensionSpan)
	b.flag(id, "copyright", h.Copyright, h.CopyrightSpan)
	b.flag(id, "original", h.Original, h.OriginalSpan)
	b.encoded(id, "emphasis", "uint2", fmt.Sprintf("%d (%s)", h.Emphasis, h.Emphasis), h.EmphasisSpan)
	b.add(id, "channels", "derived count", decimal(uint64(h.Channels)), nil)
	if h.BitratePresent {
		b.add(id, "bitrateKbps", "derived kbit/s", decimal(uint64(h.BitrateKbps)), nil)
	}
	if h.SamplingFrequencyPresent {
		b.add(id, "samplingFrequencyHz", "derived Hz", decimal(uint64(h.SamplingFrequencyHz)), nil)
	}
	if h.SamplesPerFramePresent {
		b.add(id, "samplesPerFrame", "derived samples", decimal(uint64(h.SamplesPerFrame)), nil)
	}
	if h.FrameLengthPresent {
		b.add(id, "frameLengthBytes", "derived bytes", decimal(uint64(h.FrameLengthBytes)), nil)
	}
	if p.DataSpan.BitLength > 32 {
		rest := cursor.Span{ByteOffset: p.DataSpan.ByteOffset + 4, BitLength: p.DataSpan.BitLength - 32}
		b.add(root, "unparsedData", "opaque", fmt.Sprintf("%d bytes; frame bodies, CRC, VBR headers and later frames are not inspected", rest.BitLength/8), span(rest))
	}
}
