package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovedReturnRequiresOpenApprovedPRInSameRepository(t *testing.T) {
	var good pullRequest
	good.Number, good.State, good.ReviewDecision = 42, "OPEN", "APPROVED"
	good.HeadRefName, good.HeadRefOid, good.BaseRefName, good.HeadRepository.NameWithOwner = "feature", "tip", "develop", "team/project"
	for _, change := range []func(*pullRequest){
		func(p *pullRequest) { p.IsDraft = true }, func(p *pullRequest) { p.State = "MERGED" }, func(p *pullRequest) { p.State = "CLOSED" },
		func(p *pullRequest) { p.ReviewDecision = "REVIEW_REQUIRED" }, func(p *pullRequest) { p.HeadRepository.NameWithOwner = "team/fork" },
		func(p *pullRequest) { p.HeadRefOid = "" }, func(p *pullRequest) { p.BaseRefName = "feature" },
	} {
		pr := good
		change(&pr)
		if pr.approvedReturn("team/project") != nil {
			t.Fatalf("accepted unavailable PR: %+v", pr)
		}
	}
}

func TestApprovedReturnRevalidatesBeforeLocalCleanup(t *testing.T) {
	ctx := context.Background()
	repo, seed := rebaseFixture(t, false)
	// Only the reported GitHub identity is substituted. All Git network reads
	// and ref updates still use the fixture's local bare remote.
	installGitShim(t, `case "$*" in *" remote get-url origin") echo https://github.com/team/project.git; exit 0;; esac`)
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	dir := t.TempDir()
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	t.Setenv("DEVJAY_PR_REPLY", filepath.Join(dir, "reply"))
	script := "#!/bin/sh\n" + `test "$1 $2 $3 $4 $5" = 'pr view 42 --repo github.com/team/project' || exit 1
cat "$DEVJAY_PR_REPLY"
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	pr := pullRequest{Number: 42, State: "OPEN", HeadRefOid: head, HeadRefName: "feature", BaseRefName: "develop", ReviewDecision: "APPROVED"}
	pr.HeadRepository.NameWithOwner, pr.HeadRepositoryOwner.Login = "team/project", "team"
	body, err := json.Marshal(pr)
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, dir, "reply", string(body))
	local, meta := inspectLocal(ctx, repo)
	status := describeGit(local, meta, remoteCache{Checked: time.Now(), PullRequests: openPullRequestLinks([]byte("["+string(body)+"]"), meta.GitHubRepo)})
	req, ok := makeCardGitOption(cardMenuRequest{Kind: "branch"}, status).request("s", status)
	if !ok || !req.DeleteBranch {
		t.Fatal("missing cleanup request")
	}
	plan, err := prepareCheckoutAction(ctx, repo, req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if fixtureGit(t, repo, "branch", "--show-current") != "feature" {
		t.Fatal("preparation switched branches")
	}
	for _, reply := range []string{"", strings.Replace(string(body), "APPROVED", "REVIEW_REQUIRED", 1), strings.Replace(string(body), head, "changed", 1),
		strings.Replace(string(body), "develop", "main", 1), strings.Replace(string(body), "team/project", "team/fork", 1)} {
		writeFixture(t, dir, "reply", reply)
		if result := applyCheckoutAction(ctx, plan); result.Status != "failed" || result.CurrentBranch != "feature" || result.CurrentHead != head || result.Recovery != "" {
			t.Fatalf("changed PR allowed cleanup: %+v", result)
		}
	}
	writeFixture(t, dir, "reply", string(body))
	fixtureGit(t, seed, "push", "origin", "--delete", "feature")
	if result := applyCheckoutAction(ctx, plan); result.Status != "failed" || result.CurrentBranch != "feature" || result.Recovery != "" {
		t.Fatalf("cached tip allowed cleanup of unpublished work: %+v", result)
	}
	fixtureGit(t, repo, "push", "origin", "feature")
	result := applyCheckoutAction(ctx, plan)
	if result.Status != "complete" || result.CurrentBranch != "develop" || result.CurrentHead != fixtureGit(t, seed, "rev-parse", "HEAD") {
		t.Fatalf("return failed: %+v", result)
	}
	if tip, err := refHead(ctx, repo, "refs/heads/feature"); err != nil || tip != "" {
		t.Fatalf("local branch still exists: %q %v", tip, err)
	}
	if !remoteBranchAt(ctx, repo, "origin", "refs/heads/feature", head) || fixtureGit(t, repo, "rev-parse", result.Recovery) != head {
		t.Fatal("remote branch or local recovery missing")
	}
}
