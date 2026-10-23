package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Each Site owns a modeless dialog. All fields are confined to the manager queue.
type actionDialog struct {
	id, siteID      string
	window, parent  *application.WebviewWindow
	anchor          *cardAnchor
	ready           bool
	removeCloseHook func()
	action          *CheckoutAction
	cancel          context.CancelFunc
	generation      uint64
}

func (d *actionDialog) running() bool {
	return d != nil && d.action != nil && d.action.Status == "running"
}

func (d *actionDialog) busy() bool {
	return d != nil && d.action != nil && (d.running() || d.action.Status == "preparing" || d.action.Status == "loading-branches")
}

func (d *actionDialog) cancelPreparation() {
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.generation++
	d.action = nil
}

func (m *siteManager) actionBusy() bool {
	for _, d := range m.actionDialogs {
		if d.running() {
			return true
		}
	}
	return false
}

func (m *siteManager) siteActionDialog(siteID string) *actionDialog {
	for _, d := range m.actionDialogs {
		if d.siteID == siteID {
			return d
		}
	}
	return nil
}

func (m *siteManager) siteActionBusy(siteID string) bool {
	return m.siteActionDialog(siteID).busy()
}

func (m *siteManager) currentActionDialog(d *actionDialog, generation uint64, r *siteRuntime) bool {
	return d != nil && m.actionDialogs[d.id] == d && d.generation == generation && m.runtime[d.siteID] == r
}

func (m *siteManager) readyActionDialog(sender, token string) *actionDialog {
	d := m.actionDialogs[sender]
	if d == nil || d.action == nil || d.action.SiteID != d.siteID || d.action.Status != "ready" ||
		d.action.Plan == nil || token == "" || token != d.action.Plan.ID {
		return nil
	}
	return d
}

func (m *siteManager) actionSnapshot(snapshot appSnapshot, id, viewerSite string) appSnapshot {
	snapshot.Action, snapshot.SuggestedSite = nil, ""
	snapshot.ActionGeneration = 0
	d := m.actionDialogs[id]
	if d != nil {
		snapshot.SuggestedSite = d.siteID
		snapshot.ActionGeneration = d.generation
		snapshot.Sites = snapshotSitesForID(snapshot, d.siteID)
	} else {
		if strings.HasPrefix(id, "site-") {
			viewerSite = strings.TrimPrefix(id, "site-")
		}
		if viewerSite != "" {
			d = m.siteActionDialog(viewerSite)
		}
	}
	if d != nil {
		snapshot.Action = d.action
	}
	return snapshot
}

func (m *siteManager) registerSuggestionsEvents() {
	m.registerCheckoutActions()
	m.registerBranchPicker()
	m.app.Event.On("devjay:suggestions-close", func(e *application.CustomEvent) {
		sender := e.Sender
		var req struct {
			Generation uint64 `json:"generation"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			m.dismissAction(sender, req.Generation)
		})
	})
}

func (m *siteManager) ensureActionDialog(siteID string) *actionDialog {
	if d := m.siteActionDialog(siteID); d != nil {
		return d
	}
	m.actionDialogSequence++
	id := fmt.Sprintf("suggested-actions-%d", m.actionDialogSequence)
	w := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: id, Title: "Confirm action", URL: "/?actions",
		Width: 460, Height: 470, MinWidth: 400, MinHeight: 340, Hidden: true, Frameless: true,
		MaximiseButtonState: application.ButtonDisabled,
		BackgroundColour:    application.NewRGB(20, 24, 32),
		Mac:                 application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	d := &actionDialog{id: id, siteID: siteID, window: w}
	if m.actionDialogs == nil {
		m.actionDialogs = make(map[string]*actionDialog)
	}
	m.actionDialogs[id] = d
	m.watchView(id, w)
	w.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		m.send(func() {
			if m.actionDialogs[id] != d {
				return
			}
			first := !d.ready
			d.ready = true
			if first && d.parent != nil {
				showSuggestionsWindow(w, d.parent, d.anchor)
			}
			m.broadcast()
		})
	})
	d.removeCloseHook = w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if m.app.Context().Err() != nil {
			return
		}
		e.Cancel()
		m.send(func() { m.closeActionDialog(d) })
	})
	return d
}

func (m *siteManager) dismissAction(sender string, generation uint64) {
	if d := m.actionDialogs[sender]; d != nil && generation == d.generation {
		m.closeActionDialog(d)
	}
}

func (m *siteManager) closeActionDialog(d *actionDialog) {
	if d == nil || m.actionDialogs[d.id] != d || d.running() {
		return
	}
	d.cancelPreparation()
	delete(m.actionDialogs, d.id)
	m.forgetView(d.id)
	if d.removeCloseHook != nil {
		d.removeCloseHook()
	}
	d.window.Close()
	m.broadcast()
}

func (m *siteManager) closeSiteAction(siteID string) {
	m.closeActionDialog(m.siteActionDialog(siteID))
}

func (m *siteManager) closeSuggestions() {
	for _, d := range m.actionDialogs {
		m.closeActionDialog(d)
	}
}
