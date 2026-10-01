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
	Site       Site
	Window     *application.WebviewWindow
	Ready      bool
	Git        GitStatus
	Cancel     context.CancelFunc
	GitControl *gitWatchControl
}
type SiteView struct {
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
	Action        *CheckoutAction          `json:"action,omitempty"`
	QuitMessage   string                   `json:"quitMessage,omitempty"`
	SuggestedSite string                   `json:"suggestedSite,omitempty"`
	Layout        string                   `json:"layout"`
	Placements    map[string]CardPlacement `json:"placements"`
	SettingsOpen  bool                     `json:"settingsOpen"`
	Decoration    string                   `json:"decoration"`
	Sites         []SiteView               `json:"sites"`
	Manual        bool                     `json:"manual"`
	Preferences   map[string]bool          `json:"preferences"`
	Error         string                   `json:"error"`
}
type siteManager struct {
	lifecycle                                                actionLifetime
	quitMessage                                              string
	removing                                                 bool
	recovery                                                 uiRecovery
	recoveryWindows                                          map[string]*application.WebviewWindow
	editorState                                              *EditorState
	editorGeneration                                         uint64
	editorBrowsing                                           bool
	developmentApps                                          developmentAppMonitor
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
	panel, settings                                          *application.WebviewWindow
	panelReady, settingsReady, openSettingsPending, browsing bool
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
	m.recoverViews()
	startNativeMonitoring()
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
	if !m.panelReady {
		return
	}
	p := panelPreferences(m.panel)
	m.allDesktopsItem.SetChecked(p["allDesktops"])
	m.onTopItem.SetChecked(p["onTop"])
	m.broadcast()
}
func (m *siteManager) openSettings() {
	if !m.settingsReady || !m.panelReady {
		m.openSettingsPending = true
		return
	}
	m.openSettingsPending = false
	showSettingsWindow(m.settings, m.panel)
	m.refreshSettingsEditor()
	m.broadcast()
}
func (m *siteManager) createWindow(site Site) {
	window := m.app.Window.NewWithOptions(application.WebviewWindowOptions{Name: "site-" + site.ID, Title: "DevJay Site · " + filepath.Base(site.Path), URL: "/?site=" + site.ID, Width: 290, Height: 174, Frameless: m.config.decoration() != "normal", X: 160, Y: 260, InitialPosition: application.WindowXY, DisableResize: true, CloseButtonState: application.ButtonDisabled, MaximiseButtonState: application.ButtonDisabled, BackgroundColour: application.NewRGB(20, 24, 32), Mac: application.MacWindow{CornerType: application.MacWindowCornerTypeRounded, CollectionBehavior: application.MacWindowCollectionBehaviorManaged | application.MacWindowCollectionBehaviorFullScreenNone, TabbingMode: application.MacWindowTabbingModeDisallowed}})
	m.watchView("site-"+site.ID, window)
	m.mu.Lock()
	m.runtime[site.ID] = &siteRuntime{Site: site, Window: window, Git: GitStatus{Summary: "Checking repository…", Tone: "muted"}}
	m.mu.Unlock()
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		m.send(func() {
			m.mu.Lock()
			r := m.runtime[site.ID]
			m.mu.Unlock()
			if r == nil || r.Ready {
				return
			}
			restoreWindowLayout(window, "site-"+site.ID)
			applyTopPreference()
			ctx, cancel := context.WithCancel(m.app.Context())
			m.mu.Lock()
			r.Ready = true
			r.Cancel = cancel
			r.GitControl = newGitWatchControl()
			m.mu.Unlock()
			go watchGitWithControl(ctx, site.Path, r.GitControl, func(status GitStatus) {
				m.send(func() {
					if current := m.runtime[site.ID]; current == r {
						m.mu.Lock()
						previous := current.Git
						current.Git = status
						m.mu.Unlock()
						m.siteViewersChanged(site.ID, previous, status)
						m.publishSnapshot(false)
					}
				})
			})
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
	result := appSnapshot{Action: m.action, QuitMessage: m.quitMessage, SuggestedSite: m.suggestedSite, Layout: m.config.layout(), Placements: m.config.Placements, SettingsOpen: settingsOpen, Decoration: m.config.decoration(), Sites: []SiteView{}, Manual: m.config.Manual, Preferences: preferences}
	if m.configError != nil {
		result.Error = "Cannot read saved Sites. Configuration has not been overwritten: " + m.configError.Error()
	}
	for _, site := range orderedSites(m.config) {
		r := items[site.ID]
		prefix, name := displayPath(site.Path)
		apps := m.developmentApps.readings[site.ID]
		result.Sites = append(result.Sites, SiteView{Site: site, Prefix: prefix, Name: name, Window: states[site.ID], Position: positions[site.ID], Git: r.Git,
			DevelopmentApps: apps.Apps, DevelopmentAppsKnown: apps.Known})
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
	if !m.panelReady || m.recovery.paused {
		return
	}
	snapshot := m.snapshot()
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
func (m *siteManager) save(c SiteConfig) error {
	if m.configError != nil {
		return userMessage("Saved Site configuration needs repair before making changes.")
	}
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

func (m *siteManager) add(path string) {
	site, err := newSite(path, m.config.Sites)
	if err != nil {
		m.siteError(err.Error())
		return
	}
	c := m.config
	c.Sites = append(slices.Clone(c.Sites), site)
	c.Order = append(slices.Clone(c.Order), site.ID)
	if err = m.save(c); err != nil {
		m.siteError(err.Error())
		return
	}
	m.createWindow(site)
	m.siteError("")
	m.broadcast()
}
func (m *siteManager) remove(id string) {
	if m.actionBusy() {
		m.siteError("Wait for the checkout action to finish before removing a Site.")
		return
	}
	r := m.runtime[id]
	if r == nil {
		return
	}
	c := m.config
	c.Sites = slices.DeleteFunc(slices.Clone(c.Sites), func(s Site) bool { return s.ID == id })
	c.Order = slices.DeleteFunc(slices.Clone(c.Order), func(s string) bool { return s == id })
	if c.Placements != nil {
		c.Placements = maps.Clone(c.Placements)
		delete(c.Placements, id)
	}
	if err := m.save(c); err != nil {
		m.siteError(err.Error())
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
func (m *siteManager) browse() {
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
				m.add(path)
			} else {
				m.siteError("")
			}
		})
	}()
}
func (m *siteManager) registerEvents() {
	m.registerEditorSettingsEvents()
	m.app.Event.On("devjay:app-tooltip-refresh", func(e *application.CustomEvent) {
		m.send(func() {
			if e.Sender == "control-panel" && m.panelReady {
				refreshAppTooltip(m.panel)
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
		var size struct{ Height, ViewportWidth, ViewportHeight float64 }
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &size) != nil {
			return
		}
		m.send(func() {
			if e.Sender == "control-panel" && m.panelReady {
				growCardWindow(m.panel, size.Height, size.ViewportWidth, size.ViewportHeight)
				return
			}
			for id, r := range m.runtime {
				if e.Sender == "site-"+id && r.Ready {
					growCardWindow(r.Window, size.Height, size.ViewportWidth, size.ViewportHeight)
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
			fitControlPanel(m.panel, c)
			m.broadcast()
		})
	})
	m.app.Event.On("devjay:placements", func(e *application.CustomEvent) {
		if e.Sender != "control-panel" {
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
			if !m.settings.IsVisible() || m.config.layout() != "custom" || !validPlacements(m.config.Sites, positions) {
				m.broadcast()
				return
			}
			c := m.config
			c.Placements = positions
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
			m.panel.SetFrameless(value != "normal")
			m.panel.SetTitle("DevJay")
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
			if e.Sender == "control-panel" {
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
			m.send(m.broadcast)
		}
	})
	m.app.Event.On("devjay:activate", func(e *application.CustomEvent) {
		if e.Sender != "control-panel" && e.Sender != "settings" {
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
			m.send(m.browse)
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
				m.send(func() { setPanelPreference(m.panel, pref, value); m.syncSettings() })
			}
		})
	}
	m.app.Event.On("devjay:order", func(e *application.CustomEvent) {
		if e.Sender != "settings" && e.Sender != "control-panel" {
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
			if e.Sender == "control-panel" && !m.settings.IsVisible() {
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
	saveWindowLayout(m.panel)
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
	r := m.runtime[id]
	if r == nil || m.removing {
		return
	}
	if m.actionBusy() {
		m.siteError("Wait for the checkout action to finish before removing a Site.")
		return
	}
	m.removing = true
	dialog := m.app.Dialog.Question().SetTitle("Remove Site?").SetMessage("Remove " + filepath.Base(r.Site.Path) + " from DevJay? Its saved position and order will be removed. Files in the folder are kept.").AttachToWindow(m.settings)
	dialog.AddButton("Cancel").SetAsDefault().SetAsCancel().OnClick(func() { m.send(func() { m.removing = false }) })
	dialog.AddButton("Remove Site").OnClick(func() {
		m.send(func() {
			m.removing = false
			if m.runtime[id] == r {
				m.remove(id)
			}
		})
	})
	dialog.Show()
}
