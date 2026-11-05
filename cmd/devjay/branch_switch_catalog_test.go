package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestBranchCatalogUsesExactRefsAndMappedRemoteNames(t *testing.T) {
	repo := switchFixture(t)
	fixtureGit(t, repo, "remote", "rename", "origin", "company/upstream")
	fixtureGit(t, repo, "push", "company/upstream", "HEAD:refs/heads/feature/remote")
	fixtureGit(t, repo, "fetch", "company/upstream")
	fixtureGit(t, repo, "symbolic-ref", "refs/remotes/company/upstream/HEAD", "refs/remotes/company/upstream/main")
	fixtureGit(t, repo, "tag", "feature/remote")
	catalog, err := readSwitchBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Branch != "source" || catalog.Head == "" || !strings.Contains(catalog.Message, "previously fetched") {
		t.Fatalf("bad catalog: %+v", catalog)
	}
	var remote, current int
	for _, choice := range catalog.Branches {
		if choice.Current {
			current++
			if choice.Name != "source" || !choice.Local {
				t.Fatalf("wrong current: %+v", choice)
			}
		}
		if choice.Remote != "" {
			remote++
			if choice.Name != "feature/remote" || choice.Remote != "company/upstream" || choice.Local {
				t.Fatalf("remote guessed from slash: %+v", choice)
			}
		}
	}
	if current != 1 || remote != 1 {
		t.Fatalf("duplicate/symbolic choices: %+v", catalog)
	}
	p := prepareSwitch(t, repo, "feature/remote", "company/upstream", false)
	if p.trackingRef != "refs/remotes/company/upstream/feature/remote" {
		t.Fatalf("incorrect remote: %+v", p)
	}
	if result := applyCheckoutAction(context.Background(), p); result.Status != "complete" {
		t.Fatalf("slash remote failed: %+v", result)
	}
}

func TestBranchDiscoveryFindsUnfetchedRemoteBranchesAndPreparesTracking(t *testing.T) {
	repo := switchFixture(t)
	remote := fixtureGit(t, repo, "remote", "get-url", "origin")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	for _, name := range []string{"lars/new-pr", "topic/without-pr"} {
		fixtureGit(t, remote, "branch", name, head)
	}
	cached, err := readSwitchBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	before := fixtureGit(t, repo, "for-each-ref")
	checkoutState := func() string {
		return fixtureGit(t, repo, "status", "--porcelain") + fixtureGit(t, repo, "diff", "HEAD", "--binary") +
			fixtureGit(t, repo, "hash-object", ".git/HEAD", ".git/index", ".git/config")
	}
	current := checkoutState()
	catalog := discoverSwitchBranches(context.Background(), repo, cached)
	for _, name := range []string{"lars/new-pr", "topic/without-pr"} {
		var found bool
		for _, choice := range catalog.Branches {
			if choice.Name == name && choice.Remote == "origin" && !choice.Local && choice.prRemote == "origin" && choice.prBranch == name {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing newly advertised branch %q: %+v", name, catalog)
		}
	}
	if fixtureGit(t, repo, "for-each-ref") != before {
		t.Fatal("discovery changed local refs")
	}
	if checkoutState() != current {
		t.Fatal("discovery changed the checkout, index, or configuration")
	}
	for _, choice := range cached.Branches {
		if choice.Name == "lars/new-pr" || choice.Name == "topic/without-pr" {
			t.Fatal("discovery mutated the initially published catalog")
		}
	}
	// The PR reader must receive the discovered upstream identity, so searching
	// by PR number can find a branch even before any remote ref has been fetched.
	enriched := branchPullRequests(catalog.Branches, func(string) string { return "owner/repo" }, func(string) ([]byte, error) {
		return []byte(`[{"number":2972,"headRefName":"lars/new-pr","headRepository":{"nameWithOwner":"owner/repo"},"headRepositoryOwner":{"login":"owner"},"reviewDecision":"REVIEW_REQUIRED"}]`), nil
	})
	foundPR := false
	for _, choice := range enriched {
		if choice.Name == "lars/new-pr" && len(choice.PullRequests) == 1 && choice.PullRequests[0].Number == 2972 {
			foundPR = true
		}
	}
	if !foundPR {
		t.Fatal("new remote branch did not receive its PR number")
	}
	plan := prepareSwitch(t, repo, "lars/new-pr", "origin", false)
	if !plan.CreateTarget || fixtureGit(t, repo, "branch", "--show-current") != "source" || fixtureGit(t, repo, "for-each-ref") != before {
		t.Fatal("preparation switched branches or changed refs")
	}
	if result := applyCheckoutAction(context.Background(), plan); result.Status != "complete" {
		t.Fatalf("could not check out newly discovered remote branch: %+v", result)
	}
	if fixtureGit(t, repo, "branch", "--show-current") != "lars/new-pr" || fixtureGit(t, repo, "rev-parse", "refs/heads/source") != head ||
		fixtureGit(t, repo, "config", "branch.lars/new-pr.remote") != "origin" || fixtureGit(t, repo, "config", "branch.lars/new-pr.merge") != "refs/heads/lars/new-pr" {
		t.Fatal("switch did not preserve the source and configure tracking")
	}
}

func TestBranchDiscoveryRefreshesDeletedBranchesAndPreservesOfflineChoices(t *testing.T) {
	repo := switchFixture(t)
	remote := fixtureGit(t, repo, "remote", "get-url", "origin")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, remote, "branch", "removed", head)
	fixtureGit(t, repo, "fetch", "origin")
	cached, err := readSwitchBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, remote, "branch", "-D", "removed")
	for _, choice := range discoverSwitchBranches(context.Background(), repo, cached).Branches {
		if choice.Name == "removed" {
			t.Fatal("deleted branch remained after a successful discovery")
		}
	}
	fixtureGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "missing.git"))
	offline := discoverSwitchBranches(context.Background(), repo, cached)
	if !strings.Contains(offline.Message, "Could not check origin") || len(offline.Branches) != len(cached.Branches) {
		t.Fatalf("offline discovery lost the cached choices or explanation: %+v", offline)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := discoverSwitchBranches(ctx, repo, cached); len(got.Branches) != len(cached.Branches) {
		t.Fatal("cancellation discarded the local catalog")
	}
}

