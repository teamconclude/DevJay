package main

import (
	"bytes"
	"net/http"
	"sync/atomic"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Serve the current preference in the initial document, including after a
// WebKit recovery reload. HTTP readers never access manager-owned config.
type appearanceAssets struct {
	light    atomic.Bool
	index    []byte
	fallback http.Handler
}

func (a *appearanceAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		a.fallback.ServeHTTP(w, r)
		return
	}
	theme := "dark"
	if a.light.Load() {
		theme = "light"
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodHead {
		_, _ = w.Write(bytes.Replace(a.index, []byte(`data-theme="dark"`), []byte(`data-theme="`+theme+`"`), 1))
	}
}

func (c SiteConfig) backgroundColour(systemDark bool) application.RGBA {
	if c.resolvedTheme(systemDark) == "light" {
		return application.NewRGB(246, 248, 251)
	}
	return application.NewRGB(20, 24, 32)
}

func (m *siteManager) applyAppearance() {
	m.systemDark = systemAppearanceDark()
	light := m.config.resolvedTheme(m.systemDark) == "light"
	if m.appearanceAssets != nil {
		m.appearanceAssets.light.Store(light)
	}
	setNativeAppearance(m.config.theme(), light)
	for _, window := range m.recoveryWindows {
		window.SetBackgroundColour(m.config.backgroundColour(m.systemDark))
	}
}

func (m *siteManager) registerAppearanceEvents() {
	m.app.Event.OnApplicationEvent(events.Mac.ApplicationDidChangeTheme, func(*application.ApplicationEvent) {
		m.send(func() {
			if m.config.theme() == "system" {
				// Read the current OS choice on the queue; a queued notification
				// must not restore an earlier appearance after a later change.
				m.applyAppearance()
				m.broadcast()
			}
		})
	})
	m.app.Event.On("devjay:theme", func(e *application.CustomEvent) {
		value, ok := e.Data.(string)
		if e.Sender != "settings" || !ok || !validTheme(value) {
			return
		}
		m.send(func() {
			c := m.config
			c.Theme = value
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
				m.broadcast()
				return
			}
			m.applyAppearance()
			m.siteError("")
			m.broadcast()
		})
	})
}
