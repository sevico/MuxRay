package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

var memoryInfoProc = syscall.NewLazyDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

// PROCESS_MEMORY_COUNTERS_EX uses DWORD followed by native SIZE_T fields.
// https://learn.microsoft.com/en-us/windows/win32/api/psapi/ns-psapi-process_memory_counters_ex
type memoryCounters struct {
	Size, PageFaultCount                           uint32
	PeakWorkingSet, WorkingSet                     uintptr
	QuotaPeakPagedPool, QuotaPagedPool             uintptr
	QuotaPeakNonPagedPool, QuotaNonPagedPool       uintptr
	PagefileUsage, PeakPagefileUsage, PrivateUsage uintptr
}

func processMemory() (current, peak uint64, source string, err error) {
	counters := memoryCounters{}
	counters.Size = uint32(unsafe.Sizeof(counters))
	ok, _, callErr := memoryInfoProc.Call(^uintptr(0), uintptr(unsafe.Pointer(&counters)), uintptr(counters.Size))
	if ok == 0 {
		return 0, 0, "", fmt.Errorf("GetProcessMemoryInfo: %w", callErr)
	}
	return uint64(counters.WorkingSet), uint64(counters.PeakWorkingSet), "windows-process-working-set", nil
}
