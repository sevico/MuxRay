package report

import (
	"encoding/json"
	"io"

	"github.com/sevico/muxray/internal/flv"
)

const TagPageDocumentType = "flv-tag-page"

// TagPageDocument is a bounded window, not a replacement for the v1 scan report.
type TagPageDocument struct {
	SchemaVersion int          `json:"schemaVersion"`
	DocumentType  string       `json:"documentType"`
	Source        Source       `json:"source"`
	Scan          Scan         `json:"scan"`
	Page          Page         `json:"page"`
	Tags          []Tag        `json:"tags"`
	Diagnostics   []Diagnostic `json:"diagnostics"`
}

type Page struct {
	Start uint64 `json:"start"`
	Limit uint32 `json:"limit"`
	Total uint64 `json:"total"`
	Next  uint64 `json:"next"`
}

func NewTagPage(scan flv.ScanResult, start uint64, count uint32) (TagPageDocument, error) {
	page, err := scan.Index().Page(start, count)
	if err != nil {
		return TagPageDocument{}, err
	}
	document := TagPageDocument{
		SchemaVersion: 1, DocumentType: TagPageDocumentType,
		Source: Source{SizeBytes: scan.SourceSize}, Scan: Scan{BytesScanned: scan.BytesScanned, Complete: scan.Complete},
		Page: Page{Start: page.Start, Limit: page.Limit, Total: page.Total, Next: page.Next},
		Tags: make([]Tag, 0, len(page.Records)), Diagnostics: make([]Diagnostic, 0, len(scan.Diagnostics)),
	}
	for _, tag := range page.Records {
		document.Tags = append(document.Tags, mapTag(tag))
	}
	for _, diagnostic := range scan.Diagnostics {
		document.Diagnostics = append(document.Diagnostics, mapDiagnostic(diagnostic))
	}
	return document, nil
}

func WriteTagPageJSON(output io.Writer, document TagPageDocument, pretty bool) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(document)
}
