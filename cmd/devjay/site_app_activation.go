package main

type appActivateRequest struct {
	SiteID   string `json:"siteID"`
	BundleID string `json:"bundleID"`
}

// Names and process identities come from the observer, never from UI labels.
// Refuse unknown/stale Site placement and ambiguous running app instances.
func (m *siteManager) requestedDevelopmentApp(siteID, bundleID string) (observedDevelopmentApp, bool) {
	monitor := &m.developmentApps
	reading, ok := monitor.readings[siteID]
	if !validAppBundleID(bundleID) || !m.config.showIDEStatus() || monitor.disabled || !ok || !reading.Known ||
		reading.Target.WindowNumber == 0 || reading.Target != monitor.targets[siteID] || !monitor.windows.Available {
		return observedDevelopmentApp{}, false
	}
	for _, app := range monitor.windows.Sites[siteID] {
		if app.BundleID == bundleID && app.Name != "" && len(app.ProcessIDs) == 1 && app.ProcessIDs[0] > 0 {
			for _, current := range reading.Apps {
				if current.BundleID == bundleID {
					return app, true
				}
			}
		}
	}
	return observedDevelopmentApp{}, false
}
