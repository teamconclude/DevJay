package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type EditorApp struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	BundleID    string `json:"bundleID"`
	PIDs        []int  `json:"pids,omitempty"`
	ProjectOpen bool   `json:"projectOpen,omitempty"`
}
type EditorState struct {
	UseProject bool        `json:"useProject"`
	Apps       []EditorApp `json:"apps"`
	Choice     string      `json:"choice"`
	Resolved   *EditorApp  `json:"resolved,omitempty"`
	CanOpen    bool        `json:"canOpen"`
	Busy       bool        `json:"busy"`
	Note       string      `json:"note"`
	Error      string      `json:"error,omitempty"`
}
type ideDiscovery struct {
	PID   int `json:"pid"`
	Paths struct {
		InstallDir string `json:"installDir"`
	} `json:"paths"`
	Properties struct {
		OpenProjectPaths []string `json:"openProjectPaths"`
	} `json:"properties"`
}

func canonicalPath(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(resolved)
}

// Discovery is an optional JetBrains implementation detail. Require a matching
// live PID AND application path; a leftover discovery file is not a live IDE.
func markOpenProjects(apps []EditorApp, records []ideDiscovery, project string) {
	project = canonicalPath(project)
	if project == "" {
		return
	}
	for i := range apps {
		app := &apps[i]
		appPath := canonicalPath(app.Path)
		if appPath == "" || !strings.HasPrefix(strings.ToLower(app.BundleID), "com.jetbrains.") {
			continue
		}
		for _, record := range records {
			if !slices.Contains(app.PIDs, record.PID) || record.PID <= 0 ||
				canonicalPath(filepath.Dir(record.Paths.InstallDir)) != appPath {
				continue
			}
			for _, path := range record.Properties.OpenProjectPaths {
				if canonicalPath(path) == project {
					app.ProjectOpen = true
				}
			}
		}
	}
}
func readIDEDiscovery(home string) []ideDiscovery {
	files, _ := filepath.Glob(filepath.Join(home, "Library", "Application Support", "JetBrains", "discovery", "*-ide-instance.json"))
	var records []ideDiscovery
	for i, path := range files {
		if i >= 100 {
			break
		}
		file, err := os.Open(path)
		if err != nil {
			continue
		}
		var record ideDiscovery
		err = json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&record)
		file.Close()
		if err == nil {
			records = append(records, record)
		}
	}
	return records
}

// Project detection is optional; the user's global preference is the fallback.
func resolveEditor(apps []EditorApp, choice string, useProject bool) (*EditorApp, string) {
	var preferred *EditorApp
	var matches []EditorApp
	for _, app := range apps {
		if app.Path == choice {
			preferred = &app
		}
		if useProject && app.ProjectOpen {
			matches = append(matches, app)
		}
	}
	if len(matches) == 1 {
		return &matches[0], "Uses the IDE reporting this project open."
	}
	if preferred != nil {
		return preferred, ""
	}
	if choice != "" {
		return nil, "The preferred editor is unavailable. Choose another in Settings."
	}
	if len(matches) > 1 {
		return nil, "More than one IDE has this project open. Choose a preferred editor in Settings."
	}
	return nil, "Choose a preferred editor in Settings."
}

// Only open the selected server-owned path in the CURRENT working tree. Never
// materialize an old revision, create deleted files, or follow a link outside it.
func selectedEditorFile(v *GitView) (string, error) {
	if v.Loading || v.File < 0 || v.File >= len(v.Files) || v.root == "" {
		return "", errors.New("Select an available file first.")
	}
	path := v.Files[v.File].Path
	if !filepath.IsLocal(path) || strings.ContainsRune(path, 0) {
		return "", errors.New("This file is outside the checkout.")
	}
	root := canonicalPath(v.root)
	if root == "" {
		return "", errors.New("The checkout is unavailable.")
	}
	file, err := filepath.EvalSymlinks(filepath.Join(root, path))
	if err != nil {
		return "", errors.New("This file is not present in the current checkout.")
	}
	relative, err := filepath.Rel(root, file)
	if err != nil || !filepath.IsLocal(relative) {
		return "", errors.New("This file points outside the checkout. Open it manually.")
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("Only regular files in the current checkout can be opened.")
	}
	return file, nil
}
