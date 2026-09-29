package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func switchRequest(t *testing.T, repo, target, remote string, update bool) ActionRequest {
	t.Helper()
	req := actionRequest(t, repo, "switch")
	req.TargetBranch, req.TargetRemote, req.UpdateTarget = target, remote, update
	return req
}

func prepareSwitch(t *testing.T, repo, target, remote string, update bool) *CheckoutPlan {
	t.Helper()
	p, err := prepareCheckoutAction(context.Background(), repo, switchRequest(t, repo, target, remote, update), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func switchFixture(t *testing.T) string {
	t.Helper()
	repo := trackedFixture(t)
	fixtureGit(t, repo, "switch", "-c", "source")
	return repo
}

func TestBranchSwitchKeepsLocalByDefaultAndOffersOnlyFastForward(t *testing.T) {
	for _, update := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep local", true: "use remote"}[update], func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			original := fixtureGit(t, repo, "rev-parse", "HEAD")
			oldTarget := fixtureGit(t, repo, "rev-parse", "refs/heads/develop")
			newTarget := fixtureGit(t, seed, "rev-parse", "HEAD")
			p := prepareSwitch(t, repo, "develop", "", update)
			if !p.CanUpdateTarget || p.LocalAhead != 0 || p.RemoteAhead != 1 || p.UpdateTarget != update {
				t.Fatalf("bad offer: %+v", p)
			}
			if fixtureGit(t, repo, "rev-parse", "HEAD") != original || fixtureGit(t, repo, "rev-parse", "refs/heads/develop") != oldTarget {
				t.Fatal("prepare changed a local branch")
			}
			want := oldTarget
			if update {
				want = newTarget
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "complete" || result.CurrentBranch != "develop" || result.CurrentHead != want {
				t.Fatalf("result: %+v", result)
			}
			if fixtureGit(t, repo, "rev-parse", "refs/heads/feature") != original || fixtureGit(t, repo, "rev-parse", result.Recovery) != original {
				t.Fatal("source or recovery was lost")
			}
			if fixtureGit(t, repo, "status", "--porcelain") != "" {
				t.Fatal("switch left local changes")
			}
		})
	}
}

