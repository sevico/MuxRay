package detailview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const CodeAVCContextLookupLimitReached = "AVC_CONTEXT_LOOKUP_LIMIT_REACHED"

// DesktopAVCOptions is shared with the context resolver so a candidate avcC
// cannot consume larger syntax budgets than the selected desktop detail.
func DesktopAVCOptions() flv.AVCVideoOptions {
	return flv.AVCVideoOptions{AVC: avc.Options{
		MaxDiagnostics: MaxDiagnostics, MaxParameterSets: 64, MaxNALUnits: 256,
		MaxParameterSetBytes: 64 << 10, MaxRBSPMappingEntries: 4096,
	}}
}

func (b *builder) avc(ctx context.Context, source io.ReaderAt, tag flv.TagRecord, resolution avccontext.Resolution) error {
	options := DesktopAVCOptions()
	options.AVC.ParseVUI = true
	if resolution.State == "found" {
		if (resolution.NALLengthSize != 1 && resolution.NALLengthSize != 2 && resolution.NALLengthSize != 4) ||
			resolution.SourceLengthSpan.BitOffset != 6 || resolution.SourceLengthSpan.BitLength != 2 || resolution.SourceLengthSpan.Validate() != nil {
			return ErrInvalidInput
		}
		options.NALLengthSize = resolution.NALLengthSize
	}
	p, err := flv.ParseAVCVideo(source, tag, options)
	if errors.Is(err, flv.ErrAVCVideoWrongCodecID) || errors.Is(err, flv.ErrAVCVideoCommandFrame) {
		return ErrUnsupported
	}
	if err != nil {
		return err
	}
	b.detail.Kind, b.detail.Complete = string(flv.DetailKindAVCVideo), p.Complete
	for _, d := range p.Diagnostics {
		if d.Code == avc.CodeParameterSetTooLarge {
			b.detail.Truncated = true
		}
		b.diagnostic(d.Code, string(d.Severity), d.Offset, d.BitOffset, d.Message, d.Expected, d.Actual)
	}
	needsContext := p.PacketTypePresent && p.PacketType == flv.AVCPacketTypeNALU
	if needsContext && resolution.State == "budget_exhausted" {
		b.diagnostic(CodeAVCContextLookupLimitReached, "error", tag.PayloadSpan().ByteOffset, 0,
			"AVC sequence-header lookup reached its budget; NAL length size is unavailable and is not guessed", nil, nil)
	}
	b.add("", "tagId", "uint64", decimal(tag.ID), nil)
	root := b.add("", "AVC video", "avcVideo", "FLV AVC framing; slice payloads are not decoded", span(p.Span))
	if p.HeaderPresent {
		h := p.Header
		id := b.add(root, "VideoTagHeader", "bitfield", fmt.Sprintf("0x%02x", h.Raw), span(h.Span))
		label := "reserved"
		switch h.FrameType {
		case 1:
			label = "key frame"
		case 2:
			label = "inter frame"
		case 3:
			label = "disposable inter frame (H.263 only)"
		case 4:
			label = "generated key frame"
		}
		b.encoded(id, "frameType", "uint4", fmt.Sprintf("%d (%s)", h.FrameType, label), h.FrameTypeSpan)
		b.encoded(id, "codecId", "uint4", "7 (AVC/H.264)", h.CodecIDSpan)
	}
	if p.PacketTypePresent {
		b.encoded(root, "AVCPacketType", "uint8", fmt.Sprintf("%d (%s)", p.PacketType, p.PacketType), p.PacketTypeSpan)
	}
	b.add(root, "DTS", "derived milliseconds", strconv.FormatInt(tag.DTS, 10)+" (from indexed FLV timestamp)", nil)
	if p.CompositionTimePresent {
		b.encoded(root, "compositionTime", "int24 milliseconds", strconv.FormatInt(int64(p.CompositionTime), 10), p.CompositionTimeSpan)
		pts := new(big.Int).Add(big.NewInt(tag.DTS), big.NewInt(int64(p.CompositionTime)))
		b.add(root, "PTS", "derived milliseconds", pts.String()+" (DTS + compositionTime)", nil)
	}
	state := "not_needed"
	if needsContext {
		state = resolution.State
		if state != "found" && state != "budget_exhausted" {
			state = "missing"
		}
	}
	b.avcContext(root, resolution, state)
	if p.ConfigPresent {
		if err := b.avcConfig(ctx, root, p.Config); err != nil {
			return err
		}
	}
	if p.NALUnitsPresent {
		s := p.NALUnits
		id := b.add(root, "NAL units", "length-prefixed sequence", fmt.Sprintf("%d retained; framingComplete=%t", len(s.NALUnits), s.FramingComplete), span(s.Span))
		for _, n := range s.NALUnits {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !b.avcUnit(id, "NAL["+decimal(uint64(n.Index))+"]", n.Length, n.LengthSpan, n.Span, n.Header, n.HeaderPresent, n.SequenceParameterSet, n.PictureParameterSet, n.SummaryComplete) {
				break
			}
		}
		b.encoded(id, "unparsedNALData", "opaque", "NAL data not interpreted; see diagnostics", s.TrailingSpan)
	} else if p.PacketTypePresent && p.PacketType == flv.AVCPacketTypeNALU {
		b.encoded(root, "unparsedNALData", "opaque", "NAL boundaries unavailable; length size is not guessed", p.DataSpan)
	}
	if p.EndOfSequencePresent {
		b.add(root, "endOfSequence", "marker", "AVC end of sequence", nil)
		b.encoded(root, "unexpectedData", "opaque", "Unexpected end-of-sequence payload; see diagnostics", p.DataSpan)
	} else if p.PacketTypePresent && p.PacketType > flv.AVCPacketTypeEndOfSequence {
		b.encoded(root, "unknownPacketData", "opaque", "Unknown packet type; payload not interpreted", p.DataSpan)
	}
	return ctx.Err()
}

