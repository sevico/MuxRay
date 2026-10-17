//go:build windows

package picker

import (
	"errors"
	"io"
	"runtime"
	"strconv"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	coinitApartmentThreaded = 2
	clsctxInprocServer      = 1
	hresultCanceled         = 0x800704c7
	sigdnFileSystemPath     = 0x80058000
	maxCOMPathUnits         = 32768
)

var (
	childOle32            = windows.NewLazySystemDLL("ole32.dll")
	childCoInitializeEx   = childOle32.NewProc("CoInitializeEx")
	childCoCreateInstance = childOle32.NewProc("CoCreateInstance")
	fileOpenClassID       = windows.GUID{Data1: 0xdc1c5a9c, Data2: 0xe88a, Data3: 0x4dde, Data4: [8]byte{0xa5, 0xa1, 0x60, 0xf8, 0x2a, 0x20, 0xae, 0xf7}}
	fileOpenInterfaceID   = windows.GUID{Data1: 0xd57c7288, Data2: 0xd4ad, Data3: 0x4768, Data4: [8]byte{0xbe, 0x02, 0x9d, 0x96, 0x95, 0x32, 0xd9, 0x60}}
)

// These are the COM ABI slots through IFileDialog::GetResult. Only the
// vtable pointer belongs to the native object; owner HWND is kept separately.
type childUnknownVTable struct {
	QueryInterface, AddRef, Release uintptr
}

type childDialogVTable struct {
	childUnknownVTable
	Show, SetFileTypes, SetFileTypeIndex, GetFileTypeIndex  uintptr
	Advise, Unadvise, SetOptions, GetOptions                uintptr
	SetDefaultFolder, SetFolder, GetFolder                  uintptr
	GetCurrentSelection, SetFileName, GetFileName           uintptr
	SetTitle, SetOkButtonLabel, SetFileNameLabel, GetResult uintptr
}

type childFileDialog struct{ vtable *childDialogVTable }

type childShellItemVTable struct {
	childUnknownVTable
	BindToHandler, GetParent, GetDisplayName uintptr
}

type childShellItem struct{ vtable *childShellItemVTable }
type childFilterSpec struct{ name, pattern *uint16 }

// childOperations allows non-UI tests to verify HRESULT and COM ownership
// behavior without creating a Shell dialog or initializing COM.
type childOperations struct {
	owner        func(Request) (uintptr, func() bool, func(), error)
	initialize   func() uint32
	uninitialize func()
	create       func() (*childFileDialog, uint32)
	freeString   func(*uint16)
}

func nativeChildOperations() childOperations {
	return childOperations{
		owner: childOwner,
		initialize: func() uint32 {
			hr, _, _ := childCoInitializeEx.Call(0, coinitApartmentThreaded)
			return uint32(hr)
		},
		uninitialize: windows.CoUninitialize,
		create: func() (*childFileDialog, uint32) {
			var dialog *childFileDialog
			hr, _, _ := childCoCreateInstance.Call(
				uintptr(unsafe.Pointer(&fileOpenClassID)), 0, clsctxInprocServer,
				uintptr(unsafe.Pointer(&fileOpenInterfaceID)), uintptr(unsafe.Pointer(&dialog)))
			return dialog, uint32(hr)
		},
		freeString: func(p *uint16) { windows.CoTaskMemFree(unsafe.Pointer(p)) },
	}
}

// ChildMain handles exactly one bounded request. The caller must dispatch this
// mode before starting Wails. No COM or UI runs before ReadRequest succeeds.
func ChildMain(in io.Reader) int {
	return childMainWithOperations(in, openResponsePipe, nativeChildOperations())
}

func childMainWithOperations(in io.Reader, openOutput func(string) (io.WriteCloser, error), operations childOperations) int {
	req, err := ReadRequest(in)
	if err != nil {
		// An invalid request has no trustworthy nonce to echo.
		return 2
	}
	out, err := openOutput(req.ResponseHandle)
	if err != nil {
		return 2
	}
	defer out.Close()
	response := childDialogResponse(req, operations)
	if err := WriteResponse(out, response); err != nil {
		return 3
	}
	return 0
}

