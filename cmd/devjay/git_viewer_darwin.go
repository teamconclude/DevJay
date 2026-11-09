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
	PRNumber         int                 `json:"prNumber"`
	PRLoadID         uint64              `json:"prLoadID"`
	PRSubmission     *PRReviewSubmission `json:"prSubmission"`
	PRAnchor         *PRReviewAnchor     `json:"prAnchor"`
	PRComment        *PRCommentRequest   `json:"prComment"`
	BaseRef          string              `json:"baseRef"`
	IgnoreWhitespace bool                `json:"ignoreWhitespace"`
	Token            string              `json:"token"`
	Content          string              `json:"content"`
	Editing          bool                `json:"editing"`
	Anchor           *cardAnchor         `json:"anchor,omitempty"`
	SiteID           string              `json:"siteID"`
	Kind             string              `json:"kind"`
	Scope            string              `json:"scope"`
	Order            string              `json:"order"`
	Operation        string              `json:"operation"`
	Version          uint64              `json:"version"`
	Commit           string              `json:"commit"`
	File             int                 `json:"file"`
	Prefetch         []int               `json:"prefetch"`
	PrefetchID       uint64              `json:"prefetchID"`
	Page             int                 `json:"page"`
	NewWindow        bool                `json:"newWindow"`
}

// Each window owns its snapshot, selections, and cancellable read. UI events
// are routed by Wails' sender, never by a client-supplied Site identifier.
type gitViewerWindow struct {
	navigation       uint64
	prReview         *prReviewWindow
	removeCloseHook  func()
	reviewEditing    bool
	reviewSaving     bool
	editorGeneration uint64
	editorOpening    bool
	window           *application.WebviewWindow
	title            string
	parent           *application.WebviewWindow
	anchor           *cardAnchor
	ready            bool
	state            *GitView
	cancel           context.CancelFunc
	generation       uint64
	used             uint64
	refresh          viewerRefreshSchedule
	refreshCancel    context.CancelFunc
	diffCache        viewerDiffCache
	prefetchCancel   context.CancelFunc
	prefetchEpoch    uint64
}

func (m *siteManager) emitViewer(id string) {
	r := m.viewers[id]
	if r != nil && r.ready && r.state != nil && !m.recovery.paused {
		if site := m.runtime[r.state.SiteID]; site != nil {
			r.updateTitle(site.Git)
			if r.state.Kind == "review" {
				next := *r.state
				next.ReviewPRs = nil
				if next.Branch != "" && next.Branch == site.Git.Branch {
					next.ReviewPRs = site.Git.PullRequests
				}
				r.state = &next
			}
		}
		dispatchWindowEvent(r.window, "devjay:viewer-state:"+id, r.state)
		m.emitPRReview(id)
	}
}

