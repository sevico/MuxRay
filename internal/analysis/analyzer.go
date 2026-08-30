package analysis

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

var ErrNilSource = errors.New("analysis: source is nil")

const bitrateBasis = "flvMediaTagCoveredDataSizeOverForwardDtsIntervals"
const avSyncBasis = "videoPtsExtremaMinusAudioDtsExtrema"

// Analyze derives cross-tag timing, bitrate, keyframe/GOP, configuration, and
// A/V range summaries. It reads only codec framing prefixes and sequence
// headers; ordinary media payloads remain in the source.
func Analyze(ctx context.Context, source io.ReaderAt, scan flv.ScanResult, options Options) (Result, error) {
	result := Result{Complete: !scan.HasErrors()}
	if ctx == nil {
		ctx = context.Background()
	}
	if source == nil {
		result.Complete = false
		return result, ErrNilSource
	}
	normalized, err := options.Normalized()
	if err != nil {
		result.Complete = false
		return result, err
	}

	a := analyzer{
		ctx: ctx, source: source, options: normalized, result: result,
		diagnostics:                   diagnosticCollector{limit: normalized.MaxDiagnostics},
		previousKeyframeRetainedIndex: -1,
	}
	a.result.Keyframes.Entries = make([]Keyframe, 0, minCapacity(len(scan.Tags), normalized.MaxKeyframes))
	a.result.AAC.Events = make([]AACConfiguration, 0, minCapacity(len(scan.Tags), normalized.MaxConfigurationEvents))
	a.result.AVC.Events = make([]AVCConfiguration, 0, minCapacity(len(scan.Tags), normalized.MaxConfigurationEvents))

	for index, tag := range scan.Tags {
		if index&1023 == 0 {
			if err := a.ctx.Err(); err != nil {
				a.result.Complete = false
				return a.finish(), err
			}
		}
		if err := a.visit(tag); err != nil {
			a.result.Complete = false
			return a.finish(), err
		}
	}
	if err := a.ctx.Err(); err != nil {
		a.result.Complete = false
		return a.finish(), err
	}
	return a.finish(), nil
}

type analyzer struct {
	ctx         context.Context
	source      io.ReaderAt
	options     Options
	result      Result
	diagnostics diagnosticCollector

	previousAACFingerprint        string
	haveAACFingerprint            bool
	previousAVCFingerprint        string
	haveAVCFingerprint            bool
	avcGOPConfigUncertain         bool
	previousKeyframeDTS           int64
	havePreviousKeyframe          bool
	previousKeyframeRetainedIndex int
	gopDurationTotal              uint64
	configurationBytesUsed        uint64
	audioBitrate                  bitrateAccumulator
	videoBitrate                  bitrateAccumulator
	framingScratch                [5]byte
	hashScratch                   [32 << 10]byte
}

func (a *analyzer) visit(tag flv.TagRecord) error {
	a.result.Tags.Total++
	a.result.Tags.DataSizeBytes += uint64(tag.DataSize)
	switch tag.Type() {
	case flv.TagTypeAudio:
		a.result.Tags.Audio++
		a.result.Audio.TagCount++
		a.result.Audio.IndexedDataSizeBytes += uint64(tag.DataSize)
		return a.visitAudio(tag)
	case flv.TagTypeVideo:
		a.result.Tags.Video++
		a.result.Video.TagCount++
		a.result.Video.IndexedDataSizeBytes += uint64(tag.DataSize)
		return a.visitVideo(tag)
	case flv.TagTypeScript:
		a.result.Tags.Script++
	default:
		a.result.Tags.Other++
	}
	return nil
}

