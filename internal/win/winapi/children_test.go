package winapi

import (
	"golang.org/x/sys/windows"
	"runtime"
	"testing"
	"unsafe"
)

func TestChildWindowsDoesNotRetainPreviousEnumeration(t *testing.T) {
	// Hidden test-owned windows only; no interaction with the user's apps.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	create := user32.NewProc("CreateWindowExW")
	destroy := user32.NewProc("DestroyWindow")
	class, _ := windows.UTF16PtrFromString("STATIC")
	newWindow := func(parent windows.HWND) windows.HWND {
		var style uintptr
		if parent != 0 {
			style = 0x40000000
		} // WS_CHILD
		handle, _, err := create.Call(0, uintptr(unsafe.Pointer(class)), 0, style, 0, 0, 20, 20, uintptr(parent), 0, 0, 0)
		if handle == 0 {
			t.Fatal(err)
		}
		return windows.HWND(handle)
	}
	root := newWindow(0)
	defer destroy.Call(uintptr(root))
	child := newWindow(root)
	grandchild := newWindow(child)
	for i := 0; i < 100; i++ {
		got := ChildWindows(root)
		if len(got) != 2 || got[0] != child || got[1] != grandchild {
			t.Fatalf("pass %d: %v", i, got)
		}
		if collectedChildren != nil {
			t.Fatal("collector retained the result")
		}
	}
	destroy.Call(uintptr(child))
	if got := ChildWindows(root); len(got) != 0 {
		t.Fatalf("closed descendants retained: %v", got)
	}
	child = newWindow(root)
	if got := ChildWindows(root); len(got) != 1 || got[0] != child {
		t.Fatalf("new child not detected: %v", got)
	}
}
