package avccontext

import (
	"github.com/sevico/muxray/internal/avc"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	MaxParameterCheckpoints = 8
	// Compact contexts contain only fixed-size objects: 32 SPS, 256 PPS,
	// bounded fixed-value PPS and SPS timing metadata and their original NAL spans. The generous
	// bound is checked against the reachable Go storage by tests.
	MaxParameterCheckpointBytes = 512 << 10
)

// ParameterCheckpoint is an immutable, fully replayed prefix of one source
// generation. It has no reader or payload and must stay scoped to that source.
// A caller commits it only after checking source identity and request currency.
type ParameterCheckpoint struct {
	sourceTagID   uint64
	lengthSpan    cursor.Span
	declaredWidth uint8
	options       avc.Options
	nextTagID     uint64
	sets          *avc.ParameterSetContext
	width         uint8
	invalidated   bool
}

// ParameterCheckpoints keeps at most eight bounded snapshots (at most 4 MiB).
// Its owner serializes access and clears it on source invalidation or closure.
type ParameterCheckpoints struct {
	entries [MaxParameterCheckpoints]*ParameterCheckpoint
	count   uint32
}

func (c *ParameterCheckpoints) Clear()        { *c = ParameterCheckpoints{} }
func (c *ParameterCheckpoints) Count() uint32 { return c.count }

// Lookup chooses the closest earlier prefix with the same framing origin and
// parser options. Looking up does not mutate cache state or its snapshots.
func (c *ParameterCheckpoints) Lookup(framing Resolution, selected uint64, options flv.AVCVideoOptions) *ParameterCheckpoint {
	normalized, err := parameterOptions(options.AVC)
	if err != nil || framing.State != StateFound {
		return nil
	}
	var best *ParameterCheckpoint
	for _, p := range c.entries[:c.count] {
		if p.matches(framing, selected, normalized) && (best == nil || p.nextTagID > best.nextTagID) {
			best = p
		}
	}
	return best
}

// Commit retains a completed request's snapshot, replacing a duplicate or
// evicting the oldest committed entry. Uncommitted work never touches the cache.
func (c *ParameterCheckpoints) Commit(p *ParameterCheckpoint) {
	if p == nil {
		return
	}
	for i, old := range c.entries[:c.count] {
		if old.sourceTagID == p.sourceTagID && old.lengthSpan == p.lengthSpan && old.declaredWidth == p.declaredWidth && old.options == p.options && old.nextTagID == p.nextTagID {
			copy(c.entries[i:], c.entries[i+1:c.count])
			c.count--
			break
		}
	}
	if c.count == MaxParameterCheckpoints {
		copy(c.entries[:], c.entries[1:])
		c.count--
	}
	c.entries[c.count] = p
	c.count++
}

func (p *ParameterCheckpoint) matches(framing Resolution, selected uint64, options avc.Options) bool {
	return p != nil && p.sourceTagID == framing.SourceTagID && p.lengthSpan == framing.SourceLengthSpan && p.declaredWidth == framing.NALLengthSize && p.options == options && p.nextTagID >= framing.SourceTagID && p.nextTagID <= selected
}

func parameterOptions(options avc.Options) (avc.Options, error) {
	options.ParsePPS, options.ParseSliceHeaders, options.ParseVUI = true, false, options.ParseSEITiming
	options.ParseSEI = options.ParseSEITiming
	options.ParameterSets = nil
	return options.Normalized()
}
