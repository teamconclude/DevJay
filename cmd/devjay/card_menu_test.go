package main

import (
	"math"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestNormalizeCardMenuRequest(t *testing.T) {
	valid := cardMenuRequest{SiteID: "alpha", X: 23.5, Y: 60}
	got, ok := normalizeCardMenuRequest(valid)
	if !ok || got.Kind != "site" {
		t.Fatalf("card background should select desktop navigation: %#v, %v", got, ok)
	}
	for _, kind := range []string{"auto", "site", "edits", "branch", "local", "remote"} {
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

func TestCardMenuActionLabels(t *testing.T) {
	for _, test := range []struct {
		req    cardMenuRequest
		labels [4]string
	}{
		{cardMenuRequest{Kind: "auto"}, [4]string{"View branch", "", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "edits"}, [4]string{"View branch", "", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "branch"}, [4]string{"View branch", "", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "edits", hasViewer: true}, [4]string{"View branch", "View branch in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "branch", hasViewer: true}, [4]string{"View branch", "View branch in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "site"}, [4]string{"Go to Site desktop", "", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{BundleID: "com.jetbrains.goland", appName: "GoLand"}, [4]string{"Show GoLand", "", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "local", hasViewer: true}, [4]string{"View local commits", "View local commits in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "remote", hasViewer: true}, [4]string{"View remote commits", "View remote commits in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "edits", Number: 123}, [4]string{"Open PR #123 in browser", "Open PR #123 in new browser tab", "DevJay settings…", "Quit DevJay"}},
	} {
		if got := cardMenuLabels(test.req); got != test.labels {
			t.Errorf("menu for %#v = %#v, want %#v", test.req, got, test.labels)
		}
	}
}

func TestCardMenuNewWindowRequiresAnExistingViewerForThisSite(t *testing.T) {
	m := &siteManager{viewers: map[string]*gitViewerWindow{
		"other": {state: &GitView{SiteID: "other", Kind: "branch"}},
	}}
	check := func(wantNewWindow bool) {
		t.Helper()
		for _, kind := range []string{"auto", "edits", "branch", "local", "remote"} {
			req := m.resolveCardMenuRequest(cardMenuRequest{SiteID: "site", Kind: kind}, GitStatus{Branch: "feature"})
			labels := cardMenuLabels(req)
			if labels[0] == "" || (labels[1] != "") != wantNewWindow {
				t.Fatalf("wrong viewer choices for %s: %v", kind, labels)
			}
			if !validCardMenuSelection(req, cardMenuView, PullRequestLink{}, GitStatus{}) ||
				validCardMenuSelection(req, cardMenuViewNew, PullRequestLink{}, GitStatus{}) != wantNewWindow {
				t.Fatalf("selection does not match the displayed choices for %s", kind)
			}
		}
	}
	check(false)
	// A viewer in another category is still reused, even while loading.
	m.viewers["own"] = &gitViewerWindow{state: &GitView{SiteID: "site", Kind: "edits", Loading: true}}
	check(true)
	delete(m.viewers, "own")
	check(false)
}

func TestMergedCardMenuPreparesDeletionAndRejectsChangedState(t *testing.T) {
	status := func() GitStatus {
		return GitStatus{Branch: "feature", Head: "head", DefaultBranch: "develop", Tracking: "origin/feature",
			MergedPR: &MergedPullRequest{Number: 42, Head: "head", SameHead: true, Base: "develop"}}
	}
	var option *cardGitOption
	for _, menu := range []cardMenuRequest{{Kind: "site"}, {Kind: "branch"}, {Kind: "edits"}, {Kind: "site", Number: 42}} {
		option = makeCardGitOption(menu, status())
		if option == nil || option.DisabledReason != "" || option.title() != "Return to develop & delete branch…" {
			t.Fatalf("missing merged action: %+v", option)
		}
		action, ok := option.request("site", status())
		if !ok || action.Kind != "merged" || action.SiteID != "site" || action.Branch != "feature" || action.Head != "head" || action.TargetBranch != "develop" || !action.DeleteBranch {
			t.Fatalf("wrong preparation: %+v", action)
		}
		if menu.Number > 0 && cardMenuLabels(menu)[0] != "Open PR #42 in browser" {
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
	m := &siteManager{}
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
		for _, kind := range []string{"auto", "edits", "branch"} {
			req := m.resolveCardMenuRequest(cardMenuRequest{Kind: kind}, test.git)
			if got := cardMenuViewKind(req.Kind, test.git); got != test.want {
				t.Errorf("%s menu for %#v = %q, want %q", kind, test.git, got, test.want)
			}
			if req.Kind != kind {
				t.Fatal("clicked Git action target changed")
			}
		}
		for _, kind := range []string{"site", "local", "remote"} {
			if got := cardMenuViewKind(kind, test.git); got != kind {
				t.Fatal("separate navigation target changed")
			}
		}
	}
	for _, req := range []cardMenuRequest{{Kind: "site"}, {BundleID: "com.jetbrains.goland"}} {
		if validCardMenuSelection(req, cardMenuViewNew, PullRequestLink{}, GitStatus{}) {
			t.Fatal("activation offered a new window")
		}
	}
}

func TestCardMenuOffersDiffsForLikelyBranchChanges(t *testing.T) {
	m := &siteManager{}
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
		{"main behind", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Behind: 1}, true},
		{"untracked main ahead", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Comparison: &HistoryComparison{Ahead: 1}}, true},
		{"untracked main behind", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Comparison: &HistoryComparison{Behind: 1}}, true},
		{"untracked main synced", GitStatus{Branch: "main", Head: "head", IsMainBranch: true, Comparison: &HistoryComparison{}}, false},
		{"unborn branch", GitStatus{Branch: "feature", Changes: 1}, false},
		{"unavailable status", GitStatus{Branch: "feature", Head: "head", LocalNote: "Unavailable"}, false},
		{"no repository", GitStatus{}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, kind := range []string{"site", "auto", "edits", "branch", "local", "remote"} {
				req := m.resolveCardMenuRequest(cardMenuRequest{SiteID: "a", Kind: kind}, test.status)
				if req.viewDiffs != test.want || validCardMenuSelection(req, cardMenuViewDiffs, PullRequestLink{}, test.status) != test.want {
					t.Errorf("%s: View diffs eligibility = %v, want %v", kind, req.viewDiffs, test.want)
				}
			}
		})
	}
}

