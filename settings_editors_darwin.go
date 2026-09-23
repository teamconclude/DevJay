package main

import (
	"encoding/json"
	"errors"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) emitEditorSettings() {
	if m.recovery.paused {
		return
	}
	m.app.Event.Emit("devjay:editor-settings-state", struct {
		Editor *EditorState `json:"editor"`
	}{m.editorState})
}

func (m *siteManager) refreshSettingsEditor() {
	m.editorGeneration++
	generation := m.editorGeneration
	choice, useProject := m.config.PreferredEditor, m.config.useProjectEditor()
	m.editorState = nil
	m.emitEditorSettings()
	go func() {
		apps := installedEditors(choice)
		m.send(func() {
			if m.editorGeneration != generation {
				return
			}
			m.editorState = &EditorState{Apps: apps, Choice: choice, UseProject: useProject, Busy: m.editorBrowsing}
			m.emitEditorSettings()
		})
	}()
}

func (m *siteManager) editorSettingsError(err error) {
	state := EditorState{}
	if m.editorState != nil {
		state = *m.editorState
	}
	state.Busy = false
	state.Error = err.Error()
	m.editorState = &state
	m.emitEditorSettings()
}

func (m *siteManager) saveEditorPreference(choice string, useProject bool) error {
	c := m.config
	c.PreferredEditor, c.UseProjectEditor = choice, &useProject
	if err := m.save(c); err != nil {
		return err
	}
	for id := range m.viewers {
		m.refreshViewerEditor(id)
	}
	m.refreshSettingsEditor()
	return nil
}

func (m *siteManager) browseSettingsEditor() {
	if m.editorBrowsing {
		return
	}
	m.editorBrowsing = true
	if m.editorState != nil {
		state := *m.editorState
		state.Busy = true
		m.editorState = &state
		m.emitEditorSettings()
	}
	go func() {
		path, err := m.app.Dialog.OpenFile().SetTitle("Choose an editor").SetButtonText("Choose editor").CanChooseFiles(true).CanChooseDirectories(false).AddFilter("Applications", "*.app").AttachToWindow(m.settings).PromptForSingleSelection()
		choice := ""
		if err == nil && path != "" {
			path = canonicalPath(path)
			if filepath.Ext(path) == ".app" {
				for _, app := range installedEditors(path) {
					if app.Path == path {
						choice = path
						break
					}
				}
			}
			if choice == "" {
				err = errors.New("Choose an installed application (.app).")
			}
		}
		m.send(func() {
			m.editorBrowsing = false
			if err == nil && choice != "" {
				err = m.saveEditorPreference(choice, m.config.useProjectEditor())
			}
			if err != nil {
				m.editorSettingsError(err)
			} else if choice == "" {
				m.refreshSettingsEditor()
			}
		})
	}()
}

func (m *siteManager) registerEditorSettingsEvents() {
	m.app.Event.On("devjay:editor-settings-request", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		var req struct {
			Operation  string `json:"operation"`
			Editor     string `json:"editor"`
			UseProject bool   `json:"useProject"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		m.send(func() {
			if m.editorBrowsing {
				return
			}
			if req.Operation == "refresh" {
				m.refreshSettingsEditor()
				return
			}
			if m.editorState == nil {
				return
			}
			switch req.Operation {
			case "browse":
				m.browseSettingsEditor()
			case "automatic":
				if err := m.saveEditorPreference(m.config.PreferredEditor, req.UseProject); err != nil {
					m.editorSettingsError(err)
				}
			case "select":
				valid := req.Editor == ""
				for _, app := range m.editorState.Apps {
					if app.Path == req.Editor {
						valid = true
						break
					}
				}
				if !valid {
					m.emitEditorSettings()
					return
				}
				if err := m.saveEditorPreference(req.Editor, m.config.useProjectEditor()); err != nil {
					m.editorSettingsError(err)
				}
			}
		})
	})
}
