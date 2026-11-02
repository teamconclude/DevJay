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

func divergedReturnFixture(t *testing.T) (string, string, *CheckoutPlan) {
	t.Helper()
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, repo, "switch", "develop")
	writeFixture(t, repo, "local-default", "orgID\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "fix organization")
	writeFixture(t, repo, "local-default", "orgID, channel\n")
	fixtureGit(t, repo, "commit", "-am", "fix channel")
	fixtureGit(t, repo, "switch", "feature")
	// Both fixes were published on a refactored base under different commit IDs.
	writeFixture(t, seed, "local-default", "org.id, channel\n")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "fix organization and channel on newer code")
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "merge", "--squash", "origin/feature")
	fixtureGit(t, seed, "commit", "-m", "merge feature PR")
	fixtureGit(t, seed, "push")
	req := actionRequest(t, repo, "merged")
	req.DeleteBranch = true
	oldDefault := fixtureGit(t, repo, "rev-parse", "develop")
	before := checkoutFingerprint(t, repo)
	p, err := prepareCheckoutAction(context.Background(), repo, req, &MergedPullRequest{Number: 1, Head: req.Head, Base: "develop", MergedAt: time.Now().Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	after := checkoutFingerprint(t, repo)
	for key, value := range before {
		if key != "refs" && after[key] != value {
			t.Fatalf("preparation changed %s", key)
		}
	}
	if !p.ReplaceDefault || p.LocalAhead != 2 || p.RemoteAhead == 0 || p.RemoteTarget != "origin/develop" || p.targetLocalHead != oldDefault {
		t.Fatalf("missing explicit replacement plan: %+v", p)
	}
	if fixtureGit(t, repo, "rev-parse", "develop") != oldDefault || fixtureGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/devjay/default-backups/") != "" {
		t.Fatal("prepare moved or backed up default before confirmation")
	}
	return repo, seed, p
}

func TestReturnReplacesDivergedDefaultAndKeepsBothRecoveries(t *testing.T) {
	for _, remove := range []bool{false, true} {
		t.Run(fmt.Sprint(remove), func(t *testing.T) {
			repo, _, p := divergedReturnFixture(t)
			p.DeleteBranch = remove
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "complete" || result.CurrentBranch != "develop" || result.CurrentHead != p.TargetHead {
				t.Fatalf("%+v", result)
			}
			if result.DefaultRecovery != "refs/devjay/default-backups/"+p.ID || fixtureGit(t, repo, "rev-parse", result.DefaultRecovery) != p.targetLocalHead {
				t.Fatal("old default not preserved")
			}
			if fixtureGit(t, repo, "show", result.DefaultRecovery+":local-default") != "orgID, channel" {
				t.Fatal("lost old default contents")
			}
			if fixtureGit(t, repo, "rev-parse", result.Recovery) != p.Head {
				t.Fatal("merged branch not preserved")
			}
			remaining, err := refHead(context.Background(), repo, "refs/heads/feature")
			if err != nil || (remaining == "") != remove {
				t.Fatalf("wrong branch deletion: %q %v", remaining, err)
			}
			if fixtureGit(t, repo, "status", "--porcelain") != "" || fixtureGit(t, repo, "rev-parse", "@{upstream}") != p.TargetHead {
				t.Fatal("dirty checkout or lost tracking")
			}
			if result := applyCheckoutAction(context.Background(), p); result.Status != "failed" {
				t.Fatal("replayed confirmation")
			}
			for i := 0; i < 12; i++ {
				id := fmt.Sprintf("later-%d", i)
				ref := "refs/devjay/backups/" + id
				fixtureGit(t, repo, "update-ref", ref, p.TargetHead)
				finishRecovery(context.Background(), &CheckoutPlan{path: repo, ID: id, Head: p.TargetHead}, &CheckoutAction{Recovery: ref})
			}
			if fixtureGit(t, repo, "rev-parse", result.DefaultRecovery) != p.targetLocalHead {
				t.Fatal("default recovery pruned by later actions")
			}
		})
	}
}

