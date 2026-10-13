package avc

import "github.com/sevico/muxray/internal/cursor"

// SPSTimingParameters is a fixed-size, validated SPS timing dependency. It
// retains no RBSP, dynamic mapping, CPB entries, or source handle.
type SPSTimingParameters struct {
	SPSID                     uint32
	SPSSpan                   cursor.Span
	NALHRD, VCLHRD            HRDTimingParameters
	PicStructPresent          bool
	TimingInfoPresent         bool
	NumUnitsInTick, TimeScale uint32
	FixedFrameRate            bool
}

type HRDTimingParameters struct {
	Present                                                                                     bool
	CPBCount                                                                                    uint32
	InitialCPBRemovalDelayLength, CPBRemovalDelayLength, DPBOutputDelayLength, TimeOffsetLength uint8
}

// SEITimingContext describes the evidence used to select syntax bit widths.
// State is resolved, missing, or ambiguous. A resolved context always has the
// original SPS span; Source identifies bufferingPeriod or same-packet slice.
// No clock conversion, HRD schedule, or decoded-picture timing is inferred.
type SEITimingContext struct {
	State        string
	Source       string
	SPSID        *uint32
	SPSSpan      *cursor.Span
	EvidenceSpan *cursor.Span
	Parameters   *SPSTimingParameters
}

type SEICPBInitialDelay struct {
	Index                                                uint32
	InitialCPBRemovalDelay, InitialCPBRemovalDelayOffset *VUIUnsignedField
}

type SEIBufferingPeriod struct {
	SequenceParameterSetID                     *VUIUnsignedField
	Context                                    SEITimingContext
	NALInitialDelays, VCLInitialDelays         []SEICPBInitialDelay
	PayloadExtensionBits, PayloadAlignmentBits *MappedSpan
}

type SEIPictureTiming struct {
	Context                                    SEITimingContext
	CPBRemovalDelay, DPBOutputDelay, PicStruct *VUIUnsignedField
	ClockTimestamps                            []SEIClockTimestamp
	PayloadExtensionBits, PayloadAlignmentBits *MappedSpan
}

type SEIClockTimestamp struct {
	Index                                                uint32
	ClockTimestampFlag                                   *VUIBooleanField
	CTType                                               *VUIUnsignedField
	NuitFieldBasedFlag                                   *VUIBooleanField
	CountingType                                         *VUIUnsignedField
	FullTimestampFlag, DiscontinuityFlag, CntDroppedFlag *VUIBooleanField
	NFrames                                              *VUIUnsignedField
	SecondsFlag, MinutesFlag, HoursFlag                  *VUIBooleanField
	SecondsValue, MinutesValue, HoursValue               *VUIUnsignedField
	TimeOffset                                           *SignedField
}
