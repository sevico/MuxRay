//go:build !windows

package main

import "time"

func newBenchmarkClock() (benchmarkClock, error) {
	base := time.Now()
	return benchmarkClock{read: func() (int64, error) { return time.Since(base).Nanoseconds(), nil }, frequency: 1e9, source: "go-monotonic-clock"}, nil
}
