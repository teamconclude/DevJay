package main

import (
	"context"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

func (m *siteManager) createSuggestionsWindow() {
	w := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "suggested-actions", Title: "Confirm action", URL: "/?actions",
		Width: 460, Height: 470, MinWidth: 400, MinHeight: 340, Hidden: true, Frameless: true,
		MaximiseButtonState: application.ButtonDisabled,
		BackgroundColour:    application.NewRGB(20, 24, 32),
		Mac:                 application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	m.suggestions = w
	m.watchView("suggested-actions", w)
	w.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		m.send(func() {
			first := !m.suggestionsReady
			m.suggestionsReady = true
			if first && m.suggestionsParent != nil {
				showSuggestionsWindow(w, m.suggestionsParent)
			}
			m.broadcast()
		})
	})
	w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if m.app.Context().Err() != nil {
			return
		}
		e.Cancel()
		m.send(m.closeSuggestions)
	})
	m.app.Event.On("devjay:rebase-check", func(e *application.CustomEvent) {
		if e.Sender != "suggested-actions" {
			return
		}
		id, ok := e.Data.(string)
		if !ok {
			return
		}
		m.send(func() {
			r := m.runtime[id]
			if id != m.suggestedSite || r == nil || !r.Ready || r.Git.MergedPR != nil || r.Git.Base == nil || r.Git.Base.Behind <= 0 {
				return
			}
			if m.rebaseCancel != nil || m.actionBusy() {
				return
			}
			ctx, cancel := context.WithCancel(m.app.Context())
			m.rebaseCancel = cancel
			m.rebaseGeneration++
			generation := m.rebaseGeneration
			m.rebaseCheck = &RebaseCheck{SiteID: id, Branch: r.Git.Branch, Head: r.Git.Head, BaseHead: r.Git.Base.Head, Target: r.Git.Base.Branch, Status: "checking", Message: "Fetching the latest default branch and checking in a temporary checkout…"}
			m.broadcast()
			go func() {
				result := rebasePreview(ctx, r.Site.Path)
				cancel()
				result.SiteID = id
				m.send(func() {
					if generation != m.rebaseGeneration || m.suggestedSite != id || m.runtime[id] != r {
						return
					}
					m.rebaseCancel = nil
					m.rebaseCheck = &result
					m.broadcast()
				})
			}()
		})
	})
	m.registerCheckoutActions()
	m.app.Event.On("devjay:suggestions-close", func(e *application.CustomEvent) {
		if e.Sender == "suggested-actions" {
			m.send(m.closeSuggestions)
		}
	})
}

func (m *siteManager) cancelRebaseCheck() {
	if m.rebaseCancel != nil {
		m.rebaseCancel()
		m.rebaseCancel = nil
	}
	m.rebaseGeneration++
	m.rebaseCheck = nil
}

func (m *siteManager) closeSuggestions() {
	if m.actionBusy() {
		return
	}
	m.cancelCheckoutAction()
	m.cancelRebaseCheck()
	m.suggestions.Hide()
	m.suggestedSite = ""
	m.suggestionsParent = nil
	m.broadcast()
}
