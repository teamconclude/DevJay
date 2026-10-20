package main

/*
#cgo LDFLAGS: -framework AppKit -framework Carbon
#include <stdlib.h>
void devjay_stop_strip_controls(void);
void devjay_set_site_windows_hidden(int hidden);
void devjay_refresh_minimal_strip(void);
int devjay_set_strip_mode(void *window, int mode, const char *cards);
void devjay_update_compact_strip(const char *cards);
*/
import "C"

import (
	"encoding/json"
	"sync/atomic"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type stripMode int

const (
	stripFull stripMode = iota
	stripCompact
	stripHidden
)

type stripControlEvent struct {
	action int
	siteID string
}

// Native callbacks never wait on the manager or AppKit. Key repeats are
// suppressed natively; the bounded queue also bounds an input flood.
var stripControlEvents = make(chan stripControlEvent, 32)

// Coalesce modifier changes without losing a release when the manager is busy.
var pendingShortcutHints atomic.Uint32
var shortcutHintEvents = make(chan struct{}, 1)

func queueShortcutHints(mask uint32) {
	pendingShortcutHints.Store(mask)
	select {
	case shortcutHintEvents <- struct{}{}:
	default:
	}
}

//export devjay_shortcut_hints
func devjay_shortcut_hints(mask C.uint) {
	queueShortcutHints(uint32(mask))
}

func enqueueStripControl(event stripControlEvent) {
	select {
	case stripControlEvents <- event:
	default:
	}
}

//export devjay_strip_control
func devjay_strip_control(action C.int) {
	enqueueStripControl(stripControlEvent{action: int(action)})
}

//export devjay_compact_site
func devjay_compact_site(id *C.char) {
	enqueueStripControl(stripControlEvent{action: 15, siteID: C.GoString(id)})
}

// The native row only needs identity, headings and current-desktop state.
func compactStripCards(sites []SiteView) string {
	type card struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Path   string `json:"path"`
		Active bool   `json:"active"`
	}
	cards := make([]card, 0, len(sites))
	for _, site := range sites {
		cards = append(cards, card{site.ID, site.Name, site.Path, site.Window.OnDesktop})
	}
	data, _ := json.Marshal(cards)
	return string(data)
}

func (m *siteManager) setStripMode(mode stripMode) {
	if !m.stripReady || m.strip == nil || mode < stripFull || mode > stripHidden || m.stripMode == mode {
		return
	}
	cards := ""
	if mode == stripCompact {
		cards = compactStripCards(m.snapshot().Sites)
	}
	value := C.CString(cards)
	defer C.free(unsafe.Pointer(value))
	var applied C.int
	application.InvokeSync(func() { applied = C.devjay_set_strip_mode(m.strip.NativeWindow(), C.int(mode), value) })
	if applied != 0 {
		// Keep Wails' visibility state aligned so reloading a hidden WebView
		// cannot automatically reveal the full strip.
		if mode == stripFull {
			m.strip.Show()
		} else {
			m.strip.Hide()
		}
		if mode == stripHidden {
			m.stripRestoreMode = m.stripMode
		}
		m.stripMode = mode
		m.compactSignature = cards
		m.minimalItem.SetChecked(mode == stripCompact)
		m.hiddenItem.SetLabel(stripVisibilityLabel(mode))
		m.broadcast()
	}
}

func (m *siteManager) updateCompactStrip(sites []SiteView) {
	if m.stripMode != stripCompact {
		return
	}
	cards := compactStripCards(sites)
	if cards == m.compactSignature {
		return
	}
	value := C.CString(cards)
	defer C.free(unsafe.Pointer(value))
	application.InvokeSync(func() { C.devjay_update_compact_strip(value) })
	m.compactSignature = cards
}

func (m *siteManager) showStrip() {
	if !m.stripReady {
		return
	}
	m.setStripMode(stripFull)
	focusSite(m.strip)
	m.broadcast()
}

func (m *siteManager) activateSiteNumber(index int) {
	sites := visibleSites(m.config)
	if index >= 0 && index < len(sites) {
		m.activate(sites[index].ID)
	}
}

func (m *siteManager) handleStripControl(event stripControlEvent) {
	action := event.action
	switch {
	case action == 0:
		if m.stripMode == stripFull {
			m.setStripMode(stripCompact)
		} else {
			m.setStripMode(stripFull)
		}
	case action >= 1 && action <= 9:
		m.activateSiteNumber(action - 1)
	case action == 10:
		m.showStrip()
	case action == 11:
		m.openSettings()
	case action == 12:
		m.setStripMode(stripHidden)
	case action == 13:
		m.setStripMode(stripCompact)
	case action == 14:
		go m.app.Quit()
	case action == 16:
		m.toggleStripVisibility()
	case action == 17:
		m.setSiteWindowsHidden(!m.config.SiteWindowsHidden)
	case action == 18 || action == 19:
		m.setSiteWindowsHidden(action == 18)
	case action == 15 && m.stripMode == stripCompact:
		m.activate(event.siteID) // Revalidate live Site membership, never a stale index.
	}
}

func stopStripControls() {
	application.InvokeSync(func() { C.devjay_stop_strip_controls() })
}

func refreshMinimalStrip() {
	application.InvokeSync(func() { C.devjay_refresh_minimal_strip() })
}

func stripVisibilityLabel(mode stripMode) string {
	if mode == stripHidden {
		return "Show strip"
	}
	return "Hide strip"
}
func restoredStripMode(mode stripMode) stripMode {
	if mode == stripCompact {
		return stripCompact
	}
	return stripFull
}
func (m *siteManager) toggleStripVisibility() {
	if m.stripMode == stripHidden {
		m.setStripMode(restoredStripMode(m.stripRestoreMode))
	} else {
		m.setStripMode(stripHidden)
	}
}
func (m *siteManager) setSiteWindowsHidden(hidden bool) {
	if m.config.SiteWindowsHidden == hidden {
		return
	}
	c := m.config
	c.SiteWindowsHidden = hidden
	if err := m.save(c); err != nil {
		showMessageDialog(m.strip, nil, "Could not change Site window visibility", err.Error())
		return
	}
	m.applySiteWindowsVisibility()
	m.broadcast()
}

// Also run at launch, before ready callbacks, so menus and new windows agree
// with the saved preference. Only explicit Show/Hide actions change it.
func (m *siteManager) applySiteWindowsVisibility() {
	hidden := m.config.SiteWindowsHidden
	var nativeHidden C.int
	if hidden {
		nativeHidden = 1
	}
	application.InvokeSync(func() { C.devjay_set_site_windows_hidden(nativeHidden) })
	for _, site := range m.runtime {
		if site.Ready {
			setSiteWindowVisibility(site.Window, hidden)
		} else if hidden {
			site.Window.Hide()
		}
	}
	if m.siteWindowsItem != nil {
		label := "Hide Site windows"
		if hidden {
			label = "Show Site windows"
		}
		m.siteWindowsItem.SetLabel(label)
	}
}
