package main

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <stdlib.h>
#include "site_apps_darwin.h"
#include "agent_processes_darwin.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"slices"
	"time"
	"unsafe"
)

func discoverDevelopmentApps(targets map[string]developmentAppTarget) (developmentAppWindowScan, []observedAgentProcess) {
	windows := make(map[string]uint32, len(targets))
	for id, target := range targets {
		windows[id] = target.WindowNumber
	}
	input, _ := json.Marshal(windows)
	value := C.CString(string(input))
	defer C.free(unsafe.Pointer(value))
	raw := C.devjay_site_apps(value)
	if raw == nil {
		return developmentAppWindowScan{}, nil
	}
	defer C.free(unsafe.Pointer(raw))
	var result developmentAppWindowScan
	if json.Unmarshal([]byte(C.GoString(raw)), &result) != nil {
		return developmentAppWindowScan{}, nil
	}
	var agents []observedAgentProcess
	if result.Available && len(result.Sites) != 0 {
		processes := C.devjay_agent_processes()
		if processes != nil {
			_ = json.Unmarshal([]byte(C.GoString(processes)), &agents)
			C.free(unsafe.Pointer(processes))
		}
	}
	return result, agents
}

func refreshKnownDevelopmentAgents(known []observedAgentProcess) []observedAgentProcess {
	if len(known) == 0 {
		return nil
	}
	input, _ := json.Marshal(known)
	value := C.CString(string(input))
	defer C.free(unsafe.Pointer(value))
	raw := C.devjay_refresh_agent_processes(value)
	if raw == nil {
		return nil
	}
	defer C.free(unsafe.Pointer(raw))
	var result []observedAgentProcess
	_ = json.Unmarshal([]byte(C.GoString(raw)), &result)
	return verifiedRefreshedAgents(known, result)
}

func scanDevelopmentApps(ctx context.Context, targets map[string]developmentAppTarget) developmentAppScan {
	windows, agents := discoverDevelopmentApps(targets)
	return readDevelopmentAgentStatus(ctx, targets, windows, agents)
}

func readDevelopmentAgentStatus(ctx context.Context, targets map[string]developmentAppTarget, windows developmentAppWindowScan, agents []observedAgentProcess) developmentAppScan {
	observeAgentWaiting(ctx, targets, windows, agents)
	result := matchDevelopmentAgents(targets, windows, agents)
	result.windows, result.processes = windows, agents
	return result
}

func (m *siteManager) observeDevelopmentApps(windows map[string]uint32, positions map[string]DesktopPosition) {
	m.developmentApps.setEnabled(m.config.showIDEStatus())
	if !m.config.showIDEStatus() {
		return
	}
	targets := developmentAppTargets(windows, positions, m.config.Sites)
	request, generation, start := m.developmentApps.request(targets, time.Now())
	if !start {
		return
	}
	discovery := m.developmentApps.discovering
	inventory := m.developmentApps.windows
	known := slices.Clone(m.developmentApps.processes)
	go func() {
		var result developmentAppScan
		if discovery {
			result = scanDevelopmentApps(m.app.Context(), request)
		} else {
			agents := refreshKnownDevelopmentAgents(known)
			result = readDevelopmentAgentStatus(m.app.Context(), request, inventory, agents)
			result.discoveryNeeded = len(agents) != len(known)
		}
		m.send(func() {
			m.developmentApps.finish(request, generation, result, time.Now())
			if discovery && generation == m.developmentApps.generation && !m.developmentApps.disabled {
				m.developmentApps.startRegistryWatch(m.app.Context())
			}
			m.publishSnapshot(false)
		})
	}()
}
