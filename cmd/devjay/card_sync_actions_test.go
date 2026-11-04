package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestCardUpdateSourceOnlyAuthorizesIncomingMainBranch(t *testing.T) {
	strip, window := &application.WebviewWindow{}, &application.WebviewWindow{}
	status := GitStatus{Branch: "develop", Head: "head", Tracking: "origin/develop", Behind: 3, IsMainBranch: true}
	site := &siteRuntime{Ready: true, Window: window, Git: status}
	m := &siteManager{strip: strip, stripReady: true, runtime: map[string]*siteRuntime{"a": site}}
	req := ActionRequest{SiteID: "a", Kind: "update", Branch: "develop", Head: "head", Anchor: &cardAnchor{Width: 200, Height: 140}}
	for sender, expected := range map[string]*application.WebviewWindow{"strip": strip, "site-a": window} {
		if m.cardUpdateSource(req, sender) != expected {
			t.Fatalf("incoming main-branch click rejected from %s", sender)
		}
		for _, change := range []func(*ActionRequest){
			func(r *ActionRequest) { r.SiteID = "missing" }, func(r *ActionRequest) { r.Head = "stale" },
			func(r *ActionRequest) { r.Branch = "other" }, func(r *ActionRequest) { r.Kind = "push" },
			func(r *ActionRequest) { r.DeleteBranch = true }, func(r *ActionRequest) { r.UpdateTarget = true },
			func(r *ActionRequest) { r.TargetBranch = "other" }, func(r *ActionRequest) { r.TargetRemote = "other" },
			func(r *ActionRequest) { r.Anchor = &cardAnchor{} },
		} {
			invalid := req
			change(&invalid)
			if m.cardUpdateSource(invalid, sender) != nil {
				t.Fatalf("accepted stale or unrelated request: %+v", invalid)
			}
		}
		for _, change := range []func(*GitStatus){
			func(g *GitStatus) { g.IsMainBranch = false }, func(g *GitStatus) { g.Behind = 0 },
			func(g *GitStatus) { g.Ahead = 1 }, func(g *GitStatus) { g.Changes = 1 },
			func(g *GitStatus) { g.Conflicts = 1 }, func(g *GitStatus) { g.LocalNote = "unavailable" },
			func(g *GitStatus) { g.Tracking = "" }, func(g *GitStatus) { g.Rewritten = true },
			func(g *GitStatus) { g.Rebased = true }, func(g *GitStatus) { g.CanReplaceRemote = true },
			func(g *GitStatus) { g.Comparison = &HistoryComparison{Behind: 3} },
		} {
			site.Git = status
			change(&site.Git)
			if m.cardUpdateSource(req, sender) != nil {
				t.Fatalf("accepted unavailable update: %+v", site.Git)
			}
		}
		site.Git = status
		for _, phase := range []string{"preparing", "running"} {
			m.actionDialogs = map[string]*actionDialog{"action": {siteID: "a", action: &CheckoutAction{Status: phase}}}
			if m.cardUpdateSource(req, sender) != nil {
				t.Fatal("accepted a second action on a busy Site")
			}
			m.actionDialogs["action"].siteID = "other"
			if m.cardUpdateSource(req, sender) != expected {
				t.Fatal("another Site's action blocked this update")
			}
		}
		m.actionDialogs = nil
	}
	for _, sender := range []string{"settings", "site-b", "git-viewer-1", "suggested-actions-1", ""} {
		if m.cardUpdateSource(req, sender) != nil {
			t.Fatalf("accepted wrong sender %q", sender)
		}
	}
	site.Ready = false
	if m.cardUpdateSource(req, "site-a") != nil || m.cardUpdateSource(req, "strip") != nil {
		t.Fatal("accepted an unready Site")
	}
	site.Ready, m.stripReady = true, false
	if m.cardUpdateSource(req, "strip") != nil {
		t.Fatal("accepted an unready strip")
	}
	m.config = SiteConfig{ActiveProfile: "test", Profiles: []Profile{{ID: "test", Excluded: []string{"a"}}}}
	if m.cardUpdateSource(req, "site-a") != nil {
		t.Fatal("accepted a Site excluded by the current Profile")
	}
}

