package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestWaitingAgentsFollowVerifiedHostAndClearOnNextReading(t *testing.T) {
	root := t.TempDir()
	targets := map[string]developmentAppTarget{"one": {SitePath: root}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {
		{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}},
		{DevelopmentApp: DevelopmentApp{Name: "ChatGPT", BundleID: "com.openai.codex"}, Kind: "assistant", ProcessIDs: []int{43}},
	}}}
	processes := []observedAgentProcess{
		{Name: "Claude Code", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{42}, Waiting: true},
		{Name: "Claude Code", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{42}},
		{Name: "Codex", PID: 102, Started: 123000002, Cwd: root, Ancestors: []int{43}, Waiting: true},
	}
	result := matchDevelopmentAgents(targets, windows, processes)
	if !reflect.DeepEqual(result.Sites["one"][0].WaitingAgents, []string{"Claude Code"}) || len(result.Sites["one"][1].WaitingAgents) != 0 {
		t.Fatal("waiting requires the same verified editor/terminal association as presence", result)
	}
	data, _ := json.Marshal(result)
	for _, private := range []string{`"pid"`, `"started"`, `"cwd"`, `"ancestors"`, root} {
		if strings.Contains(string(data), private) {
			t.Fatal("waiting metadata leaked into a card", private)
		}
	}
	processes[0].Waiting = false
	result = matchDevelopmentAgents(targets, windows, processes)
	if len(result.Sites["one"][0].WaitingAgents) != 0 || len(result.Sites["one"][0].Agents) != 1 {
		t.Fatal("resuming must remove waiting without hiding the agent", result)
	}
	result = matchDevelopmentAgents(targets, windows, nil)
	if len(result.Sites["one"][0].WaitingAgents) != 0 || len(result.Sites["one"][0].Agents) != 0 {
		t.Fatal("exited processes must remove presence and waiting")
	}
}

func TestCodexWaitingRequiresAnUnambiguousLiveFolderAndHost(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	targets := map[string]developmentAppTarget{"one": {SitePath: root}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {
		{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}},
		{DevelopmentApp: DevelopmentApp{Name: "iTerm2", BundleID: "com.googlecode.iterm2"}, Kind: "terminal", ProcessIDs: []int{43}},
	}}}
	base := observedAgentProcess{Name: "Codex", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{42}}
	waiting := codexWaitingThread{Cwd: root, Source: "vscode", Waiting: true}
	for _, test := range []struct {
		name      string
		processes []observedAgentProcess
		threads   []codexWaitingThread
		want      []bool
	}{
		{"matching", []observedAgentProcess{base}, []codexWaitingThread{waiting}, []bool{true}},
		{"ide-thread-with-only-terminal-candidate", []observedAgentProcess{{Name: "Codex", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{43}}}, []codexWaitingThread{waiting}, []bool{false}},
		{"cli-thread-in-terminal", []observedAgentProcess{{Name: "Codex", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{43}}}, []codexWaitingThread{{Cwd: root, Source: "cli", Waiting: true}}, []bool{true}},
		{"no-status", []observedAgentProcess{base}, nil, []bool{false}},
		{"idle", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: root, Source: "cli"}}, []bool{false}},
		{"unknown-source", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: root, Source: "appServer", Waiting: true}}, []bool{false}},
		{"different-folder", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: other, Source: "cli", Waiting: true}}, []bool{false}},
		{"same-host-sessions", []observedAgentProcess{base, {Name: "Codex", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{42}}}, []codexWaitingThread{waiting}, []bool{true, true}},
		{"competing-hosts", []observedAgentProcess{base, {Name: "Codex", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{43}}}, []codexWaitingThread{waiting}, []bool{false, false}},
		{"unowned-session", []observedAgentProcess{base, {Name: "Codex", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{99}}}, []codexWaitingThread{waiting}, []bool{false, false}},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := range test.processes {
				test.processes[i].Waiting = true // A failed/cleared source must not retain yesterday's flag.
			}
			applyCodexWaiting(targets, windows, test.processes, test.threads)
			for i, want := range test.want {
				if test.processes[i].Waiting != want {
					t.Fatalf("process %d waiting=%v, want %v", i, test.processes[i].Waiting, want)
				}
			}
		})
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	processes := []observedAgentProcess{base}
	applyCodexWaiting(targets, windows, processes, []codexWaitingThread{{Cwd: alias, Source: "cli", Waiting: true}})
	if !processes[0].Waiting {
		t.Fatal("canonical folder aliases should match")
	}
	windows.Available = false
	applyCodexWaiting(targets, windows, processes, []codexWaitingThread{waiting})
	if processes[0].Waiting {
		t.Fatal("unknown desktop membership must clear waiting")
	}
}

