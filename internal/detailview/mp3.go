package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

// mp3 projects bounded same-tag syntax; compressed audio is never decoded.
func (b *builder) mp3(p flv.MP3Audio) {
	description := "FLV framing and first MPEG frame header only; audio and frame bodies are not decoded or validated"
	if p.Frames != nil {
		description = "Same-tag frame boundaries, first-frame VBR metadata and header/side-information CRC; compressed audio is not decoded or validated"
	}
	root := b.add("", "MP3 audio", "mp3Audio", description, span(p.Span))
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
	dataDescription := fmt.Sprintf("%d bytes; only the first four-byte MPEG header is inspected", p.DataSpan.BitLength/8)
	if p.Frames != nil {
		dataDescription = fmt.Sprintf("%d bytes; audio bodies remain opaque", p.DataSpan.BitLength/8)
	}
	b.add(root, "SoundData", "opaque", dataDescription, span(p.DataSpan))
	if !p.FrameHeader.HeaderPresent {
		if p.Frames != nil {
			b.mp3Frames(root, p.Frames)
		}
		return
	}
	h := p.FrameHeader.Header
	id := b.add(root, "MPEG frame header", "bitfield", fmt.Sprintf("0x%08x", h.Raw), span(h.Span))
	b.encoded(id, "syncWord", "uint11", fmt.Sprintf("0x%03x", h.SyncWord), h.SyncWordSpan)
	b.encoded(id, "version", "uint2", fmt.Sprintf("%d (%s)", h.Version, h.Version), h.VersionSpan)
	b.encoded(id, "layer", "uint2", fmt.Sprintf("%d (%s)", h.Layer, h.Layer), h.LayerSpan)
	b.encoded(id, "protectionBit", "uint1", decimal(uint64(h.ProtectionBit)), h.ProtectionBitSpan)
	crcDescription := " (header declaration; CRC data is not checked)"
	if p.Frames != nil {
		crcDescription = " (header declaration; see frame CRC status)"
	}
	b.add(id, "crcProtected", "derived boolean", strconv.FormatBool(h.CRCProtected)+crcDescription, nil)
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
	if p.Frames != nil {
		b.mp3Frames(root, p.Frames)
	} else if p.DataSpan.BitLength > 32 {
		rest := cursor.Span{ByteOffset: p.DataSpan.ByteOffset + 4, BitLength: p.DataSpan.BitLength - 32}
		b.add(root, "unparsedData", "opaque", fmt.Sprintf("%d bytes; frame bodies, CRC, VBR headers and later frames are not inspected", rest.BitLength/8), span(rest))
	}
}

