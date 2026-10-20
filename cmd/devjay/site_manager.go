package main

import (
	"context"
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type siteRuntime struct {
	FolderIdentity string
	Site           Site
	Window         *application.WebviewWindow
	Ready          bool
	Git            GitStatus
	Cancel         context.CancelFunc
	GitControl     *gitWatchControl
}
type SiteView struct {
	NeedsPlacement bool `json:"needsPlacement,omitempty"`
	Enabled        bool `json:"enabled"`
	FolderMissing  bool `json:"folderMissing,omitempty"`
	Site
	Prefix               string           `json:"prefix"`
	Name                 string           `json:"name"`
	Window               cardState        `json:"window"`
	Position             DesktopPosition  `json:"position"`
	Git                  GitStatus        `json:"git"`
	DevelopmentApps      []DevelopmentApp `json:"developmentApps"`
	DevelopmentAppsKnown bool             `json:"developmentAppsKnown"`
}
type appSnapshot struct {
	ShortcutSettings *ShortcutSettingsView    `json:"shortcutSettings,omitempty"`
	ShortcutLabels   []string                 `json:"shortcutLabels,omitempty"`
	Profiles         ProfilesView             `json:"profiles"`
	Catalog          []SiteView               `json:"catalog,omitempty"`
	StripMode        stripMode                `json:"stripMode"`
	ShortcutError    string                   `json:"shortcutError,omitempty"`
	ShortcutHints    uint32                   `json:"shortcutHints"`
	Action           *CheckoutAction          `json:"action,omitempty"`
	QuitMessage      string                   `json:"quitMessage,omitempty"`
	SuggestedSite    string                   `json:"suggestedSite,omitempty"`
	Layout           string                   `json:"layout"`
	Placements       map[string]CardPlacement `json:"placements"`
	SettingsOpen     bool                     `json:"settingsOpen"`
	Decoration       string                   `json:"decoration"`
	Sites            []SiteView               `json:"sites"`
	Manual           bool                     `json:"manual"`
	Preferences      map[string]bool          `json:"preferences"`
	Error            string                   `json:"error"`
}
type siteManager struct {
	shortcutFailed                                           uint32
	shortcutRecording, shortcutMessage                       string
	shortcutRecordingToken                                   uint64
	settingsPage                                             string
	addAfterID                                               string
	profiles                                                 profileMonitor
	folders                                                  folderMonitor
	stripRestoreMode                                         stripMode
	siteWindowsItem                                          *application.MenuItem
	stripMode                                                stripMode
	compactSignature                                         string
	shortcutError                                            string
	shortcutHints                                            uint32
	minimalItem, hiddenItem                                  *application.MenuItem
	lifecycle                                                actionLifetime
	quitMessage                                              string
	removing                                                 bool
	recovery                                                 uiRecovery
	recoveryWindows                                          map[string]*application.WebviewWindow
	editorState                                              *EditorState
	editorGeneration                                         uint64
	editorBrowsing                                           bool
	developmentApps                                          developmentAppMonitor
	appActivationGeneration                                  uint64
	viewers                                                  map[string]*gitViewerWindow
	viewerSequence                                           uint64
	prOpening                                                bool
	suggestionsAnchor                                        *cardAnchor
	suggestions, suggestionsParent                           *application.WebviewWindow
	suggestionsReady                                         bool
	suggestedSite                                            string
	action                                                   *CheckoutAction
	actionCancel                                             context.CancelFunc
	actionGeneration                                         uint64
	app                                                      *application.App
	strip, settings                                          *application.WebviewWindow
	stripReady, settingsReady, openSettingsPending, browsing bool
	allDesktopsItem, onTopItem                               *application.MenuItem
	mu                                                       sync.Mutex // Protect runtime membership against the shutdown callback.
	config                                                   SiteConfig
	configError                                              error
	runtime                                                  map[string]*siteRuntime
	commands                                                 chan func()
	monitor                                                  monitorSchedule
}

func newSiteManager(app *application.App, c SiteConfig, err error) *siteManager {
	if err != nil {
		c = SiteConfig{Version: 1, Sites: []Site{}}
	}
	return &siteManager{app: app, config: c, configError: err, runtime: map[string]*siteRuntime{}, commands: make(chan func(), 128)}
}
func (m *siteManager) send(fn func()) {
	select {
	case <-m.app.Context().Done():
		return
	case m.commands <- fn:
	}
}
func (m *siteManager) run() {
	// Install the existing power observer before the wake bridge. Native
	// callbacks only wake this queue; none wait on it from the AppKit thread.
	m.applySiteWindowsVisibility()
	m.initProfiles()
	m.recoverViews()
	startNativeMonitoring()
	m.updateShortcutStatus(configureGlobalShortcuts(resolvedShortcuts(m.config.Shortcuts)))
	m.broadcast()
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-m.app.Context().Done():
			return
		case fn := <-m.commands:
			if m.app.Context().Err() == nil {
				fn()
			}
		case action := <-stripControlEvents:
			m.handleStripControl(action)
		case <-profilePromptEvents:
			if signature := pendingProfilePrompt.Swap(nil); signature != nil {
				m.handleProfilePrompt(*signature)
			}
		case <-shortcutCaptureEvents:
			if capture := pendingShortcutCapture.Swap(nil); capture != nil {
				m.finishShortcutCapture(*capture)
			}
		case <-shortcutHintEvents:
			m.shortcutHints = pendingShortcutHints.Load()
			m.publishSnapshot(false)
		case <-nativeMonitorEvents():
			m.handleNativeMonitorEvents(takeNativeMonitorEvents(), time.Now())
		case <-m.developmentApps.changes:
			m.developmentApps.filesystemChanged(time.Now())
			m.publishSnapshot(false)
		case <-timer.C:
			m.pollMonitoring(time.Now())
		}
		m.tickViewRecovery(time.Now())
		timer.Reset(m.nextMonitorDelay(time.Now()))
	}
}
func (m *siteManager) syncSettings() {
	if !m.stripReady {
		return
	}
	refreshMinimalStrip()
	p := stripPreferences(m.strip)
	m.allDesktopsItem.SetChecked(p["allDesktops"])
	m.onTopItem.SetChecked(p["onTop"])
	m.broadcast()
}
func (m *siteManager) openSettings() {
	if !m.settingsReady || !m.stripReady {
		m.openSettingsPending = true
		return
	}
	m.openSettingsPending = false
	m.setStripMode(stripFull)
	showSettingsWindow(m.settings, m.strip)
	m.refreshSettingsEditor()
	m.broadcast()
	if m.settingsPage != "" {
		dispatchWindowEvent(m.settings, "devjay:settings-page", m.settingsPage)
		m.settingsPage = ""
	}
}
func (m *siteManager) createWindow(site Site) {
	window := m.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "site-" + site.ID, Title: "DevJay Site · " + filepath.Base(site.Path), URL: "/?site=" + site.ID, Hidden: m.config.SiteWindowsHidden, Width: 290, Height: 174, Frameless: m.config.decoration() != "normal", X: 160, Y: 260, InitialPosition: application.WindowXY, DisableResize: true, CloseButtonState: application.ButtonDisabled, MaximiseButtonState: application.ButtonDisabled, BackgroundColour: application.NewRGB(20, 24, 32), Mac: application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, CollectionBehavior: application.MacWindowCollectionBehaviorManaged | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed}})
	m.watchView("site-"+site.ID, window)
	m.mu.Lock()
	m.runtime[site.ID] = &siteRuntime{Site: site, Window: window, Git: GitStatus{Summary: "Checking repository…", Tone: "muted"}}
	m.mu.Unlock()
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		m.send(func() {
			m.mu.Lock()
			r := m.runtime[site.ID]
			m.mu.Unlock()
			if r == nil || r.Ready || r.Window != window {
				return
			}
			m.prepareSitePlacement(site.ID)
			restoreWindowLayout(window, "site-"+site.ID)
			applyTopPreference()
			setSiteWindowVisibility(window, m.config.SiteWindowsHidden)
			m.mu.Lock()
			r.Ready = true
			m.mu.Unlock()
			m.startSiteGit(r)
			m.broadcast()
		})
	})
}
func (m *siteManager) snapshot() appSnapshot {
	m.mu.Lock()
	items := make(map[string]siteRuntime, len(m.runtime))
	for id, r := range m.runtime {
		items[id] = *r
	}
	m.mu.Unlock()
	m.ensureWindowSnapshot(items)
	positions, states := m.monitor.positions, m.monitor.states
	preferences := maps.Clone(m.monitor.preferences)
	preferences["showIDEStatus"] = m.config.showIDEStatus()
	m.observeDevelopmentApps(m.monitor.windowNumbers, positions)
	settingsOpen := m.monitor.settingsOpen
	result := appSnapshot{ShortcutSettings: m.shortcutSettingsView(), ShortcutLabels: m.shortcutLabels(), Profiles: m.profilesView(), StripMode: m.stripMode, ShortcutError: m.shortcutError, ShortcutHints: m.shortcutHints, Action: m.action, QuitMessage: m.quitMessage, SuggestedSite: m.suggestedSite, Layout: m.config.layout(), Placements: m.config.Placements, SettingsOpen: settingsOpen, Decoration: m.config.decoration(), Sites: []SiteView{}, Manual: m.config.Manual, Preferences: preferences}
	if m.configError != nil {
		result.Error = "Cannot read saved Sites. Configuration has not been overwritten: " + m.configError.Error()
	}
	for _, site := range orderedSites(m.config) {
		r := items[site.ID]
		prefix, name := displayPath(site.Path)
		apps := m.developmentApps.readings[site.ID]
		view := SiteView{NeedsPlacement: m.needsSitePlacement(site.ID), Enabled: siteEnabled(m.config, site.ID), FolderMissing: m.folders.missing[site.ID], Site: site, Prefix: prefix, Name: name, Window: states[site.ID], Position: positions[site.ID], Git: r.Git, DevelopmentApps: apps.Apps, DevelopmentAppsKnown: apps.Known}
		result.Catalog = append(result.Catalog, view)
		if view.Enabled {
			result.Sites = append(result.Sites, view)
		}
	}
	return result
}