func TestWorkingAgentsYieldToWaitingInTheSameHostAndClear(t *testing.T) {
	root := t.TempDir()
	targets := map[string]developmentAppTarget{"one": {SitePath: root}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {
		{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}},
		{DevelopmentApp: DevelopmentApp{Name: "iTerm2", BundleID: "com.googlecode.iterm2"}, Kind: "terminal", ProcessIDs: []int{43}},
		{DevelopmentApp: DevelopmentApp{Name: "ChatGPT", BundleID: "com.openai.codex"}, Kind: "assistant", ProcessIDs: []int{44}},
	}}}
	for _, agent := range []string{"Claude Code", "Codex"} {
		for _, reverse := range []bool{false, true} {
			processes := []observedAgentProcess{
				{Name: agent, Cwd: root, Ancestors: []int{42}, Working: true},
				{Name: agent, Cwd: root, Ancestors: []int{42}, Waiting: true},
				{Name: agent, Cwd: root, Ancestors: []int{43}, Working: true},
				{Name: agent, Cwd: root, Ancestors: []int{44}, Working: true},
			}
			if reverse {
				slices.Reverse(processes)
			}
			result := matchDevelopmentAgents(targets, windows, processes)
			apps := result.Sites["one"]
			if !reflect.DeepEqual(apps[0].WaitingAgents, []string{agent}) || len(apps[0].WorkingAgents) != 0 {
				t.Fatalf("%s reverse=%v: waiting session hidden by working session: %+v", agent, reverse, apps[0])
			}
			if !reflect.DeepEqual(apps[1].WorkingAgents, []string{agent}) || len(apps[1].WaitingAgents) != 0 {
				t.Fatal("a waiting session in another host suppressed this host's processing", apps[1])
			}
			if len(apps[2].Agents) != 0 || len(apps[2].WorkingAgents) != 0 {
				t.Fatal("desktop assistant helpers cannot establish processing", apps[2])
			}
			for i := range processes {
				processes[i].Waiting = false
			}
			resumed := matchDevelopmentAgents(targets, windows, processes)
			if !reflect.DeepEqual(resumed.Sites["one"][0].WorkingAgents, []string{agent}) || len(resumed.Sites["one"][0].WaitingAgents) != 0 {
				t.Fatal("resuming did not replace waiting with processing", resumed)
			}
			for i := range processes {
				processes[i].Working = false
			}
			unknown := matchDevelopmentAgents(targets, windows, processes)
			if len(unknown.Sites["one"][0].WorkingAgents) != 0 || len(unknown.Sites["one"][0].Agents) != 1 {
				t.Fatal("unknown state must clear processing without removing agent presence", unknown)
			}
			if len(matchDevelopmentAgents(targets, windows, nil).Sites["one"][0].Agents) != 0 {
				t.Fatal("an exited process must remove its presence and processing")
			}
		}
	}
}

func TestWorkingAndWaitingForDifferentAgentsRemainIndependent(t *testing.T) {
	root := t.TempDir()
	targets := map[string]developmentAppTarget{"one": {SitePath: root}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {
		{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}},
	}}}
	processes := []observedAgentProcess{
		{Name: "Claude Code", Cwd: root, Ancestors: []int{42}, Waiting: true, Working: true},
		{Name: "Codex", Cwd: root, Ancestors: []int{42}, Working: true},
		{Name: "Codex", Cwd: root, Ancestors: []int{42}, Working: true},
	}
	first := matchDevelopmentAgents(targets, windows, processes)
	second := matchDevelopmentAgents(targets, windows, processes)
	if !reflect.DeepEqual(first, second) ||
		!reflect.DeepEqual(first.Sites["one"][0].WaitingAgents, []string{"Claude Code"}) ||
		!reflect.DeepEqual(first.Sites["one"][0].WorkingAgents, []string{"Codex"}) {
		t.Fatal("agent states must aggregate independently and deterministically", first, second)
	}
	first.Sites["one"][0].WorkingAgents[0] = "changed"
	first.Sites["one"][0].WaitingAgents[0] = "changed"
	first.Sites["one"][0].Agents[0] = "changed"
	if second.Sites["one"][0].WorkingAgents[0] != "Codex" || second.Sites["one"][0].WaitingAgents[0] != "Claude Code" ||
		second.Sites["one"][0].Agents[0] != "Claude Code" || len(windows.Sites["one"][0].WorkingAgents) != 0 || processes[0].Name != "Claude Code" {
		t.Fatal("status slices must not alias another reading or its inputs")
	}
	data, err := json.Marshal(second)
	if err != nil || !strings.Contains(string(data), `"workingAgents":["Codex"]`) {
		t.Fatal("working state missing from public snapshot", string(data), err)
	}
	for _, private := range []string{`"pid"`, `"started"`, `"cwd"`, `"ancestors"`, root} {
		if strings.Contains(string(data), private) {
			t.Fatal("working status leaked process metadata", private)
		}
	}
}

