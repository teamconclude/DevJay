package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
unsigned int devjay_window_state(void *window);
int devjay_prepare_panel(void *window);
unsigned int devjay_panel_preferences(void *window);
void devjay_all_desktops(void *window, int enabled);
void devjay_all_windows_on_top(int enabled);
void devjay_focus_site(void *window);
void devjay_restore_layout(void *window, const char *identifier);
void devjay_save_layout(void *window);
char *devjay_desktop_position(void *window);
void devjay_show_settings(void *window, void *parent);
void devjay_apply_top_preference(void);
void devjay_fit_panel(void *window, int mode, int count, int header, double extentWidth, double extentHeight);
*/
import "C"

import (
	"encoding/json"
	"github.com/wailsapp/wails/v3/pkg/application"
	"unsafe"
)

func windowState(window *application.WebviewWindow, name string) cardState {
	var flags uint
	application.InvokeSync(func() { flags = uint(C.devjay_window_state(window.NativeWindow())) })
	return cardState{Name: name, OnDesktop: flags&1 != 0, Focused: flags&2 != 0, Minimized: flags&4 != 0}
}

func prepareControlPanel(window *application.WebviewWindow) bool {
	var ready bool
	application.InvokeSync(func() { ready = C.devjay_prepare_panel(window.NativeWindow()) != 0 })
	return ready
}

func panelPreferences(window *application.WebviewWindow) map[string]bool {
	var flags C.uint
	application.InvokeSync(func() { flags = C.devjay_panel_preferences(window.NativeWindow()) })
	return map[string]bool{"allDesktops": flags&1 != 0, "onTop": flags&2 != 0}
}

func setPanelPreference(window *application.WebviewWindow, name string, enabled bool) {
	var value C.int
	if enabled {
		value = 1
	}
	application.InvokeSync(func() {
		switch name {
		case "allDesktops":
			C.devjay_all_desktops(window.NativeWindow(), value)
		case "onTop":
			C.devjay_all_windows_on_top(value)
		}
	})
}

func focusSite(window *application.WebviewWindow) {
	application.InvokeSync(func() { C.devjay_focus_site(window.NativeWindow()) })
}

func restoreWindowLayout(window *application.WebviewWindow, identifier string) {
	value := C.CString(identifier)
	defer C.free(unsafe.Pointer(value))
	application.InvokeSync(func() { C.devjay_restore_layout(window.NativeWindow(), value) })
}

func saveWindowLayout(window *application.WebviewWindow) {
	application.InvokeSync(func() { C.devjay_save_layout(window.NativeWindow()) })
}

func desktopPosition(window *application.WebviewWindow) DesktopPosition {
	var result DesktopPosition
	application.InvokeSync(func() {
		raw := C.devjay_desktop_position(window.NativeWindow())
		if raw != nil {
			defer C.free(unsafe.Pointer(raw))
			_ = json.Unmarshal([]byte(C.GoString(raw)), &result)
		}
	})
	return result
}
func showSettingsWindow(window, parent *application.WebviewWindow) {
	application.InvokeSync(func() { C.devjay_show_settings(window.NativeWindow(), parent.NativeWindow()) })
}
func applyTopPreference() { application.InvokeSync(func() { C.devjay_apply_top_preference() }) }

func fitControlPanel(window *application.WebviewWindow, config SiteConfig) {
	mode := 0
	if config.layout() == "vertical" {
		mode = 1
	}
	if config.layout() == "custom" {
		mode = 2
	}
	header := 0
	if config.decoration() != "none" {
		header = 22
	}
	width, height := 0, 0
	if mode == 2 && len(config.Placements) > 0 {
		for i, site := range orderedSites(config, nil) {
			p, ok := config.Placements[site.ID]
			if !ok {
				p = CardPlacement{X: (i % 3) * 224, Y: (i / 3) * 160}
			}
			width = max(width, p.X+216)
			height = max(height, p.Y+152)
		}
	}
	application.InvokeSync(func() {
		C.devjay_fit_panel(window.NativeWindow(), C.int(mode), C.int(len(config.Sites)), C.int(header), C.double(width), C.double(height))
	})
}
