package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestViewerRefreshPreservesFileGroupAndReadsSameCountChanges(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	writeFixture(t, repo, "file", "staged\n")
	fixtureGit(t, repo, "add", "file")
	writeFixture(t, repo, "file", "first working contents\n")
	v := GitView{Path: repo, Kind: "edits"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	selected := -1
	for i, file := range v.Files {
		if file.Path == "file" && file.Group == "Unstaged" {
			selected = i
		}
	}
	if selected < 0 {
		t.Fatal("missing unstaged fixture")
	}
	selectViewFile(context.Background(), &v, selected)
	writeFixture(t, repo, "file", "second working contents\n")
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Counts.Edits != 1 || v.Files[v.File].Group != "Unstaged" || !strings.Contains(v.Diff, "+second working contents") {
		t.Fatalf("same-count edit did not refresh the selected unstaged file: %+v", v)
	}
	writeFixture(t, repo, "aaa", "new earlier path\n")
	fixtureGit(t, repo, "add", "aaa")
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.File == selected || v.Files[v.File].Path != "file" || v.Files[v.File].Group != "Unstaged" {
		t.Fatalf("selection followed a list index instead of the file: %+v", v)
	}
}

func TestViewerRefreshFallsBackWhenSelectedFileDisappears(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	writeFixture(t, repo, "a-new", "first\n")
	writeFixture(t, repo, "z-new", "second\n")
	v := GitView{Path: repo, Kind: "edits"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	selectViewFile(context.Background(), &v, 1)
	if err := os.Remove(filepath.Join(repo, "z-new")); err != nil {
		t.Fatal(err)
	}
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.File != 0 || v.Files[v.File].Path != "a-new" || !strings.Contains(v.Diff, "+first") {
		t.Fatalf("missing file left a stale selection: %+v", v)
	}
}

func TestViewerRefreshPreservesCommitAndItsFile(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	for _, name := range []string{"a-local", "z-local"} {
		writeFixture(t, repo, name, name+"\n")
	}
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "selected commit")
	selected := fixtureGit(t, repo, "rev-parse", "HEAD")
	v := GitView{Path: repo, Kind: "local"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	selectViewFile(context.Background(), &v, 1)
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "newer commit")
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Commit != selected || v.File != 1 || v.Files[v.File].Path != "z-local" || len(v.Commits) != 2 {
		t.Fatalf("new commit displaced the existing selection: %+v", v)
	}
	fixtureGit(t, repo, "switch", "-c", "another-branch")
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Branch != "another-branch" || v.Commit == selected || v.Page != 0 {
		t.Fatalf("new branch retained an unrelated selection: %+v", v)
	}
}

func TestViewerRefreshPreservesHistoryPageAndHandlesRemovedPage(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	tip := fixtureGit(t, repo, "rev-parse", "HEAD")
	original := tip
	tree := fixtureGit(t, repo, "rev-parse", "HEAD^{tree}")
	for i := 0; i < viewerPageSize+1; i++ {
		tip = fixtureGit(t, repo, "commit-tree", tree, "-p", tip, "-m", "history")
	}
	fixtureGit(t, repo, "update-ref", "refs/remotes/origin/feature", tip)
	v := GitView{Path: repo, Kind: "remote"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	v.Page = 1
	if err := loadGitView(context.Background(), &v, false); err != nil {
		t.Fatal(err)
	}
	selected := v.Commit
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Page != 1 || v.Commit != selected {
		t.Fatalf("refresh reset the history page: page=%d commit=%s", v.Page, v.Commit)
	}
	fixtureGit(t, repo, "update-ref", "refs/remotes/origin/feature", original)
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Page != 0 || len(v.Commits) == 0 || v.Commit != original {
		t.Fatalf("removed history page did not fall back: %+v", v)
	}
	if !strings.Contains(v.HistoryNote, "previously selected commit") {
		t.Fatal("removed selection changed without an explanation")
	}
}

func TestViewerRefreshFollowsCommitAcrossHistoryPageBoundaries(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	tip := fixtureGit(t, repo, "rev-parse", "HEAD")
	tree := fixtureGit(t, repo, "rev-parse", "HEAD^{tree}")
	add := func(count int) {
		for i := 0; i < count; i++ {
			tip = fixtureGit(t, repo, "commit-tree", tree, "-p", tip, "-m", "history")
		}
		fixtureGit(t, repo, "update-ref", "refs/remotes/origin/feature", tip)
	}
	add(viewerPageSize + 3)
	oldTip := tip
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	selected := v.Commits[viewerPageSize-1].ID
	if err := selectViewCommit(context.Background(), &v, selected); err != nil {
		t.Fatal(err)
	}
	add(1)
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Page != 1 || v.Commit != selected || v.Commits[0].ID != selected || v.HistoryNote != "" {
		t.Fatalf("one new commit displaced the last row instead of following it to page 2: page=%d commit=%s", v.Page, v.Commit)
	}
	// A batch larger than the neighboring-page search must use the count-change
	// prediction, without scanning every intervening history page.
	add(3*viewerPageSize + 3)
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Page != 4 || v.Commit != selected || v.Commits[3].ID != selected || v.HistoryNote != "" {
		t.Fatalf("large incoming batch displaced the selection: page=%d commit=%s", v.Page, v.Commit)
	}
	// A shorter upstream can move a retained commit back to an earlier page.
	fixtureGit(t, repo, "update-ref", "refs/remotes/origin/feature", oldTip)
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Page != 0 || v.Commit != selected || v.Commits[viewerPageSize-1].ID != selected || v.HistoryNote != "" {
		t.Fatalf("shorter history displaced the retained selection: page=%d commit=%s", v.Page, v.Commit)
	}
}

func TestViewerRefreshCoalescesChangesAndWaitsForIdle(t *testing.T) {
	now := time.Unix(100, 0)
	s := viewerRefreshSchedule{lastFinished: now}
	if s.due(now, true, false, false) {
		t.Fatal("immediate repeated poll")
	}
	s.pending = true
	if s.due(now, true, true, false) || s.due(now, true, false, true) || s.due(now, false, false, false) {
		t.Fatal("refresh interrupted loading, sleep, or window initialization")
	}
	if !s.due(now, true, false, false) {
		t.Fatal("card change was not scheduled")
	}
	epoch := s.start()
	for i := 0; i < 20; i++ {
		s.pending = true
		if s.due(now.Add(time.Hour), true, false, false) {
			t.Fatal("queued another read while a refresh was running")
		}
	}
	if !s.finish(epoch, now.Add(time.Second)) || !s.due(now.Add(time.Second), true, false, false) {
		t.Fatal("change during refresh was lost")
	}
	epoch = s.start()
	if !s.finish(epoch, now.Add(2*time.Second)) || s.due(now.Add(11*time.Second), true, false, false) || !s.due(now.Add(12*time.Second), true, false, false) {
		t.Fatal("periodic refresh did not use completion time")
	}
}

func TestViewerRefreshRejectsCancelledAndStaleSelectionResults(t *testing.T) {
	now := time.Unix(100, 0)
	r := &gitViewerWindow{generation: 4, state: &GitView{Version: 4, Diff: "selected"}}
	epoch := r.refresh.start()
	r.cancelViewerRefresh()
	newEpoch := r.refresh.start()
	if r.completeViewerRefresh(epoch, 4, GitView{Diff: "cancelled"}, nil, false, now) || !r.refresh.running {
		t.Fatal("cancelled result replaced a newer refresh")
	}
	r.generation++ // A file or mode selection completed while the read was running.
	if r.completeViewerRefresh(newEpoch, 4, GitView{Diff: "stale"}, nil, false, now) || r.state.Diff != "selected" || !r.refresh.pending {
		t.Fatal("background refresh overwrote the user's newer selection")
	}
	for _, busy := range []string{"loading", "editor", "paused"} {
		r.state.Loading = busy == "loading"
		r.editorOpening = busy == "editor"
		epoch = r.refresh.start()
		if r.completeViewerRefresh(epoch, r.generation, GitView{Diff: "busy"}, nil, busy == "paused", now) || r.state.Diff != "selected" {
			t.Fatalf("background refresh interrupted %s", busy)
		}
	}
}

func TestViewerRefreshPausingCancelsBackgroundReads(t *testing.T) {
	cancelled := 0
	r := &gitViewerWindow{
		ready: true, state: &GitView{Kind: "edits"},
		refreshCancel: func() { cancelled++ },
	}
	epoch := r.refresh.start()
	m := &siteManager{recovery: uiRecovery{paused: true}, viewers: map[string]*gitViewerWindow{"viewer": r}}
	m.refreshViewers(time.Now())
	if cancelled != 1 || r.refresh.running || !r.refresh.pending || r.refresh.epoch == epoch {
		t.Fatal("suspending the session did not cancel and defer its background read")
	}
	m.refreshViewers(time.Now())
	if cancelled != 1 {
		t.Fatal("paused ticks kept cancelling the same read")
	}
}

func TestViewerRefreshSuppressesUnchangedSnapshotsAndPreservesReadErrors(t *testing.T) {
	now := time.Unix(100, 0)
	r := &gitViewerWindow{generation: 3, state: &GitView{Version: 3, Diff: "selected", Captured: "earlier", Editor: &EditorState{}}}
	before := r.state
	next := *before
	next.Captured = "later"
	epoch := r.refresh.start()
	if r.completeViewerRefresh(epoch, 3, next, nil, false, now) || r.state != before || r.generation != 3 {
		t.Fatal("unchanged refresh would replace the DOM or invalidate a click")
	}
	epoch = r.refresh.start()
	if !r.completeViewerRefresh(epoch, 3, GitView{}, errors.New("transient"), false, now) || r.state.Diff != "selected" || r.state.Error != "transient" {
		t.Fatal("failed read erased the last readable selection")
	}
	next = *r.state
	next.Diff, next.Error = "new contents", ""
	epoch = r.refresh.start()
	if !r.completeViewerRefresh(epoch, r.generation, next, nil, false, now) || r.state.Diff != "new contents" || r.state.Error != "" || r.state.Version != 5 {
		t.Fatal("successful refresh did not clear the error and advance version")
	}
}

func TestViewerRefreshIgnoresPollTimestampsButNotCardChanges(t *testing.T) {
	before := GitStatus{Branch: "feature", Changes: 2, Checked: "old", RemoteChecked: "old"}
	after := before
	after.Checked, after.RemoteChecked = "new", "new"
	if gitStatusChangedForViewer(before, after) {
		t.Fatal("poll timestamp requested a refresh")
	}
	after.Changes++
	if !gitStatusChangedForViewer(before, after) {
		t.Fatal("changed card did not request a refresh")
	}
}

func TestViewerReusePrefersSameModeThenRecentAndHonorsNewWindow(t *testing.T) {
	old := &gitViewerWindow{used: 1, state: &GitView{SiteID: "site", Kind: "edits"}}
	recent := &gitViewerWindow{used: 2, state: &GitView{SiteID: "site", Kind: "local"}}
	other := &gitViewerWindow{used: 3, state: &GitView{SiteID: "other", Kind: "remote"}}
	m := &siteManager{viewers: map[string]*gitViewerWindow{"old": old, "recent": recent, "other": other}}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "edits"}) != old || m.existingViewer(viewerRequest{SiteID: "site", Kind: "remote"}) != recent {
		t.Fatal("viewer reuse selected the wrong mode or Site")
	}
	if m.existingViewer(viewerRequest{SiteID: "site", Kind: "edits", NewWindow: true}) != nil {
		t.Fatal("explicit new window request was reused")
	}
}

func TestViewerReopenUsesLatestParentAndCardAnchor(t *testing.T) {
	site := &application.WebviewWindow{}
	strip := &application.WebviewWindow{}
	r := &gitViewerWindow{parent: site, state: &GitView{SiteID: "site", Kind: "edits"}}
	m := &siteManager{viewers: map[string]*gitViewerWindow{"viewer": r}}
	anchor := &cardAnchor{X: 240, Y: 10, Width: 216, Height: 152}
	// A not-yet-ready viewer must also use the latest source when first shown.
	m.openViewer(viewerRequest{SiteID: "site", Kind: "edits", Anchor: anchor}, strip, nil)
	if r.parent != strip || r.anchor != anchor || len(m.viewers) != 1 {
		t.Fatal("reused viewer retained its old Site placement")
	}
	m.openViewer(viewerRequest{SiteID: "site", Kind: "edits"}, site, nil)
	if r.parent != site || r.anchor != nil {
		t.Fatal("reused viewer retained its old card placement")
	}
	m.openViewer(viewerRequest{SiteID: "site", Kind: "edits", Anchor: &cardAnchor{Width: -1}}, strip, nil)
	if r.parent != site || r.anchor != nil {
		t.Fatal("invalid card bounds replaced the viewer source")
	}
}
