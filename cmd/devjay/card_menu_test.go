package main

import (
	"math"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestNormalizeCardMenuRequest(t *testing.T) {
	valid := cardMenuRequest{SiteID: "alpha", X: 23.5, Y: 60}
	got, ok := normalizeCardMenuRequest(valid)
	if !ok || got.Kind != "edits" {
		t.Fatalf("card background should select edits: %#v, %v", got, ok)
	}
	for _, kind := range []string{"edits", "local", "remote"} {
		req := valid
		req.Kind = kind
		if got, ok := normalizeCardMenuRequest(req); !ok || got.Kind != kind {
			t.Errorf("rejected category %q", kind)
		}
	}
	for _, change := range []func(*cardMenuRequest){
		func(r *cardMenuRequest) { r.SiteID = "" },
		func(r *cardMenuRequest) { r.Kind = "settings" },
		func(r *cardMenuRequest) { r.Number = -1 },
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

func TestCardMenuActionLabels(t *testing.T) {
	for _, test := range []struct {
		req    cardMenuRequest
		labels [4]string
	}{
		{cardMenuRequest{Kind: "edits"}, [4]string{"View local edits", "View local edits in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "local"}, [4]string{"View local commits", "View local commits in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "remote"}, [4]string{"View remote commits", "View remote commits in new window", "DevJay settings…", "Quit DevJay"}},
		{cardMenuRequest{Kind: "edits", Number: 123}, [4]string{"Open PR #123", "Open PR #123 in new tab", "DevJay settings…", "Quit DevJay"}},
	} {
		if got := cardMenuLabels(test.req); got != test.labels {
			t.Errorf("menu for %#v = %#v, want %#v", test.req, got, test.labels)
		}
	}
}

func TestCardMenuSourceValidatesSenderAndSite(t *testing.T) {
	panel, window := &application.WebviewWindow{}, &application.WebviewWindow{}
	site := &siteRuntime{Ready: true, Window: window, Git: GitStatus{PullRequests: []PullRequestLink{{Number: 123, URL: "https://github.com/owner/repo/pull/123"}}}}
	m := &siteManager{panel: panel, panelReady: true, runtime: map[string]*siteRuntime{"alpha": site}}
	req := cardMenuRequest{SiteID: "alpha", Kind: "edits"}
	for _, test := range []struct {
		sender string
		window *application.WebviewWindow
	}{
		{"control-panel", panel}, {"site-alpha", window},
		{"site-other", nil}, {"settings", nil}, {"git-viewer-1", nil}, {"", nil},
	} {
		got, source := m.cardMenuSource(req, test.sender)
		if got != test.window || (got != nil && source != site) {
			t.Errorf("sender %q routed to %p, %p", test.sender, got, source)
		}
	}
	m.panelReady = false
	if got, _ := m.cardMenuSource(req, "control-panel"); got != nil {
		t.Fatal("panel accepted before ready")
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