func (a *analyzer) visitAudio(tag flv.TagRecord) error {
	if !a.usable(tag, &a.result.Audio) {
		a.resetAACEpoch()
		return nil
	}
	prefix := a.framingScratch[:2]
	if tag.DataSize == 0 {
		a.resetAACEpoch()
		a.unclassified(&a.result.Audio, Diagnostic{
			Code: CodeAudioHeaderTruncated, Severity: SeverityError, Offset: tag.PayloadSpan().ByteOffset,
			Message: "FLV audio tag header is missing", Expected: number(1), Actual: number(0),
		}, tag)
		return nil
	}
	if err := readAtLeast(a.source, prefix[:1], tag.PayloadSpan().ByteOffset); err != nil {
		return fmt.Errorf("analysis: read audio tag %d header: %w", tag.ID, err)
	}
	if soundFormat := prefix[0] >> 4; soundFormat != flv.SoundFormatAAC {
		a.resetAACEpoch()
		a.unclassified(&a.result.Audio, Diagnostic{
			Code: CodeAudioCodecUnsupported, Severity: SeverityWarning,
			Offset: tag.PayloadSpan().ByteOffset, BitOffset: 0,
			Message: "audio codec is outside the AAC media-summary v1 scope",
			Actual:  number(uint64(soundFormat)),
		}, tag)
		return nil
	}
	if tag.DataSize < 2 {
		a.unclassified(&a.result.Audio, Diagnostic{
			Code: CodeAACPacketTypeTruncated, Severity: SeverityError,
			Offset:  tag.PayloadSpan().ByteOffset + uint64(tag.DataSize),
			Message: "FLV AAC packet type is missing", Expected: number(2), Actual: number(uint64(tag.DataSize)),
		}, tag)
		return nil
	}
	if err := readAtLeast(a.source, prefix[1:2], tag.PayloadSpan().ByteOffset+1); err != nil {
		return fmt.Errorf("analysis: read AAC tag %d packet type: %w", tag.ID, err)
	}
	switch flv.AACPacketType(prefix[1]) {
	case flv.AACPacketTypeSequenceHeader:
		a.result.Audio.ExcludedTagCount++
		return a.visitAACConfiguration(tag)
	case flv.AACPacketTypeRaw:
		a.addMedia(&a.result.Audio, &a.audioBitrate, tag)
	default:
		a.unclassified(&a.result.Audio, Diagnostic{
			Code: CodeAACPacketTypeUnknown, Severity: SeverityError,
			Offset:  tag.PayloadSpan().ByteOffset + 1,
			Message: "FLV AAC packet type is neither sequence header nor raw AAC",
			Actual:  number(uint64(prefix[1])),
		}, tag)
	}
	return nil
}

func (a *analyzer) visitVideo(tag flv.TagRecord) error {
	if !a.usable(tag, &a.result.Video) {
		a.resetGOP()
		a.resetAVCEpoch()
		return nil
	}
	prefix := a.framingScratch[:5]
	if tag.DataSize == 0 {
		a.resetGOP()
		a.resetAVCEpoch()
		a.unclassified(&a.result.Video, Diagnostic{
			Code: CodeVideoHeaderTruncated, Severity: SeverityError, Offset: tag.PayloadSpan().ByteOffset,
			Message: "FLV video tag header is missing", Expected: number(1), Actual: number(0),
		}, tag)
		return nil
	}
	if err := readAtLeast(a.source, prefix[:1], tag.PayloadSpan().ByteOffset); err != nil {
		return fmt.Errorf("analysis: read video tag %d header: %w", tag.ID, err)
	}
	frameType, codecID := prefix[0]>>4, prefix[0]&0x0f
	if frameType == 5 {
		a.result.Video.ExcludedTagCount++
		return nil
	}
	if codecID != flv.CodecIDAVC {
		a.resetGOP()
		a.resetAVCEpoch()
		a.unclassified(&a.result.Video, Diagnostic{
			Code: CodeVideoCodecUnsupported, Severity: SeverityWarning,
			Offset: tag.PayloadSpan().ByteOffset, BitOffset: 4,
			Message: "video codec is outside the AVC media-summary v1 scope",
			Actual:  number(uint64(codecID)),
		}, tag)
		return nil
	}

	pts, ptsPresent := tag.DTS, false
	if tag.DataSize < 5 {
		a.resetGOP()
		a.unclassified(&a.result.Video, Diagnostic{
			Code: CodeAVCFramingTruncated, Severity: SeverityError,
			Offset:   tag.PayloadSpan().ByteOffset + uint64(tag.DataSize),
			Message:  "FLV AVC packet type or composition time is truncated",
			Expected: number(5), Actual: number(uint64(tag.DataSize)),
		}, tag)
		return nil
	}
	if err := readAtLeast(a.source, prefix[1:], tag.PayloadSpan().ByteOffset+1); err != nil {
		return fmt.Errorf("analysis: read AVC tag %d framing: %w", tag.ID, err)
	}
	switch flv.AVCPacketType(prefix[1]) {
	case flv.AVCPacketTypeSequenceHeader:
		a.result.Video.ExcludedTagCount++
		return a.visitAVCConfiguration(tag)
	case flv.AVCPacketTypeEndOfSequence:
		a.appendAVCFrameTypeDiagnostics(tag, frameType)
		a.appendAVCEndOfSequenceDiagnostics(tag, signedInt24(prefix[2:5]))
		a.resetGOP()
		a.resetAVCEpoch()
		a.result.Video.ExcludedTagCount++
		return nil
	case flv.AVCPacketTypeNALU:
		a.appendAVCFrameTypeDiagnostics(tag, frameType)
		composition := signedInt24(prefix[2:5])
		a.addCompositionTime(composition)
		pts = tag.DTS + int64(composition)
		ptsPresent = true
		a.result.VideoPTS.add(pts)
	default:
		a.appendAVCFrameTypeDiagnostics(tag, frameType)
		a.resetGOP()
		a.unclassified(&a.result.Video, Diagnostic{
			Code: CodeAVCPacketTypeUnknown, Severity: SeverityError,
			Offset:  tag.PayloadSpan().ByteOffset + 1,
			Message: "FLV AVC packet type is neither sequence header, NALU, nor end of sequence",
			Actual:  number(uint64(prefix[1])),
		}, tag)
		return nil
	}

	if a.result.Video.Timing.Present && tag.DTS < a.result.Video.Timing.LastMS {
		a.resetGOP()
	}
	a.addMedia(&a.result.Video, &a.videoBitrate, tag)
	if frameType == 1 {
		a.addKeyframe(tag, codecID, pts, ptsPresent)
	}
	return nil
}

