package main

import (
	"maps"
	"sync/atomic"
	"time"
)

// These are observed windows on a Site's desktop, not proof that an app has
// opened the Site's folder. File-opening preferences remain separate.
type DevelopmentApp struct {
	ClaudePermissions int      `json:"claudePermissions,omitempty"`
	Name              string   `json:"name"`
	BundleID          string   `json:"bundleID"`
	Icon              string   `json:"icon,omitempty"`
	WindowCount       int      `json:"windowCount,omitempty"`
	Agents            []string `json:"agents,omitempty"`
	WaitingAgents     []string `json:"waitingAgents,omitempty"`
	WorkingAgents     []string `json:"workingAgents,omitempty"`
}

type developmentAppScan struct {
	Available       bool                        `json:"available"`
	Sites           map[string][]DevelopmentApp `json:"sites"`
	windows         developmentAppWindowScan
	processes       []observedAgentProcess
	discoveryNeeded bool
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

const (
	developmentAppDiscoveryInterval = 30 * time.Second
	developmentAgentIdleInterval    = 12 * time.Second
	developmentAgentActiveInterval  = 2 * time.Second
	developmentAgentMinimumInterval = 250 * time.Millisecond
	developmentAgentBurstDuration   = 12 * time.Second
)

// Owned by the manager command queue. Window/process discovery is independent
// from cheap activity reads. Only the filesystem watcher's mask is atomic;
// events wake the queue through a coalesced channel, never mutate this state.
type developmentAppMonitor struct {
	targets                                                                map[string]developmentAppTarget
	readings                                                               map[string]developmentAppReading
	generation                                                             uint64
	inFlight, dirty, statusDirty, discovering, disabled, active, hasAgents bool
	started, completed, discovered, burstUntil                             time.Time
	windows                                                                developmentAppWindowScan
	processes                                                              []observedAgentProcess
	changes                                                                <-chan struct{}
	watchCancel                                                            func()
	watchMask                                                              *atomic.Uint32
	watchRoot                                                              string
}

func (s *developmentAppMonitor) hasWindows() bool {
	for _, target := range s.targets {
		if target.WindowNumber != 0 {
			return true
		}
	}
	return false
}

// Zero means no timer is needed. In-flight completion and filesystem/native
// events wake the manager separately, rather than requiring a one-second tick.
func (s *developmentAppMonitor) nextDue(now time.Time) time.Time {
	if s.disabled || s.inFlight || !s.hasWindows() {
		return time.Time{}
	}
	due := s.discovered.Add(developmentAppDiscoveryInterval)
	if s.dirty || s.discovered.IsZero() {
		due = now
	}
	if s.windows.Available && s.hasAgents && len(s.processes) != 0 {
		interval := developmentAgentIdleInterval
		if s.active || now.Before(s.burstUntil) {
			interval = developmentAgentActiveInterval
		}
		status := s.completed.Add(interval)
		if s.statusDirty || s.completed.IsZero() {
			status = now
		}
		if status.Before(due) {
			due = status
		}
	}
	if minimum := s.started.Add(developmentAgentMinimumInterval); !s.started.IsZero() && due.Before(minimum) {
		due = minimum
	}
	return due
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
	due := s.nextDue(now)
	if due.IsZero() || now.Before(due) {
		return nil, 0, false
	}
	s.discovering = s.dirty || s.discovered.IsZero() || !now.Before(s.discovered.Add(developmentAppDiscoveryInterval))
	s.inFlight, s.statusDirty, s.started = true, false, now
	if s.discovering {
		s.dirty = false
	}
	return maps.Clone(targets), s.generation, true
}

func (s *developmentAppMonitor) finish(targets map[string]developmentAppTarget, generation uint64, result developmentAppScan, now time.Time) {
	s.inFlight = false
	if generation != s.generation {
		return
	}
	s.completed = now
	if s.discovering {
		s.discovered, s.windows = now, result.windows
	}
	s.processes = result.processes
	s.active, s.hasAgents = false, false
	s.readings = make(map[string]developmentAppReading, len(targets))
	for id, target := range targets {
		apps, known := result.Sites[id]
		known = known && result.Available
		if !known {
			apps = nil
		}
		for _, app := range apps {
			s.hasAgents = s.hasAgents || len(app.Agents) != 0
			s.active = s.active || len(app.WaitingAgents) != 0 || len(app.WorkingAgents) != 0
		}
		s.readings[id] = developmentAppReading{Target: target, Apps: apps, Known: known}
	}
	// A vanished/replaced process cannot retain status until the next inventory.
	// Re-discover promptly after clearing it in this reading.
	if result.discoveryNeeded {
		s.dirty = true
	}
}

// Events saying inventory may have changed preserve currently known labels
// until refreshed, but reject a worker started before that event.
func (s *developmentAppMonitor) markDiscoveryDirty() {
	s.generation++
	s.dirty = true
}

func (s *developmentAppMonitor) requestStatusRefresh(now time.Time) {
	s.statusDirty = true
	s.burstUntil = now.Add(developmentAgentBurstDuration)
}

func (s *developmentAppMonitor) invalidateDiscovery() {
	s.markDiscoveryDirty()
	s.completed, s.discovered = time.Time{}, time.Time{}
	s.readings, s.processes = nil, nil
	s.windows = developmentAppWindowScan{}
	s.active, s.hasAgents = false, false
}

func (s *developmentAppMonitor) invalidate() { s.invalidateDiscovery() }

func (s *developmentAppMonitor) setEnabled(enabled bool) {
	if s.disabled == !enabled {
		return
	}
	s.disabled = !enabled
	// Keep the in-flight guard until the worker returns; reject its old result.
	s.invalidateDiscovery()
	if !enabled {
		s.stopRegistryWatch()
	}
}

func (s *developmentAppMonitor) stopRegistryWatch() {
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.watchCancel, s.changes, s.watchMask, s.watchRoot = nil, nil, nil, ""
}

const (
	developmentRegistryStatus uint32 = 1 << iota
	developmentRegistryDiscovery
)

func (s *developmentAppMonitor) filesystemChanged(now time.Time) {
	if s.disabled || s.watchMask == nil {
		return
	}
	flags := s.watchMask.Swap(0)
	if flags == 0 {
		return
	}
	s.requestStatusRefresh(now)
	if flags&developmentRegistryDiscovery != 0 {
		s.markDiscoveryDirty()
	}
}
