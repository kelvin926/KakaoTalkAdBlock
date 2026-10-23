package internal

import (
	"context"
	"kakaotalkadblock/internal/beta"
	"kakaotalkadblock/internal/win/winapi"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const eventDebounce = 50 * time.Millisecond
const discoveryInterval = 2 * time.Second
const eventHealthInterval = 30 * time.Second
const executable = "kakaotalk.exe"

var mutex sync.Mutex
var mainWindowHandleMap = make(map[windows.HWND]struct{})
var adSubwindowCandidateMap = make(map[windows.HWND]struct{})

// Each snapshot is owned by this call, including empty/failed enumerations.
func kakaoProcessIDs() map[uint32]bool {
	ids := make(map[uint32]bool)
	snapshot := winapi.CreateToolhelp32Snapshot(winapi.Th32csSnapprocess, 0)
	if snapshot == 0 || snapshot == windows.InvalidHandle {
		return ids
	}
	defer windows.CloseHandle(snapshot)
	var entry winapi.ProcessEntry32
	entry.DwSize = uint32(unsafe.Sizeof(entry))
	if !winapi.Process32First(uintptr(snapshot), &entry) {
		return ids
	}
	for {
		name := windows.ByteSliceToString(entry.SzExeFile[:])
		if strings.EqualFold(name, executable) || beta.IsExecutable(name) {
			ids[entry.Th32ProcessID] = beta.IsExecutable(name)
		}
		if !winapi.Process32Next(uintptr(snapshot), &entry) {
			break
		}
	}
	return ids
}

func watch(ctx context.Context, signals *activitySignals) {
	var processIDs map[uint32]bool
	var nextMain, nextAds map[windows.HWND]struct{}
	// One callback per watcher, never one per window or polling iteration.
	enumWindow := syscall.NewCallback(func(handle windows.HWND, _ uintptr) uintptr {
		var pid uint32
		winapi.GetWindowThreadProcessId(handle, &pid)
		if _, ok := processIDs[pid]; !ok {
			return 1
		}
		className := winapi.GetClassName(handle)
		parentHandle := winapi.GetParent(handle)
		beta.TrackWindow(handle, className, parentHandle)
		if className == "EVA_Window_Dblclk" || className == "EVA_Window" {
			windowText := winapi.GetWindowText(handle)
			trackClassicWindow(childWindow{handle, parentHandle, className, windowText}, nextMain, nextAds)
		}
		return 1
	})
	interval := discoveryInterval
	if signals.eventDriven {
		interval = eventHealthInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		processIDs = kakaoProcessIDs()
		signals.processes.Store(processIDs)
		qtTargets := make([]uint32, 0)
		for pid, isQt := range processIDs {
			if isQt {
				qtTargets = append(qtTargets, pid)
			}
		}
		sort.Slice(qtTargets, func(i, j int) bool { return qtTargets[i] < qtTargets[j] })
		replaceQtTargets(signals.qtTargets, qtTargets)
		nextMain = make(map[windows.HWND]struct{})
		nextAds = make(map[windows.HWND]struct{})
		if len(processIDs) != 0 {
			winapi.EnumWindows(enumWindow, 0)
		}
		mutex.Lock()
		// Replace sets so closed windows and reused HWNDs do not accumulate.
		mainWindowHandleMap, adSubwindowCandidateMap = nextMain, nextAds
		mutex.Unlock()
		notify(signals.work)
		notify(signals.layout)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-signals.rescan:
			timer := time.NewTimer(eventDebounce)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-signals.rescan:
			default:
			}
		}
	}
}

func removeAd(ctx context.Context, changes <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-changes:
			timer := time.NewTimer(eventDebounce)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			select {
			case <-changes:
			default:
			}
			removeAdsOnce()
		}
	}
}

