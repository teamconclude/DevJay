package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func actionRequest(t *testing.T, repo, kind string) ActionRequest {
	t.Helper()
	s, m := inspectLocal(context.Background(), repo)
	if m.ReadError != nil {
		t.Fatal(m.ReadError)
	}
	return ActionRequest{Kind: kind, Branch: s.Branch, Head: m.OID}
}
func updateFixture(t *testing.T) (string, string) {
	t.Helper()
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, repo, "switch", "develop")
	return repo, seed
}
func prepareForTest(t *testing.T, repo, kind string) *CheckoutPlan {
	t.Helper()
	p, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, kind), nil)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func TestApplyFastForwardAndPreserveRecovery(t *testing.T) {
	repo, seed := updateFixture(t)
	old := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "config", "branch.develop.mergeOptions", "--squash")
	p := prepareForTest(t, repo, "update")
	if fixtureGit(t, repo, "rev-parse", "HEAD") != old {
		t.Fatal("prepare changed branch")
	}
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "complete" {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, repo, "rev-parse", "HEAD") != fixtureGit(t, seed, "rev-parse", "HEAD") {
		t.Fatal("not updated")
	}
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != old {
		t.Fatal("missing recovery")
	}
	if fixtureGit(t, repo, "status", "--porcelain") != "" {
		t.Fatal("dirty result")
	}
	if result := applyCheckoutAction(context.Background(), p); result.Status != "failed" {
		t.Fatal("replayed old confirmation")
	}
}
func TestActionsRejectChangesAfterPreparation(t *testing.T) {
	for _, scenario := range []string{"dirty", "staged", "untracked", "branch", "commit", "remote", "operation", "expired", "ignored"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := updateFixture(t)
			if scenario == "ignored" {
				writeFixture(t, seed, "ignored", "remote data")
				fixtureGit(t, seed, "add", ".")
				fixtureGit(t, seed, "commit", "-m", "new file")
				fixtureGit(t, seed, "push")
			}
			p := prepareForTest(t, repo, "update")
			switch scenario {
			case "dirty", "staged":
				writeFixture(t, repo, "file", "local edits")
				if scenario == "staged" {
					fixtureGit(t, repo, "add", "file")
				}
			case "untracked":
				writeFixture(t, repo, "untracked", "keep me")
			case "branch":
				fixtureGit(t, repo, "switch", "-c", "other")
			case "commit":
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "new local")
			case "remote":
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "new remote")
				fixtureGit(t, seed, "push")
			case "operation":
				os.WriteFile(filepath.Join(repo, ".git", "CHERRY_PICK_HEAD"), []byte(p.Head), 0600)
			case "expired":
				p.Expires = time.Now().Add(-time.Second)
			case "ignored":
				os.WriteFile(filepath.Join(repo, ".git", "info", "exclude"), []byte("ignored\n"), 0600)
				writeFixture(t, repo, "ignored", "ignored local work")
			}
			before := fixtureGit(t, repo, "rev-parse", "HEAD")
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || fixtureGit(t, repo, "rev-parse", "HEAD") != before {
				t.Fatalf("unsafe result: %+v", result)
			}
			if scenario == "ignored" {
				b, _ := os.ReadFile(filepath.Join(repo, "ignored"))
				if string(b) != "ignored local work" {
					t.Fatal("overwrote ignored work")
				}
			}
		})
	}
}
func TestActionsRefuseDivergenceAndHiddenIndexChanges(t *testing.T) {
	for _, scenario := range []string{"diverged", "assume-unchanged", "skip-worktree"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _ := updateFixture(t)
			if scenario == "diverged" {
				fixtureGit(t, repo, "commit", "--allow-empty", "-m", "local")
			} else {
				fixtureGit(t, repo, "update-index", "--"+scenario, "file")
				writeFixture(t, repo, "file", "hidden work")
			}
			if _, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "update"), nil); err == nil {
				t.Fatal("accepted unsafe action")
			}
		})
	}
}
func TestApplyCheckedRebasePreservesIdentityAndOriginalCommits(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	fixtureGit(t, repo, "config", "user.name", "Action Test")
	fixtureGit(t, repo, "config", "user.email", "action@example.invalid")
	p := prepareForTest(t, repo, "rebase")
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "complete" {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, repo, "branch", "--show-current") != "feature" {
		t.Fatal("detached or wrong branch")
	}
	fixtureGit(t, repo, "merge-base", "--is-ancestor", fixtureGit(t, seed, "rev-parse", "HEAD"), "HEAD")
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != p.Head {
		t.Fatal("original commits lost")
	}
	if fixtureGit(t, repo, "show", "-s", "--format=%cn <%ce>", "HEAD") != "Action Test <action@example.invalid>" {
		t.Fatal("wrong committer identity")
	}
	if fixtureGit(t, repo, "status", "--porcelain") != "" {
		t.Fatal("dirty after rebase")
	}
	if fixtureGit(t, repo, "rev-parse", "origin/feature") != p.Head {
		t.Fatal("pushed automatically")
	}
}
func TestConflictingRebaseCannotBePrepared(t *testing.T) {
	repo, _ := rebaseFixture(t, true)
	before := checkoutFingerprint(t, repo)
	_, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "rebase"), nil)
	if err == nil || !strings.Contains(err.Error(), "manually") {
		t.Fatalf("%v", err)
	}
	after := checkoutFingerprint(t, repo)
	for _, key := range []string{"head", "status", "file", "feature", ".git/index"} {
		if before[key] != after[key] {
			t.Fatalf("changed %s", key)
		}
	}
}

