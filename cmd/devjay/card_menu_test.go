package main

import (
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestNormalizeCardMenuRequest(t *testing.T) {
	valid := cardMenuRequest{SiteID: "alpha", X: 23.5, Y: 60}
	got, ok := normalizeCardMenuRequest(valid)
	if !ok || got.Kind != "site" {
		t.Fatalf("card background should select desktop navigation: %#v, %v", got, ok)
	}
	for _, kind := range []string{"auto", "site", "edits", "branch", "local", "remote", "return-status"} {
		req := valid
		req.Kind = kind
		if got, ok := normalizeCardMenuRequest(req); !ok || got.Kind != kind {
			t.Errorf("rejected category %q", kind)
		}
	}
	for _, change := range []func(*cardMenuRequest){
		func(r *cardMenuRequest) { r.SiteID = "" },
		func(r *cardMenuRequest) { r.Kind = "settings" },
		func(r *cardMenuRequest) { r.BaseBranch = "develop" },
		func(r *cardMenuRequest) { r.PublishButton = true },
		func(r *cardMenuRequest) { r.Kind = "return-status"; r.Number = 42 },
		func(r *cardMenuRequest) { r.Kind = "return-status"; r.BundleID = "app" },
		func(r *cardMenuRequest) { r.Kind = "branch"; r.BaseBranch = "develop\nother" },
		func(r *cardMenuRequest) { r.Number = -1 },
		func(r *cardMenuRequest) { r.BundleID = "app\x00other" },
		func(r *cardMenuRequest) { r.BundleID = "app"; r.Number = 123 },
		func(r *cardMenuRequest) { r.X = -1 },
		func(r *cardMenuRequest) { r.Y = 32769 },
		func(r *cardMenuRequest) { r.X = math.Inf(1) },
		func(r *cardMenuRequest) { r.Y = math.NaN() },
	} {
		req := valid
		change(&req)
		if _, ok := normalizeCardMenuRequest(req); ok {
			t.Errorf("accepted invalid request: %#v", req)
		}
	}
}

func TestCardRebaseRequiresTheClickedBaseAndUnchangedCheckout(t *testing.T) {
	status := func() GitStatus {
		return GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature", PushTarget: "origin/feature", Base: &BranchBase{Branch: "develop", Tracking: "origin/develop", Head: "base", Behind: 3}}
	}
	menu := cardMenuRequest{SiteID: "site", Kind: "branch", BaseBranch: "develop"}
	original := makeCardGitOption(menu, status())
	if original == nil || original.DisabledReason != "" {
		t.Fatalf("missing enabled option: %#v", original)
	}
	req, ok := original.request("site", status())
	if !ok || req.Kind != "rebase-push" || req.SiteID != "site" || req.Branch != "feature" || req.Head != "head" || req.TargetBranch != "develop" || req.TargetRemote != "origin" || req.DeleteBranch {
		t.Fatalf("wrong preparation request: %+v", req)
	}
	newer := status()
	newer.Base.Head = "newest"
	newer.Base.Behind++
	if _, ok := original.request("site", newer); !ok {
		t.Fatal("a newer remote tip must still be fetched")
	}
	for _, change := range []func(*GitStatus){
		func(s *GitStatus) { s.Changes = 1 }, func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.LocalNote = "unavailable" },
		func(s *GitStatus) { s.Behind = 1 }, func(s *GitStatus) { s.Rebased = true }, func(s *GitStatus) { s.CanReplaceRemote = true },
		func(s *GitStatus) { s.Head = "new-head" }, func(s *GitStatus) { s.Branch = "other" },
		func(s *GitStatus) { s.Base.Branch = "main" }, func(s *GitStatus) { s.Base.Tracking = "other/develop" },
		func(s *GitStatus) { s.Base.Behind = 0 }, func(s *GitStatus) { s.Base = nil }, func(s *GitStatus) { s.IsMainBranch = true },
	} {
		current := status()
		change(&current)
		if _, ok := original.request("site", current); ok {
			t.Fatalf("accepted changed/blocked checkout: %+v", current)
		}
	}
	for _, req := range []cardMenuRequest{{Kind: "edits", BaseBranch: "develop"}, {Kind: "branch", BaseBranch: "main"}, {Kind: "branch", BaseBranch: "develop", Number: 4}, {Kind: "branch", BaseBranch: "develop", BundleID: "app"}} {
		if makeCardGitOption(req, status()) != nil {
			t.Fatal("rebase offered on another target")
		}
	}
	dirty := status()
	dirty.Changes = 1
	if option := makeCardGitOption(menu, dirty); option == nil || option.DisabledReason == "" {
		t.Fatal("dirty checkout should show disabled rebase with a reason")
	}
	trackingBase := status()
	trackingBase.Tracking = "origin/develop"
	trackingBase.Behind = 3
	trackingBase.PushTarget = ""
	if _, ok := original.request("site", trackingBase); ok {
		t.Fatal("tracking the default branch must not publish to it")
	}
	var absent *cardGitOption
	if _, ok := absent.request("site", status()); ok {
		t.Fatal("absent menu action accepted")
	}
}