func (a *analyzer) appendAVCFrameTypeDiagnostics(tag flv.TagRecord, frameType uint8) {
	var code, message string
	switch {
	case frameType == 0 || frameType > 5:
		code = flv.CodeAVCFrameTypeReserved
		message = "FLV AVC frame type is reserved"
	case frameType == 3:
		code = flv.CodeAVCFrameTypeUnexpected
		message = "FLV disposable inter frame type is defined only for H.263 video"
	default:
		return
	}
	a.diagnostics.add(Diagnostic{
		Code: code, Severity: SeverityWarning, TagID: tagID(tag.ID),
		Offset: tag.PayloadSpan().ByteOffset, BitOffset: 0, Message: message,
		Actual: number(uint64(frameType)),
	})
}

func (a *analyzer) appendAVCEndOfSequenceDiagnostics(tag flv.TagRecord, composition int32) {
	payloadStart := tag.PayloadSpan().ByteOffset
	payloadEnd := payloadStart + uint64(tag.DataSize)
	if composition != 0 {
		a.diagnostics.add(Diagnostic{
			Code: flv.CodeAVCCompositionTimeNonZero, Severity: SeverityWarning, TagID: tagID(tag.ID),
			Offset: payloadStart + 2, BitOffset: 0,
			Message:  "FLV AVC sequence-header and end-of-sequence composition time should be zero",
			Expected: number(0), Actual: number(uint64(uint32(composition) & 0x00ff_ffff)),
		})
	}
	if tag.DataSize > 5 {
		a.result.Complete = false
		a.diagnostics.add(Diagnostic{
			Code: flv.CodeAVCEndOfSequencePayloadUnexpected, Severity: SeverityError, TagID: tagID(tag.ID),
			Offset:   payloadStart + 5,
			Message:  "FLV AVC end-of-sequence packet contains unexpected payload",
			Expected: number(0), Actual: number(uint64(tag.DataSize - 5)), RecoveryOffset: number(payloadEnd),
		})
	}
}

