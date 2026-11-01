package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewWindowsAreNeverReusedForOrdinaryHistory(t *testing.T) {
	changes := &gitViewerWindow{used: 2, state: &GitView{SiteID: "site", Kind: "edits"}}
	review := &gitViewerWindow{used: 3, state: &GitView{SiteID: "site", Kind: "review"}}
	m := siteManager{viewers: map[string]*gitViewerWindow{"changes": changes, "review": review}}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "branch"}) != changes {
		t.Fatal("history reused a dedicated review window")
	}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "review"}) != review {
		t.Fatal("review reused the Changes window")
	}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "review", NewWindow: true}) != nil {
		t.Fatal("explicit review new-window request reused a window")
	}
	m.viewerMode("review", "edits")
	m.viewerMode("changes", "review")
	if review.state.Kind != "review" || changes.state.Kind != "edits" {
		t.Fatal("mode request changed the dedicated surface")
	}
}

func TestReviewEditingCancelsRefreshAndPreservesWindowUntilResolved(t *testing.T) {
	cancelled := false
	r := &gitViewerWindow{state: &GitView{Kind: "review", Version: 1, Files: []ViewFile{{Path: "file"}}, Review: &BranchReview{Selected: &BranchReviewFile{Editable: true, Token: "contents"}}}, refreshCancel: func() { cancelled = true }}
	r.refresh.start()
	m := siteManager{viewers: map[string]*gitViewerWindow{"review": r}}
	m.handleReviewRequest("review", viewerRequest{Operation: "review-edit", Editing: true, Token: "contents"})
	if !r.reviewEditing || !r.state.ReviewEditing || !cancelled || r.refresh.running || !m.lifecycle.hasDrafts() {
		t.Fatal("edit did not suspend refresh and register its draft")
	}
	m.handleReviewRequest("review", viewerRequest{Operation: "review-edit", Editing: true, Token: "contents"})
	m.requestCloseViewer("review")
	if m.viewers["review"] != r || !strings.Contains(r.state.Error, "Save or cancel") {
		t.Fatal("close lost the draft instead of explaining how to finish editing")
	}
	m.closeViewer("review") // Profile/Site lifecycle closure must also retain drafts.
	if m.viewers["review"] != r {
		t.Fatal("forced close lost the draft")
	}
	m.handleReviewRequest("review", viewerRequest{Operation: "review-edit", Editing: false})
	if r.reviewEditing || r.state.ReviewEditing || m.lifecycle.hasDrafts() {
		t.Fatal("ending an edit leaked the draft guard")
	}
}

func TestReviewRefreshCannotReplaceAnActiveEditOrSave(t *testing.T) {
	for _, saving := range []bool{false, true} {
		r := &gitViewerWindow{ready: true, reviewEditing: !saving, reviewSaving: saving, generation: 3, state: &GitView{Kind: "review", Version: 3}}
		m := siteManager{viewers: map[string]*gitViewerWindow{"review": r}, monitor: monitorSchedule{visibility: map[string]bool{"review": true}}}
		r.refresh.pending = true
		if !m.nextViewerRefresh(time.Now()).IsZero() {
			t.Fatal("editing review scheduled background refresh")
		}
		epoch := r.refresh.start()
		if r.completeViewerRefresh(epoch, 3, GitView{Kind: "edits"}, nil, false, time.Now()) || r.state.Kind != "review" {
			t.Fatal("in-flight refresh overwrote a review edit")
		}
	}
}

func TestReviewSaveIgnoresNavigationAndEditCancellationWhileWriting(t *testing.T) {
	r := &gitViewerWindow{reviewEditing: true, reviewSaving: true, state: &GitView{Kind: "review", Loading: true}}
	m := siteManager{viewers: map[string]*gitViewerWindow{"review": r}}
	for _, operation := range []string{"refresh", "review-file", "review-options", "review-edit", "review-save", "mode"} {
		m.handleReviewRequest("review", viewerRequest{Operation: operation})
		if !r.reviewEditing || !r.reviewSaving || !r.state.Loading {
			t.Fatalf("%s interrupted save", operation)
		}
	}
}

