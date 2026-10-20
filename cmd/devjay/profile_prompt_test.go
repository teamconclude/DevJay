package main

import "testing"

func TestProfilePromptIgnoresObsoleteChoice(t *testing.T) {
	m := siteManager{profiles: profileMonitor{unknown: "current-displays"}, settingsPage: "sites"}
	for _, signature := range []string{"", "disconnected-displays"} {
		m.handleProfilePrompt(signature)
		if m.settingsPage != "sites" {
			t.Fatal("obsolete Profile choice opened Settings")
		}
	}
}
