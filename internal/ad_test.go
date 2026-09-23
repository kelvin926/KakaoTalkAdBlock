package internal

import (
	"golang.org/x/sys/windows"
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

func TestResizeAdAreaSkipsUnchangedGeometry(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	dll := windows.NewLazySystemDLL("user32.dll")
	create := dll.NewProc("CreateWindowExW")
	destroy := dll.NewProc("DestroyWindow")
	callOriginal := dll.NewProc("CallWindowProcW")
	setProcName := "SetWindowLongPtrW"
	if unsafe.Sizeof(uintptr(0)) == 4 {
		setProcName = "SetWindowLongW"
	}
	setProc := dll.NewProc(setProcName)
	class, _ := windows.UTF16PtrFromString("STATIC")
	handle, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, 0x80000000, 0, 0, 100, 100, 0, 0, 0, 0)
	if handle == 0 {
		t.Fatal(err)
	}
	defer destroy.Call(handle)
	var original uintptr
	changes := 0
	callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, wparam, lparam uintptr) uintptr {
		if msg == 0x0046 {
			changes++
		} // WM_WINDOWPOSCHANGING
		result, _, _ := callOriginal.Call(original, hwnd, uintptr(msg), wparam, lparam)
		return result
	})
	original, _, err = setProc.Call(handle, ^uintptr(3), callback) // GWLP_WNDPROC = -4
	if original == 0 {
		t.Fatal(err)
	}
	defer setProc.Call(handle, ^uintptr(3), original)
	resizeAdArea(windows.HWND(handle), 80, 60)
	if changes != 1 {
		t.Fatalf("expected one initial resize, got %d", changes)
	}
	for i := 0; i < 100; i++ {
		resizeAdArea(windows.HWND(handle), 80, 60)
	}
	if changes != 1 {
		t.Fatalf("unchanged geometry triggered %d resize messages", changes)
	}
	resizeAdArea(windows.HWND(handle), 90, 70)
	if changes != 2 {
		t.Fatal("changed geometry was not applied")
	}
	resizeAdArea(windows.HWND(handle), -1, 0)
	if changes != 2 {
		t.Fatal("invalid geometry was applied")
	}
}
