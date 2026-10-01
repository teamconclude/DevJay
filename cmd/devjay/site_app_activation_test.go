package main

import "testing"

func TestDevelopmentAppActivationRequiresCurrentObservedIdentity(t *testing.T) {
	fixture := func() *siteManager {
		target := developmentAppTarget{WindowNumber: 12, SitePath: "/tmp/site"}
		app := observedDevelopmentApp{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, ProcessIDs: []int{42}}
		return &siteManager{developmentApps: developmentAppMonitor{
			targets:  map[string]developmentAppTarget{"site": target},
			readings: map[string]developmentAppReading{"site": {Target: target, Known: true, Apps: []DevelopmentApp{app.DevelopmentApp}}},
			windows:  developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"site": {app}}},
		}}
	}
	if app, ok := fixture().requestedDevelopmentApp("site", "com.jetbrains.goland"); !ok || app.Name != "GoLand" || app.ProcessIDs[0] != 42 {
		t.Fatal("observed app rejected")
	}
	for _, modify := range []func(*siteManager){
		func(m *siteManager) { m.developmentApps.disabled = true },
		func(m *siteManager) { delete(m.developmentApps.readings, "site") },
		func(m *siteManager) { m.developmentApps.targets["site"] = developmentAppTarget{WindowNumber: 99} },
		func(m *siteManager) {
			r := m.developmentApps.readings["site"]
			r.Known = false
			m.developmentApps.readings["site"] = r
		},
		func(m *siteManager) {
			r := m.developmentApps.readings["site"]
			r.Apps = nil
			m.developmentApps.readings["site"] = r
		},
		func(m *siteManager) { m.developmentApps.windows.Available = false },
		func(m *siteManager) { m.developmentApps.windows.Sites["site"][0].ProcessIDs = []int{42, 43} },
		func(m *siteManager) { m.developmentApps.windows.Sites["site"][0].ProcessIDs = []int{-1} },
		func(m *siteManager) { m.developmentApps.windows.Sites["site"][0].BundleID = "other" },
	} {
		m := fixture()
		modify(m)
		if _, ok := m.requestedDevelopmentApp("site", "com.jetbrains.goland"); ok {
			t.Fatal("stale/unknown app accepted")
		}
	}
	if _, ok := fixture().requestedDevelopmentApp("other", "com.jetbrains.goland"); ok {
		t.Fatal("other Site accepted")
	}
}