func (a *analyzer) usable(tag flv.TagRecord, track *Track) bool {
	if tag.Filter() {
		a.unclassified(track, Diagnostic{
			Code: CodeFilteredTagSkipped, Severity: SeverityWarning, Offset: tag.Offset,
			Message: "filtered FLV media tag was excluded from media analysis",
		}, tag)
		return false
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		a.unclassified(track, Diagnostic{
			Code: CodeIncompleteTagSkipped, Severity: SeverityError, Offset: tag.PayloadSpan().ByteOffset,
			Message:  "incomplete FLV media tag was excluded from media analysis",
			Expected: number(uint64(tag.DataSize)), Actual: number(uint64(tag.AvailableDataSize)),
		}, tag)
		return false
	}
	return true
}

func (a *analyzer) unclassified(track *Track, diagnostic Diagnostic, tag flv.TagRecord) {
	track.UnclassifiedTagCount++
	a.result.Complete = false
	diagnostic.TagID = tagID(tag.ID)
	if diagnostic.Severity == SeverityError && diagnostic.RecoveryOffset == nil {
		diagnostic.RecoveryOffset = number(tag.PayloadSpan().ByteOffset + uint64(tag.AvailableDataSize))
	}
	a.diagnostics.add(diagnostic)
}

func (a *analyzer) addMedia(track *Track, bitrate *bitrateAccumulator, tag flv.TagRecord) {
	track.MediaTagCount++
	track.MediaDataSizeBytes += uint64(tag.DataSize)
	track.Timing.add(tag.DTS)
	bitrate.add(tag.DTS, uint64(tag.DataSize))
}

func (a *analyzer) addCompositionTime(value int32) {
	composition := &a.result.Composition
	if !composition.Present {
		composition.Present = true
		composition.MinMS = value
		composition.MaxMS = value
	} else {
		if value < composition.MinMS {
			composition.MinMS = value
		}
		if value > composition.MaxMS {
			composition.MaxMS = value
		}
	}
	composition.SampleCount++
	switch {
	case value < 0:
		composition.NegativeCount++
	case value > 0:
		composition.PositiveCount++
	default:
		composition.ZeroCount++
	}
}

func (a *analyzer) addKeyframe(tag flv.TagRecord, codecID uint8, pts int64, ptsPresent bool) {
	keyframes := &a.result.Keyframes
	keyframes.DeclaredCount++
	a.result.GOP.Count++

	if a.havePreviousKeyframe {
		delta := tag.DTS - a.previousKeyframeDTS
		if delta <= 0 {
			a.result.GOP.NonPositiveDurationCount++
			a.resetGOP()
		} else {
			duration := uint64(delta)
			gop := &a.result.GOP
			if gop.ClosedDurationCount == 0 || duration < gop.MinDurationMS {
				gop.MinDurationMS = duration
			}
			if duration > gop.MaxDurationMS {
				gop.MaxDurationMS = duration
			}
			gop.ClosedDurationCount++
			a.gopDurationTotal += duration
			if a.previousKeyframeRetainedIndex >= 0 {
				previous := &keyframes.Entries[a.previousKeyframeRetainedIndex]
				previous.GOPDurationMS = duration
				previous.GOPDurationPresent = true
			}
		}
	}
	a.previousKeyframeDTS = tag.DTS
	a.havePreviousKeyframe = true

	if uint32(len(keyframes.Entries)) < a.options.MaxKeyframes {
		keyframes.Entries = append(keyframes.Entries, Keyframe{
			TagID: tag.ID, Offset: tag.Offset, DTS: tag.DTS,
			PTS: pts, PTSPresent: ptsPresent, DataSize: tag.DataSize, CodecID: codecID,
		})
		a.previousKeyframeRetainedIndex = len(keyframes.Entries) - 1
	} else {
		keyframes.Truncated = true
		a.previousKeyframeRetainedIndex = -1
	}
}

func (a *analyzer) resetGOP() {
	a.havePreviousKeyframe = false
	a.previousKeyframeRetainedIndex = -1
}

func (a *analyzer) resetAACEpoch() {
	a.previousAACFingerprint = ""
	a.haveAACFingerprint = false
}

func (a *analyzer) resetAVCEpoch() {
	a.previousAVCFingerprint = ""
	a.haveAVCFingerprint = false
	a.avcGOPConfigUncertain = false
}

