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
		CheckHeads: map[int]string{12: "head"}, PullRequests: []PullRequestLink{{Number: 12, CIState: "pending"}},
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
	if len(cache.PullRequests) != 1 || cache.PullRequests[0].CIState != "success" || cache.ChecksAttempted.IsZero() {
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
	// A transient failure cannot leave the previous known CI result current.
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	before := append([]PullRequestLink(nil), cache.PullRequests...)
	if refreshPullRequestChecks(context.Background(), m, &cache) || cache.PullRequests[0].CIState != "unknown" || before[0].CIState != "failure" {
		t.Fatal("failed query kept old state or mutated another cache's slice")
	}
	if !checksNeedRefresh(cache) || checkRefreshInterval(cache) != time.Minute {
		t.Fatal("unavailable checks lost their bounded lightweight retry")
	}
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	writeReply("new-head", "SUCCESS")
	refreshPullRequestChecks(context.Background(), m, &cache)
	if cache.ChecksRetry || checksNeedRefresh(cache) {
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
