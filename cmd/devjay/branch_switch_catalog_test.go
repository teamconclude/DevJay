package main

import (
	"context"
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
