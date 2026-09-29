package main

import (
	"reflect"
	"testing"
)

func TestKnownAgentRefreshRejectsChangedIdentityAndAssociation(t *testing.T) {
	known := observedAgentProcess{Name: "Claude Code", PID: 123, Started: 456, Cwd: "/work/site", Ancestors: []int{50, 20, 1}, HostBundleID: "com.googlecode.iterm2", Waiting: true, Working: true}
	for _, test := range []struct {
		name   string
		change func(*observedAgentProcess)
	}{
		{"pid reuse", func(p *observedAgentProcess) { p.Started++ }},
		{"new pid", func(p *observedAgentProcess) { p.PID++ }},
		{"zero start", func(p *observedAgentProcess) { p.Started = 0 }},
		{"new executable", func(p *observedAgentProcess) { p.Name = "Codex" }},
		{"changed cwd", func(p *observedAgentProcess) { p.Cwd = "/work/other" }},
		{"reparented", func(p *observedAgentProcess) { p.Ancestors = []int{51, 20, 1} }},
		{"changed host", func(p *observedAgentProcess) { p.HostBundleID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := known
			test.change(&changed)
			if got := verifiedRefreshedAgents([]observedAgentProcess{known}, []observedAgentProcess{changed}); len(got) != 0 {
				t.Fatalf("changed process accepted: %+v", got)
			}
		})
	}
	got := verifiedRefreshedAgents([]observedAgentProcess{known}, []observedAgentProcess{known, known})
	if len(got) != 1 || got[0].Waiting || got[0].Working || !reflect.DeepEqual(got[0].Ancestors, known.Ancestors) {
		t.Fatalf("refresh must deduplicate identities and clear stale status: %+v", got)
	}
	if got := verifiedRefreshedAgents([]observedAgentProcess{known}, nil); len(got) != 0 {
		t.Fatal("exited process retained")
	}
}