func TestCardReplacementRequiresVerifiedHistoryAndRevalidatesSelection(t *testing.T) {
	status := GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature", Ahead: 2, Behind: 3,
		Rewritten: true, CanReplaceRemote: true, Base: &BranchBase{Branch: "develop", Tracking: "origin/develop", Behind: 4}}
	for _, kind := range []string{"site", "branch", "edits", "local", "remote"} {
		option := makeCardGitOption(cardMenuRequest{Kind: kind}, status)
		if option == nil || option.Kind != "replace" || option.DisabledReason != "" || option.title() != "Replace from remote…" || option.tooltip() == "" {
			t.Fatalf("missing replacement: %+v", option)
		}
		req, ok := option.request("site", status)
		if !ok || req.Kind != "replace" || req.SiteID != "site" || req.Branch != status.Branch || req.Head != status.Head || req.DeleteBranch {
			t.Fatalf("wrong replacement preparation: %+v", req)
		}
		newer := status
		newer.Behind++
		newer.RemoteNote = "Last remote check failed"
		if _, ok := option.request("site", newer); !ok {
			t.Fatal("preparation should allow fetching a newer remote tip")
		}
		for _, change := range []func(*GitStatus){
			func(s *GitStatus) { s.Head = "new" }, func(s *GitStatus) { s.Head = "" },
			func(s *GitStatus) { s.Branch = "other" }, func(s *GitStatus) { s.Branch = "(detached)" },
			func(s *GitStatus) { s.Tracking = "other/feature" }, func(s *GitStatus) { s.Tracking = "" },
			func(s *GitStatus) { s.CanReplaceRemote = false }, func(s *GitStatus) { s.Changes = 1 },
			func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.LocalNote = "unavailable" },
		} {
			current := status
			change(&current)
			if _, ok := option.request("site", current); ok {
				t.Fatalf("accepted stale or unsafe replacement: %+v", current)
			}
		}
	}
	for _, change := range []func(*GitStatus){
		func(s *GitStatus) { s.Changes = 1 }, func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.LocalNote = "unknown" },
	} {
		current := status
		change(&current)
		option := makeCardGitOption(cardMenuRequest{Kind: "branch"}, current)
		if option == nil || option.DisabledReason == "" || option.tooltip() != option.DisabledReason {
			t.Fatalf("blocked replacement must explain why: %+v", option)
		}
	}
	for _, menu := range []cardMenuRequest{{Kind: "site", BundleID: "app"}, {Kind: "branch", Number: 12}, {Kind: "branch", GitAction: "update"}, {Kind: "branch", GitAction: "push"}} {
		if makeCardGitOption(menu, status) != nil {
			t.Fatalf("replacement changed a dedicated menu: %+v", menu)
		}
	}
	status.CanReplaceRemote = false
	if option := makeCardGitOption(cardMenuRequest{Kind: "branch"}, status); option != nil && option.Kind == "replace" {
		t.Fatal("rewrite warning alone must not authorize replacement")
	}
}

func TestCardIncomingActionRevalidatesFastForwardEligibility(t *testing.T) {
	status := GitStatus{Branch: "develop", Head: "head", Tracking: "origin/develop", Behind: 3, IsMainBranch: true}
	for _, menu := range []cardMenuRequest{{Kind: "branch", GitAction: "update"}, {Kind: "branch"}, {Kind: "site"}, {Kind: "edits"}} {
		option := makeCardGitOption(menu, status)
		if option == nil || option.DisabledReason != "" || option.title() != "Fetch and update develop…" {
			t.Fatalf("missing update: %+v", option)
		}
		request, ok := option.request("site", status)
		if !ok || request.Kind != "update" || request.SiteID != "site" || request.Head != "head" || request.Branch != "develop" || request.DeleteBranch {
			t.Fatalf("wrong preparation: %+v", request)
		}
		newer := status
		newer.Behind = 4
		newer.RemoteNote = "Last remote check failed"
		if _, ok := option.request("site", newer); !ok {
			t.Fatal("a changed remote count or failed check must allow a fresh fetch")
		}
		for _, change := range []func(*GitStatus){
			func(s *GitStatus) { s.Head = "new" }, func(s *GitStatus) { s.Branch = "feature" },
			func(s *GitStatus) { s.Tracking = "other/develop" }, func(s *GitStatus) { s.Tracking = "" },
			func(s *GitStatus) { s.Changes = 1 }, func(s *GitStatus) { s.Conflicts = 1 },
			func(s *GitStatus) { s.Ahead = 1 }, func(s *GitStatus) { s.Behind = 0 },
			func(s *GitStatus) { s.Rewritten = true }, func(s *GitStatus) { s.Rebased = true },
			func(s *GitStatus) { s.CanReplaceRemote = true }, func(s *GitStatus) { s.LocalNote = "unavailable" },
		} {
			current := status
			change(&current)
			if _, ok := option.request("site", current); ok {
				t.Fatalf("accepted changed or unsafe update: %+v", current)
			}
		}
	}
	for _, delta := range []GitStatus{{Changes: 1}, {Conflicts: 1}, {Ahead: 1}, {Rewritten: true}, {LocalNote: "unknown"}} {
		delta.Branch, delta.Head, delta.Tracking, delta.Behind = status.Branch, status.Head, status.Tracking, status.Behind
		option := makeCardGitOption(cardMenuRequest{Kind: "branch", GitAction: "update"}, delta)
		if option == nil || option.DisabledReason == "" || option.tooltip() != option.DisabledReason {
			t.Fatalf("blocked update must explain why: %+v", option)
		}
	}
}