func TestCardMenuContentsAndPrimaryAction(t *testing.T) {
	update := &cardGitOption{Kind: "update", Branch: "develop"}
	approved := &cardGitOption{Kind: "approved", Target: "develop", approvedPR: &approvedBranchPR{Number: 42}}
	for _, tc := range []struct {
		req     cardMenuRequest
		git     GitStatus
		option  *cardGitOption
		titles  string
		actions []cardMenuAction
	}{
		{cardMenuRequest{Kind: "strip"}, GitStatus{}, nil, "", nil},
		{cardMenuRequest{Kind: "auto"}, GitStatus{Head: "head"}, nil, "View branch|Review", []cardMenuAction{cardMenuView, cardMenuViewDiffs}},
		{cardMenuRequest{Kind: "edits"}, GitStatus{Head: "head"}, update, "View branch|Fetch and update develop…|Review", []cardMenuAction{cardMenuView, cardMenuGitAction, cardMenuViewDiffs}},
		{cardMenuRequest{Kind: "branch"}, GitStatus{}, update, "Fetch and update develop…|View branch", []cardMenuAction{cardMenuGitAction, cardMenuView}},
		{cardMenuRequest{Kind: "branch", PublishButton: true}, GitStatus{Head: "head"}, nil, "", nil},
		{cardMenuRequest{Kind: "branch", PublishButton: true}, GitStatus{Head: "head"}, &cardGitOption{Kind: "publish"}, "Push rebased branch…|Review", []cardMenuAction{cardMenuGitAction, cardMenuViewDiffs}},
		{cardMenuRequest{Kind: "site"}, GitStatus{Head: "head"}, update, "Go to Site desktop|Fetch and update develop…|Review", []cardMenuAction{cardMenuGoToSite, cardMenuGitAction, cardMenuViewDiffs}},
		{cardMenuRequest{BundleID: "com.jetbrains.goland", appName: "GoLand"}, GitStatus{}, nil, "Show GoLand", []cardMenuAction{cardMenuShowApp}},
		{cardMenuRequest{Kind: "local"}, GitStatus{}, nil, "View local commits", []cardMenuAction{cardMenuView}},
		{cardMenuRequest{Kind: "remote"}, GitStatus{}, nil, "View remote commits", []cardMenuAction{cardMenuView}},
		{cardMenuRequest{Kind: "site", Number: 42}, GitStatus{}, nil, "Open PR #42 in browser|Open PR #42 in new browser tab", []cardMenuAction{cardMenuOpenPR, cardMenuOpenPRNew}},
		{cardMenuRequest{Kind: "review-status", Number: 42}, GitStatus{Head: "head"}, nil, "Review|View branch|Open PR #42 in browser|Open PR #42 in new browser tab", []cardMenuAction{cardMenuViewDiffs, cardMenuView, cardMenuOpenPR, cardMenuOpenPRNew}},
		{cardMenuRequest{Kind: "review-status", Number: 42}, GitStatus{}, nil, "View branch|Open PR #42 in browser|Open PR #42 in new browser tab", []cardMenuAction{cardMenuView, cardMenuOpenPR, cardMenuOpenPRNew}},
		{cardMenuRequest{Kind: "pr-status", Number: 42}, GitStatus{Head: "head"}, approved, "View branch|Review|Return to develop & delete local branch…|Open PR #42 in browser|Open PR #42 in new browser tab", []cardMenuAction{cardMenuView, cardMenuViewDiffs, cardMenuGitAction, cardMenuOpenPR, cardMenuOpenPRNew}},
	} {
		var titles []string
		var actions []cardMenuAction
		for _, item := range cardMenuActions(tc.req, tc.git, tc.option) {
			titles = append(titles, item.Title)
			actions = append(actions, item.Action)
		}
		if strings.Join(titles, "|") != tc.titles || !slices.Equal(actions, tc.actions) {
			t.Errorf("menu for %+v = %v (%v)", tc.req, titles, actions)
		}
	}
	update.DisabledReason = "Commit or stash local edits."
	actions := cardMenuActions(cardMenuRequest{Kind: "branch"}, GitStatus{}, update)
	if !actions[0].Disabled || actions[0].Hint != update.DisabledReason {
		t.Fatal("disabled primary action lost its explanation", actions)
	}
	for _, strip := range []bool{false, true} {
		for _, hidden := range []bool{false, true} {
			items := withCardMenuControls(actions, strip, hidden)
			tail := items[len(actions):]
			want := []cardMenuAction{cardMenuDismissed}
			if strip {
				sites := cardMenuHideSites
				if hidden {
					sites = cardMenuShowSites
				}
				want = append(want, cardMenuMinimize, cardMenuHide, sites)
			}
			want = append(want, cardMenuSettings, cardMenuQuit)
			var got []cardMenuAction
			for _, item := range tail {
				got = append(got, item.Action)
			}
			if !slices.Equal(got, want) {
				t.Fatalf("unexpected utility menu %v", got)
			}
		}
	}
}

