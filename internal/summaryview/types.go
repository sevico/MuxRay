// Package summaryview projects bounded media-summary v2 facts for the desktop.
// It retains no source handle or index and uses strings for exact wire values.
package summaryview

import (
	"encoding/json"
	"errors"
	"unicode/utf8"

	"github.com/sevico/muxray/internal/aac"
	"github.com/sevico/muxray/internal/analysis"
	"github.com/sevico/muxray/internal/detailview"
)

const (
	MaxMetrics       = 128
	MaxValues        = 24
	MaxTextBytes     = 256
	MaxOverviewBytes = 64 << 10
	MaxEventBytes    = 16 << 10
	MaxResponseBytes = 256 << 10
	MaxReportBytes   = 8 << 20
)

var ErrInvalidProjection = errors.New("summaryview: projection exceeds desktop bounds")

type Metric struct {
	Section string  `json:"section"`
	Name    string  `json:"name"`
	Value   *string `json:"value"`
	Unit    string  `json:"unit"`
}
type ListInfo struct {
	Kind      string  `json:"kind"`
	Observed  *string `json:"observed"`
	Retained  string  `json:"retained"`
	Truncated bool    `json:"truncated"`
}
type Overview struct {
	SourceSize    string     `json:"sourceSize"`
	IndexedTags   string     `json:"indexedTags"`
	ScanComplete  bool       `json:"scanComplete"`
	ScanHasErrors bool       `json:"scanHasErrors"`
	Complete      bool       `json:"complete"`
	HasErrors     bool       `json:"hasErrors"`
	Metrics       []Metric   `json:"metrics"`
	Lists         []ListInfo `json:"lists"`
}
type Event struct {
	ID        string   `json:"id"`
	TagID     *string  `json:"tagId"`
	Offset    string   `json:"offset"`
	BitOffset uint8    `json:"bitOffset"`
	DTS       *string  `json:"dts"`
	Title     string   `json:"title"`
	Severity  string   `json:"severity"`
	Message   string   `json:"message"`
	Values    []Metric `json:"values"`
}
type Report struct {
	Overview Overview
	Events   map[string][]Event
}

func DesktopOptions() analysis.OptionsV2 {
	return analysis.OptionsV2{Options: analysis.Options{
		MaxKeyframes: 1024, MaxConfigurationEvents: 128,
		MaxConfigurationBytes: 16 << 20, MaxDiagnostics: 256,
		AAC: aac.Options{MaxDiagnostics: 64}, AVC: detailview.DesktopAVCOptions().AVC,
	}, MaxMP3HeaderEvents: 256}
}

func ValidKind(kind string) bool {
	switch kind {
	case "keyframes", "aac", "avc", "mp3", "diagnostics":
		return true
	}
	return false
}

func (r *Report) validate() error {
	if len(r.Overview.Metrics) > MaxMetrics || len(r.Overview.Lists) != 5 {
		return ErrInvalidProjection
	}
	checkMetrics := func(metrics []Metric) bool {
		for _, m := range metrics {
			for _, s := range []string{m.Section, m.Name, m.Unit} {
				if len(s) > MaxTextBytes || !utf8.ValidString(s) {
					return false
				}
			}
			if m.Value != nil && (len(*m.Value) > MaxTextBytes || !utf8.ValidString(*m.Value)) {
				return false
			}
		}
		return true
	}
	if !checkMetrics(r.Overview.Metrics) {
		return ErrInvalidProjection
	}
	data, err := json.Marshal(r.Overview)
	if err != nil || len(data) > MaxOverviewBytes {
		return ErrInvalidProjection
	}
	totalBytes := len(data)
	for kind, events := range r.Events {
		if !ValidKind(kind) {
			return ErrInvalidProjection
		}
		for _, e := range events {
			if len(e.Values) > MaxValues || !checkMetrics(e.Values) || e.BitOffset > 7 {
				return ErrInvalidProjection
			}
			for _, s := range []string{e.Title, e.Severity, e.Message} {
				if len(s) > MaxTextBytes || !utf8.ValidString(s) {
					return ErrInvalidProjection
				}
			}
			data, err = json.Marshal(e)
			if err != nil || len(data) > MaxEventBytes {
				return ErrInvalidProjection
			}
			totalBytes += len(data)
			if totalBytes > MaxReportBytes {
				return ErrInvalidProjection
			}
		}
	}
	return nil
}