// Poll timestamps alone do not warrant waking every web view. Explicit UI
// requests still receive a snapshot, including rejected optimistic edits.
func snapshotSignature(snapshot appSnapshot) string {
	snapshot.Sites = slices.Clone(snapshot.Sites)
	for i := range snapshot.Sites {
		snapshot.Sites[i].Git.Checked = ""
	}
	data, _ := json.Marshal(snapshot)
	return string(data)
}
func (m *siteManager) broadcast() {
	// Explicit UI operations may have changed preferences or window geometry.
	m.monitor.windowsValid = false
	m.monitor.visibilityDirty = true
	m.publishSnapshot(true)
}
func (m *siteManager) publishSnapshot(force bool) {
	if !m.stripReady || m.recovery.paused {
		return
	}
	snapshot := m.snapshot()
	m.updateCompactStrip(snapshot.Sites)
	m.refreshWindowVisibility()
	m.deliverSnapshot(snapshot, force)
}
func (m *siteManager) activate(id string) {
	r := m.runtime[id]
	if r == nil || !r.Ready {
		return
	}
	// Focus is transient and cannot reliably confirm a desktop switch.
	// Request activation without treating a later focus sample as a failure.
	focusSite(r.Window)
	m.broadcast()
}
func (m *siteManager) save(c SiteConfig) error { return m.saveForTargets(c, m.profiles.targets) }
func (m *siteManager) saveForTargets(c SiteConfig, targets []string) error {
	if m.configError != nil {
		return userMessage("Saved Site configuration needs repair before making changes.")
	}
	c = captureProfileChanges(m.config, c, targets)
	if err := saveSites(siteConfigPath(), c); err != nil {
		return userMessage("Could not save Sites: " + err.Error())
	}
	m.config = c
	return nil
}

