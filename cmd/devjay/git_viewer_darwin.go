package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type viewerRequest struct {
	SiteID    string `json:"siteID"`
	Kind      string `json:"kind"`
	Operation string `json:"operation"`
	Version   uint64 `json:"version"`
	Commit    string `json:"commit"`
	File      int    `json:"file"`
	Page      int    `json:"page"`
	NewWindow bool   `json:"newWindow"`
}

// Each window owns its snapshot, selections, and cancellable read. UI events
// are routed by Wails' sender, never by a client-supplied Site identifier.
type gitViewerWindow struct {
	editorGeneration uint64
	editorOpening    bool
	window           *application.WebviewWindow
	parent           *application.WebviewWindow
	ready            bool
	state            *GitView
	cancel           context.CancelFunc
	generation       uint64
	used             uint64
	refresh          viewerRefreshSchedule
	refreshCancel    context.CancelFunc
}

func (m *siteManager) emitViewer(id string) {
	r := m.viewers[id]
	if r != nil && r.ready && r.state != nil && !m.recovery.paused {
		m.app.Event.Emit("devjay:viewer-state:"+id, r.state)
	}
}
func (m *siteManager) viewerLoad(id string, work func(context.Context, *GitView) error) {
	r := m.viewers[id]
	if r == nil {
		return
	}
	if r.cancel != nil {
		r.cancel()
	}
	r.cancelViewerRefresh()
	r.generation++
	version := r.generation
	v := *r.state
	v.Version, v.Loading, v.Error = version, true, ""
	v.Editor = nil
	r.state = &v
	m.emitViewer(id)
	ctx, cancel := context.WithCancel(m.app.Context())
	r.cancel = cancel
	go func() {
		next := v
		err := work(ctx, &next)
		cancel()
		m.send(func() {
			if m.viewers[id] != r || version != r.generation {
				return
			}
			next.Loading = false
			if err != nil {
				next.Error = err.Error()
			}
			r.state = &next
			r.cancel = nil
			r.refresh.lastFinished = time.Now()
			m.emitViewer(id)
			m.refreshViewerEditor(id)
		})
	}()
}
func (m *siteManager) forgetViewer(id string) {
	if r := m.viewers[id]; r != nil {
		r.cancelViewerRefresh()
		if r.cancel != nil {
			r.cancel()
		}
		if m.suggestionsParent == r.window {
			m.suggestionsParent = m.panel
			if site := m.runtime[r.state.SiteID]; site != nil && site.Ready {
				m.suggestionsParent = site.Window
			}
		}
		m.forgetView(id)
		delete(m.viewers, id)
	}
}
func (m *siteManager) closeViewer(id string) {
	if r := m.viewers[id]; r != nil {
		m.forgetViewer(id)
		r.window.Close()
	}
}
func (m *siteManager) openViewer(req viewerRequest, parent *application.WebviewWindow, site *siteRuntime) {
	if r := m.existingViewer(req); r != nil {
		m.viewerSequence++
		r.used = m.viewerSequence
		for id, candidate := range m.viewers {
			if candidate == r {
				if r.state.Kind != req.Kind {
					m.viewerMode(id, req.Kind)
				} else {
					r.refresh.pending = true
					m.refreshViewers(time.Now())
				}
				break
			}
		}
		if r.ready {
			focusSite(r.window)
		}
		return
	}
	m.viewerSequence++
	id := fmt.Sprintf("git-viewer-%d", m.viewerSequence)
	w := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: id, Title: "DevJay · " + filepath.Base(site.Site.Path) + " · Changes", URL: "/?viewer=" + id,
		Width: 1100, Height: 740, MinWidth: 720, MinHeight: 460, Hidden: true,
		BackgroundColour: application.NewRGB(20, 24, 32),
		Mac:              application.MacWindow{CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	m.viewers[id] = &gitViewerWindow{window: w, parent: parent, used: m.viewerSequence, state: &GitView{SiteID: req.SiteID, Name: filepath.Base(site.Site.Path), Path: site.Site.Path, Kind: req.Kind, File: -1, RemoteNote: site.Git.RemoteNote, Rebased: site.Git.Rebased}}
	m.watchView(id, w)
	w.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		m.send(func() {
			if r := m.viewers[id]; r != nil {
				if !r.ready {
					r.ready = true
					showViewerWindow(w, r.parent)
				}
				m.emitViewer(id)
			}
		})
	})
	w.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		m.send(func() { m.forgetViewer(id) })
	})
	m.viewerLoad(id, func(ctx context.Context, v *GitView) error { return loadGitView(ctx, v, true) })
}

