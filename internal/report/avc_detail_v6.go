package report

import (
	"errors"
	"fmt"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

// These declarations describe the syntax dependencies, not clock conversion
// or a decoded-picture timeline. Source spans identify the original evidence.
type AVCHRDTimingParameters struct {
	Present                      bool   `json:"present"`
	CPBCount                     uint32 `json:"cpbCount"`
	InitialCPBRemovalDelayLength uint8  `json:"initialCpbRemovalDelayLength"`
	CPBRemovalDelayLength        uint8  `json:"cpbRemovalDelayLength"`
	DPBOutputDelayLength         uint8  `json:"dpbOutputDelayLength"`
	TimeOffsetLength             uint8  `json:"timeOffsetLength"`
}

type AVCSPSTimingParameters struct {
	NALHRD            AVCHRDTimingParameters `json:"nalHrd"`
	VCLHRD            AVCHRDTimingParameters `json:"vclHrd"`
	PicStructPresent  bool                   `json:"picStructPresent"`
	TimingInfoPresent bool                   `json:"timingInfoPresent"`
	NumUnitsInTick    uint32                 `json:"numUnitsInTick"`
	TimeScale         uint32                 `json:"timeScale"`
	FixedFrameRate    bool                   `json:"fixedFrameRate"`
}

type AVCSEITimingContext struct {
	State        string                  `json:"state"`
	Source       string                  `json:"source,omitempty"`
	SPSID        *uint32                 `json:"spsId,omitempty"`
	SPSSpan      *Span                   `json:"spsSpan,omitempty"`
	EvidenceSpan *Span                   `json:"evidenceSpan,omitempty"`
	Parameters   *AVCSPSTimingParameters `json:"parameters,omitempty"`
}

type AVCSEICPBInitialDelay struct {
	Index                        uint32                  `json:"index"`
	InitialCPBRemovalDelay       *AVCMappedUnsignedField `json:"initialCpbRemovalDelay,omitempty"`
	InitialCPBRemovalDelayOffset *AVCMappedUnsignedField `json:"initialCpbRemovalDelayOffset,omitempty"`
}

type AVCSEIBufferingPeriod struct {
	SequenceParameterSetID *AVCMappedUnsignedField `json:"sequenceParameterSetId,omitempty"`
	Context                AVCSEITimingContext     `json:"context"`
	NALInitialDelays       []AVCSEICPBInitialDelay `json:"nalInitialDelays"`
	VCLInitialDelays       []AVCSEICPBInitialDelay `json:"vclInitialDelays"`
	PayloadExtensionBits   *AVCMappedSpan          `json:"payloadExtensionBits,omitempty"`
	PayloadAlignmentBits   *AVCMappedSpan          `json:"payloadAlignmentBits,omitempty"`
}

type AVCSEIPictureTiming struct {
	Context              AVCSEITimingContext     `json:"context"`
	CPBRemovalDelay      *AVCMappedUnsignedField `json:"cpbRemovalDelay,omitempty"`
	DPBOutputDelay       *AVCMappedUnsignedField `json:"dpbOutputDelay,omitempty"`
	PicStruct            *AVCMappedUnsignedField `json:"picStruct,omitempty"`
	ClockTimestamps      []AVCSEIClockTimestamp  `json:"clockTimestamps"`
	PayloadExtensionBits *AVCMappedSpan          `json:"payloadExtensionBits,omitempty"`
	PayloadAlignmentBits *AVCMappedSpan          `json:"payloadAlignmentBits,omitempty"`
}

type AVCSEIClockTimestamp struct {
	Index              uint32                  `json:"index"`
	ClockTimestampFlag *AVCMappedBooleanField  `json:"clockTimestampFlag,omitempty"`
	CTType             *AVCMappedUnsignedField `json:"ctType,omitempty"`
	NuitFieldBasedFlag *AVCMappedBooleanField  `json:"nuitFieldBasedFlag,omitempty"`
	CountingType       *AVCMappedUnsignedField `json:"countingType,omitempty"`
	FullTimestampFlag  *AVCMappedBooleanField  `json:"fullTimestampFlag,omitempty"`
	DiscontinuityFlag  *AVCMappedBooleanField  `json:"discontinuityFlag,omitempty"`
	CntDroppedFlag     *AVCMappedBooleanField  `json:"cntDroppedFlag,omitempty"`
	NFrames            *AVCMappedUnsignedField `json:"nFrames,omitempty"`
	SecondsFlag        *AVCMappedBooleanField  `json:"secondsFlag,omitempty"`
	MinutesFlag        *AVCMappedBooleanField  `json:"minutesFlag,omitempty"`
	HoursFlag          *AVCMappedBooleanField  `json:"hoursFlag,omitempty"`
	SecondsValue       *AVCMappedUnsignedField `json:"secondsValue,omitempty"`
	MinutesValue       *AVCMappedUnsignedField `json:"minutesValue,omitempty"`
	HoursValue         *AVCMappedUnsignedField `json:"hoursValue,omitempty"`
	TimeOffset         *AVCMappedSignedField   `json:"timeOffset,omitempty"`
}

func mapTimingHRD(h avc.HRDTimingParameters) AVCHRDTimingParameters {
	return AVCHRDTimingParameters{h.Present, h.CPBCount, h.InitialCPBRemovalDelayLength, h.CPBRemovalDelayLength, h.DPBOutputDelayLength, h.TimeOffsetLength}
}

func mapTimingContext(c avc.SEITimingContext) AVCSEITimingContext {
	r := AVCSEITimingContext{State: c.State, Source: c.Source, SPSID: c.SPSID}
	if c.SPSSpan != nil {
		s := mapSpan(*c.SPSSpan)
		r.SPSSpan = &s
	}
	if c.EvidenceSpan != nil {
		s := mapSpan(*c.EvidenceSpan)
		r.EvidenceSpan = &s
	}
	if p := c.Parameters; p != nil {
		r.Parameters = &AVCSPSTimingParameters{mapTimingHRD(p.NALHRD), mapTimingHRD(p.VCLHRD), p.PicStructPresent, p.TimingInfoPresent, p.NumUnitsInTick, p.TimeScale, p.FixedFrameRate}
	}
	return r
}

func mapSEITimingSpan(s *avc.MappedSpan) *AVCMappedSpan {
	if s == nil {
		return nil
	}
	r := mapAVCMappedSpan(*s)
	return &r
}

func mapInitialDelays(values []avc.SEICPBInitialDelay) []AVCSEICPBInitialDelay {
	r := make([]AVCSEICPBInitialDelay, 0, len(values))
	for _, v := range values {
		r = append(r, AVCSEICPBInitialDelay{v.Index, mapVUIUint(v.InitialCPBRemovalDelay), mapVUIUint(v.InitialCPBRemovalDelayOffset)})
	}
	return r
}

func mapSEIV6(p *avc.SEIMessageSequence) *AVCSEIMessageSequence {
	r := mapSEI(p)
	if r == nil {
		return nil
	}
	for i, m := range p.Messages {
		n := &r.Messages[i]
		n.Kind, n.SyntaxSupported = m.Kind, m.SyntaxSupported
		if v := m.BufferingPeriod; v != nil {
			n.BufferingPeriod = &AVCSEIBufferingPeriod{mapVUIUint(v.SequenceParameterSetID), mapTimingContext(v.Context), mapInitialDelays(v.NALInitialDelays), mapInitialDelays(v.VCLInitialDelays), mapSEITimingSpan(v.PayloadExtensionBits), mapSEITimingSpan(v.PayloadAlignmentBits)}
		}
		if v := m.PictureTiming; v != nil {
			n.PictureTiming = &AVCSEIPictureTiming{Context: mapTimingContext(v.Context), CPBRemovalDelay: mapVUIUint(v.CPBRemovalDelay), DPBOutputDelay: mapVUIUint(v.DPBOutputDelay), PicStruct: mapVUIUint(v.PicStruct), ClockTimestamps: make([]AVCSEIClockTimestamp, 0, len(v.ClockTimestamps)), PayloadExtensionBits: mapSEITimingSpan(v.PayloadExtensionBits), PayloadAlignmentBits: mapSEITimingSpan(v.PayloadAlignmentBits)}
			for _, c := range v.ClockTimestamps {
				n.PictureTiming.ClockTimestamps = append(n.PictureTiming.ClockTimestamps, AVCSEIClockTimestamp{
					Index: c.Index, ClockTimestampFlag: mapVUIBool(c.ClockTimestampFlag), CTType: mapVUIUint(c.CTType), NuitFieldBasedFlag: mapVUIBool(c.NuitFieldBasedFlag), CountingType: mapVUIUint(c.CountingType), FullTimestampFlag: mapVUIBool(c.FullTimestampFlag), DiscontinuityFlag: mapVUIBool(c.DiscontinuityFlag), CntDroppedFlag: mapVUIBool(c.CntDroppedFlag), NFrames: mapVUIUint(c.NFrames), SecondsFlag: mapVUIBool(c.SecondsFlag), MinutesFlag: mapVUIBool(c.MinutesFlag), HoursFlag: mapVUIBool(c.HoursFlag), SecondsValue: mapVUIUint(c.SecondsValue), MinutesValue: mapVUIUint(c.MinutesValue), HoursValue: mapVUIUint(c.HoursValue), TimeOffset: mapPrefixSigned(c.TimeOffset),
				})
			}
		}
	}
	return r
}

var ErrDetailV6Unsupported = errors.New("report: detail schema version 6 supports only AVC")

func NewTagDetailV6(size uint64, tag flv.TagRecord, parsed flv.ParsedTagDetail, parameters avccontext.ParameterResolution) (ParsedTagDetailDocument, error) {
	switch p := parsed.(type) {
	case flv.AVCVideo:
		return NewAVCTagDetailV6(size, tag, p, parameters), nil
	case *flv.AVCVideo:
		if p != nil {
			return NewAVCTagDetailV6(size, tag, *p, parameters), nil
		}
	}
	return nil, fmt.Errorf("%w: %T", ErrDetailV6Unsupported, parsed)
}

func NewAVCTagDetailV6(size uint64, tag flv.TagRecord, p flv.AVCVideo, parameters avccontext.ParameterResolution) AVCTagDetailDocument {
	d := NewAVCTagDetailV4(size, tag, p, parameters)
	d.SchemaVersion = 6
	attach := func(mapped []AVCNALUnitDetail, sets []avc.ParameterSet) {
		for i, set := range sets {
			mapped[i].SEI = mapSEIV6(set.SEI)
		}
	}
	if c := d.Detail.AVCDecoderConfigurationRecord; c != nil {
		attach(c.SequenceParameterSets, p.Config.SequenceParameterSets)
		attach(c.PictureParameterSets, p.Config.PictureParameterSets)
		if c.HighProfileExtension != nil {
			attach(c.HighProfileExtension.SequenceParameterSetExts, p.Config.SequenceParameterSetExts)
		}
	}
	if seq := d.Detail.NALUnitSequence; seq != nil {
		for i, unit := range p.NALUnits.NALUnits {
			seq.NALUnits[i].SEI = mapSEIV6(unit.SEI)
		}
	}
	return d
}
