package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestGitMonitorRejectsReplacementSiteFolder(t *testing.T) {
	repo := trackedFixture(t)
	control := newGitWatchControl()
	control.folderIdentity = folderIdentity(repo)
	if control.folderIdentity == "" {
		t.Fatal("missing folder identity")
	}
	if err := os.Rename(repo, repo+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(repo + "-moved") })
	if err := os.Mkdir(repo, 0700); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, repo, "init")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	statuses := make(chan GitStatus, 1)
	done := make(chan struct{})
	go func() {
		defer close(done)
		watchGitWithControl(ctx, repo, control, func(s GitStatus) {
			select {
			case statuses <- s:
			default:
			}
		})
	}()
	select {
	case status := <-statuses:
		if status.Head != "" || status.Branch != "" || !strings.Contains(status.LocalNote, "Folder moved") {
			t.Fatalf("read replacement folder: %+v", status)
		}
	case <-ctx.Done():
		t.Fatal("no missing-folder status")
	}
	cancel()
	<-done
}

func TestGitChangeScheduleCoalescesAndBoundsWrites(t *testing.T) {
	now := time.Unix(100, 0)
	var schedule gitChangeSchedule
	schedule.changed(now)
	schedule.changed(now.Add(200 * time.Millisecond))
	if schedule.due != now.Add(500*time.Millisecond) {
		t.Fatal("save burst did not debounce")
	}
	for elapsed := 400 * time.Millisecond; elapsed < 3*time.Second; elapsed += 200 * time.Millisecond {
		schedule.changed(now.Add(elapsed))
	}
	if schedule.due != now.Add(gitChangeMaximum) {
		t.Fatal("continuous writes postponed the status forever")
	}
	schedule.clear()
	schedule.changed(now.Add(time.Hour))
	if schedule.due != now.Add(time.Hour+gitChangeDebounce) {
		t.Fatal("completed batch retained its deadline")
	}
}

func TestGitWatchControlCoalescesAndCancelsOnSleep(t *testing.T) {
	control := newGitWatchControl()
	ctx, cancel := control.workContext(context.Background())
	defer cancel()
	control.setActivity(false, false)
	if len(control.wake) != 0 {
		t.Fatal("unchanged state woke the worker")
	}
	control.setActivity(true, false)
	control.setActivity(true, false)
	if len(control.wake) != 1 {
		t.Fatal("activation was not coalesced")
	}
	<-control.wake
	control.setActivity(true, true)
	if ctx.Err() == nil {
		t.Fatal("sleep did not cancel an in-flight read")
	}
	control.refresh()
	active, paused, serial := control.state()
	if !active || !paused || serial != 1 || len(control.wake) != 1 {
		t.Fatal("paused explicit refresh lost its invalidation")
	}
}

