package main

import (
	"maps"
	"time"
)

// These are observed windows on a Site's desktop, not proof that an app has
// opened the Site's folder. File-opening preferences remain separate.
type DevelopmentApp struct {
	Name        string   `json:"name"`
	BundleID    string   `json:"bundleID"`
	Icon        string   `json:"icon,omitempty"`
	WindowCount int      `json:"windowCount,omitempty"`
	Agents      []string `json:"agents,omitempty"`
}

type developmentAppScan struct {
	Available bool                        `json:"available"`
	Sites     map[string][]DevelopmentApp `json:"sites"`
}

type developmentAppTarget struct {
	WindowNumber       uint32
	MonitorX, MonitorY float64
	Desktop            int
	SitePath           string
}

func developmentAppTargets(windows map[string]uint32, positions map[string]DesktopPosition, sites []Site) map[string]developmentAppTarget {
	paths := make(map[string]string, len(sites))
	for _, site := range sites {
		paths[site.ID] = site.Path
	}
	targets := make(map[string]developmentAppTarget, len(windows))
	for id, number := range windows {
		// NSWindow.screen (and hence Position.Known) can be unavailable on an
		// inactive Space. The native scanner resolves its own Space and display
		// from this window ID; do not exclude a usable ID based on AppKit state.
		if number == 0 {
			continue
		}
		position := positions[id]
		targets[id] = developmentAppTarget{WindowNumber: number, MonitorX: position.MonitorX, MonitorY: position.MonitorY, Desktop: position.Desktop, SitePath: paths[id]}
	}
	// Keep folder scope even when a Site has no readable native window yet.
	// Its sessions must not be attributed to a less-specific parent Site.
	for _, site := range sites {
		if _, ok := targets[site.ID]; !ok {
			targets[site.ID] = developmentAppTarget{SitePath: site.Path}
		}
	}
	return targets
}

type developmentAppReading struct {
	Target developmentAppTarget
	Apps   []DevelopmentApp
	Known  bool
}

// Owned by the manager command queue. One scan serves every Site, at most once
// every ten seconds while unchanged. A moved/new Site refreshes sooner, with a
// two-second minimum spacing to avoid repeated scans during window dragging.
type developmentAppMonitor struct {
	targets            map[string]developmentAppTarget
	readings           map[string]developmentAppReading
	generation         uint64
	inFlight, dirty    bool
	disabled           bool
	started, completed time.Time
}

func (s *developmentAppMonitor) request(targets map[string]developmentAppTarget, now time.Time) (map[string]developmentAppTarget, uint64, bool) {
	if s.disabled {
		return nil, 0, false
	}
	if !maps.Equal(s.targets, targets) {
		s.targets = maps.Clone(targets)
		s.generation++
		s.dirty = true
		for id, reading := range s.readings {
			if target, ok := targets[id]; !ok || target != reading.Target {
				delete(s.readings, id)
			}
		}
	}
	hasWindows := false
	for _, target := range targets {
		hasWindows = hasWindows || target.WindowNumber != 0
	}
	if s.inFlight || !hasWindows ||
		(!s.started.IsZero() && now.Sub(s.started) < 2*time.Second) ||
		(!s.dirty && !s.completed.IsZero() && now.Sub(s.completed) < 10*time.Second) {
		return nil, 0, false
	}
	s.inFlight, s.dirty, s.started = true, false, now
	return maps.Clone(targets), s.generation, true
}

func (s *developmentAppMonitor) finish(targets map[string]developmentAppTarget, generation uint64, result developmentAppScan, now time.Time) {
	s.inFlight = false
	if generation != s.generation {
		return
	}
	s.completed = now
	s.readings = make(map[string]developmentAppReading, len(targets))
	for id, target := range targets {
		apps, known := result.Sites[id]
		known = known && result.Available
		if !known {
			apps = nil
		}
		s.readings[id] = developmentAppReading{Target: target, Apps: apps, Known: known}
	}
}

func (s *developmentAppMonitor) invalidate() {
	s.generation++
	s.dirty = true
	s.completed = time.Time{}
	s.readings = nil
}

func (s *developmentAppMonitor) setEnabled(enabled bool) {
	if s.disabled == !enabled {
		return
	}
	s.disabled = !enabled
	// Keep the in-flight guard until the worker returns, but reject that old
	// result even if the user enables detection again before it finishes.
	s.invalidate()
}
