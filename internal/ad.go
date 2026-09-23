package internal

import (
	"strings"

	"golang.org/x/sys/windows"

	"kakaotalkadblock/internal/win/winapi"
)

const (
	LayoutShadowPadding = 2
	MainViewPadding     = 31
)

func HideLockScreenAdArea(windowText string, rect *winapi.Rect, handle windows.HWND) {
	if strings.HasPrefix(windowText, "LockModeView") {
		width := rect.Right - rect.Left - LayoutShadowPadding
		height := rect.Bottom - rect.Top
		resizeAdArea(handle, width, height)
	}
}

func HideMainViewAdArea(windowText string, rect *winapi.Rect, handle windows.HWND) {
	if strings.HasPrefix(windowText, "OnlineMainView") {
		width := rect.Right - rect.Left - LayoutShadowPadding
		height := rect.Bottom - rect.Top - MainViewPadding
		if height < 1 {
			return
		}
		resizeAdArea(handle, width, height)
	}
}

func resizeAdArea(handle windows.HWND, width, height int32) {
	if width < 1 || height < 1 {
		return
	}
	var current winapi.Rect
	if !winapi.GetWindowRect(handle, &current) {
		return
	}
	if current.Right-current.Left == width && current.Bottom-current.Top == height {
		return
	}
	winapi.SetWindowPos(handle, 0, 0, 0, width, height, winapi.SwpNomove|winapi.SwpNozorder|winapi.SwpNoactivate)
}
