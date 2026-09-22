package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pushFixture(t *testing.T) (string, string) {
	t.Helper()
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "local work")
	return repo, seed
}

func TestPushConfirmedCommitsOnly(t *testing.T) {
	repo, _ := pushFixture(t)
	origin := fixtureGit(t, repo, "remote", "get-url", "origin")
	old := fixtureGit(t, origin, "rev-parse", "refs/heads/feature")
	// Ambient push configuration must not publish unrelated refs or tags.
	fixtureGit(t, repo, "config", "push.default", "matching")
	fixtureGit(t, repo, "config", "push.followTags", "true")
	fixtureGit(t, repo, "config", "remote.origin.mirror", "true")
	fixtureGit(t, repo, "config", "remote.origin.push", "+refs/heads/*:refs/heads/*")
	fixtureGit(t, repo, "branch", "unrelated")
	fixtureGit(t, repo, "tag", "-a", "private-tag", "-m", "private")
	before := checkoutFingerprint(t, repo)
	s, _ := inspectLocal(context.Background(), repo)
	if s.PushTarget != "origin/feature" || !hasSuggestedActions(s) {
		t.Fatalf("%+v", s)
	}
	p := prepareForTest(t, repo, "push")
	if p.Commits != 1 || fixtureGit(t, origin, "rev-parse", "refs/heads/feature") != old {
		t.Fatal("preview published or wrong count")
	}
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "complete" || result.Kind != "push" || result.Recovery != "" {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, origin, "rev-parse", "refs/heads/feature") != p.Head {
		t.Fatal("wrong published commit")
	}
	if fixtureGit(t, origin, "for-each-ref", "--format=%(refname)", "refs/heads/unrelated", "refs/tags") != "" {
		t.Fatal("published extra refs")
	}
	after := checkoutFingerprint(t, repo)
	for _, key := range []string{"head", "file", "feature", ".git/index", ".git/config"} {
		if before[key] != after[key] {
			t.Fatalf("changed %s", key)
		}
	}
	s, _ = inspectLocal(context.Background(), repo)
	if s.Ahead != 0 || s.Behind != 0 {
		t.Fatalf("stale comparison: %+v", s)
	}
	if replay := applyCheckoutAction(context.Background(), p); replay.Status != "failed" {
		t.Fatal("replayed confirmation")
	}
}

func TestPushRejectsUnsafePreparation(t *testing.T) {
	for _, scenario := range []string{"incoming", "synced", "dirty", "untracked", "no-upstream", "other-remote", "other-branch", "push-url", "multiple-urls", "missing-remote"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := pushFixture(t)
			origin := fixtureGit(t, repo, "remote", "get-url", "origin")
			switch scenario {
			case "incoming":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "incoming")
				fixtureGit(t, seed, "push", "origin", "feature")
			case "synced":
				fixtureGit(t, repo, "push", "origin", "feature")
			case "dirty":
				writeFixture(t, repo, "feature", "edits")
			case "untracked":
				writeFixture(t, repo, "new-file", "edits")
			case "no-upstream":
				fixtureGit(t, repo, "branch", "--unset-upstream")
			case "other-remote":
				fixtureGit(t, repo, "remote", "rename", "origin", "upstream")
			case "other-branch":
				fixtureGit(t, repo, "branch", "--set-upstream-to=origin/develop")
			case "push-url":
				fixtureGit(t, repo, "remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "other.git"))
			case "multiple-urls":
				fixtureGit(t, repo, "config", "--add", "remote.origin.pushurl", origin)
				fixtureGit(t, repo, "config", "--add", "remote.origin.pushurl", origin)
			case "missing-remote":
				fixtureGit(t, origin, "update-ref", "-d", "refs/heads/feature")
			}
			before := fixtureGit(t, origin, "show-ref")
			_, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "push"), nil)
			if err == nil {
				t.Fatal("unsafe preparation accepted")
			}
			if fixtureGit(t, origin, "show-ref") != before {
				t.Fatal("preview changed remote")
			}
		})
	}
}

func TestPushStopsWhenConfirmationChanges(t *testing.T) {
	for _, scenario := range []string{"remote", "head", "branch", "dirty", "upstream", "url", "expired", "rejected"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := pushFixture(t)
			origin := fixtureGit(t, repo, "remote", "get-url", "origin")
			p := prepareForTest(t, repo, "push")
			switch scenario {
			case "remote":
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "incoming")
				fixtureGit(t, seed, "push", "origin", "feature")
			case "head":
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "more work")
			case "branch":
				fixtureGit(t, repo, "switch", "-c", "other")
			case "dirty":
				writeFixture(t, repo, "feature", "edits")
			case "upstream":
				fixtureGit(t, repo, "branch", "--unset-upstream")
			case "url":
				fixtureGit(t, repo, "remote", "set-url", "--push", "origin", filepath.Join(t.TempDir(), "other.git"))
			case "expired":
				p.Expires = time.Now().Add(-time.Second)
			case "rejected":
				if err := os.WriteFile(filepath.Join(origin, "hooks", "pre-receive"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			old := fixtureGit(t, origin, "show-ref")
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || fixtureGit(t, origin, "show-ref") != old {
				t.Fatalf("unsafe push: %+v", result)
			}
			if scenario == "rejected" && !strings.Contains(result.Message, "rejected") {
				t.Fatalf("%+v", result)
			}
		})
	}
}

func TestPushNeverForcesDivergedHistory(t *testing.T) {
	repo, seed := pushFixture(t)
	p := prepareForTest(t, repo, "push")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "racing update")
	fixtureGit(t, seed, "push", "origin", "feature")
	origin := fixtureGit(t, repo, "remote", "get-url", "origin")
	old := fixtureGit(t, origin, "rev-parse", "feature")
	// Exercise the final push itself, as if the remote changed after validation.
	result := applyPush(context.Background(), p)
	if result.Status != "failed" || fixtureGit(t, origin, "rev-parse", "feature") != old {
		t.Fatalf("overwrote remote: %+v", result)
	}
}
