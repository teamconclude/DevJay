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
				return
			}
			if sender == "suggested-actions" {
				if req.SiteID != m.suggestedSite {
					return
				}
			} else {
				viewer := m.viewers[sender]
				if viewer == nil || viewer.state.SiteID != req.SiteID || !hasSuggestedActions(r.Git) {
					return
				}
				// Bind the selected action to this viewer’s Site. Preparation
				// validates the requested branch and commit against live Git.
				m.suggestedSite = req.SiteID
				m.suggestionsParent = viewer.window
			}
			title := map[string]string{"push": "Confirm push", "update": "Confirm branch update", "rebase": "Confirm rebase", "merged": "Confirm return to default branch"}[req.Kind]
			if title == "" {
				return
			}
			m.suggestions.SetTitle(title)
			m.cancelCheckoutAction()
			m.cancelRebaseCheck()
			ctx, cancel := context.WithTimeout(m.app.Context(), 2*time.Minute)
			m.actionCancel = cancel
			generation := m.actionGeneration
			merged := r.Git.MergedPR
			m.action = &CheckoutAction{SiteID: req.SiteID, Kind: req.Kind, Status: "preparing", Message: "Checking the checkout and fetching the remote…"}
			if m.suggestionsReady {
				showSuggestionsWindow(m.suggestions, m.suggestionsParent)
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
		})
	})
	m.app.Event.On("devjay:action-confirm", func(e *application.CustomEvent) {
		if e.Sender != "suggested-actions" {
			return
		}
		token, ok := e.Data.(string)
		if !ok {
			return
		}
		m.send(func() {
			if m.action == nil || m.action.Status != "ready" || m.action.Plan == nil || token != m.action.Plan.ID {
				return
			}
			id, plan := m.action.SiteID, m.action.Plan
			r := m.runtime[id]
			if r == nil || id != m.suggestedSite {
				return
			}
			// Consume the token before starting. Duplicate clicks cannot execute twice.
			m.mu.Lock()
			if m.shuttingDown {
				m.mu.Unlock()
				return
			}
			m.actionWG.Add(1)
			m.mu.Unlock()
			m.action = &CheckoutAction{SiteID: id, Kind: plan.Kind, Status: "running", Message: "Applying the confirmed action. Please wait…"}
			m.broadcast()
			go func() {
				defer m.actionWG.Done()
				// Quitting waits for this bounded operation instead of interrupting a
				// checkout halfway through. Closing the popup cannot cancel a mutation.
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				defer cancel()
				result := applyCheckoutAction(ctx, plan)
				result.SiteID = id
				m.send(func() { m.action = &result; m.broadcast() })
			}()
		})
	})
}
