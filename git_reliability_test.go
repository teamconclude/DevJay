package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func trackedFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	fixtureGit(t, root, "init", "--bare", "--initial-branch=main", origin)
	fixtureGit(t, root, "clone", origin, seed)
	writeFixture(t, seed, "file", "initial")
	fixtureGit(t, seed, "add", ".")
	fixtureGit(t, seed, "commit", "-m", "initial")
	fixtureGit(t, seed, "push", "-u", "origin", "main")
	return seed
}

func TestFailedLocalReadRetainsStatusAndCache(t *testing.T) {
	repo := trackedFixture(t)
	ctx := context.Background()
	s, m := inspectLocal(ctx, repo)
	if m.ReadError != nil || m.Ref == "" {
		t.Fatalf("baseline: %+v %+v", s, m)
	}
	originalCache := remoteCache{Key: m.key(), Checked: time.Now(), Attempted: time.Now(), GitHub: "PR #4 · Approved", Rewritten: true, Reviews: []ReviewFindings{{Number: 4, Blockers: 1, Warnings: 7}}}
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"rev-parse", "status", "for-each-ref", "symbolic-ref", "remote"} {
		t.Run(command, func(t *testing.T) {
			cache := originalCache
			var last GitStatus
			baseline, ok := applyLocalReading(s, m, &cache, &last)
			if !ok {
				t.Fatal("valid baseline rejected")
			}
			shim := filepath.Join(t.TempDir(), "git")
			script := "#!/bin/sh\nif test \"$3\" = " + command + "; then exit 70; fi\nexec '" + strings.ReplaceAll(realGit, "'", "'\\''") + "' \"$@\"\n"
			if err := os.WriteFile(shim, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", filepath.Dir(shim)+":"+os.Getenv("PATH"))
			failed, meta := inspectLocal(ctx, repo)
			if meta.ReadError == nil {
				t.Fatal("failure silently accepted")
			}
			got, ok := applyLocalReading(failed, meta, &cache, &last)
			if ok || !strings.Contains(got.LocalNote, "cached") {
				t.Fatalf("got %+v", got)
			}
			got.LocalNote = ""
			if !reflect.DeepEqual(got, baseline) || !reflect.DeepEqual(cache, originalCache) || !reflect.DeepEqual(last, baseline) {
				t.Fatal("failure discarded status, PR findings, cache identity, or retry time")
			}
			recovered, ok := applyLocalReading(s, m, &cache, &last)
			if !ok || recovered.LocalNote != "" || recovered.GitHub == "" {
				t.Fatal("recovery failed")
			}
		})
	}
	cache := originalCache
	var last GitStatus
	applyLocalReading(s, m, &cache, &last)
	fixtureGit(t, repo, "checkout", "-b", "another-branch")
	s, m = inspectLocal(ctx, repo)
	got, ok := applyLocalReading(s, m, &cache, &last)
	if !ok || got.Branch != "another-branch" || got.GitHub != "" || !cache.Attempted.IsZero() {
		t.Fatalf("real branch change retained previous cache: %+v", got)
	}
}

func TestLocalPollingHasReservedCapacityAndReportsTimeouts(t *testing.T) {
	repo := trackedFixture(t)
	for i := 0; i < cap(remoteCommandSlots); i++ {
		remoteCommandSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(remoteCommandSlots); i++ {
			<-remoteCommandSlots
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	s, m := inspectLocal(ctx, repo)
	if m.ReadError != nil || s.Branch != "main" {
		t.Fatalf("remote commands starved local poll: %+v %+v", s, m)
	}
	for i := 0; i < cap(commandSlots); i++ {
		commandSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(commandSlots); i++ {
			<-commandSlots
		}
	}()
	timeout, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	s, m = inspectLocal(timeout, repo)
	if !errors.Is(m.ReadError, context.DeadlineExceeded) || s.LocalNote != "Status check timed out" || s.Summary == "Not a Git working tree" {
		t.Fatalf("misclassified timeout: %+v %+v", s, m)
	}
	var cache remoteCache
	var last GitStatus
	got, ok := applyLocalReading(s, m, &cache, &last)
	if ok || got.LocalNote == "" || got.Checked != "" {
		t.Fatalf("first failed read appeared fresh: %+v", got)
	}
}

func TestNonRepositoryAndBrokenRepositoryAreDistinct(t *testing.T) {
	dir := t.TempDir()
	s, m := inspectLocal(context.Background(), dir)
	if m.ReadError != nil || s.Summary != "Not a Git working tree" {
		t.Fatalf("plain folder: %+v %+v", s, m)
	}
	writeFixture(t, dir, ".git", "broken gitfile")
	s, m = inspectLocal(context.Background(), dir)
	if m.ReadError == nil || s.Summary == "Not a Git working tree" {
		t.Fatalf("broken repo misclassified: %+v %+v", s, m)
	}
}

func BenchmarkInspectLocal(b *testing.B) {
	// Opt-in read-only measurement; no repository is modified or fetched.
	path := os.Getenv("DEVJAY_BENCH_REPO")
	if path == "" {
		b.Skip("set DEVJAY_BENCH_REPO to a local checkout")
	}
	for i := 0; i < b.N; i++ {
		_, m := inspectLocal(context.Background(), path)
		if m.ReadError != nil {
			b.Fatal(m.ReadError)
		}
	}
}

func TestAheadBehindIgnoresUserDisplayPreference(t *testing.T) {
	repo := trackedFixture(t)
	fixtureGit(t, repo, "config", "status.aheadBehind", "false")
	writeFixture(t, repo, "local", "work")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "local work")
	s, m := inspectLocal(context.Background(), repo)
	if m.ReadError != nil || !m.Comparable || s.Ahead != 1 || s.Behind != 0 {
		t.Fatalf("missing ahead/behind: %+v %+v", s, m)
	}
}
