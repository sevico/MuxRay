package desktop

import (
	"strconv"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const APIVersion uint8 = 1

// OpenRequest is bounded before any goroutine or source is created. Zero scan
// budgets select the usual FLV defaults. Paths are never echoed in responses.
type OpenRequest struct {
	Path           string `json:"path"`
	MaxTags        uint32 `json:"maxTags"`
	MaxDiagnostics uint32 `json:"maxDiagnostics"`
}

// PageRequest requires a canonical decimal start and a count in 1..4,096.
type PageRequest struct {
	SessionID string `json:"sessionId"`
	Start     string `json:"start"`
	Count     uint32 `json:"count"`
}

// Fault is a bounded code/message pair, not a serialized Go or OS error.
type Fault struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type SessionResponse struct {
	APIVersion uint8            `json:"apiVersion"`
	Session    *SessionSnapshot `json:"session"`
	Error      *Fault           `json:"error"`
}

type TagPageResponse struct {
	APIVersion uint8    `json:"apiVersion"`
	Page       *TagPage `json:"page"`
	Error      *Fault   `json:"error"`
}

type DiagnosticPageResponse struct {
	APIVersion uint8           `json:"apiVersion"`
	Page       *DiagnosticPage `json:"page"`
	Error      *Fault          `json:"error"`
}

// SessionSnapshot distinguishes a request error from a task failure. Progress
// is nil before a session exists; Scan is nil until an actual scan terminates.
type SessionSnapshot struct {
	SessionID string       `json:"sessionId"`
	State     string       `json:"state"`
	Terminal  bool         `json:"terminal"`
	Closed    bool         `json:"closed"`
	Progress  *Progress    `json:"progress"`
	Scan      *ScanOutcome `json:"scan"`
	Failure   *Fault       `json:"failure"`
}

type Progress struct {
	SourceSize      string `json:"sourceSize"`
	BytesScanned    string `json:"bytesScanned"`
	TagCount        string `json:"tagCount"`
	DiagnosticCount uint32 `json:"diagnosticCount"`
}

type ScanOutcome struct {
	Complete  bool `json:"complete"`
	HasErrors bool `json:"hasErrors"`
}

type PageBounds struct {
	Start string `json:"start"`
	Limit uint32 `json:"limit"`
	Total string `json:"total"`
	Next  string `json:"next"`
}

type TagPage struct {
	SessionID string     `json:"sessionId"`
	Bounds    PageBounds `json:"bounds"`
	Tags      []Tag      `json:"tags"`
}

type DiagnosticPage struct {
	SessionID   string       `json:"sessionId"`
	Bounds      PageBounds   `json:"bounds"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

// Span preserves the core bit coordinate system with a lossless byte offset.
type Span struct {
	ByteOffset string `json:"byteOffset"`
	BitOffset  uint8  `json:"bitOffset"`
	BitLength  uint32 `json:"bitLength"`
}

type Tag struct {
	ID                string          `json:"id"`
	TypeCode          uint8           `json:"typeCode"`
	TypeName          string          `json:"typeName"`
	Offset            string          `json:"offset"`
	Span              Span            `json:"span"`
	HeaderSpan        Span            `json:"headerSpan"`
	PayloadSpan       Span            `json:"payloadSpan"`
	RawTypeFlags      uint8           `json:"rawTypeFlags"`
	Filter            bool            `json:"filter"`
	DataSize          uint32          `json:"dataSize"`
	AvailableDataSize uint32          `json:"availableDataSize"`
	DTS               string          `json:"dts"`
	StreamID          uint32          `json:"streamId"`
	PayloadComplete   bool            `json:"payloadComplete"`
	PreviousTagSize   PreviousTagSize `json:"previousTagSize"`
}

type PreviousTagSize struct {
	Present  bool   `json:"present"`
	Span     *Span  `json:"span"`
	Value    uint32 `json:"value"`
	Expected uint32 `json:"expected"`
	Valid    bool   `json:"valid"`
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

func decimal(value uint64) string { return strconv.FormatUint(value, 10) }

func mapBounds(start uint64, limit uint32, total, next uint64) PageBounds {
	return PageBounds{Start: decimal(start), Limit: limit, Total: decimal(total), Next: decimal(next)}
}

func mapSpan(span cursor.Span) Span {
	return Span{ByteOffset: decimal(span.ByteOffset), BitOffset: span.BitOffset, BitLength: span.BitLength}
}

func mapTag(record flv.TagRecord) Tag {
	tag := Tag{
		ID: decimal(record.ID), TypeCode: uint8(record.Type()), TypeName: record.Type().String(),
		Offset: decimal(record.Offset), Span: mapSpan(record.Span()),
		HeaderSpan: mapSpan(record.HeaderSpan()), PayloadSpan: mapSpan(record.PayloadSpan()),
		RawTypeFlags: record.RawTypeFlags, Filter: record.Filter(), DataSize: record.DataSize,
		AvailableDataSize: record.AvailableDataSize, DTS: strconv.FormatInt(record.DTS, 10),
		StreamID: record.StreamID, PayloadComplete: record.PayloadComplete(),
		PreviousTagSize: PreviousTagSize{
			Present: record.PreviousTagSizePresent(), Value: record.PreviousTagSize,
			Expected: record.ExpectedPreviousTagSize(), Valid: record.PreviousTagSizeValid(),
		},
	}
	if sourceSpan, present := record.PreviousTagSizeSpan(); present {
		span := mapSpan(sourceSpan)
		tag.PreviousTagSize.Span = &span
	}
	return tag
}

func mapDiagnostic(diagnostic flv.Diagnostic) Diagnostic {
	return Diagnostic{
		Code: diagnostic.Code, Severity: string(diagnostic.Severity), Offset: decimal(diagnostic.Offset),
		BitOffset: diagnostic.BitOffset, Message: diagnostic.Message,
		Expected: optionalDecimal(diagnostic.Expected), Actual: optionalDecimal(diagnostic.Actual),
	}
}

func optionalDecimal(value *uint64) *string {
	if value == nil {
		return nil
	}
	text := decimal(*value)
	return &text
}
