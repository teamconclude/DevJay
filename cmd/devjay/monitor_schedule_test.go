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
	for _, id := range []string{"strip", "settings", "suggestions"} {
		if got := snapshotForWindow(all, id, ""); len(got.Sites) != 2 {
			t.Fatal("overview was filtered")
		}
		if got := snapshotForWindow(all, id, ""); (got.ShortcutHints == 3) != (id == "strip") {
			t.Fatal("wrong shortcut hint target", id)
		}
	}
	if len(all.Sites) != 2 || all.ShortcutHints != 3 {
		t.Fatal("shared snapshot was mutated")
	}
}

func TestMonitorSleepsUntilActualDeadline(t *testing.T) {
	now := time.Unix(1000, 0)
	m := siteManager{monitor: monitorSchedule{nativeDue: now.Add(nativeMonitorFallback)}, folders: folderMonitor{due: now.Add(15 * time.Second)}, profiles: profileMonitor{due: now.Add(nativeMonitorFallback)}}
	if got := m.nextMonitorDelay(now); got != 15*time.Second {
		t.Fatal("quiet manager polls", got)
	}
	m.folders.busy = true
	if got := m.nextMonitorDelay(now); got != nativeMonitorFallback {
		t.Fatal("running folder scan was scheduled twice", got)
	}
	m.folders.busy = false
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

func TestSettingsReceivesHiddenSitesButStripDoesNot(t *testing.T) {
	all := appSnapshot{Profiles: ProfilesView{Current: "home"}, Sites: []SiteView{{Site: Site{ID: "a"}, Enabled: true}}, Catalog: []SiteView{{Site: Site{ID: "a"}, Enabled: true}, {Site: Site{ID: "b"}}}}
	settings, strip := snapshotForWindow(all, "settings", ""), snapshotForWindow(all, "strip", "")
	if len(settings.Sites) != 2 || settings.Profiles.Current != "home" || settings.Catalog != nil {
		t.Fatal("Settings lost catalog")
	}
	if len(strip.Sites) != 1 || strip.Profiles.Current != "" || strip.Catalog != nil {
		t.Fatal("strip leaked hidden Sites or Profile metadata")
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

func TestInteractionWakesAllSitesWithoutInvalidatingWindowSnapshot(t *testing.T) {
	now := time.Unix(1000, 0)
	onDesktop, elsewhere := newGitWatchControl(), newGitWatchControl()
	m := siteManager{
		runtime: map[string]*siteRuntime{"here": {GitControl: onDesktop}, "elsewhere": {GitControl: elsewhere}},
		monitor: monitorSchedule{windowsValid: true, states: map[string]cardState{"here": {OnDesktop: true}}},
	}
	m.handleNativeMonitorEvents(nativeMonitorInteraction, now)
	if !m.monitor.windowsValid || m.monitor.visibilityDirty || !m.monitor.settleDue.IsZero() {
		t.Fatal("input unnecessarily invalidated native window state")
	}
	for _, control := range []*gitWatchControl{onDesktop, elsewhere} {
		_, paused, serial, last := control.state()
		if paused || serial != 0 || last != now || len(control.wake) != 1 {
			t.Fatal("interaction failed to wake a Site or requested a local scan", paused, serial, last)
		}
		<-control.wake
	}
	m.updateGitWatchActivity()
	if len(onDesktop.wake) != 0 || len(elsewhere.wake) != 0 {
		t.Fatal("ordinary snapshot repeated the interaction wakeup")
	}
	m.handleNativeMonitorEvents(nativeMonitorInteraction, now.Add(20*time.Second))
	_, _, _, last := elsewhere.state()
	if last != now.Add(20*time.Second) {
		t.Fatal("continued input did not extend the activity window", last)
	}
	m.recovery.paused = true
	m.updateGitWatchActivity()
	<-elsewhere.wake
	m.handleNativeMonitorEvents(nativeMonitorInteraction, now.Add(90*time.Second))
	_, paused, _, after := elsewhere.state()
	if !paused || after != last || len(elsewhere.wake) != 0 {
		t.Fatal("input renewed activity while sleeping", paused, after)
	}
}

func TestUserCommandResumesPollingWithoutNativeInputOnManagerQueue(t *testing.T) {
	now := time.Now()
	previous := now.Add(-11 * time.Minute)
	for _, paused := range []bool{false, true} {
		here, elsewhere := newGitWatchControl(), newGitWatchControl()
		m := siteManager{
			commands: make(chan func(), 1),
			runtime:  map[string]*siteRuntime{"here": {GitControl: here}, "elsewhere": {GitControl: elsewhere}},
			monitor:  monitorSchedule{lastInteraction: previous, states: map[string]cardState{"here": {OnDesktop: true}}},
		}
		m.recovery.paused = paused
		m.updateGitWatchActivity()
		<-here.wake
		<-elsewhere.wake
		calls := 0
		command := m.userCommand(func() {
			calls++
			if !paused && m.monitor.lastInteraction.Before(now) {
				t.Fatal("menu action ran before activity was recorded")
			}
		})
		m.commands <- command
		if calls != 0 || m.monitor.lastInteraction != previous || len(here.wake) != 0 || len(elsewhere.wake) != 0 {
			t.Fatal("constructing/enqueueing a command changed manager state")
		}
		(<-m.commands)()
		if calls != 1 {
			t.Fatal("menu command did not run exactly once")
		}
		for _, control := range []*gitWatchControl{here, elsewhere} {
			onDesktop, sleeping, _, last := control.state()
			if sleeping != paused {
				t.Fatal("menu action changed sleep state")
			}
			if paused {
				if last != previous || len(control.wake) != 0 {
					t.Fatal("paused menu action renewed polling")
				}
				continue
			}
			want := 15 * time.Second
			if onDesktop {
				want = 10 * time.Second
			}
			if last.Before(now) || len(control.wake) != 1 || gitPollingCadence(onDesktop, last, time.Now()).status != want {
				t.Fatal("menu action left a Site in background cadence")
			}
		}
	}
}
