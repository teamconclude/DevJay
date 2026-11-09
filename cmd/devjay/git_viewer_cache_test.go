package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestViewerDiffCacheKeysAndLimits(t *testing.T) {
	v := GitView{Kind: "branch", root: "/repo", Commit: "commit", Parent: "parent", File: 0,
		Files: []ViewFile{{Path: "file.go", OldPath: "old.go"}}, Diff: "+original", DiffNote: "limit note"}
	var cache viewerDiffCache
	cache.remember(&v)
	key, ok := viewFileCacheKey(&v, 0)
	if !ok {
		t.Fatal("committed diff was not cacheable")
	}
	if got, ok := cache.get(key); !ok || got.diff != v.Diff || got.note != v.DiffNote {
		t.Fatalf("cached contents/notice lost: %+v, %v", got, ok)
	}
	for _, change := range []func(*GitView){
		func(v *GitView) { v.root = "/other" }, func(v *GitView) { v.Parent = "other-parent" },
		func(v *GitView) { v.Commit = "other-commit" }, func(v *GitView) { v.Files = []ViewFile{{Path: "other.go"}} },
		func(v *GitView) { v.Files = []ViewFile{{Path: "file.go", OldPath: "another.go"}} },
		func(v *GitView) { v.Kind = "edits" }, func(v *GitView) { v.Kind = "review" },
		func(v *GitView) { v.AllChanges, v.Head = true, "other-head" },
		func(v *GitView) { v.IgnoreWhitespace = true },
	} {
		next := v
		change(&next)
		if key, ok := viewFileCacheKey(&next, 0); ok && cache.has(key) {
			t.Fatalf("reused a diff for a different comparison: %+v", next)
		}
	}
	for i := 0; i < viewerDiffCacheEntries; i++ {
		next := v
		next.Commit = strings.Repeat("x", i+1)
		next.Diff = strings.Repeat("a", viewerDiffLimit)
		cache.remember(&next)
	}
	if cache.bytes > viewerDiffCacheBytes || len(cache.entries) > viewerDiffCacheEntries || cache.has(key) {
		t.Fatalf("cache did not evict old data within its budget: %d bytes, %d entries", cache.bytes, len(cache.entries))
	}
	n := len(cache.entries)
	v.Diff, v.Error = "", "read failed"
	cache.remember(&v)
	if len(cache.entries) != n {
		t.Fatal("cached an unsuccessful read")
	}
}

func TestViewerCachedFileSelectionCancelsObsoleteWork(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "package main\nfunc main() { println(\"new\") }\n")
	writeFixture(t, repo, "second.go", "package main\n")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Two files")
	v := GitView{Kind: "branch", Path: repo}
	if err := loadGitView(context.Background(), &v, true); err != nil {
		t.Fatal(err)
	}
	r := &gitViewerWindow{state: &v, generation: 3}
	selectViewFile(context.Background(), &v, 1)
	r.diffCache.remember(&v)
	want := v.Diff
	selectViewFile(context.Background(), &v, 0)
	v.Loading = true
	readCancelled, refreshCancelled, prefetchCancelled := false, false, false
	r.cancel = func() { readCancelled = true }
	r.refreshCancel = func() { refreshCancelled = true }
	r.prefetchCancel = func() { prefetchCancelled = true }
	if !r.useCachedFile(1) || r.state.File != 1 || r.state.Diff != want || r.state.Loading || r.state.Version != 4 {
		t.Fatal("cache hit did not immediately select the exact captured diff")
	}
	if !readCancelled || !refreshCancelled || !prefetchCancelled {
		t.Fatal("old asynchronous work could overwrite the cached selection")
	}
	r.state.Kind = "edits"
	if r.useCachedFile(1) {
		t.Fatal("used committed data for working-tree contents")
	}
}

func TestViewerForegroundReadDoesNotWaitForMonitoring(t *testing.T) {
	for i := 0; i < cap(commandSlots); i++ {
		commandSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < cap(commandSlots); i++ {
			<-commandSlots
		}
	}()
	root := t.TempDir()
	background, stopBackground := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopBackground()
	if _, err := viewerGit(background, root, "--version"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("background work unexpectedly bypassed its slots: %v", err)
	}
	foreground, stopForeground := context.WithTimeout(context.WithValue(context.Background(), viewerForegroundKey{}, true), 2*time.Second)
	defer stopForeground()
	if out, err := viewerGit(foreground, root, "--version"); err != nil || !strings.Contains(string(out), "git version") {
		t.Fatalf("interactive read was blocked by monitoring: %q, %v", out, err)
	}
}

func TestReviewPrefetchReadsWithoutChangingSelection(t *testing.T) {
	repo := branchReviewFixture(t)
	writeFixture(t, repo, "app.go", "package main\nfunc main() {}\n")
	writeFixture(t, repo, "second.go", "package second\n")
	writeFixture(t, repo, "third.go", "package third\n")
	v := GitView{Kind: "review", Path: repo}
	if err := loadBranchReview(context.Background(), &v, "main", false); err != nil {
		t.Fatal(err)
	}
	selected, index := v.Review.Selected, v.File
	var received []GitView
	readReviewPrefetch(context.Background(), v, []int{1, 1, -1, 2}, func(next GitView) { received = append(received, next) })
	if len(received) != 2 || received[0].File != 1 || received[1].File != 2 {
		t.Fatalf("prefetch did not read distinct valid requested files: %+v", received)
	}
	for _, next := range received {
		if next.Review.Selected == nil || next.Review.Selected.Path != v.Files[next.File].Path || next.Review.Selected.NewText == "" {
			t.Fatal("prefetch did not deliver the requested local contents")
		}
	}
	if v.File != index || v.Review.Selected != selected {
		t.Fatal("background prefetch changed the visible/native selection")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	fail := func(GitView) { t.Fatal("cancelled or oversized prefetch published a file") }
	readReviewPrefetch(ctx, v, []int{0}, fail)
	readReviewPrefetch(context.Background(), v, make([]int, reviewPreloadBatchSize+1), fail)
	message := reviewPrefetchPayload(received[0], 7, false)
	if message.PrefetchID != 7 || message.Done || message.Review.Selected != received[0].Review.Selected || message.Review.Refs != nil {
		t.Fatal("preload payload lost its batch or duplicated the ref list")
	}
	done := reviewPrefetchPayload(v, 7, true)
	if !done.Done || done.Review.Selected != nil || v.Review.Selected != selected {
		t.Fatal("completion changed the original selection or resent a file")
	}
	fixtureGit(t, repo, "switch", "main")
	var failures []GitView
	readReviewPrefetch(context.Background(), v, []int{1}, func(next GitView) { failures = append(failures, next) })
	if len(failures) != 1 || failures[0].Error == "" || failures[0].Review.Selected.Path != v.Files[1].Path {
		t.Fatal("failed background read did not identify its file for completion")
	}
}
