package main

import (
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
	m.registerCheckoutActions()
	m.app.Event.On("devjay:suggestions-close", func(e *application.CustomEvent) {
		if e.Sender == "suggested-actions" {
			m.send(m.closeSuggestions)
		}
	})
}

func (m *siteManager) closeSuggestions() {
	if m.actionBusy() {
		return
	}
	m.cancelCheckoutAction()
	m.suggestions.Hide()
	m.suggestedSite = ""
	m.suggestionsParent = nil
	m.broadcast()
}