func TestCardMenuLeavesNewWindowsToFileMenu(t *testing.T) {
	for _, kind := range []string{"auto", "edits", "branch", "local", "remote"} {
		req := cardMenuRequest{SiteID: "site", Kind: kind}
		items := cardMenuActions(req, GitStatus{}, nil)
		if len(items) != 1 || items[0].Action != cardMenuView {
			t.Fatalf("wrong viewer choices for %s: %v", kind, items)
		}
	}
}

func TestMergedCardMenuPreparesDeletionAndRejectsChangedState(t *testing.T) {
	status := func() GitStatus {
		return GitStatus{Branch: "feature", Head: "head", DefaultBranch: "develop", Tracking: "origin/feature",
			MergedPR: &MergedPullRequest{Number: 42, Head: "head", SameHead: true, Base: "develop"}}
	}
	var option *cardGitOption
	for _, menu := range []cardMenuRequest{{Kind: "site"}, {Kind: "branch"}, {Kind: "edits"}, {Kind: "site", Number: 42}, {Kind: "return-status"}} {
		option = makeCardGitOption(menu, status())
		if option == nil || option.DisabledReason != "" || option.title() != "Return to develop…" {
			t.Fatalf("missing merged action: %+v", option)
		}
		action, ok := option.request("site", status())
		if !ok || action.Kind != "merged" || action.SiteID != "site" || action.Branch != "feature" || action.Head != "head" || action.TargetBranch != "develop" || !action.DeleteBranch {
			t.Fatalf("wrong preparation: %+v", action)
		}
		if menu.Number > 0 && cardMenuActions(menu, status(), option)[0].Title != "Open PR #42 in browser" {
			t.Fatal("cleanup displaced the default browser action")
		}
	}
	for _, change := range []func(*GitStatus){
		func(s *GitStatus) { s.Changes = 1 }, func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.LocalNote = "cached" },
		func(s *GitStatus) { s.Head = "new" }, func(s *GitStatus) { s.Branch = "other" }, func(s *GitStatus) { s.Tracking = "other/feature" },
		func(s *GitStatus) { s.DefaultBranch = "main" }, func(s *GitStatus) { s.DefaultBranch = "" }, func(s *GitStatus) { s.IsMainBranch = true },
		func(s *GitStatus) { s.MergedPR.SameHead = false }, func(s *GitStatus) { s.MergedPR.Head = "other" },
		func(s *GitStatus) { s.MergedPR.Base = "main" }, func(s *GitStatus) { s.MergedPR.Number = 43 }, func(s *GitStatus) { s.MergedPR = nil },
	} {
		current := status()
		change(&current)
		if _, ok := option.request("site", current); ok {
			t.Fatalf("stale or blocked cleanup accepted: %+v", current)
		}
	}
	for _, menu := range []cardMenuRequest{{Kind: "site", Number: 43}, {Kind: "site", BundleID: "app"}, {Kind: "branch", BaseBranch: "develop"}, {Kind: "branch", PublishRebased: true}} {
		if makeCardGitOption(menu, status()) != nil {
			t.Fatal("cleanup offered for a different action target")
		}
	}
	dirty := status()
	dirty.Changes = 1
	if option := makeCardGitOption(cardMenuRequest{Kind: "branch"}, dirty); option == nil || option.DisabledReason == "" {
		t.Fatal("dirty merged branch must explain why cleanup is disabled")
	}
}

