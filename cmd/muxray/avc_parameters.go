package main

import (
	"context"
	"io"

	"github.com/sevico/muxray/internal/avccontext"
	"github.com/sevico/muxray/internal/flv"
)

// CLI v3 has no interactive continuation. Walk up to 128 bounded segments,
// enough for the scanner's one-million-Tag limit when the byte budget does not
// stop a segment earlier. Ctrl+C remains effective between all source reads.
// Publish only the last result's context, with cumulative work counters in the
// existing v3 contract; a short file is identical to its original single pass.
func resolveAVCParameterSegments(ctx context.Context, source io.ReaderAt, tag flv.TagRecord, lookup avccontext.Lookup, framing avccontext.Resolution, options flv.AVCVideoOptions) (avccontext.ParameterResolution, error) {
	var checkpoint *avccontext.ParameterCheckpoint
	var total avccontext.ParameterResolution
	for segment := 0; segment < 128; segment++ {
		p, next, err := avccontext.ResolveParameterSegment(ctx, source, tag, lookup, framing, options, avccontext.ParameterLimits{}, checkpoint)
		if err != nil {
			return total, err
		}
		p.ScannedTags += total.ScannedTags
		p.VideoTags += total.VideoTags
		p.Configurations += total.Configurations
		p.NALPackets += total.NALPackets
		p.BytesRead += total.BytesRead
		p.BarrierCount += total.BarrierCount
		total = p
		if p.State != avccontext.StateBudgetExhausted || !p.CanResume || next == nil {
			break
		}
		checkpoint = next
	}
	return total, nil
}
