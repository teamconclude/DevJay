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

const mergedFixture = `[{"number":2812,"state":"MERGED","headRefName":"feature","headRefOid":"merged-head","headRepository":{"nameWithOwner":"teamconclude/project"},"mergedAt":"2026-09-21T19:43:32Z","url":"https://github.com/teamconclude/project/pull/2812"}]`

func TestLatestMergedPRIdentity(t *testing.T) {
	latest, err := latestMergedPR([]byte(mergedFixture), "TeamConclude/Project", "feature")
	if err != nil || latest == nil || latest.Number != 2812 {
		t.Fatalf("%+v %v", latest, err)
	}
	for _, data := range []string{
		strings.ReplaceAll(mergedFixture, `"MERGED"`, `"CLOSED"`),
		strings.ReplaceAll(mergedFixture, `"feature"`, `"another-branch"`),
		strings.ReplaceAll(mergedFixture, `"teamconclude/project"`, `"someoneelse/project"`),
		strings.ReplaceAll(mergedFixture, `"teamconclude/project"`, `"teamconclude/other-repo"`),
		strings.ReplaceAll(mergedFixture, `"merged-head"`, `""`),
		`[]`,
	} {
		got, err := latestMergedPR([]byte(data), "teamconclude/project", "feature")
		if err != nil || got != nil {
			t.Fatalf("accepted unrelated or incomplete PR: %+v %v", got, err)
		}
	}
	older := strings.ReplaceAll(strings.ReplaceAll(mergedFixture, "2812", "2811"), "19:43:32", "18:43:32")
	data := strings.TrimSuffix(mergedFixture, "]") + "," + strings.TrimPrefix(older, "[")
	latest, err = latestMergedPR([]byte(data), "teamconclude/project", "feature")
	if err != nil || latest.Number != 2812 {
		t.Fatalf("not newest merged PR: %+v %v", latest, err)
	}
	if _, err = latestMergedPR([]byte(`broken`), "teamconclude/project", "feature"); err == nil {
		t.Fatal("malformed response accepted")
	}
}

func TestMergedBranchReflectsCurrentCheckout(t *testing.T) {
	merged := &MergedPullRequest{Number: 2812, Head: "merged-head", MergedAt: time.Now()}
	cache := remoteCache{GitHub: "No open pull request", MergedPR: merged}
	meta := gitMeta{OID: "merged-head", Branch: "feature"}
	status := describeGit(GitStatus{Changes: 2}, meta, cache)
	if status.MergedPR == nil || !status.MergedPR.SameHead || status.Changes != 2 || status.GitHub != "PR #2812 · Merged" {
		t.Fatalf("%+v", status)
	}
	meta.OID = "new-work"
	status = describeGit(GitStatus{}, meta, cache)
	if status.MergedPR != nil || status.GitHub != "No open pull request" {
		t.Fatal("old PR attached to new work")
	}
	if merged.SameHead {
		t.Fatal("mutated cached metadata")
	}
	status = describeGit(GitStatus{IsMainBranch: true}, meta, cache)
	if status.MergedPR != nil || status.GitHub != "No open pull request" {
		t.Fatal("default branch marked leftover")
	}
}

func TestMergedLookupOnlyWithoutOpenPR(t *testing.T) {
	for _, scenario := range []string{"merged", "open", "unavailable", "none"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join(root, "calls")
			t.Setenv("DEVJAY_TEST_CALLS", log)
			t.Setenv("PATH", root+":"+os.Getenv("PATH"))
			open := `[]`
			if scenario == "open" {
				open = `[{"number":2813,"reviewDecision":"REVIEW_REQUIRED","headRepositoryOwner":{"login":"teamconclude"}}]`
			}
			merged := mergedFixture
			if scenario == "none" {
				merged = `[]`
			}
			failure := ""
			if scenario == "unavailable" {
				failure = "exit 1"
			}
			script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> \"$DEVJAY_TEST_CALLS\"\ncase \"$*\" in\n *'--state open'*) printf '%%s' '%s';;\n *'--state merged'*) %s\nprintf '%%s' '%s';;\n *) printf '[]';;\nesac\n", open, failure, merged)
			if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			cache := remoteCache{Reviews: []ReviewFindings{{Number: 2812}}, MergedPR: &MergedPullRequest{Number: 1}}
			refreshRemote(context.Background(), gitMeta{GitHubRepo: "teamconclude/project", Branch: "feature", OID: "merged-head"}, &cache)
			calls, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			if (scenario != "open") != strings.Contains(string(calls), "--state merged") {
				t.Fatalf("unexpected lookup: %s", calls)
			}
			switch scenario {
			case "merged":
				if cache.MergedPR == nil || cache.MergedPR.Number != 2812 || len(cache.Reviews) != 0 {
					t.Fatalf("%+v", cache)
				}
			case "open":
				if cache.GitHub != "PR #2813 · Review required" || cache.MergedPR != nil {
					t.Fatalf("%+v", cache)
				}
			case "none":
				if cache.GitHub != "No open pull request" || cache.MergedPR != nil {
					t.Fatalf("%+v", cache)
				}
			case "unavailable":
				if cache.GitHub != "GitHub unavailable" || cache.MergedPR != nil {
					t.Fatalf("%+v", cache)
				}
			}
		})
	}
}

func TestReturnSuggestionRequiresCleanExactMergedTip(t *testing.T) {
	merged := &MergedPullRequest{Number: 2798, Head: "old-tip", Base: "develop"}
	cache := remoteCache{GitHub: "No open pull request", MergedPR: merged}
	cases := []struct {
		name, head, base string
		edits, conflicts int
		stale            bool
		want             bool
	}{
		{name: "merged tip", head: "old-tip", base: "develop", want: true},
		{name: "reused branch", head: "new-tip", base: "develop"},
		{name: "local edits", head: "old-tip", base: "develop", edits: 1},
		{name: "conflicts", head: "old-tip", base: "develop", conflicts: 1},
		{name: "different base", head: "old-tip", base: "main"},
		{name: "unknown default", head: "old-tip"},
		{name: "unverified local", head: "old-tip", base: "develop", stale: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := describeGit(GitStatus{Changes: tc.edits, Conflicts: tc.conflicts, DefaultBranch: tc.base}, gitMeta{OID: tc.head, Branch: "hanord/backoffice", DefaultBranch: tc.base}, cache)
			if tc.stale {
				s.LocalNote = "cached"
			}
			if got := hasSuggestedActions(s); got != tc.want {
				t.Fatalf("suggestion=%v status=%+v", got, s)
			}
		})
	}
}