func TestReturnStatusMenuUsesReturnThenViewWithoutReviewOrBrowser(t *testing.T) {
	for _, merged := range []bool{false, true} {
		for _, branch := range []string{"develop", "main"} {
			status := GitStatus{Branch: "feature", Head: "head", DefaultBranch: branch, Tracking: "origin/feature", Summary: "Remote branch removed"}
			if merged {
				status.MergedPR = &MergedPullRequest{Number: 42, Head: "head", SameHead: true, Base: branch}
			}
			req := cardMenuRequest{Kind: "return-status"}
			option := makeCardGitOption(req, status)
			if option == nil || option.title() != "Return to "+branch+"…" || cardMenuActions(req, status, option)[0].Action != cardMenuGitAction {
				t.Fatalf("missing primary return action: %+v", option)
			}
			items := cardMenuActions(req, status, option)
			if len(items) != 2 || items[1].Title != "View branch" {
				t.Fatalf("unexpected status actions: %v", items)
			}
			for _, action := range []cardMenuAction{cardMenuViewDiffs, cardMenuOpenPR, cardMenuOpenPRNew} {
				if validCardMenuSelection(items, req, action, PullRequestLink{}, status) {
					t.Fatalf("unexpected navigation action %d", action)
				}
			}
			action, ok := option.request("site", status)
			if !ok || action.TargetBranch != branch || action.DeleteBranch != merged || (!merged && action.Kind != "switch") || (merged && action.Kind != "merged") {
				t.Fatalf("incorrect return preparation: %+v", action)
			}
			if cardMenuViewKind(req.Kind, status) != "branch" {
				t.Fatal("View branch did not select History")
			}
			status.Changes = 1
			if cardMenuViewKind(req.Kind, status) != "edits" || makeCardGitOption(req, status).DisabledReason == "" {
				t.Fatal("local edits must remain viewable and block returning")
			}
		}
	}
}

func TestRemovedRemoteReturnRejectsChangedStateAndOtherGitSuggestions(t *testing.T) {
	status := GitStatus{Branch: "feature", Head: "head", DefaultBranch: "main", Tracking: "origin/feature", RemoteNote: "Remote branch no longer exists"}
	menu := cardMenuRequest{Kind: "return-status"}
	option := makeCardGitOption(menu, status)
	if option == nil || option.Kind != "return" {
		t.Fatal("missing return action for the freshness row")
	}
	for _, change := range []func(*GitStatus){
		func(s *GitStatus) { s.Changes = 1 }, func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.LocalNote = "unavailable" },
		func(s *GitStatus) { s.Branch = "other" }, func(s *GitStatus) { s.Head = "new" }, func(s *GitStatus) { s.Tracking = "other/feature" },
		func(s *GitStatus) { s.DefaultBranch = "develop" }, func(s *GitStatus) { s.DefaultBranch = "" }, func(s *GitStatus) { s.IsMainBranch = true },
		func(s *GitStatus) { s.RemoteNote = "" },
		func(s *GitStatus) {
			s.MergedPR = &MergedPullRequest{Number: 42, Head: "head", SameHead: true, Base: "main"}
		},
	} {
		current := status
		change(&current)
		if _, ok := option.request("site", current); ok {
			t.Fatalf("accepted changed return action: %+v", current)
		}
	}
	status.RemoteNote, status.Ahead, status.PushTarget = "", 2, "origin/feature"
	if makeCardGitOption(menu, status) != nil {
		t.Fatal("return status must not fall back to a push")
	}
}

