//go:build darwin

package main

import "testing"

func TestSnapshotSignatureIgnoresPollTimeButRetainsVisibleChanges(t *testing.T) {
	s := appSnapshot{Sites: []SiteView{{Site: Site{ID: "one"}, Git: GitStatus{Branch: "main", Checked: "first"}}}}
	first := snapshotSignature(s)
	if s.Sites[0].Git.Checked != "first" {
		t.Fatal("signature mutated snapshot")
	}
	s.Sites[0].Git.Checked = "second"
	if snapshotSignature(s) != first {
		t.Fatal("poll timestamp creates a broadcast")
	}
	for _, change := range []func(*appSnapshot){
		func(s *appSnapshot) { s.Sites[0].Git.LocalNote = "Status check timed out · cached" },
		func(s *appSnapshot) { s.Sites[0].Git.Reviews = []ReviewFindings{{Number: 1, Blockers: 1}} },
		func(s *appSnapshot) { s.Sites[0].Window.OnDesktop = true },
		func(s *appSnapshot) {
			s.Sites[0].DevelopmentApps = []DevelopmentApp{{Name: "iTerm2", BundleID: "com.googlecode.iterm2"}}
		},
		func(s *appSnapshot) {
			s.Sites[0].DevelopmentApps = []DevelopmentApp{{Name: "iTerm2", BundleID: "com.googlecode.iterm2", Agents: []string{"Claude Code"}}}
		},
		func(s *appSnapshot) { s.Sites[0].DevelopmentAppsKnown = true },
		func(s *appSnapshot) { s.SettingsOpen = true },
		func(s *appSnapshot) { s.Preferences = map[string]bool{"showIDEStatus": false} },
	} {
		next := s
		next.Sites = append([]SiteView(nil), s.Sites...)
		change(&next)
		if snapshotSignature(next) == first {
			t.Fatal("visible change suppressed")
		}
	}
}

func TestSnapshotSignatureRetainsWaitingOnlyChanges(t *testing.T) {
	s := appSnapshot{Sites: []SiteView{{Site: Site{ID: "one"}, DevelopmentApps: []DevelopmentApp{
		{Name: "GoLand", BundleID: "com.jetbrains.goland", Agents: []string{"Claude Code", "Codex"}},
	}}}}
	before := snapshotSignature(s)
	s.Sites[0].DevelopmentApps[0].WaitingAgents = []string{"Claude Code"}
	claude := snapshotSignature(s)
	s.Sites[0].DevelopmentApps[0].WaitingAgents = []string{"Codex"}
	if codex := snapshotSignature(s); claude == before || codex == before || codex == claude {
		t.Fatal("a change in which agent needs input must publish a new snapshot")
	}
	s.Sites[0].DevelopmentApps[0].WaitingAgents = nil
	if snapshotSignature(s) != before {
		t.Fatal("resuming should restore the ordinary agent display")
	}
}

func TestSnapshotSignatureRetainsWorkingOnlyChanges(t *testing.T) {
	s := appSnapshot{Sites: []SiteView{{Site: Site{ID: "one"}, DevelopmentApps: []DevelopmentApp{
		{Name: "GoLand", BundleID: "com.jetbrains.goland", Agents: []string{"Claude Code", "Codex"}},
	}}}}
	before := snapshotSignature(s)
	s.Sites[0].DevelopmentApps[0].WorkingAgents = []string{"Claude Code"}
	claude := snapshotSignature(s)
	s.Sites[0].DevelopmentApps[0].WorkingAgents = []string{"Codex"}
	if codex := snapshotSignature(s); claude == before || codex == before || codex == claude {
		t.Fatal("a change in which agent is working must publish a new snapshot")
	}
	s.Sites[0].DevelopmentApps[0].WorkingAgents = nil
	if snapshotSignature(s) != before {
		t.Fatal("clearing working should restore the ordinary agent display")
	}
}