func TestReturnReplacementStopsForChangedState(t *testing.T) {
	for _, scenario := range []string{"dirty", "staged", "untracked", "default", "tracking", "feature", "remote", "worktree", "expired", "backup-failed", "ignored"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed, p := divergedReturnFixture(t)
			switch scenario {
			case "dirty", "staged":
				writeFixture(t, repo, "feature", "unsaved work\n")
				if scenario == "staged" {
					fixtureGit(t, repo, "add", "feature")
				}
			case "untracked":
				writeFixture(t, repo, "untracked", "unsaved work\n")
			case "default":
				fixtureGit(t, repo, "update-ref", "refs/heads/develop", p.Head)
			case "tracking":
				fixtureGit(t, repo, "config", "branch.develop.merge", "refs/heads/other")
			case "feature":
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "new work")
			case "remote":
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "remote moved")
				fixtureGit(t, seed, "push")
			case "worktree":
				fixtureGit(t, repo, "worktree", "add", filepath.Join(t.TempDir(), "other"), "develop")
			case "expired":
				p.Expires = time.Now().Add(-time.Second)
			case "backup-failed":
				fixtureGit(t, repo, "update-ref", "refs/devjay/default-backups/"+p.ID, p.Head)
			case "ignored":
				if err := os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("local-default\n"), 0600); err != nil {
					t.Fatal(err)
				}
				writeFixture(t, repo, "local-default", "ignored local work\n")
			}
			before := checkoutFingerprint(t, repo)
			oldDefault := fixtureGit(t, repo, "rev-parse", "develop")
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" {
				t.Fatalf("unsafe return: %+v", result)
			}
			after := checkoutFingerprint(t, repo)
			for _, key := range []string{"head", "status", "file", "feature", ".git/index", ".git/config"} {
				if before[key] != after[key] {
					t.Fatalf("failed return changed %s: %+v", key, result)
				}
			}
			if fixtureGit(t, repo, "rev-parse", "develop") != oldDefault || fixtureGit(t, repo, "rev-parse", "feature") != after["head"] {
				t.Fatal("branch changed after failed check")
			}
			if scenario == "ignored" {
				b, _ := os.ReadFile(filepath.Join(repo, "local-default"))
				if string(b) != "ignored local work\n" {
					t.Fatal("ignored file overwritten")
				}
			}
		})
	}
}

func TestReturnReplacementStopsDuringApplication(t *testing.T) {
	for _, scenario := range []string{"default-after-detach", "default-at-update", "edits-after-detach", "reattach"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _, p := divergedReturnFixture(t)
			t.Setenv("DEVJAY_TEST_REPO", repo)
			t.Setenv("DEVJAY_TEST_RETURN_SCENARIO", scenario)
			t.Setenv("DEVJAY_TEST_OTHER_HEAD", p.Head)
			installGitShim(t, `detach=false
reattach=false
for arg in "$@"; do
 if test "$arg" = --detach; then detach=true; fi
 if test "$arg" = --no-guess; then reattach=true; fi
 if test "$arg" = 'DevJay: return to confirmed default' && test "$DEVJAY_TEST_RETURN_SCENARIO" = default-at-update; then
  "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_REPO" update-ref refs/heads/develop "$DEVJAY_TEST_OTHER_HEAD" || exit $?
 fi
done
if $reattach && test "$DEVJAY_TEST_RETURN_SCENARIO" = reattach; then exit 79; fi
if $detach; then
 "$DEVJAY_TEST_GIT" "$@" || exit $?
 case "$DEVJAY_TEST_RETURN_SCENARIO" in
 default-after-detach) "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_REPO" update-ref refs/heads/develop "$DEVJAY_TEST_OTHER_HEAD" ;;
 edits-after-detach) printf 'new edits\n' > "$DEVJAY_TEST_REPO/feature" ;;
 esac
 exit 0
fi`)
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || result.DefaultRecovery == "" {
				t.Fatalf("unsafe partial return: %+v", result)
			}
			if fixtureGit(t, repo, "rev-parse", "feature") != p.Head || fixtureGit(t, repo, "rev-parse", result.DefaultRecovery) != p.targetLocalHead {
				t.Fatal("lost original commits")
			}
			expected := p.targetLocalHead
			if strings.HasPrefix(scenario, "default-") {
				expected = p.Head
			}
			if scenario == "reattach" {
				expected = p.TargetHead
			}
			if fixtureGit(t, repo, "rev-parse", "develop") != expected {
				t.Fatal("overwrote an unexpected default tip")
			}
			if scenario == "edits-after-detach" {
				b, _ := os.ReadFile(filepath.Join(repo, "feature"))
				if string(b) != "new edits\n" {
					t.Fatal("lost new edits")
				}
			}
		})
	}
}