func TestCardRebasePreparationFetchesLatestAndPinsDefaultIdentity(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	before := checkoutFingerprint(t, repo)
	req := actionRequest(t, repo, "rebase")
	req.TargetBranch, req.TargetRemote = "develop", "origin"
	for _, field := range []string{"branch", "remote"} {
		changed := req
		if field == "branch" {
			changed.TargetBranch = "main"
		} else {
			changed.TargetRemote = "other"
		}
		if _, err := prepareCheckoutAction(context.Background(), repo, changed, nil); err == nil || !strings.Contains(err.Error(), "default branch changed") {
			t.Fatalf("accepted different %s: %v", field, err)
		}
	}
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "newest default commit")
	fixtureGit(t, seed, "push")
	plan, err := prepareCheckoutAction(context.Background(), repo, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Target != "develop" || plan.TargetHead != fixtureGit(t, seed, "rev-parse", "HEAD") {
		t.Fatalf("did not prepare latest develop: %+v", plan)
	}
	after := checkoutFingerprint(t, repo)
	for _, key := range []string{"head", "status", "file", "feature", ".git/index"} {
		if before[key] != after[key] {
			t.Fatalf("preparation changed %s before confirmation", key)
		}
	}
}
func TestReturnToDefaultAndOptionalDeletion(t *testing.T) {
	for _, scenario := range []string{"keep", "delete", "autosave", "new-default", "changed-default", "worktree"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			if scenario == "new-default" {
				fixtureGit(t, repo, "branch", "-D", "develop")
			}
			req := actionRequest(t, repo, "merged")
			req.DeleteBranch = scenario == "delete" || scenario == "autosave"
			merged := &MergedPullRequest{Number: 1, Head: req.Head, Base: "develop", MergedAt: time.Now().Add(time.Second)}
			p, err := prepareCheckoutAction(context.Background(), repo, req, merged)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "autosave" {
				t.Setenv("DEVJAY_TEST_REPO", repo)
				installGitShim(t, `for arg in "$@"; do if test "$arg" = merge; then
 "$DEVJAY_TEST_GIT" "$@" || exit $?
 printf 'autosave' > "$DEVJAY_TEST_REPO/file"
 exit 0
fi; done`)
			}
			if scenario == "changed-default" {
				fixtureGit(t, repo, "branch", "-f", "develop", req.Head)
			}
			if scenario == "worktree" {
				fixtureGit(t, repo, "worktree", "add", filepath.Join(t.TempDir(), "other"), "develop")
			}
			result := applyCheckoutAction(context.Background(), p)
			if scenario == "changed-default" || scenario == "worktree" {
				if result.Status != "failed" || fixtureGit(t, repo, "branch", "--show-current") != "feature" {
					t.Fatalf("%+v", result)
				}
				return
			}
			if result.Status != "complete" {
				t.Fatalf("%+v", result)
			}
			if fixtureGit(t, repo, "branch", "--show-current") != "develop" || fixtureGit(t, repo, "rev-parse", "HEAD") != fixtureGit(t, seed, "rev-parse", "HEAD") {
				t.Fatal("not on latest default")
			}
			remaining, err := refHead(context.Background(), repo, "refs/heads/feature")
			if err != nil || (scenario == "delete") != (remaining == "") {
				t.Fatalf("branch retention: %q %v", remaining, err)
			}
			if fixtureGit(t, repo, "rev-parse", result.Recovery) != req.Head {
				t.Fatal("missing recovery")
			}
		})
	}
}
func TestReturnRequiresExactMergedHeadAndBaseEvenWhenKeepingBranch(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	req := actionRequest(t, repo, "merged")
	before := checkoutFingerprint(t, repo)
	for _, remove := range []bool{false, true} {
		req.DeleteBranch = remove
		for _, pr := range []*MergedPullRequest{nil, {Head: "other", Base: "develop"}, {Head: req.Head, Base: "other"}} {
			if _, err := prepareCheckoutAction(context.Background(), repo, req, pr); err == nil {
				t.Fatal("nonmatching merged branch accepted")
			}
		}
	}
	if !reflect.DeepEqual(before, checkoutFingerprint(t, repo)) {
		t.Fatal("rejected action changed checkout")
	}
	// Even an exact merged tip cannot return with tracked or untracked edits.
	for _, name := range []string{"feature", "untracked"} {
		writeFixture(t, repo, name, "unsaved work\n")
		if _, err := prepareCheckoutAction(context.Background(), repo, req, &MergedPullRequest{Number: 1, Head: req.Head, Base: "develop", MergedAt: time.Now().Add(time.Second)}); err == nil {
			t.Fatal("dirty merged checkout accepted")
		}
		if name == "feature" {
			fixtureGit(t, repo, "restore", "--", name)
		}
	}
}

