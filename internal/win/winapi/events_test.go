package winapi

import (
	"context"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWindowEventsDeliveredAndReleased(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	class, _ := windows.UTF16PtrFromString("STATIC")
	window, _, err := user32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(class)), 0, 0, 0, 0, 20, 20, 0, 0, 0, 0)
	if window == 0 {
		t.Fatal(err)
	}
	defer user32.NewProc("DestroyWindow").Call(window)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready, done, received := make(chan bool, 1), make(chan struct{}), make(chan struct{}, 1)
	go func() {
		WatchWindowEvents(ctx, 0, func(event uint32, handle windows.HWND) {
			if event == EventObjectNameChange && uintptr(handle) == window {
				select {
				case received <- struct{}{}:
				default:
				}
			}
		}, ready)
		close(done)
	}()
	if !<-ready {
		t.Fatal("SetWinEventHook failed")
	}
	user32.NewProc("NotifyWinEvent").Call(EventObjectNameChange, window, 0, 0)
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("window event not delivered")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("event pump did not stop")
	}
	eventSinksMutex.RLock()
	retained := len(eventSinks)
	eventSinksMutex.RUnlock()
	if retained != 0 {
		t.Fatalf("retained %d event sinks after shutdown", retained)
	}
}
