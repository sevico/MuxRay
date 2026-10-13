package detailview

import (
	"fmt"
	"strconv"

	"github.com/sevico/muxray/internal/avc"
)

func (b *builder) avcSEITiming(parent string, m avc.SEIMessage) {
	if v := m.BufferingPeriod; v != nil {
		id := b.add(parent, "Buffering period", "SEI timing syntax", "Encoded initial CPB delays; no buffer schedule is reconstructed", nil)
		b.seiUint(id, "sequenceParameterSetId", v.SequenceParameterSetID)
		b.avcSEITimingContext(id, v.Context)
		for _, delays := range []struct {
			name   string
			values []avc.SEICPBInitialDelay
		}{{"NAL", v.NALInitialDelays}, {"VCL", v.VCLInitialDelays}} {
			for _, d := range delays.values {
				entry := b.add(id, fmt.Sprintf("%s initial CPB[%d]", delays.name, d.Index), "SEI timing syntax", "Encoded delay and offset", nil)
				b.seiUint(entry, "initialCpbRemovalDelay", d.InitialCPBRemovalDelay)
				b.seiUint(entry, "initialCpbRemovalDelayOffset", d.InitialCPBRemovalDelayOffset)
			}
		}
		b.avcSEITimingTail(id, v.PayloadExtensionBits, v.PayloadAlignmentBits)
	}
	if v := m.PictureTiming; v != nil {
		id := b.add(parent, "Picture timing", "SEI timing syntax", "Encoded delays and clock fields; not FLV DTS/PTS or a decoded-picture timeline", nil)
		b.avcSEITimingContext(id, v.Context)
		b.seiUint(id, "cpbRemovalDelay", v.CPBRemovalDelay)
		b.seiUint(id, "dpbOutputDelay", v.DPBOutputDelay)
		b.seiUint(id, "picStruct", v.PicStruct)
		for _, c := range v.ClockTimestamps {
			entry := b.add(id, fmt.Sprintf("Clock timestamp[%d]", c.Index), "SEI timing syntax", "Only explicitly encoded fields have source positions", nil)
			b.seiBool(entry, "clockTimestampFlag", c.ClockTimestampFlag)
			b.seiUint(entry, "ctType", c.CTType)
			b.seiBool(entry, "nuitFieldBasedFlag", c.NuitFieldBasedFlag)
			b.seiUint(entry, "countingType", c.CountingType)
			b.seiBool(entry, "fullTimestampFlag", c.FullTimestampFlag)
			b.seiBool(entry, "discontinuityFlag", c.DiscontinuityFlag)
			b.seiBool(entry, "cntDroppedFlag", c.CntDroppedFlag)
			b.seiUint(entry, "nFrames", c.NFrames)
			b.seiBool(entry, "secondsFlag", c.SecondsFlag)
			b.seiBool(entry, "minutesFlag", c.MinutesFlag)
			b.seiBool(entry, "hoursFlag", c.HoursFlag)
			b.seiUint(entry, "secondsValue", c.SecondsValue)
			b.seiUint(entry, "minutesValue", c.MinutesValue)
			b.seiUint(entry, "hoursValue", c.HoursValue)
			if c.TimeOffset != nil {
				b.seiMapped(entry, "timeOffset", "RBSP signed", strconv.FormatInt(c.TimeOffset.Value, 10), c.TimeOffset.Span)
			}
		}
		b.avcSEITimingTail(id, v.PayloadExtensionBits, v.PayloadAlignmentBits)
	}
}

func (b *builder) avcSEITimingContext(parent string, c avc.SEITimingContext) {
	b.add(parent, "timingContextState", "derived state", c.State, nil)
	if c.Source != "" {
		b.add(parent, "timingContextSource", "derived source", c.Source, nil)
	}
	if c.SPSID != nil {
		b.add(parent, "timingSpsId", "derived identifier", decimal(uint64(*c.SPSID)), nil)
	}
	if c.SPSSpan != nil {
		b.add(parent, "timingSpsSource", "parameter source", "Original SPS supplying timing syntax", span(*c.SPSSpan))
	}
	if c.EvidenceSpan != nil {
		b.add(parent, "timingSelectionSource", "context evidence source", "Original evidence selecting the SPS", span(*c.EvidenceSpan))
	}
	if p := c.Parameters; p != nil {
		b.add(parent, "timingPicStructPresent", "derived boolean", strconv.FormatBool(p.PicStructPresent), nil)
		b.add(parent, "timingInfoPresent", "derived boolean", strconv.FormatBool(p.TimingInfoPresent), nil)
		if p.TimingInfoPresent {
			b.add(parent, "timingNumUnitsInTick", "derived SPS declaration", decimal(uint64(p.NumUnitsInTick)), nil)
			b.add(parent, "timingTimeScale", "derived SPS declaration", decimal(uint64(p.TimeScale)), nil)
			b.add(parent, "timingFixedFrameRate", "derived SPS declaration", strconv.FormatBool(p.FixedFrameRate), nil)
		}
		for _, h := range []struct {
			name  string
			value avc.HRDTimingParameters
		}{{"NAL", p.NALHRD}, {"VCL", p.VCLHRD}} {
			id := b.add(parent, h.name+" HRD syntax context", "derived SPS declaration", "Validated syntax widths from the source SPS", nil)
			b.add(id, "hrdPresent", "derived boolean", strconv.FormatBool(h.value.Present), nil)
			if !h.value.Present {
				continue
			}
			b.add(id, "cpbCount", "derived count", decimal(uint64(h.value.CPBCount)), nil)
			b.add(id, "initialCpbRemovalDelayLength", "derived bits", decimal(uint64(h.value.InitialCPBRemovalDelayLength)), nil)
			b.add(id, "cpbRemovalDelayLength", "derived bits", decimal(uint64(h.value.CPBRemovalDelayLength)), nil)
			b.add(id, "dpbOutputDelayLength", "derived bits", decimal(uint64(h.value.DPBOutputDelayLength)), nil)
			b.add(id, "timeOffsetLength", "derived bits", decimal(uint64(h.value.TimeOffsetLength)), nil)
		}
	}
}

func (b *builder) avcSEITimingTail(parent string, extension, alignment *avc.MappedSpan) {
	if extension != nil {
		b.seiMapped(parent, "payloadExtensionBits", "RBSP reserved bits", "Reserved extension data", *extension)
	}
	if alignment != nil {
		b.seiMapped(parent, "payloadAlignmentBits", "RBSP alignment", "Validated payload alignment bits", *alignment)
	}
}