func removeAdsOnce() {
	mutex.Lock()
	defer mutex.Unlock()
	for wnd := range mainWindowHandleMap {
		if !winapi.IsWindow(wnd) {
			delete(mainWindowHandleMap, wnd)
			continue
		}
		if !winapi.IsWindowVisible(wnd) {
			continue
		}
		// EnumChildWindows visits every descendant. Do not enumerate recursively.
		children := inspectChildren(wnd)
		if !isMainWindow(children) {
			continue
		}
		var rect winapi.Rect
		if !winapi.GetWindowRect(wnd, &rect) {
			continue
		}
		for _, child := range children {
			if child.parent != wnd {
				continue
			}
			if child.class == "EVA_ChildWindow" && child.text == "" && winapi.GetWindowText(wnd) != "" {
				if !hasCustomScroll(child.handle, children) {
					winapi.SendMessage(child.handle, winapi.WmClose, 0, 0)
				}
			}
			HideMainViewAdArea(child.text, &rect, child.handle)
			HideLockScreenAdArea(child.text, &rect, child.handle)
		}
	}
	for wnd := range adSubwindowCandidateMap {
		if !winapi.IsWindow(wnd) {
			delete(adSubwindowCandidateMap, wnd)
			continue
		}
		if !winapi.IsWindowVisible(wnd) {
			continue
		}
		children := inspectChildren(wnd)
		if winapi.GetWindowText(wnd) == "Chrome Legacy Window" || hasChromeLegacyWindow(children) || hasChromiumAdvertisement(children) {
			winapi.ShowWindow(wnd, 0)
		}
	}
	beta.RemoveAds()
}

type childWindow struct {
	handle windows.HWND
	parent windows.HWND
	class  string
	text   string
}

func trackClassicWindow(window childWindow, main, ads map[windows.HWND]struct{}) {
	if window.class != "EVA_Window_Dblclk" && window.class != "EVA_Window" {
		return
	}
	if window.class == "EVA_Window_Dblclk" && window.text != "" && window.parent == 0 {
		main[window.handle] = struct{}{}
	} else if window.text == "" {
		// EnumWindows includes owned popups, usually before their owner in Z order.
		// Collect candidates independently; browser/ad validation happens below.
		ads[window.handle] = struct{}{}
	}
}

func hasChromiumAdvertisement(children []childWindow) bool {
	hosts := make(map[windows.HWND]struct{})
	for _, child := range children {
		if child.class == "Chrome_WidgetWin_0" {
			hosts[child.handle] = struct{}{}
		}
	}
	for _, child := range children {
		if child.class != "Chrome_WidgetWin_1" || child.text != "MOMENT 광고" && child.text != "AdFit NAS Advertisement" {
			continue
		}
		if _, ok := hosts[child.parent]; ok {
			return true
		}
	}
	return false
}

func inspectChildren(parent windows.HWND) []childWindow {
	handles := winapi.ChildWindows(parent)
	children := make([]childWindow, 0, len(handles))
	for _, handle := range handles {
		children = append(children, childWindow{handle, winapi.GetParent(handle), winapi.GetClassName(handle), winapi.GetWindowText(handle)})
	}
	return children
}

func isMainWindow(children []childWindow) bool {
	for _, child := range children {
		if child.class == "EVA_ChildWindow" && (strings.HasPrefix(child.text, "OnlineMainView") || strings.HasPrefix(child.text, "LockModeView")) {
			return true
		}
	}
	return false
}

func hasCustomScroll(parent windows.HWND, children []childWindow) bool {
	// Parent links are local to this pass, so HWND reuse cannot return stale data.
	parents := make(map[windows.HWND]windows.HWND, len(children))
	for _, child := range children {
		parents[child.handle] = child.parent
	}
	for _, child := range children {
		if !strings.HasPrefix(child.class, "_EVA_") {
			continue
		}
		for handle, remaining := child.handle, len(children); handle != 0 && remaining > 0; remaining-- {
			if handle == parent {
				return true
			}
			handle = parents[handle]
		}
	}
	return false
}

func hasChromeLegacyWindow(children []childWindow) bool {
	for _, child := range children {
		if child.text == "Chrome Legacy Window" {
			return true
		}
	}
	return false
}

func Run(ctx context.Context) {
	signals := newActivitySignals()
	go func() {
		ready := make(chan bool, 1)
		go winapi.WatchWindowEvents(ctx, winapi.WinEventSkipOwnProcess, signals.onWindowEvent, ready)
		select {
		case signals.eventDriven = <-ready:
		case <-ctx.Done():
			return
		}
		go watch(ctx, signals)
		go removeAd(ctx, signals.work)
		go runQtLayoutHelper(ctx, signals.qtTargets, signals.layout)
	}()
}