func TestMergedCardActionAfterRemoteDeletion(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	// Model a real merged PR whose remote feature branch has been removed.
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "merge", "--no-edit", "origin/feature")
	fixtureGit(t, seed, "push")
	fixtureGit(t, seed, "push", "origin", "--delete", "feature")
	merged := &MergedPullRequest{Number: 42, Head: head, Base: "develop", SameHead: true, MergedAt: time.Now().Add(time.Second)}
	status := GitStatus{Branch: "feature", Head: head, DefaultBranch: "develop", Tracking: "origin/feature", MergedPR: merged}
	option := makeCardGitOption(cardMenuRequest{Kind: "site", Number: 42}, status)
	req, ok := option.request("site", status)
	if !ok {
		t.Fatal("missing merged cleanup request")
	}
	before := checkoutFingerprint(t, repo)
	changed := req
	changed.TargetBranch = "main"
	if _, err := prepareCheckoutAction(context.Background(), repo, changed, merged); err == nil || !strings.Contains(err.Error(), "default branch changed") {
		t.Fatalf("changed menu destination accepted: %v", err)
	}
	plan, err := prepareCheckoutAction(context.Background(), repo, req, merged)
	if err != nil {
		t.Fatal(err)
	}
	after := checkoutFingerprint(t, repo)
	for _, key := range []string{"head", "status", "file", "feature", ".git/index", ".git/config"} {
		if before[key] != after[key] {
			t.Fatalf("preparing cleanup changed %s", key)
		}
	}
	result := applyCheckoutAction(context.Background(), plan)
	if result.Status != "complete" || fixtureGit(t, repo, "branch", "--show-current") != "develop" || fixtureGit(t, repo, "rev-parse", "HEAD") != fixtureGit(t, seed, "rev-parse", "HEAD") {
		t.Fatalf("cleanup did not return to updated develop: %+v", result)
	}
	if remaining, err := refHead(context.Background(), repo, "refs/heads/feature"); err != nil || remaining != "" {
		t.Fatalf("local branch not deleted: %q %v", remaining, err)
	}
	if fixtureGit(t, repo, "rev-parse", result.Recovery) != head {
		t.Fatal("missing recovery commits")
	}
}

func TestRebaseStopsForIgnoredCollisionAndNewUpstream(t *testing.T) {
	for _, scenario := range []string{"ignored", "upstream"} {
		t.Run(scenario, func(t *testing.T) {
			repo, seed := rebaseFixture(t, false)
			fixtureGit(t, repo, "config", "user.name", "Action Test")
			fixtureGit(t, repo, "config", "user.email", "action@example.invalid")
			if scenario == "ignored" {
				writeFixture(t, seed, "ignored", "remote")
				fixtureGit(t, seed, "add", ".")
				fixtureGit(t, seed, "commit", "-m", "new file")
				fixtureGit(t, seed, "push")
			}
			p := prepareForTest(t, repo, "rebase")
			if scenario == "ignored" {
				writeFixture(t, repo, ".git/info/exclude", "ignored\n")
				writeFixture(t, repo, "ignored", "keep local work")
			} else {
				fixtureGit(t, seed, "fetch", "origin", "feature")
				fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
				fixtureGit(t, seed, "commit", "--allow-empty", "-m", "colleague update")
				fixtureGit(t, seed, "push", "origin", "feature")
			}
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "failed" || fixtureGit(t, repo, "rev-parse", "HEAD") != p.Head || fixtureGit(t, repo, "branch", "--show-current") != p.Branch {
				t.Fatalf("%+v", result)
			}
			if scenario == "ignored" {
				b, _ := os.ReadFile(filepath.Join(repo, "ignored"))
				if string(b) != "keep local work" {
					t.Fatal("ignored work lost")
				}
			}
		})
	}
}