func TestCardPublishRevalidatesBranchAndOnlyPrepares(t *testing.T) {
	status := GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature", PushTarget: "origin/feature", Rebased: true}
	req := cardMenuRequest{SiteID: "site", Kind: "branch", PublishRebased: true}
	if _, ok := normalizeCardMenuRequest(req); !ok {
		t.Fatal("publish target rejected")
	}
	option := makeCardGitOption(req, status)
	action, ok := option.request("site", status)
	if !ok || action.Kind != "publish" || action.Head != "head" || action.Branch != "feature" {
		t.Fatalf("wrong preparation: %+v", action)
	}
	for _, change := range []func(*GitStatus){
		func(s *GitStatus) { s.Head = "new" }, func(s *GitStatus) { s.Branch = "other" }, func(s *GitStatus) { s.Tracking = "origin/other" },
		func(s *GitStatus) { s.Rebased = false }, func(s *GitStatus) { s.Conflicts = 1 }, func(s *GitStatus) { s.Changes = 1 },
		func(s *GitStatus) { s.PushTarget = "" }, func(s *GitStatus) { s.LocalNote = "unavailable" }, func(s *GitStatus) { s.CanReplaceRemote = true },
	} {
		current := status
		change(&current)
		if _, ok := option.request("site", current); ok {
			t.Fatalf("unsafe selection: %+v", current)
		}
	}
	for _, change := range []func(*cardMenuRequest){
		func(r *cardMenuRequest) { r.Kind = "site" }, func(r *cardMenuRequest) { r.BaseBranch = "develop" }, func(r *cardMenuRequest) { r.Number = 1 }, func(r *cardMenuRequest) { r.BundleID = "app" },
	} {
		invalid := req
		change(&invalid)
		if _, ok := normalizeCardMenuRequest(invalid); ok {
			t.Fatal("ambiguous target allowed")
		}
	}
}

func TestSitePublishSourceOnlyAuthorizesItsCurrentRebasedBranch(t *testing.T) {
	window := &application.WebviewWindow{}
	status := GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature", PushTarget: "origin/feature", Rebased: true}
	site := &siteRuntime{Ready: true, Window: window, Git: status}
	m := &siteManager{runtime: map[string]*siteRuntime{"a": site}}
	req := ActionRequest{SiteID: "a", Kind: "publish", Branch: "feature", Head: "head"}
	if m.sitePublishSource(req, "site-a") != window {
		t.Fatal("own publication button rejected")
	}
	for _, sender := range []string{"strip", "site-b", "suggested-actions", "settings", "git-viewer-1", ""} {
		if m.sitePublishSource(req, sender) != nil {
			t.Fatalf("accepted sender %s", sender)
		}
	}
	for _, change := range []func(*ActionRequest){
		func(r *ActionRequest) { r.SiteID = "b" }, func(r *ActionRequest) { r.Kind = "push" }, func(r *ActionRequest) { r.Kind = "rebase" }, func(r *ActionRequest) { r.Kind = "switch" }, func(r *ActionRequest) { r.Head = "stale" }, func(r *ActionRequest) { r.Branch = "other" },
	} {
		invalid := req
		change(&invalid)
		if m.sitePublishSource(invalid, "site-a") != nil {
			t.Fatalf("accepted stale/wrong request: %+v", invalid)
		}
	}
	for _, change := range []func(*siteRuntime){
		func(s *siteRuntime) { s.Ready = false }, func(s *siteRuntime) { s.Window = nil }, func(s *siteRuntime) { s.Git.Rebased = false }, func(s *siteRuntime) { s.Git.Changes = 1 }, func(s *siteRuntime) { s.Git.Conflicts = 1 }, func(s *siteRuntime) { s.Git.LocalNote = "unavailable" }, func(s *siteRuntime) { s.Git.PushTarget = "" },
	} {
		*site = siteRuntime{Ready: true, Window: window, Git: status}
		change(site)
		if m.sitePublishSource(req, "site-a") != nil {
			t.Fatal("accepted blocked Site")
		}
	}
	*site = siteRuntime{Ready: true, Window: window, Git: status}
	for _, status := range []string{"preparing", "running"} {
		m.actionDialogs = map[string]*actionDialog{"own": {siteID: "a", action: &CheckoutAction{SiteID: "a", Status: status}}}
		if m.sitePublishSource(req, "site-a") != nil {
			t.Fatal("accepted during busy action")
		}
		m.actionDialogs["own"].siteID = "other"
		if m.sitePublishSource(req, "site-a") != window {
			t.Fatal("another Site's action blocked publication")
		}
	}
}