func TestReviewEditingRejectsStaleContentsBeforeProtectingDraft(t *testing.T) {
	r := &gitViewerWindow{state: &GitView{Kind: "review", Files: []ViewFile{{Path: "selected"}}, Review: &BranchReview{Selected: &BranchReviewFile{Editable: true, Token: "current"}}}}
	m := siteManager{viewers: map[string]*gitViewerWindow{"review": r}}
	for _, token := range []string{"", "previous"} {
		m.handleReviewRequest("review", viewerRequest{Operation: "review-edit", Editing: true, Token: token})
		if r.reviewEditing || r.state.ReviewEditing || m.lifecycle.hasDrafts() {
			t.Fatal("stale file contents received an edit-session acknowledgement")
		}
	}
	m.handleReviewRequest("review", viewerRequest{Operation: "review-edit", Editing: true, Token: "current"})
	if !r.reviewEditing || !r.state.ReviewEditing || !m.lifecycle.hasDrafts() {
		t.Fatal("current file contents did not enter a protected edit session")
	}
}

func TestReviewEditorRecoveryDefersWithoutConsumingRetryBudget(t *testing.T) {
	now := time.Now()
	r := &gitViewerWindow{reviewEditing: true, state: &GitView{Kind: "review"}}
	m := siteManager{viewers: map[string]*gitViewerWindow{"review": r}}
	m.recovery.add("review")
	m.recovery.add("changes")
	m.recovery.manual(now.Add(-time.Minute))
	m.deferReviewEditorRecovery(now)
	reload, failed := m.recovery.tick(now)
	if len(reload) != 1 || reload[0] != "changes" || len(failed) != 0 || m.recovery.views["review"].attempts != 0 {
		t.Fatal("recovery destroyed the active draft or consumed its retry budget")
	}
	r.reviewEditing = false
	m.deferReviewEditorRecovery(now.Add(uiRecoveryTimeout))
	reload, _ = m.recovery.tick(now.Add(uiRecoveryTimeout))
	found := false
	for _, id := range reload {
		found = found || id == "review"
	}
	if !found {
		t.Fatal("finishing editing left recovery permanently disabled")
	}
}

func TestReviewContentEqualityIgnoresPrivateMetadataButRefreshesSaveGuards(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Unix(500, 0), time.Unix(600, 0)); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := GitView{Kind: "review", Version: 4, Captured: "visible snapshot", Review: &BranchReview{rootInfo: beforeInfo, Selected: &BranchReviewFile{Path: "file", NewText: "unchanged", Token: "same", info: beforeInfo}}}
	after := before
	after.Captured = "later read"
	after.Review = &BranchReview{rootInfo: afterInfo, Selected: &BranchReviewFile{Path: "file", NewText: "unchanged", Token: "same", info: afterInfo}}
	if !gitViewContentEqual(before, after) || before.Review.rootInfo == nil || before.Review.Selected.info == nil {
		t.Fatal("private stat metadata changed visible equality or comparison mutated a snapshot")
	}
	r := gitViewerWindow{generation: 4, state: &before}
	epoch := r.refresh.start()
	if r.completeViewerRefresh(epoch, 4, after, nil, false, time.Now()) || r.state.Version != 4 || r.state.Captured != before.Captured {
		t.Fatal("private save guard refresh invalidated the frontend snapshot")
	}
	if r.state.Review.Selected.info != afterInfo {
		t.Fatal("backend retained obsolete private save guards")
	}
	after.Review.Selected.Token = "changed"
	if gitViewContentEqual(before, after) {
		t.Fatal("new visible save token was ignored")
	}
}

func TestQuitWithReviewDraftCanBeRetriedAfterSavingOrCancelling(t *testing.T) {
	var life actionLifetime
	if !life.beginDraft() || !life.beginDraft() {
		t.Fatal("draft admission failed")
	}
	if ready, drained := life.quit(); ready || drained != nil {
		t.Fatal("quit accepted unsaved review drafts")
	}
	if !life.begin() {
		t.Fatal("rejected quit prevented the user from saving")
	}
	life.finish()
	life.finishDraft()
	if ready, _ := life.quit(); ready {
		t.Fatal("one completed draft released another draft")
	}
	life.finishDraft()
	if ready, _ := life.quit(); !ready {
		t.Fatal("resolved drafts kept app from quitting")
	}
	if life.beginDraft() {
		t.Fatal("new draft admitted after quit")
	}
}
