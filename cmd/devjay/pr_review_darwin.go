package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type prReviewState struct {
	LoadID     uint64                `json:"loadID"`
	Snapshot   *PRReviewSnapshot     `json:"snapshot,omitempty"`
	Loading    bool                  `json:"loading"`
	Submitting bool                  `json:"submitting"`
	Unknown    bool                  `json:"unknown"`
	RequestID  string                `json:"requestID,omitempty"`
	ReceiptID  int64                 `json:"receiptID,omitempty"`
	Error      string                `json:"error,omitempty"`
	Anchor     *PRReviewAnchorResult `json:"anchor,omitempty"`
	Pending    *PRPendingReview      `json:"pending"`
	Comment    *PRCommentResult      `json:"comment,omitempty"`
}
type prReviewWindow struct {
	state        prReviewState
	cancel       context.CancelFunc
	draftGuard   bool
	lastEmission string
	anchorID     uint64
	anchorCancel context.CancelFunc
	lastSnapshot string
}

func (m *siteManager) emitPRReview(id string, force ...bool) {
	if r := m.viewers[id]; r != nil && r.ready && r.prReview != nil && !m.recovery.paused {
		// Re-publish deferred results after wake, without repeatedly sending the
		// preloaded diff on unrelated working-tree/monitor updates.
		keyState := r.prReview.state
		if keyState.Snapshot != nil {
			keyState.Snapshot = &PRReviewSnapshot{Token: keyState.Snapshot.Token}
		}
		key, _ := json.Marshal(keyState)
		if r.prReview.lastEmission == string(key) && (len(force) == 0 || !force[0]) {
			return
		}
		r.prReview.lastEmission = string(key)
		payload := r.prReview.state
		if payload.Snapshot != nil {
			if r.prReview.lastSnapshot == payload.Snapshot.Token && (len(force) == 0 || !force[0]) {
				payload.Snapshot = nil
			} else {
				r.prReview.lastSnapshot = payload.Snapshot.Token
			}
		}
		dispatchWindowEvent(r.window, "devjay:pr-review:"+id, payload)
	}
}

func (m *siteManager) handlePRReviewRequest(id string, req viewerRequest) {
	r := m.viewers[id]
	if r == nil || r.state == nil || r.state.Kind != "review" || m.recovery.paused {
		return
	}
	if req.Operation == "pr-review-draft" {
		session := r.prReview
		if session == nil || session.state.Snapshot == nil || session.state.Snapshot.Token != req.Token || session.draftGuard == req.Editing {
			return
		}
		if req.Editing {
			if !m.lifecycle.beginDraft() {
				return
			}
		} else {
			m.lifecycle.finishDraft()
		}
		session.draftGuard = req.Editing
		return
	}
	if r.prReview != nil && r.prReview.state.Submitting {
		m.emitPRReview(id)
		return
	}
	if req.Operation == "pr-review-load" {
		if r.prReview != nil && r.prReview.draftGuard {
			return
		}
		if r.reviewEditing || r.reviewSaving || req.PRLoadID == 0 {
			return
		}
		if r.prReview != nil && r.prReview.cancel != nil {
			r.prReview.cancel()
		}
		if r.prReview != nil && r.prReview.anchorCancel != nil {
			r.prReview.anchorCancel()
		}
		session := &prReviewWindow{state: prReviewState{LoadID: req.PRLoadID, Loading: true}}
		r.prReview = session
		site := m.runtime[r.state.SiteID]
		if site == nil || !site.Ready || site.Site.Path != r.state.Path || site.Git.Branch != r.state.Branch {
			session.state.Loading = false
			session.state.Error = "This Site changed. Refresh the branch before opening its PR."
			m.emitPRReview(id)
			return
		}
		link, ok := requestedPR(site.Git, req.PRNumber)
		if !ok {
			session.state.Loading = false
			session.state.Error = "This branch has no matching open PR. Refresh its status first."
			m.emitPRReview(id)
			return
		}
		branch := r.state.Branch
		ctx, cancel := context.WithTimeout(m.app.Context(), 2*time.Minute)
		session.cancel = cancel
		m.emitPRReview(id)
		go func() {
			defer cancel()
			snapshot, err := loadPRReview(ctx, link, branch)
			m.send(func() {
				if m.viewers[id] != r || r.prReview != session {
					return
				}
				session.cancel = nil
				session.state.Loading = false
				if err != nil {
					session.state.Error = err.Error()
				} else {
					snapshot.Token = newProfileID()
					session.state.Snapshot = snapshot
					session.state.Pending = snapshot.Pending
				}
				m.emitPRReview(id)
			})
		}()
		return
	}
	session := r.prReview
	if req.Operation == "pr-review-comment" || req.Operation == "pr-review-comment-check" || req.Operation == "pr-review-comment-retry" {
		m.changePRReviewComment(id, req)
		return
	}
	if req.Operation == "pr-review-anchor" {
		if session == nil || session.state.Snapshot == nil || req.PRAnchor == nil || req.Token != session.state.Snapshot.Token {
			return
		}
		a := *req.PRAnchor
		if a.ID == 0 || a.ID <= session.anchorID {
			return
		}
		session.anchorID = a.ID
		if session.anchorCancel != nil {
			session.anchorCancel()
		}
		ctx, cancel := context.WithTimeout(m.app.Context(), 30*time.Second)
		session.anchorCancel = cancel
		snapshot, root := session.state.Snapshot, r.state.root
		go func() {
			defer cancel()
			location, err := resolvePRReviewAnchor(ctx, root, snapshot, a)
			m.send(func() {
				if m.viewers[id] != r || r.prReview != session || session.anchorID != a.ID {
					return
				}
				session.anchorCancel = nil
				result := &PRReviewAnchorResult{ID: a.ID, Location: location}
				if err != nil {
					result.Error = err.Error()
				}
				session.state.Anchor = result
				m.emitPRReview(id)
			})
		}()
		return
	}
	if session == nil || session.state.Loading || session.state.Snapshot == nil || req.PRSubmission == nil {
		return
	}
	snapshot := session.state.Snapshot
	submission := *req.PRSubmission
	if err := validatePRReviewSubmission(snapshot, submission); err != nil {
		session.state.RequestID = submission.RequestID
		session.state.Unknown = false
		session.state.Error = err.Error()
		m.emitPRReview(id)
		return
	}
	key := fmt.Sprintf("%s#%d", snapshot.Repo, snapshot.Number)
	if m.prReviewPublishing[key] {
		session.state.RequestID = submission.RequestID
		session.state.Unknown = false
		session.state.Error = "A review for this PR is being submitted in another window. Wait for it to finish."
		m.emitPRReview(id)
		return
	}
	if !m.lifecycle.begin() {
		return
	}
	if m.prReviewPublishing == nil {
		m.prReviewPublishing = make(map[string]bool)
	}
	m.prReviewPublishing[key] = true
	checkOnly := req.Operation == "pr-review-check" || (req.Operation != "pr-review-retry" && session.state.Unknown && session.state.RequestID == submission.RequestID)
	session.state.RequestID = submission.RequestID
	session.state.Submitting = true
	session.state.Error = ""
	session.state.ReceiptID = 0
	m.emitPRReview(id)
	go func() {
		defer m.lifecycle.finish()
		ctx, cancel := context.WithTimeout(m.app.Context(), 2*time.Minute)
		defer cancel()
		receipt, unknown, err := submitPRReview(ctx, snapshot, submission, checkOnly)
		m.send(func() {
			delete(m.prReviewPublishing, key)
			if m.viewers[id] != r || r.prReview != session {
				return
			}
			session.state.Submitting = false
			session.state.Unknown = unknown
			if err != nil {
				session.state.Error = err.Error()
			} else if receipt != nil {
				session.state.ReceiptID = receipt.ID
				session.state.Pending = nil
			}
			m.emitPRReview(id)
		})
	}()
}

