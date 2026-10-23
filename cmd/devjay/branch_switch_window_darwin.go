package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) registerBranchPicker() {
	m.app.Event.On("devjay:branches-open", func(e *application.CustomEvent) {
		sender := e.Sender
		var req struct {
			Anchor    *cardAnchor `json:"anchor,omitempty"`
			SiteID    string      `json:"siteID"`
			RequestID string      `json:"requestID"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			r := m.runtime[req.SiteID]
			viewerSite := ""
			d := m.actionDialogs[sender]
			var parent *application.WebviewWindow
			if d != nil {
				parent = d.parent
			}
			if viewer := m.viewers[sender]; viewer != nil && viewer.state != nil {
				viewerSite, parent = viewer.state.SiteID, viewer.window
			}
			if r == nil || !r.Ready || parent == nil || m.siteActionBusy(req.SiteID) ||
				!branchPickerSenderAllowed(d, req.SiteID, viewerSite) {
				m.actionReply(req.RequestID, "Open branch switching from this Site's Changes window when no action is running for this Site.")
				return
			}
			m.actionReply(req.RequestID, "")
			fromDialog := d != nil
			d = m.ensureActionDialog(req.SiteID)
			d.cancelPreparation()
			d.parent = parent
			if !fromDialog {
				d.anchor = nil
				if req.Anchor.valid() {
					d.anchor = req.Anchor
				}
			}
			d.window.SetTitle("Switch branch")
			d.window.SetMinSize(560, 700)
			d.window.SetSize(620, 740)
			d.action = &CheckoutAction{SiteID: req.SiteID, Kind: "switch", Status: "loading-branches", Message: "Reading branches…"}
			ctx, cancel := context.WithTimeout(m.app.Context(), 30*time.Second)
			d.cancel = cancel
			generation := d.generation
			if d.ready {
				showSuggestionsWindow(d.window, parent, d.anchor)
			}
			m.broadcast()
			go func() {
				defer cancel()
				catalog, err := readSwitchBranches(ctx, r.Site.Path)
				result := &CheckoutAction{SiteID: req.SiteID, Kind: "switch", Status: "choosing",
					CurrentBranch: catalog.Branch, CurrentHead: catalog.Head, Branches: catalog.Branches, Message: catalog.Message}
				if err != nil {
					result.Status, result.Message = "failed", err.Error()
				}
				m.send(func() {
					if !m.currentActionDialog(d, generation, r) {
						return
					}
					d.action = result
					m.broadcast()
				})
				if err != nil {
					return
				}
				enriched := *result
				enriched.Branches = readBranchPullRequests(ctx, r.Site.Path, catalog.Branches)
				m.send(func() {
					if !m.currentActionDialog(d, generation, r) || d.action != result {
						return
					}
					d.cancel, d.action = nil, &enriched
					m.broadcast()
				})
			}()
		})
	})
}
