package main

import (
	"encoding/json"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type profileMonitor struct {
	due               time.Time
	displays          DisplayConfiguration
	unknown, prompted string
	targets           []string
	message           string
}
type ProfilesView struct {
	Current   string           `json:"current"`
	Profiles  []Profile        `json:"profiles"`
	Displays  []ProfileDisplay `json:"displays"`
	Automatic bool             `json:"automatic"`
	Unknown   bool             `json:"unknown"`
	Targets   []string         `json:"targets"`
	Message   string           `json:"message"`
}

func (m *siteManager) profilesView() ProfilesView {
	return ProfilesView{m.config.ActiveProfile, m.config.Profiles, m.profiles.displays.Displays, m.config.AutomaticProfiles == nil || *m.config.AutomaticProfiles, m.profiles.unknown != "", m.profileTargets(), m.profiles.message}
}
func (m *siteManager) profileTargets() []string {
	result := []string{m.config.ActiveProfile}
	for _, id := range m.profiles.targets {
		if !slices.Contains(result, id) {
			result = append(result, id)
		}
	}
	return result
}

func (m *siteManager) initProfiles() {
	m.profiles.due = time.Now().Add(30 * time.Second)
	if m.configError != nil {
		return
	}
	m.profiles.displays = readProfileDisplays()
	if m.profiles.displays.Signature == "" {
		m.profiles.due = time.Now().Add(2 * time.Second)
		return
	}
	migrate := len(m.config.Profiles) == 0
	if migrate {
		c := m.config
		preferences := panelPreferences(m.panel)
		all, top := preferences["allDesktops"], preferences["onTop"]
		c.AllDesktops, c.OnTop = &all, &top
		id := newProfileID()
		c.ActiveProfile = id
		c.Profiles = []Profile{{ID: id, Name: "Default", Displays: []string{m.profiles.displays.Signature}, Settings: profileSettings(c)}}
		if err := m.save(c); err != nil {
			m.profiles.message = err.Error()
			return
		}
	}
	p := m.config.Profiles[profileIndex(m.config, m.config.ActiveProfile)]
	signature := m.profiles.displays.Signature
	known := slices.Contains(p.Displays, signature)
	activateNativeProfile(p.ID, signature, migrate)
	pauseProfileLayouts(!known)
	m.profiles.due = time.Now().Add(2 * time.Second)
}

func (m *siteManager) pollProfiles(now time.Time) {
	if m.recovery.paused || m.configError != nil || now.Before(m.profiles.due) {
		return
	}
	if len(m.config.Profiles) == 0 {
		m.initProfiles()
		return
	}
	m.profiles.due = now.Add(30 * time.Second)
	displays := readProfileDisplays()
	if displays.Signature == "" {
		pauseProfileLayouts(true)
		return
	}
	m.profiles.displays = displays
	current := m.config.Profiles[profileIndex(m.config, m.config.ActiveProfile)]
	if slices.Contains(current.Displays, displays.Signature) {
		m.profiles.prompted = ""
		if m.profiles.unknown != "" {
			m.switchProfile(current.ID, false)
		} else {
			pauseProfileLayouts(false)
		}
		return
	}
	pauseProfileLayouts(true)
	m.profiles.unknown = displays.Signature
	if m.actionBusy() || m.browsing || m.editorBrowsing {
		m.profiles.due = now.Add(time.Second)
		return
	}
	if m.config.AutomaticProfiles == nil || *m.config.AutomaticProfiles {
		for _, p := range m.config.Profiles {
			if slices.Contains(p.Displays, displays.Signature) {
				m.switchProfile(p.ID, false)
				return
			}
		}
	}
	if m.profiles.prompted == displays.Signature {
		return
	}
	m.profiles.prompted = displays.Signature
	dialog := m.app.Dialog.Question().SetTitle("Choose a Profile for these displays").SetMessage("DevJay detected a different monitor combination. Create a Profile or choose an existing one. Your saved window arrangements are kept until you choose.").AttachToWindow(m.panel)
	dialog.AddButton("Not now").SetAsCancel().OnClick(func() {})
	open := func() {
		m.send(func() {
			if readProfileDisplays().Signature != displays.Signature {
				return
			}
			m.settingsPage = "profiles"
			m.openSettings()
		})
	}
	dialog.AddButton("Choose Profile…").OnClick(open)
	dialog.AddButton("Create Profile…").SetAsDefault().OnClick(open)
	dialog.Show()
}

func (m *siteManager) switchProfile(id string, bind bool) {
	i := profileIndex(m.config, id)
	if i < 0 || m.actionBusy() || m.browsing || m.editorBrowsing {
		m.profiles.message = "Finish the current action or folder picker before switching Profiles."
		return
	}
	displays := readProfileDisplays()
	if displays.Signature == "" {
		m.profiles.message = "Displays are not ready yet."
		return
	}
	c := m.config
	c.Profiles = slices.Clone(c.Profiles)
	if bind {
		for j, p := range c.Profiles {
			c.Profiles[j].Displays = slices.DeleteFunc(slices.Clone(p.Displays), func(s string) bool { return s == displays.Signature })
		}
		c.Profiles[i].Displays = append(c.Profiles[i].Displays, displays.Signature)
	} else if !slices.Contains(c.Profiles[i].Displays, displays.Signature) {
		return
	}
	c.ActiveProfile = id
	c = applyProfileSettings(c, c.Profiles[i].Settings)
	// A Profile switch loads preferences rather than treating them as user edits.
	if err := saveSites(siteConfigPath(), c); err != nil {
		m.profiles.message = err.Error()
		return
	}
	m.closeSuggestions()
	for viewerID := range m.viewers {
		m.closeViewer(viewerID)
	}
	activateNativeProfile(id, displays.Signature, false)
	m.config = c
	m.profiles.targets = nil
	m.profiles.displays = displays
	m.profiles.unknown, m.profiles.message, m.profiles.prompted = "", "", ""
	m.syncProfileSites()
	m.panel.SetFrameless(c.decoration() != "normal")
	m.panel.SetTitle("DevJay")
	if m.panelReady {
		restoreWindowLayout(m.panel, "control-panel")
	}
	for siteID, r := range m.runtime {
		r.Window.SetFrameless(c.decoration() != "normal")
		r.Window.SetTitle("DevJay Site · " + filepath.Base(r.Site.Path))
		if r.Ready {
			m.prepareSitePlacement(siteID)
			restoreWindowLayout(r.Window, "site-"+siteID)
		}
	}
	m.applyProfileViewSettings()
	m.refreshSettingsEditor()
	m.developmentApps.markDiscoveryDirty()
	m.broadcast()
}

func (m *siteManager) needsSitePlacement(id string) bool {
	i := profileIndex(m.config, m.config.ActiveProfile)
	if i < 0 {
		return false
	}
	_, ok := m.config.Profiles[i].PlacementHints[id]
	return ok
}
func (m *siteManager) keepSitePlacement(id string) {
	r := m.runtime[id]
	if r == nil || !r.Ready || !captureProfilePlacement(r.Window) {
		m.profiles.message = "Choose a Profile for the connected displays, then try Keep here again."
		return
	}
	m.clearSitePlacement(id)
}
func (m *siteManager) clearSitePlacement(id string) {
	i := profileIndex(m.config, m.config.ActiveProfile)
	if i < 0 {
		return
	}
	c := m.config
	c.Profiles = slices.Clone(c.Profiles)
	c.Profiles[i].PlacementHints = maps.Clone(c.Profiles[i].PlacementHints)
	delete(c.Profiles[i].PlacementHints, id)
	if err := m.save(c); err != nil {
		m.profiles.message = err.Error()
	}
}
func (m *siteManager) prepareSitePlacement(id string) {
	i := profileIndex(m.config, m.config.ActiveProfile)
	if i < 0 {
		return
	}
	hint, ok := m.config.Profiles[i].PlacementHints[id]
	if ok && seedNativeSite(id, hint, visibleSites(m.config)) {
		m.clearSitePlacement(id)
	}
}

func (m *siteManager) syncProfileSites() {
	for id, r := range m.runtime {
		if siteEnabled(m.config, id) {
			continue
		}
		if r.Cancel != nil {
			r.Cancel()
		}
		if r.Ready {
			saveWindowLayout(r.Window)
		}
		m.forgetView("site-" + id)
		m.mu.Lock()
		delete(m.runtime, id)
		m.mu.Unlock()
		r.Window.Close()
	}
	for _, site := range visibleSites(m.config) {
		if m.runtime[site.ID] == nil {
			m.createWindow(site)
		}
	}
}

func (m *siteManager) applyProfileViewSettings() {
	if !m.panelReady {
		return
	}
	all, top := m.config.AllDesktops, m.config.OnTop
	setPanelPreference(m.panel, "allDesktops", all == nil || *all)
	setPanelPreference(m.panel, "onTop", top == nil || *top)
	m.syncSettings()
}
func (m *siteManager) setViewPreference(name string, value bool) {
	c := m.config
	if name == "allDesktops" {
		c.AllDesktops = &value
	} else {
		c.OnTop = &value
	}
	if err := m.save(c); err != nil {
		m.siteError(err.Error())
		m.broadcast()
		return
	}
	setPanelPreference(m.panel, name, value)
	m.syncSettings()
}

func (m *siteManager) registerProfileEvents() {
	m.app.Event.On("devjay:profile", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		var req struct {
			Operation, ID, Name, SiteID, RequestID string
			Enabled                                bool
			Targets                                []string
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			m.profiles.message = ""
			c := m.config
			c.Profiles = slices.Clone(c.Profiles)
			switch req.Operation {
			case "placement":
				if m.runtime[req.SiteID] != nil {
					m.keepSitePlacement(req.SiteID)
				}
			case "scope":
				m.profiles.targets = nil
				for _, id := range req.Targets {
					if profileIndex(c, id) >= 0 && !slices.Contains(m.profiles.targets, id) {
						m.profiles.targets = append(m.profiles.targets, id)
					}
				}
			case "automatic":
				c.AutomaticProfiles = &req.Enabled
				if err := m.save(c); err != nil {
					m.profiles.message = err.Error()
				}
				m.profiles.due = time.Now()
			case "use":
				m.switchProfile(req.ID, true)
			case "create":
				if profileIndex(c, c.ActiveProfile) < 0 {
					m.profiles.message = "Wait for the connected displays to be detected before creating a Profile."
					break
				}
				name := strings.TrimSpace(req.Name)
				if name == "" || len(name) > 160 {
					m.profiles.message = "Enter a Profile name."
					break
				}
				if m.actionBusy() || m.browsing || m.editorBrowsing {
					m.profiles.message = "Finish the current action before creating a Profile."
					break
				}
				id := newProfileID()
				p := Profile{ID: id, Name: name, Settings: profileSettings(c)}
				if i := profileIndex(c, c.ActiveProfile); i >= 0 {
					p.Excluded = slices.Clone(c.Profiles[i].Excluded)
				}
				c.Profiles = append(c.Profiles, p)
				if err := m.save(c); err != nil {
					m.profiles.message = err.Error()
					break
				}
				m.switchProfile(id, true)
			case "rename":
				i := profileIndex(c, req.ID)
				name := strings.TrimSpace(req.Name)
				if i < 0 || name == "" || len(name) > 160 {
					m.profiles.message = "Enter a Profile name."
					break
				}
				c.Profiles[i].Name = name
				if err := m.save(c); err != nil {
					m.profiles.message = err.Error()
				}
			case "delete":
				updated, err := withoutProfile(c, req.ID)
				if err == nil {
					err = m.save(updated)
				}
				if err != nil {
					m.profiles.message = err.Error()
					break
				}
				deleteNativeProfile(req.ID)
				m.profiles.targets = slices.DeleteFunc(m.profiles.targets, func(id string) bool { return id == req.ID })
			case "site":
				if m.actionBusy() {
					m.profiles.message = "Finish the Git action before changing Site visibility."
					break
				}
				if !slices.ContainsFunc(c.Sites, func(s Site) bool { return s.ID == req.SiteID }) {
					break
				}
				for i, p := range c.Profiles {
					if !slices.Contains(m.profileTargets(), p.ID) {
						continue
					}
					p.Excluded = slices.DeleteFunc(slices.Clone(p.Excluded), func(id string) bool { return id == req.SiteID })
					if !req.Enabled {
						p.Excluded = append(p.Excluded, req.SiteID)
					}
					c.Profiles[i] = p
				}
				if err := m.save(c); err != nil {
					m.profiles.message = err.Error()
					break
				}
				m.closeSuggestions()
				for id := range m.viewers {
					m.closeViewer(id)
				}
				m.syncProfileSites()
			}
			if m.profiles.message != "" {
				m.siteError(m.profiles.message)
			}
			m.broadcast()
			if req.RequestID != "" {
				m.actionReply(req.RequestID, m.profiles.message)
			}
		})
	})
}
