//go:build windows

package picker

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

type fakeChildCOM struct {
	dialog struct {
		childFileDialog
		opaqueSentinel uintptr
	}
	item            childShellItem
	path            []uint16
	initHR          uint32
	showHR          uint32
	resultHR        uint32
	displayHR       uint32
	ownerValid      bool
	order           []string
	badThis         bool
	shownOwner      uintptr
	showCount       int
	options         uint32
	title           string
	filterName      string
	filterPattern   string
	filterCount     uintptr
	filterIndex     uintptr
	pathDisplayKind uintptr
}

func newFakeChildCOM() *fakeChildCOM {
	f := &fakeChildCOM{ownerValid: true}
	f.dialog.opaqueSentinel = 0x1234abcd
	f.path, _ = windows.UTF16FromString(`C:\Media\示例.flv`)
	dialogThis := func(p uintptr) { f.badThis = f.badThis || p != uintptr(unsafe.Pointer(&f.dialog.childFileDialog)) }
	itemThis := func(p uintptr) { f.badThis = f.badThis || p != uintptr(unsafe.Pointer(&f.item)) }
	f.dialog.vtable = &childDialogVTable{
		childUnknownVTable: childUnknownVTable{Release: syscall.NewCallback(func(p uintptr) uintptr {
			dialogThis(p)
			f.order = append(f.order, "dialog_release")
			return 42 // A nonzero returned count must never trigger another Release.
		})},
		GetOptions: syscall.NewCallback(func(p uintptr, out *uint32) uintptr {
			dialogThis(p)
			*out = 0x2a2
			return 0
		}),
		SetOptions: syscall.NewCallback(func(p, options uintptr) uintptr {
			dialogThis(p)
			f.options = uint32(options)
			return 0
		}),
		SetTitle: syscall.NewCallback(func(p uintptr, title *uint16) uintptr {
			dialogThis(p)
			f.title, _ = childUTF16Path(title)
			return 0
		}),
		SetFileTypes: syscall.NewCallback(func(p, count uintptr, filter *childFilterSpec) uintptr {
			dialogThis(p)
			f.filterCount = count
			f.filterName, _ = childUTF16Path(filter.name)
			f.filterPattern, _ = childUTF16Path(filter.pattern)
			return 0
		}),
		SetFileTypeIndex: syscall.NewCallback(func(p, index uintptr) uintptr {
			dialogThis(p)
			f.filterIndex = index
			return 0
		}),
		Show: syscall.NewCallback(func(p, owner uintptr) uintptr {
			dialogThis(p)
			f.shownOwner, f.showCount = owner, f.showCount+1
			f.order = append(f.order, "show")
			return uintptr(f.showHR)
		}),
		GetResult: syscall.NewCallback(func(p uintptr, out **childShellItem) uintptr {
			dialogThis(p)
			*out = &f.item
			f.order = append(f.order, "get_result")
			return uintptr(f.resultHR)
		}),
	}
	f.item.vtable = &childShellItemVTable{
		childUnknownVTable: childUnknownVTable{Release: syscall.NewCallback(func(p uintptr) uintptr {
			itemThis(p)
			f.order = append(f.order, "item_release")
			return 7
		})},
		GetDisplayName: syscall.NewCallback(func(p, kind uintptr, out **uint16) uintptr {
			itemThis(p)
			f.pathDisplayKind = kind
			*out = &f.path[0]
			f.order = append(f.order, "get_path")
			return uintptr(f.displayHR)
		}),
	}
	return f
}

func (f *fakeChildCOM) operations() childOperations {
	return childOperations{
		owner: func(Request) (uintptr, func() bool, func(), error) {
			return 12345, func() bool { return f.ownerValid }, func() { f.order = append(f.order, "parent_close") }, nil
		},
		initialize: func() uint32 {
			f.order = append(f.order, "initialize")
			return f.initHR
		},
		uninitialize: func() { f.order = append(f.order, "uninitialize") },
		create: func() (*childFileDialog, uint32) {
			f.order = append(f.order, "create")
			return &f.dialog.childFileDialog, 0
		},
		freeString: func(p *uint16) {
			f.badThis = f.badThis || p != &f.path[0]
			f.order = append(f.order, "free_path")
		},
	}
}

func childTestRequest() Request {
	return Request{Version: Version, Nonce: strings.Repeat("a", 32), Owner: "12345", Kind: "open", ParentPID: 1, ParentCreated: "1"}
}

