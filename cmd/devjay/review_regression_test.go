package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReviewSymbolicFetchDestination(t *testing.T) {
	for _, preparation := range []bool{false, true} {
		name := "background"
		if preparation {
			name = "prepare-update"
		}
		t.Run(name, func(t *testing.T) {
			repo, _ := updateFixture(t)
			fixtureGit(t, repo, "switch", "-c", "victim")
			fixtureGit(t, repo, "commit", "--allow-empty", "-m", "unpublished victim commit")
			fixtureGit(t, repo, "switch", "develop")
			before := fixtureGit(t, repo, "rev-parse", "refs/heads/victim")
			fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/develop", "refs/heads/victim")
			if preparation {
				_, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "update"), nil)
				t.Logf("prepare error: %v", err)
			} else {
				_, meta := inspectLocal(context.Background(), repo)
				if meta.ReadError != nil {
					t.Fatal(meta.ReadError)
				}
				cache := remoteCache{}
				refreshRemote(context.Background(), meta, &cache)
				t.Logf("remote error: %s", cache.Error)
			}
			after := fixtureGit(t, repo, "rev-parse", "refs/heads/victim")
			if after != before {
				t.Fatalf("local victim branch changed without confirmation: %s -> %s", before, after)
			}
		})
	}
}

func TestReviewRemoteURLInvalidatesStatus(t *testing.T) {
	repo, seed := updateFixture(t)
	ctx := context.Background()
	s, old := inspectLocal(ctx, repo)
	if old.ReadError != nil {
		t.Fatal(old.ReadError)
	}
	cache := remoteCache{Key: old.key(), Checked: time.Now(), Attempted: time.Now(), Missing: true, Error: "Remote branch no longer exists"}
	var last GitStatus
	applyLocalReading(s, old, &cache, &last)
	fixtureGit(t, repo, "remote", "set-url", "origin", seed)
	s, next := inspectLocal(ctx, repo)
	if next.ReadError != nil {
		t.Fatal(next.ReadError)
	}
	if old.RemoteURL == next.RemoteURL {
		t.Fatal("fixture did not change URL")
	}
	result, valid := applyLocalReading(s, next, &cache, &last)
	if !valid {
		t.Fatal("invalid reading")
	}
	if !cache.Attempted.IsZero() || cache.Missing || result.RemoteNote != "Remote not checked yet" {
		t.Fatalf("status from old remote retained after URL change: summary=%q note=%q attempted=%v", result.Summary, result.RemoteNote, cache.Attempted)
	}
}

func TestReviewChecksUseTrackedRemoteBranch(t *testing.T) {
	repo := trackedFixture(t)
	fixtureGit(t, repo, "branch", "-m", "local-main")
	_, meta := inspectLocal(context.Background(), repo)
	if meta.ReadError != nil {
		t.Fatal(meta.ReadError)
	}
	if meta.Merge != "refs/heads/main" {
		t.Fatalf("fixture upstream: %+v", meta)
	}
	meta.GitHubRepo = "example/repo"
	shim := filepath.Join(t.TempDir(), "gh")
	script := `#!/bin/sh
while test "$#" -gt 0; do
 if test "$1" = --head; then shift; head="$1"; fi
 shift
done
if test "$head" = main; then
 echo '[{"number":42,"headRepositoryOwner":{"login":"example"},"headRefOid":"abc123","statusCheckRollup":[]}]'
else
 echo '[]'
fi
`
	if err := os.WriteFile(shim, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(shim)+":"+os.Getenv("PATH"))
	control := meta
	control.Branch = "main"
	controlCache := remoteCache{}
	refreshPullRequestChecks(context.Background(), control, &controlCache)
	if len(controlCache.PullRequests) != 1 {
		t.Fatal("control query failed")
	}
	cache := remoteCache{CheckHeads: map[int]string{42: "abc123"}}
	refreshPullRequestChecks(context.Background(), meta, &cache)
	if len(cache.PullRequests) != 1 {
		t.Fatalf("PR for tracked remote main absent: local=%s remote=%s summary=%q", meta.Branch, meta.Merge, cache.GitHub)
	}
}

