package report

import (
	"errors"
	"fmt"

	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

var ErrDetailV3Unsupported = errors.New("report: detail schema version 3 supports only AVC and MP3")

// NewTagDetailV3 preserves the older projections and publishes only explicitly
// requested PPS/slice prefixes. Prefix completeness never claims decoding.
func NewTagDetailV3(size uint64, tag flv.TagRecord, parsed flv.ParsedTagDetail, parameters avccontext.ParameterResolution) (ParsedTagDetailDocument, error) {
	switch p := parsed.(type) {
	case flv.AVCVideo:
		return NewAVCTagDetailV3(size, tag, p, parameters), nil
	case *flv.AVCVideo:
		if p != nil {
			return NewAVCTagDetailV3(size, tag, *p, parameters), nil
		}
	case flv.MP3Audio:
		return NewMP3TagDetailV3(size, tag, p), nil
	case *flv.MP3Audio:
		if p != nil {
			return NewMP3TagDetailV3(size, tag, *p), nil
		}
	}
	return nil, fmt.Errorf("%w: %T", ErrDetailV3Unsupported, parsed)
}

type AVCParameterContextDetail struct {
	State          string `json:"state"`
	StartTagID     uint64 `json:"startTagId"`
	NextTagID      uint64 `json:"nextTagId"`
	SelectedTagID  uint64 `json:"selectedTagId"`
	ScannedTags    uint64 `json:"scannedTags"`
	VideoTags      uint64 `json:"videoTags"`
	Configurations uint64 `json:"configurations"`
	NALPackets     uint64 `json:"nalPackets"`
	BytesRead      uint64 `json:"bytesRead"`
	BarrierCount   uint64 `json:"barrierCount"`
	SPSCount       uint32 `json:"spsCount"`
	PPSCount       uint32 `json:"ppsCount"`
}

type AVCPPSPrefixDetail struct {
	NumSliceGroupsMinus1               *AVCMappedUnsignedField `json:"numSliceGroupsMinus1,omitempty"`
	NumRefIdxL0DefaultActiveMinus1     *AVCMappedUnsignedField `json:"numRefIdxL0DefaultActiveMinus1,omitempty"`
	NumRefIdxL1DefaultActiveMinus1     *AVCMappedUnsignedField `json:"numRefIdxL1DefaultActiveMinus1,omitempty"`
	WeightedPredFlag                   *AVCMappedBooleanField  `json:"weightedPredFlag,omitempty"`
	WeightedBipredIDC                  *AVCMappedUnsignedField `json:"weightedBipredIdc,omitempty"`
	PicInitQPMinus26                   *AVCMappedSignedField   `json:"picInitQpMinus26,omitempty"`
	PicInitQSMinus26                   *AVCMappedSignedField   `json:"picInitQsMinus26,omitempty"`
	ChromaQPIndexOffset                *AVCMappedSignedField   `json:"chromaQpIndexOffset,omitempty"`
	DeblockingFilterControlPresentFlag *AVCMappedBooleanField  `json:"deblockingFilterControlPresentFlag,omitempty"`
	ConstrainedIntraPredFlag           *AVCMappedBooleanField  `json:"constrainedIntraPredFlag,omitempty"`
	RedundantPicCntPresentFlag         *AVCMappedBooleanField  `json:"redundantPicCntPresentFlag,omitempty"`
	RBSPBitOffset                      uint32                  `json:"rbspBitOffset"`
	SyntaxBits                         uint32                  `json:"syntaxBits"`
	RemainingRBSPBits                  uint32                  `json:"remainingRbspBits"`
	PrefixComplete                     bool                    `json:"prefixComplete"`
}

type AVCSlicePrefixDetail struct {
	RBSP                   AVCRBSPViewDetail       `json:"rbsp"`
	FirstMBInSlice         *AVCMappedUnsignedField `json:"firstMbInSlice,omitempty"`
	SliceType              *AVCMappedUnsignedField `json:"sliceType,omitempty"`
	SliceTypeName          string                  `json:"sliceTypeName,omitempty"`
	PictureParameterSetID  *AVCMappedUnsignedField `json:"pictureParameterSetId,omitempty"`
	ColourPlaneID          *AVCMappedUnsignedField `json:"colourPlaneId,omitempty"`
	FrameNum               *AVCMappedUnsignedField `json:"frameNum,omitempty"`
	FieldPicFlag           *AVCMappedBooleanField  `json:"fieldPicFlag,omitempty"`
	BottomFieldFlag        *AVCMappedBooleanField  `json:"bottomFieldFlag,omitempty"`
	IDRPicID               *AVCMappedUnsignedField `json:"idrPicId,omitempty"`
	PicOrderCntLSB         *AVCMappedUnsignedField `json:"picOrderCntLsb,omitempty"`
	DeltaPicOrderCntBottom *AVCMappedSignedField   `json:"deltaPicOrderCntBottom,omitempty"`
	DeltaPicOrderCnt0      *AVCMappedSignedField   `json:"deltaPicOrderCnt0,omitempty"`
	DeltaPicOrderCnt1      *AVCMappedSignedField   `json:"deltaPicOrderCnt1,omitempty"`
	RedundantPicCnt        *AVCMappedUnsignedField `json:"redundantPicCnt,omitempty"`
	ReferencedSPSID        *uint32                 `json:"referencedSpsId,omitempty"`
	ReferencedSPSSpan      *Span                   `json:"referencedSpsSpan,omitempty"`
	ReferencedPPSSpan      *Span                   `json:"referencedPpsSpan,omitempty"`
	SyntaxBits             uint32                  `json:"syntaxBits"`
	BytesRead              uint32                  `json:"bytesRead"`
	PrefixComplete         bool                    `json:"prefixComplete"`
}

func NewAVCTagDetailV3(size uint64, tag flv.TagRecord, p flv.AVCVideo, parameters avccontext.ParameterResolution) AVCTagDetailDocument {
	d := NewAVCTagDetailV2(size, tag, p)
	d.SchemaVersion = 3
	d.Detail.ParameterContext = &AVCParameterContextDetail{
		State: parameters.State, StartTagID: parameters.StartTagID, NextTagID: parameters.NextTagID,
		SelectedTagID: parameters.SelectedTagID, ScannedTags: parameters.ScannedTags,
		VideoTags: parameters.VideoTags, Configurations: parameters.Configurations,
		NALPackets: parameters.NALPackets, BytesRead: parameters.BytesRead,
		BarrierCount: parameters.BarrierCount, SPSCount: parameters.SPSCount, PPSCount: parameters.PPSCount,
	}
	if d.Detail.ParameterContext.State == "" {
		d.Detail.ParameterContext.State = avccontext.StateNotNeeded
	}
	attach := func(mapped []AVCNALUnitDetail, sets []avc.ParameterSet) {
		for i, set := range sets {
			if set.PictureParameterSet != nil {
				mapped[i].PictureParameterSet.Prefix = mapPPSPrefix(set.PictureParameterSet.Deep)
			}
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
			if unit.PictureParameterSet != nil {
				seq.NALUnits[i].PictureParameterSet.Prefix = mapPPSPrefix(unit.PictureParameterSet.Deep)
			}
			seq.NALUnits[i].SliceHeaderPrefix = mapSlicePrefix(unit.SliceHeader)
		}
	}
	return d
}

func mapPPSPrefix(p *avc.PPSDetails) *AVCPPSPrefixDetail {
	if p == nil {
		return nil
	}
	return &AVCPPSPrefixDetail{
		NumSliceGroupsMinus1:           mapVUIUint(p.NumSliceGroupsMinus1),
		NumRefIdxL0DefaultActiveMinus1: mapVUIUint(p.NumRefIdxL0DefaultActiveMinus1),
		NumRefIdxL1DefaultActiveMinus1: mapVUIUint(p.NumRefIdxL1DefaultActiveMinus1),
		WeightedPredFlag:               mapVUIBool(p.WeightedPredFlag), WeightedBipredIDC: mapVUIUint(p.WeightedBipredIDC),
		PicInitQPMinus26: mapPrefixSigned(p.PicInitQPMinus26), PicInitQSMinus26: mapPrefixSigned(p.PicInitQSMinus26),
		ChromaQPIndexOffset:                mapPrefixSigned(p.ChromaQPIndexOffset),
		DeblockingFilterControlPresentFlag: mapVUIBool(p.DeblockingFilterControlPresentFlag),
		ConstrainedIntraPredFlag:           mapVUIBool(p.ConstrainedIntraPredFlag), RedundantPicCntPresentFlag: mapVUIBool(p.RedundantPicCntPresentFlag),
		RBSPBitOffset: p.RBSPBitOffset, SyntaxBits: p.SyntaxBits, RemainingRBSPBits: p.RemainingRBSPBits, PrefixComplete: p.PrefixComplete,
	}
}

func mapSlicePrefix(p *avc.SliceHeader) *AVCSlicePrefixDetail {
	if p == nil {
		return nil
	}
	r := &AVCSlicePrefixDetail{
		RBSP: mapAVCRBSP(p.RBSP), FirstMBInSlice: mapVUIUint(p.FirstMBInSlice), SliceType: mapVUIUint(p.SliceType), SliceTypeName: p.SliceTypeName,
		PictureParameterSetID: mapVUIUint(p.PictureParameterSetID), ColourPlaneID: mapVUIUint(p.ColourPlaneID), FrameNum: mapVUIUint(p.FrameNum),
		FieldPicFlag: mapVUIBool(p.FieldPicFlag), BottomFieldFlag: mapVUIBool(p.BottomFieldFlag), IDRPicID: mapVUIUint(p.IDRPicID),
		PicOrderCntLSB: mapVUIUint(p.PicOrderCntLSB), DeltaPicOrderCntBottom: mapPrefixSigned(p.DeltaPicOrderCntBottom),
		DeltaPicOrderCnt0: mapPrefixSigned(p.DeltaPicOrderCnt0), DeltaPicOrderCnt1: mapPrefixSigned(p.DeltaPicOrderCnt1),
		RedundantPicCnt: mapVUIUint(p.RedundantPicCnt), ReferencedSPSID: p.ReferencedSPSID,
		SyntaxBits: p.SyntaxBits, BytesRead: p.BytesRead, PrefixComplete: p.PrefixComplete,
	}
	if p.ReferencedSPSSpan != nil {
		s := mapSpan(*p.ReferencedSPSSpan)
		r.ReferencedSPSSpan = &s
	}
	if p.ReferencedPPSSpan != nil {
		s := mapSpan(*p.ReferencedPPSSpan)
		r.ReferencedPPSSpan = &s
	}
	return r
}

func mapPrefixSigned(p *avc.SignedField) *AVCMappedSignedField {
	if p == nil {
		return nil
	}
	return avcMappedSignedField(p.Value, p.Span)
}
