package main

import (
	"path/filepath"
	"slices"
	"strings"
)

// Process IDs and working directories are only intermediate observations. They
// are neither persisted nor sent to the frontend.
type observedDevelopmentApp struct {
	DevelopmentApp
	Kind       string `json:"kind"`
	ProcessIDs []int  `json:"processIDs"`
}

type developmentAppWindowScan struct {
	Available bool                                `json:"available"`
	Sites     map[string][]observedDevelopmentApp `json:"sites"`
}

type observedAgentProcess struct {
	Name         string `json:"name"`
	PID          int    `json:"pid"`
	Started      uint64 `json:"started"`
	Cwd          string `json:"cwd"`
	Ancestors    []int  `json:"ancestors"`
	HostBundleID string `json:"hostBundleID"`
	Waiting      bool   `json:"-"`
	Working      bool   `json:"-"`
}

// Called only by the background scan. Resolve symlinks once per distinct path;
// inaccessible/relative paths provide no folder evidence.
func matchDevelopmentAgents(targets map[string]developmentAppTarget, windows developmentAppWindowScan, processes []observedAgentProcess) developmentAppScan {
	result := developmentAppScan{Available: windows.Available, Sites: make(map[string][]DevelopmentApp, len(windows.Sites))}
	for site, apps := range windows.Sites {
		for _, app := range apps {
			result.Sites[site] = append(result.Sites[site], DevelopmentApp{Name: app.Name, BundleID: app.BundleID, Icon: app.Icon, WindowCount: app.WindowCount})
		}
		if len(apps) == 0 {
			result.Sites[site] = nil
		}
	}
	if !windows.Available || len(processes) == 0 {
		return result
	}
	resolved := make(map[string]string)
	canonical := func(path string) string {
		if value, ok := resolved[path]; ok {
			return value
		}
		value := ""
		if filepath.IsAbs(path) {
			if real, err := filepath.EvalSymlinks(path); err == nil {
				value = filepath.Clean(real)
			}
		}
		resolved[path] = value
		return value
	}
	roots := make(map[string]string, len(targets))
	for site, target := range targets {
		if path := canonical(target.SitePath); path != "" {
			roots[site] = path
		}
	}
	for _, process := range processes {
		if process.Name != "Claude Code" && process.Name != "Codex" {
			continue
		}
		cwd := canonical(process.Cwd)
		if cwd == "" {
			continue
		}
		// Select the most specific registered folder BEFORE considering apps.
		// A nested Site must never fall back to its parent's desktop merely
		// because the nested desktop or its host window could not be read.
		site, longest, ambiguous := "", 0, false
		for id, root := range roots {
			if cwd != root && !strings.HasPrefix(cwd, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator)) {
				continue
			}
			if len(root) > longest {
				site, longest, ambiguous = id, len(root), false
			} else if len(root) == longest {
				ambiguous = true
			}
		}
		if site == "" || ambiguous {
			continue
		}
		for index, app := range windows.Sites[site] {
			if app.Kind != "editor" && app.Kind != "terminal" {
				continue
			}
			hosted := false
			for _, pid := range app.ProcessIDs {
				if pid > 0 && slices.Contains(process.Ancestors, pid) {
					hosted = true
					break
				}
			}
			// iTerm's persistent shell server is intentionally independent of
			// its GUI PID. The native reader recognizes only its exact helper
			// location, and we still require an iTerm window on this desktop.
			if process.HostBundleID == "com.googlecode.iterm2" && app.BundleID == process.HostBundleID {
				hosted = true
			}
			if !hosted {
				continue
			}
			visible := &result.Sites[site][index]
			if !slices.Contains(visible.Agents, process.Name) {
				visible.Agents = append(visible.Agents, process.Name)
				slices.Sort(visible.Agents)
			}
			if process.Waiting {
				if !slices.Contains(visible.WaitingAgents, process.Name) {
					visible.WaitingAgents = append(visible.WaitingAgents, process.Name)
					slices.Sort(visible.WaitingAgents)
				}
				// A busy session must not hide a second session that needs input
				// in this same host, regardless of process enumeration order.
				visible.WorkingAgents = slices.DeleteFunc(visible.WorkingAgents, func(name string) bool { return name == process.Name })
			} else if process.Working && !slices.Contains(visible.WaitingAgents, process.Name) && !slices.Contains(visible.WorkingAgents, process.Name) {
				visible.WorkingAgents = append(visible.WorkingAgents, process.Name)
				slices.Sort(visible.WorkingAgents)
			}
		}
	}
	return result
}
