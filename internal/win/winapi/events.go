package winapi

import (
	"context"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	EventObjectCreate         = 0x8000
	EventObjectShow           = 0x8002
	EventObjectLocationChange = 0x800B
	EventObjectNameChange     = 0x800C
	WinEventSkipOwnProcess    = 0x0002
)

type threadMessage struct {
	Window         windows.HWND
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	X, Y           int32
	Private        uint32
}

var (
	setWinEventHook   = user32.NewProc("SetWinEventHook")
	unhookWinEvent    = user32.NewProc("UnhookWinEvent")
	peekMessage       = user32.NewProc("PeekMessageW")
	getMessage        = user32.NewProc("GetMessageW")
	translateMessage  = user32.NewProc("TranslateMessage")
	dispatchMessage   = user32.NewProc("DispatchMessageW")
	postThreadMessage = user32.NewProc("PostThreadMessageW")
	eventSinksMutex   sync.RWMutex
	eventSinks        = make(map[uintptr]func(uint32, windows.HWND))
	// Go syscall callbacks cannot be freed. Allocate one for the whole process.
	windowEventCallback = syscall.NewCallback(func(hook, event, window, object, child, thread, timestamp uintptr) uintptr {
		if window == 0 || int32(object) != 0 || int32(child) != 0 {
			return 0
		}
		if event != EventObjectCreate && event != EventObjectShow && event != EventObjectNameChange && event != EventObjectLocationChange {
			return 0
		}
		eventSinksMutex.RLock()
		sink := eventSinks[hook]
		eventSinksMutex.RUnlock()
		if sink != nil {
			sink(uint32(event), windows.HWND(window))
		}
		return 0
	})
)

// WatchWindowEvents owns a message-pump thread and one out-of-context hook.
// The sink must not block. No timer resolution or system power policy is changed.
func WatchWindowEvents(ctx context.Context, flags uint32, sink func(uint32, windows.HWND), ready chan<- bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var message threadMessage
	peekMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0, 0)
	thread := windows.GetCurrentThreadId()
	hook, _, _ := setWinEventHook.Call(EventObjectCreate, EventObjectNameChange, 0, windowEventCallback, 0, 0, uintptr(flags))
	if hook == 0 {
		ready <- false
		return
	}
	eventSinksMutex.Lock()
	eventSinks[hook] = sink
	eventSinksMutex.Unlock()
	defer func() {
		unhookWinEvent.Call(hook)
		eventSinksMutex.Lock()
		delete(eventSinks, hook)
		eventSinksMutex.Unlock()
	}()
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			postThreadMessage.Call(uintptr(thread), 0x0012, 0, 0) // WM_QUIT
		case <-finished:
		}
	}()
	ready <- true
	for {
		result, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
		if int32(result) <= 0 {
			return
		}
		translateMessage.Call(uintptr(unsafe.Pointer(&message)))
		dispatchMessage.Call(uintptr(unsafe.Pointer(&message)))
	}
}
