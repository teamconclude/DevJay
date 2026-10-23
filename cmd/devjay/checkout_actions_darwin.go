package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) actionReply(requestID, message string) {
	m.app.Event.Emit("devjay:request-result", struct {
		RequestID string `json:"requestID"`
		Error     string `json:"error"`
	}{requestID, message})
}

// Site buttons may prepare only their own currently available rebased push.
// All other actions retain their existing viewer/confirmation authorization.
func (m *siteManager) sitePublishSource(req ActionRequest, sender string) *application.WebviewWindow {
	if sender != "site-"+req.SiteID || req.Kind != "publish" || m.siteActionBusy(req.SiteID) {
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
	if r == nil || m.runtime[req.SiteID] != r || !r.Ready || m.siteActionBusy(req.SiteID) {
		m.actionReply(req.RequestID, "The Site is not ready or an action is already running for this Site. Refresh and try again.")
		return
	}
	title := map[string]string{"switch": "Confirm branch switch", "replace": "Confirm branch replacement", "push": "Confirm push", "publish": "Confirm rebased push", "update": "Confirm branch update", "rebase": "Confirm rebase", "rebase-push": "Rebase and push", "merged": "Confirm return to default branch"}[req.Kind]
	if title == "" {
		m.actionReply(req.RequestID, "Unknown action.")
		return
	}
	d := m.ensureActionDialog(req.SiteID)
	// Re-preparing within a dialog keeps its original parent/control anchor.
	if req.Anchor != nil && req.Anchor.valid() {
		d.anchor = req.Anchor
	} else if d.siteID != req.SiteID || d.parent != parent || d.action == nil || d.action.Kind != req.Kind {
		d.anchor = nil
	}
	d.parent = parent
	m.actionReply(req.RequestID, "")
	if req.Kind != "switch" {
		d.window.SetMinSize(400, 340)
		height := 470
		if isPublishAction(req.Kind) {
			d.window.SetMinSize(400, 300)
			height = 330
		}
		d.window.SetSize(460, height)
	}
	d.window.SetTitle(title)
	d.cancelPreparation()
	ctx, cancel := context.WithTimeout(m.app.Context(), 2*time.Minute)
	d.cancel = cancel
	generation := d.generation
	merged := r.Git.MergedPR
	d.action = &CheckoutAction{SiteID: req.SiteID, Kind: req.Kind, Status: "preparing", Message: "Checking the checkout and fetching the remote…"}
	if d.ready {
		showSuggestionsWindow(d.window, d.parent, d.anchor)
	}
	m.broadcast()
	go func() {
		defer cancel()
		var plan *CheckoutPlan
		var err error
		if r.FolderIdentity != "" && folderIdentity(r.Site.Path) != r.FolderIdentity {
			err = errors.New("The Site folder moved or is unavailable. Locate it in Settings before continuing.")
		} else {
			plan, err = prepareCheckoutAction(ctx, r.Site.Path, req, merged)
			if r.FolderIdentity != "" && folderIdentity(r.Site.Path) != r.FolderIdentity {
				plan, err = nil, errors.New("The Site folder changed. Locate it in Settings before continuing.")
			}
		}
		result := &CheckoutAction{SiteID: req.SiteID, Kind: req.Kind, Status: "ready", Plan: plan}
		if err != nil {
			result.Status, result.Message = "failed", err.Error()
			var rebaseErr *rebaseActionError
			if errors.As(err, &rebaseErr) {
				result.Files = rebaseErr.check.Files
			}
		}
		m.send(func() {
			if !m.currentActionDialog(d, generation, r) {
				return
			}
			d.cancel = nil
			d.action = result
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
			if r == nil || !r.Ready || m.siteActionBusy(req.SiteID) {
				m.actionReply(req.RequestID, "The Site is not ready or an action is already running for this Site. Refresh and try again.")
				return
			}
			d := m.actionDialogs[sender]
			if req.Kind == "switch" && !branchSwitchPreparationAllowed(d, req.SiteID) {
				m.actionReply(req.RequestID, "Choose a branch from this Site's branch picker before continuing.")
				return
			}
			var parent *application.WebviewWindow
			if d != nil {
				if req.SiteID != d.siteID || d.action == nil || d.action.Kind != req.Kind ||
					(d.action.Status != "ready" && d.action.Status != "choosing") {
					m.actionReply(req.RequestID, "The selected action changed. Open the action again.")
					return
				}
				parent = d.parent
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
				// Preparation validates the requested branch and commit against live Git.
				parent = viewer.window
			}
			m.prepareAction(req, r, parent)
		})
	})
	m.app.Event.On("devjay:action-confirm", func(e *application.CustomEvent) {
		sender := e.Sender
		var req struct {
			Token     string `json:"token"`
			RequestID string `json:"requestID"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			d := m.readyActionDialog(sender, req.Token)
			if d == nil {
				m.actionReply(req.RequestID, "The confirmation is no longer ready. Review the current action.")
				return
			}
			id, plan := d.action.SiteID, d.action.Plan
			r := m.runtime[id]
			if r == nil || id != d.siteID {
				m.actionReply(req.RequestID, "The Site changed. Open the action again.")
				return
			}
			// Consume the token before starting. Duplicate clicks cannot execute twice.
			if !m.lifecycle.begin() {
				m.actionReply(req.RequestID, "DevJay is quitting. The action was not started.")
				return
			}
			m.actionReply(req.RequestID, "")
			generation := d.generation
			d.action = &CheckoutAction{ID: plan.ID, SiteID: id, Kind: plan.Kind, Status: "running", Message: "Applying the confirmed action. Please wait…"}
			m.broadcast()
			go func() {
				defer m.lifecycle.finish()
				// Network commands are bounded individually. Local mutations must finish.
				result := CheckoutAction{Kind: plan.Kind, Status: "failed", Message: "The Site folder changed. Locate it in Settings before continuing."}
				if r.FolderIdentity == "" || folderIdentity(r.Site.Path) == r.FolderIdentity {
					result = applyCheckoutAction(context.Background(), plan)
				}
				result.SiteID = id
				result.ID = plan.ID
				m.send(func() {
					if !m.currentActionDialog(d, generation, r) {
						return
					}
					d.action = &result
					if current := m.runtime[id]; current == r && current.GitControl != nil {
						current.GitControl.refresh()
					}
					m.broadcast()
				})
			}()
		})
	})
}
