package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func claudeWaitingFixture(t *testing.T) (string, observedAgentProcess, claudeWaitingRecord, time.Time) {
	t.Helper()
	directory, cwd := t.TempDir(), t.TempDir()
	started := time.Date(2026, 9, 7, 14, 24, 27, 123000000, time.UTC)
	now := started.Add(24 * time.Hour)
	process := observedAgentProcess{Name: "Claude Code", PID: 12345, Started: uint64(started.UnixMicro()), Cwd: cwd}
	record := claudeWaitingRecord{PID: process.PID, PIDDomain: "darwin", ProcStart: started.Format("Mon Jan _2 15:04:05 2006"),
		Cwd: cwd, Status: "waiting", StartedAt: started.Add(time.Second).UnixMilli(),
		StatusUpdatedAt: now.Add(-time.Hour).UnixMilli(), UpdatedAt: now.Add(-time.Hour).UnixMilli()}
	return directory, process, record, now
}

func writeClaudeWaitingFixture(t *testing.T, directory string, pid int, record claudeWaitingRecord) string {
	t.Helper()
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, strconv.Itoa(pid)+".json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestClaudeWaitingReadsCurrentStateAndClears(t *testing.T) {
	directory, process, record, now := claudeWaitingFixture(t)
	processes := []observedAgentProcess{process, {Name: "Codex", PID: process.PID, Waiting: true, Working: true}}
	for _, status := range []string{"waiting", "busy", "waiting", "idle", "busy", "shell", "busy", "unknown"} {
		record.Status = status
		writeClaudeWaitingFixture(t, directory, process.PID, record)
		applyClaudeWaiting(processes, directory, now)
		if processes[0].Waiting != (status == "waiting") || processes[0].Working != (status == "busy") {
			t.Fatalf("%s: waiting=%v working=%v", status, processes[0].Waiting, processes[0].Working)
		}
		if !processes[1].Waiting || !processes[1].Working {
			t.Fatal("Claude adapter changed another agent's state")
		}
	}
	processes[0].Waiting = true
	processes[0].Working = true
	if err := os.Remove(filepath.Join(directory, strconv.Itoa(process.PID)+".json")); err != nil {
		t.Fatal(err)
	}
	applyClaudeWaiting(processes, directory, now)
	if processes[0].Waiting || processes[0].Working {
		t.Fatal("missing record preserved old agent state")
	}
}

func TestClaudeWaitingRequiresIdentityAndValidState(t *testing.T) {
	changes := map[string]func(*observedAgentProcess, *claudeWaitingRecord, time.Time){
		"wrong pid":              func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.PID++ },
		"unknown status":         func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.Status = "requires_action" },
		"remote pid domain":      func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.PIDDomain = "linux" },
		"missing pid domain":     func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.PIDDomain = "" },
		"wrong start identity":   func(p *observedAgentProcess, _ *claudeWaitingRecord, _ time.Time) { p.Started += 2000000 },
		"zero kernel start":      func(p *observedAgentProcess, _ *claudeWaitingRecord, _ time.Time) { p.Started = 0 },
		"overflow kernel start":  func(p *observedAgentProcess, _ *claudeWaitingRecord, _ time.Time) { p.Started = ^uint64(0) },
		"missing start identity": func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.ProcStart = "" },
		"relative cwd":           func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.Cwd = "relative" },
		"parent cwd":             func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.Cwd = filepath.Dir(r.Cwd) },
		"nonexistent cwd": func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) {
			r.Cwd = filepath.Join(r.Cwd, "missing")
		},
		"missing start time": func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) { r.StartedAt = 0 },
		"record predates process": func(p *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) {
			r.StartedAt = int64(p.Started/1000) - 1
		},
		"same second pid reuse": func(p *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) {
			r.StartedAt = int64(p.Started / 1000)
			p.Started += 200000
		},
		"state predates registration": func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) {
			r.StatusUpdatedAt = r.StartedAt - 1
		},
		"update predates state": func(_ *observedAgentProcess, r *claudeWaitingRecord, _ time.Time) {
			r.UpdatedAt = r.StatusUpdatedAt - 1
		},
		"future state": func(_ *observedAgentProcess, r *claudeWaitingRecord, now time.Time) {
			r.StatusUpdatedAt = now.Add(2 * time.Minute).UnixMilli()
			r.UpdatedAt = r.StatusUpdatedAt
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			for _, status := range []string{"waiting", "busy"} {
				directory, process, record, now := claudeWaitingFixture(t)
				record.Status = status
				change(&process, &record, now)
				writeClaudeWaitingFixture(t, directory, process.PID, record)
				process.Waiting, process.Working = true, true
				processes := []observedAgentProcess{process}
				applyClaudeWaiting(processes, directory, now)
				if processes[0].Waiting || processes[0].Working {
					t.Fatalf("unverified %s registry record reported waiting or working", status)
				}
			}
		})
	}
}

