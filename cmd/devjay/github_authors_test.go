package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const authorFixture = `{"data":{"repository":{"pr0":{"number":12,"viewerDidAuthor":false,"author":{"login":"someone-else","avatarUrl":"https://avatars.githubusercontent.com/u/123?s=48"}},"pr1":{"number":13,"viewerDidAuthor":true,"author":{"login":"me","avatarUrl":"https://avatars.githubusercontent.com/u/456?s=48"}}}}}`

func authorLinks() []PullRequestLink {
	first, _ := pullRequestLink("team/project", 12, "Approved", false)
	second, _ := pullRequestLink("team/project", 13, "Open", false)
	return []PullRequestLink{first, second}
}

func TestOtherPullRequestAuthorsRequireConfirmedOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, data string
		want       bool
	}{
		{"other author", authorFixture, true},
		{"own PR", strings.ReplaceAll(authorFixture, `"viewerDidAuthor":false`, `"viewerDidAuthor":true`), false},
		{"unknown ownership", strings.ReplaceAll(authorFixture, `"viewerDidAuthor":false`, `"viewerDidAuthor":null`), false},
		{"missing ownership", strings.ReplaceAll(authorFixture, `"viewerDidAuthor":false,`, ``), false},
		{"wrong PR", strings.ReplaceAll(authorFixture, `"number":12`, `"number":99`), false},
		{"deleted author", `{"data":{"repository":{"pr0":{"number":12,"viewerDidAuthor":false,"author":null}}}}`, false},
		{"invalid login", strings.ReplaceAll(authorFixture, `someone-else`, `bad user`), false},
		{"unavailable repo", `{"data":{"repository":null}}`, false},
		{"partial errors", strings.Replace(authorFixture, `{"data":`, `{"errors":[{"message":"unavailable"}],"data":`, 1), false},
		{"bad JSON", `[]`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := otherPullRequestAuthors([]byte(tc.data), authorLinks())
			if (got[12] != nil) != tc.want || got[13] != nil {
				t.Fatalf("authors = %+v", got)
			}
			if tc.want && (got[12].Login != "someone-else" || got[12].AvatarURL != "https://avatars.githubusercontent.com/u/123?s=48") {
				t.Fatalf("author = %+v", got[12])
			}
		})
	}
	bot := strings.ReplaceAll(authorFixture, "someone-else", "dependabot[bot]")
	if got := otherPullRequestAuthors([]byte(bot), authorLinks()); got[12] == nil || got[12].Login != "dependabot[bot]" {
		t.Fatal("bot author lost")
	}
}

func TestUnsafeAvatarURLRetainsInitialsOnly(t *testing.T) {
	for _, raw := range []string{"", "http://avatars.githubusercontent.com/u/1", "https://example.com/u/1", "https://avatars.githubusercontent.com.example.com/u/1", "https://user@avatars.githubusercontent.com/u/1", "https://avatars.githubusercontent.com:443/u/1", "https://avatars.githubusercontent.com/u/1#fragment", "data:image/svg+xml,<svg/>", "file:///tmp/secret"} {
		data := strings.ReplaceAll(authorFixture, "https://avatars.githubusercontent.com/u/123?s=48", raw)
		got := otherPullRequestAuthors([]byte(data), authorLinks())[12]
		if got == nil || got.Login != "someone-else" || got.AvatarURL != "" {
			t.Fatalf("unsafe %q: %+v", raw, got)
		}
	}
}

func TestAuthorRefreshBatchesValidatedPRsAndPreservesPRState(t *testing.T) {
	root := t.TempDir()
	calls := filepath.Join(root, "calls")
	t.Setenv("DEVJAY_TEST_AUTHOR_CALLS", calls)
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\n' \"$*\" >> \"$DEVJAY_TEST_AUTHOR_CALLS\"\nprintf '%%s' '%s'\n", authorFixture)
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	links := authorLinks()
	refreshPullRequestAuthors(context.Background(), "team/project", links)
	if links[0].OtherAuthor == nil || links[1].OtherAuthor != nil || links[0].State != "Approved" {
		t.Fatalf("links = %+v", links)
	}
	data, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "\n") != 1 || !strings.Contains(string(data), "pr0:pullRequest(number:12)") || !strings.Contains(string(data), "pr1:pullRequest(number:13)") || !strings.Contains(string(data), "-f owner=team -f name=project") {
		t.Fatalf("calls = %s", data)
	}
	for _, invalid := range []struct {
		repo string
		prs  []PullRequestLink
	}{
		{"team/project", nil}, {"bad/repo/name", authorLinks()}, {"team/project", []PullRequestLink{{Number: 12, URL: "https://other/pull/12"}}}, {"team/project", make([]PullRequestLink, 21)},
	} {
		refreshPullRequestAuthors(context.Background(), invalid.repo, invalid.prs)
	}
	after, _ := os.ReadFile(calls)
	if string(data) != string(after) {
		t.Fatal("invalid/empty requests made network calls")
	}
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	links = authorLinks()
	refreshPullRequestAuthors(context.Background(), "team/project", links)
	if links[0].OtherAuthor != nil || links[0].State != "Approved" {
		t.Fatal("failed author lookup changed PR state")
	}
}

func TestMergedPRRetainsOtherAuthorOnlyForMatchingCheckout(t *testing.T) {
	author := &PullRequestAuthor{Login: "someone-else"}
	cache := remoteCache{MergedPR: &MergedPullRequest{Number: 12, Head: "merged-head", OtherAuthor: author}}
	meta := gitMeta{GitHubRepo: "team/project", Branch: "feature", OID: "merged-head"}
	status := describeGit(GitStatus{}, meta, cache)
	if len(status.PullRequests) != 1 || status.PullRequests[0].OtherAuthor != author {
		t.Fatalf("merged author missing: %+v", status)
	}
	meta.OID = "new-work"
	status = describeGit(GitStatus{}, meta, cache)
	if len(status.PullRequests) != 0 {
		t.Fatal("author retained for unrelated checkout")
	}
}