func (m *siteManager) viewerMode(id, kind string) {
	r := m.viewers[id]
	if r == nil || !validViewKind(kind) {
		return
	}
	// Publish the requested mode immediately. Another card click can then
	// replace this in-flight request instead of mistaking it for the old mode.
	fresh := *r.state
	fresh.Kind = kind
	if site := m.runtime[fresh.SiteID]; site != nil {
		fresh.RemoteNote, fresh.Rebased = site.Git.RemoteNote, site.Git.Rebased
	}
	r.state = &fresh
	m.viewerLoad(id, func(ctx context.Context, v *GitView) error { return loadGitView(ctx, v, true) })
}

func (m *siteManager) registerViewerEvents() {
	m.viewers = make(map[string]*gitViewerWindow)
	m.app.Event.On("devjay:viewer-open", func(e *application.CustomEvent) {
		var req viewerRequest
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil || !validViewKind(req.Kind) {
			return
		}
		sender := e.Sender
		m.send(func() {
			r := m.runtime[req.SiteID]
			if r == nil || !r.Ready {
				return
			}
			var parent *application.WebviewWindow
			if sender == "control-panel" {
				parent = m.panel
			} else if sender == "site-"+req.SiteID {
				parent = r.Window
			}
			if parent == nil {
				return
			}
			m.openViewer(req, parent, r)
		})
	})
	m.app.Event.On("devjay:viewer-request", func(e *application.CustomEvent) {
		id := e.Sender
		var req viewerRequest
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			r := m.viewers[id]
			if r == nil {
				return
			}
			m.viewerSequence++
			r.used = m.viewerSequence
			if req.Operation == "refresh" || req.Operation == "mode" {
				if site := m.runtime[r.state.SiteID]; site != nil {
					fresh := *r.state
					fresh.RemoteNote, fresh.Rebased = site.Git.RemoteNote, site.Git.Rebased
					r.state = &fresh
				}
			}
			if req.Version != r.state.Version {
				m.emitViewer(id)
				return
			}
			switch req.Operation {
			case "file-open":
				m.openViewerFile(id)
				return
			case "mode":
				m.viewerMode(id, req.Kind)
				return
			}
			m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
				switch req.Operation {
				case "refresh":
					return refreshGitView(ctx, v)
				case "page":
					if req.Page >= 0 && req.Page <= 10000 && v.Kind != "edits" {
						v.Page = req.Page
						return loadGitView(ctx, v, false)
					}
				case "commit":
					if v.Kind != "edits" {
						return selectViewCommit(ctx, v, req.Commit)
					}
				case "file":
					selectViewFile(ctx, v, req.File)
				}
				return nil
			})
		})
	})
	m.app.Event.On("devjay:viewer-hello", func(e *application.CustomEvent) {
		id := e.Sender
		m.send(func() { m.emitViewer(id) })
	})
	m.app.Event.On("devjay:viewer-close", func(e *application.CustomEvent) {
		id := e.Sender
		m.send(func() { m.closeViewer(id) })
	})
}

func (m *siteManager) existingViewer(req viewerRequest) *gitViewerWindow {
	if req.NewWindow {
		return nil
	}
	var selected *gitViewerWindow
	for _, viewer := range m.viewers {
		if viewer.state.SiteID != req.SiteID {
			continue
		}
		if selected == nil || (viewer.state.Kind == req.Kind && selected.state.Kind != req.Kind) ||
			((viewer.state.Kind == req.Kind) == (selected.state.Kind == req.Kind) && viewer.used > selected.used) {
			selected = viewer
		}
	}
	return selected
}
