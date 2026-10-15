// Package mp3search finds bounded MP3 frame-header changes and unresolved audio
// boundaries. It neither searches for sync after damage nor reads frame bodies.
package mp3search

import (
	"context"
	"errors"
	"io"
	"math"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
)

const (
	MaxScanTags   uint32 = 256
	MaxScanFrames uint32 = 1024
	MaxResults           = 64
	MaxBytes      uint64 = 64 << 10
)

var ErrInvalidInput = errors.New("mp3search: invalid input")
var errBudget = errors.New("mp3search: byte limit")

type Lookup func(uint64) (flv.TagRecord, error)

// Position uses the original zero-based Tag and Tag-relative frame indices.
// At a Tag boundary FrameIndex is zero; it does not assert an audio frame exists.
type Position struct {
	TagID      uint64
	FrameIndex uint32
}

type Parameters struct {
	Version             mp3.Version
	SamplingFrequencyHz uint32
	ChannelMode         mp3.ChannelMode
	Channels            uint8
	SamplesPerFrame     uint16
}

type Row struct {
	TagID                                                                         uint64
	FrameIndex                                                                    uint32
	DTS                                                                           int64
	Span                                                                          cursor.Span
	Status                                                                        string
	ParametersPresent                                                             bool
	Parameters                                                                    Parameters
	BitratePresent                                                                bool
	BitrateKbps                                                                   uint16
	ComparedToPrevious, ParametersChangedFromPrevious, BitrateChangedFromPrevious bool
	PreviousParameters                                                            Parameters
	PreviousBitrateKbps                                                           uint16
}

// State is an immutable value owned by the caller's source session. The zero
// value starts at Tag zero. Copy it to retry a batch; only commit NextState after
// a successful call and the caller's source-identity/cancellation checks.
// Unexported fields prevent client-supplied offsets from becoming seek evidence.
type State struct {
	position     Position
	active       bool
	tag          flv.TagRecord
	offset       uint64
	previous     Parameters
	bitrate      uint16
	dts          int64
	havePrevious bool
}

func Initial() State               { return State{} }
func (s State) Position() Position { return s.position }

type Page struct {
	Start, Next                Position
	NextState                  State
	Total                      uint64
	ScannedTags, ScannedFrames uint32
	BytesRead                  uint64
	Exhausted, ScanComplete    bool
	Filter                     string
	Rows                       []Row
}

func ValidFilter(filter string) bool {
	return filter == "parameters" || filter == "bitrate" || filter == "stops" || filter == "all"
}

