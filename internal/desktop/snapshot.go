package desktop

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/sevico/muxray/internal/document"
	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/summaryview"
)

const (
	MaxSnapshotBytes               = 2 << 20
	SnapshotDiagnosticLimit uint32 = 100
	CodeSnapshotFailed             = "snapshot_failed"
)

// SnapshotRequest describes a view, not caller-supplied report data. Optional
// IDs name completed results already retained by this exact document.
type SnapshotRequest struct {
	SchemaVersion    uint8                       `json:"schemaVersion,omitempty"`
	SessionID        string                      `json:"sessionId"`
	SelectedTagID    *string                     `json:"selectedTagId"`
	DetailRequestID  *string                     `json:"detailRequestId"`
	HexRequestID     *string                     `json:"hexRequestId"`
	SummaryRequestID *string                     `json:"summaryRequestId"`
	SummaryPage      *SnapshotSummaryPageRequest `json:"summaryPage"`
	Trend            *SnapshotTrendRequest       `json:"trend"`
	MediaCurve       *SnapshotMediaCurveRequest  `json:"mediaCurve,omitempty"`
}

type SnapshotSummaryPageRequest struct {
	Kind  string `json:"kind"`
	Start string `json:"start"`
	Count uint32 `json:"count"`
}

type SnapshotTrendRequest struct {
	Start        string `json:"start"`
	Count        uint32 `json:"count"`
	Type         string `json:"type"`
	Metric       string `json:"metric"`
	GapThreshold string `json:"gapThreshold"`
}

// SnapshotCapture is a host-only frozen value. In particular SourcePath must
// never be serialized or exposed as a frontend-selected destination.
type SnapshotCapture struct {
	Data       []byte
	SourcePath string
	SourceInfo os.FileInfo
	FileName   string
}

type snapshotReadSession interface {
	SnapshotDetail(uint64) (document.DetailResult, error)
	SnapshotHex(uint64) (document.HexResult, error)
}

type snapshotIdentitySession interface {
	SnapshotSourceInfo() (os.FileInfo, error)
}

type SnapshotOmission struct {
	Section string `json:"section"`
	Reason  string `json:"reason"`
}

// DiagnosticSnapshot is an independent format, not a CLI report schema. The
// selection describes display rules; trend gaps are not parser diagnostics.
type DiagnosticSnapshot struct {
	SchemaVersion   uint8                `json:"schemaVersion"`
	DocumentType    string               `json:"documentType"`
	FileName        string               `json:"fileName"`
	CreatedAt       string               `json:"createdAt"`
	Producer        SnapshotProducer     `json:"producer"`
	Scope           SnapshotScope        `json:"scope"`
	Session         *SessionSnapshot     `json:"session"`
	Selection       SnapshotRequest      `json:"selection"`
	ScanDiagnostics *DiagnosticPage      `json:"scanDiagnostics"`
	SelectedTag     *Tag                 `json:"selectedTag"`
	Detail          *DetailResponse      `json:"detail"`
	Hex             *HexResponse         `json:"hex"`
	Summary         *SummaryResponse     `json:"summary"`
	SummaryPage     *SummaryPageResponse `json:"summaryPage"`
	Trend           *TagTrendPage        `json:"trend"`
	TrendIssues     []SnapshotTrendIssue `json:"trendIssues"`
	Omitted         []SnapshotOmission   `json:"omitted"`
	MediaCurvePage  *MediaCurveResponse  `json:"mediaCurvePage,omitempty"`
}

type SnapshotProducer struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type SnapshotTrendIssue struct {
	Kind        string   `json:"kind"`
	SourceTagID string   `json:"sourceTagId"`
	Codes       []string `json:"codes"`
}

type SnapshotScope struct {
	Kind                     string `json:"kind"`
	SourceValidation         string `json:"sourceValidation"`
	ScanDiagnostics          string `json:"scanDiagnostics"`
	Summary                  string `json:"summary"`
	Trend                    string `json:"trend"`
	TrendInterpretation      string `json:"trendInterpretation"`
	MediaCurve               string `json:"mediaCurve,omitempty"`
	MediaCurveInterpretation string `json:"mediaCurveInterpretation,omitempty"`
}

