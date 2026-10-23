package internal

import (
	"strings"
	"sync/atomic"

	"golang.org/x/sys/windows"
	"kakaotalkadblock/internal/win/winapi"
)

type activitySignals struct {
	rescan, work, layout chan struct{}
	qtTargets            chan []uint32
	processes            atomic.Value // Immutable map[uint32]bool; true means Qt6.
	eventDriven          bool
}

func newActivitySignals() *activitySignals {
	s := &activitySignals{rescan: make(chan struct{}, 1), work: make(chan struct{}, 1), layout: make(chan struct{}, 1), qtTargets: make(chan []uint32, 1)}
	s.processes.Store(map[uint32]bool{})
	return s
}

func notify(channel chan struct{}) {
	select {
	case channel <- struct{}{}:
	default:
	}
}

func replaceQtTargets(channel chan []uint32, ids []uint32) {
	select {
	case channel <- ids:
		return
	default:
	}
	select {
	case <-channel:
	default:
	}
	select {
	case channel <- ids:
	default:
	}
}

func (s *activitySignals) onWindowEvent(event uint32, window windows.HWND) {
	// Focus, selection and accessibility state updates do not change ad geometry.
	if event != winapi.EventObjectCreate && event != winapi.EventObjectShow && event != winapi.EventObjectNameChange && event != winapi.EventObjectLocationChange {
		return
	}
	var pid uint32
	winapi.GetWindowThreadProcessId(window, &pid)
	isQt, known := s.processes.Load().(map[uint32]bool)[pid]
	appearance := event == winapi.EventObjectCreate || event == winapi.EventObjectShow || event == winapi.EventObjectNameChange
	if known {
		notify(s.work)
		if appearance {
			notify(s.rescan)
			if isQt {
				notify(s.layout)
			}
		}
	} else if appearance {
		class := winapi.GetClassName(window)
		if strings.HasPrefix(class, "EVA_Window") || strings.HasPrefix(class, "Qt6") && strings.HasSuffix(class, "QWindowIcon") {
			notify(s.rescan)
		}
	}
}
