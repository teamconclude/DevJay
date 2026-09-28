package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDevelopmentAgentsRequireFolderAndOwningApp(t *testing.T) {
	root := t.TempDir()
	one, two, similar := filepath.Join(root, "c1"), filepath.Join(root, "c2"), filepath.Join(root, "c10")
	for _, path := range []string{one, two, similar, filepath.Join(one, "src")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	targets := map[string]developmentAppTarget{"one": {SitePath: one}, "two": {SitePath: two}}
	app := observedDevelopmentApp{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland", Icon: "data:image/png;base64,fixture", WindowCount: 2}, Kind: "editor", ProcessIDs: []int{42}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {app}, "two": {app}, "empty": {}}}
	processes := []observedAgentProcess{
		{Name: "Codex", Cwd: one, Ancestors: []int{90, 42, 1}},
		{Name: "Claude Code", Cwd: filepath.Join(one, "src"), Ancestors: []int{42}},
		{Name: "Codex", Cwd: one, Ancestors: []int{42}},           // Wrapper/native children collapse to one name.
		{Name: "Claude Code", Cwd: two, Ancestors: []int{99, 1}},  // Detached/unrelated app is not this host.
		{Name: "Claude Code", Cwd: similar, Ancestors: []int{42}}, // c10 is not inside c1.
		{Name: "Unknown", Cwd: two, Ancestors: []int{42}},
		{Name: "Codex", Cwd: "c2", Ancestors: []int{42}},
		{Name: "Codex", Cwd: filepath.Join(two, "missing"), Ancestors: []int{42}},
	}
	result := matchDevelopmentAgents(targets, windows, processes)
	if got := result.Sites["one"][0]; got.Icon != app.Icon || got.WindowCount != 2 {
		t.Fatal("agent matching lost the app icon or window count", got)
	}
	if got := result.Sites["one"][0].Agents; !reflect.DeepEqual(got, []string{"Claude Code", "Codex"}) {
		t.Fatalf("one: %v", got)
	}
	if got := result.Sites["two"][0].Agents; len(got) != 0 {
		t.Fatalf("unrelated desktop: %v", got)
	}
	if _, ok := result.Sites["empty"]; !ok {
		t.Fatal("known empty desktop lost")
	}
	if len(app.Agents) != 0 {
		t.Fatal("matching mutated its input")
	}
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"processIDs", "ancestors", "cwd", root} {
		if strings.Contains(string(data), private) {
			t.Fatalf("intermediate process metadata leaked into snapshot: %s", private)
		}
	}
	// A subsequent scan with no agents removes all previous labels.
	if got := matchDevelopmentAgents(targets, windows, nil).Sites["one"][0].Agents; len(got) != 0 {
		t.Fatal(got)
	}
}

func TestDevelopmentAgentsPreferNestedSiteAndResolveSymlinks(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(child, alias); err != nil {
		t.Fatal(err)
	}
	targets := map[string]developmentAppTarget{"parent": {SitePath: root}, "child": {SitePath: alias}}
	app := observedDevelopmentApp{DevelopmentApp: DevelopmentApp{Name: "Terminal"}, Kind: "terminal", ProcessIDs: []int{42}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"parent": {app}, "child": {app}}}
	processes := []observedAgentProcess{{Name: "Claude Code", Cwd: child, Ancestors: []int{42}}}
	result := matchDevelopmentAgents(targets, windows, processes)
	if len(result.Sites["child"][0].Agents) != 1 || len(result.Sites["parent"][0].Agents) != 0 {
		t.Fatal(result)
	}
	delete(windows.Sites, "child")
	if len(matchDevelopmentAgents(targets, windows, processes).Sites["parent"][0].Agents) != 0 {
		t.Fatal("unknown child desktop fell back to parent")
	}
	// Aliased duplicate folders cannot be resolved to one Site.
	targets["duplicate"] = developmentAppTarget{SitePath: child}
	windows.Sites["child"] = []observedDevelopmentApp{app}
	if len(matchDevelopmentAgents(targets, windows, processes).Sites["child"][0].Agents) != 0 {
		t.Fatal("ambiguous folder assigned")
	}
}

func TestDevelopmentAgentsPersistentITermAndDesktopAppExclusions(t *testing.T) {
	root := t.TempDir()
	targets := map[string]developmentAppTarget{"one": {SitePath: root}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {
		{DevelopmentApp: DevelopmentApp{Name: "iTerm2", BundleID: "com.googlecode.iterm2"}, Kind: "terminal", ProcessIDs: []int{42}},
		{DevelopmentApp: DevelopmentApp{Name: "Terminal", BundleID: "com.apple.Terminal"}, Kind: "terminal", ProcessIDs: []int{43}},
		{DevelopmentApp: DevelopmentApp{Name: "ChatGPT", BundleID: "com.openai.codex"}, Kind: "assistant", ProcessIDs: []int{44}},
	}}}
	processes := []observedAgentProcess{
		{Name: "Claude Code", Cwd: root, Ancestors: []int{90, 1}, HostBundleID: "com.googlecode.iterm2"},
		{Name: "Codex", Cwd: root, Ancestors: []int{44}},
		{Name: "Codex", Cwd: root, HostBundleID: "com.apple.Terminal"},
	}
	result := matchDevelopmentAgents(targets, windows, processes)
	if !reflect.DeepEqual(result.Sites["one"][0].Agents, []string{"Claude Code"}) {
		t.Fatal(result)
	}
	if len(result.Sites["one"][1].Agents) != 0 || len(result.Sites["one"][2].Agents) != 0 {
		t.Fatal("helper attributed to unrelated terminal or desktop app")
	}
	windows.Available = false
	if len(matchDevelopmentAgents(targets, windows, processes).Sites["one"][0].Agents) != 0 {
		t.Fatal("unavailable windows acquired agents")
	}
}
