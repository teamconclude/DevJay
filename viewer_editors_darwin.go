package main

import (
	"context"
	"errors"
	"os"
	"time"
)

func (m *siteManager) refreshViewerEditor(id string) {
	r := m.viewers[id]
	if r == nil || r.state.Loading {
		return
	}
	v := *r.state
	choice, useProject := m.config.PreferredEditor, m.config.useProjectEditor()
	r.editorGeneration++
	generation := r.editorGeneration
	go func() {
		apps := installedEditors(choice)
		home, _ := os.UserHomeDir()
		if useProject {
			markOpenProjects(apps, readIDEDiscovery(home), v.root)
		}
		editor, note := resolveEditor(apps, choice, useProject)
		_, err := selectedEditorFile(&v)
		state := &EditorState{Apps: apps, Choice: choice, Resolved: editor, Note: note, CanOpen: err == nil && editor != nil}
		if err != nil {
			state.Note = err.Error()
		}
		m.send(func() {
			if m.viewers[id] != r || r.editorGeneration != generation || r.state.Version != v.Version {
				return
			}
			next := *r.state
			state.Busy = r.editorOpening
			next.Editor = state
			r.state = &next
			m.emitViewer(id)
		})
	}()
}
func (m *siteManager) viewerEditorError(id string, err error) {
	r := m.viewers[id]
	if r == nil {
		return
	}
	v := *r.state
	state := EditorState{}
	if v.Editor != nil {
		state = *v.Editor
	}
	state.Busy = false
	state.Error = err.Error()
	v.Editor = &state
	r.state = &v
	m.emitViewer(id)
}
func (m *siteManager) openViewerFile(id string) {
	r := m.viewers[id]
	if r == nil || r.state.Loading || r.editorOpening {
		return
	}
	v := *r.state
	file, err := selectedEditorFile(&v)
	if err != nil {
		m.viewerEditorError(id, err)
		return
	}
	// Pin the explicitly displayed editor. Do not change applications silently if
	// another IDE opens the project between displaying the button and clicking it.
	if v.Editor == nil || v.Editor.Resolved == nil {
		return
	}
	app := v.Editor.Resolved.Path
	r.editorOpening = true
	state := *v.Editor
	state.Busy = true
	state.Error = ""
	v.Editor = &state
	r.state = &v
	m.emitViewer(id)
	go func() {
		ctx, cancel := context.WithTimeout(m.app.Context(), 20*time.Second)
		defer cancel()
		// Revalidate the path immediately before giving it to another application.
		current, err := selectedEditorFile(&v)
		if err == nil && current != file {
			err = errors.New("The file changed location. Select it again.")
		}
		if err == nil {
			err = openFileInEditor(ctx, file, app)
		}
		m.send(func() {
			if m.viewers[id] != r {
				return
			}
			r.editorOpening = false
			if r.state.Version != v.Version {
				m.refreshViewerEditor(id)
				return
			}
			if err != nil {
				m.viewerEditorError(id, err)
			} else {
				m.refreshViewerEditor(id)
			}
		})
	}()
}