func (r *gitViewerWindow) updateTitle(status GitStatus) {
	title := gitViewerTitle(r.state.Name, r.state.Kind, r.state.Branch, status)
	if title != r.title {
		r.title = title
		r.window.SetTitle(title)
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
	r.cancelPrefetch()
	r.generation++
	version := r.generation
	v := *r.state
	v.Version, v.Loading, v.Error = version, true, ""
	v.Editor = nil
	r.state = &v
	m.emitViewer(id)
	ctx, cancel := context.WithCancel(context.WithValue(m.app.Context(), viewerForegroundKey{}, true))
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
			r.reviewSaving = false
			r.diffCache.remember(&next)
			m.emitViewer(id)
			m.refreshViewerEditor(id)
			m.prefetchViewerFiles(id)
		})
	}()
}
func (m *siteManager) forgetViewer(id string) {
	if r := m.viewers[id]; r != nil {
		if r.prReview != nil && r.prReview.draftGuard {
			m.lifecycle.finishDraft()
		}
		if r.prReview != nil && r.prReview.cancel != nil {
			r.prReview.cancel()
		}
		if r.prReview != nil && r.prReview.anchorCancel != nil {
			r.prReview.anchorCancel()
		}
		if r.reviewEditing {
			m.lifecycle.finishDraft()
		}
		r.cancelPrefetch()
		r.cancelViewerRefresh()
		if r.cancel != nil {
			r.cancel()
		}
		for _, dialog := range m.actionDialogs {
			if dialog.parent == r.window {
				dialog.anchor = nil
				dialog.parent = m.strip
				if site := m.runtime[dialog.siteID]; site != nil && site.Ready {
					dialog.parent = site.Window
				}
			}
		}
		for _, viewer := range m.viewers {
			if viewer != r && viewer.parent == r.window {
				viewer.parent, viewer.anchor = m.strip, nil
				if site := m.runtime[viewer.state.SiteID]; site != nil && site.Ready {
					viewer.parent = site.Window
				}
			}
		}
		m.forgetView(id)
		delete(m.viewers, id)
	}
}
func (m *siteManager) closeViewer(id string) {
	if r := m.viewers[id]; r != nil {
		if r.prReview != nil && r.prReview.draftGuard {
			m.reviewCloseError(id, "Save or copy your unsaved PR review draft before closing this window.")
			return
		}
		if r.prReview != nil && r.prReview.state.Submitting {
			r.prReview.state.Error = "Wait for the PR review submission to finish before closing this window."
			m.emitPRReview(id)
			m.reviewCloseError(id, "Wait for the PR review submission to finish before closing this window.")
			return
		}
		if r.reviewEditing || r.reviewSaving {
			// Site/Profile lifecycle may remove the original parent while this
			// window stays open to preserve its unsaved text.
			r.parent, r.anchor = nil, nil
			m.reviewCloseError(id, "Save or cancel your edits before closing this window.")
			return
		}
		if r.removeCloseHook != nil {
			r.removeCloseHook()
		}
		m.forgetViewer(id)
		r.window.Close()
	}
}
func (m *siteManager) openViewer(req viewerRequest, parent *application.WebviewWindow, site *siteRuntime) {
	if !req.Anchor.valid() {
		return
	}
	m.lastWindowSiteID = req.SiteID
	navigation := windowNavigationGeneration.Add(1)
	if r := m.existingViewer(req); r != nil {
		if site != nil {
			r.updateTitle(site.Git)
		}
		m.viewerSequence++
		r.used = m.viewerSequence
		// Explicit opens follow their current source, including a viewer first
		// opened from a Site and later requested from its strip card.
		r.parent, r.anchor, r.navigation = parent, req.Anchor, navigation
		for id, candidate := range m.viewers {
			if candidate == r {
				if r.state.Kind != req.Kind || (req.Kind == "branch" && (!r.state.BranchOnly || r.state.OldestFirst != m.config.BranchHistoryOldestFirst)) {
					m.viewerMode(id, req.Kind)
				} else {
					r.refresh.pending = true
					m.refreshViewers(time.Now())
				}
				if r.ready {
					m.showViewer(id)
				}
				break
			}
		}
		return
	}
	if req.Kind == "review" {
		req.IgnoreWhitespace = reviewOpenWhitespace(req, m.config)
	}
	m.viewerSequence++
	id := fmt.Sprintf("git-viewer-%d", m.viewerSequence)
	title := gitViewerTitle(filepath.Base(site.Site.Path), req.Kind, site.Git.Branch, site.Git)
	url := "/?viewer=" + id
	if req.Kind == "review" {
		url += "&review=1"
	}
	w := m.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: id, Title: title, URL: url,
		Width: 1100, Height: 740, MinWidth: 720, MinHeight: 460, Hidden: true,
		BackgroundColour: m.config.backgroundColour(m.systemDark),
		Mac:              application.MacWindow{CollectionBehavior: application.MacWindowCollectionBehaviorMoveToActiveSpace | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed},
	})
	state := &GitView{SiteID: req.SiteID, Name: filepath.Base(site.Site.Path), Path: site.Site.Path, Branch: site.Git.Branch, File: -1, RemoteNote: site.Git.RemoteNote, Rebased: site.Git.Rebased}
	state.OldestFirst = m.config.BranchHistoryOldestFirst
	if req.Kind != "review" {
		state.IgnoreWhitespace = req.IgnoreWhitespace // Only a duplicated window carries this choice.
	}
	setViewMode(state, req.Kind)
	m.viewers[id] = &gitViewerWindow{window: w, title: title, parent: parent, anchor: req.Anchor, used: m.viewerSequence, state: state, navigation: navigation}
	m.watchView(id, w)
	w.OnWindowEvent(events.Common.WindowFocus, func(*application.WindowEvent) {
		m.send(func() {
			if r := m.viewers[id]; r != nil {
				m.lastWindowSiteID = r.state.SiteID
			}
		})
	})
	w.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		m.send(func() {
			if r := m.viewers[id]; r != nil {
				if !r.ready {
					r.ready = true
					m.showViewer(id)
				}
				m.emitViewer(id)
			}
		})
	})
	m.viewers[id].removeCloseHook = w.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if m.app.Context().Err() != nil {
			return
		}
		e.Cancel()
		m.send(func() { m.requestCloseViewer(id) })
	})
	m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
		if req.Kind == "review" {
			return loadBranchReview(ctx, v, req.BaseRef, req.IgnoreWhitespace)
		}
		return loadGitView(ctx, v, true)
	})
}

