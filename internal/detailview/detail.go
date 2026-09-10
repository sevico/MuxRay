// Package detailview projects bounded syntax detail for the desktop protocol.
// It consumes the core parsers directly and never owns or retains a source.
package detailview

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/amf0"
	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const (
	MaxFields         = 512
	MaxPreviewBytes   = 256
	MaxDiagnostics    = 64
	MaxJSONBytes      = 256 << 10
	MaxAMFNodes       = 4096
	MaxAMFStringBytes = 64 << 10
)

var (
	ErrUnsupported  = errors.New("detailview: unsupported tag detail")
	ErrFiltered     = errors.New("detailview: filtered payload")
	ErrTruncated    = errors.New("detailview: incomplete container payload")
	ErrInvalidInput = errors.New("detailview: invalid source or payload span")
)

// Complete describes syntax parsing, while Truncated also includes omitted
// projection fields or previews. Complete and Truncated can both be true.
type Detail struct {
	Kind        string       `json:"kind"`
	Complete    bool         `json:"complete"`
	Truncated   bool         `json:"truncated"`
	Fields      []Field      `json:"fields"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type Field struct {
	ID       string `json:"id"`
	ParentID string `json:"parentId"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Value    string `json:"value"`
	Span     *Span  `json:"span"`
}

type Span struct {
	ByteOffset string `json:"byteOffset"`
	BitOffset  uint8  `json:"bitOffset"`
	BitLength  uint32 `json:"bitLength"`
}

type Diagnostic struct {
	Code      string  `json:"code"`
	Severity  string  `json:"severity"`
	Offset    string  `json:"offset"`
	BitOffset uint8   `json:"bitOffset"`
	Message   string  `json:"message"`
	Expected  *string `json:"expected"`
	Actual    *string `json:"actual"`
}

// Parse reads only the indexed payload. Cancellation is checked between reads
// and projection steps; a currently blocked ReaderAt cannot be interrupted here.
// Malformed syntax returns partial detail, while source failures return errors.
func Parse(ctx context.Context, source io.ReaderAt, tag flv.TagRecord) (*Detail, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tag.Type() != flv.TagTypeScript && tag.Type() != flv.TagTypeAudio {
		return nil, ErrUnsupported
	}
	if tag.Filter() {
		return nil, ErrFiltered
	}
	if !tag.PayloadComplete() || tag.AvailableDataSize != tag.DataSize {
		return nil, ErrTruncated
	}
	if source == nil || tag.DataSize > 0xffffff || tag.Offset > math.MaxInt64-flv.TagHeaderSize-uint64(tag.DataSize) {
		return nil, ErrInvalidInput
	}
	// An empty audio tag has no SoundFormat discriminator. Do not label it AAC.
	if tag.Type() == flv.TagTypeAudio && tag.DataSize == 0 {
		return nil, ErrUnsupported
	}
	start := tag.Offset + flv.TagHeaderSize
	reader := contextReader{ctx: ctx, source: source, start: start, end: start + uint64(tag.DataSize)}
	b := builder{detail: Detail{Fields: []Field{}, Diagnostics: []Diagnostic{}}}
	if tag.Type() == flv.TagTypeScript {
		parsed, err := flv.ParseScriptData(reader, tag, amf0.Options{MaxDepth: 32, MaxNodes: MaxAMFNodes, MaxContainerEntries: MaxAMFNodes, MaxStringBytes: MaxAMFStringBytes, MaxTotalStringBytes: MaxAMFStringBytes, MaxDiagnostics: MaxDiagnostics})
		if err != nil {
			return nil, err
		}
		b.detail.Kind, b.detail.Complete = string(flv.DetailKindAMF0ScriptData), parsed.Complete
		for _, d := range parsed.Diagnostics {
			b.diagnostic(d.Code, string(d.Severity), d.Offset, d.BitOffset, d.Message, d.Expected, d.Actual)
		}
		b.add("", "tagId", "uint64", decimal(tag.ID), nil)
		if err := b.amf(ctx, parsed); err != nil {
			return nil, err
		}
	} else {
		parsed, err := flv.ParseAACAudio(reader, tag, aac.Options{MaxDiagnostics: MaxDiagnostics})
		if errors.Is(err, flv.ErrAACAudioWrongSoundFormat) {
			return nil, ErrUnsupported
		}
		if err != nil {
			return nil, err
		}
		b.detail.Kind, b.detail.Complete = string(flv.DetailKindAACAudio), parsed.Complete
		for _, d := range parsed.Diagnostics {
			b.diagnostic(d.Code, string(d.Severity), d.Offset, d.BitOffset, d.Message, d.Expected, d.Actual)
		}
		b.add("", "tagId", "uint64", decimal(tag.ID), nil)
		b.aac(parsed)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &b.detail, nil
}

type contextReader struct {
	ctx        context.Context
	source     io.ReaderAt
	start, end uint64
}

func (r contextReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if off < 0 || uint64(off) < r.start || uint64(off) > r.end || uint64(len(p)) > r.end-uint64(off) {
		return 0, ErrInvalidInput
	}
	n, err := r.source.ReadAt(p, off)
	if canceled := r.ctx.Err(); canceled != nil {
		return n, canceled
	}
	return n, err
}

type builder struct {
	detail     Detail
	bytes      int
	fieldsFull bool
}

func decimal(n uint64) string { return strconv.FormatUint(n, 10) }
func optional(n *uint64) *string {
	if n == nil {
		return nil
	}
	s := decimal(*n)
	return &s
}
func span(s cursor.Span) *Span {
	return &Span{ByteOffset: decimal(s.ByteOffset), BitOffset: s.BitOffset, BitLength: s.BitLength}
}

// preview repairs invalid UTF-8 and truncates at a rune boundary. Both changes
// are explicit via Detail.Truncated; the visible ellipsis identifies previews.
func (b *builder) preview(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "�")
		b.detail.Truncated = true
	}
	if len(s) <= MaxPreviewBytes {
		return s
	}
	end := MaxPreviewBytes - len("…")
	for !utf8.RuneStart(s[end]) {
		end--
	}
	b.detail.Truncated = true
	return s[:end] + "…"
}