func TestClaudeWaitingOldStateAndCanonicalCwd(t *testing.T) {
	directory, process, record, now := claudeWaitingFixture(t)
	alias := filepath.Join(t.TempDir(), "linked-folder")
	if err := os.Symlink(process.Cwd, alias); err != nil {
		t.Fatal(err)
	}
	record.Cwd = alias
	record.ProcStart = strings.Join(strings.Fields(record.ProcStart), "   ")
	record.StatusUpdatedAt, record.UpdatedAt = record.StartedAt, record.StartedAt
	writeClaudeWaitingFixture(t, directory, process.PID, record)
	processes := []observedAgentProcess{process}
	applyClaudeWaiting(processes, directory, now.Add(7*24*time.Hour))
	if !processes[0].Waiting {
		t.Fatal("a valid long-lived waiting dialog or cwd alias was discarded")
	}
	record.Status = "busy"
	writeClaudeWaitingFixture(t, directory, process.PID, record)
	applyClaudeWaiting(processes, directory, now.Add(7*24*time.Hour))
	if !processes[0].Working || processes[0].Waiting {
		t.Fatal("a valid long-lived processing session or cwd alias was discarded")
	}
}

func TestClaudeWaitingRejectsUnsafeAndUnreadableFiles(t *testing.T) {
	for _, kind := range []string{"malformed", "wrong types", "oversized", "file symlink", "directory symlink", "directory", "fifo", "missing directory"} {
		t.Run(kind, func(t *testing.T) {
			directory, process, record, now := claudeWaitingFixture(t)
			path := filepath.Join(directory, strconv.Itoa(process.PID)+".json")
			var err error
			switch kind {
			case "malformed":
				err = os.WriteFile(path, []byte(`{"status":"waiting"`), 0600)
			case "wrong types":
				err = os.WriteFile(path, []byte(`{"pid":"12345","status":"waiting"}`), 0600)
			case "oversized":
				data, _ := json.Marshal(record)
				err = os.WriteFile(path, append(data, []byte(strings.Repeat(" ", claudeWaitingMaxBytes))...), 0600)
			case "file symlink":
				target := writeClaudeWaitingFixture(t, t.TempDir(), process.PID, record)
				err = os.Symlink(target, path)
			case "directory symlink":
				writeClaudeWaitingFixture(t, directory, process.PID, record)
				alias := filepath.Join(t.TempDir(), "sessions")
				err = os.Symlink(directory, alias)
				directory = alias
			case "directory":
				err = os.Mkdir(path, 0700)
			case "fifo":
				err = unix.Mkfifo(path, 0600)
			case "missing directory":
				directory = filepath.Join(directory, "missing")
			}
			if err != nil {
				t.Fatal(err)
			}
			process.Waiting, process.Working = true, true
			processes := []observedAgentProcess{process}
			applyClaudeWaiting(processes, directory, now)
			if processes[0].Waiting || processes[0].Working {
				t.Fatalf("%s file reported waiting or working", kind)
			}
		})
	}
}

func TestClaudePermissionIgnoresIdleStateBeforeHookAndExpiresLaterIdle(t *testing.T) {
	started := time.Date(2026, 10, 21, 10, 13, 57, 500000000, time.UTC)
	hook := uint64(started.UnixMicro())
	for _, status := range []string{"idle", "shell", "busy", "waiting", "unknown"} {
		for _, delta := range []time.Duration{-time.Minute, 0, time.Millisecond, time.Minute} {
			r := claudeWaitingRecord{Status: status, StatusUpdatedAt: started.Add(delta).UnixMilli()}
			want := (status == "idle" || status == "shell") && delta > 0
			if got := r.endedAfterClaudeHook(hook, started.Add(time.Hour)); got != want {
				t.Fatalf("status %s offset %s: got ended=%v, want %v", status, delta, got, want)
			}
		}
	}
	r := claudeWaitingRecord{Status: "idle", StatusUpdatedAt: started.Add(time.Millisecond).UnixMilli()}
	if r.endedAfterClaudeHook(hook, started.Add(time.Second)) {
		t.Fatal("new hook lost its registry startup grace")
	}
	if !r.endedAfterClaudeHook(hook, started.Add(2*time.Second)) {
		t.Fatal("quickly cancelled hook survived after startup grace")
	}
	if r.endedAfterClaudeHook(0, started.Add(time.Hour)) || r.endedAfterClaudeHook(^uint64(0), started.Add(time.Hour)) {
		t.Fatal("untrusted helper start used as cancellation evidence")
	}
	// Reconnection uses the original helper start, while a genuinely new
	// helper must not inherit the previous invocation's idle transition.
	if !r.endedAfterClaudeHook(hook, started.Add(24*time.Hour)) || r.endedAfterClaudeHook(uint64(started.Add(time.Minute).UnixMicro()), started.Add(time.Hour)) {
		t.Fatal("reconnection confused an old invocation with a new one")
	}
}
