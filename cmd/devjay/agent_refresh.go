package main

import "slices"

// Defense in depth for native refreshes: only an exact cached process identity
// and association can feed a cheap status read. Discovery establishes new ones.
func verifiedRefreshedAgents(known, refreshed []observedAgentProcess) []observedAgentProcess {
	previous := make(map[int]observedAgentProcess, len(known))
	for _, process := range known {
		previous[process.PID] = process
	}
	var result []observedAgentProcess
	seen := make(map[int]bool)
	for _, process := range refreshed {
		old, ok := previous[process.PID]
		if !ok || seen[process.PID] || process.PID <= 1 || process.Started == 0 ||
			process.Name != old.Name || process.Started != old.Started || process.Cwd != old.Cwd ||
			process.HostBundleID != old.HostBundleID || !slices.Equal(process.Ancestors, old.Ancestors) {
			continue
		}
		process.Waiting, process.Working = false, false
		result = append(result, process)
		seen[process.PID] = true
	}
	return result
}
