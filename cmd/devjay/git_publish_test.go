package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func publishFixture(t *testing.T) (string, string) {
	t.Helper()
	repo, seed := rebaseFixture(t, false)
	plan := prepareForTest(t, repo, "rebase")
	if result := applyCheckoutAction(context.Background(), plan); result.Status != "complete" {
		t.Fatalf("rebase failed: %+v", result)
	}
	return repo, seed
}

func TestPublishRebasedBranchPreservesRemoteWorkAndLocalCheckout(t *testing.T) {
	repo, _ := publishFixture(t)
	// Preserve the remote series even when more local work follows the rebase.
	writeFixture(t, repo, "extra", "local addition\n")
	fixtureGit(t, repo, "add", "extra")
	fixtureGit(t, repo, "commit", "-m", "additional work")
	origin := fixtureGit(t, repo, "remote", "get-url", "origin")
	remoteBefore := fixtureGit(t, origin, "rev-parse", "feature")
	fixtureGit(t, repo, "config", "remote.origin.mirror", "true")
	fixtureGit(t, repo, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	fixtureGit(t, repo, "config", "push.followTags", "true")
	fixtureGit(t, repo, "branch", "unrelated")
	fixtureGit(t, repo, "tag", "-a", "private", "-m", "private")
	before := checkoutFingerprint(t, repo)
	p := prepareForTest(t, repo, "publish")
	if p.Commits != 2 || fixtureGit(t, origin, "rev-parse", "feature") != remoteBefore {
		t.Fatal("preparation published or counted incorrectly")
	}
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "complete" || result.Kind != "publish" || result.Recovery == "" {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != remoteBefore || fixtureGit(t, origin, "rev-parse", "feature") != p.Head {
		t.Fatal("published wrong commit or lost recovery")
	}
	if fixtureGit(t, origin, "for-each-ref", "--format=%(refname)", "refs/heads/unrelated", "refs/tags", "refs/devjay") != "" {
		t.Fatal("published unrelated refs")
	}
	after := checkoutFingerprint(t, repo)
	for _, key := range []string{"head", "file", "feature", ".git/index", ".git/config"} {
		if before[key] != after[key] {
			t.Fatalf("changed %s", key)
		}
	}
	if result := applyCheckoutAction(context.Background(), p); result.Status != "failed" {
		t.Fatal("confirmation replayed")
	}
}

func TestPublishPreparationStopsForConflictsOrUnverifiedHistory(t *testing.T) {
	for _, scenario := range []string{"dirty", "operation", "remote-work", "remote-rewrite", "default-conflict", "default-new", "changed-content", "changed-message", "merge", "push-url", "ordinary"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := publishFixture(t)
			origin := fixtureGit(t, repo, "remote", "get-url", "origin")
			switch scenario {
			case "dirty":
				writeFixture(t, repo, "untracked", "edits")
			case "operation":
				writeFixture(t, repo, ".git/CHERRY_PICK_HEAD", fixtureGit(t, repo, "rev-parse", "HEAD"))
			case "remote-work", "remote-rewrite":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				if scenario == "remote-work" {
					writeFixture(t, seed, "colleague", "remote work")
					fixtureGit(t, seed, "add", ".")
					fixtureGit(t, seed, "commit", "-m", "colleague work")
				} else {
					fixtureGit(t, seed, "commit", "--amend", "-m", "rewritten remote message")
				}
				fixtureGit(t, seed, "push", "--force-with-lease", "origin", "feature")
			case "default-conflict", "default-new":
				file := "new-default"
				if scenario == "default-conflict" {
					file = "feature"
				}
				writeFixture(t, seed, file, "conflicting new default work\n")
				fixtureGit(t, seed, "add", ".")
				fixtureGit(t, seed, "commit", "-m", "new default")
				fixtureGit(t, seed, "push")
			case "changed-content":
				writeFixture(t, repo, "feature", "different work\n")
				fixtureGit(t, repo, "add", ".")
				fixtureGit(t, repo, "commit", "--amend", "--no-edit")
			case "changed-message":
				fixtureGit(t, repo, "commit", "--amend", "-m", "different message")
			case "merge":
				fixtureGit(t, repo, "switch", "-c", "side", "origin/develop")
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "side work")
				fixtureGit(t, repo, "switch", "feature")
				fixtureGit(t, repo, "merge", "--no-ff", "-m", "merge side", "side")
			case "push-url":
				fixtureGit(t, repo, "remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "other.git"))
			case "ordinary":
				fixtureGit(t, repo, "push", "--force-with-lease", "origin", "feature")
			}
			before := checkoutFingerprint(t, repo)
			remoteBefore := fixtureGit(t, origin, "show-ref")
			_, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "publish"), nil)
			if err == nil {
				t.Fatal("unsafe publication accepted")
			}
			if scenario == "default-conflict" {
				conflict, ok := err.(*rebaseActionError)
				if !ok || conflict.check.Status != "conflicts" || len(conflict.check.Files) == 0 || !strings.Contains(err.Error(), "Claude or Astra") {
					t.Fatalf("missing early conflict details: %v", err)
				}
			}
			if fixtureGit(t, origin, "show-ref") != remoteBefore {
				t.Fatal("preparation changed remote")
			}
			after := checkoutFingerprint(t, repo)
			for _, key := range []string{"head", "file", "feature", ".git/index"} {
				if before[key] != after[key] {
					t.Fatalf("preparation changed %s", key)
				}
			}
		})
	}
}

func TestPublishStopsForChangesAfterConfirmation(t *testing.T) {
	for _, scenario := range []string{"remote", "lease-race-with-fetch", "default", "head", "dirty", "upstream", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := publishFixture(t)
			origin := fixtureGit(t, repo, "remote", "get-url", "origin")
			p := prepareForTest(t, repo, "publish")
			switch scenario {
			case "remote", "lease-race-with-fetch":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "racing remote work")
				fixtureGit(t, seed, "push", "origin", "feature")
				fixtureGit(t, repo, "fetch", "origin")
			case "default":
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "racing default")
				fixtureGit(t, seed, "push")
			case "head":
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "more local work")
			case "dirty":
				writeFixture(t, repo, "local-edit", "edits")
			case "upstream":
				fixtureGit(t, repo, "branch", "--set-upstream-to=origin/develop")
			case "rejected":
				if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			before := fixtureGit(t, origin, "show-ref")
			var result CheckoutAction
			if scenario == "lease-race-with-fetch" {
				// Bypass preflight to exercise the final server-side lease itself.
				result = applyPush(context.Background(), p)
			} else {
				result = applyCheckoutAction(context.Background(), p)
			}
			if result.Status != "failed" || fixtureGit(t, origin, "show-ref") != before {
				t.Fatalf("unsafe publication: %+v", result)
			}
		})
	}
}
