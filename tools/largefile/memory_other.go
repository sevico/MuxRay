//go:build !windows

package main

// Heap/allocation/cancellation measurements are portable. Do not mislabel Go
// heap statistics as RSS on platforms without a process-memory adapter yet.
func processMemory() (current, peak uint64, source string, err error) {
	return 0, 0, "unavailable", nil
}
