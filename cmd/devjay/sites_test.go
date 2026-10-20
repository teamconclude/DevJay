package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSitesPersistenceAndValidation(t *testing.T) {
	dir := t.TempDir()
	site, err := newSite(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err = os.Symlink(dir, alias); err != nil {
		t.Fatal(err)
	}
	if _, err = newSite(alias, []Site{site}); err == nil {
		t.Fatal("duplicate symlink accepted")
	}
	if _, err = newSite("relative", nil); err == nil {
		t.Fatal("relative path accepted")
	}
	file := filepath.Join(dir, "not-a-folder")
	if err = os.WriteFile(file, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = newSite(file, nil); err == nil {
		t.Fatal("a dropped file must not become a Site")
	}
	c := SiteConfig{Version: 1, Sites: []Site{site}, Manual: true, Order: []string{site.ID}}
	path := filepath.Join(t.TempDir(), "config", "sites.json")
	if err = saveSites(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSites(path)
	if err != nil || !reflect.DeepEqual(c, loaded) {
		t.Fatalf("roundtrip: %+v %v", loaded, err)
	}
	if err = os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = loadSites(path); err == nil {
		t.Fatal("corruption accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "broken" {
		t.Fatal("corruption overwritten")
	}
}
func TestSiteOrder(t *testing.T) {
	c := SiteConfig{Sites: []Site{{ID: "unknown", Path: "/tmp/Zeta"}, {ID: "right", Path: "/tmp/beta"}, {ID: "later", Path: "/z/Alpha"}, {ID: "first", Path: "/a/alpha"}}}
	ids := func(s []Site) []string {
		r := []string{}
		for _, x := range s {
			r = append(r, x.ID)
		}
		return r
	}
	if got := ids(orderedSites(c)); !reflect.DeepEqual(got, []string{"first", "later", "right", "unknown"}) {
		t.Fatal(got)
	}
	c.Manual = true
	c.Order = []string{"unknown", "later", "first", "right"}
	if !reflect.DeepEqual(ids(orderedSites(c)), c.Order) {
		t.Fatal("manual order")
	}
	if !validOrder(c.Sites, c.Order) || validOrder(c.Sites, []string{"unknown", "later", "first", "first"}) {
		t.Fatal("permutation validation")
	}
	home, _ := os.UserHomeDir()
	prefix, name := displayPath(filepath.Join(home, "c1"))
	if prefix != "~/" || name != "c1" {
		t.Fatal(prefix, name)
	}
}

func TestStripLayoutPersistence(t *testing.T) {
	if (SiteConfig{}).layout() != "horizontal" {
		t.Fatal("old configurations must default to Horizontal")
	}
	site, err := newSite(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	c := SiteConfig{Version: 1, Sites: []Site{site}, Layout: "custom", Placements: map[string]CardPlacement{site.ID: {X: 224, Y: 160, Z: 0}}}
	if !validPlacements(c.Sites, c.Placements) {
		t.Fatal("valid placement rejected")
	}
	path := filepath.Join(t.TempDir(), "sites.json")
	if err := saveSites(path, c); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSites(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.layout() != "custom" || loaded.Placements[site.ID] != c.Placements[site.ID] {
		t.Fatal("layout or placement lost on reload")
	}
	for _, p := range []CardPlacement{{X: -8}, {X: 3}, {Y: 100008}, {Z: 1}} {
		if validPlacements(c.Sites, map[string]CardPlacement{site.ID: p}) {
			t.Fatalf("invalid placement accepted: %+v", p)
		}
	}
	if validPlacements(c.Sites, map[string]CardPlacement{"removed-site": {}}) {
		t.Fatal("stale Site placement accepted")
	}
	if validLayout("manual") {
		t.Fatal("Manual is a sorting mode, not a layout")
	}
}

func TestIDEStatusPreferenceDefaultsOnAndPersistsOff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	for _, body := range []string{`{"version":1}`, `{"version":1,"showIDEStatus":true}`, `{"version":1,"showIDEStatus":false}`} {
		if err := os.WriteFile(path, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		config, err := loadSites(path)
		want := body != `{"version":1,"showIDEStatus":false}`
		if err != nil || config.showIDEStatus() != want {
			t.Fatalf("preference from %s: %+v %v", body, config, err)
		}
		if err := saveSites(path, config); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadSites(path)
		if err != nil || loaded.showIDEStatus() != want {
			t.Fatalf("preference changed on reload: %+v %v", loaded, err)
		}
	}
}

func TestLegacyAppStatusViewPreservesSitesAndStatusPreference(t *testing.T) {
	for _, mode := range []string{"compact", "detailed"} {
		for _, disabled := range []bool{false, true} {
			name := mode + "/default-enabled"
			if disabled {
				name = mode + "/disabled"
			}
			t.Run(name, func(t *testing.T) {
				site, err := newSite(t.TempDir(), nil)
				if err != nil {
					t.Fatal(err)
				}
				no := false
				want := SiteConfig{Version: 1, Sites: []Site{site}, Order: []string{site.ID}, Manual: true,
					PreferredEditor: "com.jetbrains.goland", UseProjectEditor: &no, Layout: "custom", Decoration: "minimal",
					Placements: map[string]CardPlacement{site.ID: {X: 224, Y: 160, Z: 0}}}
				if disabled {
					want.ShowIDEStatus = &no
				}
				data, err := json.Marshal(want)
				if err != nil {
					t.Fatal(err)
				}
				var legacy map[string]json.RawMessage
				if err := json.Unmarshal(data, &legacy); err != nil {
					t.Fatal(err)
				}
				legacy["appStatusView"], _ = json.Marshal(mode)
				data, err = json.Marshal(legacy)
				if err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(t.TempDir(), "sites.json")
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
				loaded, err := loadSites(path)
				if err != nil || !reflect.DeepEqual(loaded, want) || loaded.showIDEStatus() == disabled {
					t.Fatalf("legacy configuration changed: %+v %v", loaded, err)
				}
				unchanged, err := os.ReadFile(path)
				if err != nil || string(unchanged) != string(data) {
					t.Fatalf("loading rewrote legacy configuration: %v", err)
				}
				if err := saveSites(path, loaded); err != nil {
					t.Fatal(err)
				}
				loaded, err = loadSites(path)
				if err != nil || !reflect.DeepEqual(loaded, want) || loaded.showIDEStatus() == disabled {
					t.Fatalf("configuration changed on save/reload: %+v %v", loaded, err)
				}
				data, err = os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				var saved map[string]json.RawMessage
				if err := json.Unmarshal(data, &saved); err != nil {
					t.Fatal(err)
				}
				if _, exists := saved["appStatusView"]; exists {
					t.Fatal("obsolete view preference persisted")
				}
			})
		}
	}
}

func TestSiteWindowVisibilityPersistsOutsideProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	legacy := SiteConfig{Version: 1, Sites: []Site{}}
	if err := saveSites(path, legacy); err != nil {
		t.Fatal(err)
	}
	c, err := loadSites(path)
	if err != nil || c.SiteWindowsHidden {
		t.Fatal("existing settings should leave Site windows visible", err)
	}
	c.SiteWindowsHidden = true
	for _, hidden := range []bool{true, false, true} {
		c.SiteWindowsHidden = hidden
		if err := saveSites(path, c); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadSites(path)
		if err != nil || loaded.SiteWindowsHidden != hidden {
			t.Fatal("visibility did not survive restart", err)
		}
		manager := newSiteManager(nil, loaded, nil)
		if manager.config.SiteWindowsHidden != hidden {
			t.Fatal("manager ignored saved visibility")
		}
		switched := applyProfileSettings(loaded, ProfileSettings{Layout: "vertical"})
		if switched.SiteWindowsHidden != hidden || switched.layout() != "vertical" {
			t.Fatal("profile switch changed group visibility")
		}
	}
}
