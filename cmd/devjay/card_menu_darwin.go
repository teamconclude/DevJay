package main

/*
#cgo LDFLAGS: -framework AppKit -framework WebKit
#include <stdlib.h>
int devjay_card_menu(void *window, double x, double y, const char *itemsJSON);
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Run off the manager queue: AppKit tracks the menu synchronously until it is
// dismissed. A per-popup result avoids mutable context-menu callback targets.
func showCardMenu(window *application.WebviewWindow, req cardMenuRequest, items []cardMenuItem) cardMenuAction {
	data, err := json.Marshal(items)
	if err != nil {
		return cardMenuDismissed
	}
	encoded := C.CString(string(data))
	defer C.free(unsafe.Pointer(encoded))
	var selected C.int
	application.InvokeSync(func() {
		selected = C.devjay_card_menu(window.NativeWindow(), C.double(req.X), C.double(req.Y), encoded)
	})
	return cardMenuAction(selected)
}

// Only card windows may operate on a Site. Return the exact window/runtime so
// the caller can reject a selection if either was replaced while the menu ran.
func (m *siteManager) cardMenuSource(req cardMenuRequest, sender string) (*application.WebviewWindow, *siteRuntime) {
	if req.Kind == "strip" {
		if sender == "strip" && m.stripReady {
			return m.strip, nil
		}
		return nil, nil
	}
	site := m.runtime[req.SiteID]
	if site == nil || !site.Ready {
		return nil, nil
	}
	if sender == "strip" && m.stripReady && m.strip != nil {
		return m.strip, site
	}
	if sender == "site-"+req.SiteID && site.Window != nil {
		return site.Window, site
	}
	return nil, nil
}

func (m *siteManager) registerCardMenuEvents() {
	// Owned by the manager queue. Do not queue multiple native tracking loops.
	menuOpen := false
	register := func(eventName string, defaultAction bool) {
		m.app.Event.On(eventName, func(e *application.CustomEvent) {
			var req cardMenuRequest
			data, err := json.Marshal(e.Data)
			if err != nil || json.Unmarshal(data, &req) != nil {
				return
			}
			var valid bool
			if req, valid = normalizeCardMenuRequest(req); !valid {
				if defaultAction {
					m.actionReply(req.RequestID, "Invalid card action. Refresh the Site and try again.")
				}
				return
			}
			sender := e.Sender
			m.send(func() {
				replyError := ""
				if defaultAction {
					defer func() { m.actionReply(req.RequestID, replyError) }()
				}
				if menuOpen {
					return
				}
				parent, site := m.cardMenuSource(req, sender)
				if parent == nil {
					return
				}
				if defaultAction && (site == nil || req.Branch != site.Git.Branch || req.Head != site.Git.Head) {
					replyError = "The branch changed. Refresh the Site and try again."
					return
				}
				status := func() GitStatus {
					if site == nil { // Strip background.
						return GitStatus{}
					}
					return site.Git
				}
				git := status()
				pr, knownPR := requestedPR(git, req.Number)
				if req.Number > 0 && !knownPR {
					return
				}
				option := makeCardGitOption(req, git)
				if option != nil && m.siteActionBusy(req.SiteID) {
					option.DisabledReason = "A Git action is already running for this Site. Wait for it to finish."
				}
				if req.BundleID != "" {
					app, ok := m.requestedDevelopmentApp(req.SiteID, req.BundleID)
					if !ok {
						return
					}
					req.appName = app.Name
				}
				actions := cardMenuActions(req, git, option)
				if m.siteActionBusy(req.SiteID) {
					for i := range actions {
						if actions[i].Action == cardMenuSwitchBranch {
							actions[i].Disabled = true
							actions[i].Hint = "A Git action is already running for this Site. Wait for it to finish."
						}
					}
				}
				items := withCardMenuControls(actions, sender == "strip", m.config.SiteWindowsHidden)
				applySelection := func(selected cardMenuAction) {
					currentParent, currentSite := m.cardMenuSource(req, sender)
					if currentParent != parent || currentSite != site || !validCardMenuSelection(items, req, selected, pr, status()) {
						return
					}
					switch selected {
					case cardMenuMinimize:
						m.setStripMode(stripCompact)
					case cardMenuHide:
						m.setStripMode(stripHidden)
					case cardMenuHideSites, cardMenuShowSites:
						m.setSiteWindowsHidden(selected == cardMenuHideSites)
					case cardMenuGitAction, cardMenuMergePR:
						if m.siteActionBusy(req.SiteID) {
							return
						}
						if action, ok := option.request(req.SiteID, site.Git); ok {
							if selected == cardMenuMergePR {
								if option.Kind != "approved" || req.Kind != "pr-status" {
									return
								}
								action.Kind = "merge-pr"
							}
							action.Anchor = req.Anchor
							m.prepareAction(action, site, parent)
						}
					case cardMenuViewDiffs:
						m.openViewer(viewerRequest{SiteID: req.SiteID, Kind: "review", Anchor: req.Anchor}, parent, site)
					case cardMenuOpenPR, cardMenuOpenPRNew:
						m.openPR(prOpenRequest{SiteID: req.SiteID, Number: req.Number, NewTab: selected == cardMenuOpenPRNew, Anchor: req.Anchor}, sender)
					case cardMenuShowApp:
						m.activateDevelopmentApp(appActivateRequest{SiteID: req.SiteID, BundleID: req.BundleID}, sender)
					case cardMenuGoToSite:
						m.activate(req.SiteID)
					case cardMenuView:
						m.openViewer(viewerRequest{SiteID: req.SiteID, Kind: cardMenuViewKind(req.Kind, site.Git), Anchor: req.Anchor}, parent, site)
					case cardMenuSwitchBranch:
						m.openBranchPicker(req.SiteID, site, parent, req.Anchor)
					case cardMenuSettings:
						m.openSettings()
					case cardMenuQuit:
						// Keep the manager queue available while the normal quit
						// lifecycle drains confirmed actions and saves window layout.
						go m.app.Quit()
					}
				}
				if defaultAction {
					if len(actions) == 0 {
						return
					}
					first := actions[0]
					if first.Disabled {
						replyError = first.Hint
						return
					}
					applySelection(first.Action)
					return
				}
				menuOpen = true
				go func() {
					selected := showCardMenu(parent, req, items)
					m.send(func() {
						menuOpen = false
						applySelection(selected)
					})
				}()
			})
		})
	}
	register("devjay:card-menu", false)
	register("devjay:card-default", true)
}