func TestGitObservationRootsResolveLinkedWorktreeAndNestedSite(t *testing.T) {
	repo := trackedFixture(t)
	nested := filepath.Join(repo, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	r := discoverGitObservationRoots(context.Background(), nested)
	if r.worktree != gitObservationPath(repo) || r.gitDir != filepath.Join(r.worktree, ".git") || len(r.watch) != 1 {
		t.Fatalf("nested repository roots: %+v", r)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	fixtureGit(t, repo, "worktree", "add", "-b", "linked", linked)
	r = discoverGitObservationRoots(context.Background(), linked)
	if r.worktree != gitObservationPath(linked) || r.common != filepath.Join(gitObservationPath(repo), ".git") || r.gitDir == r.common {
		t.Fatalf("linked worktree roots: %+v", r)
	}
	if !slices.Contains(r.watch, r.common) || !slices.Contains(r.watch, r.worktree) {
		t.Fatalf("common refs or working files are not observed: %+v", r)
	}
	for _, path := range []string{filepath.Join(r.common, "refs/heads/linked"), filepath.Join(r.gitDir, "index"), filepath.Join(r.worktree, "file.go")} {
		if !r.relevant(filesystemEvent{Path: path}) {
			t.Errorf("ignored %s", path)
		}
	}
	for _, path := range []string{filepath.Join(r.common, "objects/ab/blob"), filepath.Join(r.gitDir, "index.lock"), filepath.Join(r.common, "refs/heads/linked.lock")} {
		if r.relevant(filesystemEvent{Path: path}) {
			t.Errorf("unnecessary read for %s", path)
		}
	}
	if !r.relevant(filesystemEvent{Rescan: true}) || !r.topologyChanged(filesystemEvent{Rescan: true}) {
		t.Fatal("lost events did not force rediscovery")
	}
}

func TestGitObservationMissingSiteFiltersUnrelatedChanges(t *testing.T) {
	parent := gitObservationPath(t.TempDir())
	path := filepath.Join(parent, "missing", "site")
	r := discoverGitObservationRoots(context.Background(), path)
	if r.site != path || !slices.Equal(r.watch, []string{parent}) {
		t.Fatalf("missing Site lost its recovery root: %+v", r)
	}
	if r.relevant(filesystemEvent{Path: filepath.Join(parent, "unrelated")}) || !r.relevant(filesystemEvent{Path: filepath.Join(parent, "missing")}) {
		t.Fatal("missing Site did not distinguish recreation from unrelated activity")
	}
}

func TestRemoteRefreshPolicyKeepsPendingChecksPrompt(t *testing.T) {
	quiet := remoteCache{}
	if remoteRefreshInterval(false) != 5*time.Minute || remoteRefreshInterval(true) != time.Minute {
		t.Fatal("quiet/active policies are wrong")
	}
	quiet.PullRequests = []PullRequestLink{{CIState: "pending"}}
	if remoteRefreshInterval(false) != 5*time.Minute || !hasPendingChecks(quiet) || pendingChecksInterval != 30*time.Second {
		t.Fatal("pending CI changed the expensive background refresh interval")
	}
}

func TestGitRefreshCannotPublishAfterLocalFailureOrInvalidation(t *testing.T) {
	if !currentGitRefresh(4, 4, "branch", "branch", "branch", true, false) {
		t.Fatal("current refresh rejected")
	}
	for _, valid := range []bool{
		currentGitRefresh(4, 4, "branch", "branch", "branch", false, false),
		currentGitRefresh(4, 4, "branch", "branch", "branch", true, true),
		currentGitRefresh(3, 4, "branch", "branch", "branch", true, false),
		currentGitRefresh(4, 4, "old", "branch", "old", true, false),
		currentGitRefresh(4, 4, "branch", "branch", "old", true, false),
	} {
		if valid {
			t.Fatal("obsolete Git/CI result could replace cached/error status")
		}
	}
}

func TestGitWatcherIdleAndAtomicSave(t *testing.T) {
	repo := t.TempDir()
	fixtureGit(t, repo, "init", "--initial-branch=main")
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "initial")
	// A local-only repo avoids test network access; start counts go outside the
	// observed tree so instrumentation itself cannot cause more scans.
	log := filepath.Join(t.TempDir(), "commands")
	t.Setenv("DEVJAY_TEST_COMMAND_LOG", log)
	installGitShim(t, `printf '%s\n' "$*" >> "$DEVJAY_TEST_COMMAND_LOG"`)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	statuses := make(chan GitStatus, 64)
	control := newGitWatchControl()
	go func() {
		defer close(done)
		watchGitWithControl(ctx, repo, control, func(s GitStatus) { statuses <- s })
	}()
	defer func() { cancel(); <-done }()
	waitStatus := func(match func(GitStatus) bool) {
		t.Helper()
		deadline := time.NewTimer(8 * time.Second)
		defer deadline.Stop()
		for {
			select {
			case status := <-statuses:
				if match(status) {
					return
				}
			case <-deadline.C:
				t.Fatal("status did not follow filesystem/control event")
			}
		}
	}
	waitStatus(func(s GitStatus) bool { return s.Checked != "" })
	// Let startup events and the initial no-network remote completion settle.
	time.Sleep(time.Second)
	before, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(6 * time.Second)
	after, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatalf("unchanged repository launched more Git commands:\n%s", strings.TrimPrefix(string(after), string(before)))
	}
	file := filepath.Join(repo, "new-file")
	if err := os.WriteFile(file+".new", []byte("edit"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file+".new", file); err != nil {
		t.Fatal(err)
	}
	waitStatus(func(s GitStatus) bool { return s.Changes == 1 })
	control.setActivity(false, true)
	time.Sleep(100 * time.Millisecond)
	before, _ = os.ReadFile(log)
	if err := os.WriteFile(filepath.Join(repo, "second-file"), []byte("edit"), 0600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(500 * time.Millisecond)
	after, _ = os.ReadFile(log)
	if string(before) != string(after) {
		t.Fatal("sleep allowed Git commands")
	}
	control.setActivity(true, false)
	waitStatus(func(s GitStatus) bool { return s.Changes == 2 })
}