func (a *analyzer) visitAACConfiguration(tag flv.TagRecord) error {
	a.result.AAC.EventCount++
	if !a.reserveConfigurationBytes(uint64(tag.DataSize-2), tag.PayloadSpan().ByteOffset+2, tag, &a.result.AAC.SkippedByBudgetCount) {
		return nil
	}
	detail, err := flv.ParseAACAudio(a.source, tag, a.options.AAC)
	if err != nil {
		return fmt.Errorf("analysis: parse AAC sequence header tag %d: %w", tag.ID, err)
	}
	fingerprint, err := a.hashSpan(detail.DataSpan)
	if err != nil {
		return fmt.Errorf("analysis: hash AAC sequence header tag %d: %w", tag.ID, err)
	}
	event := AACConfiguration{
		TagID: tag.ID, Offset: tag.Offset, DTS: tag.DTS, FingerprintSHA256: fingerprint,
		ComparedToPrevious:  detail.Complete && a.haveAACFingerprint,
		ChangedFromPrevious: detail.Complete && a.haveAACFingerprint && fingerprint != a.previousAACFingerprint,
		Complete:            detail.Complete,
	}
	if event.ChangedFromPrevious {
		a.result.AAC.ChangeCount++
	}
	if detail.Complete {
		a.previousAACFingerprint, a.haveAACFingerprint = fingerprint, true
	}
	a.result.AAC.AnalyzedCount++

	config := detail.Config
	if config.AudioObjectType.Complete {
		event.AudioObjectTypePresent = true
		event.AudioObjectType = uint8(config.AudioObjectType.Value)
		event.AudioObjectTypeName = config.AudioObjectType.Name
	}
	if config.SamplingFrequency.Complete && config.SamplingFrequency.Hertz != 0 {
		event.SampleRatePresent = true
		event.SamplingFrequencyHz = config.SamplingFrequency.Hertz
	}
	if config.ExtensionSamplingFrequency.Complete && config.ExtensionSamplingFrequency.Hertz != 0 {
		event.ExtensionSamplingFrequencyPresent = true
		event.ExtensionSamplingFrequencyHz = config.ExtensionSamplingFrequency.Hertz
	}
	if config.ChannelConfiguration.Complete {
		event.ChannelConfigurationPresent = true
		event.ChannelConfiguration = config.ChannelConfiguration.Value
		event.ChannelConfigurationName = config.ChannelConfiguration.Name
		if count, ok := channelCount(config.ChannelConfiguration.Value); ok {
			event.ChannelCountPresent = true
			event.ChannelCount = count
		}
	}
	a.appendAACDiagnostics(tag.ID, detail.Diagnostics)
	if !detail.Complete {
		a.result.Complete = false
	}
	if uint32(len(a.result.AAC.Events)) < a.options.MaxConfigurationEvents {
		a.result.AAC.Events = append(a.result.AAC.Events, event)
	} else {
		a.result.AAC.Truncated = true
	}
	return nil
}

