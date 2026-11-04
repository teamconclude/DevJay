package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBranchHistoryOrderPreferencePersistsAcrossProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"sites":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := loadSites(path)
	if err != nil || c.BranchHistoryOldestFirst {
		t.Fatal("old configurations must default to newest first", err)
	}
	const firstProfile, secondProfile = "11111111111111111111111111111111", "22222222222222222222222222222222"
	c.ActiveProfile = firstProfile
	c.Profiles = []Profile{{ID: firstProfile, Name: "One"}, {ID: secondProfile, Name: "Two"}}
	for _, oldest := range []bool{true, false} {
		next := c
		next.BranchHistoryOldestFirst = oldest
		next = captureProfileChanges(c, next, []string{secondProfile})
		if !reflect.DeepEqual(next.Profiles, c.Profiles) || applyProfileSettings(next, next.Profiles[1].Settings).BranchHistoryOldestFirst != oldest {
			t.Fatal("sort order must be a global user preference")
		}
		if err := saveSites(path, next); err != nil {
			t.Fatal(err)
		}
		c, err = loadSites(path)
		if err != nil || c.BranchHistoryOldestFirst != oldest {
			t.Fatal("sort order did not survive restart", err)
		}
	}
}

func TestBranchHistoryOrderAcrossPagesAndRefresh(t *testing.T) {
	ctx := context.Background()
	repo, _ := rebaseFixture(t, false)
	ids := []string{fixtureGit(t, repo, "rev-parse", "HEAD")}
	tree := fixtureGit(t, repo, "rev-parse", "HEAD^{tree}")
	for i := 0; i < 2*viewerPageSize+2; i++ {
		ids = append(ids, fixtureGit(t, repo, "commit-tree", tree, "-p", ids[len(ids)-1], "-m", "branch work"))
	}
	fixtureGit(t, repo, "update-ref", "refs/heads/feature", ids[len(ids)-1])
	before := checkoutFingerprint(t, repo)
	v := GitView{Path: repo}
	setViewMode(&v, "branch")
	if err := loadGitView(ctx, &v, true); err != nil {
		t.Fatal(err)
	}
	if v.OldestFirst || v.Commits[0].ID != ids[len(ids)-1] {
		t.Fatal("newest first is not the default")
	}
	if err := selectViewHistoryOrder(ctx, &v, "oldest"); err != nil {
		t.Fatal(err)
	}
	var seen []string
	for page := 0; page < 3; page++ {
		v.Page = page
		if err := loadGitView(ctx, &v, false); err != nil {
			t.Fatal(err)
		}
		if v.More != (page < 2) || v.Commit != ids[page*viewerPageSize] {
			t.Fatalf("wrong oldest-first page %d: more=%v selected=%s", page, v.More, v.Commit)
		}
		for _, commit := range v.Commits {
			seen = append(seen, commit.ID)
		}
	}
	if !reflect.DeepEqual(seen, ids) || !reflect.DeepEqual(before, checkoutFingerprint(t, repo)) {
		t.Fatal("oldest-first pages skipped/repeated commits or changed the checkout")
	}
	v.Page = 1
	if err := loadGitView(ctx, &v, false); err != nil {
		t.Fatal(err)
	}
	selected := v.Commit
	// New commits append in oldest-first order; selection stays on its page.
	newTip := fixtureGit(t, repo, "commit-tree", tree, "-p", ids[len(ids)-1], "-m", "new work")
	fixtureGit(t, repo, "update-ref", "refs/heads/feature", newTip)
	if err := refreshGitView(ctx, &v); err != nil || v.Page != 1 || v.Commit != selected || !v.OldestFirst {
		t.Fatalf("new commits displaced oldest-first selection: page=%d, %v", v.Page, err)
	}
	// Advancing the base removes the first branch commit, moving the selected
	// row across a page boundary. Recovery must search in the chosen order.
	fixtureGit(t, repo, "update-ref", "refs/remotes/origin/develop", ids[0])
	if err := refreshGitView(ctx, &v); err != nil || v.Page != 0 || v.Commit != selected || v.HistoryNote != "" {
		t.Fatalf("base change lost selection: page=%d note=%s, %v", v.Page, v.HistoryNote, err)
	}
	if err := selectViewHistoryScope(ctx, &v, "full"); err != nil || v.Commits[0].ID != newTip {
		t.Fatal("oldest-first preference changed full history", err)
	}
	if err := selectViewHistoryOrder(ctx, &v, "newest"); err == nil || !v.OldestFirst {
		t.Fatal("sorting was accepted from full history")
	}
	if err := selectViewHistoryScope(ctx, &v, "branch"); err != nil || v.Commits[0].ID != ids[1] {
		t.Fatal("returning to branch history lost the order", err)
	}
	if err := selectViewHistoryOrder(ctx, &v, "invalid"); err == nil || !v.OldestFirst {
		t.Fatal("invalid sort order accepted")
	}
	v.Page = 1
	if err := selectViewHistoryOrder(ctx, &v, "newest"); err != nil || v.Page != 0 || v.Commits[0].ID != newTip || v.OldestFirst {
		t.Fatal("switching order did not return to the first page", err)
	}
}
