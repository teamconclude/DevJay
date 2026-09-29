package main

import (
	"testing"
	"time"
)

func TestDevelopmentAppTargetsIncludeInactiveSpaceWindows(t *testing.T) {
	targets := developmentAppTargets(map[string]uint32{"inactive": 42, "missing": 0}, map[string]DesktopPosition{
		"inactive": {Known: false, Desktop: 7, Label: "Studio Display · Desktop 8"},
	}, []Site{{ID: "inactive", Path: "/work/project"}})
	if len(targets) != 1 || targets["inactive"].WindowNumber != 42 {
		t.Fatal("native detection must receive valid window IDs even when AppKit screen metadata is unavailable")
	}
	if targets["inactive"].SitePath != "/work/project" {
		t.Fatal("agent matching needs the Site folder captured with its window identity")
	}
	targets = developmentAppTargets(nil, nil, []Site{{ID: "pending", Path: "/work/nested"}})
	if targets["pending"].SitePath != "/work/nested" || targets["pending"].WindowNumber != 0 {
		t.Fatal("pending native windows must retain folder scope without inventing a window")
	}
	var monitor developmentAppMonitor
	if _, _, start := monitor.request(targets, time.Now()); start {
		t.Fatal("folder-only targets must not trigger scans")
	}
}

func TestDevelopmentAppMonitorLimitsWholeDesktopScans(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1, Desktop: 2}, "two": {WindowNumber: 2, Desktop: 3}}
	var monitor developmentAppMonitor
	request, generation, ok := monitor.request(targets, now)
	if !ok || len(request) != 2 {
		t.Fatal("one initial scan must serve all Sites")
	}
	if _, _, ok := monitor.request(targets, now.Add(time.Minute)); ok {
		t.Fatal("overlapping scan")
	}
	monitor.finish(request, generation, developmentAppScan{Available: true, Sites: map[string][]DevelopmentApp{"one": {{Name: "GoLand", BundleID: "com.jetbrains.goland"}}, "two": {}}}, now.Add(time.Second))
	if !monitor.readings["one"].Known || !monitor.readings["two"].Known || len(monitor.readings["two"].Apps) != 0 {
		t.Fatal("a known empty desktop differs from unavailable detection")
	}
	if _, _, ok := monitor.request(targets, now.Add(30*time.Second)); ok {
		t.Fatal("interval should start after the preceding scan finishes")
	}
	if _, _, ok := monitor.request(targets, now.Add(31*time.Second)); !ok {
		t.Fatal("unchanged Sites still need periodic app open/close detection")
	}
}

func TestDevelopmentAppMonitorRejectsMovedAndReplacedWindows(t *testing.T) {
	for _, replacement := range []developmentAppTarget{
		{WindowNumber: 1, Desktop: 4},
		{WindowNumber: 2, Desktop: 3},
		{WindowNumber: 1, Desktop: 3, MonitorX: 2000},
		{WindowNumber: 1, Desktop: 3, SitePath: "/work/changed"},
	} {
		now := time.Unix(100, 0)
		targets := map[string]developmentAppTarget{"one": {WindowNumber: 1, Desktop: 3}}
		var monitor developmentAppMonitor
		request, generation, _ := monitor.request(targets, now)
		result := developmentAppScan{Available: true, Sites: map[string][]DevelopmentApp{"one": {{Name: "GoLand"}}}}
		monitor.finish(request, generation, result, now)
		request, generation, _ = monitor.request(targets, now.Add(30*time.Second))
		targets["one"] = replacement
		if _, _, ok := monitor.request(targets, now.Add(31*time.Second)); ok {
			t.Fatal("cannot overlap stale in-flight scan")
		}
		if monitor.readings["one"].Known {
			t.Fatal("must hide the old desktop's app immediately")
		}
		monitor.finish(request, generation, result, now.Add(31*time.Second))
		if monitor.readings["one"].Known {
			t.Fatal("stale result restored the wrong app")
		}
		if _, _, ok := monitor.request(targets, now.Add(32*time.Second)); !ok {
			t.Fatal("moved Site should refresh without waiting thirty seconds")
		}
	}
}

