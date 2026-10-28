package main

import (
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const nativeMonitorFallback = 30 * time.Second

// Owned by the manager queue. Agent/Git publications reuse this native cache;
// only window events and the slow safety sweep invalidate it.
type monitorSchedule struct {
	windowsValid, visibilityDirty bool
	positions                     map[string]DesktopPosition
	states                        map[string]cardState
	preferences                   map[string]bool
	windowNumbers                 map[string]uint32
	settingsOpen                  bool
	lastInteraction               time.Time
	nativeDue, settleDue          time.Time
	settleRemaining               int
	visibility                    map[string]bool
	sent                          map[string]string
}

func earlierDeadline(a, b time.Time) time.Time {
	if a.IsZero() || (!b.IsZero() && b.Before(a)) {
		return b
	}
	return a
}

func (m *siteManager) ensureWindowSnapshot(items map[string]siteRuntime) {
	if !m.monitor.windowsValid {
		var strip, settings *application.WebviewWindow
		if m.stripReady {
			strip = m.strip
		}
		if m.settingsReady {
			settings = m.settings
		}
		m.monitor.positions, m.monitor.states, m.monitor.preferences, m.monitor.settingsOpen, m.monitor.windowNumbers = readWindowSnapshot(items, strip, settings)
		m.monitor.windowsValid = true
		m.monitor.visibilityDirty = true
		m.monitor.nativeDue = time.Now().Add(nativeMonitorFallback)
		m.updateGitWatchActivity()
	}
}

func (m *siteManager) updateGitWatchActivity() {
	for id, site := range m.runtime {
		if site.GitControl != nil {
			site.GitControl.setActivity(m.monitor.states[id].OnDesktop, m.recovery.paused)
			site.GitControl.setLastInteraction(m.monitor.lastInteraction)
		}
	}
}

// Called only on the manager queue. All enabled Sites appear in the strip,
// including those on other desktops. Visibility/snapshots alone are not input.
func (m *siteManager) noteInteraction(now time.Time) {
	if m.recovery.paused {
		return
	}
	m.monitor.lastInteraction = now
	m.updateGitWatchActivity()
}

// AppKit menu tracking bypasses the local input monitor. Wrap explicit menu
// and reopen commands before enqueueing them, so activity and the action both
// run on the manager queue. Constructing a callback must not renew activity.
func (m *siteManager) userCommand(fn func()) func() {
	return func() {
		m.noteInteraction(time.Now())
		fn()
	}
}

func (m *siteManager) handleNativeMonitorEvents(flags nativeMonitorFlags, now time.Time) {
	if flags&nativeMonitorInteraction != 0 {
		m.noteInteraction(now)
		flags &^= nativeMonitorInteraction
	}
	if flags&nativeMonitorDisplays != 0 {
		m.profiles.due = now.Add(2 * time.Second)
	}
	if flags == 0 {
		return
	}
	if flags&nativeMonitorPower != 0 {
		m.recoverViews()
		m.profiles.due = now.Add(2 * time.Second)
	}
	if flags&(nativeMonitorSpace|nativeMonitorPlacement) != 0 {
		// The notification can arrive before AppKit finishes the transition.
		// Two bounded follow-ups cover that settling period without polling.
		if m.monitor.settleDue.IsZero() || flags&nativeMonitorSpace != 0 {
			m.monitor.settleDue = now.Add(120 * time.Millisecond)
		}
		m.monitor.settleRemaining = 2
	}
	// Live dragging may send hundreds of placement notifications. The bounded
	// settling deadline samples that motion; actual Space changes stay immediate.
	if flags&nativeMonitorPlacement != 0 && flags&(nativeMonitorSpace|nativeMonitorApplications|nativeMonitorPower) == 0 {
		return
	}
	m.monitor.windowsValid = false
	m.monitor.visibilityDirty = true
	if flags&(nativeMonitorApplications|nativeMonitorSpace) != 0 {
		m.developmentApps.markDiscoveryDirty()
		m.developmentApps.requestStatusRefresh(now)
	}
	m.updateGitWatchActivity()
	if m.recovery.paused {
		m.refreshWindowVisibility()
	} else {
		m.publishSnapshot(false)
	}
	m.refreshViewers(now)
}

func (m *siteManager) pollMonitoring(now time.Time) {
	m.pollProfiles(now)
	m.pollFolders(now)
	if m.monitor.nativeDue.IsZero() || !now.Before(m.monitor.nativeDue) {
		m.recoverViews()
		m.monitor.windowsValid = false
		m.monitor.visibilityDirty = true
		m.monitor.nativeDue = now.Add(nativeMonitorFallback)
	}
	if !m.monitor.settleDue.IsZero() && !now.Before(m.monitor.settleDue) {
		m.monitor.windowsValid = false
		m.monitor.visibilityDirty = true
		m.monitor.settleRemaining--
		m.monitor.settleDue = time.Time{}
		if m.monitor.settleRemaining > 0 {
			m.monitor.settleDue = now.Add(300 * time.Millisecond)
		}
	}
	m.updateGitWatchActivity()
	if m.recovery.paused {
		m.refreshWindowVisibility()
	} else {
		m.publishSnapshot(false)
	}
	m.refreshViewers(now)
}

func (m *siteManager) nextMonitorDelay(now time.Time) time.Duration {
	due := m.monitor.nativeDue
	if due.IsZero() {
		due = now
	}
	if !m.recovery.paused {
		due = earlierDeadline(due, m.profiles.due)
		if !m.folders.busy && m.configError == nil {
			if m.folders.due.IsZero() {
				due = now
			} else {
				due = earlierDeadline(due, m.folders.due)
			}
		}
		due = earlierDeadline(due, m.monitor.settleDue)
		due = earlierDeadline(due, m.developmentApps.nextDue(now))
		due = earlierDeadline(due, m.recovery.nextDue())
		due = earlierDeadline(due, m.nextViewerRefresh(now))
	}
	return max(20*time.Millisecond, due.Sub(now))
}

// Wails Window.EmitEvent also broadcasts globally. Dispatch directly to the
// manager-owned window to avoid waking every off-Space WebView.
func dispatchWindowEvent(window *application.WebviewWindow, name string, data any) {
	if window != nil {
		window.DispatchWailsEvent(&application.CustomEvent{Name: name, Data: data})
	}
}

func (m *siteManager) refreshWindowVisibility() {
	if !m.monitor.visibilityDirty {
		return
	}
	m.monitor.visibilityDirty = false
	visible := map[string]bool{}
	if !m.recovery.paused {
		visible = readNativeWindowVisibility(m.recoveryWindows)
	}
	if m.monitor.visibility == nil {
		m.monitor.visibility = map[string]bool{}
	}
	for id, window := range m.recoveryWindows {
		value := visible[id]
		previous, known := m.monitor.visibility[id]
		if known && previous == value {
			continue
		}
		m.monitor.visibility[id] = value
		dispatchWindowEvent(window, "devjay:visibility", value)
		if value {
			delete(m.monitor.sent, id)
			if viewer := m.viewers[id]; viewer != nil {
				viewer.refresh.pending = true
				m.emitViewer(id)
			}
		}
	}
}

func snapshotForWindow(snapshot appSnapshot, id, viewerSite string) appSnapshot {
	if id == "settings" && snapshot.Catalog != nil {
		snapshot.Sites = snapshot.Catalog
	}
	snapshot.Catalog = nil
	if id != "settings" {
		snapshot.Profiles = ProfilesView{}
		snapshot.ShortcutSettings = nil
	}
	if id != "strip" {
		snapshot.ShortcutLabels = nil
		snapshot.ShortcutHints = 0 // Only the full strip renders modifier hints.
	}
	site := viewerSite
	if strings.HasPrefix(id, "site-") {
		site = strings.TrimPrefix(id, "site-")
	}
	if site != "" {
		snapshot.Sites = snapshotSitesForID(snapshot, site)
	}
	return snapshot
}

func snapshotSitesForID(snapshot appSnapshot, id string) []SiteView {
	for _, site := range snapshot.Sites {
		if site.ID == id {
			return []SiteView{site}
		}
	}
	return []SiteView{}
}

func (m *siteManager) deliverSnapshot(snapshot appSnapshot, force bool) {
	if m.monitor.sent == nil {
		m.monitor.sent = map[string]string{}
	}
	for id, window := range m.recoveryWindows {
		if !force && !m.monitor.visibility[id] && m.monitor.sent[id] != "" {
			continue
		}
		viewerSite := ""
		if viewer := m.viewers[id]; viewer != nil && viewer.state != nil {
			viewerSite = viewer.state.SiteID
		}
		filtered := m.actionSnapshot(snapshotForWindow(snapshot, id, viewerSite), id, viewerSite)
		signature := snapshotSignature(filtered)
		if !force && m.monitor.sent[id] == signature {
			continue
		}
		m.monitor.sent[id] = signature
		dispatchWindowEvent(window, "devjay:snapshot", filtered)
	}
}
