package main

import (
	"errors"
	"maps"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestShortcutConfigurationValidationAndPersistence(t *testing.T) {
	defaults := resolvedShortcuts(nil)
	if len(defaults) != 13 || shortcutLabel(defaults[11]) != "⌃⌥⌘D" {
		t.Fatal(defaults)
	}
	valid := map[string]ShortcutBinding{"toggle-strip": {2, 15}, "site-1": {122, 9}}
	if err := validateShortcuts(valid); err != nil {
		t.Fatal(err)
	}
	for name, value := range map[string]map[string]ShortcutBinding{
		"unknown": {"missing": {0, 11}}, "key": {"toggle-strip": {999, 11}}, "escape": {"toggle-strip": {53, 11}},
		"typing": {"toggle-strip": {2, 0}}, "shift-only": {"toggle-strip": {2, 4}}, "unknown-modifier": {"toggle-strip": {2, 16}},
		"duplicate-default": {"toggle-strip": {1, 11}}, "duplicate-custom": {"toggle-strip": {2, 15}, "toggle-sites": {2, 15}},
		"menu-quit": {"toggle-strip": {12, 8}}, "menu-site": {"toggle-strip": {18, 8}},
	} {
		t.Run(name, func(t *testing.T) {
			if validateShortcuts(value) == nil {
				t.Fatal("accepted invalid binding")
			}
		})
	}
	cfg := SiteConfig{Version: 1, Shortcuts: valid, Layout: "vertical"}
	path := filepath.Join(t.TempDir(), "sites.json")
	if err := saveSites(path, cfg); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSites(path)
	if err != nil || !reflect.DeepEqual(loaded.Shortcuts, valid) {
		t.Fatal(loaded, err)
	}
	switched := applyProfileSettings(loaded, ProfileSettings{Layout: "horizontal"})
	if !reflect.DeepEqual(switched.Shortcuts, valid) {
		t.Fatal("profile replaced global bindings")
	}
	reset := updatedShortcutOverrides(valid, "toggle-strip", ShortcutBinding{}, true)
	if _, ok := reset["toggle-strip"]; ok || len(valid) != 2 || reset["site-1"] != valid["site-1"] {
		t.Fatal("reset changed other shortcuts")
	}
	// Invalid persisted bindings are reported instead of silently overwritten.
	cfg.Shortcuts = map[string]ShortcutBinding{"toggle-strip": {2, 0}}
	if err := saveSites(path, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := loadSites(path); err == nil {
		t.Fatal("invalid file accepted")
	}
}
func TestShortcutChangeRollsBackConflictsAndSaveFailures(t *testing.T) {
	current := SiteConfig{Version: 1, Shortcuts: map[string]ShortcutBinding{"site-1": {122, 9}}}
	next := updatedShortcutOverrides(current.Shortcuts, "toggle-strip", ShortcutBinding{2, 15}, false)
	for _, kind := range []string{"success", "conflict", "save-failure", "rollback-failure"} {
		t.Run(kind, func(t *testing.T) {
			var configurations [][]ShortcutBinding
			saves := 0
			configure := func(bindings []ShortcutBinding) uint32 {
				configurations = append(configurations, bindings)
				if len(configurations) == 1 && (kind == "conflict" || kind == "rollback-failure") {
					return 1 << 11
				}
				if len(configurations) == 2 && kind == "rollback-failure" {
					return 1 << 2
				}
				return 0
			}
			save := func(candidate SiteConfig) error {
				saves++
				if kind == "save-failure" {
					return errors.New("disk full")
				}
				if !reflect.DeepEqual(candidate.Shortcuts, next) {
					t.Fatal("saved wrong bindings")
				}
				return nil
			}
			mask, err := commitShortcutChange(current, next, 1<<11, configure, save)
			if kind == "success" {
				if err != nil || len(configurations) != 1 || saves != 1 || mask != 0 {
					t.Fatal(err, saves, mask)
				}
				return
			}
			if err == nil || len(configurations) != 2 || !reflect.DeepEqual(configurations[1], resolvedShortcuts(current.Shortcuts)) {
				t.Fatal("did not restore previous registrations", err)
			}
			if kind == "conflict" && saves != 0 {
				t.Fatal("persisted conflict")
			}
			if kind == "rollback-failure" && mask != 1<<2 {
				t.Fatal("lost rollback availability", mask)
			}
			if kind == "save-failure" && !strings.Contains(err.Error(), "disk full") {
				t.Fatal(err)
			}
		})
	}
	unchanged := maps.Clone(current.Shortcuts)
	touched := false
	_, err := commitShortcutChange(current, map[string]ShortcutBinding{"toggle-strip": {1, 11}}, 1<<11, func([]ShortcutBinding) uint32 { touched = true; return 0 }, func(SiteConfig) error { touched = true; return nil })
	if err == nil || touched || !reflect.DeepEqual(current.Shortcuts, unchanged) {
		t.Fatal("invalid change reached registration or storage")
	}
}
func TestShortcutViewsAndHintLabelsUseCurrentBindings(t *testing.T) {
	m := &siteManager{config: SiteConfig{Shortcuts: map[string]ShortcutBinding{"site-1": {122, 9}}}, shortcutFailed: 1 << 1, shortcutRecording: "toggle-strip"}
	v := m.shortcutSettingsView()
	if len(v.Items) != 13 || v.Items[0].ID != "toggle-strip" || v.Recording != "toggle-strip" {
		t.Fatal(v)
	}
	for _, item := range v.Items {
		if item.ID == "site-1" && (!item.Custom || !item.Unavailable || item.Label != "⌃⌘F1") {
			t.Fatal(item)
		}
	}
	if m.shortcutLabels()[0] != "F1" {
		t.Fatal("hint still shows ordinal")
	}
	filtered := snapshotForWindow(appSnapshot{ShortcutSettings: v, ShortcutLabels: m.shortcutLabels()}, "site-any", "")
	if filtered.ShortcutSettings != nil || filtered.ShortcutLabels != nil {
		t.Fatal("settings leaked into Site snapshots")
	}
}

func TestShortcutConflictNamesBothActions(t *testing.T) {
	err := validateShortcuts(map[string]ShortcutBinding{"toggle-strip": {1, 11}})
	if err == nil || !strings.Contains(err.Error(), "Hide/show Site windows") || !strings.Contains(err.Error(), "Hide/show strip") {
		t.Fatal(err)
	}
}
