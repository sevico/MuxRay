package session

import "github.com/sevico/muxray/internal/flv"

// TrendPage queries the immutable final index, including a partial index after
// cancellation or failure. It visits a bounded window/context without source I/O.
func (s *Session) TrendPage(start uint64, count uint32) (flv.TagTrendPage, error) {
	result, err := s.finishedResult()
	if err != nil {
		return flv.TagTrendPage{}, err
	}
	return flv.TrendTags(result.Index(), start, count)
}
