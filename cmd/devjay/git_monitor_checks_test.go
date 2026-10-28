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

func TestPendingChecksRefreshUsesOneQueryAndPreservesRemoteAndReviews(t *testing.T) {
	root := t.TempDir()
	fixture, calls := filepath.Join(root, "reply"), filepath.Join(root, "calls")
	t.Setenv("DEVJAY_TEST_CHECK_REPLY", fixture)
	t.Setenv("DEVJAY_TEST_CHECK_CALLS", calls)
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DEVJAY_TEST_CHECK_CALLS\"\ncat \"$DEVJAY_TEST_CHECK_REPLY\"\n"
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	writeReply := func(head, conclusion string) {
		t.Helper()
		data := `[{"number":12,"headRefOid":"` + head + `","headRepositoryOwner":{"login":"team"},"reviewDecision":"APPROVED","statusCheckRollup":[` + checkRun("COMPLETED", conclusion) + `]}]`
		if err := os.WriteFile(fixture, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	checked := time.Unix(100, 0)
	cache := remoteCache{Key: "branch-identity", Checked: checked, Attempted: checked, BaseChecked: checked,
		CheckHeads: map[int]string{12: "head"}, PullRequests: []PullRequestLink{{Number: 12, URL: "https://github.com/team/project/pull/12", CIState: "pending", OtherAuthor: &PullRequestAuthor{Login: "someone-else"}}},
		Reviews: []ReviewFindings{{Number: 12, Blockers: 1, Detail: "existing review"}}}
	reviews := append([]ReviewFindings(nil), cache.Reviews...)
	m := gitMeta{GitHubRepo: "team/project", Branch: "feature"}
	writeReply("head", "SUCCESS")
	if refreshPullRequestChecks(context.Background(), m, &cache) {
		t.Fatal("unchanged head requested a full refresh")
	}
	if cache.Key != "branch-identity" || cache.Checked != checked || cache.Attempted != checked || cache.BaseChecked != checked || !reflect.DeepEqual(cache.Reviews, reviews) {
		t.Fatalf("CI query overwrote remote/review cache: %+v", cache)
	}
	if len(cache.PullRequests) != 1 || cache.PullRequests[0].CIState != "success" || cache.ChecksAttempted.IsZero() || cache.PullRequests[0].OtherAuthor == nil || cache.PullRequests[0].OtherAuthor.Login != "someone-else" {
		t.Fatalf("check result missing: %+v", cache)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 1 || !strings.Contains(string(data), "pr list --repo team/project --head feature") || strings.Contains(string(data), "api ") {
		t.Fatalf("pending checks made unrelated requests: %s", data)
	}
	writeReply("new-head", "FAILURE")
	if !refreshPullRequestChecks(context.Background(), m, &cache) || len(cache.Reviews) != 0 || cache.PullRequests[0].CIState != "failure" {
		t.Fatalf("changed pushed head retained stale review findings: %+v", cache)
	}
	// A new PR must not inherit another PR's author, even on the same branch.
	previous := cache.PullRequests
	cache.PullRequests = []PullRequestLink{{Number: 99, URL: "https://github.com/team/project/pull/99", OtherAuthor: &PullRequestAuthor{Login: "unrelated"}}}
	refreshPullRequestChecks(context.Background(), m, &cache)
	if cache.PullRequests[0].OtherAuthor != nil {
		t.Fatal("new PR inherited another author's avatar")
	}
	cache.PullRequests = previous
	// A transient failure cannot leave the previous known CI result current.
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	before := append([]PullRequestLink(nil), cache.PullRequests...)
	if refreshPullRequestChecks(context.Background(), m, &cache) || cache.PullRequests[0].CIState != "unknown" || before[0].CIState != "failure" {
		t.Fatal("failed query kept old state or mutated another cache's slice")
	}
	if !cache.ChecksRetry || nextPullRequestCheck(m, cache, gitPollingCadence(true, checked, checked)) != cache.ChecksAttempted.Add(time.Minute) {
		t.Fatal("unavailable checks lost their bounded lightweight retry")
	}
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	writeReply("new-head", "SUCCESS")
	refreshPullRequestChecks(context.Background(), m, &cache)
	if cache.ChecksRetry {
		t.Fatal("recovered completed checks retained retry polling")
	}
}

func TestCheckHeadsRejectMalformedDataAndOtherRepositoryOwners(t *testing.T) {
	if pullRequestHeads([]byte(`{"error":true}`), "team/project") != nil {
		t.Fatal("malformed list was accepted")
	}
	heads := pullRequestHeads([]byte(`[{"number":1,"headRefOid":"head","headRepositoryOwner":{"login":"other"}},{"number":2,"headRefOid":"safe","headRepositoryOwner":{"login":"TEAM"}}]`), "team/project")
	if len(heads) != 1 || heads[2] != "safe" {
		t.Fatalf("heads = %v", heads)
	}
}

func TestPRRefreshDeadlinesFollowAllCadencesRegardlessOfCIState(t *testing.T) {
	checked := time.Unix(1000, 0)
	meta := gitMeta{GitHubRepo: "team/project", Branch: "feature"}
	for _, tc := range []struct {
		name      string
		idle      time.Duration
		onDesktop bool
		interval  time.Duration
	}{
		{"fast current", 0, true, 10 * time.Second},
		{"fast other", 0, false, 15 * time.Second},
		{"normal current", 2 * time.Minute, true, time.Minute},
		{"normal other", 2 * time.Minute, false, 2 * time.Minute},
		{"background current", 10 * time.Minute, true, 2 * time.Minute},
		{"background other", 10 * time.Minute, false, 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cadence := gitPollingCadence(tc.onDesktop, checked.Add(-tc.idle), checked)
			for _, state := range []string{"", "success", "failure", "pending"} {
				cache := remoteCache{Attempted: checked, ChecksAttempted: checked}
				if state != "" {
					cache.PullRequests = []PullRequestLink{{CIState: state}}
				}
				for _, failed := range []bool{false, true} {
					cache.ChecksRetry = failed
					full, light := nextRemoteRefresh(cache, cadence), nextPullRequestCheck(meta, cache, cadence)
					want := tc.interval
					if failed {
						want = max(time.Minute, want)
					}
					if got := earlierDeadline(full, light); got != checked.Add(want) {
						t.Fatalf("CI=%s failed=%v: due=%v want=%v", state, failed, got, checked.Add(want))
					}
					if tc.idle >= 2*time.Minute && !light.IsZero() {
						t.Fatal("lightweight polling bypassed/duplicated the full normal/background refresh", light)
					}
				}
			}
		})
	}
	for _, meta := range []gitMeta{{Branch: "feature"}, {GitHubRepo: "team/project", Branch: "(detached)"}, {GitHubRepo: "team/project", Branch: "main", Initial: true}} {
		if got := nextPullRequestCheck(meta, remoteCache{}, gitPollingCadence(true, checked, checked)); !got.IsZero() {
			t.Fatal("ineligible branch scheduled an interactive GitHub query", meta, got)
		}
	}
}

func TestInteractionDiscoversPRCreationAndClosure(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	meta := gitMeta{GitHubRepo: "team/project", Branch: "feature"}
	cache := remoteCache{CheckHeads: map[int]string{}}
	writeReply := func(reply string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, "gh"), []byte("#!/bin/sh\ncat <<'REPLY'\n"+reply+"\nREPLY\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	writeReply(`[{"number":12,"headRefOid":"head","headRepositoryOwner":{"login":"team"},"reviewDecision":"REVIEW_REQUIRED"}]`)
	if !refreshPullRequestChecks(context.Background(), meta, &cache) || len(cache.PullRequests) != 1 {
		t.Fatal("new PR did not request full author/review enrichment", cache)
	}
	writeReply(`[{"number":12,"headRefOid":"head","headRepositoryOwner":{"login":"team"},"reviewDecision":"APPROVED"}]`)
	if refreshPullRequestChecks(context.Background(), meta, &cache) || cache.PullRequests[0].State != "Approved" {
		t.Fatal("approval was not updated by the lightweight query", cache)
	}
	writeReply(`[]`)
	if !refreshPullRequestChecks(context.Background(), meta, &cache) || len(cache.PullRequests) != 0 {
		t.Fatal("closed/merged PR did not request full merged-branch recognition", cache)
	}
}

func TestFullGitHubFailureRetainsSlowInteractiveRetry(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	for _, script := range []string{"#!/bin/sh\nexit 1\n", "#!/bin/sh\nprintf '{\"error\":true}'\n"} {
		if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		meta := gitMeta{GitHubRepo: "team/project", Branch: "feature"}
		var cache remoteCache
		refreshRemote(context.Background(), meta, &cache)
		if !cache.ChecksRetry || cache.GitHub != "GitHub unavailable" || cache.ChecksAttempted.IsZero() {
			t.Fatal("failed full check did not record a retry", cache)
		}
		if due := nextPullRequestCheck(meta, cache, gitPollingCadence(true, cache.ChecksAttempted, cache.ChecksAttempted)); due != cache.ChecksAttempted.Add(time.Minute) {
			t.Fatal("full failure allowed fast interactive retries", due)
		}
	}
}