func TestReviewTrackingRefChangedDuringFetch(t *testing.T) {
	for _, timing := range []string{"fetch", "publish"} {
		t.Run(timing, func(t *testing.T) {
			repo, _ := updateFixture(t)
			before := fixtureGit(t, repo, "rev-parse", "refs/remotes/origin/develop")
			fixtureGit(t, repo, "branch", "victim", before)
			head := fixtureGit(t, repo, "rev-parse", "HEAD")
			t.Setenv("DEVJAY_TEST_REF_TIMING", timing)
			installGitShim(t, `case " $* " in
  *" fetch "*)
    if test "$DEVJAY_TEST_REF_TIMING" = fetch; then
      "$DEVJAY_TEST_GIT" -C "$2" symbolic-ref refs/remotes/origin/develop refs/heads/victim || exit 1
    fi ;;
  *" update-ref "*" refs/remotes/origin/develop "*)
    if test "$DEVJAY_TEST_REF_TIMING" = publish; then
      "$DEVJAY_TEST_GIT" -C "$2" symbolic-ref refs/remotes/origin/develop refs/heads/victim || exit 1
    fi ;;
esac`)
			_, err := fetchTrackingRef(context.Background(), repo, "origin", "refs/heads/develop", "refs/remotes/origin/develop", false)
			if timing == "fetch" && err == nil {
				t.Fatal("accepted a symbolic destination installed during fetch")
			}
			if got := fixtureGit(t, repo, "rev-parse", "refs/heads/victim"); got != before {
				t.Fatalf("fetch moved local branch: %s -> %s", before, got)
			}
			if fixtureGit(t, repo, "rev-parse", "HEAD") != head {
				t.Fatal("fetch changed HEAD")
			}
			if fixtureGit(t, repo, "for-each-ref", "--format=%(refname)", "refs/devjay/prepared/") != "" {
				t.Fatal("private fetch ref leaked")
			}
		})
	}
}

func TestReviewSymbolicDefaultAndActionFetches(t *testing.T) {
	for _, kind := range []string{"default", "push", "replace", "rebase", "merged"} {
		t.Run(kind, func(t *testing.T) {
			repo, _ := updateFixture(t)
			if kind == "default" || kind == "rebase" || kind == "merged" {
				fixtureGit(t, repo, "switch", "-c", "topic")
			}
			fixtureGit(t, repo, "branch", "victim")
			before := fixtureGit(t, repo, "rev-parse", "HEAD")
			_, meta := inspectLocal(context.Background(), repo)
			if kind == "rebase" || kind == "merged" {
				// Let preparation resolve the default branch first, then replace
				// its tracking ref while the network command is starting.
				installGitShim(t, `case " $* " in
  *" fetch "*) "$DEVJAY_TEST_GIT" -C "$2" symbolic-ref refs/remotes/origin/develop refs/heads/victim || exit 1 ;;
esac`)
			} else {
				fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/develop", "refs/heads/victim")
			}
			if kind == "default" {
				cache := remoteCache{}
				refreshDefaultBranch(context.Background(), meta, &cache)
				if cache.BaseError == "" {
					t.Fatal("symbolic default accepted")
				}
			} else {
				merged := &MergedPullRequest{Head: before, Base: "develop", MergedAt: time.Now().Add(time.Minute)}
				if _, err := prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, kind), merged); err == nil {
					t.Fatal("symbolic destination accepted")
				}
			}
			if fixtureGit(t, repo, "rev-parse", "refs/heads/victim") != before || fixtureGit(t, repo, "rev-parse", "HEAD") != before {
				t.Fatal("observation/preparation changed local branches")
			}
		})
	}
}

func TestReviewRemoteURLRejectsOutstandingResult(t *testing.T) {
	repo, seed := updateFixture(t)
	_, old := inspectLocal(context.Background(), repo)
	fixtureGit(t, repo, "remote", "set-url", "origin", seed)
	s, next := inspectLocal(context.Background(), repo)
	cache := remoteCache{Key: old.key(), Checked: time.Now(), Attempted: time.Now(), Rewritten: true}
	var last GitStatus
	applyLocalReading(s, next, &cache, &last)
	if currentGitRefresh(1, 1, old.key(), cache.Key, next.key(), true, false) {
		t.Fatal("accepted outstanding result from previous remote URL")
	}
	if !cache.Checked.IsZero() || !cache.Attempted.IsZero() || cache.Rewritten {
		t.Fatal("old freshness or rewrite claims survived URL change")
	}
}

