package main

/*
#cgo LDFLAGS: -framework AppKit
#include "native_monitor_darwin.h"
*/
import "C"

import (
	"sync"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type nativeMonitorFlags uint32

const (
	nativeMonitorSpace        nativeMonitorFlags = C.DevJayMonitorSpace
	nativeMonitorPlacement    nativeMonitorFlags = C.DevJayMonitorPlacement
	nativeMonitorVisibility   nativeMonitorFlags = C.DevJayMonitorVisibility
	nativeMonitorApplications nativeMonitorFlags = C.DevJayMonitorApplications
	nativeMonitorPower        nativeMonitorFlags = C.DevJayMonitorPower
	nativeMonitorDisplays     nativeMonitorFlags = C.DevJayMonitorDisplays
	nativeMonitorInteraction  nativeMonitorFlags = C.DevJayMonitorInteraction
)

var nativeMonitoring struct {
	once  sync.Once
	flags atomic.Uint32
}

var nativeMonitorWake = make(chan struct{}, 1)

func startNativeMonitoring() {
	nativeMonitoring.once.Do(func() {
		application.InvokeSync(func() { C.devjay_start_native_monitor() })
	})
}

func nativeMonitorEvents() <-chan struct{} { return nativeMonitorWake }

func takeNativeMonitorEvents() nativeMonitorFlags {
	return nativeMonitorFlags(nativeMonitoring.flags.Swap(0))
}

// AppKit can deliver several related notifications during a Space transition.
// The callback must never wait for the manager, which may itself be waiting for
// AppKit. Preserve all kinds of change while coalescing their wake-ups.
func signalNativeMonitor(flags nativeMonitorFlags) {
	if flags == 0 {
		return
	}
	nativeMonitoring.flags.Or(uint32(flags))
	select {
	case nativeMonitorWake <- struct{}{}:
	default:
	}
}

//export devjay_native_monitor_changed
func devjay_native_monitor_changed(flags C.uint) {
	signalNativeMonitor(nativeMonitorFlags(flags))
}

// All native window access is batched on AppKit. A nonvisible/off-Space window
// can retain its latest backend snapshot until it becomes visible again.
func readNativeWindowVisibility(windows map[string]*application.WebviewWindow) map[string]bool {
	visible := make(map[string]bool, len(windows))
	application.InvokeSync(func() {
		for id, window := range windows {
			visible[id] = window != nil && C.devjay_monitor_window_visible(window.NativeWindow()) != 0
		}
	})
	return visible
}
