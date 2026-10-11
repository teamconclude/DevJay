package main

/*
#cgo LDFLAGS: -framework AppKit -framework Carbon
#include <stdlib.h>
unsigned int devjay_start_shortcuts(void);
void devjay_stop_strip_controls(void);
void devjay_refresh_minimal_strip(void);
int devjay_set_strip_mode(void *window, int mode, const char *cards);
void devjay_update_compact_strip(const char *cards);
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"strings"
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

func startGlobalShortcuts() string {
	var failed C.uint
	application.InvokeSync(func() { failed = C.devjay_start_shortcuts() })
	return shortcutFailureMessage(uint32(failed))
}

func shortcutFailureMessage(failed uint32) string {
	var keys []string
	for id := 0; id <= 10; id++ {
		if failed&(1<<id) == 0 {
			continue
		}
		key := fmt.Sprint(id)
		if id == 0 {
			key = "M"
		} else if id == 10 {
			key = "0"
		}
		keys = append(keys, "⌃⌥⌘"+key)
	}
	if len(keys) == 0 {
		return ""
	}
	return "Global shortcuts unavailable: " + strings.Join(keys, ", ") + ". Another app or DevJay copy may be using them. Quit that copy or free those shortcuts, then restart DevJay."
}

func (m *siteManager) setStripMode(mode stripMode) {
	if !m.panelReady || m.panel == nil || mode < stripFull || mode > stripHidden || m.stripMode == mode {
		return
	}
	cards := ""
	if mode != stripFull {
		cards = compactStripCards(m.snapshot().Sites)
	}
	value := C.CString(cards)
	defer C.free(unsafe.Pointer(value))
	var applied C.int
	application.InvokeSync(func() { applied = C.devjay_set_strip_mode(m.panel.NativeWindow(), C.int(mode), value) })
	if applied != 0 {
		m.stripMode = mode
		m.compactSignature = cards
		m.minimalItem.SetChecked(mode == stripCompact)
		m.hiddenItem.SetChecked(mode == stripHidden)
		m.broadcast()
	}
}

func (m *siteManager) updateCompactStrip(sites []SiteView) {
	if m.stripMode == stripFull {
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

func (m *siteManager) showControlPanel() {
	if !m.panelReady {
		return
	}
	m.setStripMode(stripFull)
	focusSite(m.panel)
	m.broadcast()
}

func (m *siteManager) activateSiteNumber(index int) {
	sites := orderedSites(m.config)
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
		m.showControlPanel()
	case action == 11:
		m.openSettings()
	case action == 12:
		m.setStripMode(stripHidden)
	case action == 13:
		m.setStripMode(stripCompact)
	case action == 14:
		go m.app.Quit()
	case action == 15 && m.stripMode != stripFull:
		m.activate(event.siteID) // Revalidate live Site membership, never a stale index.
	}
}

func stopStripControls() {
	application.InvokeSync(func() { C.devjay_stop_strip_controls() })
}

func refreshMinimalStrip() {
	application.InvokeSync(func() { C.devjay_refresh_minimal_strip() })
}
