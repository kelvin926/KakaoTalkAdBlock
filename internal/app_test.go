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
	go func() { watch(ctx, newActivitySignals()); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watch ignored cancellation")
	}
}

func TestOwnedPopupBeforeMainWindow(t *testing.T) {
	main, ads := make(map[windows.HWND]struct{}), make(map[windows.HWND]struct{})
	// Owned popups can precede their owner in EnumWindows' Z order.
	trackClassicWindow(childWindow{2, 1, "EVA_Window_Dblclk", ""}, main, ads)
	trackClassicWindow(childWindow{3, 1, "EVA_Window", ""}, main, ads)
	trackClassicWindow(childWindow{1, 0, "EVA_Window_Dblclk", "카카오톡"}, main, ads)
	trackClassicWindow(childWindow{4, 0, "EVA_Window_Dblclk", "Chat"}, main, ads)
	trackClassicWindow(childWindow{5, 1, "tooltips_class32", ""}, main, ads)
	if len(main) != 2 || len(ads) != 2 {
		t.Fatalf("unexpected discovery: main=%v ads=%v", main, ads)
	}
	if _, ok := ads[2]; !ok {
		t.Fatal("owned advertisement candidate lost before owner discovery")
	}
}

func TestChromiumPopupRequiresAdvertisementIdentity(t *testing.T) {
	for _, title := range []string{"MOMENT 광고", "AdFit NAS Advertisement"} {
		children := []childWindow{
			{handle: 3, parent: 2, class: "Chrome_WidgetWin_1", text: title},
			{handle: 2, parent: 1, class: "Chrome_WidgetWin_0"},
			{handle: 4, parent: 3, class: "Intermediate D3D Window"},
		}
		if !hasChromiumAdvertisement(children) {
			t.Fatalf("advertisement without legacy renderer missed: %s", title)
		}
		children[0].text = "Message preview"
		if hasChromiumAdvertisement(children) {
			t.Fatal("ordinary web content classified as an advertisement")
		}
		children[0].text, children[0].parent = title, 9
		if hasChromiumAdvertisement(children) {
			t.Fatal("unrelated Chromium host accepted")
		}
		children[0].parent, children[0].class = 2, "STATIC"
		if hasChromiumAdvertisement(children) {
			t.Fatal("native notification with matching text accepted")
		}
	}
}
