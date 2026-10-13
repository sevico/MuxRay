package detailview

import (
	"strconv"
	"strings"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/mp3"
	"github.com/sevico/muxray/internal/mp3context"
)

// MP3PageInfo separates requested identity, actual page and bounded replay
// progress. Ready describes the requested page, never whole-Tag integrity.
type MP3PageInfo struct {
	Diagnostics                                    []mp3.Diagnostic
	StopReason                                     string
	State                                          string
	RequestedStart, StartIndex, NextFrameIndex     uint32
	NextPageStart                                  *uint32
	RequestedFrame                                 *uint32
	Frames                                         uint32
	BytesCovered, BytesRead                        uint64
	ReplayPages, ReplayPageBudget, CheckpointCount uint32
}

// ProjectMP3Page projects only the requested page. A stopped or preparing
// result can carry no syntax parse; its request identity is still retained.
func ProjectMP3Page(tagID uint64, parsed *flv.MP3Audio, selected *uint32, info MP3PageInfo, dependency *mp3context.Resolution) (*Detail, error) {
	b := builder{detail: Detail{Kind: "mp3Audio", Fields: []Field{}, Diagnostics: []Diagnostic{}}}
	b.add("", "tagId", "uint64", decimal(tagID), nil)
	b.mp3Page(info)
	for _, d := range info.Diagnostics {
		b.diagnostic(d.Code, string(d.Severity), d.Offset, d.BitOffset, d.Message, d.Expected, d.Actual)
	}
	if dependency != nil {
		b.mp3Dependency(*dependency)
	}
	if parsed != nil {
		if selected != nil && !selectableMP3Frame(parsed.Frames, *selected) {
			return nil, ErrInvalidInput
		}
		b.detail.Complete = parsed.Complete
		b.mp3Mode(*parsed, selected, true)
	}
	if info.State != "ready" || info.NextPageStart != nil {
		b.detail.Truncated = true
	}
	if dependency != nil && (strings.HasSuffix(dependency.Reason, "budget") || dependency.Reason == "source_limit") {
		b.detail.Truncated = true
	}
	return &b.detail, nil
}

func (b *builder) mp3Page(p MP3PageInfo) {
	id := b.add("", "MP3 frame page", "bounded frame page", p.State, nil)
	b.add(id, "pageState", "derived enum", p.State, nil)
	b.add(id, "pageStopReason", "derived enum", p.StopReason, nil)
	for _, field := range []struct {
		name  string
		value uint64
	}{
		{"requestedPageStart", uint64(p.RequestedStart)}, {"pageStartIndex", uint64(p.StartIndex)},
		{"pageSize", uint64(mp3.FramePageSize)}, {"nextFrameIndex", uint64(p.NextFrameIndex)}, {"pageFrames", uint64(p.Frames)},
		{"pageBytesCovered", p.BytesCovered}, {"pageBytesRead", p.BytesRead}, {"pageReadBudget", mp3.FramePageReadBudget},
		{"replayPages", uint64(p.ReplayPages)}, {"replayPageBudget", uint64(p.ReplayPageBudget)}, {"checkpointCount", uint64(p.CheckpointCount)},
	} {
		b.add(id, field.name, "derived count", decimal(field.value), nil)
	}
	next := "none"
	if p.NextPageStart != nil {
		next = decimal(uint64(*p.NextPageStart))
	}
	b.add(id, "nextPageStart", "derived index or none", next, nil)
	b.add(id, "hasNext", "derived boolean", strconv.FormatBool(p.NextPageStart != nil), nil)
	b.add(id, "canContinue", "derived boolean", strconv.FormatBool(p.State == "preparing"), nil)
	selected := "preview"
	if p.RequestedFrame != nil {
		selected = decimal(uint64(*p.RequestedFrame))
	}
	b.add(id, "requestedFrameIndex", "derived selection", selected, nil)
	b.add(id, "pageScope", "scope", "64 sequential frames per page; original Tag-relative indices. Checkpoints preserve physical main-data history, not decoded reservoir state. Ready describes this page; whole-Tag coverage and summary event retention are separate.", nil)
}

// AnnotateMP3Preview adds desktop navigation to an unchanged legacy preview.
// It does not extend its inspection budget or claim a cached continuation.
func AnnotateMP3Preview(detail *Detail, selected *uint32) {
	if detail == nil || detail.Kind != "mp3Audio" {
		return
	}
	p := MP3PageInfo{State: "stopped", RequestedFrame: selected, ReplayPageBudget: 16}
	root := ""
	for _, f := range detail.Fields {
		if f.Name == "Frame inspection" && f.Type == "bounded sequence" {
			root, p.State = f.ID, "ready"
			if after := strings.SplitN(f.Value, "stop=", 2); len(after) == 2 {
				p.StopReason = strings.SplitN(after[1], ";", 2)[0]
			}
			if p.StopReason == "frame_limit" {
				v := mp3.FramePageSize
				p.NextPageStart = &v
			}
			break
		}
	}
	for _, f := range detail.Fields {
		if root == "" || f.ParentID != root {
			continue
		}
		n, _ := strconv.ParseUint(f.Value, 10, 64)
		switch f.Name {
		case "checkedFrameCount":
			p.Frames = uint32(n)
			p.NextFrameIndex = uint32(n)
		case "bytesCovered":
			p.BytesCovered = n
		case "bytesRead":
			p.BytesRead = n
		}
	}
	b := builder{detail: Detail{Kind: detail.Kind, Complete: detail.Complete, Truncated: detail.Truncated, Fields: []Field{}, Diagnostics: []Diagnostic{}}}
	b.mp3Page(p)
	// Prefix new IDs while retaining every original ID/parent relationship.
	// Re-account the exact encoded bytes after changing the metadata IDs.
	b.bytes = 0
	for i := range b.detail.Fields {
		b.detail.Fields[i].ID = "page-" + b.detail.Fields[i].ID
		if b.detail.Fields[i].ParentID != "" {
			b.detail.Fields[i].ParentID = "page-" + b.detail.Fields[i].ParentID
		}
		b.fits(b.detail.Fields[i])
	}
	for _, diagnostic := range detail.Diagnostics {
		if len(b.detail.Diagnostics) >= MaxDiagnostics || !b.fits(diagnostic) {
			b.detail.Truncated = true
			break
		}
		b.detail.Diagnostics = append(b.detail.Diagnostics, diagnostic)
	}
	for _, f := range detail.Fields {
		if len(b.detail.Fields) >= MaxFields || !b.fits(f) {
			b.detail.Truncated = true
			break
		}
		b.detail.Fields = append(b.detail.Fields, f)
	}
	*detail = b.detail
}
