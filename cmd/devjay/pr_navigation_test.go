package main

import (
	"testing"
	"time"
)

func TestPullRequestLinksFilterOwnerAndConstructURLs(t *testing.T) {
	data := []byte(`[
		{"number":12,"url":"https://untrusted.invalid/","headRepositoryOwner":{"login":"Team"},"reviewDecision":"APPROVED"},
		{"number":13,"headRepositoryOwner":{"login":"someoneelse"}},
		{"number":14,"headRepositoryOwner":{"login":"team"},"isDraft":true},
		{"number":12,"headRepositoryOwner":{"login":"team"}},
		{"number":0,"headRepositoryOwner":{"login":"team"}}
	]`)
	links := openPullRequestLinks(data, "team/project")
	if len(links) != 2 || links[0].URL != "https://github.com/team/project/pull/12" || links[0].State != "Approved" ||
		links[1].Number != 14 || !links[1].IsDraft || links[1].State != "Draft" {
		t.Fatalf("unexpected PR links: %+v", links)
	}
	for _, repo := range []string{"team/../evil", "team/..", "team/project?x", "team/project#x", "team/project\n", "https://github.com/team/project"} {
		if _, ok := pullRequestLink(repo, 1, "Open", false); ok {
			t.Errorf("accepted invalid repository %q", repo)
		}
	}
	if got := openPullRequestLinks([]byte(`bad json`), "team/project"); len(got) != 0 {
		t.Fatal("accepted malformed GitHub response")
	}
}

func TestMergedPRLinkFollowsCurrentBranchIdentity(t *testing.T) {
	meta := gitMeta{GitHubRepo: "team/project", OID: "head", Branch: "feature"}
	cache := remoteCache{MergedPR: &MergedPullRequest{Number: 8, Head: "head", MergedAt: time.Now(), URL: "https://untrusted.invalid/"}}
	status := describeGit(GitStatus{}, meta, cache)
	pr, ok := requestedPR(status, 8)
	if !ok || pr.State != "Merged" || pr.URL != "https://github.com/team/project/pull/8" {
		t.Fatalf("missing or unsafe merged PR link: %+v", status.PullRequests)
	}
	meta.OID = "new-head"
	if _, ok := requestedPR(describeGit(GitStatus{}, meta, cache), 8); ok {
		t.Fatal("old merged PR link attached to new work")
	}
	meta.OID = "head"
	meta.BranchCreated = cache.MergedPR.MergedAt.Add(time.Second)
	if _, ok := requestedPR(describeGit(GitStatus{}, meta, cache), 8); ok {
		t.Fatal("old merged PR link attached to recreated branch at the same tip")
	}
	meta.BranchCreated = time.Time{}
	if _, ok := requestedPR(describeGit(GitStatus{IsMainBranch: true}, meta, cache), 8); ok {
		t.Fatal("merged PR link attached to default branch")
	}
}

func TestPROpenRequiresCurrentPRAndMatchingSender(t *testing.T) {
	link, _ := pullRequestLink("team/project", 20, "Open", false)
	status := GitStatus{PullRequests: []PullRequestLink{link}}
	if _, ok := requestedPR(status, 20); !ok {
		t.Fatal("current PR missing")
	}
	for _, number := range []int{-1, 0, 19, 21} {
		if _, ok := requestedPR(status, number); ok {
			t.Errorf("accepted unknown PR %d", number)
		}
	}
	for _, sender := range []string{"settings", "site-other", "git-viewer-1", "", "control-panel-other"} {
		if validPRSender(sender, "test") {
			t.Errorf("accepted sender %q", sender)
		}
	}
	if !validPRSender("control-panel", "test") || !validPRSender("site-test", "test") || validPRSender("control-panel", "") {
		t.Fatal("sender matching is incorrect")
	}
}