func (m *siteManager) viewerMode(id, kind string) {
	r := m.viewers[id]
	if r == nil || !validViewKind(kind) || kind == "review" || r.state.Kind == "review" {
		return
	}
	// Publish the requested mode immediately. Another card click can then
	// replace this in-flight request instead of mistaking it for the old mode.
	fresh := *r.state
	setViewMode(&fresh, kind)
	fresh.OldestFirst = m.config.BranchHistoryOldestFirst
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
			if sender == "strip" {
				parent = m.strip
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
			if req.Operation == "pr-review-load" || req.Operation == "pr-review-anchor" || req.Operation == "pr-review-comment" || req.Operation == "pr-review-comment-check" || req.Operation == "pr-review-comment-retry" || req.Operation == "pr-review-submit" || req.Operation == "pr-review-check" || req.Operation == "pr-review-retry" || req.Operation == "pr-review-draft" {
				m.handlePRReviewRequest(id, req)
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
			if req.Operation == "review-open" {
				if site := m.runtime[r.state.SiteID]; site != nil && site.Ready {
					open := viewerRequest{SiteID: r.state.SiteID, Kind: "review", NewWindow: true, IgnoreWhitespace: r.state.IgnoreWhitespace}
					if r.state.Review != nil {
						open.BaseRef, open.IgnoreWhitespace = r.state.Review.BaseRef, r.state.Review.IgnoreWhitespace
					}
					m.openViewer(open, r.window, site)
				}
				return
			}
			if r.state.Kind == "review" {
				m.handleReviewRequest(id, req)
				return
			}
			switch req.Operation {
			case "file-open":
				m.openViewerFile(id)
				return
			case "mode":
				m.viewerMode(id, req.Kind)
				return
			case "history-sort":
				if r.state.Loading || r.state.Kind != "branch" || !r.state.BranchOnly || !validHistoryOrder(req.Order) {
					m.emitViewer(id)
					return
				}
				config := m.config
				config.BranchHistoryOldestFirst = req.Order == "oldest"
				if err := m.save(config); err != nil {
					m.viewerLoad(id, func(context.Context, *GitView) error { return err })
					return
				}
				m.viewerLoad(id, func(ctx context.Context, v *GitView) error { return selectViewHistoryOrder(ctx, v, req.Order) })
				return
			}
			if req.Operation == "file" && r.useCachedFile(req.File) {
				m.emitViewer(id)
				m.refreshViewerEditor(id)
				m.prefetchViewerFiles(id)
				return
			}
			if req.Operation == "refresh" {
				r.diffCache = viewerDiffCache{}
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
				case "branch-diff":
					return selectViewBranchDiff(ctx, v)
				case "history-scope":
					return selectViewHistoryScope(ctx, v, req.Scope)
				case "file":
					selectViewFile(ctx, v, req.File)
				case "whitespace":
					return selectViewWhitespace(ctx, v, req.IgnoreWhitespace)
				}
				return nil
			})
		})
	})
	m.app.Event.On("devjay:viewer-hello", func(e *application.CustomEvent) {
		id := e.Sender
		m.send(func() { m.emitViewer(id); m.emitPRReview(id, true) })
	})
	m.app.Event.On("devjay:viewer-close", func(e *application.CustomEvent) {
		id := e.Sender
		m.send(func() { m.requestCloseViewer(id) })
	})
}

func (m *siteManager) existingViewer(req viewerRequest) *gitViewerWindow {
	if req.NewWindow {
		return nil
	}
	var selected *gitViewerWindow
	for _, viewer := range m.viewers {
		if viewer.state.SiteID != req.SiteID || (viewer.state.Kind == "review") != (req.Kind == "review") {
			continue
		}
		if selected == nil || (viewer.state.Kind == req.Kind && selected.state.Kind != req.Kind) ||
			((viewer.state.Kind == req.Kind) == (selected.state.Kind == req.Kind) && viewer.used > selected.used) {
			selected = viewer
		}
	}
	return selected
}
