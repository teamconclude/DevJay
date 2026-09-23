package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

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
			m.emitViewer(id)
			m.refreshViewerEditor(id)
		})
	}()
}
func (m *siteManager) forgetViewer(id string) {
	if r := m.viewers[id]; r != nil {
		if r.cancel != nil {
			r.cancel()
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
	m.viewerSequence++
	id := fmt.Sprintf("git-viewer-%d", m.viewerSequence)
	w := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: id, Title: "DevJay · " + filepath.Base(site.Site.Path) + " · Changes", URL: "/?viewer=" + id,
		Width: 1100, Height: 740, MinWidth: 720, MinHeight: 460, Hidden: true,
		BackgroundColour: application.NewRGB(20, 24, 32),
		Mac:              application.MacWindow{CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	m.viewers[id] = &gitViewerWindow{window: w, parent: parent, state: &GitView{SiteID: req.SiteID, Name: filepath.Base(site.Site.Path), Path: site.Site.Path, Kind: req.Kind, File: -1, RemoteNote: site.Git.RemoteNote, Rebased: site.Git.Rebased}}
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
			}
			m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
				switch req.Operation {
				case "refresh":
					return loadGitView(ctx, v, true)
				case "mode":
					if validViewKind(req.Kind) {
						v.Kind = req.Kind
						return loadGitView(ctx, v, true)
					}
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