func TestCardPushAllowsEditsAndRejectsChangedDestination(t *testing.T) {
	status := GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature", PushTarget: "origin/feature", Ahead: 2, Changes: 3}
	for _, menu := range []cardMenuRequest{{Kind: "branch", GitAction: "push"}, {Kind: "branch"}, {Kind: "site"}, {Kind: "edits"}} {
		option := makeCardGitOption(menu, status)
		if option == nil || option.DisabledReason != "" || option.title() != "Push 2 commits…" {
			t.Fatalf("dirty push not offered: %+v", option)
		}
		request, ok := option.request("site", status)
		if !ok || request.Kind != "push" || request.Head != status.Head || request.Branch != status.Branch {
			t.Fatalf("wrong push preparation: %+v", request)
		}
		for _, change := range []func(*GitStatus){
			func(s *GitStatus) { s.Head = "new" }, func(s *GitStatus) { s.Branch = "other" },
			func(s *GitStatus) { s.Tracking = "other/feature" }, func(s *GitStatus) { s.PushTarget = "other/feature" },
			func(s *GitStatus) { s.Ahead = 0 }, func(s *GitStatus) { s.Behind = 1 },
			func(s *GitStatus) { s.Rewritten = true }, func(s *GitStatus) { s.Rebased = true },
			func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.LocalNote = "unavailable" },
		} {
			current := status
			change(&current)
			if _, ok := option.request("site", current); ok {
				t.Fatalf("accepted unsafe push: %+v", current)
			}
		}
	}
}

func TestCardContextChoosesOneRelevantAction(t *testing.T) {
	status := GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature", PushTarget: "origin/feature", Ahead: 1,
		Base: &BranchBase{Branch: "develop", Tracking: "origin/develop", Behind: 2}}
	for _, kind := range []string{"branch", "site", "edits"} {
		option := makeCardGitOption(cardMenuRequest{Kind: kind}, status)
		if option == nil || option.Kind != "rebase-push" {
			t.Fatalf("generic menu should suggest updated base: %+v", option)
		}
	}
	option := makeCardGitOption(cardMenuRequest{Kind: "branch", GitAction: "push"}, status)
	if option == nil || option.Kind != "push" || option.title() != "Push 1 commit…" {
		t.Fatal("outgoing metric must retain its push action when the base is newer")
	}
	status.Changes = 1
	if option := makeCardGitOption(cardMenuRequest{Kind: "site"}, status); option == nil || option.Kind != "push" || option.DisabledReason != "" {
		t.Fatal("an available push should outrank a rebase blocked by local edits")
	}
	status.Changes = 0
	status.Ahead, status.Behind = 0, 2
	if option := makeCardGitOption(cardMenuRequest{Kind: "site"}, status); option == nil || option.Kind != "update" {
		t.Fatal("incoming work must take priority over rebasing")
	}
	status.Rebased = true
	if option := makeCardGitOption(cardMenuRequest{Kind: "site"}, status); option == nil || option.Kind != "publish" {
		t.Fatal("a rebased branch must never suggest pulling old history")
	}
	for _, menu := range []cardMenuRequest{{Kind: "site", BundleID: "app"}, {Kind: "branch", Number: 12}} {
		if makeCardGitOption(menu, status) != nil {
			t.Fatal("ordinary branch action added to an app or PR menu")
		}
	}
	for _, git := range []GitStatus{
		{Branch: "main", Head: "head", Tracking: "origin/main"},
		{Branch: "feature", Head: "head", Comparison: &HistoryComparison{Tracking: "origin/feature", Ahead: 2, Behind: 3}},
	} {
		if makeCardGitOption(cardMenuRequest{Kind: "site"}, git) != nil {
			t.Fatal("in-sync or untracked comparison must not invent a Git action")
		}
	}
}

func TestCardSyncHintCannotBroadenMenuAuthorization(t *testing.T) {
	valid := cardMenuRequest{SiteID: "site", Kind: "branch", GitAction: "update"}
	if _, ok := normalizeCardMenuRequest(valid); !ok {
		t.Fatal("incoming metric rejected")
	}
	for _, change := range []func(*cardMenuRequest){
		func(r *cardMenuRequest) { r.GitAction = "replace" }, func(r *cardMenuRequest) { r.Kind = "site" },
		func(r *cardMenuRequest) { r.Number = 12 }, func(r *cardMenuRequest) { r.BundleID = "app" },
		func(r *cardMenuRequest) { r.BaseBranch = "develop" }, func(r *cardMenuRequest) { r.PublishRebased = true },
	} {
		req := valid
		change(&req)
		if _, ok := normalizeCardMenuRequest(req); ok {
			t.Fatalf("ambiguous action hint accepted: %+v", req)
		}
	}
}
