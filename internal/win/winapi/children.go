package winapi

import (
	"golang.org/x/sys/windows"
	"sync"
	"syscall"
)

var childrenMutex sync.Mutex
var collectedChildren []windows.HWND
var collectChildCallback = syscall.NewCallback(func(handle windows.HWND, _ uintptr) uintptr {
	collectedChildren = append(collectedChildren, handle)
	return 1
})

// ChildWindows returns all descendants. Go callbacks cannot be freed, so use
// one callback which never captures a per-call slice. Enumeration is synchronous.
func ChildWindows(parent windows.HWND) []windows.HWND {
	childrenMutex.Lock()
	defer childrenMutex.Unlock()
	collectedChildren = nil
	EnumChildWindows(parent, collectChildCallback, 0)
	result := collectedChildren
	collectedChildren = nil
	return result
}
