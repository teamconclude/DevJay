package main

/*
#cgo LDFLAGS: -framework AppKit -framework WebKit
#include <stdlib.h>
int devjay_card_menu(void *window, double x, double y, const char *first, const char *second, const char *settings, const char *quit, const char *rebase, const char *rebaseDisabled);
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Run off the manager queue: AppKit tracks the menu synchronously until it is
// dismissed. A per-popup result avoids mutable context-menu callback targets.
func showCardMenu(window *application.WebviewWindow, req cardMenuRequest, rebase *cardGitOption) cardMenuAction {
	labels := cardMenuLabels(req)
	rebaseTitle, rebaseReason := "", ""
	if rebase != nil {
		rebaseTitle, rebaseReason = "Rebase and push…", rebase.DisabledReason
		if rebase.Kind == "publish" {
			rebaseTitle = "Push rebased branch…"
		}
	}
	rebaseLabel, disabled := C.CString(rebaseTitle), C.CString(rebaseReason)
	defer C.free(unsafe.Pointer(rebaseLabel))
	defer C.free(unsafe.Pointer(disabled))
	first, second, settings := C.CString(labels[0]), C.CString(labels[1]), C.CString(labels[2])
	quit := C.CString(labels[3])
	defer C.free(unsafe.Pointer(first))
	defer C.free(unsafe.Pointer(second))
	defer C.free(unsafe.Pointer(settings))
	defer C.free(unsafe.Pointer(quit))
	var selected C.int
	application.InvokeSync(func() {
		selected = C.devjay_card_menu(window.NativeWindow(), C.double(req.X), C.double(req.Y), first, second, settings, quit, rebaseLabel, disabled)
	})
	return cardMenuAction(selected)
}

// Only card windows may operate on a Site. Return the exact window/runtime so
// the caller can reject a selection if either was replaced while the menu ran.
func (m *siteManager) cardMenuSource(req cardMenuRequest, sender string) (*application.WebviewWindow, *siteRuntime) {
	site := m.runtime[req.SiteID]
	if site == nil || !site.Ready {
		return nil, nil
	}
	if sender == "control-panel" && m.panelReady && m.panel != nil {
		return m.panel, site
	}
	if sender == "site-"+req.SiteID && site.Window != nil {
		return site.Window, site
	}
	return nil, nil
}

func (m *siteManager) registerCardMenuEvents() {
	// Owned by the manager queue. Do not queue multiple native tracking loops.
	menuOpen := false
	m.app.Event.On("devjay:card-menu", func(e *application.CustomEvent) {
		var req cardMenuRequest
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		var valid bool
		if req, valid = normalizeCardMenuRequest(req); !valid {
			return
		}
		sender := e.Sender
		m.send(func() {
			if menuOpen {
				return
			}
			parent, site := m.cardMenuSource(req, sender)
			if parent == nil {
				return
			}
			pr, knownPR := requestedPR(site.Git, req.Number)
			if req.Number > 0 && !knownPR {
				return
			}
			req = resolveCardMenuRequest(req, site.Git)
			rebase := makeCardGitOption(req, site.Git)
			if rebase != nil && (m.actionBusy() || (m.action != nil && m.action.Status == "preparing")) {
				rebase.DisabledReason = "Another Git action is running. Wait for it to finish."
			}
			if req.BundleID != "" {
				app, ok := m.requestedDevelopmentApp(req.SiteID, req.BundleID)
				if !ok {
					return
				}
				req.appName = app.Name
			}
			menuOpen = true
			go func() {
				selected := showCardMenu(parent, req, rebase)
				m.send(func() {
					menuOpen = false
					currentParent, currentSite := m.cardMenuSource(req, sender)
					if currentParent != parent || currentSite != site {
						return
					}
					if selected == cardMenuGitAction {
						if action, ok := rebase.request(req.SiteID, site.Git); ok {
							action.Anchor = req.Anchor
							m.prepareAction(action, site, parent)
						}
						return
					}
					if !validCardMenuSelection(req, selected, pr, site.Git) {
						return
					}
					switch selected {
					case cardMenuView, cardMenuViewNew:
						if req.Number > 0 {
							m.openPR(prOpenRequest{SiteID: req.SiteID, Number: req.Number, NewTab: selected == cardMenuViewNew}, sender)
						} else if req.BundleID != "" {
							m.activateDevelopmentApp(appActivateRequest{SiteID: req.SiteID, BundleID: req.BundleID}, sender)
						} else if req.Kind == "site" {
							m.activate(req.SiteID)
						} else {
							m.openViewer(viewerRequest{SiteID: req.SiteID, Kind: req.Kind, NewWindow: selected == cardMenuViewNew, Anchor: req.Anchor}, parent, site)
						}
					case cardMenuSettings:
						m.openSettings()
					case cardMenuQuit:
						// Keep the manager queue available while the normal quit
						// lifecycle drains confirmed actions and saves window layout.
						go m.app.Quit()
					}
				})
			}()
		})
	})
}
