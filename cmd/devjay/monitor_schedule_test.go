package main

import (
	"testing"
	"time"
)

func TestSnapshotForWindowKeepsOnlyItsSiteWithoutChangingSharedState(t *testing.T) {
	all := appSnapshot{ShortcutHints: 3, Sites: []SiteView{{Site: Site{ID: "a"}}, {Site: Site{ID: "b"}}}}
	for _, target := range []struct{ id, site string }{{"site-b", ""}, {"git-viewer-1", "b"}} {
		got := snapshotForWindow(all, target.id, target.site)
		if len(got.Sites) != 1 || got.Sites[0].ID != "b" {
			t.Fatal("wrong window snapshot", got)
		}
		if got.ShortcutHints != 0 {
			t.Fatal("modifier hints must not wake Site/viewer web views")
		}
	}
	if got := snapshotForWindow(all, "site-removed", ""); len(got.Sites) != 0 {
		t.Fatal("removed Site received unrelated data")
	}
	for _, id := range []string{"control-panel", "settings", "suggestions"} {
		if got := snapshotForWindow(all, id, ""); len(got.Sites) != 2 {
			t.Fatal("overview was filtered")
		}
		if got := snapshotForWindow(all, id, ""); (got.ShortcutHints == 3) != (id == "control-panel") {
			t.Fatal("wrong shortcut hint target", id)
		}
	}
	if len(all.Sites) != 2 || all.ShortcutHints != 3 {
		t.Fatal("shared snapshot was mutated")
	}
}

func TestMonitorSleepsUntilActualDeadline(t *testing.T) {
	now := time.Unix(1000, 0)
	m := siteManager{monitor: monitorSchedule{nativeDue: now.Add(nativeMonitorFallback)}}
	if got := m.nextMonitorDelay(now); got != nativeMonitorFallback {
		t.Fatal("quiet manager polls", got)
	}
	m.recovery.add("site-a")
	m.recovery.request("site-a", now, false)
	if got := m.nextMonitorDelay(now); got != uiRecoveryDelay {
		t.Fatal("recovery deadline missed", got)
	}
	m.monitor.settleDue = now.Add(120 * time.Millisecond)
	if got := m.nextMonitorDelay(now); got != 120*time.Millisecond {
		t.Fatal("desktop settling delayed", got)
	}
	m.recovery.paused = true
	if got := m.nextMonitorDelay(now); got != nativeMonitorFallback {
		t.Fatal("sleeping manager runs UI work", got)
	}
}

func TestHiddenViewerNeverSchedulesPeriodicReads(t *testing.T) {
	now := time.Unix(1000, 0)
	r := &gitViewerWindow{ready: true, state: &GitView{}, refresh: viewerRefreshSchedule{pending: true}}
	m := siteManager{viewers: map[string]*gitViewerWindow{"viewer": r}, monitor: monitorSchedule{visibility: map[string]bool{}}}
	if !m.nextViewerRefresh(now).IsZero() {
		t.Fatal("hidden viewer scheduled a read")
	}
	m.monitor.visibility["viewer"] = true
	if !m.nextViewerRefresh(now).Equal(now) {
		t.Fatal("visible dirty viewer did not refresh immediately")
	}
	r.refresh.start()
	if !m.nextViewerRefresh(now).IsZero() {
		t.Fatal("running viewer was scheduled twice")
	}
}
