package main

import (
	"reflect"
	"testing"
)

func TestProfileChangesOnlyCopyEditedSettings(t *testing.T) {
	before := SiteConfig{ActiveProfile: "home", Layout: "horizontal", Decoration: "none", Order: []string{"a", "b"}, Profiles: []Profile{
		{ID: "home", Settings: ProfileSettings{Layout: "horizontal", Decoration: "none", Order: []string{"a", "b"}}},
		{ID: "cabin", Settings: ProfileSettings{Layout: "vertical", Decoration: "normal", Order: []string{"b", "a"}}},
		{ID: "laptop", Settings: ProfileSettings{Layout: "custom", Decoration: "minimal"}},
	}}
	after := before
	after.Decoration = "minimal"
	got := captureProfileChanges(before, after, []string{"cabin"})
	if got.Profiles[0].Settings.Decoration != "minimal" || got.Profiles[1].Settings.Decoration != "minimal" {
		t.Fatal("edit not shared")
	}
	if got.Profiles[1].Settings.Layout != "vertical" || !reflect.DeepEqual(got.Profiles[1].Settings.Order, []string{"b", "a"}) {
		t.Fatal("overwrote unrelated settings")
	}
	if before.Profiles[1].Settings.Decoration != "normal" {
		t.Fatal("mutated previous configuration before save")
	}
	if got.Profiles[2].Settings.Layout != "custom" {
		t.Fatal("changed unselected Profile")
	}
}

func TestProfilesFilterShortcutsWithoutChangingCatalog(t *testing.T) {
	c := SiteConfig{ActiveProfile: "home", Manual: true, Order: []string{"b", "a", "c"}, Sites: []Site{{ID: "a"}, {ID: "b"}, {ID: "c"}}, Profiles: []Profile{{ID: "home", Excluded: []string{"a"}}}}
	sites := visibleSites(c)
	if len(sites) != 2 || sites[0].ID != "b" || sites[1].ID != "c" {
		t.Fatal(sites)
	}
	if len(c.Sites) != 3 || len(orderedSites(c)) != 3 {
		t.Fatal("hiding removed global Site")
	}
}

func TestNewSiteInsertionPreservesOtherProfilesOrder(t *testing.T) {
	for _, tc := range []struct{ order, want []string }{
		{[]string{"a", "b", "c"}, []string{"a", "new", "b", "c"}},
		{[]string{"c", "a", "b"}, []string{"c", "a", "new", "b"}},
	} {
		got := insertSiteAfter(tc.order, "new", "a")
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got)
		}
		if len(tc.order) != 3 {
			t.Fatal("mutated original order")
		}
	}
}

func TestRenamingFolderPreservesEveryProfileOrder(t *testing.T) {
	before := SiteConfig{ActiveProfile: "home", Sites: []Site{{ID: "a", Path: "/a"}, {ID: "b", Path: "/b"}}, Profiles: []Profile{
		{ID: "home"}, {ID: "cabin", Settings: ProfileSettings{Manual: true, Order: []string{"b", "a"}}, Excluded: []string{"a"}},
	}}
	after := before
	after.Sites = []Site{{ID: "a", Path: "/z"}, {ID: "b", Path: "/b"}}
	after = preserveRenamedSiteOrder(before, after)
	if !after.Manual || !reflect.DeepEqual(after.Order, []string{"a", "b"}) {
		t.Fatal("renamed Site moved in active Profile", after)
	}
	if !after.Profiles[0].Settings.Manual || !reflect.DeepEqual(after.Profiles[0].Settings.Order, []string{"a", "b"}) {
		t.Fatal("name order lost")
	}
	if !reflect.DeepEqual(after.Profiles[1], before.Profiles[1]) {
		t.Fatal("manual Profile or membership changed")
	}
	if before.Manual || before.Profiles[0].Settings.Manual || before.Sites[0].Path != "/a" {
		t.Fatal("mutated old config")
	}
}

func TestDeleteProfilePreservesActiveArrangementAndCatalog(t *testing.T) {
	before := SiteConfig{ActiveProfile: "home", Sites: []Site{{ID: "a", Path: "/a"}}, Order: []string{"a"}, Profiles: []Profile{
		{ID: "home", Name: "Home", Settings: ProfileSettings{Order: []string{"a"}}},
		{ID: "office", Name: "Office", Displays: []string{"office-displays"}, Excluded: []string{"a"}},
	}}
	got, err := withoutProfile(before, "office")
	if err != nil || len(got.Profiles) != 1 || !reflect.DeepEqual(got.Profiles[0], before.Profiles[0]) {
		t.Fatal(got, err)
	}
	if got.ActiveProfile != before.ActiveProfile || !reflect.DeepEqual(got.Sites, before.Sites) || !reflect.DeepEqual(got.Order, before.Order) {
		t.Fatal("deletion changed the catalog or current arrangement")
	}
	if len(before.Profiles) != 2 || before.Profiles[1].Name != "Office" {
		t.Fatal("mutated the prior configuration before saving")
	}
	for _, id := range []string{"home", "missing"} {
		unchanged, err := withoutProfile(before, id)
		if err == nil || !reflect.DeepEqual(unchanged, before) {
			t.Fatal("accepted an active or stale Profile", id)
		}
	}
	if _, err := withoutProfile(got, "home"); err == nil {
		t.Fatal("removed the last Profile")
	}
}
