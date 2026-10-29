package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestViewerBranchDiffUsesMergeBaseAndPreservesPinnedSelection(t *testing.T) {
	ctx := context.Background()
	repo, _ := rebaseFixture(t, false)
	base := fixtureGit(t, repo, "rev-parse", "origin/develop")
	fixtureGit(t, repo, "fetch", "origin") // The default branch has advanced independently.
	writeFixture(t, repo, "local.txt", "committed local work\n")
	fixtureGit(t, repo, "add", "local.txt")
	fixtureGit(t, repo, "commit", "-m", "local work")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	writeFixture(t, repo, "file", "uncommitted work must stay separate\n")
	before := checkoutFingerprint(t, repo)
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(ctx, &v, true); err != nil {
		t.Fatal(err)
	}
	if v.BranchDiff == nil || v.BranchDiff.Base != base || v.BranchDiff.Target != "origin/develop" {
		t.Fatalf("wrong captured base: %+v", v.BranchDiff)
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil {
		t.Fatal(err)
	}
	if !v.AllChanges || v.Commit != "" || v.Parent != base || len(v.Files) != 2 {
		t.Fatalf("wrong branch comparison: %+v", v)
	}
	if !v.BranchOnly || v.BranchCommits == nil || *v.BranchCommits != 2 || len(v.Commits) != 2 {
		t.Fatalf("branch list includes shared history: %+v", v)
	}
	for _, commit := range v.Commits {
		if commit.ID == base {
			t.Fatal("comparison base appeared among branch commits")
		}
	}
	for i, file := range v.Files {
		if file.Path != "feature" && file.Path != "local.txt" {
			t.Fatalf("default-branch or working-tree change leaked into branch diff: %+v", file)
		}
		selectViewFile(ctx, &v, i)
		if !file.StatsKnown || file.Added != 1 || file.Removed != 0 || !strings.Contains(v.Diff, "+") {
			t.Fatalf("branch file statistics/diff: %+v, %s", file, v.Diff)
		}
	}
	if err := selectViewCommit(ctx, &v, head); err != nil || v.AllChanges || len(v.Files) != 1 || v.Files[0].Path != "local.txt" {
		t.Fatalf("individual commit selection: %+v, %v", v, err)
	}
	if !v.BranchOnly || len(v.Commits) != 2 {
		t.Fatal("selecting a commit restored the full history")
	}
	if err := selectViewCommit(ctx, &v, "--all"); err == nil {
		t.Fatal("unlisted revision accepted")
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil {
		t.Fatal(err)
	}
	selectViewFile(ctx, &v, 1)
	if !reflect.DeepEqual(before, checkoutFingerprint(t, repo)) {
		t.Fatal("reading branch differences changed the checkout, index, refs, or configuration")
	}
	writeFixture(t, repo, "another.txt", "later commit\n")
	fixtureGit(t, repo, "add", "another.txt")
	fixtureGit(t, repo, "commit", "-m", "later work")
	if err := selectViewBranchDiff(ctx, &v); err != nil || v.Head != head || len(v.Files) != 2 {
		t.Fatalf("comparison followed a moving ref: %+v, %v", v, err)
	}
	selectViewFile(ctx, &v, 1) // local.txt moves from index 1 to 2 after refresh.
	before = checkoutFingerprint(t, repo)
	if err := refreshGitView(ctx, &v); err != nil {
		t.Fatal(err)
	}
	if !v.AllChanges || v.Head == head || len(v.Files) != 3 || v.Files[v.File].Path != "local.txt" || v.File != 2 {
		t.Fatalf("branch selection not preserved by refresh: %+v", v)
	}
	if v.BranchCommits == nil || *v.BranchCommits != 3 || len(v.Commits) != 3 {
		t.Fatalf("refresh restored shared history: %+v", v.Commits)
	}
	if !reflect.DeepEqual(before, checkoutFingerprint(t, repo)) {
		t.Fatal("refresh changed the checkout")
	}
}

func TestViewerBranchDiffSupportsUnpublishedAndEmptyBranches(t *testing.T) {
	ctx := context.Background()
	repo, _ := rebaseFixture(t, false)
	fixtureGit(t, repo, "branch", "--unset-upstream")
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(ctx, &v, true); err != nil {
		t.Fatal(err)
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil || len(v.Files) != 1 || v.Files[0].Path != "feature" {
		t.Fatalf("published branch work disappeared without tracking: %+v, %v", v, err)
	}
	fixtureGit(t, repo, "switch", "develop")
	if err := refreshGitView(ctx, &v); err != nil {
		t.Fatal(err)
	}
	if !v.AllChanges || v.BranchDiff.Target != "origin/develop" || v.Parent != v.Head || len(v.Files) != 0 || v.File != -1 || v.Diff != "" {
		t.Fatalf("empty default-branch comparison: %+v", v)
	}
	if len(v.Commits) != 0 || v.BranchCommits == nil || *v.BranchCommits != 0 || v.More {
		t.Fatal("empty branch comparison showed repository history")
	}
}

func TestViewerBranchDiffUnavailableBaseDoesNotReuseAnOldDiff(t *testing.T) {
	ctx := context.Background()
	repo, _ := rebaseFixture(t, false)
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(ctx, &v, true); err != nil {
		t.Fatal(err)
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	if err := refreshGitView(ctx, &v); err == nil || v.BranchDiff.Base != "" || v.BranchDiff.Note == "" || v.Diff != "" || len(v.Files) != 0 {
		t.Fatalf("missing base reused stale contents: %+v, %v", v, err)
	}
	if len(v.Commits) != 0 || !v.BranchOnly {
		t.Fatal("missing base silently fell back to full history")
	}
	// Full history remains available when a branch comparison is unavailable.
	if err := selectViewHistoryScope(ctx, &v, "full"); err != nil || v.BranchOnly || v.AllChanges || v.Diff == "" {
		t.Fatalf("individual history unavailable without base: %+v, %v", v, err)
	}
	v.Kind = "edits"
	if err := selectViewBranchDiff(ctx, &v); err == nil {
		t.Fatal("branch comparison accepted outside History")
	}
}

func TestViewerBranchDiffExcludesSharedAndRemoteOnlyCommits(t *testing.T) {
	ctx := context.Background()
	repo, seed := rebaseFixture(t, false)
	published := fixtureGit(t, repo, "rev-parse", "HEAD")
	base := fixtureGit(t, repo, "rev-parse", "origin/develop")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "remote only")
	remote := fixtureGit(t, seed, "rev-parse", "HEAD")
	fixtureGit(t, seed, "push", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "local only")
	local := fixtureGit(t, repo, "rev-parse", "HEAD")
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(ctx, &v, true); err != nil || len(v.Commits) != 4 {
		t.Fatalf("full history: %+v, %v", v.Commits, err)
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil {
		t.Fatal(err)
	}
	if len(v.Commits) != 2 || v.Commits[0].ID != local || v.Commits[1].ID != published {
		t.Fatalf("wrong branch commits: %+v", v.Commits)
	}
	for _, id := range []string{base, remote} {
		if err := selectViewCommit(ctx, &v, id); err == nil {
			t.Fatal("commit outside branch range accepted")
		}
	}
	if err := selectViewCommit(ctx, &v, published); err != nil {
		t.Fatal(err)
	}
	if err := refreshGitView(ctx, &v); err != nil || !v.BranchOnly || v.AllChanges || v.Commit != published || len(v.Commits) != 2 {
		t.Fatalf("individual branch selection lost on refresh: %+v, %v", v, err)
	}
	if err := selectViewHistoryScope(ctx, &v, "full"); err != nil || v.BranchOnly || len(v.Commits) != 4 {
		t.Fatalf("full history did not return: %+v, %v", v.Commits, err)
	}
}

func TestViewerHistoryDefaultsToBranchAndPreservesExplicitScopeOnRefresh(t *testing.T) {
	ctx := context.Background()
	repo, _ := rebaseFixture(t, false)
	base := fixtureGit(t, repo, "rev-parse", "origin/develop")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	v := GitView{Path: repo}
	setViewMode(&v, "branch")
	if err := loadGitView(ctx, &v, true); err != nil || !v.BranchOnly || v.AllChanges || len(v.Commits) != 1 || v.Commit != head || *v.BranchCommits != 1 {
		t.Fatalf("History did not open with individual branch commits: %+v, %v", v, err)
	}
	if err := selectViewHistoryScope(ctx, &v, "full"); err != nil || v.BranchOnly || len(v.Commits) != 2 {
		t.Fatalf("could not choose Full history: %+v, %v", v, err)
	}
	if err := selectViewCommit(ctx, &v, base); err != nil {
		t.Fatal(err)
	}
	if err := refreshGitView(ctx, &v); err != nil || v.BranchOnly || v.Commit != base || len(v.Commits) != 2 {
		t.Fatalf("refresh reset the chosen full-history scope: %+v, %v", v, err)
	}
	// Scope changes read the pinned snapshot, even if the branch has moved.
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "later branch work")
	if err := selectViewHistoryScope(ctx, &v, "branch"); err != nil || !v.BranchOnly || v.AllChanges || v.Commit != head || len(v.Commits) != 1 {
		t.Fatalf("Branch history required an aggregate diff or followed a moving ref: %+v, %v", v, err)
	}
	before := v
	if err := selectViewHistoryScope(ctx, &v, "HEAD~1"); err == nil || !reflect.DeepEqual(v, before) {
		t.Fatal("invalid history scope accepted or changed the view")
	}
	if err := selectViewHistoryScope(ctx, &v, "full"); err != nil {
		t.Fatal(err)
	}
	// Explicitly reopening History defaults to the branch, even from full history.
	setViewMode(&v, "branch")
	if err := loadGitView(ctx, &v, true); err != nil || !v.BranchOnly || v.AllChanges || len(v.Commits) != 2 {
		t.Fatalf("reopening History kept Full history: %+v, %v", v, err)
	}
	setViewMode(&v, "edits")
	if err := loadGitView(ctx, &v, true); err != nil || v.BranchOnly || v.AllChanges {
		t.Fatalf("Changes retained history scope: %+v, %v", v, err)
	}
	if err := selectViewHistoryScope(ctx, &v, "branch"); err == nil {
		t.Fatal("history scope accepted outside History")
	}
	setViewMode(&v, "branch")
	if err := loadGitView(ctx, &v, true); err != nil || !v.BranchOnly || v.AllChanges || len(v.Commits) != 2 {
		t.Fatalf("returning to History did not default to branch commits: %+v, %v", v, err)
	}
}

func TestViewerBranchDiffPaginationStaysWithinBranchRange(t *testing.T) {
	ctx := context.Background()
	repo, _ := rebaseFixture(t, false)
	first := fixtureGit(t, repo, "rev-parse", "HEAD")
	base := fixtureGit(t, repo, "rev-parse", "origin/develop")
	for i := 0; i < viewerPageSize+1; i++ {
		fixtureGit(t, repo, "commit", "--allow-empty", "-m", "branch work")
	}
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(ctx, &v, true); err != nil {
		t.Fatal(err)
	}
	v.Page = 1
	if err := loadGitView(ctx, &v, false); err != nil {
		t.Fatal(err)
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil || v.Page != 0 || !v.More || len(v.Commits) != viewerPageSize || *v.BranchCommits != viewerPageSize+2 {
		t.Fatalf("enter branch comparison from old history page: %+v, %v", v, err)
	}
	v.Page = 1
	if err := loadGitView(ctx, &v, false); err != nil || !v.BranchOnly || !v.AllChanges || v.More || len(v.Commits) != 2 || *viewCategoryCount(&v) != viewerPageSize+2 {
		t.Fatalf("second branch page: %+v, %v", v, err)
	}
	if err := selectViewCommit(ctx, &v, base); err == nil {
		t.Fatal("shared ancestor appeared on a later page")
	}
	if err := selectViewCommit(ctx, &v, first); err != nil {
		t.Fatal(err)
	}
	if err := refreshGitView(ctx, &v); err != nil || v.Page != 1 || v.Commit != first || !v.BranchOnly {
		t.Fatalf("refresh lost paginated branch selection: %+v, %v", v, err)
	}
	if err := selectViewBranchDiff(ctx, &v); err != nil {
		t.Fatal(err)
	}
	// Shorten only the disposable fixture's branch; all commits have the same tree.
	fixtureGit(t, repo, "update-ref", "refs/heads/feature", first)
	if err := refreshGitView(ctx, &v); err != nil || v.Page != 0 || len(v.Commits) != 1 || *v.BranchCommits != 1 || !v.AllChanges {
		t.Fatalf("removed history page was not clamped: %+v, %v", v, err)
	}
}