func TestCardMenuChoosesChangesOrHistoryWithoutChangingGitActionTargets(t *testing.T) {
	for _, test := range []struct {
		git  GitStatus
		want string
	}{
		{GitStatus{Branch: "main"}, "branch"},
		{GitStatus{Branch: "main", Changes: 2}, "edits"},
		{GitStatus{Branch: "main", Conflicts: 1}, "edits"},
		{GitStatus{Branch: "main", LocalNote: "Could not read local status"}, "edits"},
		{GitStatus{}, "edits"},
	} {
		for _, kind := range []string{"auto", "edits", "branch", "review-status"} {
			if got := cardMenuViewKind(kind, test.git); got != test.want {
				t.Errorf("%s menu for %#v = %q, want %q", kind, test.git, got, test.want)
			}
		}
		for _, kind := range []string{"site", "local", "remote"} {
			if got := cardMenuViewKind(kind, test.git); got != kind {
				t.Fatal("separate navigation target changed")
			}
		}
	}
	for _, req := range []cardMenuRequest{{Kind: "site"}, {BundleID: "com.jetbrains.goland"}} {
		if validCardMenuSelection(cardMenuActions(req, GitStatus{}, nil), req, cardMenuOpenPRNew, PullRequestLink{}, GitStatus{}) {
			t.Fatal("activation offered a new window")
		}
	}
}

func TestCardMenuOffersDiffsForLikelyBranchChanges(t *testing.T) {
	for _, test := range []struct {
		name   string
		status GitStatus
		want   bool
	}{
		{"synced main", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Tracking: "origin/main"}, false},
		{"synced custom default", GitStatus{Branch: "trunk", Head: "head", IsMainBranch: true}, false},
		{"synced feature", GitStatus{Branch: "feature", Head: "head", Tracking: "origin/feature"}, true},
		{"unpublished feature", GitStatus{Branch: "feature", Head: "head"}, true},
		{"main edits", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Changes: 2}, true},
		{"main conflicts", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Conflicts: 1}, true},
		{"main ahead", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Ahead: 1}, true},
		{"main behind", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Behind: 1}, false},
		{"develop behind", GitStatus{Branch: "develop", Head: "head", IsMainBranch: true, Behind: 3}, false},
		{"main behind with edits", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Behind: 1, Changes: 1}, true},
		{"main diverged", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Behind: 1, Ahead: 1}, true},
		{"main rewritten", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Behind: 1, Rewritten: true}, true},
		{"untracked main ahead", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Comparison: &HistoryComparison{Ahead: 1}}, true},
		{"untracked main behind", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Comparison: &HistoryComparison{Behind: 1}}, false},
		{"untracked main synced", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Comparison: &HistoryComparison{}}, false},
		{"unborn branch", GitStatus{Branch: "feature", Changes: 1}, false},
		{"unavailable status", GitStatus{Branch: "feature", Head: "head", LocalNote: "Unavailable"}, false},
		{"no repository", GitStatus{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, kind := range []string{"site", "auto", "edits", "branch", "local", "remote"} {
				req := cardMenuRequest{SiteID: "a", Kind: kind}
				if got := validCardMenuSelection(cardMenuActions(req, test.status, nil), req, cardMenuViewDiffs, PullRequestLink{}, test.status); got != test.want {
					t.Errorf("%s: Review eligibility = %v, want %v", kind, got, test.want)
				}
			}
		})
	}
}

func TestCardMenuDiffsRevalidatesEligibilityAndKeepsDedicatedMenus(t *testing.T) {
	status := GitStatus{Branch: "feature", Head: "head"}
	for _, req := range []cardMenuRequest{
		{Kind: "strip"}, {Kind: "branch", Number: 42}, {Kind: "site", BundleID: "com.example.editor"},
	} {
		if validCardMenuSelection(cardMenuActions(req, status, nil), req, cardMenuViewDiffs, PullRequestLink{}, status) {
			t.Fatalf("Review offered on a dedicated menu: %+v", req)
		}
	}
	req := cardMenuRequest{SiteID: "a", Kind: "branch"}
	if validCardMenuSelection(cardMenuActions(req, GitStatus{}, nil), req, cardMenuViewDiffs, PullRequestLink{}, status) {
		t.Fatal("accepted an item that was not offered")
	}
	items := cardMenuActions(req, status, nil)
	for _, current := range []GitStatus{
		{}, {Branch: "main", Head: "head", IsMainBranch: true}, {Branch: "feature", Head: "head", LocalNote: "Unavailable"},
	} {
		if validCardMenuSelection(items, req, cardMenuViewDiffs, PullRequestLink{}, current) {
			t.Fatalf("accepted stale diff eligibility: %+v", current)
		}
	}
}