func (m *siteManager) changePRReviewComment(id string, req viewerRequest) {
	r := m.viewers[id]
	if r == nil || r.prReview == nil || r.prReview.state.Loading || r.prReview.state.Snapshot == nil || req.PRComment == nil {
		return
	}
	session, change := r.prReview, *req.PRComment
	if change.Token != session.state.Snapshot.Token {
		return
	}
	key := fmt.Sprintf("%s#%d", session.state.Snapshot.Repo, session.state.Snapshot.Number)
	if m.prReviewPublishing[key] {
		session.state.Comment = &PRCommentResult{RequestID: change.RequestID, Error: "This review is being changed in another window. Try again when it finishes."}
		m.emitPRReview(id)
		return
	}
	if !m.lifecycle.begin() {
		return
	}
	if m.prReviewPublishing == nil {
		m.prReviewPublishing = make(map[string]bool)
	}
	m.prReviewPublishing[key] = true
	checkOnly := req.Operation == "pr-review-comment-check" || (req.Operation != "pr-review-comment-retry" && session.state.Comment != nil && session.state.Comment.RequestID == change.RequestID && session.state.Comment.Unknown)
	session.state.Submitting, session.state.Error = true, ""
	session.state.Comment = &PRCommentResult{RequestID: change.RequestID}
	m.emitPRReview(id)
	snapshot := session.state.Snapshot
	go func() {
		defer m.lifecycle.finish()
		ctx, cancel := context.WithTimeout(m.app.Context(), 2*time.Minute)
		defer cancel()
		pending, unknown, err := changePendingPRComment(ctx, snapshot, change, checkOnly)
		m.send(func() {
			delete(m.prReviewPublishing, key)
			if m.viewers[id] != r || r.prReview != session {
				return
			}
			session.state.Submitting = false
			if pending != nil {
				session.state.Pending = pending
			}
			result := &PRCommentResult{RequestID: change.RequestID, Unknown: unknown, Done: err == nil}
			if err != nil {
				result.Error = err.Error()
			}
			session.state.Comment = result
			m.emitPRReview(id)
		})
	}()
}
