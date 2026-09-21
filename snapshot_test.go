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
		func(s *appSnapshot) { s.SettingsOpen = true },
	} {
		next := s
		next.Sites = append([]SiteView(nil), s.Sites...)
		change(&next)
		if snapshotSignature(next) == first {
			t.Fatal("visible change suppressed")
		}
	}
}