func (b *builder) mp3Frames(root string, p *mp3.FrameSequence) {
	id := b.add(root, "Frame inspection", "bounded sequence", fmt.Sprintf("%d complete frame boundaries; %d headers; stop=%s; audio main data is not decoded", p.CompleteFrames, len(p.Frames), p.StopReason), span(p.Span))
	b.add(id, "frameBudget", "derived count", decimal(uint64(p.MaxFrames)), nil)
	b.add(id, "byteCoverageBudget", "derived bytes", decimal(p.MaxBytes), nil)
	b.add(id, "bytesCovered", "derived bytes", decimal(p.BytesCovered), nil)
	b.add(id, "bytesRead", "derived bytes", decimal(p.BytesRead), nil)
	for _, f := range p.Frames {
		length, bitrate, frequency, channels, version := "unknown", "unknown", "unknown", "unknown", "unknown"
		if f.Header.Complete {
			version = f.Header.Version.String()
			channels = decimal(uint64(f.Header.Channels))
			if f.Header.FrameLengthPresent {
				length = fmt.Sprintf("%d bytes", f.Header.FrameLengthBytes)
			}
			if f.Header.BitratePresent {
				bitrate = fmt.Sprintf("%d kbit/s", f.Header.BitrateKbps)
			}
			if f.Header.SamplingFrequencyPresent {
				frequency = fmt.Sprintf("%d Hz", f.Header.SamplingFrequencyHz)
			}
		}
		frame := b.add(id, fmt.Sprintf("Frame %d", f.Index), "MPEG frame", fmt.Sprintf("declared length=%s; boundary complete=%t; version=%s; bitrate=%s; sampling=%s; channels=%s", length, f.BoundaryComplete, version, bitrate, frequency, channels), span(f.Span))
		b.add(frame, "header", "bitfield", fmt.Sprintf("0x%08x", f.Header.Raw), span(f.Header.Span))
		value := f.CRC.Status + " (header bits and Layer III side information only; main data excluded)"
		var crcSpan *Span
		if !f.CRC.Span.Empty() {
			crcSpan = span(f.CRC.Span)
		}
		if f.CRC.ValuesPresent {
			value += fmt.Sprintf("; stored=0x%04x computed=0x%04x", f.CRC.Stored, f.CRC.Computed)
		}
		b.add(frame, "CRC16", "integrity check", value, crcSpan)
	}
	if v := p.VBR; v != nil {
		meta := b.add(id, v.Kind, "VBR metadata", fmt.Sprintf("first-frame metadata; complete=%t; counts describe the whole stream, not this FLV tag", v.Complete), span(v.Span))
		if v.Flags != nil {
			b.add(meta, "flags", "encoded uint32", fmt.Sprintf("0x%08x", *v.Flags), span(cursor.Span{ByteOffset: v.Span.ByteOffset + 4, BitLength: 32}))
		}
		next := v.Span.ByteOffset + 8
		if v.Version != nil {
			b.add(meta, "version", "encoded uint16", decimal(uint64(*v.Version)), span(cursor.Span{ByteOffset: v.Span.ByteOffset + 4, BitLength: 16}))
			next = v.Span.ByteOffset + 10
		}
		if v.Delay != nil {
			b.add(meta, "delay", "encoded uint16", decimal(uint64(*v.Delay)), span(cursor.Span{ByteOffset: v.Span.ByteOffset + 6, BitLength: 16}))
		}
		if v.Kind == "VBRI" {
			if v.Quality != nil {
				b.add(meta, "quality", "encoded uint16", decimal(uint64(*v.Quality)), span(cursor.Span{ByteOffset: v.Span.ByteOffset + 8, BitLength: 16}))
			}
			if v.DeclaredBytes != nil {
				b.add(meta, "declaredStreamBytes", "encoded uint32", decimal(uint64(*v.DeclaredBytes)), span(cursor.Span{ByteOffset: next, BitLength: 32}))
				next += 4
			}
			if v.DeclaredFrames != nil {
				b.add(meta, "declaredStreamFrames", "encoded uint32", decimal(uint64(*v.DeclaredFrames)), span(cursor.Span{ByteOffset: next, BitLength: 32}))
			}
			b.add(meta, "VBRI scope", "scope", "fixed header counts only; seek-table entries are not inspected", nil)
		} else {
			if v.DeclaredFrames != nil {
				b.add(meta, "declaredStreamFrames", "encoded uint32", decimal(uint64(*v.DeclaredFrames)), span(cursor.Span{ByteOffset: next, BitLength: 32}))
				next += 4
			}
			if v.DeclaredBytes != nil {
				b.add(meta, "declaredStreamBytes", "encoded uint32", decimal(uint64(*v.DeclaredBytes)), span(cursor.Span{ByteOffset: next, BitLength: 32}))
				next += 4
			}
			if v.TOCSpan != nil {
				b.add(meta, "TOC", "opaque", "100-byte seek table; entries not interpreted", span(*v.TOCSpan))
				next += 100
			}
			if v.Quality != nil {
				b.add(meta, "quality", "encoded uint32", decimal(uint64(*v.Quality)), span(cursor.Span{ByteOffset: next, BitLength: 32}))
			}
		}
	}
	if !p.UninspectedSpan.Empty() {
		b.add(id, "uninspectedFrames", "opaque", fmt.Sprintf("%d bytes from the first unresolved boundary", p.UninspectedSpan.BitLength/8), span(p.UninspectedSpan))
	}
}
