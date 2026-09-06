package desktop

import (
	"errors"

	"github.com/sevico/muxray/internal/flv"
	"github.com/sevico/muxray/internal/session"
)

func (a *App) pageSource(request PageRequest) (scanSession, uint64, *Fault) {
	start, valid := parseStart(request.Start)
	if !valid || request.Count == 0 || request.Count > flv.MaxPageSize {
		return nil, 0, fault(CodeInvalidArgument)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	e := a.findLocked(request.SessionID)
	if e == nil {
		return nil, 0, fault(CodeSessionNotFound)
	}
	if e.closed {
		return nil, 0, fault(CodeSessionClosed)
	}
	if !a.terminalLocked(e) {
		return nil, 0, fault(CodeNotReady)
	}
	if e.scan == nil {
		return nil, 0, fault(CodeOpenFailed)
	}
	return e.scan, start, nil
}

// GetTags returns at most 4,096 independent records, never the full index.
func (b *Bindings) GetTags(request PageRequest) TagPageResponse {
	scan, start, failure := b.app.pageSource(request)
	response := TagPageResponse{APIVersion: APIVersion, Error: failure}
	if failure != nil {
		return response
	}
	page, err := scan.Page(start, request.Count)
	if err != nil {
		response.Error = pageFailure(err)
		return response
	}
	view := &TagPage{SessionID: request.SessionID, Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next), Tags: make([]Tag, 0, len(page.Records))}
	for _, record := range page.Records {
		view.Tags = append(view.Tags, mapTag(record))
	}
	response.Page = view
	return response
}

// GetDiagnostics pages retained diagnostics without cloning all of them.
func (b *Bindings) GetDiagnostics(request PageRequest) DiagnosticPageResponse {
	scan, start, failure := b.app.pageSource(request)
	response := DiagnosticPageResponse{APIVersion: APIVersion, Error: failure}
	if failure != nil {
		return response
	}
	page, err := scan.DiagnosticPage(start, request.Count)
	if err != nil {
		response.Error = pageFailure(err)
		return response
	}
	view := &DiagnosticPage{SessionID: request.SessionID, Bounds: mapBounds(page.Start, page.Limit, page.Total, page.Next), Diagnostics: make([]Diagnostic, 0, len(page.Records))}
	for _, diagnostic := range page.Records {
		view.Diagnostics = append(view.Diagnostics, mapDiagnostic(diagnostic))
	}
	response.Page = view
	return response
}

func pageFailure(err error) *Fault {
	switch {
	case errors.Is(err, session.ErrClosed):
		return fault(CodeSessionClosed)
	case errors.Is(err, session.ErrNotReady):
		return fault(CodeNotReady)
	default: // the underlying page APIs otherwise return only bounds errors
		return fault(CodeInvalidArgument)
	}
}