func TestCardMenuSourceValidatesSenderAndSite(t *testing.T) {
	strip, window := &application.WebviewWindow{}, &application.WebviewWindow{}
	site := &siteRuntime{Ready: true, Window: window, Git: GitStatus{PullRequests: []PullRequestLink{{Number: 123, URL: "https://github.com/owner/repo/pull/123"}}}}
	m := &siteManager{strip: strip, stripReady: true, runtime: map[string]*siteRuntime{"alpha": site}}
	req := cardMenuRequest{SiteID: "alpha", Kind: "edits"}
	for _, test := range []struct {
		sender string
		window *application.WebviewWindow
	}{
		{"strip", strip}, {"site-alpha", window},
		{"site-other", nil}, {"settings", nil}, {"git-viewer-1", nil}, {"", nil},
	} {
		got, source := m.cardMenuSource(req, test.sender)
		if got != test.window || (got != nil && source != site) {
			t.Errorf("sender %q routed to %p, %p", test.sender, got, source)
		}
	}
	m.stripReady = false
	if got, _ := m.cardMenuSource(req, "strip"); got != nil {
		t.Fatal("strip accepted before ready")
	}
	site.Ready = false
	if got, _ := m.cardMenuSource(req, "site-alpha"); got != nil {
		t.Fatal("Site accepted before ready")
	}
	delete(m.runtime, "alpha")
	if got, _ := m.cardMenuSource(req, "site-alpha"); got != nil {
		t.Fatal("removed Site accepted")
	}
}

func TestCardMenuAppActionsSurviveStalePRButNavigationDoesNot(t *testing.T) {
	pr := PullRequestLink{Number: 123, URL: "https://github.com/owner/repo/pull/123"}
	req := cardMenuRequest{SiteID: "alpha", Kind: "edits", Number: 123}
	current := GitStatus{PullRequests: []PullRequestLink{pr}}
	items := withCardMenuControls(cardMenuActions(req, current, nil), false, false)
	for _, action := range []cardMenuAction{cardMenuOpenPR, cardMenuOpenPRNew, cardMenuSettings, cardMenuQuit} {
		if !validCardMenuSelection(items, req, action, pr, current) {
			t.Fatalf("valid action %d rejected", action)
		}
	}
	for _, status := range []GitStatus{{}, {PullRequests: []PullRequestLink{{Number: 123, URL: "https://github.com/other/repo/pull/123"}}}} {
		for _, action := range []cardMenuAction{cardMenuSettings, cardMenuQuit} {
			if !validCardMenuSelection(items, req, action, pr, status) {
				t.Fatalf("app action %d was blocked when the clicked PR changed", action)
			}
		}
		for _, action := range []cardMenuAction{cardMenuOpenPR, cardMenuOpenPRNew} {
			if validCardMenuSelection(items, req, action, pr, status) {
				t.Fatalf("stale PR navigation action %d accepted", action)
			}
		}
	}
	if validCardMenuSelection(items, req, cardMenuDismissed, pr, current) {
		t.Fatal("dismissed menu accepted")
	}
}

func TestCardReviewStatusMenuKeepsNavigationSeparateAndRevalidatesPR(t *testing.T) {
	pr := PullRequestLink{Number: 123, URL: "https://github.com/owner/repo/pull/123"}
	status := GitStatus{Branch: "feature", Head: "head", PullRequests: []PullRequestLink{pr}, Ahead: 2, PushTarget: "origin/feature"}
	req := cardMenuRequest{SiteID: "a", Kind: "review-status", Number: pr.Number}
	if _, ok := normalizeCardMenuRequest(req); !ok {
		t.Fatal("review status rejected")
	}
	for _, change := range []func(*cardMenuRequest){
		func(r *cardMenuRequest) { r.Number = 0 },
		func(r *cardMenuRequest) { r.BundleID = "app" },
		func(r *cardMenuRequest) { r.BaseBranch = "main" },
		func(r *cardMenuRequest) { r.PublishRebased = true },
		func(r *cardMenuRequest) { r.GitAction = "push" },
	} {
		invalid := req
		change(&invalid)
		if _, ok := normalizeCardMenuRequest(invalid); ok {
			t.Fatalf("ambiguous review target accepted: %+v", invalid)
		}
	}
	items := cardMenuActions(req, status, nil)
	for _, action := range []cardMenuAction{cardMenuView, cardMenuViewDiffs, cardMenuOpenPR, cardMenuOpenPRNew} {
		if !validCardMenuSelection(items, req, action, pr, status) {
			t.Fatalf("review navigation %d rejected", action)
		}
		for _, current := range []GitStatus{
			{Head: "head"},
			{Head: "head", PullRequests: []PullRequestLink{{Number: pr.Number, URL: "https://github.com/other/repo/pull/123"}}},
		} {
			if validCardMenuSelection(items, req, action, pr, current) {
				t.Fatalf("stale review navigation %d accepted", action)
			}
		}
	}
	for _, action := range []cardMenuAction{cardMenuGitAction, cardMenuDismissed} {
		if validCardMenuSelection(items, req, action, pr, status) {
			t.Fatalf("unoffered action %d accepted", action)
		}
	}
	badge := cardMenuRequest{Kind: "site", Number: pr.Number}
	if validCardMenuSelection(cardMenuActions(badge, status, nil), badge, cardMenuView, pr, status) {
		t.Fatal("badge menu accepted branch navigation")
	}
	status.MergedPR = &MergedPullRequest{Number: pr.Number, Head: "head", SameHead: true, Base: "main"}
	status.DefaultBranch = "main"
	if makeCardGitOption(req, status) != nil {
		t.Fatal("Git action displaced review navigation")
	}
	status.LocalNote = "Unavailable"
	if validCardMenuSelection(items, req, cardMenuViewDiffs, pr, status) {
		t.Fatal("Review accepted with unavailable local status")
	}
	if !validCardMenuSelection(items, req, cardMenuOpenPR, pr, status) {
		t.Fatal("unavailable local status blocked the PR browser action")
	}
}