func TestChildCOMABIAndTeardownBeforeResponse(t *testing.T) {
	ptr := unsafe.Sizeof(uintptr(0))
	var table childDialogVTable
	if unsafe.Offsetof(table.Show) != 3*ptr || unsafe.Offsetof(table.SetOptions) != 9*ptr || unsafe.Offsetof(table.SetTitle) != 17*ptr || unsafe.Offsetof(table.GetResult) != 20*ptr {
		t.Fatal("IFileDialog ABI slot mismatch")
	}
	var itemTable childShellItemVTable
	if unsafe.Offsetof(itemTable.GetDisplayName) != 5*ptr || unsafe.Sizeof(childFileDialog{}) != ptr {
		t.Fatal("native COM object layout mismatch")
	}
	f := newFakeChildCOM()
	f.initHR = 1 // CoInitializeEx S_FALSE still needs one CoUninitialize.
	var request bytes.Buffer
	if err := WriteRequest(&request, childTestRequest()); err != nil {
		t.Fatal(err)
	}
	var response bytes.Buffer
	writer := childTestWriter(func(p []byte) (int, error) {
		want := []string{"initialize", "create", "show", "get_result", "get_path", "free_path", "item_release", "dialog_release", "uninitialize", "parent_close"}
		if !reflect.DeepEqual(f.order, want) {
			t.Errorf("response was written before ordered teardown: %v", f.order)
		}
		return response.Write(p)
	})
	if exit := childMainWithOperations(&request, writer, f.operations()); exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	var got Response
	if err := json.Unmarshal(response.Bytes(), &got); err != nil || got.Status != "selected" || got.Path != `C:\Media\示例.flv` {
		t.Fatalf("response = %+v, error = %v", got, err)
	}
	if f.badThis || f.dialog.opaqueSentinel != 0x1234abcd || f.shownOwner != 12345 || f.showCount != 1 {
		t.Fatal("Show or Release changed native this/opaque COM memory/owner")
	}
	if f.options&0x1840 != 0x1840 || f.options&0x2a0 != 0 || f.options&2 == 0 || f.title == "" || f.filterName == "" || f.filterPattern != "*.flv" || f.filterCount != 1 || f.filterIndex != 1 || f.pathDisplayKind != sigdnFileSystemPath {
		t.Fatal("dialog configuration or filesystem result request was wrong")
	}
}

func TestChildHRESULTAndFailureCleanup(t *testing.T) {
	for _, tt := range []struct {
		name   string
		set    func(*fakeChildCOM)
		status string
		code   string
		order  []string
	}{
		{"cancel", func(f *fakeChildCOM) { f.showHR = hresultCanceled }, "canceled", "", []string{"initialize", "create", "show", "dialog_release", "uninitialize", "parent_close"}},
		{"show_error", func(f *fakeChildCOM) { f.showHR = 0x80070005 }, "error", "dialog_show", []string{"initialize", "create", "show", "dialog_release", "uninitialize", "parent_close"}},
		{"init_error", func(f *fakeChildCOM) { f.initHR = 0x80010106 }, "error", "com_initialization", []string{"initialize", "parent_close"}},
		{"owner_gone", func(f *fakeChildCOM) { f.ownerValid = false }, "error", "invalid_owner", []string{"initialize", "create", "dialog_release", "uninitialize", "parent_close"}},
		{"result_error", func(f *fakeChildCOM) { f.resultHR = 0x80004005 }, "error", "dialog_result", []string{"initialize", "create", "show", "get_result", "item_release", "dialog_release", "uninitialize", "parent_close"}},
		{"path_error", func(f *fakeChildCOM) { f.displayHR = 0x80004005 }, "error", "dialog_result", []string{"initialize", "create", "show", "get_result", "get_path", "free_path", "item_release", "dialog_release", "uninitialize", "parent_close"}},
		{"bad_utf16", func(f *fakeChildCOM) { f.path = []uint16{'C', ':', '\\', 0xd800, 0} }, "error", "invalid_path", []string{"initialize", "create", "show", "get_result", "get_path", "free_path", "item_release", "dialog_release", "uninitialize", "parent_close"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeChildCOM()
			tt.set(f)
			got := childDialogResponse(childTestRequest(), f.operations())
			if got.Status != tt.status || got.Error != tt.code || got.Path != "" || !reflect.DeepEqual(f.order, tt.order) || f.badThis {
				t.Fatalf("got %+v, calls %v, bad this %v", got, f.order, f.badThis)
			}
		})
	}
}

type childTestWriter func([]byte) (int, error)

func (w childTestWriter) Write(p []byte) (int, error) { return w(p) }

func TestChildRejectsInvalidRequestBeforeNativeCalls(t *testing.T) {
	for _, input := range []string{"", "{}", strings.Repeat("x", MaxMessageBytes+1)} {
		if exit := childMainWithOperations(strings.NewReader(input), io.Discard, childOperations{}); exit != 2 {
			t.Fatalf("invalid input returned %d", exit)
		}
	}
	// A reader error before EOF must also prevent all native operations.
	if exit := childMainWithOperations(childFailingReader{}, io.Discard, childOperations{}); exit != 2 {
		t.Fatalf("read failure returned %d", exit)
	}
}

type childFailingReader struct{}

func (childFailingReader) Read([]byte) (int, error) { return 0, errors.New("fixture") }

func TestChildUTF16BoundAndSurrogates(t *testing.T) {
	for _, input := range [][]uint16{{0xd800, 0}, {0xdc00, 0}, {0xd800, 'a', 0}} {
		if _, ok := childUTF16Path(&input[0]); ok {
			t.Fatal("accepted unmatched UTF-16 surrogate")
		}
	}
	bounded := make([]uint16, maxCOMPathUnits)
	for i := range bounded {
		bounded[i] = 'a'
	}
	if _, ok := childUTF16Path(&bounded[0]); ok {
		t.Fatal("accepted unterminated oversized path")
	}
	paired := []uint16{0xd83d, 0xde00, 0}
	if got, ok := childUTF16Path(&paired[0]); !ok || got != "😀" {
		t.Fatal("rejected paired UTF-16 surrogate")
	}
}

func TestChildOwnerRejectsForeignParentWithoutUI(t *testing.T) {
	req := childTestRequest()
	req.ParentPID = windows.GetCurrentProcessId() // Cannot be our own actual parent.
	if _, _, _, err := childOwner(req); err == nil {
		t.Fatal("accepted a request from a process other than the actual parent")
	}
}