func TestCodexWorkingRequiresTheSameVerifiedHostAndWaitingWins(t *testing.T) {
	root, other := t.TempDir(), t.TempDir()
	targets := map[string]developmentAppTarget{"one": {SitePath: root}}
	windows := developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{"one": {
		{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}},
		{DevelopmentApp: DevelopmentApp{Name: "iTerm2", BundleID: "com.googlecode.iterm2"}, Kind: "terminal", ProcessIDs: []int{43}},
	}}}
	base := observedAgentProcess{Name: "Codex", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{42}}
	working := codexWaitingThread{Cwd: root, Source: "vscode", Working: true}
	waiting := codexWaitingThread{Cwd: root, Source: "vscode", Waiting: true}
	for _, test := range []struct {
		name      string
		processes []observedAgentProcess
		threads   []codexWaitingThread
		working   bool
		waiting   bool
	}{
		{"matching", []observedAgentProcess{base}, []codexWaitingThread{working}, true, false},
		{"waiting-first", []observedAgentProcess{base}, []codexWaitingThread{waiting, working, {Cwd: root, Source: "vscode"}}, false, true},
		{"waiting-last", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: root, Source: "vscode"}, working, waiting}, false, true},
		{"missing-status", []observedAgentProcess{base}, nil, false, false},
		{"idle-or-unknown", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: root, Source: "vscode"}}, false, false},
		{"unsupported-source", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: root, Source: "appServer", Working: true}}, false, false},
		{"different-folder", []observedAgentProcess{base}, []codexWaitingThread{{Cwd: other, Source: "vscode", Working: true}}, false, false},
		{"same-host", []observedAgentProcess{base, {Name: "Codex", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{42}}}, []codexWaitingThread{working}, true, false},
		{"competing-hosts", []observedAgentProcess{base, {Name: "Codex", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{43}}}, []codexWaitingThread{working}, false, false},
		{"unowned-process", []observedAgentProcess{base, {Name: "Codex", PID: 101, Started: 123000001, Cwd: root, Ancestors: []int{99}}}, []codexWaitingThread{working}, false, false},
		{"ide-status-in-terminal", []observedAgentProcess{{Name: "Codex", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{43}}}, []codexWaitingThread{working}, false, false},
		{"cli-in-terminal", []observedAgentProcess{{Name: "Codex", PID: 100, Started: 123000000, Cwd: root, Ancestors: []int{43}}}, []codexWaitingThread{{Cwd: root, Source: "cli", Working: true}}, true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			for i := range test.processes {
				test.processes[i].Waiting, test.processes[i].Working = true, true
			}
			test.processes = append(test.processes, observedAgentProcess{Name: "Claude Code", Waiting: true, Working: true})
			applyCodexWaiting(targets, windows, test.processes, test.threads)
			for i, process := range test.processes {
				if process.Name == "Claude Code" {
					if !process.Waiting || !process.Working {
						t.Fatal("Codex observer changed Claude's state")
					}
				} else if process.Waiting != test.waiting || process.Working != test.working {
					t.Fatalf("process %d: waiting=%v working=%v, want %v %v", i, process.Waiting, process.Working, test.waiting, test.working)
				}
			}
		})
	}
	processes := []observedAgentProcess{base}
	applyCodexWaiting(targets, windows, processes, []codexWaitingThread{working})
	windows.Available = false
	applyCodexWaiting(targets, windows, processes, []codexWaitingThread{working})
	if processes[0].Working || processes[0].Waiting {
		t.Fatal("unknown desktop membership must clear all activity status")
	}
}