func (b *builder) avcContext(parent string, r avccontext.Resolution, state string) {
	id := b.add(parent, "AVC context", "lookup", state, nil)
	if r.LookupMethod != "" {
		b.add(id, "lookupMethod", "derived lookup method", r.LookupMethod, nil)
	}
	if state == "found" {
		b.add(id, "sourceTagId", "uint64", decimal(r.SourceTagID), nil)
		b.encoded(id, "sourceLengthSizeMinusOne", "uint2", decimal(uint64(r.NALLengthSize-1)), r.SourceLengthSpan)
		b.add(id, "NALLengthSize", "derived bytes", fmt.Sprintf("%d (source lengthSizeMinusOne + 1)", r.NALLengthSize), nil)
	} else if state != "not_needed" {
		b.add(id, "NALLengthSize", "unavailable", "No usable sequence-header context; NAL length size is not guessed", nil)
	}
	b.add(id, "scannedTags", "uint64", decimal(r.ScannedTags), nil)
	b.add(id, "candidates", "uint32", decimal(uint64(r.Candidates)), nil)
	b.add(id, "bytesRead", "uint64", decimal(r.BytesRead), nil)
}

func (b *builder) avcConfig(ctx context.Context, parent string, c avc.ConfigurationRecord) error {
	id := b.add(parent, "AVCDecoderConfigurationRecord", "avcC", fmt.Sprintf("framingComplete=%t; complete=%t", c.FramingComplete, c.Complete), span(c.Span))
	fields := []struct {
		name  string
		value uint8
		s     cursor.Span
	}{
		{"configurationVersion", c.ConfigurationVersion, c.ConfigurationVersionSpan},
		{"AVCProfileIndication", c.AVCProfileIndication, c.AVCProfileIndicationSpan},
		{"profileCompatibility", c.ProfileCompatibility, c.ProfileCompatibilitySpan},
		{"AVCLevelIndication", c.AVCLevelIndication, c.AVCLevelIndicationSpan},
		{"lengthSizeReserved", c.LengthSizeReserved, c.LengthSizeReservedSpan},
		{"lengthSizeMinusOne", c.LengthSizeMinusOne, c.LengthSizeMinusOneSpan},
		{"SPSCountReserved", c.SPSCountReserved, c.SPSCountReservedSpan},
		{"numSequenceParameterSets", c.NumSequenceParameterSets, c.NumSequenceParameterSetsSpan},
		{"numPictureParameterSets", c.NumPictureParameterSets, c.NumPictureParameterSetsSpan},
		{"chromaFormatReserved", c.ChromaFormatReserved, c.ChromaFormatReservedSpan},
		{"chromaFormat", c.ChromaFormat, c.ChromaFormatSpan},
		{"bitDepthLumaReserved", c.BitDepthLumaReserved, c.BitDepthLumaReservedSpan},
		{"bitDepthLumaMinus8", c.BitDepthLumaMinus8, c.BitDepthLumaMinus8Span},
		{"bitDepthChromaReserved", c.BitDepthChromaReserved, c.BitDepthChromaReservedSpan},
		{"bitDepthChromaMinus8", c.BitDepthChromaMinus8, c.BitDepthChromaMinus8Span},
		{"numSequenceParameterSetExt", c.NumSequenceParameterSetExt, c.NumSequenceParameterSetExtSpan},
	}
	for _, f := range fields {
		b.encoded(id, f.name, "uint"+decimal(uint64(f.s.BitLength)), decimal(uint64(f.value)), f.s)
	}
	if c.LengthSizeMinusOneSpan.BitLength != 0 && c.LengthSize != 0 {
		b.add(id, "NALLengthSize", "derived bytes", fmt.Sprintf("%d (lengthSizeMinusOne + 1)", c.LengthSize), nil)
	}
	for _, group := range []struct {
		name string
		sets []avc.ParameterSet
	}{
		{"SPS", c.SequenceParameterSets}, {"PPS", c.PictureParameterSets}, {"SPS extension", c.SequenceParameterSetExts},
	} {
		for _, s := range group.sets {
			if err := ctx.Err(); err != nil {
				return err
			}
			if !b.avcUnit(id, group.name+"["+decimal(uint64(s.Index))+"]", s.Length, s.LengthSpan, s.Span, s.Header, s.HeaderPresent, s.SequenceParameterSet, s.PictureParameterSet, s.SummaryComplete) {
				return nil
			}
		}
	}
	b.encoded(id, "trailingConfigurationData", "opaque", "Configuration bytes not interpreted; see diagnostics", c.TrailingSpan)
	return ctx.Err()
}