func TestCardMenuDiffsRevalidatesEligibilityAndKeepsDedicatedMenus(t *testing.T) {
	m := &siteManager{}
	status := GitStatus{Branch: "feature", Head: "head"}
	for _, req := range []cardMenuRequest{
		{Kind: "strip"}, {Kind: "branch", Number: 42}, {Kind: "site", BundleID: "com.example.editor"},
	} {
		req = m.resolveCardMenuRequest(req, status)
		if req.viewDiffs || validCardMenuSelection(req, cardMenuViewDiffs, PullRequestLink{}, status) {
			t.Fatalf("View diffs offered on a dedicated menu: %+v", req)
		}
	}
	req := cardMenuRequest{SiteID: "a", Kind: "branch"}
	if validCardMenuSelection(req, cardMenuViewDiffs, PullRequestLink{}, status) {
		t.Fatal("accepted an item that was not offered")
	}
	req = m.resolveCardMenuRequest(req, status)
	for _, current := range []GitStatus{
		{}, {Branch: "main", Head: "head", IsMainBranch: true}, {Branch: "feature", Head: "head", LocalNote: "Unavailable"},
	} {
		if validCardMenuSelection(req, cardMenuViewDiffs, PullRequestLink{}, current) {
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
	for _, action := range []cardMenuAction{cardMenuView, cardMenuViewNew, cardMenuSettings, cardMenuQuit} {
		if !validCardMenuSelection(req, action, pr, current) {
			t.Fatalf("valid action %d rejected", action)
		}
	}
	for _, status := range []GitStatus{{}, {PullRequests: []PullRequestLink{{Number: 123, URL: "https://github.com/other/repo/pull/123"}}}} {
		for _, action := range []cardMenuAction{cardMenuSettings, cardMenuQuit} {
			if !validCardMenuSelection(req, action, pr, status) {
				t.Fatalf("app action %d was blocked when the clicked PR changed", action)
			}
		}
		for _, action := range []cardMenuAction{cardMenuView, cardMenuViewNew} {
			if validCardMenuSelection(req, action, pr, status) {
				t.Fatalf("stale PR navigation action %d accepted", action)
			}
		}
	}
	if validCardMenuSelection(req, cardMenuDismissed, pr, current) {
		t.Fatal("dismissed menu accepted")
	}
}
