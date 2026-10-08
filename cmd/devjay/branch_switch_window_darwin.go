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
			parent := m.suggestionsParent
			if viewer := m.viewers[sender]; viewer != nil && viewer.state != nil {
				viewerSite, parent = viewer.state.SiteID, viewer.window
			}
			if r == nil || !r.Ready || parent == nil || m.actionBusy() ||
				!branchPickerSenderAllowed(sender, req.SiteID, m.suggestedSite, viewerSite, m.action) {
				m.actionReply(req.RequestID, "Open branch switching from this Site's Changes window when no other action is running.")
				return
			}
			m.actionReply(req.RequestID, "")
			m.cancelCheckoutAction()
			m.suggestedSite, m.suggestionsParent = req.SiteID, parent
			if sender != "suggested-actions" {
				m.suggestionsAnchor = nil
				if req.Anchor.valid() {
					m.suggestionsAnchor = req.Anchor
				}
			}
			m.suggestions.SetTitle("Switch branch")
			m.suggestions.SetMinSize(560, 700)
			m.suggestions.SetSize(620, 740)
			m.action = &CheckoutAction{SiteID: req.SiteID, Kind: "switch", Status: "loading-branches", Message: "Reading branches…"}
			ctx, cancel := context.WithTimeout(m.app.Context(), 30*time.Second)
			m.actionCancel = cancel
			generation := m.actionGeneration
			if m.suggestionsReady {
				showSuggestionsWindow(m.suggestions, parent, m.suggestionsAnchor)
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
					if generation != m.actionGeneration || m.suggestedSite != req.SiteID || m.runtime[req.SiteID] != r {
						return
					}
					m.action = result
					m.broadcast()
				})
				if err != nil {
					return
				}
				enriched := *result
				enriched.Branches = readBranchPullRequests(ctx, r.Site.Path, catalog.Branches)
				m.send(func() {
					if generation != m.actionGeneration || m.suggestedSite != req.SiteID || m.runtime[req.SiteID] != r || m.action != result {
						return
					}
					m.actionCancel, m.action = nil, &enriched
					m.broadcast()
				})
			}()
		})
	})
}
