package main

import (
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) addFileMenu(menu *application.Menu) {
	file := menu.AddSubmenu("File")
	newWindow := func(review bool) func(*application.Context) {
		return func(*application.Context) {
			// Capture the source at invocation, before another window can gain
			// focus while this command waits on the manager queue.
			source := ""
			if window := m.app.Window.Current(); window != nil {
				source = window.Name()
			}
			m.send(m.userCommand(func() { m.newFileMenuWindow(source, review) }))
		}
	}
	file.Add("New Window").SetAccelerator("CmdOrCtrl+n").OnClick(newWindow(false))
	file.Add("New Review Window").SetAccelerator("CmdOrCtrl+Shift+n").OnClick(newWindow(true))
	file.AddSeparator()
	// Use AppKit's normal close action, including the viewer's unsaved-edit
	// guard and the existing Settings/strip close behavior.
	file.AddRole(application.CloseWindow).SetLabel("Close Window")
}

func (m *siteManager) newFileMenuWindow(source string, review bool) {
	if m.recovery.paused {
		return
	}
	items := make(map[string]siteRuntime, len(m.runtime))
	for id, site := range m.runtime {
		items[id] = *site
	}
	m.ensureWindowSnapshot(items)
	req, parent, site := m.fileMenuTarget(source, review)
	if site == nil {
		if !strings.HasPrefix(source, "git-viewer-") && !strings.HasPrefix(source, "site-") {
			m.openSettings()
		}
		return
	}
	m.openViewer(req, parent, site)
}

// Resolve only from manager-owned windows and current Profile membership.
// No Git reads are needed to choose a Site or open the new window.
func (m *siteManager) fileMenuTarget(source string, review bool) (viewerRequest, *application.WebviewWindow, *siteRuntime) {
	req := viewerRequest{NewWindow: true}
	parent := m.strip
	var site *siteRuntime
	readySite := func(id string) *siteRuntime {
		if r := m.runtime[id]; r != nil && r.Ready && siteEnabled(m.config, id) {
			return r
		}
		return nil
	}
	if viewer := m.viewers[source]; viewer != nil && viewer.state != nil {
		site = readySite(viewer.state.SiteID)
		if site != nil && site.Site.Path != viewer.state.Path {
			site = nil // The source belongs to a removed or relocated folder.
		}
		req.Kind, parent = viewer.state.Kind, viewer.window
		req.IgnoreWhitespace = viewer.state.IgnoreWhitespace
		if viewer.state.Review != nil {
			req.BaseRef = viewer.state.Review.BaseRef
			req.IgnoreWhitespace, req.reviewWhitespace = viewer.state.Review.IgnoreWhitespace, true
		}
	} else if strings.HasPrefix(source, "git-viewer-") {
		// The captured source closed before its queued command could run.
		return req, nil, nil
	} else if strings.HasPrefix(source, "site-") {
		site = readySite(strings.TrimPrefix(source, "site-"))
		if site != nil {
			parent = site.Window
		}
	} else {
		// Prefer the last-used Site on the current desktop, then another Site
		// on that desktop, then the last-used Site or first enabled Site.
		last := readySite(m.lastWindowSiteID)
		if last != nil && m.monitor.states[last.Site.ID].OnDesktop {
			site = last
		}
		var first *siteRuntime
		for _, configured := range visibleSites(m.config) {
			r := readySite(configured.ID)
			if r == nil {
				continue
			}
			if first == nil {
				first = r
			}
			if site == nil && m.monitor.states[configured.ID].OnDesktop {
				site = r
			}
		}
		if site == nil {
			site = last
		}
		if site == nil {
			site = first
		}
	}
	if site != nil {
		req.SiteID = site.Site.ID
		if review {
			req.Kind = "review"
		} else if req.Kind == "" {
			req.Kind = cardMenuViewKind("auto", site.Git)
		}
	}
	return req, parent, site
}
