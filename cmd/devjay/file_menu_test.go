package main

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func fileMenuTestManager() *siteManager {
	m := &siteManager{
		config: SiteConfig{Sites: []Site{{ID: "one", Path: "/one"}, {ID: "two", Path: "/two"}}},
		strip:  &application.WebviewWindow{}, runtime: map[string]*siteRuntime{},
		viewers: map[string]*gitViewerWindow{},
		monitor: monitorSchedule{states: map[string]cardState{}},
	}
	for _, site := range m.config.Sites {
		m.runtime[site.ID] = &siteRuntime{Site: site, Ready: true, Window: &application.WebviewWindow{}, Git: GitStatus{Branch: "feature"}}
	}
	return m
}

func TestFileMenuDuplicatesSourceWindowAndReviewOptions(t *testing.T) {
	m := fileMenuTestManager()
	m.lastWindowSiteID = "two"
	m.monitor.states["two"] = cardState{OnDesktop: true}
	for _, kind := range []string{"edits", "branch", "local", "remote", "review"} {
		t.Run(kind, func(t *testing.T) {
			viewer := &gitViewerWindow{window: &application.WebviewWindow{}, state: &GitView{SiteID: "one", Path: "/one", Kind: kind}}
			if kind == "review" {
				viewer.state.Review = &BranchReview{BaseRef: "origin/develop", IgnoreWhitespace: true}
			}
			m.viewers["git-viewer-1"] = viewer
			for _, review := range []bool{false, true} {
				req, parent, site := m.fileMenuTarget("git-viewer-1", review)
				wantKind := kind
				if review {
					wantKind = "review"
				}
				if site != m.runtime["one"] || parent != viewer.window || req.SiteID != "one" || req.Kind != wantKind || !req.NewWindow || req.Anchor != nil {
					t.Fatalf("wrong source: %+v, %p, %p", req, parent, site)
				}
				if m.existingViewer(req) != nil {
					t.Fatal("New Window reused the source window")
				}
				if kind == "review" && (req.BaseRef != "origin/develop" || !req.IgnoreWhitespace) {
					t.Fatalf("lost review comparison options: %+v", req)
				}
			}
		})
	}
}

func TestFileMenuUsesSiteWindowAndCurrentDesktop(t *testing.T) {
	m := fileMenuTestManager()
	m.runtime["one"].Git.Changes = 1
	m.lastWindowSiteID = "two"
	req, parent, site := m.fileMenuTarget("site-one", false)
	if site != m.runtime["one"] || parent != site.Window || req.Kind != "edits" {
		t.Fatalf("ignored source Site or edits: %+v", req)
	}
	check := func(want string) {
		t.Helper()
		req, parent, site := m.fileMenuTarget("strip", false)
		if site == nil || req.SiteID != want || parent != m.strip || !req.NewWindow {
			t.Fatalf("strip target = %+v, want %s", req, want)
		}
	}
	check("two") // No Site here: use the most recently used one.
	m.monitor.states["one"] = cardState{OnDesktop: true}
	check("one") // This desktop takes precedence over a previous desktop.
	m.monitor.states["two"] = cardState{OnDesktop: true}
	check("two") // Multiple Sites here: preserve the user's recent choice.
	m.lastWindowSiteID = "removed"
	check("one")
	m.runtime["one"].Ready = false
	check("two")
	m.config.ActiveProfile = "profile"
	m.config.Profiles = []Profile{{ID: "profile", Excluded: []string{"two"}}}
	if _, _, site := m.fileMenuTarget("strip", false); site != nil {
		t.Fatal("selected an unavailable or disabled Site")
	}
}

func TestFileMenuRejectsStaleWindowSite(t *testing.T) {
	if _, _, site := fileMenuTestManager().fileMenuTarget("git-viewer-closed", false); site != nil {
		t.Fatal("closed source window fell back to another Site")
	}
	for _, change := range []string{"removed", "relocated", "not-ready", "excluded"} {
		t.Run(change, func(t *testing.T) {
			m := fileMenuTestManager()
			m.viewers["git-viewer-1"] = &gitViewerWindow{state: &GitView{SiteID: "one", Path: "/one", Kind: "review"}}
			switch change {
			case "removed":
				delete(m.runtime, "one")
			case "relocated":
				m.runtime["one"].Site.Path = "/elsewhere"
			case "not-ready":
				m.runtime["one"].Ready = false
			case "excluded":
				m.config.ActiveProfile = "profile"
				m.config.Profiles = []Profile{{ID: "profile", Excluded: []string{"one"}}}
			}
			if _, _, site := m.fileMenuTarget("git-viewer-1", true); site != nil {
				t.Fatal("opened an unrelated Site from a stale viewer")
			}
		})
	}
}
