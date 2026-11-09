package main

import (
	"context"
	"time"
)

// A review window is a separate surface: history and checkout-navigation
// requests cannot silently turn it back into the Changes window.
func (m *siteManager) handleReviewRequest(id string, req viewerRequest) {
	r := m.viewers[id]
	if r == nil || r.state.Kind != "review" || r.state.Loading || r.reviewSaving {
		return
	}
	if req.Operation == "review-edit" {
		if req.Editing && !r.reviewEditing {
			if req.File < 0 || req.File >= len(r.state.Files) || req.Token == "" {
				m.reviewCloseError(id, "Select a text file before editing.")
				return
			}
			if req.File != r.state.File {
				// Continuous review can display a cached file while its next
				// file is prefetched. Pin the requested edit selection first.
				m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
					return selectBranchReviewFile(ctx, v, req.File)
				})
				return
			}
			selected := r.state.Review
			if selected == nil || selected.Selected == nil || !selected.Selected.Editable || selected.Selected.Token != req.Token {
				m.emitViewer(id)
				return
			}
		}
		if req.Editing == r.reviewEditing {
			m.emitViewer(id)
			return
		}
		if req.Editing && !m.lifecycle.beginDraft() {
			return
		}
		r.reviewEditing = req.Editing
		next := *r.state
		next.ReviewEditing = req.Editing
		r.state = &next
		if req.Editing {
			r.cancelViewerRefresh()
			r.cancelPrefetch()
		} else {
			m.lifecycle.finishDraft()
			r.refresh.pending = true
		}
		m.emitViewer(id)
		if !req.Editing {
			m.refreshViewers(time.Now())
		}
		return
	}
	if r.reviewEditing && req.Operation != "review-save" {
		return
	}
	switch req.Operation {
	case "review-prefetch":
		m.prefetchReviewFiles(id, req.PrefetchID, req.Prefetch)
	case "refresh":
		m.viewerLoad(id, refreshGitView)
	case "review-options":
		if reviewWhitespaceChoiceChanged(m.config, r.state.Review, req.IgnoreWhitespace) {
			config := m.config
			config.ReviewIgnoreWhitespace = req.IgnoreWhitespace
			if err := m.save(config); err != nil {
				m.viewerLoad(id, func(context.Context, *GitView) error { return err })
				return
			}
		}
		m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
			return loadBranchReview(ctx, v, req.BaseRef, req.IgnoreWhitespace)
		})
	case "review-file":
		m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
			return selectBranchReviewFile(ctx, v, req.File)
		})
	case "review-save":
		if !r.reviewEditing || !m.lifecycle.begin() {
			return
		}
		r.reviewSaving = true
		m.viewerLoad(id, func(ctx context.Context, v *GitView) error {
			defer m.lifecycle.finish()
			return saveBranchReviewFile(ctx, v, req.Token, req.Content)
		})
	}
}

func (m *siteManager) requestCloseViewer(id string) {
	r := m.viewers[id]
	if r == nil {
		return
	}
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
		m.reviewCloseError(id, "Save or cancel your edits before closing this window.")
		return
	}
	m.closeViewer(id)
}

func (m *siteManager) reviewCloseError(id, message string) {
	if r := m.viewers[id]; r != nil {
		next := *r.state
		next.Error = message
		r.state = &next
		m.emitViewer(id)
	}
}

// Wake/manual recovery must not destroy a live textarea. Automatic WebKit
// process-crash reloads are handled by the frontend's restored edit session.
func (m *siteManager) deferReviewEditorRecovery(now time.Time) {
	for id, viewer := range m.viewers {
		if !viewer.reviewEditing && !viewer.reviewSaving {
			continue
		}
		if recovery := m.recovery.views[id]; recovery != nil && !recovery.due.IsZero() {
			recovery.due = now.Add(uiRecoveryTimeout)
		}
	}
}