func TestDevelopmentAppMonitorUnavailableAndWake(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1}}
	var monitor developmentAppMonitor
	request, generation, _ := monitor.request(targets, now)
	monitor.finish(request, generation, developmentAppScan{}, now)
	if monitor.readings["one"].Known {
		t.Fatal("failed scans cannot claim no apps")
	}
	request, generation, _ = monitor.request(targets, now.Add(30*time.Second))
	monitor.invalidate()
	monitor.finish(request, generation, developmentAppScan{Available: true, Sites: map[string][]DevelopmentApp{"one": {{Name: "Old app"}}}}, now.Add(40*time.Second))
	if monitor.readings["one"].Known {
		t.Fatal("scan started before sleep must not survive invalidation")
	}
	if _, _, ok := monitor.request(targets, now.Add(40*time.Second)); !ok {
		t.Fatal("wake needs a fresh scan")
	}
}

func TestDevelopmentAppMonitorDoesNotPollWithoutSites(t *testing.T) {
	var monitor developmentAppMonitor
	if _, _, ok := monitor.request(nil, time.Now()); ok {
		t.Fatal("no Sites need no scans")
	}
}

func TestDevelopmentAppMonitorDisabledStopsScansAndRejectsPendingResults(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1}}
	result := developmentAppScan{Available: true, Sites: map[string][]DevelopmentApp{"one": {{Name: "GoLand"}}}}
	var monitor developmentAppMonitor
	request, generation, _ := monitor.request(targets, now)
	monitor.finish(request, generation, result, now)
	request, generation, _ = monitor.request(targets, now.Add(30*time.Second))
	monitor.setEnabled(false)
	if monitor.readings["one"].Known {
		t.Fatal("disabled detection must clear cached labels")
	}
	if _, _, start := monitor.request(targets, now.Add(time.Hour)); start {
		t.Fatal("disabled detection started a scan")
	}
	monitor.setEnabled(true)
	if _, _, start := monitor.request(targets, now.Add(time.Hour)); start {
		t.Fatal("rapid re-enable must wait for the old worker, not overlap it")
	}
	monitor.finish(request, generation, result, now.Add(time.Hour))
	if monitor.readings["one"].Known {
		t.Fatal("result from before disabling cannot restore an old label")
	}
	request, generation, start := monitor.request(targets, now.Add(time.Hour))
	if !start {
		t.Fatal("re-enabled detection needs a fresh scan")
	}
	monitor.finish(request, generation, result, now.Add(time.Hour))
	if !monitor.readings["one"].Known {
		t.Fatal("fresh result should restore detection")
	}
	monitor.setEnabled(false)
	if _, _, start := monitor.request(targets, now.Add(2*time.Hour)); start {
		t.Fatal("disabled detector polled after its worker finished")
	}
}

func appMonitorActivityResult(working, waiting bool) developmentAppScan {
	process := observedAgentProcess{Name: "Claude Code", PID: 123, Started: 456, Cwd: "/work", Working: working, Waiting: waiting}
	app := DevelopmentApp{Name: "GoLand", Agents: []string{"Claude Code"}}
	if working {
		app.WorkingAgents = []string{"Claude Code"}
	}
	if waiting {
		app.WaitingAgents = []string{"Claude Code"}
	}
	return developmentAppScan{Available: true, Sites: map[string][]DevelopmentApp{"one": {app}},
		windows: developmentAppWindowScan{Available: true}, processes: []observedAgentProcess{process}}
}

func TestAgentStatusRefreshDoesNotRepeatWindowInventory(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1}}
	var monitor developmentAppMonitor
	request, generation, ok := monitor.request(targets, now)
	if !ok || !monitor.discovering {
		t.Fatal("initial read must discover")
	}
	monitor.finish(request, generation, appMonitorActivityResult(false, false), now)
	if got := monitor.nextDue(now); !got.Equal(now.Add(developmentAgentIdleInterval)) {
		t.Fatalf("idle deadline %v", got)
	}
	request, generation, ok = monitor.request(targets, now.Add(12*time.Second))
	if !ok || monitor.discovering {
		t.Fatal("idle status refresh rediscovered every window")
	}
	monitor.finish(request, generation, appMonitorActivityResult(true, false), now.Add(12*time.Second))
	if got := monitor.nextDue(now.Add(12 * time.Second)); !got.Equal(now.Add(14 * time.Second)) {
		t.Fatalf("working deadline %v", got)
	}
	request, generation, ok = monitor.request(targets, now.Add(14*time.Second))
	if !ok || monitor.discovering {
		t.Fatal("working status needs a cheap refresh")
	}
	monitor.finish(request, generation, appMonitorActivityResult(false, true), now.Add(14*time.Second))
	if got := monitor.nextDue(now.Add(14 * time.Second)); !got.Equal(now.Add(16 * time.Second)) {
		t.Fatalf("waiting deadline %v", got)
	}
	request, generation, ok = monitor.request(targets, now.Add(30*time.Second))
	if !ok || !monitor.discovering {
		t.Fatal("periodic inventory was lost behind status updates")
	}
	monitor.finish(request, generation, appMonitorActivityResult(false, false), now.Add(30*time.Second))
	if got := monitor.nextDue(now.Add(30 * time.Second)); !got.Equal(now.Add(42 * time.Second)) {
		t.Fatalf("idle did not back off: %v", got)
	}
}