func (a *analyzer) visitAVCConfiguration(tag flv.TagRecord) error {
	a.result.AVC.EventCount++
	if !a.reserveConfigurationBytes(uint64(tag.DataSize-5), tag.PayloadSpan().ByteOffset+5, tag, &a.result.AVC.SkippedByBudgetCount) {
		a.resetGOP()
		a.avcGOPConfigUncertain = true
		return nil
	}
	detail, err := flv.ParseAVCVideo(a.source, tag, flv.AVCVideoOptions{AVC: a.options.AVC})
	if err != nil {
		return fmt.Errorf("analysis: parse AVC sequence header tag %d: %w", tag.ID, err)
	}
	fingerprint, err := a.hashSpan(detail.DataSpan)
	if err != nil {
		return fmt.Errorf("analysis: hash AVC sequence header tag %d: %w", tag.ID, err)
	}
	event := AVCConfiguration{
		TagID: tag.ID, Offset: tag.Offset, DTS: tag.DTS, FingerprintSHA256: fingerprint,
		ComparedToPrevious:  detail.Complete && a.haveAVCFingerprint,
		ChangedFromPrevious: detail.Complete && a.haveAVCFingerprint && fingerprint != a.previousAVCFingerprint,
		Complete:            detail.Complete,
	}
	if event.ChangedFromPrevious {
		a.result.AVC.ChangeCount++
	}
	if !detail.Complete || !a.haveAVCFingerprint || event.ChangedFromPrevious || a.avcGOPConfigUncertain {
		a.resetGOP()
	}
	if detail.Complete {
		a.previousAVCFingerprint, a.haveAVCFingerprint = fingerprint, true
		a.avcGOPConfigUncertain = false
	} else {
		a.avcGOPConfigUncertain = true
	}
	a.result.AVC.AnalyzedCount++

	config := detail.Config
	if config.AVCProfileIndicationSpan.BitLength == 8 && config.ProfileCompatibilitySpan.BitLength == 8 && config.AVCLevelIndicationSpan.BitLength == 8 {
		event.ProfilePresent = true
		event.ProfileIDC = config.AVCProfileIndication
		event.ProfileCompatibility = config.ProfileCompatibility
		event.LevelIDC = config.AVCLevelIndication
	}
	if config.LengthSize == 1 || config.LengthSize == 2 || config.LengthSize == 4 {
		event.LengthSizePresent = true
		event.LengthSize = config.LengthSize
	}
	dimensions := make(map[[4]uint64]struct{})
	for _, parameterSet := range config.SequenceParameterSets {
		if parameterSet.SequenceParameterSet == nil || !parameterSet.SequenceParameterSet.Complete {
			continue
		}
		sps := parameterSet.SequenceParameterSet
		event.CompleteSPSCount++
		dimensions[[4]uint64{sps.CodedWidth, sps.CodedHeight, sps.DisplayWidth, sps.DisplayHeight}] = struct{}{}
		if !event.FirstCompleteSPSPresent {
			event.FirstCompleteSPSPresent = true
			event.FirstCompleteSPSIndex = parameterSet.Index
			event.FirstCompleteSPSID = sps.SequenceParameterSetID
			event.CodedWidth, event.CodedHeight = sps.CodedWidth, sps.CodedHeight
			event.DisplayWidth, event.DisplayHeight = sps.DisplayWidth, sps.DisplayHeight
		}
	}
	event.DistinctSPSDimensions = uint32(len(dimensions))
	a.appendAVCDiagnostics(tag.ID, detail.Diagnostics)
	if !detail.Complete {
		a.result.Complete = false
	}
	if uint32(len(a.result.AVC.Events)) < a.options.MaxConfigurationEvents {
		a.result.AVC.Events = append(a.result.AVC.Events, event)
	} else {
		a.result.AVC.Truncated = true
	}
	return nil
}

func (a *analyzer) reserveConfigurationBytes(size, dataOffset uint64, tag flv.TagRecord, skipped *uint64) bool {
	if size <= a.options.MaxConfigurationBytes-a.configurationBytesUsed {
		a.configurationBytesUsed += size
		return true
	}
	(*skipped)++
	a.result.Complete = false
	a.diagnostics.add(Diagnostic{
		Code: CodeConfigurationByteLimit, Severity: SeverityError, TagID: tagID(tag.ID),
		Offset:   dataOffset,
		Message:  "cumulative AAC/AVC configuration byte budget was exceeded",
		Expected: number(a.options.MaxConfigurationBytes), Actual: number(a.configurationBytesUsed + size),
		RecoveryOffset: number(tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)),
	})
	return false
}

func (a *analyzer) appendAACDiagnostics(tagIDValue uint64, items []aac.Diagnostic) {
	for _, item := range items {
		a.diagnostics.add(Diagnostic{
			Code: item.Code, Severity: Severity(item.Severity), TagID: tagID(tagIDValue),
			Offset: item.Offset, BitOffset: item.BitOffset, Message: item.Message,
			Expected: cloneNumber(item.Expected), Actual: cloneNumber(item.Actual), RecoveryOffset: cloneNumber(item.RecoveryOffset),
		})
	}
}

func (a *analyzer) appendAVCDiagnostics(tagIDValue uint64, items []avc.Diagnostic) {
	for _, item := range items {
		a.diagnostics.add(Diagnostic{
			Code: item.Code, Severity: Severity(item.Severity), TagID: tagID(tagIDValue),
			Offset: item.Offset, BitOffset: item.BitOffset, Message: item.Message,
			Expected: cloneNumber(item.Expected), Actual: cloneNumber(item.Actual), RecoveryOffset: cloneNumber(item.RecoveryOffset),
		})
	}
}

