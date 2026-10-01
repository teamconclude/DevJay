package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestViewerShowsStagedUnstagedAndUntrackedWithoutWrites(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	writeFixture(t, repo, "file", "staged line\n")
	fixtureGit(t, repo, "add", "file")
	writeFixture(t, repo, "file", "working line\n")
	fixtureGit(t, repo, "mv", "feature", "renamed\nfeature")
	writeFixture(t, repo, ":(glob)*", "literal path\n")
	fixtureGit(t, repo, "--literal-pathspecs", "add", ":(glob)*")
	writeFixture(t, repo, "new.txt", "new file\n")
	writeFixture(t, repo, "binary", string([]byte{1, 0, 2}))
	outside := filepath.Join(t.TempDir(), "outside")
	if err := os.WriteFile(outside, []byte("DO NOT READ THE TARGET"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(repo, "link")); err != nil {
		t.Fatal(err)
	}
	before := fixtureGit(t, repo, "status", "--porcelain=v2", "-z")
	index, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
	v := GitView{Path: repo, Kind: "edits"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if v.Counts == nil || v.Counts.Edits != 6 {
		t.Fatalf("distinct edited paths: %+v", v.Counts)
	}
	seen := map[string]bool{}
	for i, f := range v.Files {
		selectViewFile(context.Background(), &v, i)
		key := f.Group + ":" + f.Path
		seen[key] = true
		switch key {
		case "Staged:file":
			if !strings.Contains(v.Diff, "+staged line") || strings.Contains(v.Diff, "+working line") {
				t.Fatalf("staged diff: %s", v.Diff)
			}
		case "Unstaged:file":
			if !strings.Contains(v.Diff, "+working line") || !strings.Contains(v.Diff, "-staged line") {
				t.Fatalf("working diff: %s", v.Diff)
			}
		case "Staged:renamed\nfeature":
			if f.OldPath != "feature" || !strings.Contains(v.Diff, "rename from") {
				t.Fatalf("rename: %+v %s", f, v.Diff)
			}
		case "Staged::(glob)*":
			if !strings.Contains(v.Diff, "+literal path") || strings.Contains(v.Diff, "+new file") {
				t.Fatal("pathspec was expanded")
			}
		case "Untracked:binary":
			if !strings.Contains(v.DiffNote, "Binary") {
				t.Fatal(v.DiffNote)
			}
		case "Untracked:link":
			if strings.Contains(v.Diff, "DO NOT READ") || !strings.Contains(v.Diff, outside) {
				t.Fatal("symlink target was followed")
			}
		}
	}
	for _, key := range []string{"Staged:file", "Unstaged:file", "Staged:renamed\nfeature", "Staged::(glob)*", "Untracked:binary", "Untracked:link"} {
		if !seen[key] {
			t.Fatalf("missing %s: %+v", key, v.Files)
		}
	}
	if after := fixtureGit(t, repo, "status", "--porcelain=v2", "-z"); after != before {
		t.Fatal("viewer changed checkout")
	}
	afterIndex, _ := os.ReadFile(filepath.Join(repo, ".git", "index"))
	if !reflect.DeepEqual(index, afterIndex) {
		t.Fatal("viewer rewrote index")
	}
}

func TestViewerCommitRangesAndPinnedSelection(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	writeFixture(t, repo, "local", "local change\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "local subject")
	local := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	writeFixture(t, seed, "remote", "remote change\n")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "remote subject")
	fixtureGit(t, seed, "push", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	remote := fixtureGit(t, repo, "rev-parse", "origin/feature")
	for index, kind := range []string{"local", "remote"} {
		v := GitView{Path: repo, Kind: kind}
		if err := loadGitView(context.Background(), &v, true); err != nil {
			t.Fatal(err)
		}
		if v.Counts == nil || v.Counts.Local == nil || *v.Counts.Local != index+1 || v.Counts.Remote == nil || *v.Counts.Remote != 3 {
			t.Fatalf("divergent category counts: %+v", v.Counts)
		}
		want, subject := local, "local subject"
		if kind == "remote" {
			want, subject = remote, "remote subject"
		}
		if len(v.Commits) == 0 || (kind == "local" && len(v.Commits) != 1) || v.Commit != want || v.Commits[0].Subject != subject || len(v.Files) != 1 || !strings.Contains(v.Diff, "+"+kind+" change") {
			t.Fatalf("%+v", v)
		}

		if kind == "remote" {
			if v.IncomingCount != 1 || v.Commits[0].Presence != "incoming" {
				t.Fatalf("incoming: %+v", v)
			}
			shared := false
			for _, commit := range v.Commits[1:] {
				if commit.Presence == "local" {
					shared = true
				}
			}
			if !shared {
				t.Fatal("remote view hid shared history")
			}
		}
		if err := selectViewCommit(context.Background(), &v, "--all"); err == nil {
			t.Fatal("accepted arbitrary revision")
		}
		// Selection remains tied to the captured commit after a branch movement.
		fixtureGit(t, repo, "commit", "--allow-empty", "-m", "later local")
		selectViewFile(context.Background(), &v, 0)
		if !strings.Contains(v.Diff, "+"+kind+" change") {
			t.Fatal("lost pinned diff")
		}
	}
}

func TestViewerInitialCommitMergeAndExternalDiff(t *testing.T) {
	repo := t.TempDir()
	fixtureGit(t, repo, "init", "--initial-branch=main")
	writeFixture(t, repo, "a.txt", "initial\n")
	fixtureGit(t, repo, "add", ".")
	v := GitView{Path: repo, Kind: "edits"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.Diff, "+initial") {
		t.Fatalf("unborn diff: %+v", v)
	}
	fixtureGit(t, repo, "commit", "-m", "root")
	root := fixtureGit(t, repo, "rev-parse", "HEAD")
	v.Kind = "local"
	v.Commits = []ViewCommit{{ID: root}}
	if err := selectViewCommit(context.Background(), &v, root); err != nil {
		t.Fatal(err)
	}
	if v.Parent != "" || !strings.Contains(v.Diff, "+initial") {
		t.Fatalf("root diff: %+v", v)
	}
	fixtureGit(t, repo, "switch", "-c", "side")
	writeFixture(t, repo, "side", "side content\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "side")
	fixtureGit(t, repo, "switch", "main")
	fixtureGit(t, repo, "merge", "--no-ff", "-m", "merge side", "side")
	merge := fixtureGit(t, repo, "rev-parse", "HEAD")
	v.Commits = []ViewCommit{{ID: merge}}
	fixtureGit(t, repo, "config", "diff.external", "/usr/bin/false")
	if err := selectViewCommit(context.Background(), &v, merge); err != nil {
		t.Fatal(err)
	}
	if !v.Merge || v.Parent != root || len(v.Files) != 1 || !strings.Contains(v.Diff, "+side content") {
		t.Fatalf("merge diff: %+v", v)
	}
}

func TestViewerLimitsAndPathConfinement(t *testing.T) {
	repo := t.TempDir()
	if text, note := untrackedViewDiff(repo, "../outside"); text != "" || note == "" {
		t.Fatal("escaped root")
	}
	writeFixture(t, repo, "large", strings.Repeat("x", viewerDiffLimit+1))
	if text, note := untrackedViewDiff(repo, "large"); text != "" || !strings.Contains(note, "larger") {
		t.Fatal("large file not bounded")
	}
	if text, note := boundViewDiff([]byte(strings.Repeat("+line\n", 6000))); len(strings.Split(text, "\n")) > 5000 || note == "" {
		t.Fatal("diff not bounded")
	}
	if _, err := parseViewFiles([]byte("R100\x00old\x00"), "Staged"); err == nil {
		t.Fatal("accepted incomplete rename")
	}
}

func TestViewerRemoteHistoryWhenSyncedAndAcrossPages(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	v := GitView{Path: repo, Kind: "remote"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if v.Counts == nil || v.Counts.Edits != 0 || v.Counts.Local == nil || *v.Counts.Local != 0 || v.Counts.Remote == nil || *v.Counts.Remote != 2 {
		t.Fatalf("synced category counts: %+v", v.Counts)
	}
	if len(v.Commits) != 2 || v.IncomingCount != 0 {
		t.Fatalf("synced history: %+v", v)
	}
	for _, commit := range v.Commits {
		if commit.Presence != "local" {
			t.Fatalf("synced commit: %+v", commit)
		}
	}
	head := v.Head
	tree := fixtureGit(t, repo, "rev-parse", "HEAD^{tree}")
	tip := head
	for i := 0; i < viewerPageSize; i++ {
		tip = fixtureGit(t, repo, "commit-tree", tree, "-p", tip, "-m", "incoming")
	}
	fixtureGit(t, repo, "update-ref", "refs/remotes/origin/feature", tip)
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if len(v.Commits) != viewerPageSize || !v.More || v.IncomingCount != viewerPageSize {
		t.Fatalf("first page: %+v", v)
	}
	for _, commit := range v.Commits {
		if commit.Presence != "incoming" {
			t.Fatalf("incoming: %+v", commit)
		}
	}
	// Moving the remote ref does not change this viewer's captured comparison.
	fixtureGit(t, repo, "update-ref", "refs/remotes/origin/feature", head)
	v.Page = 1
	if err := loadGitView(context.Background(), &v, false); err != nil {
		t.Fatal(err)
	}
	if v.Counts.Remote == nil || *v.Counts.Remote != viewerPageSize+2 {
		t.Fatalf("count changed with page/ref: %+v", v.Counts)
	}
	if len(v.Commits) != 2 || v.More || v.Upstream != tip || v.IncomingCount != viewerPageSize {
		t.Fatalf("second page: %+v", v)
	}
	for _, commit := range v.Commits {
		if commit.Presence != "local" {
			t.Fatalf("shared: %+v", commit)
		}
	}
	v.Head = "unavailable-commit"
	v.IncomingCount = -1
	markRemotePresence(context.Background(), &v)
	if v.HistoryNote == "" || v.IncomingCount != -1 {
		t.Fatal("unavailable comparison was not reported")
	}
	for _, commit := range v.Commits {
		if commit.Presence != "unknown" {
			t.Fatal("failed comparison reported a commit as local")
		}
	}
}

func TestViewerCategoryCountsUnavailableAreNotZero(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	fixtureGit(t, repo, "branch", "--unset-upstream")
	fixtureGit(t, repo, "update-ref", "-d", "refs/remotes/origin/feature")
	fixtureGit(t, repo, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")
	v := GitView{Path: repo, Kind: "edits"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if v.Counts == nil || v.Counts.Edits != 0 || v.Counts.Local != nil || v.Counts.Remote != nil {
		t.Fatalf("missing upstream: %+v", v.Counts)
	}
	v.Head, v.Upstream = "missing-head", "missing-upstream"
	counts := countViewCategories(context.Background(), &v, 2)
	if counts.Local != nil || counts.Remote != nil || counts.Edits != 2 {
		t.Fatalf("failed counts: %+v", counts)
	}
	v.Path = t.TempDir()
	if err := loadGitView(context.Background(), &v, true); err == nil || v.Counts != nil {
		t.Fatal("failed refresh retained stale counts")
	}
}

func TestViewerUnpublishedBranchUsesRemoteDefaultHistory(t *testing.T) {
	repo := trackedFixture(t)
	fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	fixtureGit(t, repo, "switch", "--no-track", "-c", "review-fixes")
	for _, subject := range []string{"first fix", "second fix", "third fix"} {
		writeFixture(t, repo, "file", subject+"\n")
		fixtureGit(t, repo, "add", "file")
		fixtureGit(t, repo, "commit", "-m", subject)
	}
	writeFixture(t, repo, "CODE_REVIEW.md", "Review notes\n")
	s, m := inspectLocal(context.Background(), repo)
	if m.ReadError != nil || s.Comparison == nil || s.Comparison.Ahead != 3 || s.Comparison.Behind != 0 {
		t.Fatalf("missing Site comparison: %+v %+v", s, m)
	}
	if m.Ref != "" || m.Comparable || s.Tracking != "" || s.Ahead != 0 || s.Behind != 0 || s.PushTarget != "" || hasSuggestedActions(s) {
		t.Fatalf("browsing comparison changed action eligibility: %+v %+v", s, m)
	}
	before := fixtureGit(t, repo, "status", "--porcelain=v2", "--branch", "-z")
	config, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	refs := fixtureGit(t, repo, "show-ref")
	for _, kind := range []string{"edits", "local", "remote"} {
		v := GitView{Path: repo, Kind: kind}
		if err := loadGitView(context.Background(), &v, true); err != nil {
			t.Fatal(err)
		}
		if v.Counts == nil || v.Counts.Edits != 1 || v.Counts.Local == nil || *v.Counts.Local != 3 || v.Counts.Remote == nil || *v.Counts.Remote != 1 {
			t.Fatalf("%s: missing unpublished branch history: %+v", kind, v.Counts)
		}
		if v.Tracking != "origin/main" || !v.ComparisonOnly {
			t.Fatalf("wrong comparison: %+v", v)
		}
		if kind == "local" && (len(v.Commits) != 3 || v.Commits[0].Subject != "third fix" || !strings.Contains(v.Diff, "+third fix")) {
			t.Fatalf("local history: %+v", v)
		}
		if kind == "remote" && (len(v.Commits) != 1 || v.IncomingCount != 0 || v.Commits[0].Presence != "local") {
			t.Fatalf("remote history: %+v", v)
		}
	}
	afterConfig, _ := os.ReadFile(filepath.Join(repo, ".git", "config"))
	if fixtureGit(t, repo, "status", "--porcelain=v2", "--branch", "-z") != before || fixtureGit(t, repo, "show-ref") != refs || string(config) != string(afterConfig) {
		t.Fatal("reading history changed checkout, refs, or tracking configuration")
	}
}

func TestViewerWithoutUpstreamPrefersSameNamedRemoteBranch(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	fixtureGit(t, repo, "branch", "--unset-upstream")
	v := GitView{Path: repo, Kind: "local"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if v.Tracking != "origin/feature" || v.Counts.Local == nil || *v.Counts.Local != 0 || v.Counts.Remote == nil || *v.Counts.Remote != 2 || len(v.Commits) != 0 {
		t.Fatalf("already-published commits counted as local: %+v", v)
	}
	// Once tracking is configured, the normal upstream path takes over.
	fixtureGit(t, repo, "branch", "--set-upstream-to=origin/feature")
	if err := loadGitView(context.Background(), &v, true); err != nil || v.ComparisonOnly {
		t.Fatalf("comparison mode survived setting an upstream: %+v, %v", v, err)
	}
}

func TestBranchHistoryCombinesBothTipsAndPreservesSelection(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	shared := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "not pushed")
	outgoing := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "incoming")
	fixtureGit(t, seed, "push", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	incoming := fixtureGit(t, repo, "rev-parse", "origin/feature")
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	seen := map[string]string{}
	for _, c := range v.Commits {
		if _, exists := seen[c.ID]; exists {
			t.Fatal("duplicate commit")
		}
		seen[c.ID] = c.Presence
	}
	if seen[shared] != "shared" || seen[outgoing] != "outgoing" || seen[incoming] != "incoming" || v.IncomingCount != 1 {
		t.Fatalf("incorrect branch comparison: %+v", v)
	}
	if v.Counts.Branch == nil || *v.Counts.Branch != len(v.Commits) {
		t.Fatal("incorrect union count")
	}
	if err := selectViewCommit(context.Background(), &v, outgoing); err != nil {
		t.Fatal(err)
	}
	// A new commit in one side retains selection and labels from the new snapshot.
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "another local commit")
	if err := refreshGitView(context.Background(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Commit != outgoing || *v.Counts.Local != 2 {
		t.Fatalf("selection lost: %+v", v)
	}
	if fixtureGit(t, repo, "rev-parse", "origin/feature") != incoming {
		t.Fatal("viewer changed remote ref")
	}
}

func TestBranchHistoryWithoutRemoteAndBeforeFirstCommit(t *testing.T) {
	repo := t.TempDir()
	fixtureGit(t, repo, "init", "-b", "main")
	fixtureGit(t, repo, "config", "user.name", "Test")
	fixtureGit(t, repo, "config", "user.email", "test@example.com")
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if len(v.Commits) != 0 || v.Counts.Branch == nil || *v.Counts.Branch != 0 {
		t.Fatalf("unborn: %+v", v)
	}
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	if len(v.Commits) != 1 || v.Commits[0].Presence != "unlinked" || *v.Counts.Branch != 1 {
		t.Fatalf("unlinked: %+v", v)
	}
}
