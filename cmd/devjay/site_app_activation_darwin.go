package main

/*
#include <stdlib.h>
int devjay_activate_development_app(int pid, const char *bundle);
*/
import "C"

import (
	"encoding/json"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Both icon clicks and context-menu selection use this manager-queue path.
// Activation only targets an already-running observed app; it never launches
// another instance or opens a folder/file. macOS chooses that app's window.
func (m *siteManager) activateDevelopmentApp(req appActivateRequest, sender string) {
	if parent, _ := m.cardMenuSource(cardMenuRequest{SiteID: req.SiteID}, sender); parent == nil {
		return
	}
	app, ok := m.requestedDevelopmentApp(req.SiteID, req.BundleID)
	message := "This app is no longer available for the Site. Check the updated card."
	if ok {
		bundle := C.CString(app.BundleID)
		defer C.free(unsafe.Pointer(bundle))
		application.InvokeSync(func() { ok = C.devjay_activate_development_app(C.int(app.ProcessIDs[0]), bundle) != 0 })
		if ok {
			message = ""
		} else {
			message = "Could not show " + app.Name + ". It may have closed; check the updated card."
		}
	}
	if !ok {
		m.developmentApps.markDiscoveryDirty()
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