// CaptureSnapshot runs only bounded in-memory queries. Holding the slot lock
// fences CloseFile, OpenFile and Shutdown throughout capture. Getters return
// detached values with exact request identities; reads already admitted before
// this lock may finish or supersede a result, which is explicitly omitted.
// No source Stat/ReadAt, analysis worker or full-index traversal is started.
// Once returned, the frozen bytes survive closing or replacing the document.
func (a *App) CaptureSnapshot(request SnapshotRequest) (SnapshotCapture, *Fault) {
	if !validSnapshotRequest(request) {
		return SnapshotCapture{}, fault(CodeInvalidArgument)
	}
	version := uint8(1)
	if request.SchemaVersion == 2 {
		version = 2
	} else {
		// An explicitly selected v1 keeps the original selection JSON shape.
		request.SchemaVersion = 0
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.ctx.Err() != nil {
		return SnapshotCapture{}, fault(CodeShuttingDown)
	}
	e := a.findLocked(request.SessionID)
	if e == nil {
		return SnapshotCapture{}, fault(CodeSessionNotFound)
	}
	if e.closed {
		return SnapshotCapture{}, fault(CodeSessionClosed)
	}
	if !a.terminalLocked(e) {
		return SnapshotCapture{}, fault(CodeNotReady)
	}
	if e.scan == nil {
		return SnapshotCapture{}, fault(CodeOpenFailed)
	}
	// TrendPage checks the document's change latch without probing the file.
	if _, err := e.scan.TrendPage(0, 1); err != nil {
		return SnapshotCapture{}, detailFailure(err)
	}
	var sourceInfo os.FileInfo
	if owner, ok := e.scan.(snapshotIdentitySession); ok {
		var err error
		sourceInfo, err = owner.SnapshotSourceInfo()
		if err != nil {
			return SnapshotCapture{}, detailFailure(err)
		}
	}
	report := DiagnosticSnapshot{
		SchemaVersion: version, DocumentType: "desktopDiagnosticSnapshot", FileName: filepath.Base(e.sourcePath),
		CreatedAt: time.Now().UTC().Format(time.RFC3339Nano), Producer: SnapshotProducer{Name: "MuxRay", Version: "0.1.0-dev"},
		Scope: SnapshotScope{Kind: "bounded_desktop_snapshot", SourceValidation: "existing_change_latch_only",
			ScanDiagnostics: "first_100_retained", Summary: "ready_overview_and_requested_event_page",
			Trend: "requested_source_order_window", TrendInterpretation: "local_display_rule_not_parser_diagnostic"},
		Session: a.responseLocked(e).Session, Selection: request, Omitted: []SnapshotOmission{}, TrendIssues: []SnapshotTrendIssue{},
	}
	omit := func(section, reason string) {
		report.Omitted = append(report.Omitted, SnapshotOmission{section, reason})
	}
	page, err := e.scan.DiagnosticPage(0, SnapshotDiagnosticLimit)
	if err != nil {
		return SnapshotCapture{}, detailFailure(err)
	}
	report.ScanDiagnostics = &DiagnosticPage{SessionID: e.id, Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next), Diagnostics: make([]Diagnostic, 0, len(page.Records))}
	for _, diagnostic := range page.Records {
		report.ScanDiagnostics.Diagnostics = append(report.ScanDiagnostics.Diagnostics, mapDiagnostic(diagnostic))
	}
	if request.SelectedTagID == nil {
		omit("selectedTag", "not_requested")
	} else {
		id, _ := parseStart(*request.SelectedTagID)
		page, err := e.scan.Page(id, 1)
		if err != nil || len(page.Records) != 1 || page.Records[0].ID != id {
			omit("selectedTag", CodeTagNotFound)
		} else {
			tag := mapTag(page.Records[0])
			report.SelectedTag = &tag
		}
	}
	a.captureDetail(e, &report, omit)
	a.captureHex(e, &report, omit)
	a.captureSummary(e, &report, omit)
	if version == 2 {
		report.Scope.MediaCurve = "requested_retained_summary_curve_page"
		report.Scope.MediaCurveInterpretation = "codec_payload_rate_and_declared_gop_not_transport_rate_or_decodability"
		a.captureMediaCurve(e, &report, omit)
	}
	if request.Trend == nil {
		omit("trend", "not_requested")
	} else {
		q := request.Trend
		start, _ := parseStart(q.Start)
		page, err := e.scan.TrendPage(start, q.Count)
		if err != nil {
			if errors.Is(err, document.ErrSourceChanged) {
				omit("trend", CodeSourceChanged)
			} else {
				omit("trend", pageFailure(err).Code)
			}
		} else {
			report.Trend = &TagTrendPage{SessionID: e.id, Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next), ContextStart: decimal(page.ContextStart), ContextScanned: page.ContextScanned, Points: make([]TagTrendPoint, 0, len(page.Points))}
			for _, point := range page.Points {
				report.Trend.Points = append(report.Trend.Points, mapTrendPoint(point))
			}
			report.TrendIssues = snapshotTrendIssues(report.Trend.Points, *q)
		}
	}
	data, err := json.Marshal(report)
	if err != nil || len(data)+1 > MaxSnapshotBytes {
		return SnapshotCapture{}, snapshotFault()
	}
	// A worker admitted before capture may discover a source change while the
	// detached sections are copied. Never publish a mixture after that latch.
	if _, err := e.scan.TrendPage(0, 1); err != nil {
		return SnapshotCapture{}, detailFailure(err)
	}
	if a.ctx.Err() != nil {
		return SnapshotCapture{}, fault(CodeShuttingDown)
	}
	return SnapshotCapture{Data: append(data, '\n'), SourcePath: e.sourcePath, SourceInfo: sourceInfo, FileName: filepath.Base(e.sourcePath)}, nil
}