func TestBranchSwitchPreservesAheadAndDivergedLocalTargets(t *testing.T) {
	for _, diverged := range []bool{false, true} {
		t.Run(map[bool]string{false: "ahead", true: "diverged"}[diverged], func(t *testing.T) {
			var repo, target string
			if diverged {
				repo, _ = rebaseFixture(t, false)
				target = "develop"
			} else {
				repo = switchFixture(t)
				target = "main"
			}
			source := fixtureGit(t, repo, "branch", "--show-current")
			fixtureGit(t, repo, "switch", target)
			fixtureGit(t, repo, "commit", "--allow-empty", "-m", "unpublished local work")
			local := fixtureGit(t, repo, "rev-parse", "HEAD")
			fixtureGit(t, repo, "switch", source)
			p := prepareSwitch(t, repo, target, "", false)
			if p.CanUpdateTarget || p.LocalAhead != 1 || p.TargetHead != local {
				t.Fatalf("offered overwrite: %+v", p)
			}
			if _, err := prepareCheckoutAction(context.Background(), repo, switchRequest(t, repo, target, "", true), nil); err == nil {
				t.Fatal("accepted explicit unsafe update")
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "complete" || result.CurrentHead != local {
				t.Fatalf("local work changed: %+v", result)
			}
		})
	}
}

func TestBranchSwitchRejectsDirtyCheckoutAndShorthand(t *testing.T) {
	for _, dirty := range []string{"unstaged", "staged", "untracked"} {
		t.Run(dirty, func(t *testing.T) {
			repo := switchFixture(t)
			if dirty == "untracked" {
				writeFixture(t, repo, "new-file", "keep")
			} else {
				writeFixture(t, repo, "file", "keep")
				if dirty == "staged" {
					fixtureGit(t, repo, "add", "file")
				}
			}
			before := fixtureGit(t, repo, "rev-parse", "HEAD")
			if _, err := prepareCheckoutAction(context.Background(), repo, switchRequest(t, repo, "main", "", false), nil); err == nil {
				t.Fatal("dirty switch was allowed")
			}
			if fixtureGit(t, repo, "rev-parse", "HEAD") != before || fixtureGit(t, repo, "status", "--porcelain") == "" {
				t.Fatal("local work was changed")
			}
		})
	}
	repo := switchFixture(t)
	for _, name := range []string{"@", "@{-1}", "main~1", "-main", "HEAD", "source", "refs/heads/main"} {
		if _, err := prepareCheckoutAction(context.Background(), repo, switchRequest(t, repo, name, "", false), nil); err == nil {
			t.Errorf("accepted non-choice %q", name)
		}
	}
}

func TestBranchSwitchWorksOfflineAndWithoutTracking(t *testing.T) {
	for _, tracking := range []bool{false, true} {
		t.Run(map[bool]string{false: "no upstream", true: "unavailable upstream"}[tracking], func(t *testing.T) {
			repo := switchFixture(t)
			if tracking {
				fixtureGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
			} else {
				fixtureGit(t, repo, "branch", "--unset-upstream", "main")
			}
			p := prepareSwitch(t, repo, "main", "", false)
			if p.RemoteNote == "" || p.CanUpdateTarget {
				t.Fatalf("missing unavailable note: %+v", p)
			}
			if result := applyCheckoutAction(context.Background(), p); result.Status != "complete" || result.CurrentBranch != "main" {
				t.Fatalf("offline local switch failed: %+v", result)
			}
		})
	}
}

func TestBranchSwitchRejectsChangesAfterConfirmation(t *testing.T) {
	for _, change := range []string{"destination", "destination config", "destination worktree", "source edits", "expired", "remote"} {
		t.Run(change, func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			p := prepareSwitch(t, repo, "develop", "", change == "remote")
			switch change {
			case "destination":
				fixtureGit(t, repo, "update-ref", "refs/heads/develop", p.Head)
			case "destination config":
				fixtureGit(t, repo, "config", "branch.develop.merge", "refs/heads/other")
			case "destination worktree":
				fixtureGit(t, repo, "worktree", "add", filepath.Join(t.TempDir(), "other"), "develop")
			case "source edits":
				writeFixture(t, repo, "file", "keep me")
			case "expired":
				p.Expires = time.Now().Add(-time.Second)
			case "remote":
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "new remote")
				fixtureGit(t, seed, "push")
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || fixtureGit(t, repo, "branch", "--show-current") != "feature" || fixtureGit(t, repo, "rev-parse", "HEAD") != p.Head {
				t.Fatalf("stale action applied: %+v", result)
			}
		})
	}
}

func TestBranchSwitchProtectsIgnoredIncomingFiles(t *testing.T) {
	repo := switchFixture(t)
	fixtureGit(t, repo, "switch", "main")
	writeFixture(t, repo, "ignored", "incoming")
	fixtureGit(t, repo, "add", "ignored")
	fixtureGit(t, repo, "commit", "-m", "incoming file")
	fixtureGit(t, repo, "switch", "source")
	if err := os.WriteFile(filepath.Join(repo, ".git/info/exclude"), []byte("ignored\n"), 0600); err != nil {
		t.Fatal(err)
	}
	writeFixture(t, repo, "ignored", "precious local data")
	p := prepareSwitch(t, repo, "main", "", false)
	result := applyCheckoutAction(context.Background(), p)
	data, err := os.ReadFile(filepath.Join(repo, "ignored"))
	if result.Status != "failed" || err != nil || string(data) != "precious local data" || fixtureGit(t, repo, "branch", "--show-current") != "source" || fixtureGit(t, repo, "rev-parse", "HEAD") != p.Head {
		t.Fatalf("ignored data was not protected: %+v", result)
	}
}

func TestBranchSwitchFetchCannotMoveLocalThroughSymbolicTrackingRef(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	old := fixtureGit(t, repo, "rev-parse", "refs/heads/develop")
	source := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "branch", "protected", old)
	fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/develop", "refs/heads/protected")
	p := prepareSwitch(t, repo, "develop", "", false)
	if !p.CanUpdateTarget || fixtureGit(t, repo, "rev-parse", "refs/heads/protected") != old || fixtureGit(t, repo, "rev-parse", "refs/heads/develop") != old || fixtureGit(t, repo, "rev-parse", "HEAD") != source {
		t.Fatal("preparation changed a local branch through the remote-tracking ref")
	}
	if fixtureGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/devjay/prepared/") != "" {
		t.Fatal("temporary fetched ref leaked")
	}
}