func childDialogResponse(req Request, operations childOperations) Response {
	response := Response{Version: req.Version, Nonce: req.Nonce, Status: "error"}
	owner, ownerStillValid, closeParent, err := operations.owner(req)
	if err != nil {
		response.Error = "invalid_owner"
		return response
	}
	defer closeParent()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if hr := operations.initialize(); childHRESULTFailed(hr) {
		response.Error = "com_initialization"
		return response
	}
	// S_FALSE is also a successful initialization requiring CoUninitialize.
	defer operations.uninitialize()
	dialog, hr := operations.create()
	if dialog != nil {
		defer dialog.release()
	}
	if childHRESULTFailed(hr) || dialog == nil {
		response.Error = "dialog_creation"
		return response
	}
	if !dialog.configure() {
		response.Error = "dialog_configuration"
		return response
	}
	if !ownerStillValid() {
		response.Error = "invalid_owner"
		return response
	}
	hr = dialog.show(owner)
	if hr == hresultCanceled {
		response.Status = "canceled"
		return response
	}
	if childHRESULTFailed(hr) {
		response.Error = "dialog_show"
		return response
	}
	item, hr := dialog.result()
	if item != nil {
		defer item.release()
	}
	if childHRESULTFailed(hr) || item == nil {
		response.Error = "dialog_result"
		return response
	}
	pathPointer, hr := item.fileSystemPath()
	if pathPointer != nil {
		defer operations.freeString(pathPointer)
	}
	if childHRESULTFailed(hr) || pathPointer == nil {
		response.Error = "dialog_result"
		return response
	}
	path, ok := childUTF16Path(pathPointer)
	if !ok || !validPath(path) {
		response.Error = "invalid_path"
		return response
	}
	response.Status, response.Path = "selected", path
	return response
}

func childHRESULTFailed(hr uint32) bool { return int32(hr) < 0 }

func (d *childFileDialog) release() {
	// Release only this owned interface reference, regardless of returned count.
	syscall.SyscallN(d.vtable.Release, uintptr(unsafe.Pointer(d)))
	runtime.KeepAlive(d)
}

func (d *childFileDialog) configure() bool {
	var options uint32
	hr, _, _ := syscall.SyscallN(d.vtable.GetOptions, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&options)))
	if childHRESULTFailed(uint32(hr)) {
		return false
	}
	const forceFilesystem, pathMustExist, fileMustExist = 0x40, 0x800, 0x1000
	const pickFolders, allowMultiSelect, allNonStorageItems = 0x20, 0x200, 0x80
	options = (options &^ (pickFolders | allowMultiSelect | allNonStorageItems)) | forceFilesystem | pathMustExist | fileMustExist
	hr, _, _ = syscall.SyscallN(d.vtable.SetOptions, uintptr(unsafe.Pointer(d)), uintptr(options))
	if childHRESULTFailed(uint32(hr)) {
		return false
	}
	title, _ := windows.UTF16FromString("选择 FLV 文件")
	hr, _, _ = syscall.SyscallN(d.vtable.SetTitle, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&title[0])))
	runtime.KeepAlive(title)
	if childHRESULTFailed(uint32(hr)) {
		return false
	}
	name, _ := windows.UTF16FromString("FLV 文件 (*.flv)")
	pattern, _ := windows.UTF16FromString("*.flv")
	filter := childFilterSpec{&name[0], &pattern[0]}
	hr, _, _ = syscall.SyscallN(d.vtable.SetFileTypes, uintptr(unsafe.Pointer(d)), 1, uintptr(unsafe.Pointer(&filter)))
	runtime.KeepAlive(name)
	runtime.KeepAlive(pattern)
	if childHRESULTFailed(uint32(hr)) {
		return false
	}
	hr, _, _ = syscall.SyscallN(d.vtable.SetFileTypeIndex, uintptr(unsafe.Pointer(d)), 1)
	runtime.KeepAlive(d)
	return !childHRESULTFailed(uint32(hr))
}