func (b *builder) avcUnit(parent, name string, length uint32, lengthSpan, dataSpan cursor.Span, h avc.NALHeader, headerPresent bool, sps *avc.SequenceParameterSet, pps *avc.PictureParameterSet, summaryComplete bool) bool {
	id := b.add(parent, name, "NAL unit", fmt.Sprintf("declared=%d bytes; available=%d bytes; summaryComplete=%t", length, dataSpan.BitLength/8, summaryComplete), span(dataSpan))
	if id == "" {
		return false
	}
	b.encoded(id, "NALLength", "uint"+decimal(uint64(lengthSpan.BitLength)), decimal(uint64(length)), lengthSpan)
	b.add(id, "payloadEndOffset", "derived byte offset (exclusive)", decimal(dataSpan.ByteOffset+uint64(dataSpan.BitLength)/8), nil)
	if headerPresent {
		header := b.add(id, "NALHeader", "bitfield", fmt.Sprintf("0x%02x", h.Raw), span(h.Span))
		b.flag(header, "forbiddenZeroBit", h.ForbiddenZeroBit, h.ForbiddenZeroBitSpan)
		b.encoded(header, "nalRefIdc", "uint2", decimal(uint64(h.NALRefIDC)), h.NALRefIDCSpan)
		b.encoded(header, "nalUnitType", "uint5", fmt.Sprintf("%d (%s)", h.NALUnitType, h.NALUnitType), h.NALUnitTypeSpan)
	}
	// The one-span desktop DTO must never collapse a mapped RBSP range into a
	// contiguous source range. The full original EBSP range stays on the NAL;
	// these phase-one summaries deliberately have no individual source spans.
	if sps != nil {
		summary := b.add(id, "SPS summary", "derived from RBSP", "Base SPS summary; optional VUI completeness is reported separately", nil)
		b.add(summary, "summaryComplete", "derived boolean", strconv.FormatBool(sps.Complete), nil)
		if sps.ProfileIDCSpan.BitLength != 0 {
			b.add(summary, "profileIdc", "derived uint8", decimal(uint64(sps.ProfileIDC)), nil)
		}
		if sps.LevelIDCSpan.BitLength != 0 {
			b.add(summary, "levelIdc", "derived uint8", decimal(uint64(sps.LevelIDC)), nil)
		}
		if sps.SequenceParameterSetIDSpan.BitLength != 0 {
			b.add(summary, "sequenceParameterSetId", "derived uint32", decimal(uint64(sps.SequenceParameterSetID)), nil)
		}
		if sps.Complete {
			b.add(summary, "codedDimensions", "derived pixels", fmt.Sprintf("%d × %d", sps.CodedWidth, sps.CodedHeight), nil)
			b.add(summary, "displayDimensions", "derived pixels", fmt.Sprintf("%d × %d", sps.DisplayWidth, sps.DisplayHeight), nil)
		}
		b.rbspSummary(summary, sps.RBSP)
		if sps.VUI != nil {
			b.avcVUI(summary, sps.VUI)
		}
	}
	if pps != nil {
		summary := b.add(id, "PPS summary", "derived from RBSP", "Phase-one identifiers and flags; remaining PPS syntax is not expanded", nil)
		b.add(summary, "summaryComplete", "derived boolean", strconv.FormatBool(pps.Complete), nil)
		if pps.PictureParameterSetIDSpan.BitLength != 0 {
			b.add(summary, "pictureParameterSetId", "derived uint32", decimal(uint64(pps.PictureParameterSetID)), nil)
		}
		if pps.SequenceParameterSetIDSpan.BitLength != 0 {
			b.add(summary, "sequenceParameterSetId", "derived uint32", decimal(uint64(pps.SequenceParameterSetID)), nil)
		}
		if pps.EntropyCodingModeFlagSpan.BitLength != 0 {
			b.add(summary, "entropyCodingModeFlag", "derived boolean", strconv.FormatBool(pps.EntropyCodingModeFlag), nil)
		}
		if pps.BottomFieldPicOrderInFramePresentFlagSpan.BitLength != 0 {
			b.add(summary, "bottomFieldPicOrderInFramePresentFlag", "derived boolean", strconv.FormatBool(pps.BottomFieldPicOrderInFramePresentFlag), nil)
		}
		b.rbspSummary(summary, pps.RBSP)
	}
	return !b.fieldsFull
}

