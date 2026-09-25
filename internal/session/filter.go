package session

import "github.com/sevico/muxray/internal/flv"

// FilteredPage queries only the committed final index, including a partial
// index after cancellation or failure. No source or full result copy is needed.
func (s *Session) FilteredPage(start uint64, count uint32, filter flv.TagFilter) (flv.FilteredTagPage, error) {
	result, err := s.finishedResult()
	if err != nil {
		return flv.FilteredTagPage{}, err
	}
	return flv.FilterTags(result.Index(), start, count, filter)
}