func (d *childFileDialog) show(owner uintptr) uint32 {
	hr, _, _ := syscall.SyscallN(d.vtable.Show, uintptr(unsafe.Pointer(d)), owner)
	runtime.KeepAlive(d)
	return uint32(hr)
}

func (d *childFileDialog) result() (*childShellItem, uint32) {
	var item *childShellItem
	hr, _, _ := syscall.SyscallN(d.vtable.GetResult, uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&item)))
	runtime.KeepAlive(d)
	return item, uint32(hr)
}

func (s *childShellItem) release() {
	syscall.SyscallN(s.vtable.Release, uintptr(unsafe.Pointer(s)))
	runtime.KeepAlive(s)
}

func (s *childShellItem) fileSystemPath() (*uint16, uint32) {
	var path *uint16
	hr, _, _ := syscall.SyscallN(s.vtable.GetDisplayName, uintptr(unsafe.Pointer(s)), sigdnFileSystemPath, uintptr(unsafe.Pointer(&path)))
	runtime.KeepAlive(s)
	return path, uint32(hr)
}

func childUTF16Path(p *uint16) (string, bool) {
	if p == nil {
		return "", false
	}
	units := make([]uint16, 0, 260)
	for i := 0; i < maxCOMPathUnits; i++ {
		unit := *(*uint16)(unsafe.Add(unsafe.Pointer(p), uintptr(i)*2))
		if unit == 0 {
			for j := 0; j < len(units); j++ {
				if units[j] >= 0xd800 && units[j] <= 0xdbff {
					j++
					if j >= len(units) || units[j] < 0xdc00 || units[j] > 0xdfff {
						return "", false
					}
				} else if units[j] >= 0xdc00 && units[j] <= 0xdfff {
					return "", false
				}
			}
			return string(utf16.Decode(units)), true
		}
		units = append(units, unit)
	}
	return "", false
}

// Bind the HWND to the actual parent process generation, holding that process
// handle until COM teardown. Recheck its liveness and window immediately before Show.
func childOwner(req Request) (uintptr, func() bool, func(), error) {
	bad := func() (uintptr, func() bool, func(), error) { return 0, nil, nil, errors.New("invalid_owner") }
	owner, err := strconv.ParseUint(req.Owner, 10, strconv.IntSize)
	if err != nil || owner == 0 || req.ParentPID == 0 {
		return bad()
	}
	created, err := strconv.ParseUint(req.ParentCreated, 10, 64)
	if err != nil || created == 0 {
		return bad()
	}
	var info windows.PROCESS_BASIC_INFORMATION
	var returned uint32
	if err := windows.NtQueryInformationProcess(windows.CurrentProcess(), windows.ProcessBasicInformation, unsafe.Pointer(&info), uint32(unsafe.Sizeof(info)), &returned); err != nil || info.InheritedFromUniqueProcessId != uintptr(req.ParentPID) {
		return bad()
	}
	parent, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, req.ParentPID)
	if err != nil {
		return bad()
	}
	closeParent := func() { windows.CloseHandle(parent) }
	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(parent, &creation, &exit, &kernel, &user); err != nil || uint64(creation.HighDateTime)<<32|uint64(creation.LowDateTime) != created {
		closeParent()
		return bad()
	}
	stillValid := func() bool {
		status, err := windows.WaitForSingleObject(parent, 0)
		if err != nil || status != uint32(windows.WAIT_TIMEOUT) || !windows.IsWindow(windows.HWND(owner)) {
			return false
		}
		var ownerPID uint32
		tid, err := windows.GetWindowThreadProcessId(windows.HWND(owner), &ownerPID)
		return err == nil && tid != 0 && ownerPID == req.ParentPID
	}
	if !stillValid() {
		closeParent()
		return bad()
	}
	return uintptr(owner), stillValid, closeParent, nil
}