func TestReviewPostPushCannotWriteThroughSymbolicTrackingRef(t *testing.T) {
	repo, _ := updateFixture(t)
	old := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "branch", "victim")
	fixtureGit(t, repo, "commit", "--allow-empty", "-m", "unpublished")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/develop", "refs/heads/victim")
	if _, err := publishTrackingRef(context.Background(), repo, "refs/remotes/origin/develop", head, old, "DevJay: confirmed push"); err == nil {
		t.Fatal("accepted symbolic post-push destination")
	}
	if fixtureGit(t, repo, "rev-parse", "refs/heads/victim") != old {
		t.Fatal("post-push update moved local branch")
	}
}

func TestReviewFullRefreshUsesTrackedPRBranch(t *testing.T) {
	for _, state := range []string{"open", "merged"} {
		t.Run(state, func(t *testing.T) {
			t.Setenv("DEVJAY_TEST_PR_STATE", state)
			shim := filepath.Join(t.TempDir(), "gh")
			script := `#!/bin/sh
if test "$1" != pr; then echo '[]'; exit 0; fi
while test "$#" -gt 0; do
 case "$1" in
 --head) shift; head="$1" ;;
 --state) shift; state="$1" ;;
 esac
 shift
done
if test "$head" != feature; then
 echo '[{"number":99,"headRepositoryOwner":{"login":"example"}}]'
elif test "$state" != "$DEVJAY_TEST_PR_STATE"; then
 echo '[]'
elif test "$state" = open; then
 echo '[{"number":42,"headRefName":"feature","headRefOid":"tip","headRepository":{"nameWithOwner":"example/repo"},"headRepositoryOwner":{"login":"example"},"statusCheckRollup":[]}]'
else
 echo '[{"number":42,"state":"MERGED","headRefName":"feature","headRefOid":"tip","baseRefName":"main","headRepository":{"nameWithOwner":"example/repo"},"mergedAt":"2026-09-28T12:00:00Z"}]'
fi
`
			if err := os.WriteFile(shim, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", filepath.Dir(shim)+":"+os.Getenv("PATH"))
			meta := gitMeta{Branch: "local-feature", Remote: "origin", Merge: "refs/heads/feature", GitHubRepo: "example/repo", OID: "tip"}
			cache := remoteCache{}
			refreshRemote(context.Background(), meta, &cache)
			if state == "open" {
				if len(cache.PullRequests) != 1 || cache.PullRequests[0].Number != 42 {
					t.Fatalf("wrong PR: %+v", cache)
				}
				choices := branchPullRequests([]BranchChoice{{Name: meta.Branch, prRemote: meta.Remote, prBranch: trackedPullRequestBranch(meta.Remote, meta.Merge)}}, func(string) string { return meta.GitHubRepo }, func(string) ([]byte, error) {
					return []byte(`[{"number":42,"headRefName":"feature","headRepository":{"nameWithOwner":"example/repo"},"headRepositoryOwner":{"login":"example"}}]`), nil
				})
				if len(choices[0].PullRequests) != 1 || choices[0].PullRequests[0].Number != cache.PullRequests[0].Number {
					t.Fatal("chooser and card disagree")
				}
			} else if cache.MergedPR == nil || cache.MergedPR.Number != 42 || !mergedPRMatchesBranch(cache.MergedPR, meta) {
				t.Fatalf("missing exact merged PR: %+v", cache)
			}
		})
	}
}

func TestReviewRebaseOwnUpstreamFetchPreservesLocalBranch(t *testing.T) {
	for _, validation := range []bool{false, true} {
		t.Run(map[bool]string{false: "prepare", true: "validate"}[validation], func(t *testing.T) {
			repo, _ := rebaseFixture(t, false)
			var plan *CheckoutPlan
			if validation {
				plan = prepareForTest(t, repo, "rebase")
			}
			before := fixtureGit(t, repo, "rev-parse", "HEAD")
			fixtureGit(t, repo, "branch", "victim")
			fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/feature", "refs/heads/victim")
			var err error
			if validation {
				err = validateCheckoutPlan(context.Background(), plan, true)
			} else {
				_, err = prepareCheckoutAction(context.Background(), repo, actionRequest(t, repo, "rebase"), nil)
			}
			if err == nil {
				t.Fatal("accepted symbolic own upstream")
			}
			if fixtureGit(t, repo, "rev-parse", "refs/heads/victim") != before || fixtureGit(t, repo, "rev-parse", "HEAD") != before {
				t.Fatal("own-upstream fetch moved a local branch")
			}
		})
	}
}
