package beta

import (
	"kakaotalkadblock/internal/win/winapi"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
)

const (
	executable            = "kakaotalkui.exe"
	qt6WindowClassPrefix  = "Qt6"
	qtWindowClassSuffix   = "QWindowIcon"
	chromeWidgetWindow    = "Chrome_WidgetWin_1"
	chromeWidgetContainer = "Chrome_WidgetWin_0"
	momentAdWindowTitle   = "MOMENT 광고"
	adfitAdWindowTitle    = "AdFit NAS Advertisement"
	chromeLegacyTitle     = "Chrome Legacy Window"
	qt6AdContainerTitle   = "KakaoTalkUI"
)

var mutex = &sync.Mutex{}
var mainWindowHandleMap = make(map[windows.HWND]struct{})
var ownedAdWindowHandleMap = make(map[windows.HWND]windows.HWND)
var adWindowHandleMap = make(map[windows.HWND]windows.HWND)

func IsExecutable(name string) bool {
	return strings.EqualFold(name, executable)
}

func TrackWindow(handle windows.HWND, className string, parentHandle windows.HWND) {
	if !isQt6WindowClass(className) {
		return
	}
	mutex.Lock()
	defer mutex.Unlock()
	if parentHandle == 0 {
		mainWindowHandleMap[handle] = struct{}{}
	} else if winapi.GetWindowText(handle) == qt6AdContainerTitle {
		ownedAdWindowHandleMap[handle] = parentHandle
	}
}

func RemoveAds() {
	mutex.Lock()
	defer mutex.Unlock()
	for mainWindow := range mainWindowHandleMap {
		if !winapi.IsWindow(mainWindow) {
			delete(mainWindowHandleMap, mainWindow)
			delete(adWindowHandleMap, mainWindow)
			continue
		}
		if winapi.IsWindowVisible(mainWindow) {
			hideAds(mainWindow)
		}
	}
}

func isQt6WindowClass(className string) bool {
	return strings.HasPrefix(className, qt6WindowClassPrefix) && strings.HasSuffix(className, qtWindowClassSuffix)
}

func hideAds(mainWindow windows.HWND) {
	if adWindow, known := adWindowHandleMap[mainWindow]; known {
		if winapi.IsWindow(adWindow) {
			hideAdWindow(adWindow)
			return
		}
		delete(adWindowHandleMap, mainWindow)
	}

	searchRoots := []windows.HWND{mainWindow}
	for handle, owner := range ownedAdWindowHandleMap {
		if !winapi.IsWindow(handle) {
			delete(ownedAdWindowHandleMap, handle)
			continue
		}
		if owner == mainWindow {
			searchRoots = append(searchRoots, handle)
		}
	}
	for _, root := range searchRoots {
		for _, handle := range getChildWindows(root) {
			if winapi.GetClassName(handle) != chromeWidgetWindow {
				continue
			}
			title := winapi.GetWindowText(handle)
			if title != momentAdWindowTitle && title != adfitAdWindowTitle {
				continue
			}
			if !containsChromeLegacyWindow(handle) {
				continue
			}
			chromeHost := winapi.GetParent(handle)
			if winapi.GetClassName(chromeHost) != chromeWidgetContainer {
				continue
			}
			adWindow := winapi.GetParent(chromeHost)
			if !isQt6WindowClass(winapi.GetClassName(adWindow)) || winapi.GetWindowText(adWindow) != qt6AdContainerTitle {
				continue
			}
			if root != mainWindow {
				adWindow = root
			}
			adWindowHandleMap[mainWindow] = adWindow
			hideAdWindow(adWindow)
		}
	}
}

func hideAdWindow(adWindow windows.HWND) {
	// Keep the main window's native frame and complete drawing region.
	// Cutting its region leaves a transparent strip in DWM/Alt+Tab previews.
	if winapi.IsWindowVisible(adWindow) {
		winapi.ShowWindow(adWindow, 0)
	}
}

func containsChromeLegacyWindow(handle windows.HWND) bool {
	for _, child := range getChildWindows(handle) {
		if winapi.GetWindowText(child) == chromeLegacyTitle {
			return true
		}
	}
	return false
}

func getChildWindows(parent windows.HWND) []windows.HWND {
	return winapi.ChildWindows(parent)
}
