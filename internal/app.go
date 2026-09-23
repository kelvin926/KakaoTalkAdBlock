package internal

import (
	"context"
	"kakaotalkadblock/internal/beta"
	"kakaotalkadblock/internal/win/winapi"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const sleepTime = 100 * time.Millisecond
const discoveryInterval = 2 * time.Second
const executable = "kakaotalk.exe"

var mutex sync.Mutex
var mainWindowHandleMap = make(map[windows.HWND]struct{})
var adSubwindowCandidateMap = make(map[windows.HWND]struct{})

// Each snapshot is owned by this call, including empty/failed enumerations.
func kakaoProcessIDs() map[uint32]struct{} {
	ids := make(map[uint32]struct{})
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
			ids[entry.Th32ProcessID] = struct{}{}
		}
		if !winapi.Process32Next(uintptr(snapshot), &entry) {
			break
		}
	}
	return ids
}

func watch(ctx context.Context) {
	var processIDs map[uint32]struct{}
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
			if className == "EVA_Window_Dblclk" && windowText != "" && parentHandle == 0 {
				nextMain[handle] = struct{}{}
			} else if windowText == "" {
				if className == "EVA_Window" && parentHandle == 0 {
					nextAds[handle] = struct{}{}
				} else if _, ok := nextMain[parentHandle]; ok {
					nextAds[handle] = struct{}{}
				}
			}
		}
		return 1
	})
	ticker := time.NewTicker(discoveryInterval)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		processIDs = kakaoProcessIDs()
		nextMain = make(map[windows.HWND]struct{})
		nextAds = make(map[windows.HWND]struct{})
		if len(processIDs) != 0 {
			winapi.EnumWindows(enumWindow, 0)
		}
		mutex.Lock()
		// Replace sets so closed windows and reused HWNDs do not accumulate.
		mainWindowHandleMap, adSubwindowCandidateMap = nextMain, nextAds
		mutex.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func removeAd(ctx context.Context) {
	ticker := time.NewTicker(sleepTime)
	defer ticker.Stop()
	defer beta.Restore()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
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
		if winapi.GetWindowText(wnd) == "Chrome Legacy Window" || hasChromeLegacyWindow(inspectChildren(wnd)) {
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
	go watch(ctx)
	go removeAd(ctx)
}