// Settings feedback stays in the drop area; successful actions silently clear it.
func (m *siteManager) siteError(message string) {
	m.app.Event.Emit("devjay:site-error", message)
}

func (m *siteManager) add(path string) { m.addAfter(path, m.addAfterID) }
func (m *siteManager) addAfter(path, after string) {
	existing := slices.Clone(m.config.Sites)
	profile := m.config.ActiveProfile
	go func() {
		site, err := newSite(path, existing)
		reference := FolderReference{}
		if err == nil {
			reference = bookmarkFolder(site.Path)
		}
		m.send(func() {
			if err != nil {
				m.siteError(err.Error())
				return
			}
			if profile != m.config.ActiveProfile {
				m.siteError("The Profile changed. Add the folder again.")
				return
			}
			if slices.ContainsFunc(m.config.Sites, func(s Site) bool { return s.Path == site.Path || s.ID == site.ID }) {
				m.siteError("That folder is already a Site, or the list changed. Try again.")
				return
			}
			if reference.Identity != "" && slices.ContainsFunc(m.config.Sites, func(s Site) bool { return m.config.Folders[s.ID].Identity == reference.Identity }) {
				m.folders.due = time.Now()
				m.siteError("That folder already belongs to a Site. DevJay is checking its new location; use Locate if needed.")
				return
			}
			c := m.config
			c.Sites = append(slices.Clone(c.Sites), site)
			c.Folders = maps.Clone(c.Folders)
			if c.Folders == nil {
				c.Folders = map[string]FolderReference{}
			}
			c.Folders[site.ID] = reference
			if after == "" {
				items := orderedSites(m.config)
				if len(items) > 0 {
					after = items[len(items)-1].ID
				}
			}
			desktop := len(m.config.Sites)
			if index := slices.IndexFunc(orderedSites(m.config), func(s Site) bool { return s.ID == after }); index >= 0 {
				desktop = index + 1
			}
			if position := m.monitor.positions[after]; position.Known {
				desktop = position.Desktop + 1
			}
			c.Profiles = slices.Clone(c.Profiles)
			for i, p := range c.Profiles {
				order := []string{}
				for _, previous := range orderedSites(applyProfileSettings(m.config, p.Settings)) {
					order = append(order, previous.ID)
				}
				p.Settings.Order = insertSiteAfter(order, site.ID, after)
				p.Settings.Manual = true
				p.PlacementHints = maps.Clone(p.PlacementHints)
				if p.PlacementHints == nil {
					p.PlacementHints = map[string]SitePlacementHint{}
				}
				p.PlacementHints[site.ID] = SitePlacementHint{After: after, Desktop: desktop}
				c.Profiles[i] = p
			}
			if i := profileIndex(c, c.ActiveProfile); i >= 0 {
				c.Order, c.Manual = slices.Clone(c.Profiles[i].Settings.Order), true
			} else {
				c.Order = insertSiteAfter(c.Order, site.ID, after)
			}
			if err := m.saveForTargets(c, nil); err != nil {
				m.siteError(err.Error())
				return
			}
			m.createWindow(site)
			m.siteError("")
			m.broadcast()
		})
	}()
}
func (m *siteManager) remove(id string) {
	if m.actionBusy() {
		m.siteError("Wait for the checkout action to finish before removing a Site.")
		return
	}
	r := m.runtime[id]
	if !slices.ContainsFunc(m.config.Sites, func(s Site) bool { return s.ID == id }) {
		return
	}
	c := m.config
	c.Sites = slices.DeleteFunc(slices.Clone(c.Sites), func(s Site) bool { return s.ID == id })
	c.Order = slices.DeleteFunc(slices.Clone(c.Order), func(s string) bool { return s == id })
	if c.Placements != nil {
		c.Placements = maps.Clone(c.Placements)
		delete(c.Placements, id)
	}
	c.Folders = maps.Clone(c.Folders)
	delete(c.Folders, id)
	c.Profiles = slices.Clone(c.Profiles)
	for i, p := range c.Profiles {
		p.Excluded = slices.DeleteFunc(slices.Clone(p.Excluded), func(v string) bool { return v == id })
		p.Settings.Order = slices.DeleteFunc(slices.Clone(p.Settings.Order), func(v string) bool { return v == id })
		p.Settings.Placements = maps.Clone(p.Settings.Placements)
		delete(p.Settings.Placements, id)
		p.PlacementHints = maps.Clone(p.PlacementHints)
		delete(p.PlacementHints, id)
		c.Profiles[i] = p
	}
	if err := m.saveForTargets(c, nil); err != nil {
		m.siteError(err.Error())
		return
	}
	if r == nil {
		m.siteError("")
		m.broadcast()
		return
	}
	for viewerID, viewer := range m.viewers {
		if viewer.state.SiteID == id {
			m.closeViewer(viewerID)
		}
	}
	if m.suggestedSite == id {
		m.closeSuggestions()
	}
	m.mu.Lock()
	delete(m.runtime, id)
	m.mu.Unlock()
	if r.Cancel != nil {
		r.Cancel()
	}
	if r.Ready {
		saveWindowLayout(r.Window)
	}
	m.forgetView("site-" + id)
	r.Window.Close()
	m.siteError("")
	m.broadcast()
}
func (m *siteManager) browse(after string) {
	if m.browsing {
		return
	}
	m.browsing = true
	go func() {
		path, err := m.app.Dialog.OpenFile().SetTitle("Add a DevJay Site").SetButtonText("Add Site").CanChooseDirectories(true).CanChooseFiles(false).CanCreateDirectories(false).AttachToWindow(m.settings).PromptForSingleSelection()
		m.send(func() {
			m.browsing = false
			if err != nil {
				m.siteError("Could not open the folder picker.")
			} else if path != "" {
				m.addAfter(path, after)
			} else {
				m.siteError("")
			}
		})
	}()
}
func (m *siteManager) registerEvents() {
	m.registerShortcutEvents()
	m.registerFolderEvents()
	m.registerProfileEvents()
	m.app.Event.On("devjay:add-after", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		if id, ok := e.Data.(string); ok {
			m.send(func() { m.addAfterID = id })
		}
	})
	m.registerEditorSettingsEvents()
	m.app.Event.On("devjay:app-tooltip-refresh", func(e *application.CustomEvent) {
		m.send(func() {
			if e.Sender == "strip" && m.stripReady {
				refreshAppTooltip(m.strip)
				return
			}
			for id, r := range m.runtime {
				if e.Sender == "site-"+id && r.Ready {
					refreshAppTooltip(r.Window)
					return
				}
			}
		})
	})
	m.app.Event.On("devjay:showIDEStatus", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		value, ok := e.Data.(bool)
		if !ok {
			return
		}
		m.send(func() {
			c := m.config
			c.ShowIDEStatus = &value
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
				m.broadcast()
				return
			}
			m.developmentApps.setEnabled(value)
			m.siteError("")
			m.broadcast()
		})
	})
	m.app.Event.On("devjay:fit-cards", func(e *application.CustomEvent) {
		var size struct {
			Height, ViewportWidth, ViewportHeight float64
			Decoration                            string
			Refit                                 bool
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &size) != nil {
			return
		}
		m.send(func() {
			if size.Decoration != m.config.decoration() {
				return // A delayed measurement from the previous decoration.
			}
			if e.Sender == "strip" && m.stripReady {
				fitCardWindow(m.strip, size.Height, size.ViewportWidth, size.ViewportHeight, size.Refit)
				return
			}
			for id, r := range m.runtime {
				if e.Sender == "site-"+id && r.Ready {
					fitCardWindow(r.Window, size.Height, size.ViewportWidth, size.ViewportHeight, false)
					return
				}
			}
		})
	})
	m.app.Event.On("devjay:reset-placements", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		m.send(func() {
			c := m.config
			c.Placements = nil
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
			}
			m.broadcast()
		})
	})
	m.app.Event.On("devjay:layout", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		value, ok := e.Data.(string)
		if !ok || !validLayout(value) {
			return
		}
		m.send(func() {
			c := m.config
			if c.layout() == value {
				return
			}
			c.Layout = value
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
				m.broadcast()
				return
			}
			fitStrip(m.strip, c)
			m.broadcast()
		})
	})
	m.app.Event.On("devjay:placements", func(e *application.CustomEvent) {
		if e.Sender != "strip" {
			return
		}
		data, err := json.Marshal(e.Data)
		if err != nil {
			return
		}
		var positions map[string]CardPlacement
		if json.Unmarshal(data, &positions) != nil {
			return
		}
		m.send(func() {
			if !m.settings.IsVisible() || m.config.layout() != "custom" || !validPlacements(visibleSites(m.config), positions) {
				m.broadcast()
				return
			}
			c := m.config
			c.Placements = maps.Clone(c.Placements)
			if c.Placements == nil {
				c.Placements = map[string]CardPlacement{}
			}
			maps.Copy(c.Placements, positions)
			c.Manual = true
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
			}
			m.broadcast()
		})
	})
	m.app.Event.On("devjay:decoration", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		value, ok := e.Data.(string)
		if !ok || (value != "none" && value != "minimal" && value != "normal") {
			return
		}
		m.send(func() {
			c := m.config
			c.Decoration = value
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
				m.broadcast()
				return
			}
			m.strip.SetFrameless(value != "normal")
			m.strip.SetTitle("DevJay")
			for _, r := range m.runtime {
				r.Window.SetFrameless(value != "normal")
				r.Window.SetTitle("DevJay Site · " + filepath.Base(r.Site.Path))
			}
			m.siteError("")
			m.broadcast()
		})
	})

	m.app.Event.On("devjay:hello", func(e *application.CustomEvent) {
		m.send(func() {
			if m.recoveryWindows[e.Sender] == nil {
				return
			}
			delete(m.monitor.sent, e.Sender)
			delete(m.monitor.visibility, e.Sender)
			m.monitor.visibilityDirty = true
			m.publishSnapshot(false)
		})
	})
	m.app.Event.On("devjay:settings-open", func(e *application.CustomEvent) {
		m.send(func() {
			if e.Sender == "strip" {
				m.openSettings()
				return
			}
			for id := range m.runtime {
				if e.Sender == "site-"+id {
					m.openSettings()
					return
				}
			}
		})
	})
	m.app.Event.On("devjay:settings-close", func(e *application.CustomEvent) {
		if e.Sender == "settings" {
			m.settings.Hide()
			m.send(func() { m.profiles.targets = nil; m.broadcast() })
		}
	})
	m.app.Event.On("devjay:activate", func(e *application.CustomEvent) {
		if e.Sender != "strip" && e.Sender != "settings" {
			return
		}
		if id, ok := e.Data.(string); ok {
			m.send(func() { m.activate(id) })
		}
	})
	m.app.Event.On("devjay:add", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		if path, ok := e.Data.(string); ok {
			m.send(func() { m.add(path) })
		}
	})
	m.app.Event.On("devjay:browse", func(e *application.CustomEvent) {
		if e.Sender == "settings" {
			after, _ := e.Data.(string)
			m.send(func() { m.browse(after) })
		}
	})
	m.app.Event.On("devjay:remove", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		if id, ok := e.Data.(string); ok {
			m.send(func() { m.confirmRemove(id) })
		}
	})
	for _, pref := range []string{"allDesktops", "onTop"} {
		m.app.Event.On("devjay:"+pref, func(e *application.CustomEvent) {
			if e.Sender != "settings" {
				return
			}
			if value, ok := e.Data.(bool); ok {
				m.send(func() { m.setViewPreference(pref, value) })
			}
		})
	}
	m.app.Event.On("devjay:order", func(e *application.CustomEvent) {
		if e.Sender != "settings" && e.Sender != "strip" {
			return
		}
		data, err := json.Marshal(e.Data)
		if err != nil {
			return
		}
		var ids []string
		if json.Unmarshal(data, &ids) != nil {
			return
		}
		m.send(func() {
			if e.Sender == "strip" && !m.settings.IsVisible() {
				m.broadcast()
				return
			}
			if !validOrder(m.config.Sites, ids) {
				m.siteError("The Site list changed. Try dragging again.")
				m.broadcast()
				return
			}
			c := m.config
			c.Manual = true
			c.Order = ids
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
				m.broadcast()
				return
			}
			m.siteError("")
			m.broadcast()
		})
	})
	m.app.Event.On("devjay:sorting", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		value, ok := e.Data.(string)
		if !ok || (value != "name" && value != "manual") {
			return
		}
		m.send(func() {
			c := m.config
			c.Manual = value == "manual"
			if err := m.save(c); err != nil {
				m.siteError(err.Error())
				m.broadcast()
				return
			}
			m.siteError("")
			m.broadcast()
		})
	})
}
func (m *siteManager) shutdown() {
	m.mu.Lock()
	items := make([]siteRuntime, 0, len(m.runtime))
	for _, r := range m.runtime {
		items = append(items, *r)
	}
	m.mu.Unlock()
	saveWindowLayout(m.strip)
	stopStripControls()
	for _, r := range items {
		if r.Cancel != nil {
			r.Cancel()
		}
		if r.Ready {
			saveWindowLayout(r.Window)
		}
	}
}

