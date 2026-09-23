package internal

import (
	"context"
	"golang.org/x/sys/windows"
	"testing"
	"time"
	"unsafe"
)

func processHandleCount(t *testing.T) uint32 {
	t.Helper()
	var count uint32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetProcessHandleCount")
	ret, _, err := proc.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&count)))
	if ret == 0 {
		t.Fatal(err)
	}
	return count
}

func TestSnapshotsReleaseHandles(t *testing.T) {
	// Warm up the runtime/API, then exercise many real snapshot allocations.
	kakaoProcessIDs()
	before := processHandleCount(t)
	for i := 0; i < 64; i++ {
		kakaoProcessIDs()
	}
	after := processHandleCount(t)
	if after > before+4 {
		t.Fatalf("snapshot handles grew: %d -> %d", before, after)
	}
	t.Logf("64 snapshots: handles %d -> %d", before, after)
}

func TestWindowClassification(t *testing.T) {
	children := []childWindow{
		{handle: 2, parent: 1, class: "EVA_ChildWindow", text: "OnlineMainView"},
		{handle: 3, parent: 1, class: "EVA_ChildWindow"},
		{handle: 4, parent: 2, class: "container"},
		{handle: 5, parent: 4, class: "_EVA_CustomScroll"},
		{handle: 6, parent: 3, text: "Chrome Legacy Window"},
	}
	if !isMainWindow(children) || !hasCustomScroll(2, children) || hasCustomScroll(3, children) || !hasChromeLegacyWindow(children) {
		t.Fatal("nested scroll/advertisement classification failed")
	}
	// The next pass sees new titles/classes, even when HWNDs are reused.
	children[0].text = "Chat"
	children[3].class = "plain"
	children[4].text = "plain"
	if isMainWindow(children) || hasCustomScroll(2, children) || hasChromeLegacyWindow(children) {
		t.Fatal("classification retained a prior pass")
	}
}

func TestWatchCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { watch(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch ignored cancellation")
	}
}
