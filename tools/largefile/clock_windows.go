package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

func newBenchmarkClock() (benchmarkClock, error) {
	kernel := syscall.NewLazyDLL("kernel32.dll")
	frequencyProc := kernel.NewProc("QueryPerformanceFrequency")
	counterProc := kernel.NewProc("QueryPerformanceCounter")
	var frequency int64
	ok, _, err := frequencyProc.Call(uintptr(unsafe.Pointer(&frequency)))
	if ok == 0 || frequency <= 0 {
		return benchmarkClock{}, fmt.Errorf("QueryPerformanceFrequency: %v", err)
	}
	read := func() (int64, error) {
		var ticks int64
		ok, _, err := counterProc.Call(uintptr(unsafe.Pointer(&ticks)))
		if ok == 0 {
			return 0, fmt.Errorf("QueryPerformanceCounter: %w", err)
		}
		return ticks, nil
	}
	return benchmarkClock{read: read, frequency: float64(frequency), source: "windows-query-performance-counter"}, nil
}
