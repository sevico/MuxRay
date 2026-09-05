// Package report maps core scan results to stable public representations.
package report

import (
	"bytes"
	"encoding/json"
	"io"

	"github.com/sevico/muxray/internal/cursor"
	"github.com/sevico/muxray/internal/flv"
)

const SchemaVersion = 1

// Document is the versioned JSON contract emitted by the CLI.
type Document struct {
	SchemaVersion int          `json:"schemaVersion"`
	Source        Source       `json:"source"`
	Scan          Scan         `json:"scan"`
	Header        Header       `json:"header"`
	Tags          []Tag        `json:"tags"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

type Source struct {
	SizeBytes uint64 `json:"sizeBytes"`
}

type Scan struct {
	BytesScanned uint64 `json:"bytesScanned"`
	Complete     bool   `json:"complete"`
}

type Span struct {
	ByteOffset uint64 `json:"byteOffset"`
	BitOffset  uint8  `json:"bitOffset"`
	BitLength  uint32 `json:"bitLength"`
}

type Header struct {
	Present          bool   `json:"present"`
	Span             *Span  `json:"span,omitempty"`
	Version          uint8  `json:"version"`
	TypeFlags        uint8  `json:"typeFlags"`
	HasAudio         bool   `json:"hasAudio"`
	HasVideo         bool   `json:"hasVideo"`
	DataOffset       uint32 `json:"dataOffset"`
	ExtraDataSize    uint64 `json:"extraDataSize"`
	PreviousTagSize0 uint32 `json:"previousTagSize0"`
}

type Tag struct {
	ID                uint64          `json:"id"`
	TypeCode          uint8           `json:"typeCode"`
	TypeName          string          `json:"typeName"`
	Offset            uint64          `json:"offset"`
	Span              Span            `json:"span"`
	HeaderSpan        Span            `json:"headerSpan"`
	PayloadSpan       Span            `json:"payloadSpan"`
	RawTypeFlags      uint8           `json:"rawTypeFlags"`
	Filter            bool            `json:"filter"`
	DataSize          uint32          `json:"dataSize"`
	AvailableDataSize uint32          `json:"availableDataSize"`
	DTS               int64           `json:"dts"`
	StreamID          uint32          `json:"streamId"`
	PayloadComplete   bool            `json:"payloadComplete"`
	PreviousTagSize   PreviousTagSize `json:"previousTagSize"`
}

type PreviousTagSize struct {
	Present  bool   `json:"present"`
	Span     *Span  `json:"span,omitempty"`
	Value    uint32 `json:"value"`
	Expected uint32 `json:"expected"`
	Valid    bool   `json:"valid"`
}

type Diagnostic struct {
	Code      string       `json:"code"`
	Severity  flv.Severity `json:"severity"`
	Offset    uint64       `json:"offset"`
	BitOffset uint8        `json:"bitOffset"`
	Message   string       `json:"message"`
	Expected  *uint64      `json:"expected,omitempty"`
	Actual    *uint64      `json:"actual,omitempty"`
}

// New materializes a deterministic v1 report without paths, timestamps, or
// random IDs. Use WriteScanJSON for large scans to avoid duplicating tag data.
func New(result flv.ScanResult) Document {
	index := result.Index()
	document := Document{
		SchemaVersion: SchemaVersion,
		Source:        Source{SizeBytes: result.SourceSize},
		Scan:          Scan{BytesScanned: result.BytesScanned, Complete: result.Complete},
		Header:        mapHeader(result),
		Tags:          make([]Tag, 0, index.Len()),
		Diagnostics:   make([]Diagnostic, 0, len(result.Diagnostics)),
	}

	for _, record := range index.All() {
		document.Tags = append(document.Tags, mapTag(record))
	}

	for _, item := range result.Diagnostics {
		document.Diagnostics = append(document.Diagnostics, mapDiagnostic(item))
	}

	return document
}

func mapHeader(result flv.ScanResult) Header {
	header := Header{
		Present:          result.HeaderPresent,
		Version:          result.Header.Version,
		TypeFlags:        result.Header.TypeFlags,
		HasAudio:         result.Header.HasAudio,
		HasVideo:         result.Header.HasVideo,
		DataOffset:       result.Header.DataOffset,
		ExtraDataSize:    result.Header.ExtraDataSize,
		PreviousTagSize0: result.PreviousTagSize0,
	}
	if result.HeaderPresent {
		span := mapSpan(result.Header.Span)
		header.Span = &span
	}
	return header
}

func mapTag(record flv.TagRecord) Tag {
	tagType := record.Type()
	tag := Tag{
		ID:                record.ID,
		TypeCode:          uint8(tagType),
		TypeName:          tagType.String(),
		Offset:            record.Offset,
		Span:              mapSpan(record.Span()),
		HeaderSpan:        mapSpan(record.HeaderSpan()),
		PayloadSpan:       mapSpan(record.PayloadSpan()),
		RawTypeFlags:      record.RawTypeFlags,
		Filter:            record.Filter(),
		DataSize:          record.DataSize,
		AvailableDataSize: record.AvailableDataSize,
		DTS:               record.DTS,
		StreamID:          record.StreamID,
		PayloadComplete:   record.PayloadComplete(),
		PreviousTagSize: PreviousTagSize{
			Present:  record.PreviousTagSizePresent(),
			Value:    record.PreviousTagSize,
			Expected: record.ExpectedPreviousTagSize(),
			Valid:    record.PreviousTagSizeValid(),
		},
	}
	if previousSpan, present := record.PreviousTagSizeSpan(); present {
		span := mapSpan(previousSpan)
		tag.PreviousTagSize.Span = &span
	}
	return tag
}

func mapDiagnostic(item flv.Diagnostic) Diagnostic {
	return Diagnostic{
		Code: item.Code, Severity: item.Severity, Offset: item.Offset,
		BitOffset: item.BitOffset, Message: item.Message,
		Expected: cloneNumber(item.Expected), Actual: cloneNumber(item.Actual),
	}
}

func mapSpan(span cursor.Span) Span {
	return Span{
		ByteOffset: span.ByteOffset,
		BitOffset:  span.BitOffset,
		BitLength:  span.BitLength,
	}
}

func cloneNumber(value *uint64) *uint64 {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}

// WriteJSON writes exactly one JSON document followed by a newline.
func WriteJSON(output io.Writer, document Document, pretty bool) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(document)
}

// WriteScanJSON streams a scan result as one schemaVersion 1 document without
// constructing a second tag slice or buffering the entire JSON payload. The
// CLI uses this path so output memory does not scale with report byte length.
func WriteScanJSON(output io.Writer, result flv.ScanResult, pretty bool) error {
	writer := jsonStreamWriter{output: output, pretty: pretty}
	if pretty {
		return writer.writePretty(result)
	}
	return writer.writeCompact(result)
}

type jsonStreamWriter struct {
	output  io.Writer
	pretty  bool
	scratch bytes.Buffer
}

func (w *jsonStreamWriter) writeCompact(result flv.ScanResult) error {
	index := result.Index()
	if err := w.text(`{"schemaVersion":1,"source":`); err != nil {
		return err
	}
	if err := w.value(Source{SizeBytes: result.SourceSize}, ""); err != nil {
		return err
	}
	if err := w.text(`,"scan":`); err != nil {
		return err
	}
	if err := w.value(Scan{BytesScanned: result.BytesScanned, Complete: result.Complete}, ""); err != nil {
		return err
	}
	if err := w.text(`,"header":`); err != nil {
		return err
	}
	if err := w.value(mapHeader(result), ""); err != nil {
		return err
	}
	if err := w.text(`,"tags":[`); err != nil {
		return err
	}
	for id, record := range index.All() {
		if id > 0 {
			if err := w.text(","); err != nil {
				return err
			}
		}
		if err := w.value(mapTag(record), ""); err != nil {
			return err
		}
	}
	if err := w.text(`],"diagnostics":[`); err != nil {
		return err
	}
	for index, diagnostic := range result.Diagnostics {
		if index > 0 {
			if err := w.text(","); err != nil {
				return err
			}
		}
		if err := w.value(mapDiagnostic(diagnostic), ""); err != nil {
			return err
		}
	}
	return w.text("]}\n")
}

func (w *jsonStreamWriter) writePretty(result flv.ScanResult) error {
	index := result.Index()
	if err := w.text("{\n  \"schemaVersion\": 1,\n  \"source\": "); err != nil {
		return err
	}
	if err := w.value(Source{SizeBytes: result.SourceSize}, "  "); err != nil {
		return err
	}
	if err := w.text(",\n  \"scan\": "); err != nil {
		return err
	}
	if err := w.value(Scan{BytesScanned: result.BytesScanned, Complete: result.Complete}, "  "); err != nil {
		return err
	}
	if err := w.text(",\n  \"header\": "); err != nil {
		return err
	}
	if err := w.value(mapHeader(result), "  "); err != nil {
		return err
	}
	if err := w.text(",\n  \"tags\": "); err != nil {
		return err
	}
	if index.Len() == 0 {
		if err := w.text("[]"); err != nil {
			return err
		}
	} else {
		if err := w.text("[\n"); err != nil {
			return err
		}
		for id, record := range index.All() {
			if err := w.text("    "); err != nil {
				return err
			}
			if err := w.value(mapTag(record), "    "); err != nil {
				return err
			}
			if id+1 < index.Len() {
				if err := w.text(","); err != nil {
					return err
				}
			}
			if err := w.text("\n"); err != nil {
				return err
			}
		}
		if err := w.text("  ]"); err != nil {
			return err
		}
	}
	if err := w.text(",\n  \"diagnostics\": "); err != nil {
		return err
	}
	if len(result.Diagnostics) == 0 {
		if err := w.text("[]"); err != nil {
			return err
		}
	} else {
		if err := w.text("[\n"); err != nil {
			return err
		}
		for index, diagnostic := range result.Diagnostics {
			if err := w.text("    "); err != nil {
				return err
			}
			if err := w.value(mapDiagnostic(diagnostic), "    "); err != nil {
				return err
			}
			if index+1 < len(result.Diagnostics) {
				if err := w.text(","); err != nil {
					return err
				}
			}
			if err := w.text("\n"); err != nil {
				return err
			}
		}
		if err := w.text("  ]"); err != nil {
			return err
		}
	}
	return w.text("\n}\n")
}

func (w *jsonStreamWriter) value(value any, continuationPrefix string) error {
	w.scratch.Reset()
	encoder := json.NewEncoder(&w.scratch)
	encoder.SetEscapeHTML(false)
	if w.pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(value); err != nil {
		return err
	}
	encoded := w.scratch.Bytes()
	if len(encoded) > 0 && encoded[len(encoded)-1] == '\n' {
		encoded = encoded[:len(encoded)-1]
	}
	if !w.pretty || continuationPrefix == "" {
		return writeAll(w.output, encoded)
	}

	start := 0
	for start < len(encoded) {
		relativeEnd := bytes.IndexByte(encoded[start:], '\n')
		if relativeEnd < 0 {
			return writeAll(w.output, encoded[start:])
		}
		end := start + relativeEnd
		if err := writeAll(w.output, encoded[start:end+1]); err != nil {
			return err
		}
		if err := w.text(continuationPrefix); err != nil {
			return err
		}
		start = end + 1
	}
	return nil
}

func (w *jsonStreamWriter) text(value string) error {
	written, err := io.WriteString(w.output, value)
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}

func writeAll(output io.Writer, value []byte) error {
	for len(value) > 0 {
		written, err := output.Write(value)
		if written < 0 || written > len(value) {
			return io.ErrShortWrite
		}
		value = value[written:]
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
