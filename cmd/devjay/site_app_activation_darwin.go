package main

/*
#include <stdlib.h>
int devjay_development_app_available(int pid, const char *bundle);
int devjay_activate_development_app_on_site(void *window, unsigned int number, int pid, const char *bundle);
*/
import "C"

import (
	"encoding/json"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Both icon clicks and context-menu selection use this manager-queue path.
// Switch to the Site first, then hand focus to its already-running app once
// AppKit reports that desktop active. Never block the manager or AppKit thread.
func (m *siteManager) activateDevelopmentApp(req appActivateRequest, sender string) {
	parent, site := m.cardMenuSource(cardMenuRequest{SiteID: req.SiteID}, sender)
	if parent == nil || site.Window == nil {
		return
	}
	m.activateDevelopmentAppForSite(req, sender, site)
}

// Shared by validated card events and the native Claude request token path.
func (m *siteManager) activateDevelopmentAppForSite(req appActivateRequest, sender string, site *siteRuntime) {
	m.appActivationGeneration++
	generation := m.appActivationGeneration
	app, ok := m.requestedDevelopmentApp(req.SiteID, req.BundleID)
	if ok {
		bundle := C.CString(app.BundleID)
		application.InvokeSync(func() { ok = C.devjay_development_app_available(C.int(app.ProcessIDs[0]), bundle) != 0 })
		C.free(unsafe.Pointer(bundle))
	}
	if !ok {
		m.finishDevelopmentAppActivation(req, sender, "This app is no longer available for the Site. Check the updated card.")
		return
	}
	target := m.developmentApps.targets[req.SiteID]
	focusSite(site.Window)
	navigation := windowNavigationGeneration.Load()
	deadline := time.Now().Add(2 * time.Second)
	m.broadcast()
	var handoff func()
	handoff = func() {
		if generation != m.appActivationGeneration || navigation != windowNavigationGeneration.Load() ||
			m.runtime[req.SiteID] != site || !site.Ready || m.recovery.paused {
			return
		}
		if !time.Now().Before(deadline) {
			m.finishDevelopmentAppActivation(req, sender, "Could not switch to this Site’s desktop. Select the Site and try again.")
			return
		}
		current, available := m.requestedDevelopmentApp(req.SiteID, req.BundleID)
		if !available || current.ProcessIDs[0] != app.ProcessIDs[0] || m.developmentApps.targets[req.SiteID] != target {
			m.finishDevelopmentAppActivation(req, sender, "This app or Site changed while switching desktops. Check the updated card.")
			return
		}
		bundle := C.CString(app.BundleID)
		var result C.int
		application.InvokeSync(func() {
			result = C.devjay_activate_development_app_on_site(site.Window.NativeWindow(), C.uint(target.WindowNumber), C.int(app.ProcessIDs[0]), bundle)
		})
		C.free(unsafe.Pointer(bundle))
		switch result {
		case 1:
			m.finishDevelopmentAppActivation(req, sender, "")
			m.broadcast()
		case 0:
			time.AfterFunc(60*time.Millisecond, func() { m.send(handoff) })
		case -2:
			// Another user action took focus; do not steal it back or show an error.
		default:
			m.finishDevelopmentAppActivation(req, sender, "Could not show "+app.Name+". It may have closed; check the updated card.")
		}
	}
	// Let the explicit activation request reach AppKit before checking focus.
	time.AfterFunc(60*time.Millisecond, func() { m.send(handoff) })
}

func (m *siteManager) finishDevelopmentAppActivation(req appActivateRequest, sender, message string) {
	if message != "" {
		m.developmentApps.markDiscoveryDirty()
	}
	if sender == "claude-permission" {
		if message != "" {
			showMessageDialog(m.strip, nil, "Could not go to Claude", message)
		}
		return
	}
	m.app.Event.Emit("devjay:app-activate-result", map[string]string{"siteID": req.SiteID, "sender": sender, "error": message})
}

func (m *siteManager) registerDevelopmentAppEvents() {
	m.app.Event.On("devjay:app-activate", func(e *application.CustomEvent) {
		var req appActivateRequest
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil || !validAppBundleID(req.BundleID) {
			return
		}
		sender := e.Sender
		m.send(func() { m.activateDevelopmentApp(req, sender) })
	})
}