func (a *analyzer) hashSpan(span cursor.Span) (string, error) {
	if span.BitOffset != 0 || span.BitLength%8 != 0 {
		return "", fmt.Errorf("non-byte-aligned configuration span")
	}
	hash := sha256.New()
	offset := span.ByteOffset
	remaining := uint64(span.BitLength / 8)
	for remaining > 0 {
		if err := a.ctx.Err(); err != nil {
			return "", err
		}
		chunk := uint64(len(a.hashScratch))
		if chunk > remaining {
			chunk = remaining
		}
		if err := readAtLeast(a.source, a.hashScratch[:chunk], offset); err != nil {
			return "", err
		}
		_, _ = hash.Write(a.hashScratch[:chunk])
		offset += chunk
		remaining -= chunk
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (a *analyzer) finish() Result {
	a.result.Diagnostics = a.diagnostics.items
	if a.diagnostics.truncated {
		a.result.Complete = false
	}
	a.result.Audio.finish(a.audioBitrate)
	a.result.Video.finish(a.videoBitrate)
	a.result.VideoPTS.finish()
	if a.result.GOP.ClosedDurationCount > 0 {
		a.result.GOP.AverageDurationMS = a.gopDurationTotal / a.result.GOP.ClosedDurationCount
	}
	if a.result.Complete && a.result.Audio.UnclassifiedTagCount == 0 && a.result.Video.UnclassifiedTagCount == 0 &&
		a.result.Audio.Timing.Present && a.result.VideoPTS.Present &&
		a.result.Audio.Timing.RegressionCount == 0 && a.result.Video.Timing.RegressionCount == 0 {
		overlapStart := maxInt64(a.result.Audio.Timing.MinMS, a.result.VideoPTS.MinMS)
		overlapEnd := minInt64(a.result.Audio.Timing.MaxMS, a.result.VideoPTS.MaxMS)
		a.result.AVSync = AVSync{
			Present: true, Basis: avSyncBasis,
			StartExtremaSkewMS: a.result.VideoPTS.MinMS - a.result.Audio.Timing.MinMS,
			EndExtremaSkewMS:   a.result.VideoPTS.MaxMS - a.result.Audio.Timing.MaxMS,
			RangesOverlap:      overlapStart <= overlapEnd,
		}
		if overlapStart <= overlapEnd {
			a.result.AVSync.OverlapMS = uint64(overlapEnd - overlapStart)
		}
	}
	return a.result
}

func (t *Track) finish(bitrate bitrateAccumulator) {
	t.Timing.finish()
	if bitrate.coveredIntervalMS > 0 {
		t.Bitrate = Bitrate{
			Present: true, Basis: bitrateBasis, Bytes: bitrate.committedBytes,
			CoveredIntervalMS:    bitrate.coveredIntervalMS,
			AverageBitsPerSecond: bitrate.committedBytes * 8_000 / bitrate.coveredIntervalMS,
		}
	}
}

// bitrateAccumulator assigns every byte sharing one DTS to the interval that
// ends at the next strictly greater DTS in the same segment. A regression
// starts a new segment; the final timestamp group of each segment is not
// covered because its duration is unknown.
type bitrateAccumulator struct {
	present           bool
	previousDTS       int64
	pendingBytes      uint64
	committedBytes    uint64
	coveredIntervalMS uint64
}

func (b *bitrateAccumulator) add(dts int64, size uint64) {
	if !b.present {
		b.present = true
		b.previousDTS = dts
		b.pendingBytes = size
		return
	}
	if dts == b.previousDTS {
		b.pendingBytes += size
		return
	}
	if dts > b.previousDTS {
		b.committedBytes += b.pendingBytes
		b.coveredIntervalMS += uint64(dts - b.previousDTS)
	}
	b.previousDTS = dts
	b.pendingBytes = size
}

func (t *Timeline) add(value int64) {
	if !t.Present {
		t.Present = true
		t.SampleCount = 1
		t.FirstMS, t.LastMS, t.MinMS, t.MaxMS = value, value, value, value
		return
	}
	delta := value - t.LastMS
	if value < t.LastMS {
		t.RegressionCount++
	} else if value == t.LastMS {
		t.DuplicateCount++
	} else {
		forward := uint64(delta)
		t.ForwardDeltaTotalMS += forward
		if forward > t.LargestForwardGapMS {
			t.LargestForwardGapMS = forward
		}
	}
	if value < t.MinMS {
		t.MinMS = value
	}
	if value > t.MaxMS {
		t.MaxMS = value
	}
	t.LastMS = value
	t.SampleCount++
}

func (t *Timeline) finish() {
	if t.Present {
		t.RangeMS = uint64(t.MaxMS - t.MinMS)
	}
}

func readAtLeast(source io.ReaderAt, destination []byte, offset uint64) error {
	if offset > math.MaxInt64 || uint64(len(destination)) > math.MaxInt64-offset {
		return fmt.Errorf("source range exceeds ReaderAt address space")
	}
	n, err := source.ReadAt(destination, int64(offset))
	if n == len(destination) {
		return nil
	}
	if err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func signedInt24(value []byte) int32 {
	raw := uint32(value[0])<<16 | uint32(value[1])<<8 | uint32(value[2])
	if raw&0x0080_0000 != 0 {
		raw |= 0xff00_0000
	}
	return int32(raw)
}

func minCapacity(tags int, limit uint32) int {
	if tags < int(limit) {
		return tags
	}
	return int(limit)
}

func number(value uint64) *uint64 { return &value }
func tagID(value uint64) *uint64  { return &value }

func cloneNumber(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

func channelCount(configuration uint8) (uint8, bool) {
	switch configuration {
	case 1, 2, 3, 4, 5, 6:
		return configuration, true
	case 7:
		return 8, true
	default:
		return 0, false
	}
}

func minInt64(left, right int64) int64 {
	if left < right {
		return left
	}
	return right
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

type diagnosticCollector struct {
	items     []Diagnostic
	limit     uint32
	truncated bool
}

func (c *diagnosticCollector) add(item Diagnostic) {
	if uint32(len(c.items)) < c.limit {
		c.items = append(c.items, item)
		return
	}
	if c.limit == 0 {
		return
	}
	if c.truncated {
		if item.Severity == SeverityError {
			c.retainFirstActionableError(item)
		}
		return
	}
	c.truncated = true
	marker := Diagnostic{
		Code: CodeDiagnosticLimitReached, Severity: SeverityError,
		Offset: item.Offset, BitOffset: item.BitOffset,
		Message: "diagnostic limit reached; additional diagnostics were omitted",
	}
	markerIndex := len(c.items) - 1
	var fatal Diagnostic
	haveFatal := false
	for _, existing := range c.items {
		if existing.Severity == SeverityError {
			fatal, haveFatal = existing, true
			break
		}
	}
	if !haveFatal && item.Severity == SeverityError {
		fatal, haveFatal = item, true
	}
	if haveFatal {
		fatalRetained := false
		for _, existing := range c.items[:markerIndex] {
			if existing.Severity == SeverityError {
				fatalRetained = true
				break
			}
		}
		if !fatalRetained && markerIndex > 0 {
			c.items[markerIndex-1] = fatal
		}
		marker.TagID = cloneNumber(fatal.TagID)
		marker.Offset = fatal.Offset
		marker.BitOffset = fatal.BitOffset
		marker.RecoveryOffset = cloneNumber(fatal.RecoveryOffset)
	}
	c.items[markerIndex] = marker
}

func (c *diagnosticCollector) retainFirstActionableError(item Diagnostic) {
	markerIndex := len(c.items) - 1
	for _, existing := range c.items[:markerIndex] {
		if existing.Severity == SeverityError {
			return
		}
	}
	if markerIndex == 0 {
		return
	}
	c.items[markerIndex-1] = item
	marker := &c.items[markerIndex]
	marker.TagID = cloneNumber(item.TagID)
	marker.Offset = item.Offset
	marker.BitOffset = item.BitOffset
	marker.RecoveryOffset = cloneNumber(item.RecoveryOffset)
}
