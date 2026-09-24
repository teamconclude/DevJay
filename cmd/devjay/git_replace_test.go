package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func replacementFixture(t *testing.T) (string, string) {
	t.Helper()
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	writeFixture(t, seed, "remote.txt", "rewritten remote data")
	fixtureGit(t, seed, "add", "remote.txt")
	fixtureGit(t, seed, "commit", "--amend", "-m", "rewritten remote feature")
	fixtureGit(t, seed, "push", "--force", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	return repo, seed
}

func TestReplacementRepeatedRewritesPreserveRecoveryAndTracking(t *testing.T) {
	ctx := context.Background()
	repo, seed := replacementFixture(t)
	old := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, seed, "commit", "--amend", "-m", "rewritten again")
	fixtureGit(t, seed, "push", "--force", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	s, m := inspectLocal(ctx, repo)
	if !canReplaceFromRemote(ctx, s, m) {
		t.Fatal("did not recognize an older published tip across two rewrites")
	}
	s.CanReplaceRemote = true
	if !describeGit(s, m, remoteCache{}).Rewritten {
		t.Fatal("recorded rewrite disappeared after restart")
	}
	p := prepareForTest(t, repo, "replace")
	if fixtureGit(t, repo, "rev-parse", "HEAD") != old {
		t.Fatal("preparation changed checkout")
	}
	result := applyCheckoutAction(ctx, p)
	if result.Status != "complete" || result.CurrentBranch != "feature" || result.CurrentHead != p.TargetHead {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != old {
		t.Fatal("old history not retained")
	}
	if fixtureGit(t, repo, "rev-parse", "@{upstream}") != p.TargetHead || fixtureGit(t, repo, "status", "--porcelain") != "" {
		t.Fatal("tracking or clean checkout lost")
	}
	data, err := os.ReadFile(filepath.Join(repo, "remote.txt"))
	if err != nil || string(data) != "rewritten remote data" {
		t.Fatal("new remote files missing")
	}
	if result := applyCheckoutAction(ctx, p); result.Status != "failed" {
		t.Fatal("replayed confirmation")
	}
}

func TestReplacementRejectsUnpublishedWorkAndMissingEvidence(t *testing.T) {
	for _, scenario := range []string{"commit", "amended", "dirty", "staged", "untracked", "assume-unchanged", "skip-worktree", "no-reflog", "other-upstream"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _ := replacementFixture(t)
			switch scenario {
			case "commit":
				fixtureGit(t, repo, "commit", "--allow-empty", "--date=2001-01-01T00:00:00Z", "-m", "unpublished work with old date")
			case "amended":
				fixtureGit(t, repo, "commit", "--amend", "-m", "unpublished amendment")
			case "dirty", "staged":
				writeFixture(t, repo, "file", "local work")
				if scenario == "staged" {
					fixtureGit(t, repo, "add", "file")
				}
			case "untracked":
				writeFixture(t, repo, "untracked", "local work")
			case "assume-unchanged", "skip-worktree":
				fixtureGit(t, repo, "update-index", "--"+scenario, "file")
				writeFixture(t, repo, "file", "hidden local work")
			case "no-reflog":
				fixtureGit(t, repo, "reflog", "expire", "--expire=all", "refs/remotes/origin/feature")
			case "other-upstream":
				fixtureGit(t, repo, "branch", "--set-upstream-to=origin/develop")
			}
			before := checkoutFingerprint(t, repo)
			if _, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "replace"), nil); err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			if after := checkoutFingerprint(t, repo); !reflect.DeepEqual(before, after) {
				t.Fatalf("preparation changed checkout: before=%v after=%v", before, after)
			}
			if scenario != "assume-unchanged" && scenario != "skip-worktree" {
				s, m := inspectLocal(context.Background(), repo)
				if canReplaceFromRemote(context.Background(), s, m) {
					t.Fatal("unsafe replacement recommended")
				}
			}
		})
	}
}

