package main

import (
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
	p := map[string]DesktopPosition{"right": {Known: true, MonitorX: 100, Desktop: 0}, "later": {Known: true, MonitorX: -100, Desktop: 2}, "first": {Known: true, MonitorX: -100, Desktop: 1}}
	ids := func(s []Site) []string {
		r := []string{}
		for _, x := range s {
			r = append(r, x.ID)
		}
		return r
	}
	if got := ids(orderedSites(c, p)); !reflect.DeepEqual(got, []string{"first", "later", "right", "unknown"}) {
		t.Fatal(got)
	}
	c.Manual = true
	c.Order = []string{"unknown", "later", "first", "right"}
	if !reflect.DeepEqual(ids(orderedSites(c, p)), c.Order) {
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

func TestPanelLayoutPersistence(t *testing.T) {
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