// ShouldQuit runs on AppKit. Wait off that thread so windows stay responsive.
func (m *siteManager) shouldQuit() bool {
	ready, drained := m.lifecycle.quit()
	if drained != nil {
		go func() {
			m.send(func() {
				m.quitMessage = "Waiting for the confirmed action to finish before quitting…"
				m.broadcast()
			})
			<-drained
			m.app.Quit()
		}()
	}
	return ready
}

func (m *siteManager) confirmRemove(id string) {
	index := slices.IndexFunc(m.config.Sites, func(s Site) bool { return s.ID == id })
	if index < 0 || m.removing {
		return
	}
	if m.actionBusy() {
		m.siteError("Wait for the checkout action to finish before removing a Site.")
		return
	}
	site := m.config.Sites[index]
	m.removing = true
	dialog := m.app.Dialog.Question().SetTitle("Remove Site?").SetMessage("Remove " + filepath.Base(site.Path) + " from all Profiles? Files in the folder are kept.").AttachToWindow(m.settings)
	dialog.AddButton("Cancel").SetAsDefault().SetAsCancel().OnClick(func() { m.send(func() { m.removing = false }) })
	dialog.AddButton("Remove Site").OnClick(func() {
		m.send(func() {
			m.removing = false
			if slices.Contains(m.config.Sites, site) {
				m.remove(id)
			}
		})
	})
	dialog.Show()
}
