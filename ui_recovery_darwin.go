package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdint.h>
#include <stdlib.h>
void devjay_log_ui_recovery(const char *message);
void devjay_ui_power_state(unsigned int *sleepReasons, uint64_t *wakeSerial);
*/
import "C"

import (
	"fmt"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func (m *siteManager) watchView(id string, w *application.WebviewWindow) {
	if m.recoveryWindows == nil {
		m.recoveryWindows = make(map[string]*application.WebviewWindow)
	}
	m.recoveryWindows[id] = w
	m.recovery.add(id)
	w.OnWindowEvent(events.Mac.WebViewWebContentProcessDidTerminate, func(*application.WindowEvent) {
		m.send(func() {
			if m.recoveryWindows[id] != w {
				return
			}
			logUIRecovery("WebContent process terminated for %s", id)
			// Wails reloads automatically. Give that reload time to acknowledge a
			// rendered snapshot before attempting our bounded fallback.
			m.recovery.request(id, time.Now(), false)
		})
	})
}
func (m *siteManager) forgetView(id string) {
	delete(m.recoveryWindows, id)
	delete(m.recovery.views, id)
}
func (m *siteManager) recoverViews() {
	var sleeping C.uint
	var serial C.uint64_t
	application.InvokeSync(func() { C.devjay_ui_power_state(&sleeping, &serial) })
	now := time.Now()
	wasPaused := m.recovery.paused
	oldSerial := m.recovery.wakeSerial
	m.recovery.power(sleeping != 0, uint64(serial), now)
	if wasPaused != m.recovery.paused || oldSerial != uint64(serial) {
		logUIRecovery("suspended=%t, wake=%d", m.recovery.paused, serial)
	}
	reload, failed := m.recovery.tick(now)
	for _, id := range reload {
		if w := m.recoveryWindows[id]; w != nil {
			logUIRecovery("reloading %s (attempt %d)", id, m.recovery.views[id].attempts)
			w.Reload()
		}
	}
	for _, id := range failed {
		logUIRecovery("no rendered snapshot from %s; use View > Reload windows to retry", id)
	}
}
func (m *siteManager) registerRecoveryEvents() {
	m.app.Event.On("devjay:ui-ready", func(e *application.CustomEvent) {
		document, ok := e.Data.(string)
		if !ok || len(document) > 100 {
			return
		}
		id := e.Sender
		m.send(func() {
			if m.recovery.ready(id, document) {
				logUIRecovery("%s rendered fresh state", id)
			}
		})
	})
}

// Use macOS unified logging even when Finder launched the app without stderr.
func logUIRecovery(format string, args ...any) {
	message := C.CString(fmt.Sprintf(format, args...))
	defer C.free(unsafe.Pointer(message))
	C.devjay_log_ui_recovery(message)
}