func validSnapshotRequest(q SnapshotRequest) bool {
	if q.SessionID == "" || len(q.SessionID) > 64 || q.SchemaVersion > 2 {
		return false
	}
	if q.MediaCurve != nil && (q.SchemaVersion != 2 || !validSnapshotMediaCurve(*q.MediaCurve)) {
		return false
	}
	for _, id := range []*string{q.SelectedTagID, q.DetailRequestID, q.HexRequestID, q.SummaryRequestID} {
		if id != nil {
			if _, ok := parseStart(*id); !ok {
				return false
			}
		}
	}
	for _, id := range []*string{q.DetailRequestID, q.HexRequestID, q.SummaryRequestID} {
		if id != nil && *id == "0" {
			return false
		}
	}
	if q.SummaryPage != nil {
		p := q.SummaryPage
		if _, ok := parseStart(p.Start); !ok || p.Count == 0 || p.Count > 100 || !summaryview.ValidKind(p.Kind) {
			return false
		}
	}
	if q.Trend != nil {
		t := q.Trend
		if _, ok := parseStart(t.Start); !ok || t.Count == 0 || t.Count > flv.MaxTrendPoints {
			return false
		}
		gap, ok := parseStart(t.GapThreshold)
		if !ok || gap == 0 || gap > math.MaxUint32 {
			return false
		}
		switch t.Type {
		case "all", "audio", "video", "script", "other":
		default:
			return false
		}
		switch t.Metric {
		case "dts", "delta", "size":
		default:
			return false
		}
	}
	return true
}

func (a *App) captureDetail(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection
	if q.DetailRequestID == nil {
		omit("detail", "not_requested")
		return
	}
	if r.SelectedTag == nil {
		omit("detail", "selected_tag_unavailable")
		return
	}
	owner, ok := e.scan.(detailSession)
	if !ok {
		omit("detail", CodeDetailUnsupported)
		return
	}
	id, _ := parseStart(*q.DetailRequestID)
	var result document.DetailResult
	var err error
	if cached, ok := e.scan.(snapshotReadSession); ok {
		result, err = cached.SnapshotDetail(id)
	} else {
		result, err = owner.GetDetail(id)
	}
	response := mapDetailResult(detailResponse(e.id, *q.DetailRequestID, *q.SelectedTagID), result, err)
	if response.Error != nil {
		omit("detail", response.Error.Code)
		return
	}
	if response.RequestID != *q.DetailRequestID || response.TagID != *q.SelectedTagID {
		omit("detail", "identity_mismatch")
		return
	}
	if response.State != "ready" || response.Detail == nil {
		omit("detail", CodeNotReady)
		return
	}
	r.Detail = &response
}

