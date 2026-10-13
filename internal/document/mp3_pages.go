package document

import (
	"context"
	"errors"
	"math"

	"github.com/sevico/muxray/internal/detailview"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
	"github.com/sevico/muxray/internal/mp3context"
)

const MP3PageReplayBudget uint32 = 16
const MP3PageCheckpointLimit = 64

type mp3PageCheckpoint struct {
	tag    uint64
	cursor *mp3.FrameCursor
}
type mp3PageCheckpoints []mp3PageCheckpoint

func (c mp3PageCheckpoints) lookup(tag uint64, start uint32) *mp3.FrameCursor {
	var result *mp3.FrameCursor
	for _, entry := range c {
		if entry.tag == tag && entry.cursor.NextFrameIndex() <= start && (result == nil || entry.cursor.NextFrameIndex() > result.NextFrameIndex()) {
			result = entry.cursor
		}
	}
	return result
}

func (c *mp3PageCheckpoints) commit(entries []mp3PageCheckpoint) {
	for _, entry := range entries {
		for i, old := range *c {
			if old.tag == entry.tag && old.cursor.NextFrameIndex() == entry.cursor.NextFrameIndex() {
				*c = append((*c)[:i], (*c)[i+1:]...)
				break
			}
		}
		*c = append(*c, entry)
		if len(*c) > MP3PageCheckpointLimit {
			*c = (*c)[len(*c)-MP3PageCheckpointLimit:]
		}
	}
}

func validMP3PageSelection(frame, page *uint32) bool {
	if page == nil {
		return frame == nil || *frame < mp3.DefaultMaxFrames
	}
	return *page%mp3.FramePageSize == 0 && *page <= math.MaxUint32-(mp3.FramePageSize-1) && (frame == nil || *frame >= *page && uint64(*frame) < uint64(*page)+uint64(mp3.FramePageSize))
}

func (d *Document) readMP3Page(ctx context.Context, tag flv.TagRecord, frame *uint32, requested uint32, dependency bool) (*detailview.Detail, []mp3PageCheckpoint, error) {
	if tag.Type() != flv.TagTypeAudio || tag.DataSize > 0xffffff || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.DataSize) {
		return nil, nil, detailview.ErrInvalidInput
	}
	if tag.Filter() {
		return nil, nil, detailview.ErrFiltered
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		return nil, nil, detailview.ErrTruncated
	}
	d.mu.Lock()
	previous := d.mp3Pages.lookup(tag.ID, requested)
	preview := append(mp3PageCheckpoints(nil), d.mp3Pages...)
	d.mu.Unlock()
	staged := []mp3PageCheckpoint{}
	info := detailview.MP3PageInfo{State: "preparing", RequestedStart: requested, RequestedFrame: frame, ReplayPageBudget: MP3PageReplayBudget}
	for info.ReplayPages < MP3PageReplayBudget {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		parsed, page, err := flv.ParseMP3AudioPage(ctx, d.source, tag, previous)
		if errors.Is(err, flv.ErrMP3AudioWrongSoundFormat) {
			err = detailview.ErrUnsupported
		}
		if err != nil {
			return nil, nil, err
		}
		info.ReplayPages++
		info.StopReason = page.Sequence.StopReason
		info.Diagnostics = page.Sequence.Diagnostics
		info.StartIndex, info.Frames = page.StartIndex, uint32(len(page.Sequence.Frames))
		info.BytesCovered, info.BytesRead = page.Sequence.BytesCovered, page.Sequence.BytesRead
		info.NextFrameIndex = page.StartIndex + page.Sequence.CompleteFrames
		if page.Next != nil {
			entry := mp3PageCheckpoint{tag.ID, page.Next}
			staged = append(staged, entry)
			preview.commit([]mp3PageCheckpoint{entry})
		}
		info.CheckpointCount = uint32(len(preview))
		if page.StartIndex == requested {
			info.State = "ready"
			if page.Next != nil {
				next := page.Next.NextFrameIndex()
				info.NextPageStart = &next
			}
			if page.Sequence.StopReason != "end" && page.Sequence.StopReason != "page_limit" && page.Sequence.StopReason != "diagnostics" {
				info.State = "stopped"
			}
			if frame != nil {
				found := false
				for _, f := range page.Sequence.Frames {
					if f.Index == *frame && f.BoundaryComplete && f.SideInfo != nil {
						found = true
					}
				}
				if !found {
					info.State = "stopped"
					detail, e := detailview.ProjectMP3Page(tag.ID, nil, nil, info, nil)
					return detail, staged, e
				}
			}
			var resolved *mp3context.Resolution
			if dependency {
				r, e := mp3context.ResolvePage(ctx, d.source, tag, *frame, d.scan.Tag, mp3context.Limits{}, parsed, page)
				if e != nil {
					return nil, nil, e
				}
				resolved = &r
			}
			detail, e := detailview.ProjectMP3Page(tag.ID, &parsed, frame, info, resolved)
			return detail, staged, e
		}
		if page.Next == nil {
			info.State = "stopped"
			detail, e := detailview.ProjectMP3Page(tag.ID, nil, nil, info, nil)
			return detail, staged, e
		}
		previous = page.Next
	}
	detail, err := detailview.ProjectMP3Page(tag.ID, nil, nil, info, nil)
	return detail, staged, err
}