func TestBranchDiscoveryTrackingCreationRejectsSymbolicAndConcurrentRefs(t *testing.T) {
	for _, race := range []bool{false, true} {
		t.Run(map[bool]string{false: "symbolic", true: "concurrent"}[race], func(t *testing.T) {
			repo := switchFixture(t)
			remote := fixtureGit(t, repo, "remote", "get-url", "origin")
			head := fixtureGit(t, repo, "rev-parse", "HEAD")
			fixtureGit(t, remote, "branch", "new-branch", head)
			plan := prepareSwitch(t, repo, "new-branch", "origin", false)
			if race {
				t.Setenv("DEVJAY_TEST_SWITCH_REPO", repo)
				t.Setenv("DEVJAY_TEST_SWITCH_HEAD", head)
				installGitShim(t, `case "$*" in *'DevJay: track confirmed remote branch'*) "$DEVJAY_TEST_GIT" -C "$DEVJAY_TEST_SWITCH_REPO" update-ref refs/remotes/origin/new-branch "$DEVJAY_TEST_SWITCH_HEAD" ;; esac`)
			} else {
				fixtureGit(t, repo, "symbolic-ref", "refs/remotes/origin/new-branch", "refs/heads/source")
			}
			result := applyCheckoutAction(context.Background(), plan)
			if result.Status != "failed" || fixtureGit(t, repo, "branch", "--show-current") != "source" || fixtureGit(t, repo, "rev-parse", "refs/heads/source") != head {
				t.Fatalf("unsafe tracking setup changed the checkout: %+v", result)
			}
		})
	}
}

