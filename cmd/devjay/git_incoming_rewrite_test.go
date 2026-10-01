package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAmendedIncomingCommitStillOffersNormalUpdate(t *testing.T) {
	ctx := context.Background()
	seed := trackedFixture(t)
	origin := fixtureGit(t, seed, "remote", "get-url", "origin")
	root := t.TempDir()
	checkout, affected := filepath.Join(root, "behind"), filepath.Join(root, "already-updated")
	fixtureGit(t, root, "clone", origin, checkout)
	head := fixtureGit(t, checkout, "rev-parse", "HEAD")

	writeFixture(t, seed, "incoming", "new work")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "new work with an awkward message")
	fixtureGit(t, seed, "push")
	_, meta := inspectLocal(ctx, checkout)
	cache := remoteCache{Key: meta.key()}
	refreshRemote(ctx, meta, &cache)
	fixtureGit(t, root, "clone", origin, affected)
	_, affectedMeta := inspectLocal(ctx, affected)
	affectedCache := remoteCache{Key: affectedMeta.key()}

	// The other checkout amends only its message after publishing. The first
	// checkout has fetched both versions but has not incorporated either.
	fixtureGit(t, seed, "commit", "--amend", "-m", "new work")
	fixtureGit(t, seed, "push", "--force-with-lease")
	refreshRemote(ctx, meta, &cache)
	if !cache.Rewritten {
		t.Fatal("fixture did not detect the remote-tip replacement")
	}
	local, meta := inspectLocal(ctx, checkout)
	if local.Ahead != 0 || local.Behind != 1 || describeGit(local, meta, cache).Rewritten {
		t.Fatalf("unaffected checkout must be an ordinary incoming update: %+v", local)
	}
	var last GitStatus
	status, valid := applyLocalReading(local, meta, &cache, &last)
	if !valid || status.Rewritten || cache.Rewritten {
		t.Fatal("irrelevant remote event survived a complete local reading")
	}
	option := makeCardGitOption(cardMenuRequest{Kind: "branch", GitAction: "update"}, status)
	request, ok := option.request("site", status)
	if !ok || request.Kind != "update" {
		t.Fatalf("normal update was blocked: %+v", option)
	}
	plan, err := prepareCheckoutAction(ctx, checkout, request, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fixtureGit(t, checkout, "rev-parse", "HEAD") != head {
		t.Fatal("preparation moved the local branch")
	}
	result := applyCheckoutAction(ctx, plan)
	if result.Status != "complete" || fixtureGit(t, checkout, "rev-parse", "HEAD") != fixtureGit(t, seed, "rev-parse", "HEAD") {
		t.Fatalf("safe fast-forward failed: %+v", result)
	}

	// A checkout that did incorporate the replaced commit still needs the
	// warning and must not be offered an ordinary fast-forward update.
	refreshRemote(ctx, affectedMeta, &affectedCache)
	local, affectedMeta = inspectLocal(ctx, affected)
	status, valid = applyLocalReading(local, affectedMeta, &affectedCache, &last)
	if !valid || !status.Rewritten || !affectedCache.Rewritten || status.Ahead != 1 || status.Behind != 1 {
		t.Fatalf("affected local history lost its warning: %+v", status)
	}
	option = makeCardGitOption(cardMenuRequest{Kind: "branch", GitAction: "update"}, status)
	if _, ok := option.request("site", status); ok {
		t.Fatal("rewritten local history allowed an ordinary update")
	}
}

func TestUnknownComparisonRetainsRewriteWarning(t *testing.T) {
	meta := gitMeta{Branch: "feature", Ref: "refs/remotes/origin/feature"}
	cache := remoteCache{Key: meta.key(), Rewritten: true}
	var last GitStatus
	status, valid := applyLocalReading(GitStatus{Branch: "feature"}, meta, &cache, &last)
	if !valid || !status.Rewritten || !cache.Rewritten {
		t.Fatal("missing comparison must not clear a known rewrite")
	}
}
