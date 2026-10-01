package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebaseAndPushChecksBothOperationsBeforeApplying(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	writeFixture(t, repo, "extra", "unpublished work\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "local work")
	origin := fixtureGit(t, repo, "remote", "get-url", "origin")
	original := fixtureGit(t, repo, "rev-parse", "HEAD")
	remote := fixtureGit(t, origin, "rev-parse", "feature")
	before := checkoutFingerprint(t, repo)
	p := prepareForTest(t, repo, "rebase-push")
	if p.preparedSeries == "" || p.Commits != 2 || p.PublishBase != "develop" {
		t.Fatalf("unverified plan: %+v", p)
	}
	after := checkoutFingerprint(t, repo)
	for _, key := range []string{"head", "file", "feature", ".git/index", ".git/config"} {
		if before[key] != after[key] {
			t.Fatalf("preparation changed %s", key)
		}
	}
	if fixtureGit(t, origin, "rev-parse", "feature") != remote {
		t.Fatal("preparation pushed")
	}
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "complete" || result.Kind != "rebase-push" {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, origin, "rev-parse", "feature") != result.CurrentHead || result.CurrentBranch != "feature" || result.CurrentHead == original {
		t.Fatal("wrong published checkout")
	}
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != original || fixtureGit(t, repo, "rev-parse", result.RemoteRecovery) != remote {
		t.Fatal("lost original tips")
	}
	if fixtureGit(t, repo, "status", "--porcelain") != "" || fixtureGit(t, repo, "log", "-1", "--format=%s") != "local work" {
		t.Fatal("lost work or left a conflict")
	}
	fixtureGit(t, repo, "merge-base", "--is-ancestor", p.publishBaseHead, "HEAD")
	if result := applyCheckoutAction(context.Background(), p); result.Status != "failed" {
		t.Fatal("replayed confirmation")
	}
}

func TestRebaseAndPushRefusesUnsafePreparationWithoutChanges(t *testing.T) {
	for _, scenario := range []string{"conflict", "dirty", "remote-incoming", "merge", "signing", "different-push-url", "wrong-base", "other-worktree"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := rebaseFixture(t, scenario == "conflict")
			origin := fixtureGit(t, repo, "remote", "get-url", "origin")
			switch scenario {
			case "other-worktree":
				fixtureGit(t, repo, "worktree", "add", "--force", filepath.Join(t.TempDir(), "other"), "feature")
			case "dirty":
				writeFixture(t, repo, "new-file", "local edits")
			case "remote-incoming":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "colleague work")
				fixtureGit(t, seed, "push", "origin", "feature")
			case "merge":
				fixtureGit(t, repo, "switch", "-c", "side", "origin/develop")
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "side work")
				fixtureGit(t, repo, "switch", "feature")
				fixtureGit(t, repo, "merge", "--no-ff", "-m", "merge", "side")
			case "signing":
				fixtureGit(t, repo, "config", "commit.gpgsign", "true")
			case "different-push-url":
				fixtureGit(t, repo, "remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "other.git"))
			}
			before := checkoutFingerprint(t, repo)
			remoteBefore := fixtureGit(t, origin, "show-ref")
			req := actionRequest(t, repo, "rebase-push")
			if scenario == "wrong-base" {
				req.TargetBranch = "other"
			}
			_, err := prepareCheckoutAction(context.Background(), repo, req, nil)
			if err == nil {
				t.Fatal("unsafe action offered")
			}
			if scenario == "conflict" {
				conflict, ok := err.(*rebaseActionError)
				if !ok || len(conflict.check.Files) == 0 || !strings.Contains(err.Error(), "Claude or Astra") {
					t.Fatalf("missing conflict details: %v", err)
				}
			}
			after := checkoutFingerprint(t, repo)
			for _, key := range []string{"head", "file", "feature", ".git/index", ".git/config"} {
				if before[key] != after[key] {
					t.Fatalf("changed %s", key)
				}
			}
			if fixtureGit(t, origin, "show-ref") != remoteBefore {
				t.Fatal("changed remote")
			}
		})
	}
}

func TestRebaseAndPushRechecksConfirmedStateAndLeavesRejectedPushRecoverable(t *testing.T) {
	for _, scenario := range []string{"remote", "base", "dirty", "rejected", "backup-collision", "result-changed"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			origin := fixtureGit(t, repo, "remote", "get-url", "origin")
			p := prepareForTest(t, repo, "rebase-push")
			switch scenario {
			case "remote":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "new remote work")
				fixtureGit(t, seed, "push", "origin", "feature")
				fixtureGit(t, repo, "fetch", "origin")
			case "base":
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "new default")
				fixtureGit(t, seed, "push")
			case "other-worktree":
				fixtureGit(t, repo, "worktree", "add", "--force", filepath.Join(t.TempDir(), "other"), "feature")
			case "dirty":
				writeFixture(t, repo, "edits", "unsaved")
			case "rejected":
				if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			case "backup-collision":
				fixtureGit(t, repo, "update-ref", "refs/devjay/backups/"+p.ID+"-remote", p.Head)
			case "result-changed":
				p.preparedSeries = "different result"
			}
			before := fixtureGit(t, origin, "show-ref")
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || fixtureGit(t, origin, "show-ref") != before {
				t.Fatalf("unsafe result: %+v", result)
			}
			if scenario != "rejected" && fixtureGit(t, repo, "rev-parse", "HEAD") != p.Head {
				t.Fatal("changed checkout before check passed")
			}
			if scenario == "rejected" {
				if result.CurrentBranch != "feature" || result.CurrentHead == p.Head || fixtureGit(t, repo, "status", "--porcelain") != "" || !strings.Contains(result.Message, "Local rebase completed") {
					t.Fatalf("bad partial state: %+v", result)
				}
				if fixtureGit(t, repo, "rev-parse", result.Recovery) != p.Head || fixtureGit(t, repo, "rev-parse", result.RemoteRecovery) != p.TargetHead {
					t.Fatal("lost recovery")
				}
			}
		})
	}
}

func TestRebaseAndPushLeaseRejectsLastMomentRemoteUpdateAndBackgroundFetch(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	p := prepareForTest(t, repo, "rebase-push")
	origin := fixtureGit(t, repo, "remote", "get-url", "origin")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "concurrent remote work")
	racingHead := fixtureGit(t, seed, "rev-parse", "HEAD")
	t.Setenv("DEVJAY_TEST_SEED", seed)
	t.Setenv("DEVJAY_TEST_CHECKOUT", repo)
	installGitShim(t, `for arg in "$@"; do
 if test "$arg" = push; then
  "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_SEED" push origin feature || exit 1
  "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_CHECKOUT" fetch origin || exit 1
  break
 fi
done`)
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "failed" || fixtureGit(t, origin, "rev-parse", "feature") != racingHead {
		t.Fatalf("overwrote remote work: %+v", result)
	}
	if result.CurrentBranch != "feature" || fixtureGit(t, repo, "status", "--porcelain") != "" {
		t.Fatal("left checkout conflicted")
	}
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != p.Head {
		t.Fatal("lost original work")
	}
}
