package session

import (
	"context"

	"github.com/sevico/muxray/internal/timesearch"
)

// TimeSearch borrows only the final immutable index, including a partial prefix
// after cancellation/failure. No source access or diagnostic copying is needed.
func (s *Session) TimeSearch(ctx context.Context, seed timesearch.State, filter string, gapThreshold uint32) (timesearch.Page, timesearch.State, error) {
	result, err := s.finishedResult()
	if err != nil {
		return timesearch.Page{}, timesearch.State{}, err
	}
	page, next, err := timesearch.Search(ctx, result.Index(), seed, filter, gapThreshold)
	page.ScanComplete = result.Complete
	return page, next, err
}