// Search advances through validated boundaries. Normal frames are not retained;
// all means parameter/bitrate changes or stops, not all observed frames. A page
// budget leaves a continuable position, not a media-error row. Each resumed Tag
// counts once in that batch's ScannedTags. ScannedFrames counts attempted frame
// headers, including short suffixes. Cancellation and I/O errors return no page.
func Search(ctx context.Context, source io.ReaderAt, lookup Lookup, state State, total uint64, scanComplete bool, filter string) (*Page, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if source == nil || lookup == nil || total > flv.HardMaxTags || state.position.TagID > total || !ValidFilter(filter) || state.active && state.position.TagID == total {
		return nil, ErrInvalidInput
	}
	r := reader{ctx: ctx, source: source}
	p := &Page{Start: state.Position(), Total: total, ScanComplete: scanComplete, Filter: filter, Rows: make([]Row, 0, MaxResults)}
scan:
	for state.position.TagID < total && p.ScannedTags < MaxScanTags && p.ScannedFrames < MaxScanFrames && len(p.Rows) < MaxResults {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		tag, err := lookup(state.position.TagID)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if tag.ID != state.position.TagID || tag.DataSize > 0xffffff || tag.AvailableDataSize > tag.DataSize || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.AvailableDataSize) || state.active && state.tag != tag {
			return nil, ErrInvalidInput
		}
		p.ScannedTags++
		if !state.active {
			if tag.Type() != flv.TagTypeAudio {
				state.advanceTag()
				continue
			}
			row := Row{TagID: tag.ID, DTS: tag.DTS, Span: tag.PayloadSpan()}
			switch {
			case tag.Filter():
				row.Status = "filteredAudio"
			case !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize:
				row.Status = "truncatedAudio"
			case tag.DataSize == 0:
				row.Status = "missingAudioHeader"
			default:
				var audio [1]byte
				if _, err := r.ReadAt(audio[:], int64(row.Span.ByteOffset)); err != nil {
					if errors.Is(err, errBudget) {
						break scan
					}
					return nil, err
				}
				format := audio[0] >> 4
				if format != flv.SoundFormatMP3 {
					state.havePrevious = false
					if format != flv.SoundFormatAAC {
						row.Status = "unsupportedAudio"
						row.Span.BitLength = 4
					}
				} else {
					if state.havePrevious && tag.DTS < state.dts {
						state.havePrevious = false
					}
					state.active, state.tag, state.offset = true, tag, row.Span.ByteOffset+1
				}
			}
			if row.Status != "" {
				state.havePrevious = false
				p.retain(row)
			}
			if !state.active {
				state.advanceTag()
				continue
			}
		}
		end := tag.PayloadSpan().ByteOffset + uint64(tag.DataSize)
		for state.active && p.ScannedFrames < MaxScanFrames && len(p.Rows) < MaxResults {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			remaining := end - state.offset
			row := Row{TagID: tag.ID, FrameIndex: state.position.FrameIndex, DTS: tag.DTS, Span: cursor.Span{ByteOffset: state.offset, BitLength: uint32(remaining * 8)}}
			parsed, err := mp3.ParseFrameHeader(&r, cursor.Span{ByteOffset: state.offset, BitLength: uint32(min(remaining, 4) * 8)}, mp3.Options{InspectFrames: true})
			if err != nil {
				if errors.Is(err, errBudget) {
					break scan
				}
				return nil, err
			}
			p.ScannedFrames++
			h := parsed.Header
			switch {
			case remaining < 4:
				row.Status = "truncatedHeader"
			case !parsed.Complete:
				row.Status = "malformedHeader"
				if h.SyncWord == 0x7ff && (h.Layer == mp3.LayerI || h.Layer == mp3.LayerII) {
					row.Status = "unsupportedLayer"
				}
			default:
				row.ParametersPresent, row.BitratePresent, row.BitrateKbps = true, h.BitratePresent, h.BitrateKbps
				row.Parameters = Parameters{Version: h.Version, SamplingFrequencyHz: h.SamplingFrequencyHz, ChannelMode: h.ChannelMode, Channels: h.Channels, SamplesPerFrame: h.SamplesPerFrame}
				switch {
				case !h.FrameLengthPresent:
					row.Status = "freeFormat"
				case uint64(h.FrameLengthBytes) > remaining:
					row.Status = "truncatedFrame"
				default:
					row.Status, row.Span.BitLength = "complete", h.FrameLengthBytes*8
					if state.havePrevious {
						row.ComparedToPrevious = true
						row.PreviousParameters, row.PreviousBitrateKbps = state.previous, state.bitrate
						row.ParametersChangedFromPrevious = row.Parameters != state.previous
						row.BitrateChangedFromPrevious = row.BitrateKbps != state.bitrate
					}
					state.previous, state.bitrate, state.dts, state.havePrevious = row.Parameters, row.BitrateKbps, tag.DTS, true
				}
			}
			p.retain(row)
			if row.Status != "complete" {
				state.havePrevious = false
				state.advanceTag()
			} else {
				state.offset += uint64(h.FrameLengthBytes)
				state.position.FrameIndex++
				if state.offset == end {
					state.advanceTag()
				}
			}
		}
		if state.active {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.NextState, p.Next, p.BytesRead = state, state.Position(), r.bytes
	p.Exhausted = state.position.TagID == total
	return p, nil
}

func (s *State) advanceTag() {
	s.position.TagID++
	s.position.FrameIndex, s.offset, s.active, s.tag = 0, 0, false, flv.TagRecord{}
}

func (p *Page) retain(row Row) {
	change := row.ParametersChangedFromPrevious || row.BitrateChangedFromPrevious
	if p.Filter == "all" && (change || row.Status != "complete") || p.Filter == "parameters" && row.ParametersChangedFromPrevious || p.Filter == "bitrate" && row.BitrateChangedFromPrevious || p.Filter == "stops" && row.Status != "complete" {
		p.Rows = append(p.Rows, row)
	}
}

type reader struct {
	ctx    context.Context
	source io.ReaderAt
	bytes  uint64
}

func (r *reader) ReadAt(b []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if offset < 0 || uint64(offset) > math.MaxInt64-uint64(len(b)) {
		return 0, ErrInvalidInput
	}
	if uint64(len(b)) > MaxBytes-r.bytes {
		return 0, errBudget
	}
	n, err := r.source.ReadAt(b, offset)
	if n < 0 || n > len(b) {
		return 0, io.ErrUnexpectedEOF
	}
	r.bytes += uint64(n)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, err
	}
	if n != len(b) {
		return n, io.ErrUnexpectedEOF
	}
	return n, nil
}