// The standard JSON encoder can expand one input byte to six bytes. Count its
// exact output for each member, including separators, rather than assuming a
// UTF-8 limit is also a wire limit. 512 bytes reserve the fixed envelope (under
// 128 bytes) and future boolean changes; fields/diagnostics are the only arrays.
func (b *builder) fits(value any) bool {
	encoded, _ := json.Marshal(value) // All DTO values are finite integers/strings.
	if b.bytes+len(encoded)+1 > MaxJSONBytes-512 {
		b.detail.Truncated = true
		return false
	}
	b.bytes += len(encoded) + 1
	return true
}

func (b *builder) add(parent, name, kind, value string, s *Span) string {
	if b.fieldsFull || len(b.detail.Fields) >= MaxFields {
		b.fieldsFull = true
		b.detail.Truncated = true
		return ""
	}
	f := Field{ID: decimal(uint64(len(b.detail.Fields))), ParentID: parent, Name: b.preview(name), Type: b.preview(kind), Value: b.preview(value), Span: s}
	if !b.fits(f) {
		b.fieldsFull = true
		return ""
	}
	b.detail.Fields = append(b.detail.Fields, f)
	return f.ID
}

func (b *builder) encoded(parent, name, kind, value string, s cursor.Span) {
	if s.BitLength != 0 {
		b.add(parent, name, kind, value, span(s))
	}
}

func (b *builder) diagnostic(code, severity string, offset uint64, bit uint8, message string, expected, actual *uint64) {
	if strings.Contains(code, "TRUNCATED") || strings.Contains(code, "LIMIT") || strings.Contains(code, "BUDGET") {
		b.detail.Truncated = true
	}
	d := Diagnostic{Code: b.preview(code), Severity: b.preview(severity), Offset: decimal(offset), BitOffset: bit, Message: b.preview(message), Expected: optional(expected), Actual: optional(actual)}
	if len(b.detail.Diagnostics) >= MaxDiagnostics || !b.fits(d) {
		b.detail.Truncated = true
		return
	}
	b.detail.Diagnostics = append(b.detail.Diagnostics, d)
}
