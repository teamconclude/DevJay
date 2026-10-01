package main

import "testing"

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
