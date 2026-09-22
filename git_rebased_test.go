package main

import (
	"context"
	"testing"
	"time"
)

func TestRebasedHistoryRecognizedUntilPublishedOrRemoteChanges(t *testing.T) {
	for _, scenario := range []string{"publish", "remote-work", "new-default"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			ctx := context.Background()
			var comparison rebaseComparison
			inspect := func() (GitStatus, gitMeta) { return inspectLocal(ctx, repo) }
			s, m := inspect()
			if comparison.check(ctx, s, m) {
				t.Fatal("unrebased branch recognized")
			}
			fixtureGit(t, repo, "fetch", "origin")
			fixtureGit(t, repo, "rebase", "origin/develop")
			s, m = inspect()
			if !comparison.check(ctx, s, m) {
				t.Fatalf("rebased branch not recognized: %+v", s)
			}
			if !comparison.check(ctx, s, m) {
				t.Fatal("cached result lost")
			}
			s.Rebased = true
			display := describeGit(s, m, remoteCache{Checked: time.Now()})
			if display.Summary != "Rebased · ready to publish" || hasSuggestedActions(display) {
				t.Fatalf("bad presentation: %+v", display)
			}
			if stale := describeGit(s, m, remoteCache{Error: "Offline"}); stale.Summary != "Rebased · remote not verified" {
				t.Fatalf("offline ready claim: %+v", stale)
			}
			if removed := describeGit(s, m, remoteCache{Missing: true}); removed.Rebased {
				t.Fatal("missing remote treated as publish ready")
			}
			switch scenario {
			case "publish":
				fixtureGit(t, repo, "push", "--force-with-lease", "origin", "feature")
			case "remote-work":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				writeFixture(t, seed, "colleague", "new remote work")
				fixtureGit(t, seed, "add", ".")
				fixtureGit(t, seed, "commit", "-m", "colleague work")
				fixtureGit(t, seed, "push", "origin", "feature")
			case "new-default":
				writeFixture(t, seed, "more-base", "default work")
				fixtureGit(t, seed, "add", ".")
				fixtureGit(t, seed, "commit", "-m", "new default work")
				fixtureGit(t, seed, "push")
			}
			fixtureGit(t, repo, "fetch", "origin")
			s, m = inspect()
			if got := comparison.check(ctx, s, m); got != (scenario == "new-default") {
				t.Fatalf("%s: matched=%v status=%+v", scenario, got, s)
			}
		})
	}
}

func TestRebaseDetectionDoesNotHideMergeCommits(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	fixtureGit(t, repo, "switch", "-c", "side")
	writeFixture(t, repo, "side", "side work")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "side work")
	fixtureGit(t, repo, "switch", "feature")
	fixtureGit(t, repo, "merge", "--no-ff", "-m", "merge side", "side")
	fixtureGit(t, repo, "push", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	fixtureGit(t, repo, "rebase", "--rebase-merges", "origin/develop")
	ctx := context.Background()
	s, m := inspectLocal(ctx, repo)
	var comparison rebaseComparison
	if comparison.check(ctx, s, m) {
		t.Fatal("merge commit semantics were not verified, but remote counts were suppressed")
	}
}
