package main

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestChangedRebaseSeparatesInheritedCommitsWithoutEnablingPublish(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "another default commit")
	fixtureGit(t, seed, "push")
	fixtureGit(t, repo, "fetch", "origin")
	fixtureGit(t, repo, "rebase", "origin/develop")
	// Simulate edits made while resolving/reviewing a rebase. Similar subjects
	// and matching series lengths cannot prove that remote work was preserved.
	writeFixture(t, repo, "feature", "changed during rebase\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "--amend", "--no-edit")
	before := checkoutFingerprint(t, repo)
	ctx := context.Background()
	s, m := inspectLocal(ctx, repo)
	var comparison rebaseComparison
	history := comparison.check(ctx, s, m)
	if history.rebased || history.work == nil || history.work.Local != 1 || history.work.Inherited != 2 || history.work.Base != "origin/develop" || s.Ahead != 3 || s.Behind != 1 {
		t.Fatalf("incorrect display or unsafe rebase recognition: %+v %+v", history, s)
	}
	if cached := comparison.check(ctx, s, m); !reflect.DeepEqual(cached, history) {
		t.Fatal("cached counts changed")
	}
	// A raced upstream reading must not reuse counts from another snapshot.
	stale := s
	stale.Ahead++
	if raced := comparison.check(ctx, stale, m); raced.work != nil || raced.rebased {
		t.Fatal("mixed snapshot accepted")
	}
	v := GitView{Path: repo, Kind: "branch"}
	if err := loadGitView(ctx, &v, true); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(v.BranchWork, history.work) || *v.Counts.Local != 3 || v.IncomingCount != 1 {
		t.Fatalf("viewer/count mismatch: %+v", v)
	}
	presence := map[string]int{}
	for _, commit := range v.Commits {
		presence[commit.Presence]++
	}
	if presence["inherited"] != 2 || presence["outgoing"] != 1 || presence["incoming"] != 1 {
		t.Fatalf("hidden remote work or mislabeled default commits: %v", presence)
	}
	if after := checkoutFingerprint(t, repo); !reflect.DeepEqual(before, after) {
		t.Fatal("history inspection changed the checkout")
	}
	if _, err := prepareCheckoutAction(ctx, repo, actionRequest(t, repo, "publish"), nil); err == nil {
		t.Fatal("unverified changed history allowed publication")
	}
	// A subsequent ordinary branch must not retain the display metadata.
	fixtureGit(t, repo, "switch", "develop")
	if err := refreshGitView(ctx, &v); err != nil || v.BranchWork != nil {
		t.Fatalf("stale branch work after switch: %+v, %v", v.BranchWork, err)
	}
	s, m = inspectLocal(ctx, repo)
	if next := comparison.check(ctx, s, m); next.work != nil || next.rebased {
		t.Fatal("stale cached branch work after switch")
	}
}

func TestRebasedHistoryRecognizedUntilPublishedOrRemoteChanges(t *testing.T) {
	for _, scenario := range []string{"publish", "remote-work", "new-default"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			ctx := context.Background()
			var comparison rebaseComparison
			inspect := func() (GitStatus, gitMeta) { return inspectLocal(ctx, repo) }
			s, m := inspect()
			if comparison.check(ctx, s, m).rebased {
				t.Fatal("unrebased branch recognized")
			}
			fixtureGit(t, repo, "fetch", "origin")
			fixtureGit(t, repo, "rebase", "origin/develop")
			s, m = inspect()
			if !comparison.check(ctx, s, m).rebased {
				t.Fatalf("rebased branch not recognized: %+v", s)
			}
			if !comparison.check(ctx, s, m).rebased {
				t.Fatal("cached result lost")
			}
			s.Rebased = true
			display := describeGit(s, m, remoteCache{Checked: time.Now()})
			if display.Summary != "Rebased · ready to publish" || !hasSuggestedActions(display) {
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
			if got := comparison.check(ctx, s, m).rebased; got != (scenario == "new-default") {
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
	if comparison.check(ctx, s, m).rebased {
		t.Fatal("merge commit semantics were not verified, but remote counts were suppressed")
	}
}
