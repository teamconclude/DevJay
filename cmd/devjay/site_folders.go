package main

import (
	"context"
	"maps"
	"path/filepath"
	"slices"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type folderMonitor struct {
	due     time.Time
	busy    bool
	missing map[string]bool
}

type folderReading struct {
	site                Site
	previous, reference FolderReference
	path                string
}

func (m *siteManager) pollFolders(now time.Time) {
	if m.folders.busy || m.recovery.paused || m.configError != nil || now.Before(m.folders.due) {
		return
	}
	m.folders.busy = true
	sites, references := slices.Clone(m.config.Sites), maps.Clone(m.config.Folders)
	go func() {
		readings := make([]folderReading, 0, len(sites))
		for _, site := range sites {
			if m.app.Context().Err() != nil {
				return
			}
			path, reference := resolveFolder(site, references[site.ID])
			if reference.Bookmark == "" && folderIdentity(site.Path) == "" {
				path = ""
			}
			readings = append(readings, folderReading{site, references[site.ID], reference, path})
		}
		m.send(func() {
			m.folders.busy = false
			m.folders.due = time.Now().Add(15 * time.Second)
			if m.recovery.paused || m.actionBusy() {
				return
			}
			c := m.config
			c.Sites, c.Folders = slices.Clone(c.Sites), maps.Clone(c.Folders)
			if c.Folders == nil {
				c.Folders = map[string]FolderReference{}
			}
			if m.folders.missing == nil {
				m.folders.missing = map[string]bool{}
			}
			changed := false
			for _, reading := range readings {
				i := slices.Index(c.Sites, reading.site)
				if i < 0 || c.Folders[reading.site.ID] != reading.previous {
					continue
				}
				missing := reading.path == "" || slices.ContainsFunc(c.Sites, func(s Site) bool { return s.ID != reading.site.ID && s.Path == reading.path })
				m.folders.missing[reading.site.ID] = missing
				if missing {
					continue
				}
				if reading.path != c.Sites[i].Path || reading.reference != reading.previous {
					c.Sites[i].Path, c.Folders[reading.site.ID] = reading.path, reading.reference
					changed = true
				}
			}
			if changed {
				c = preserveRenamedSiteOrder(m.config, c)
				if err := m.saveForTargets(c, nil); err != nil {
					m.siteError(err.Error())
					return
				}
				for _, site := range c.Sites {
					m.updateSiteFolder(site)
				}
			}
			m.publishSnapshot(false)
		})
	}()
}

func (m *siteManager) startSiteGit(r *siteRuntime) {
	ctx, cancel := context.WithCancel(m.app.Context())
	control := newGitWatchControl()
	control.folderIdentity = m.config.Folders[r.Site.ID].Identity
	m.mu.Lock()
	r.Cancel, r.GitControl, r.FolderIdentity = cancel, control, control.folderIdentity
	m.mu.Unlock()
	go watchGitWithControl(ctx, r.Site.Path, control, func(status GitStatus) {
		m.send(func() {
			if current := m.runtime[r.Site.ID]; current == r {
				m.mu.Lock()
				previous := current.Git
				current.Git = status
				m.mu.Unlock()
				m.siteViewersChanged(r.Site.ID, previous, status)
				m.publishSnapshot(false)
			}
		})
	})
}

func (m *siteManager) updateSiteFolder(site Site) {
	old := m.runtime[site.ID]
	if old == nil || (old.Site == site && old.FolderIdentity == m.config.Folders[site.ID].Identity) {
		return
	}
	if old.Cancel != nil {
		old.Cancel()
	}
	for id, viewer := range m.viewers {
		if viewer.state.SiteID == site.ID {
			m.closeViewer(id)
		}
	}
	m.closeSiteAction(site.ID)
	// Replace the runtime identity to reject workers still using the old path.
	r := &siteRuntime{Site: site, Window: old.Window, Ready: old.Ready, Git: GitStatus{Summary: "Checking repository…", Tone: "muted"}}
	m.mu.Lock()
	m.runtime[site.ID] = r
	m.mu.Unlock()
	r.Window.SetTitle("DevJay Site · " + filepath.Base(site.Path))
	if r.Ready {
		m.startSiteGit(r)
	}
	m.developmentApps.markDiscoveryDirty()
}

func (m *siteManager) locateFolder(id string) {
	i := slices.IndexFunc(m.config.Sites, func(s Site) bool { return s.ID == id })
	if i < 0 || m.browsing || m.actionBusy() {
		return
	}
	site := m.config.Sites[i]
	existing := slices.DeleteFunc(slices.Clone(m.config.Sites), func(s Site) bool { return s.ID == id })
	m.browsing = true
	go func() {
		path, err := m.app.Dialog.OpenFile().SetTitle("Locate " + filepath.Base(site.Path)).SetButtonText("Use folder").CanChooseDirectories(true).CanChooseFiles(false).CanCreateDirectories(false).AttachToWindow(m.settings).PromptForSingleSelection()
		var replacement Site
		var reference FolderReference
		if err == nil && path != "" {
			replacement, err = newSite(path, existing)
			if err == nil {
				reference = bookmarkFolder(replacement.Path)
				replacement.ID = id
			}
		}
		m.send(func() {
			m.browsing = false
			if err != nil {
				m.siteError(err.Error())
				return
			}
			if path == "" {
				return
			}
			i := slices.Index(m.config.Sites, site)
			if i < 0 || m.actionBusy() {
				m.siteError("The Site changed. Try locating it again.")
				return
			}
			if slices.ContainsFunc(m.config.Sites, func(s Site) bool { return s.ID != id && s.Path == replacement.Path }) {
				m.siteError("That folder is already a Site.")
				return
			}
			c := m.config
			c.Sites, c.Folders = slices.Clone(c.Sites), maps.Clone(c.Folders)
			if c.Folders == nil {
				c.Folders = map[string]FolderReference{}
			}
			c.Sites[i], c.Folders[id] = replacement, reference
			c = preserveRenamedSiteOrder(m.config, c)
			if err := m.saveForTargets(c, nil); err != nil {
				m.siteError(err.Error())
				return
			}
			delete(m.folders.missing, id)
			m.updateSiteFolder(replacement)
			m.siteError("")
			m.broadcast()
		})
	}()
}

func (m *siteManager) registerFolderEvents() {
	m.app.Event.On("devjay:locate-folder", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		if id, ok := e.Data.(string); ok {
			m.send(func() { m.locateFolder(id) })
		}
	})
}
