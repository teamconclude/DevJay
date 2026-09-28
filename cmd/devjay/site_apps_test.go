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
	if _, _, ok := monitor.request(targets, now.Add(10*time.Second)); ok {
		t.Fatal("interval should start after the preceding scan finishes")
	}
	if _, _, ok := monitor.request(targets, now.Add(11*time.Second)); !ok {
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
		request, generation, _ = monitor.request(targets, now.Add(10*time.Second))
		targets["one"] = replacement
		if _, _, ok := monitor.request(targets, now.Add(11*time.Second)); ok {
			t.Fatal("cannot overlap stale in-flight scan")
		}
		if monitor.readings["one"].Known {
			t.Fatal("must hide the old desktop's app immediately")
		}
		monitor.finish(request, generation, result, now.Add(11*time.Second))
		if monitor.readings["one"].Known {
			t.Fatal("stale result restored the wrong app")
		}
		if _, _, ok := monitor.request(targets, now.Add(12*time.Second)); !ok {
			t.Fatal("moved Site should refresh without waiting ten seconds")
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
	request, generation, _ = monitor.request(targets, now.Add(10*time.Second))
	monitor.invalidate()
	monitor.finish(request, generation, developmentAppScan{Available: true, Sites: map[string][]DevelopmentApp{"one": {{Name: "Old app"}}}}, now.Add(20*time.Second))
	if monitor.readings["one"].Known {
		t.Fatal("scan started before sleep must not survive invalidation")
	}
	if _, _, ok := monitor.request(targets, now.Add(20*time.Second)); !ok {
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
	request, generation, _ = monitor.request(targets, now.Add(10*time.Second))
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
