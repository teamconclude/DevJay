package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBranchCreationRequiresTheOriginalCreationRecord(t *testing.T) {
	sha := strings.Repeat("a", 40)
	line := strings.Repeat("0", 40) + " " + sha + " Developer Name <dev@example.invalid> 1700000000 +0200\tbranch: Created from HEAD\n"
	id, born := parseBranchCreation(line)
	if id == "" || born.Unix() != 1700000000 {
		t.Fatalf("%q %v", id, born)
	}
	for _, bad := range []string{
		strings.Replace(line, strings.Repeat("0", 40), sha, 1),
		strings.Replace(line, "branch: Created from HEAD", "commit: branch: Created from HEAD", 1),
		strings.Replace(line, "1700000000", "invalid", 1), "", "truncated",
	} {
		if id, born := parseBranchCreation(bad); id != "" || !born.IsZero() {
			t.Fatalf("accepted %q", bad)
		}
	}
}

func TestRecreatedBranchDoesNotInheritMergedPR(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	_, old := inspectLocal(context.Background(), repo)
	if old.ReadError != nil || old.BranchGeneration == "" || old.BranchCreated.IsZero() {
		t.Fatalf("%+v", old)
	}
	pr := &MergedPullRequest{Number: 1, Head: old.OID, Base: "develop", MergedAt: old.BranchCreated.Add(time.Hour)}
	if !mergedPRMatchesBranch(pr, old) {
		t.Fatal("original branch lost matching PR")
	}
	fixtureGit(t, repo, "switch", "develop")
	fixtureGit(t, repo, "branch", "-D", "feature")
	// Controlled reflog dates make recreation deterministic without sleeps. The
	// new branch even starts at the SAME commit to rule out SHA-only identity.
	t.Setenv("GIT_COMMITTER_DATE", fmt.Sprintf("%d +0000", pr.MergedAt.Add(time.Hour).Unix()))
	fixtureGit(t, repo, "switch", "-c", "feature", old.OID)
	fresh, meta := inspectLocal(context.Background(), repo)
	if meta.ReadError != nil || meta.OID != old.OID || meta.BranchGeneration == old.BranchGeneration || !meta.BranchCreated.After(pr.MergedAt) {
		t.Fatalf("%+v", meta)
	}
	// Tracking was removed by recreation; restore the old metadata to ensure
	// birth, rather than an incidental upstream change, invalidates the cache.
	sameConfig := meta
	sameConfig.Ref, sameConfig.Remote, sameConfig.Merge = old.Ref, old.Remote, old.Merge
	if sameConfig.key() == old.key() {
		t.Fatal("recreated branch kept old cache identity")
	}
	cache := remoteCache{Key: old.key(), GitHub: "No open pull request", MergedPR: pr}
	displayed := describeGit(fresh, meta, cache)
	if displayed.MergedPR != nil || displayed.GitHub != "No open pull request" || hasSuggestedActions(displayed) {
		t.Fatalf("old PR attached: %+v", displayed)
	}
	var last GitStatus
	applyLocalReading(fresh, sameConfig, &cache, &last)
	if cache.MergedPR != nil {
		t.Fatal("old merged PR cache survived recreation")
	}
}

func TestMergedAssociationDoesNotUseNameOrAncestryAlone(t *testing.T) {
	pr := &MergedPullRequest{Head: "old-tip", MergedAt: time.Unix(1700000100, 0)}
	for _, tc := range []struct {
		meta gitMeta
		want bool
	}{
		{gitMeta{OID: "old-tip"}, true}, // Missing/expired reflog: require exact tip.
		{gitMeta{OID: "different-tip"}, false},
		{gitMeta{OID: "old-tip", BranchCreated: time.Unix(1700000000, 0)}, true},
		{gitMeta{OID: "old-tip", BranchCreated: time.Unix(1700000200, 0)}, false},
	} {
		if mergedPRMatchesBranch(pr, tc.meta) != tc.want {
			t.Fatalf("%+v", tc)
		}
	}
	repo, _ := rebaseFixture(t, false)
	// Expired history that starts with a later ordinary update is not creation.
	log := gitString(context.Background(), repo, "rev-parse", "--git-path", "logs/refs/heads/feature")
	if !filepath.IsAbs(log) {
		log = filepath.Join(repo, log)
	}
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	if err := os.WriteFile(log, []byte(head+" "+head+" Test <test@example.invalid> 1700000000 +0000\tcommit: later\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if id, born := branchCreation(context.Background(), repo, "feature"); id != "" || !born.IsZero() {
		t.Fatal("expired reflog treated as creation")
	}
}
