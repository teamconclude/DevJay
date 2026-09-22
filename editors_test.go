package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSelectedEditorFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "-a file.go")
	if err := os.WriteFile(path, []byte("current contents"), 0600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "external.go")
	if err := os.WriteFile(outside, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"inside": path, "outside": outside} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		path           string
		loading, valid bool
	}{
		{"-a file.go", false, true}, {"inside", false, true}, {"outside", false, false},
		{"deleted", false, false}, {"../external.go", false, false}, {outside, false, false},
		{".", false, false}, {"-a file.go", true, false}, {"bad\x00path", false, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			v := &GitView{root: root, Files: []ViewFile{{Path: tc.path}}, File: 0, Loading: tc.loading, Kind: "remote"}
			got, err := selectedEditorFile(v)
			if (err == nil) != tc.valid {
				t.Fatalf("path %q: %q %v", tc.path, got, err)
			}
			if tc.valid && got != canonicalPath(path) {
				t.Fatalf("wrong file: %q", got)
			}
		})
	}
	if _, err := selectedEditorFile(&GitView{root: root, File: -1}); err == nil {
		t.Fatal("missing selection accepted")
	}
	b, _ := os.ReadFile(path)
	if string(b) != "current contents" {
		t.Fatal("file modified")
	}
	if _, err := os.Stat(filepath.Join(root, "deleted")); !os.IsNotExist(err) {
		t.Fatal("deleted file created")
	}
}

func TestEditorProjectMatching(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "GoLand.app")
	if err := os.MkdirAll(filepath.Join(app, "Contents"), 0700); err != nil {
		t.Fatal(err)
	}
	project := t.TempDir()
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Fatal(err)
	}
	record := ideDiscovery{PID: 42}
	record.Paths.InstallDir = filepath.Join(app, "Contents")
	record.Properties.OpenProjectPaths = []string{alias}
	for _, tc := range []struct {
		name                 string
		pid                  int
		app, project, bundle string
		want                 bool
	}{
		{"canonical exact project", 42, app, project, "com.jetbrains.GoLand", true},
		{"stale process", 43, app, project, "com.jetbrains.GoLand", false},
		{"other application", 42, root, project, "com.jetbrains.GoLand", false},
		{"different checkout", 42, app, t.TempDir(), "com.jetbrains.GoLand", false},
		{"other editor", 42, app, project, "com.microsoft.VSCode", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			apps := []EditorApp{{Path: tc.app, BundleID: tc.bundle, PIDs: []int{tc.pid}}}
			markOpenProjects(apps, []ideDiscovery{record}, tc.project)
			if apps[0].ProjectOpen != tc.want {
				t.Fatal(apps)
			}
		})
	}
}

func TestResolveEditor(t *testing.T) {
	apps := []EditorApp{{Name: "GoLand", Path: "/g", ProjectOpen: true}, {Name: "Code", Path: "/c"}}
	for _, tc := range []struct {
		name, choice string
		auto         bool
		want         string
	}{
		{"auto overrides preference", "/c", true, "/g"},
		{"auto without preference", "", true, "/g"},
		{"detection disabled", "/c", false, "/c"},
		{"missing explicit editor", "/missing", false, ""},
		{"no preference and disabled", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := resolveEditor(apps, tc.choice, tc.auto)
			path := ""
			if got != nil {
				path = got.Path
			}
			if path != tc.want {
				t.Fatalf("got %q want %q", path, tc.want)
			}
		})
	}
	apps[0].ProjectOpen = false
	if got, _ := resolveEditor(apps, "/c", true); got == nil || got.Path != "/c" {
		t.Fatal("no project match must use preferred editor")
	}
	apps[0].ProjectOpen = true
	apps[1].ProjectOpen = true
	if got, _ := resolveEditor(apps, "/c", true); got == nil || got.Path != "/c" {
		t.Fatal("ambiguous projects must use preferred editor")
	}
	if got, _ := resolveEditor(apps, "", true); got != nil {
		t.Fatal("ambiguous projects without preference must not guess")
	}
}

func TestEditorPreferencePersistence(t *testing.T) {
	if !(SiteConfig{}).useProjectEditor() {
		t.Fatal("automatic detection should default on")
	}
	for _, enabled := range []bool{false, true} {
		config := SiteConfig{Version: 1, PreferredEditor: "/Applications/Editor.app", UseProjectEditor: &enabled}
		path := filepath.Join(t.TempDir(), "sites.json")
		if err := saveSites(path, config); err != nil {
			t.Fatal(err)
		}
		loaded, err := loadSites(path)
		if err != nil || loaded.PreferredEditor != config.PreferredEditor || loaded.useProjectEditor() != enabled {
			t.Fatalf("preference lost: %+v %v", loaded, err)
		}
	}
}

func TestLegacyEditorPreferences(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{`{"version":1,"editors":{"one":"/a","two":"/a"}}`, "/a"},
		{`{"version":1,"editors":{"one":"/a","two":"/b"}}`, ""},
		{`{"version":1,"preferredEditor":"/new","editors":{"one":"/old"}}`, "/new"},
	} {
		path := filepath.Join(t.TempDir(), "sites.json")
		if err := os.WriteFile(path, []byte(tc.body), 0600); err != nil {
			t.Fatal(err)
		}
		c, err := loadSites(path)
		if err != nil || c.PreferredEditor != tc.want {
			t.Fatalf("migration %+v %v", c, err)
		}
	}
}

func TestDiscoveryMissingAndMalformed(t *testing.T) {
	home := t.TempDir()
	if got := readIDEDiscovery(home); len(got) != 0 {
		t.Fatal(got)
	}
	dir := filepath.Join(home, "Library", "Application Support", "JetBrains", "discovery")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"bad-ide-instance.json": "{broken",
		"42-ide-instance.json":  `{"pid":42,"paths":{"installDir":"/Applications/GoLand.app/Contents"},"properties":{"openProjectPaths":["/projects/c1"]}}`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	got := readIDEDiscovery(home)
	if len(got) != 1 || got[0].PID != 42 || len(got[0].Properties.OpenProjectPaths) != 1 {
		t.Fatal(got)
	}
}
