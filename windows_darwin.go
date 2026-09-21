package main

/*
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>
unsigned int devjay_window_state(void *window);
int devjay_prepare_panel(void *window);
void devjay_track_hover(void *window);
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

// Collect native state in one main-thread visit instead of one per property.
func readWindowSnapshot(items map[string]siteRuntime, panel, settings *application.WebviewWindow) (map[string]DesktopPosition, map[string]cardState, map[string]bool, bool) {
	positions := map[string]DesktopPosition{}
	states := map[string]cardState{}
	preferences := map[string]bool{"allDesktops": true, "onTop": true}
	settingsOpen := false
	application.InvokeSync(func() {
		for id, item := range items {
			if !item.Ready {
				continue
			}
			pointer := item.Window.NativeWindow()
			flags := uint(C.devjay_window_state(pointer))
			states[id] = cardState{Name: id, OnDesktop: flags&1 != 0, Focused: flags&2 != 0, Minimized: flags&4 != 0}
			raw := C.devjay_desktop_position(pointer)
			if raw != nil {
				var position DesktopPosition
				_ = json.Unmarshal([]byte(C.GoString(raw)), &position)
				C.free(unsafe.Pointer(raw))
				positions[id] = position
			}
		}
		if panel != nil {
			flags := C.devjay_panel_preferences(panel.NativeWindow())
			preferences["allDesktops"], preferences["onTop"] = flags&1 != 0, flags&2 != 0
		}
		if settings != nil {
			settingsOpen = C.devjay_window_state(settings.NativeWindow())&8 != 0
		}
	})
	return positions, states, preferences, settingsOpen
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
	application.InvokeSync(func() {
		C.devjay_restore_layout(window.NativeWindow(), value)
		C.devjay_track_hover(window.NativeWindow())
	})
}

func saveWindowLayout(window *application.WebviewWindow) {
	application.InvokeSync(func() { C.devjay_save_layout(window.NativeWindow()) })
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