func TestAgentEventsWakeImmediatelyAndRejectStaleDiscovery(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1}}
	var monitor developmentAppMonitor
	request, generation, _ := monitor.request(targets, now)
	monitor.finish(request, generation, appMonitorActivityResult(false, false), now)
	monitor.requestStatusRefresh(now.Add(time.Second))
	request, generation, ok := monitor.request(targets, now.Add(time.Second))
	if !ok || monitor.discovering {
		t.Fatal("known registry event must refresh only status")
	}
	monitor.markDiscoveryDirty()
	if !monitor.readings["one"].Known {
		t.Fatal("native inventory hint erased current labels")
	}
	monitor.finish(request, generation, developmentAppScan{}, now.Add(2*time.Second))
	if !monitor.readings["one"].Known {
		t.Fatal("stale worker erased known labels")
	}
	request, generation, ok = monitor.request(targets, now.Add(2*time.Second))
	if !ok || !monitor.discovering {
		t.Fatal("inventory event not serviced after old worker")
	}
	monitor.finish(request, generation, appMonitorActivityResult(false, false), now.Add(2*time.Second))
	if got := monitor.nextDue(now.Add(2 * time.Second)); !got.Equal(now.Add(4 * time.Second)) {
		t.Fatalf("post-event burst deadline %v", got)
	}
	if got := monitor.nextDue(now.Add(14 * time.Second)); !got.Equal(now.Add(14 * time.Second)) {
		t.Fatalf("event burst failed to return to idle cadence: %v", got)
	}
}

func TestAgentSchedulerNeverOverlapsAndKeepsEventsDuringRead(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1}}
	var monitor developmentAppMonitor
	request, generation, _ := monitor.request(targets, now)
	if due := monitor.nextDue(now); !due.IsZero() {
		t.Fatal("in-flight operation should own the next wake")
	}
	monitor.requestStatusRefresh(now.Add(10 * time.Millisecond))
	monitor.finish(request, generation, appMonitorActivityResult(false, false), now.Add(20*time.Millisecond))
	if due := monitor.nextDue(now.Add(20 * time.Millisecond)); !due.Equal(now.Add(developmentAgentMinimumInterval)) {
		t.Fatalf("event lost or storm unbounded: %v", due)
	}
	request, generation, ok := monitor.request(targets, now.Add(developmentAgentMinimumInterval))
	if !ok || monitor.discovering {
		t.Fatal("event during worker was lost")
	}
	result := appMonitorActivityResult(false, false)
	result.processes = nil
	result.discoveryNeeded = true
	monitor.finish(request, generation, result, now.Add(developmentAgentMinimumInterval))
	if !monitor.dirty {
		t.Fatal("changed/exited process did not request discovery")
	}
	monitor.setEnabled(false)
	if due := monitor.nextDue(now.Add(time.Hour)); !due.IsZero() {
		t.Fatal("disabled monitor still scheduled work")
	}
}

func TestUnassociatedProcessesDoNotCreateFastActivityPolling(t *testing.T) {
	now := time.Unix(100, 0)
	targets := map[string]developmentAppTarget{"one": {WindowNumber: 1}}
	var monitor developmentAppMonitor
	request, generation, _ := monitor.request(targets, now)
	result := appMonitorActivityResult(false, false)
	result.Sites["one"] = []DevelopmentApp{{Name: "ChatGPT", BundleID: "com.openai.codex"}}
	monitor.finish(request, generation, result, now)
	if due := monitor.nextDue(now); !due.Equal(now.Add(developmentAppDiscoveryInterval)) {
		t.Fatalf("unassociated standalone/helper process scheduled activity polls: %v", due)
	}
}