func (a *App) captureHex(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection
	if q.HexRequestID == nil {
		omit("hex", "not_requested")
		return
	}
	owner, ok := e.scan.(hexSession)
	if !ok {
		omit("hex", CodeHexUnsupported)
		return
	}
	id, _ := parseStart(*q.HexRequestID)
	var result document.HexResult
	var err error
	if cached, ok := e.scan.(snapshotReadSession); ok {
		result, err = cached.SnapshotHex(id)
	} else {
		result, err = owner.GetHex(id)
	}
	response := mapHexResult(hexResponse(e.id, *q.HexRequestID, ""), result, err)
	if response.Error != nil {
		omit("hex", response.Error.Code)
		return
	}
	if response.RequestID != *q.HexRequestID {
		omit("hex", "identity_mismatch")
		return
	}
	if response.State != "ready" || response.Window == nil {
		omit("hex", CodeNotReady)
		return
	}
	r.Hex = &response
}

func (a *App) captureSummary(e *slot, r *DiagnosticSnapshot, omit func(string, string)) {
	q := r.Selection
	if q.SummaryRequestID == nil {
		omit("summary", "not_requested")
		omit("summaryPage", "summary_unavailable")
		return
	}
	owner, ok := e.scan.(summarySession)
	if !ok {
		omit("summary", CodeSummaryUnsupported)
		omit("summaryPage", "summary_unavailable")
		return
	}
	id, _ := parseStart(*q.SummaryRequestID)
	result, err := owner.GetSummary(id)
	reason := ""
	switch {
	case err != nil:
		reason = summaryFailure(err).Code
	case result.Err != nil:
		reason = summaryFailure(result.Err).Code
	case result.RequestID != id:
		reason = "identity_mismatch"
	case result.State != "ready" || result.Overview == nil:
		reason = CodeNotReady
	}
	if reason != "" {
		omit("summary", reason)
		omit("summaryPage", "summary_unavailable")
		return
	}
	r.Summary = &SummaryResponse{APIVersion: APIVersion, SessionID: e.id, RequestID: decimal(id), State: result.State, Processed: decimal(result.Processed), Total: decimal(result.Total), Overview: result.Overview}
	if q.SummaryPage == nil {
		omit("summaryPage", "not_requested")
		return
	}
	p := q.SummaryPage
	start, _ := parseStart(p.Start)
	page, err := owner.SummaryEvents(id, p.Kind, start, p.Count)
	if err != nil {
		omit("summaryPage", summaryFailure(err).Code)
		return
	}
	if page.Kind != p.Kind || page.Start != start {
		omit("summaryPage", "identity_mismatch")
		return
	}
	r.SummaryPage = &SummaryPageResponse{APIVersion: APIVersion, SessionID: e.id, RequestID: decimal(id), Kind: p.Kind,
		Page: &SummaryPage{Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next), Events: page.Events}}
}

func snapshotFault() *Fault {
	return &Fault{Code: CodeSnapshotFailed, Message: "The bounded diagnostic snapshot could not be captured."}
}

func snapshotTrendIssues(points []TagTrendPoint, q SnapshotTrendRequest) []SnapshotTrendIssue {
	issues := make([]SnapshotTrendIssue, 0)
	gap, _ := strconv.ParseInt(q.GapThreshold, 10, 64)
	for _, point := range points {
		kind := "other"
		switch point.TypeCode {
		case 8:
			kind = "audio"
		case 9:
			kind = "video"
		case 18:
			kind = "script"
		}
		if q.Type != "all" && q.Type != kind {
			continue
		}
		codes := make([]string, 0, 3)
		if point.DeltaMS != nil {
			delta, _ := strconv.ParseInt(*point.DeltaMS, 10, 64)
			switch {
			case delta < 0:
				codes = append(codes, "regression")
			case delta == 0:
				codes = append(codes, "duplicate")
			case delta > gap:
				codes = append(codes, "gap")
			}
		}
		if !point.PayloadComplete {
			codes = append(codes, "payload")
		}
		switch point.PreviousTagSizeState {
		case "missing":
			codes = append(codes, "previous_missing")
		case "invalid":
			codes = append(codes, "previous_invalid")
		}
		if len(codes) > 0 {
			issues = append(issues, SnapshotTrendIssue{Kind: "local_observation", SourceTagID: point.TagID, Codes: codes})
		}
	}
	return issues
}
