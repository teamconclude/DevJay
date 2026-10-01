package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) actionBusy() bool { return m.action != nil && m.action.Status == "running" }

func (m *siteManager) cancelCheckoutAction() {
	if m.actionCancel != nil {
		m.actionCancel()
		m.actionCancel = nil
	}
	m.actionGeneration++
	m.action = nil
}

func (m *siteManager) actionReply(requestID, message string) {
	m.app.Event.Emit("devjay:request-result", struct {
		RequestID string `json:"requestID"`
		Error     string `json:"error"`
	}{requestID, message})
}

// Site buttons may prepare only their own currently available rebased push.
// All other actions retain their existing viewer/confirmation authorization.
func (m *siteManager) sitePublishSource(req ActionRequest, sender string) *application.WebviewWindow {
	if sender != "site-"+req.SiteID || req.Kind != "publish" || m.actionBusy() || (m.action != nil && m.action.Status == "preparing") {
		return nil
	}
	parent, site := m.cardMenuSource(cardMenuRequest{SiteID: req.SiteID}, sender)
	if parent == nil || req.Head != site.Git.Head || req.Branch != site.Git.Branch {
		return nil
	}
	option := makeCardGitOption(cardMenuRequest{Kind: "branch", PublishRebased: true}, site.Git)
	if option == nil || option.DisabledReason != "" {
		return nil
	}
	return parent
}

// Caller authorizes the originating window and action before entering this
// shared manager-queue path. It only prepares a plan; confirmation is separate.
func (m *siteManager) prepareAction(req ActionRequest, r *siteRuntime, parent *application.WebviewWindow) {
	if r == nil || m.runtime[req.SiteID] != r || !r.Ready || m.actionBusy() {
		m.actionReply(req.RequestID, "The Site is not ready or another action is running. Refresh and try again.")
		return
	}
	title := map[string]string{"switch": "Confirm branch switch", "replace": "Confirm branch replacement", "push": "Confirm push", "publish": "Confirm rebased push", "update": "Confirm branch update", "rebase": "Confirm rebase", "rebase-push": "Rebase and push", "merged": "Confirm return to default branch"}[req.Kind]
	if title == "" {
		m.actionReply(req.RequestID, "Unknown action.")
		return
	}
	m.suggestedSite = req.SiteID
	m.suggestionsParent = parent
	if req.Anchor.valid() {
		m.suggestionsAnchor = req.Anchor
	} else {
		m.suggestionsAnchor = nil
	}
	m.actionReply(req.RequestID, "")
	if req.Kind != "switch" {
		m.suggestions.SetMinSize(400, 340)
		height := 470
		if isPublishAction(req.Kind) {
			m.suggestions.SetMinSize(400, 300)
			height = 330
		}
		m.suggestions.SetSize(460, height)
	}
	m.suggestions.SetTitle(title)
	m.cancelCheckoutAction()
	ctx, cancel := context.WithTimeout(m.app.Context(), 2*time.Minute)
	m.actionCancel = cancel
	generation := m.actionGeneration
	merged := r.Git.MergedPR
	m.action = &CheckoutAction{SiteID: req.SiteID, Kind: req.Kind, Status: "preparing", Message: "Checking the checkout and fetching the remote…"}
	if m.suggestionsReady {
		showSuggestionsWindow(m.suggestions, m.suggestionsParent, m.suggestionsAnchor)
	}
	m.broadcast()
	go func() {
		defer cancel()
		plan, err := prepareCheckoutAction(ctx, r.Site.Path, req, merged)
		result := &CheckoutAction{SiteID: req.SiteID, Kind: req.Kind, Status: "ready", Plan: plan}
		if err != nil {
			result.Status, result.Message = "failed", err.Error()
			var rebaseErr *rebaseActionError
			if errors.As(err, &rebaseErr) {
				result.Files = rebaseErr.check.Files
			}
		}
		m.send(func() {
			if generation != m.actionGeneration || m.suggestedSite != req.SiteID || m.runtime[req.SiteID] != r {
				return
			}
			m.actionCancel = nil
			m.action = result
			m.broadcast()
		})
	}()
}

func (m *siteManager) registerCheckoutActions() {
	m.app.Event.On("devjay:action-prepare", func(e *application.CustomEvent) {
		sender := e.Sender
		data, err := json.Marshal(e.Data)
		var req ActionRequest
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			r := m.runtime[req.SiteID]
			if r == nil || !r.Ready || m.actionBusy() {
				m.actionReply(req.RequestID, "The Site is not ready or another action is running. Refresh and try again.")
				return
			}
			if req.Kind == "switch" && !branchSwitchPreparationAllowed(sender, req.SiteID, m.suggestedSite, m.action) {
				m.actionReply(req.RequestID, "Choose a branch from this Site's branch picker before continuing.")
				return
			}
			parent := m.suggestionsParent
			if sender == "suggested-actions" {
				if req.SiteID != m.suggestedSite {
					m.actionReply(req.RequestID, "The selected Site changed. Open the action again.")
					return
				}
			} else if sender == "site-"+req.SiteID {
				parent = m.sitePublishSource(req, sender)
				if parent == nil {
					m.actionReply(req.RequestID, "This rebased push is no longer available. Refresh the Site and review its current state.")
					return
				}
			} else {
				viewer := m.viewers[sender]
				if viewer == nil || viewer.state.SiteID != req.SiteID || !hasSuggestedActions(r.Git) {
					m.actionReply(req.RequestID, "This action is no longer available. Refresh the Changes window.")
					return
				}
				// Bind the selected action to this viewer’s Site. Preparation
				// validates the requested branch and commit against live Git.
				parent = viewer.window
			}
			m.prepareAction(req, r, parent)
		})
	})
	m.app.Event.On("devjay:action-confirm", func(e *application.CustomEvent) {
		if e.Sender != "suggested-actions" {
			return
		}
		var req struct {
			Token     string `json:"token"`
			RequestID string `json:"requestID"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			if m.action == nil || m.action.Status != "ready" || m.action.Plan == nil || req.Token != m.action.Plan.ID {
				m.actionReply(req.RequestID, "The confirmation is no longer ready. Review the current action.")
				return
			}
			id, plan := m.action.SiteID, m.action.Plan
			r := m.runtime[id]
			if r == nil || id != m.suggestedSite {
				m.actionReply(req.RequestID, "The Site changed. Open the action again.")
				return
			}
			// Consume the token before starting. Duplicate clicks cannot execute twice.
			if !m.lifecycle.begin() {
				m.actionReply(req.RequestID, "DevJay is quitting. The action was not started.")
				return
			}
			m.actionReply(req.RequestID, "")
			m.action = &CheckoutAction{SiteID: id, Kind: plan.Kind, Status: "running", Message: "Applying the confirmed action. Please wait…"}
			m.broadcast()
			go func() {
				defer m.lifecycle.finish()
				// Network commands are bounded individually. Local mutations must finish.
				result := applyCheckoutAction(context.Background(), plan)
				result.SiteID = id
				m.send(func() {
					m.action = &result
					if current := m.runtime[id]; current == r && current.GitControl != nil {
						current.GitControl.refresh()
					}
					m.broadcast()
				})
			}()
		})
	})
}
