package session

import (
	"fmt"

	"github.com/sevico/muxray/internal/flv"
)

// DiagnosticPage is an independent window of retained scan diagnostics. Total
// is not the total number of discovered problems after retention is exhausted.
type DiagnosticPage struct {
	Start   uint64
	Limit   uint32
	Total   uint64
	Next    uint64
	Records []flv.Diagnostic
}

// DiagnosticPage reads a final diagnostic window without cloning the complete
// result. Access/closure and bounds follow Page, including empty EOF pages.
func (s *Session) DiagnosticPage(start uint64, count uint32) (DiagnosticPage, error) {
	result, err := s.finishedResult()
	if err != nil {
		return DiagnosticPage{}, err
	}
	if count == 0 || count > flv.MaxPageSize {
		return DiagnosticPage{}, fmt.Errorf("session: diagnostic page count must be between 1 and %d", flv.MaxPageSize)
	}
	total := uint64(len(result.Diagnostics))
	if start > total {
		return DiagnosticPage{}, fmt.Errorf("session: diagnostic page start %d exceeds retained count %d", start, total)
	}
	next := start + min(uint64(count), total-start)
	records := cloneDiagnostics(result.Diagnostics[start:next])
	if records == nil {
		records = []flv.Diagnostic{}
	}
	return DiagnosticPage{Start: start, Limit: count, Total: total, Next: next, Records: records}, nil
}