func TestBranchSwitchDetectsSymbolicDestinationAfterDetaching(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	p := prepareSwitch(t, repo, "develop", "", true)
	fixtureGit(t, repo, "branch", "protected", p.targetLocalHead)
	t.Setenv("DEVJAY_TEST_SWITCH_REPO", repo)
	installGitShim(t, `case "$*" in *'switch '*--detach*)
 "$DEVJAY_TEST_GIT" "$@" || exit $?
 "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_SWITCH_REPO" symbolic-ref refs/heads/develop refs/heads/protected
 exit 0
 ;; esac`)
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "failed" || fixtureGit(t, repo, "rev-parse", "refs/heads/protected") != p.targetLocalHead || !strings.Contains(result.Message, "detached") {
		t.Fatalf("symbolic target not protected: %+v", result)
	}
}

func TestBranchSwitchCreatesRemoteTrackingBranchAndRejectsCollision(t *testing.T) {
	for _, collide := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "collision"}[collide], func(t *testing.T) {
			repo := switchFixture(t)
			fixtureGit(t, repo, "push", "origin", "HEAD:refs/heads/remote-feature")
			fixtureGit(t, repo, "fetch", "origin")
			fixtureGit(t, repo, "tag", "remote-feature")
			p := prepareSwitch(t, repo, "remote-feature", "origin", false)
			if !p.CreateTarget || p.RemoteTarget != "origin/remote-feature" {
				t.Fatalf("incorrect creation plan: %+v", p)
			}
			if collide {
				fixtureGit(t, repo, "branch", "remote-feature")
			}
			result := applyCheckoutAction(context.Background(), p)
			if collide {
				if result.Status != "failed" || fixtureGit(t, repo, "branch", "--show-current") != "source" {
					t.Fatalf("collision applied: %+v", result)
				}
			} else if result.Status != "complete" || result.CurrentBranch != "remote-feature" || fixtureGit(t, repo, "rev-parse", "--symbolic-full-name", "@{upstream}") != "refs/remotes/origin/remote-feature" {
				t.Fatalf("tracking creation failed: %+v", result)
			}
		})
	}
}

func TestBranchSwitchTrackingFailureReportsPartialCheckout(t *testing.T) {
	repo := switchFixture(t)
	fixtureGit(t, repo, "push", "origin", "HEAD:refs/heads/new-branch")
	p := prepareSwitch(t, repo, "new-branch", "origin", false)
	installGitShim(t, `case "$*" in *'branch --set-upstream-to='*) exit 73 ;; esac`)
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "failed" || result.CurrentBranch != "new-branch" || result.CurrentHead != p.TargetHead || !strings.Contains(result.Message, "tracking could not be configured") {
		t.Fatalf("partial action not reported: %+v", result)
	}
	if fixtureGit(t, repo, "rev-parse", "refs/heads/source") != p.Head {
		t.Fatal("source lost after tracking failure")
	}
}

func TestBranchSwitchFastForwardCASDoesNotOverwriteConcurrentTarget(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	p := prepareSwitch(t, repo, "develop", "", true)
	other := fixtureGit(t, repo, "commit-tree", fixtureGit(t, repo, "rev-parse", p.targetLocalHead+"^{tree}"), "-p", p.targetLocalHead, "-m", "external local work")
	t.Setenv("DEVJAY_TEST_SWITCH_REPO", repo)
	t.Setenv("DEVJAY_TEST_SWITCH_OTHER", other)
	installGitShim(t, `case "$*" in *'DevJay: fast-forward destination before switch'*) "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_SWITCH_REPO" update-ref refs/heads/develop "$DEVJAY_TEST_SWITCH_OTHER" ;; esac`)
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "failed" || fixtureGit(t, repo, "rev-parse", "refs/heads/develop") != other || !strings.Contains(result.Message, "detached") {
		t.Fatalf("concurrent target overwritten: %+v", result)
	}
}

func TestBranchSwitchRejectsDestinationSubmodules(t *testing.T) {
	repo := switchFixture(t)
	fixtureGit(t, repo, "switch", "main")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "update-index", "--add", "--cacheinfo", "160000,"+head+",nested")
	fixtureGit(t, repo, "commit", "-m", "gitlink")
	fixtureGit(t, repo, "switch", "source")
	if _, err := prepareCheckoutAction(context.Background(), repo, switchRequest(t, repo, "main", "", false), nil); err == nil || !strings.Contains(err.Error(), "submodules") {
		t.Fatalf("gitlink target accepted: %v", err)
	}
}
