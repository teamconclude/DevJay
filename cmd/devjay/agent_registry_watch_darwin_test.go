package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryChangesDistinguishKnownStatusAndNewProcess(t *testing.T) {
	for _, test := range []struct {
		path   string
		rescan bool
		want   uint32
	}{
		{"/registry/123.json", false, developmentRegistryStatus},
		{"/registry/124.json", false, developmentRegistryStatus | developmentRegistryDiscovery},
		{"/registry/1.json", false, 0},
		{"/registry/text.json", false, 0},
		{"/registry/tmp-file", false, 0},
		{"/registry/nested/123.json", false, 0},
		{"/other/123.json", false, 0},
		{"/registry", false, developmentRegistryStatus | developmentRegistryDiscovery},
		{"", true, developmentRegistryStatus | developmentRegistryDiscovery},
	} {
		if got := registryChangeFlags("/registry", map[int]bool{123: true}, test.path, test.rescan); got != test.want {
			t.Fatalf("%q rescan=%v: %d want %d", test.path, test.rescan, got, test.want)
		}
	}
}

func TestRegistryEventsCoalesceWithoutDroppingDiscovery(t *testing.T) {
	var monitor developmentAppMonitor
	monitor.watchMask = &atomic.Uint32{}
	monitor.watchMask.Or(developmentRegistryStatus)
	monitor.watchMask.Or(developmentRegistryStatus | developmentRegistryDiscovery)
	now := time.Unix(100, 0)
	monitor.filesystemChanged(now)
	if !monitor.statusDirty || !monitor.dirty || !monitor.burstUntil.Equal(now.Add(developmentAgentBurstDuration)) {
		t.Fatal("filesystem event did not schedule status and discovery")
	}
	generation := monitor.generation
	monitor.filesystemChanged(now.Add(time.Second))
	if monitor.generation != generation || !monitor.burstUntil.Equal(now.Add(developmentAgentBurstDuration)) {
		t.Fatal("empty coalesced event restarted the burst")
	}
}

func TestRegistryWatchDeliversChangesAndStopsWhenDisabled(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "12345.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	var monitor developmentAppMonitor
	monitor.processes = []observedAgentProcess{{Name: "Claude Code", PID: 12345}}
	monitor.startRegistryWatchDirectory(context.Background(), directory)
	defer monitor.stopRegistryWatch()
	if monitor.changes == nil {
		t.Skip("filesystem observation unavailable")
	}
	if err := os.WriteFile(path, []byte("{ }"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case <-monitor.changes:
		monitor.filesystemChanged(time.Now())
		if !monitor.statusDirty {
			t.Fatal("registry file event did not wake activity observation")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("registry change was not delivered")
	}
	monitor.setEnabled(false)
	if monitor.changes != nil || monitor.watchMask != nil || monitor.watchCancel != nil {
		t.Fatal("disabled status detection retained its filesystem observer")
	}
}