func (b *builder) rbspSummary(parent string, r avc.RBSPView) {
	b.add(parent, "emulationPreventionBytes", "derived count", decimal(uint64(len(r.EmulationPreventionBytes))), nil)
	b.add(parent, "sourceSegments", "derived count", decimal(uint64(len(r.SourceSegments)))+" (not flattened into a contiguous field span)", nil)
}

// RBSP values remain summaries in the single-Span desktop protocol. The SPS
// parent retains its exact original range; no field spans are fabricated.
func (b *builder) avcVUI(parent string, v *avc.VUIParameters) {
	id := b.add(parent, "VUI", "derived from RBSP", "Signalled display, timing and HRD parameters; no decoded-picture or measured-rate claim", nil)
	b.add(id, "present", "derived boolean", strconv.FormatBool(v.Present), nil)
	b.add(id, "vuiComplete", "derived boolean", strconv.FormatBool(v.Complete), nil)
	b.add(id, "spsTrailingBitsValid", "derived boolean", strconv.FormatBool(v.TrailingBitsValid), nil)
	b.vuiBool(id, "aspectRatioInfoPresentFlag", v.AspectRatioInfoPresentFlag)
	b.vuiUint(id, "aspectRatioIdc", v.AspectRatioIDC)
	b.vuiUint(id, "sarWidth", v.SARWidth)
	b.vuiUint(id, "sarHeight", v.SARHeight)
	b.vuiBool(id, "overscanInfoPresentFlag", v.OverscanInfoPresentFlag)
	b.vuiBool(id, "overscanAppropriateFlag", v.OverscanAppropriateFlag)
	b.vuiBool(id, "videoSignalTypePresentFlag", v.VideoSignalTypePresentFlag)
	b.vuiUint(id, "videoFormat", v.VideoFormat)
	b.vuiBool(id, "videoFullRangeFlag", v.VideoFullRangeFlag)
	b.vuiBool(id, "colourDescriptionPresentFlag", v.ColourDescriptionPresentFlag)
	b.vuiUint(id, "colourPrimaries", v.ColourPrimaries)
	b.vuiUint(id, "transferCharacteristics", v.TransferCharacteristics)
	b.vuiUint(id, "matrixCoefficients", v.MatrixCoefficients)
	b.vuiBool(id, "chromaLocInfoPresentFlag", v.ChromaLocInfoPresentFlag)
	b.vuiUint(id, "chromaSampleLocTypeTopField", v.ChromaSampleLocTypeTopField)
	b.vuiUint(id, "chromaSampleLocTypeBottomField", v.ChromaSampleLocTypeBottomField)
	b.vuiBool(id, "timingInfoPresentFlag", v.TimingInfoPresentFlag)
	b.vuiUint(id, "numUnitsInTick", v.NumUnitsInTick)
	b.vuiUint(id, "timeScale", v.TimeScale)
	b.vuiBool(id, "fixedFrameRateFlag", v.FixedFrameRateFlag)
	b.vuiBool(id, "nalHrdParametersPresentFlag", v.NALHRDParametersPresentFlag)
	b.vuiBool(id, "vclHrdParametersPresentFlag", v.VCLHRDParametersPresentFlag)
	b.vuiBool(id, "lowDelayHrdFlag", v.LowDelayHRDFlag)
	b.vuiBool(id, "picStructPresentFlag", v.PicStructPresentFlag)
	b.vuiBool(id, "bitstreamRestrictionFlag", v.BitstreamRestrictionFlag)
	b.vuiBool(id, "motionVectorsOverPicBoundariesFlag", v.MotionVectorsOverPicBoundariesFlag)
	b.vuiUint(id, "maxBytesPerPicDenom", v.MaxBytesPerPicDenom)
	b.vuiUint(id, "maxBitsPerMbDenom", v.MaxBitsPerMBDenom)
	b.vuiUint(id, "log2MaxMvLengthHorizontal", v.Log2MaxMVLengthHorizontal)
	b.vuiUint(id, "log2MaxMvLengthVertical", v.Log2MaxMVLengthVertical)
	b.vuiUint(id, "maxNumReorderFrames", v.MaxNumReorderFrames)
	b.vuiUint(id, "maxDecFrameBuffering", v.MaxDecFrameBuffering)

	b.avcHRD(id, "NAL HRD", v.NALHRD)
	b.avcHRD(id, "VCL HRD", v.VCLHRD)
	b.add(id, "vuiSyntaxBits", "derived bits", decimal(uint64(v.SyntaxBits)), nil)
	b.add(id, "remainingRbspBits", "derived bits", decimal(uint64(v.RemainingRBSPBits)), nil)
}

