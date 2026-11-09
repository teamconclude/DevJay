package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestViewerDesktopPreferencePersistsGlobally(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"sites":[]}`), 0600); err != nil {
		t.Fatal(err)
	}
	c, err := loadSites(path)
	if err != nil || c.ViewersOnSiteDesktop {
		t.Fatal("legacy configuration should keep source-desktop placement", c, err)
	}
	for _, enabled := range []bool{true, false} {
		c.ViewersOnSiteDesktop = enabled
		if err := saveSites(path, c); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadSites(path)
		if err != nil || loaded.ViewersOnSiteDesktop != enabled {
			t.Fatal(loaded, err)
		}
	}
	before := SiteConfig{ActiveProfile: "home", Profiles: []Profile{
		{ID: "home", Settings: ProfileSettings{Layout: "horizontal"}},
		{ID: "office", Settings: ProfileSettings{Layout: "vertical"}},
	}}
	after := before
	after.ViewersOnSiteDesktop = true
	after = captureProfileChanges(before, after, []string{"office"})
	if !reflect.DeepEqual(before.Profiles, after.Profiles) || !applyProfileSettings(after, after.Profiles[1].Settings).ViewersOnSiteDesktop {
		t.Fatal("Profile application changed the global window preference")
	}
}

func TestViewerDesktopNavigationRejectsStaleRequests(t *testing.T) {
	for _, kind := range []string{"changes", "branch", "review"} {
		t.Run(kind, func(t *testing.T) {
			site := &siteRuntime{Site: Site{ID: "a", Path: "/repo"}, Ready: true, Window: &application.WebviewWindow{}}
			r := &gitViewerWindow{state: &GitView{SiteID: "a", Path: "/repo", Kind: kind}, ready: true}
			m := &siteManager{config: SiteConfig{ViewersOnSiteDesktop: true}, runtime: map[string]*siteRuntime{"a": site}, viewers: map[string]*gitViewerWindow{"viewer": r}}
			navigation := windowNavigationGeneration.Add(1)
			r.navigation = navigation
			current := func() bool { return m.viewerNavigationCurrent("viewer", r, site, navigation, true) }
			if !current() {
				t.Fatal("ready Site rejected")
			}
			m.config.SiteWindowsHidden = true
			if !current() {
				t.Fatal("hidden Site lost its desktop target")
			}
			for _, change := range []func(){
				func() { m.config.ViewersOnSiteDesktop = false },
				func() { site.Ready = false },
				func() { site.Site.Path = "/different-folder" },
				func() { m.runtime["a"] = &siteRuntime{} },
				func() { m.viewers["viewer"] = &gitViewerWindow{} },
				func() { m.recovery.paused = true },
				func() { r.navigation++ },
				func() { windowNavigationGeneration.Add(1) },
				func() {
					m.config.ActiveProfile = "other"
					m.config.Profiles = []Profile{{ID: "other", Excluded: []string{"a"}}}
				},
			} {
				change()
				if current() {
					t.Fatal("stale navigation was accepted")
				}
				m.config = SiteConfig{ViewersOnSiteDesktop: true}
				site.Ready, site.Site.Path = true, "/repo"
				m.runtime["a"], m.viewers["viewer"], m.recovery.paused = site, r, false
				navigation = windowNavigationGeneration.Add(1)
				r.navigation = navigation
			}
		})
	}
}

// A visible Site desktop is used without the preference, so toggling the
// preference must not cancel it; every other staleness check still applies.
func TestVisibleSiteDesktopNavigationIgnoresPreference(t *testing.T) {
	site := &siteRuntime{Site: Site{ID: "a", Path: "/repo"}, Ready: true, Window: &application.WebviewWindow{}}
	r := &gitViewerWindow{state: &GitView{SiteID: "a", Path: "/repo", Kind: "review"}, ready: true}
	m := &siteManager{runtime: map[string]*siteRuntime{"a": site}, viewers: map[string]*gitViewerWindow{"viewer": r}}
	navigation := windowNavigationGeneration.Add(1)
	r.navigation = navigation
	if m.viewerDesktopSite(r) != nil || m.viewerSite(r) != site {
		t.Fatal("the Site should be a placement candidate only without the preference")
	}
	for _, enabled := range []bool{false, true, false} {
		m.config.ViewersOnSiteDesktop = enabled
		if !m.viewerNavigationCurrent("viewer", r, site, navigation, false) {
			t.Fatal("preference change cancelled visible-desktop placement")
		}
	}
	site.Site.Path = "/different-folder"
	if m.viewerNavigationCurrent("viewer", r, site, navigation, false) {
		t.Fatal("moved Site folder was accepted")
	}
	site.Site.Path = "/repo"
	windowNavigationGeneration.Add(1)
	if m.viewerNavigationCurrent("viewer", r, site, navigation, false) {
		t.Fatal("newer navigation was accepted")
	}
}
