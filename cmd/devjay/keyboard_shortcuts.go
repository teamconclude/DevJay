package main

import (
	"fmt"
	"maps"
	"strings"
)

// Modifier bits are independent of Carbon/AppKit: Control, Option, Shift, Command.
type ShortcutBinding struct {
	KeyCode   int    `json:"keyCode"`
	Modifiers uint32 `json:"modifiers"`
}
type shortcutDefinition struct {
	ID, Title string
	Default   ShortcutBinding
}

var shortcutDefinitions = []shortcutDefinition{
	{"compact-strip", "Toggle compact strip", ShortcutBinding{46, 11}},
	{"site-1", "Go to Site 1", ShortcutBinding{18, 11}},
	{"site-2", "Go to Site 2", ShortcutBinding{19, 11}},
	{"site-3", "Go to Site 3", ShortcutBinding{20, 11}},
	{"site-4", "Go to Site 4", ShortcutBinding{21, 11}},
	{"site-5", "Go to Site 5", ShortcutBinding{23, 11}},
	{"site-6", "Go to Site 6", ShortcutBinding{22, 11}},
	{"site-7", "Go to Site 7", ShortcutBinding{26, 11}},
	{"site-8", "Go to Site 8", ShortcutBinding{28, 11}},
	{"site-9", "Go to Site 9", ShortcutBinding{25, 11}},
	{"show-strip", "Restore full strip", ShortcutBinding{29, 11}},
	{"toggle-strip", "Hide/show strip", ShortcutBinding{2, 11}},
	{"toggle-sites", "Hide/show Site windows", ShortcutBinding{1, 11}},
}
var shortcutKeyNames = map[int]string{
	0: "A", 1: "S", 2: "D", 3: "F", 4: "H", 5: "G", 6: "Z", 7: "X", 8: "C", 9: "V", 11: "B", 12: "Q", 13: "W", 14: "E", 15: "R", 16: "Y", 17: "T",
	18: "1", 19: "2", 20: "3", 21: "4", 22: "6", 23: "5", 24: "=", 25: "9", 26: "7", 27: "−", 28: "8", 29: "0", 30: "]", 31: "O", 32: "U", 33: "[", 34: "I", 35: "P",
	36: "↩", 37: "L", 38: "J", 39: "′", 40: "K", 41: ";", 42: "\\", 43: ",", 44: "/", 45: "N", 46: "M", 47: ".", 48: "⇥", 49: "Space", 50: "`", 51: "⌫",
	64: "F17", 79: "F18", 80: "F19", 90: "F20", 96: "F5", 97: "F6", 98: "F7", 99: "F3", 100: "F8", 101: "F9", 103: "F11", 105: "F13", 106: "F16", 107: "F14", 109: "F10", 111: "F12", 113: "F15", 114: "Help", 115: "Home", 116: "PgUp", 117: "⌦", 118: "F4", 119: "End", 120: "F2", 121: "PgDn", 122: "F1", 123: "←", 124: "→", 125: "↓", 126: "↑",
}

func shortcutIndex(id string) int {
	for i, d := range shortcutDefinitions {
		if d.ID == id {
			return i
		}
	}
	return -1
}
func resolvedShortcuts(overrides map[string]ShortcutBinding) []ShortcutBinding {
	out := make([]ShortcutBinding, len(shortcutDefinitions))
	for i, d := range shortcutDefinitions {
		out[i] = d.Default
		if b, ok := overrides[d.ID]; ok {
			out[i] = b
		}
	}
	return out
}
func shortcutLabel(b ShortcutBinding) string {
	label := ""
	for _, m := range []struct {
		bit    uint32
		symbol string
	}{{1, "⌃"}, {2, "⌥"}, {4, "⇧"}, {8, "⌘"}} {
		if b.Modifiers&m.bit != 0 {
			label += m.symbol
		}
	}
	return label + shortcutKeyNames[b.KeyCode]
}
func validateShortcuts(overrides map[string]ShortcutBinding) error {
	for id, b := range overrides {
		if shortcutIndex(id) < 0 {
			return fmt.Errorf("Unknown shortcut: %s.", id)
		}
		if shortcutKeyNames[b.KeyCode] == "" {
			return userMessage("Choose a letter, number, punctuation, navigation or function key. Escape cancels recording.")
		}
		if b.Modifiers&^uint32(15) != 0 || b.Modifiers&11 == 0 {
			return userMessage("Include Control, Option or Command in the shortcut.")
		}
		if b.Modifiers == 8 && (b.KeyCode == 12 || b.KeyCode == 43 || b.KeyCode == 29 || (b.KeyCode >= 18 && b.KeyCode <= 28 && b.KeyCode != 24 && b.KeyCode != 27)) {
			return userMessage("That combination is reserved for a DevJay menu command. Add another modifier.")
		}
	}
	seen := map[ShortcutBinding]string{}
	for i, b := range resolvedShortcuts(overrides) {
		if title, ok := seen[b]; ok {
			return fmt.Errorf("%s would be used by both %s and %s. Choose another combination.", shortcutLabel(b), title, shortcutDefinitions[i].Title)
		}
		seen[b] = shortcutDefinitions[i].Title
	}
	return nil
}
func updatedShortcutOverrides(current map[string]ShortcutBinding, id string, b ShortcutBinding, reset bool) map[string]ShortcutBinding {
	next := maps.Clone(current)
	if next == nil {
		next = map[string]ShortcutBinding{}
	}
	if reset || b == shortcutDefinitions[shortcutIndex(id)].Default {
		delete(next, id)
	} else {
		next[id] = b
	}
	return next
}

// Register before saving. Conflicts and persistence failures restore the previous
// registrations and never publish a new configuration. The returned mask also
// reports any old binding that could not be reacquired during rollback.
func commitShortcutChange(current SiteConfig, overrides map[string]ShortcutBinding, changed uint32, configure func([]ShortcutBinding) uint32, save func(SiteConfig) error) (uint32, error) {
	if err := validateShortcuts(overrides); err != nil {
		return 0, err
	}
	previous, next := resolvedShortcuts(current.Shortcuts), resolvedShortcuts(overrides)
	failed := configure(next)
	if failed&changed != 0 {
		restored := configure(previous)
		return restored, userMessage("That shortcut is already in use or unavailable in macOS. Your previous shortcuts are unchanged.")
	}
	candidate := current
	candidate.Shortcuts = overrides
	if err := save(candidate); err != nil {
		return configure(previous), err
	}
	return failed, nil
}
func shortcutFailureMessage(failed uint32, supplied ...[]ShortcutBinding) string {
	bindings := resolvedShortcuts(nil)
	if len(supplied) > 0 {
		bindings = supplied[0]
	}
	var keys []string
	for i, b := range bindings {
		if failed&(1<<i) != 0 {
			keys = append(keys, shortcutLabel(b))
		}
	}
	if len(keys) == 0 {
		return ""
	}
	return "Global shortcuts unavailable: " + strings.Join(keys, ", ") + ". Another app or DevJay copy may be using them. Record another combination or free it and choose Retry."
}