func (b *builder) avcHRD(parent, name string, h *avc.HRDParameters) {
	if h == nil {
		return
	}
	id := b.add(parent, name, "derived HRD declarations", "Declared CPB model; cbrFlag does not measure bitrate variation", nil)
	b.add(id, "hrdComplete", "derived boolean", strconv.FormatBool(h.Complete), nil)
	b.vuiUint(id, "cpbCntMinus1", h.CPBCntMinus1)
	b.vuiUint(id, "bitRateScale", h.BitRateScale)
	b.vuiUint(id, "cpbSizeScale", h.CPBSizeScale)
	b.vuiUint(id, "initialCpbRemovalDelayLengthMinus1", h.InitialCPBRemovalDelayLengthMinus1)
	b.vuiUint(id, "cpbRemovalDelayLengthMinus1", h.CPBRemovalDelayLengthMinus1)
	b.vuiUint(id, "dpbOutputDelayLengthMinus1", h.DPBOutputDelayLengthMinus1)
	b.vuiUint(id, "timeOffsetLength", h.TimeOffsetLength)

	for i, cpb := range h.CPB {
		entry := b.add(id, "CPB["+strconv.Itoa(i)+"]", "derived HRD entry", "", nil)
		if entry == "" {
			break
		}
		b.vuiUint(entry, "bitRateValueMinus1", cpb.BitRateValueMinus1)
		b.vuiUint(entry, "cpbSizeValueMinus1", cpb.CPBSizeValueMinus1)
		b.vuiBool(entry, "cbrFlag", cpb.CBRFlag)
	}
}

func (b *builder) vuiUint(parent, name string, f *avc.VUIUnsignedField) {
	if f != nil {
		b.add(parent, name, "derived uint32", decimal(uint64(f.Value)), nil)
	}
}

func (b *builder) vuiBool(parent, name string, f *avc.VUIBooleanField) {
	if f != nil {
		b.add(parent, name, "derived boolean", strconv.FormatBool(f.Value), nil)
	}
}
