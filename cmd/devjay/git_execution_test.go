package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func installGitShim(t *testing.T, body string) {
	t.Helper()
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	t.Setenv("DEVJAY_TEST_GIT", real)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte("#!/bin/sh\n"+body+"\nexec \"$DEVJAY_TEST_GIT\" \"$@\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
}

func TestCommandTimeoutStartsAfterQueue(t *testing.T) {
	slots := make(chan struct{}, 1)
	slots <- struct{}{}
	done := make(chan error, 1)
	go func() {
		_, err := runCommandWithSlots(context.Background(), slots, 50*time.Millisecond, "sh", "-c", "printf ready")
		done <- err
	}()
	select {
	case err := <-done:
		<-slots
		t.Fatalf("timed out in queue: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	<-slots
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	slots <- struct{}{}
	if _, err := runCommandWithSlots(ctx, slots, time.Second, "sh", "-c", "exit 0"); !errors.Is(err, context.Canceled) {
		t.Fatalf("queue ignored cancellation: %v", err)
	}
	<-slots
}

func TestLocalMutationFinishesAfterDeadlineWithoutBackgroundSlot(t *testing.T) {
	repo := trackedFixture(t)
	fixtureGit(t, repo, "branch", "other")
	installGitShim(t, `for arg in "$@"; do if test "$arg" = switch; then sleep .1; break; fi; done`)
	for i := 0; i < cap(remoteCommandSlots); i++ {
		remoteCommandSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(remoteCommandSlots); i++ {
			<-remoteCommandSlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := actionGit(ctx, repo, "switch", "other"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil || fixtureGit(t, repo, "branch", "--show-current") != "other" {
		t.Fatal("mutation did not finish across deadline")
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "index.lock")); !os.IsNotExist(err) {
		t.Fatal("left index lock")
	}
}

func TestNetworkPreservesEffectiveSSHCommand(t *testing.T) {
	repo := trackedFixture(t)
	config := filepath.Join(t.TempDir(), "included")
	if err := os.WriteFile(config, []byte("[core]\n sshCommand = ssh -i ~/.ssh/work_key -o IdentitiesOnly=yes\n"), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, "config", "include.path", config)
	installGitShim(t, `for arg in "$@"; do if test "$arg" = ls-remote; then printf '%s' "$GIT_SSH_COMMAND"; exit 0; fi; done`)
	for _, command := range []string{"", "ssh -i ~/.ssh/environment_key"} {
		t.Setenv("GIT_SSH_COMMAND", command)
		got, err := actionGit(context.Background(), repo, "ls-remote", "origin")
		want := command
		if want == "" {
			want = "ssh -i ~/.ssh/work_key -o IdentitiesOnly=yes"
		}
		if err != nil || string(got) != nonInteractiveSSH(want) {
			t.Fatalf("SSH identity lost: %q %v", got, err)
		}
	}
}

func TestAmbiguousUpstreamStatus(t *testing.T) {
	repo := trackedFixture(t)
	fixtureGit(t, repo, "branch", "origin/main")
	s, m := inspectLocal(context.Background(), repo)
	if m.ReadError != nil || s.Tracking != "origin/main" || m.Ref != "refs/remotes/origin/main" {
		t.Fatalf("%+v %+v", s, m)
	}
}

func TestBackgroundFetchDoesNotRunHooks(t *testing.T) {
	repo, seed := rebaseFixture(t, false)
	marker := filepath.Join(t.TempDir(), "hook-ran")
	t.Setenv("DEVJAY_HOOK_MARKER", marker)
	hooks := t.TempDir()
	if err := os.WriteFile(filepath.Join(hooks, "reference-transaction"), []byte("#!/bin/sh\nprintf ran > \"$DEVJAY_HOOK_MARKER\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, "config", "core.hooksPath", hooks)
	fixtureGit(t, seed, "fetch", "origin", "feature")
	fixtureGit(t, seed, "switch", "-c", "feature", "origin/feature")
	fixtureGit(t, seed, "commit", "--allow-empty", "-m", "advance feature")
	fixtureGit(t, seed, "push", "origin", "feature")
	_, m := inspectLocal(context.Background(), repo)
	var c remoteCache
	refreshRemote(context.Background(), m, &c)
	if c.Error != "" || c.BaseError != "" {
		t.Fatalf("%+v", c)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("background fetch ran reference-transaction hook")
	}
}

func TestLargeIndexSafetyChecksStreamAllRecords(t *testing.T) {
	repo := trackedFixture(t)
	installGitShim(t, `if test "$3" = ls-files && test "$4" = --stage; then
 awk 'BEGIN { for(i=0;i<80000;i++) printf "100644 0123456789012345678901234567890123456789 0\tfile-%08d%c", i, 0 }'
 if test "$DEVJAY_TEST_SUBMODULE" = yes; then printf '160000 0123456789012345678901234567890123456789 0\tsubmodule\000'; fi
 exit 0
fi`)
	if _, _, err := cleanCheckout(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVJAY_TEST_SUBMODULE", "yes")
	if _, _, err := cleanCheckout(context.Background(), repo); err == nil || !strings.Contains(err.Error(), "submodules") {
		t.Fatalf("missed unsafe entry after 4 MiB: %v", err)
	}
}

func TestSuccessfulActionsTolerateConcurrentAutosave(t *testing.T) {
	for _, kind := range []string{"push", "update"} {
		t.Run(kind, func(t *testing.T) {
			var repo string
			if kind == "push" {
				repo, _ = pushFixture(t)
			} else {
				repo, _ = updateFixture(t)
			}
			p := prepareForTest(t, repo, kind)
			t.Setenv("DEVJAY_TEST_REPO", repo)
			t.Setenv("DEVJAY_TEST_VERB", map[string]string{"push": "push", "update": "merge"}[kind])
			installGitShim(t, `for arg in "$@"; do
 if test "$arg" = "$DEVJAY_TEST_VERB"; then
  "$DEVJAY_TEST_GIT" "$@" || exit $?
  printf 'editor autosave\n' > "$DEVJAY_TEST_REPO/file"
  exit 0
 fi
done`)
			result := applyCheckoutAction(context.Background(), p)
			if result.Status != "complete" {
				t.Fatalf("successful %s reported failed: %+v", kind, result)
			}
			if kind == "update" && !strings.Contains(result.Message, "Local edits") {
				t.Fatal("autosave not noted")
			}
		})
	}
}

func TestRebaseBranchTrackingDefault(t *testing.T) {
	repo, _ := rebaseFixture(t, false)
	fixtureGit(t, repo, "branch", "--set-upstream-to=origin/develop", "feature")
	fixtureGit(t, repo, "config", "user.name", "DevJay test")
	fixtureGit(t, repo, "config", "user.email", "test@example.invalid")
	p := prepareForTest(t, repo, "rebase")
	result := applyCheckoutAction(context.Background(), p)
	if result.Status != "complete" {
		t.Fatalf("%+v", result)
	}
	if fixtureGit(t, repo, "rev-list", "--count", "origin/develop..HEAD") != "1" {
		t.Fatal("feature commit not rebased onto default")
	}
}

func TestRecoveryRetentionNeverDeletesFailedOrChangedBackups(t *testing.T) {
	repo := trackedFixture(t)
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "update-ref", "refs/devjay/backups/failed", head)
	for i := 0; i < 12; i++ {
		id := fmt.Sprintf("success-%02d", i)
		backup := "refs/devjay/backups/" + id
		fixtureGit(t, repo, "update-ref", backup, head)
		finishRecovery(context.Background(), &CheckoutPlan{path: repo, ID: id, Head: head}, &CheckoutAction{Recovery: backup})
	}
	refs := strings.Fields(fixtureGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/devjay/backups/"))
	if len(refs) != 11 || !strings.Contains(strings.Join(refs, " "), "backups/failed") || strings.Contains(strings.Join(refs, " "), "success-00") {
		t.Fatalf("unexpected retained refs: %v", refs)
	}
}

func TestSSHHostAliasGetsExplicitGitHubNote(t *testing.T) {
	for _, remote := range []string{"git@github-work:org/repo.git", "github-work:org/repo.git", "ssh://github-work/org/repo.git"} {
		var cache remoteCache
		refreshRemote(context.Background(), gitMeta{RemoteURL: remote}, &cache)
		if !strings.Contains(cache.GitHub, "unavailable") {
			t.Fatalf("silently dropped alias: %+v", cache)
		}
	}
}

func TestLinkedWorktreeFetchesShareOneLock(t *testing.T) {
	repo := trackedFixture(t)
	worktree := filepath.Join(t.TempDir(), "linked")
	fixtureGit(t, repo, "worktree", "add", "-b", "linked", worktree)
	t.Setenv("DEVJAY_TEST_FETCH_LOCK", filepath.Join(t.TempDir(), "fetch-running"))
	installGitShim(t, `for arg in "$@"; do if test "$arg" = fetch; then
 mkdir "$DEVJAY_TEST_FETCH_LOCK" 2>/dev/null || exit 79
 sleep .1
 "$DEVJAY_TEST_GIT" "$@"
 result=$?
 rmdir "$DEVJAY_TEST_FETCH_LOCK"
 exit "$result"
fi; done`)
	results := make(chan error, 2)
	for _, path := range []string{repo, worktree} {
		go func() {
			_, err := networkGit(context.Background(), remoteCommandSlots, time.Second, path, "fetch", "origin")
			results <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Errorf("overlapping shared-repo fetch: %v", err)
		}
	}
}

func TestBranchChangeCancelsOldRemoteRefresh(t *testing.T) {
	repo := trackedFixture(t)
	fixtureGit(t, repo, "branch", "next")
	fixtureGit(t, repo, "push", "-u", "origin", "next")
	marker := filepath.Join(t.TempDir(), "old-fetch-started")
	t.Setenv("DEVJAY_TEST_OLD_FETCH", marker)
	installGitShim(t, `for arg in "$@"; do
 case "$arg" in +refs/heads/main:*) printf started > "$DEVJAY_TEST_OLD_FETCH"; exec sleep 20 ;; esac
done`)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	done := make(chan struct{})
	statuses := make(chan GitStatus, 32)
	go func() { defer close(done); watchGit(ctx, repo, func(s GitStatus) { statuses <- s }) }()
	defer func() { cancel(); <-done }()
	wait := time.NewTicker(10 * time.Millisecond)
	defer wait.Stop()
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("old refresh never started")
		case <-wait.C:
		}
	}
	fixtureGit(t, repo, "switch", "next")
	for {
		select {
		case <-ctx.Done():
			t.Fatal("new branch waited for old branch's 20-second fetch")
		case s := <-statuses:
			if s.Branch == "next" && s.RemoteChecked != "" && s.RemoteNote == "" {
				return
			}
		}
	}
}