func TestBranchDiscoveryRespectsFetchMappingsAndLocalNames(t *testing.T) {
	repo := switchFixture(t)
	remote := fixtureGit(t, repo, "remote", "get-url", "origin")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, remote, "branch", "feature/new", head)
	fixtureGit(t, remote, "branch", "excluded", head)
	fixtureGit(t, repo, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/custom/*")
	fixtureGit(t, repo, "config", "--add", "remote.origin.fetch", "^refs/heads/excluded")
	cached, err := readSwitchBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	result := discoverSwitchBranches(context.Background(), repo, cached)
	var mainCount, newCount int
	for _, choice := range result.Branches {
		if choice.Name == "excluded" {
			t.Fatal("excluded branch offered")
		}
		if choice.Name == "main" {
			mainCount++
			if !choice.Local {
				t.Fatal("remote choice duplicates an existing local branch")
			}
		}
		if choice.Name == "feature/new" {
			newCount++
		}
	}
	if mainCount != 1 || newCount != 1 {
		t.Fatalf("incorrect mapped choices: %+v", result)
	}
	fixtureGit(t, repo, "remote", "add", "other", remote)
	fixtureGit(t, repo, "config", "remote.other.fetch", "+refs/heads/*:refs/remotes/custom/*")
	for _, choice := range discoverSwitchBranches(context.Background(), repo, cached).Branches {
		if choice.Remote != "" && choice.Name != "excluded" {
			t.Fatalf("ambiguous remote branch offered: %+v", choice)
		}
	}
}

func TestBranchCatalogRejectsAmbiguousAndExcludedTrackingMappings(t *testing.T) {
	repo := switchFixture(t)
	fixtureGit(t, repo, "push", "origin", "HEAD:refs/heads/remote-branch")
	fixtureGit(t, repo, "remote", "add", "other", fixtureGit(t, repo, "remote", "get-url", "origin"))
	fixtureGit(t, repo, "config", "remote.other.fetch", "+refs/heads/*:refs/remotes/origin/*")
	if _, err := switchTrackingRef(context.Background(), repo, "origin", "remote-branch"); err == nil {
		t.Fatal("ambiguous ref ownership accepted")
	}
	catalog, err := readSwitchBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, choice := range catalog.Branches {
		if choice.Remote != "" {
			t.Fatalf("ambiguous remote choice: %+v", choice)
		}
	}
	fixtureGit(t, repo, "remote", "remove", "other")
	fixtureGit(t, repo, "config", "--add", "remote.origin.fetch", "^refs/heads/remote-branch")
	if _, err := switchTrackingRef(context.Background(), repo, "origin", "remote-branch"); err == nil {
		t.Fatal("excluded branch accepted")
	}
}

func TestSwitchRefspecMappingSupportsExactAndWildcardNames(t *testing.T) {
	for _, tt := range []struct {
		ref, pattern, replacement, want string
		match                           bool
	}{
		{"refs/remotes/team/x", "refs/remotes/team/*", "refs/heads/*", "refs/heads/x", true},
		{"refs/heads/release/a", "refs/heads/release/*", "refs/remotes/upstream/stable/*", "refs/remotes/upstream/stable/a", true},
		{"refs/heads/main", "refs/heads/main", "refs/remotes/custom/main", "refs/remotes/custom/main", true},
		{"refs/heads/other", "refs/heads/main", "refs/remotes/custom/main", "", false},
	} {
		got, match := mapSwitchRef(tt.ref, tt.pattern, tt.replacement)
		if match != tt.match || (match && got != tt.want) {
			t.Fatalf("map %q => %q,%v", tt.ref, got, match)
		}
	}
}

func TestBranchCatalogKeepsPRUpstreamIdentityAndUniqueRemoteFallback(t *testing.T) {
	repo := switchFixture(t)
	fixtureGit(t, repo, "branch", "renamed", "main")
	fixtureGit(t, repo, "branch", "--set-upstream-to=origin/main", "renamed")
	fixtureGit(t, repo, "branch", "--unset-upstream", "main")
	catalog, err := readSwitchBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"renamed", "main"} {
		found := false
		for _, choice := range catalog.Branches {
			if choice.Name == name {
				found = true
				if choice.prRemote != "origin" || choice.prBranch != "main" {
					t.Fatalf("lost PR identity: %+v", choice)
				}
			}
		}
		if !found {
			t.Fatalf("missing %s", name)
		}
	}
}
