package main

/*
int devjay_prepare_viewer_on_site(void *window, void *site);
int devjay_viewer_placement_status(void *window, void *site);
void devjay_show_viewer_on_site(void *window);
*/
import "C"

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) registerViewerPlacementEvents() {
	m.app.Event.On("devjay:viewersOnSiteDesktop", func(e *application.CustomEvent) {
		value, ok := e.Data.(bool)
		if e.Sender != "settings" || !ok {
			return
		}
		m.send(func() {
			c := m.config
			c.ViewersOnSiteDesktop = value
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
			} else {
				m.siteError("")
			}
			m.broadcast()
		})
	})
}

func (m *siteManager) viewerDesktopSite(r *gitViewerWindow) *siteRuntime {
	if !m.config.ViewersOnSiteDesktop || r == nil || r.state == nil {
		return nil
	}
	site := m.runtime[r.state.SiteID]
	if site == nil || !site.Ready || site.Window == nil || !siteEnabled(m.config, site.Site.ID) || site.Site.Path != r.state.Path {
		return nil
	}
	return site
}

func prepareViewerOnSite(window, site *application.WebviewWindow) bool {
	var prepared C.int
	application.InvokeSync(func() { prepared = C.devjay_prepare_viewer_on_site(window.NativeWindow(), site.NativeWindow()) })
	return prepared != 0
}

func viewerPlacementStatus(window, site *application.WebviewWindow) int {
	var status C.int
	application.InvokeSync(func() { status = C.devjay_viewer_placement_status(window.NativeWindow(), site.NativeWindow()) })
	return int(status)
}

func showViewerOnSite(window *application.WebviewWindow) {
	windowNavigationGeneration.Add(1)
	window.Show()
	application.InvokeSync(func() { C.devjay_show_viewer_on_site(window.NativeWindow()) })
}

func (m *siteManager) viewerNavigationCurrent(id string, r *gitViewerWindow, site *siteRuntime, navigation uint64) bool {
	return m.viewers[id] == r && r.ready && r.navigation == navigation &&
		windowNavigationGeneration.Load() == navigation && !m.recovery.paused && m.viewerDesktopSite(r) == site
}

// Explicit opens share this path, whether creating a window or reusing one.
// Verify the native viewer's Space assignment before showing it. Site windows
// can remain hidden, and no MoveToActiveSpace flag can pull the viewer back.
func (m *siteManager) showViewer(id string) {
	r := m.viewers[id]
	if r == nil || !r.ready || r.navigation != windowNavigationGeneration.Load() || m.recovery.paused {
		return
	}
	site := m.viewerDesktopSite(r)
	if site == nil {
		showViewerWindow(r.window, r.parent, r.anchor)
		return
	}
	fallback := func() {
		showViewerWindow(r.window, r.parent, r.anchor)
		fresh := *r.state
		fresh.Error = "The Site’s desktop is unavailable. This window opened on the current desktop."
		r.state = &fresh
		m.emitViewer(id)
	}
	if !prepareViewerOnSite(r.window, site.Window) {
		fallback()
		return
	}
	navigation := r.navigation
	deadline := time.Now().Add(2 * time.Second)
	var present func()
	present = func() {
		if !m.viewerNavigationCurrent(id, r, site, navigation) {
			return
		}
		status := viewerPlacementStatus(r.window, site.Window)
		if status == -2 { // The user changed focus while placement settled.
			return
		}
		if status == 1 {
			showViewerOnSite(r.window)
			return
		}
		if status < 0 || !time.Now().Before(deadline) {
			// Keep the requested window reachable if macOS could not switch.
			fallback()
			return
		}
		time.AfterFunc(60*time.Millisecond, func() { m.send(present) })
	}
	present()
}
