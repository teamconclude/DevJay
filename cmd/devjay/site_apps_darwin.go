package main

/*
#cgo LDFLAGS: -framework CoreGraphics
#include <stdlib.h>
#include "site_apps_darwin.h"
#include "agent_processes_darwin.h"
*/
import "C"

import (
	"encoding/json"
	"time"
	"unsafe"
)

func scanDevelopmentApps(targets map[string]developmentAppTarget) developmentAppScan {
	windows := make(map[string]uint32, len(targets))
	for id, target := range targets {
		windows[id] = target.WindowNumber
	}
	input, _ := json.Marshal(windows)
	value := C.CString(string(input))
	defer C.free(unsafe.Pointer(value))
	raw := C.devjay_site_apps(value)
	if raw == nil {
		return developmentAppScan{}
	}
	defer C.free(unsafe.Pointer(raw))
	var result developmentAppWindowScan
	if err := json.Unmarshal([]byte(C.GoString(raw)), &result); err != nil {
		return developmentAppScan{}
	}
	var agents []observedAgentProcess
	if result.Available && len(result.Sites) != 0 {
		processes := C.devjay_agent_processes()
		if processes != nil {
			_ = json.Unmarshal([]byte(C.GoString(processes)), &agents)
			C.free(unsafe.Pointer(processes))
		}
	}
	return matchDevelopmentAgents(targets, result, agents)
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
	go func() {
		result := scanDevelopmentApps(request)
		m.send(func() {
			m.developmentApps.finish(request, generation, result, time.Now())
			m.publishSnapshot(false)
		})
	}()
}