func TestApprovedReturnCardMenu(t *testing.T) {
	statusRow := cardMenuRequest{Kind: "pr-status", Number: 42}
	unapproved := GitStatus{PullRequests: []PullRequestLink{{Number: 42, State: "Review required"}}}
	if items := cardMenuActions(statusRow, unapproved, makeCardGitOption(statusRow, unapproved)); len(items) != 2 || items[0].Action != cardMenuOpenPR || items[1].Action != cardMenuOpenPRNew {
		t.Fatalf("unapproved PR status lost the browser actions: %v", items)
	}
	data := `[{"number":42,"state":"OPEN","reviewDecision":"APPROVED","headRefName":"feature","baseRefName":"develop","headRefOid":"tip","headRepositoryOwner":{"login":"team"},"headRepository":{"nameWithOwner":"team/project"}}]`
	status := GitStatus{Branch: "feature", Head: "tip", DefaultBranch: "develop", Summary: "Up to date", Tracking: "origin/feature",
		PullRequests: openPullRequestLinks([]byte(data), "team/project")}
	for _, menu := range []cardMenuRequest{{Kind: "branch"}, {Kind: "auto"}, {Kind: "pr-status", Number: 42}} {
		option := makeCardGitOption(menu, status)
		items := cardMenuActions(menu, status, option)
		if option == nil || option.title() != "Return to develop & delete local branch…" ||
			len(items) < 3 || items[0].Action != cardMenuView || items[1].Action != cardMenuViewDiffs || items[2].Action != cardMenuGitAction {
			t.Fatalf("wrong actions for %v: %+v", menu, option)
		}
		req, ok := option.request("s", status)
		if !ok || req.Kind != "approved" || !req.DeleteBranch || req.approvedPR.Number != 42 || req.TargetBranch != "develop" {
			t.Fatalf("wrong preparation: %+v", req)
		}
		for _, change := range []func(*GitStatus){
			func(s *GitStatus) { s.Ahead++ }, func(s *GitStatus) { s.Behind++ }, func(s *GitStatus) { s.Changes++ },
			func(s *GitStatus) { s.Conflicts++ }, func(s *GitStatus) { s.RemoteNote = "stale" }, func(s *GitStatus) { s.LocalNote = "cached" },
			func(s *GitStatus) { s.Head = "other" }, func(s *GitStatus) { s.DefaultBranch = "main" }, func(s *GitStatus) { s.IsMainBranch = true },
			func(s *GitStatus) { s.Tracking = "another/feature" }, func(s *GitStatus) { s.Branch = "other" }, func(s *GitStatus) { s.PullRequests = nil },
		} {
			changed := status
			change(&changed)
			if _, ok := option.request("s", changed); ok {
				t.Fatalf("stale selection accepted: %+v", changed)
			}
		}
	}
	for _, target := range []cardMenuRequest{{Kind: "site", Number: 42}, {Kind: "site", BundleID: "app"}, {Kind: "review-status", Number: 42}, {Kind: "return-status"}, {Kind: "pr-status", Number: 43}} {
		if got := makeCardGitOption(target, status); got != nil {
			t.Fatalf("unrelated menu %+v has action %+v", target, got)
		}
	}
}