func TestReplacementRechecksAfterConfirmationAndProtectsIgnoredFiles(t *testing.T) {
	for _, scenario := range []string{"commit", "dirty", "staged", "untracked", "remote", "tracking", "no-reflog", "ignored", "other-worktree"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := replacementFixture(t)
			p := prepareForTest(t, repo, "replace")
			switch scenario {
			case "commit":
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "new unpublished work")
			case "dirty", "staged":
				writeFixture(t, repo, "file", "local work")
				if scenario == "staged" {
					fixtureGit(t, repo, "add", "file")
				}
			case "untracked":
				writeFixture(t, repo, "untracked", "local work")
			case "remote":
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "remote moved again")
				fixtureGit(t, seed, "push", "origin", "feature")
			case "tracking":
				fixtureGit(t, repo, "branch", "--set-upstream-to=origin/develop")
			case "no-reflog":
				fixtureGit(t, repo, "reflog", "expire", "--expire=all", "refs/remotes/origin/feature")
			case "ignored":
				if err := os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("remote.txt\n"), 0600); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, repo, "remote.txt", "ignored local work")
			case "other-worktree":
				fixtureGit(t, repo, "worktree", "add", "--force", filepath.Join(t.TempDir(), "other"), "feature")
			}
			head := fixtureGit(t, repo, "rev-parse", "HEAD")
			status := fixtureGit(t, repo, "status", "--porcelain=v2", "-z")
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || fixtureGit(t, repo, "rev-parse", "HEAD") != head || fixtureGit(t, repo, "symbolic-ref", "--short", "HEAD") != "feature" {
				t.Fatalf("unsafe replacement: %+v", result)
			}
			if fixtureGit(t, repo, "status", "--porcelain=v2", "-z") != status {
				t.Fatal("local edits changed")
			}
			if scenario == "ignored" {
				data, _ := os.ReadFile(filepath.Join(repo, "remote.txt"))
				if string(data) != "ignored local work" {
					t.Fatal("ignored data overwritten")
				}
			}
		})
	}
}

func TestReplacementRecognizesPublishedMergeTips(t *testing.T) {
	ctx := context.Background()
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, repo, "switch", "-c", "side")
	writeFixture(t, repo, "side", "side work")
	fixtureGit(t, repo, "add", "side")
	fixtureGit(t, repo, "commit", "-m", "side work")
	fixtureGit(t, repo, "switch", "feature")
	fixtureGit(t, repo, "merge", "--no-ff", "side", "-m", "published merge")
	fixtureGit(t, repo, "push", "origin", "feature")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	fixtureGit(t, seed, "commit", "--amend", "-m", "rewritten merge")
	fixtureGit(t, seed, "push", "--force", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	s, m := inspectLocal(ctx, repo)
	if !canReplaceFromRemote(ctx, s, m) {
		t.Fatal("merge tip not recognized")
	}
	p := prepareForTest(t, repo, "replace")
	if result := applyCheckoutAction(ctx, p); result.Status != "complete" {
		t.Fatalf("%+v", result)
	}
}

func TestReplacementDoesNotReplaceFastForwardOrSyncedBranches(t *testing.T) {
	repo, _ := updateFixture(t)
	if _, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "replace"), nil); err == nil {
		t.Fatal("replacement accepted a normal fast-forward")
	}
	fixtureGit(t, repo, "merge", "--ff-only", "origin/develop")
	if _, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "replace"), nil); err == nil {
		t.Fatal("replacement accepted an in-sync branch")
	}
}

func TestReplacementHandlesRemoteRewindWithoutOfferingPush(t *testing.T) {
	ctx := context.Background()
	repo, seed := replacementFixture(t)
	// The server deliberately rewinds to an ancestor of the local tip.
	fixtureGit(t, seed, "reset", "--hard", "HEAD^")
	fixtureGit(t, seed, "push", "--force", "origin", "feature")
	fixtureGit(t, repo, "fetch", "origin")
	s, m := inspectLocal(ctx, repo)
	if s.Behind != 0 || !canReplaceFromRemote(ctx, s, m) {
		t.Fatal("remote rewind not recognized")
	}
	p := prepareForTest(t, repo, "replace")
	if result := applyCheckoutAction(ctx, p); result.Status != "complete" {
		t.Fatalf("%+v", result)
	}
}

func TestReplacementRefMoveCannotDiscardConcurrentCommit(t *testing.T) {
	repo, _ := replacementFixture(t)
	p := prepareForTest(t, repo, "replace")
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "concurrent local work")
	newer := fixtureGit(t, repo, "rev-parse", "HEAD")
	// Exercise the last compare-and-swap defense as if the branch moved after
	// validation. The old plan must not overwrite the new branch tip.
	if err := replaceCheckout(context.Background(), p); err == nil {
		t.Fatal("overwrote a concurrent branch movement")
	}
	if fixtureGit(t, repo, "rev-parse", "refs/heads/feature") != newer {
		t.Fatal("concurrent commit was discarded")
	}
}
