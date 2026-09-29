package main

import (
	"context"
	"os"
	"path/filepath"
	"time"
)

// Runs inside the existing background app scan. Missing/unsupported status
// sources leave the ordinary agent icon; they are never evidence of being idle.
func observeAgentWaiting(ctx context.Context, targets map[string]developmentAppTarget, windows developmentAppWindowScan, processes []observedAgentProcess) {
	for i := range processes {
		processes[i].Waiting = false
		processes[i].Working = false
	}
	home, err := os.UserHomeDir()
	if err != nil || !filepath.IsAbs(home) {
		return
	}
	applyClaudeWaiting(processes, filepath.Join(home, ".claude", "sessions"), time.Now())
	hasCodex := false
	for _, process := range processes {
		hasCodex = hasCodex || process.Name == "Codex"
	}
	if !hasCodex || ctx.Err() != nil {
		return
	}
	// One bounded read-only connection per whole-desktop scan, never one per
	// Site. No daemon is started and no turn is resumed or subscribed to.
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	threads, err := readCodexWaiting(ctx, filepath.Join(home, ".codex", "app-server-control", "app-server-control.sock"))
	if err == nil {
		applyCodexWaiting(targets, windows, processes, threads)
	}
}

func canonicalAgentDirectory(path string) string {
	if !filepath.IsAbs(path) {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(resolved)
}

// Codex's daemon identifies a thread's working folder, not its UI window. Only
// assign that signal when all matching live processes resolve to one Site/host
// group. Multiple sessions within the same group aggregate; competing hosts or
// an unowned process make the association unknown instead of guessing.
func applyCodexWaiting(targets map[string]developmentAppTarget, windows developmentAppWindowScan, processes []observedAgentProcess, threads []codexWaitingThread) {
	for i := range processes {
		if processes[i].Name == "Codex" {
			processes[i].Waiting = false
			processes[i].Working = false
		}
	}
	if !windows.Available {
		return
	}
	paths := make(map[string]string)
	canonical := func(path string) string {
		if value, ok := paths[path]; ok {
			return value
		}
		value := canonicalAgentDirectory(path)
		paths[path] = value
		return value
	}
	for _, thread := range threads {
		if (!thread.Waiting && !thread.Working) || (thread.Source != "cli" && thread.Source != "vscode") {
			continue
		}
		cwd := canonical(thread.Cwd)
		if cwd == "" {
			continue
		}
		owner := ""
		ambiguous := false
		var matches []int
		for i, process := range processes {
			if process.Name != "Codex" || process.PID <= 1 || process.Started == 0 || canonical(process.Cwd) != cwd {
				continue
			}
			process.Waiting = false
			process.Working = false
			reading := matchDevelopmentAgents(targets, windows, []observedAgentProcess{process})
			matchedOwner := ""
			for site, apps := range reading.Sites {
				for _, app := range apps {
					if len(app.Agents) == 0 {
						continue
					}
					// IDE integrations (including JetBrains) use "vscode".
					// Their thread cannot establish that a same-folder terminal
					// session is waiting. CLI sessions may live in either host.
					if thread.Source == "vscode" {
						editor := false
						for _, host := range windows.Sites[site] {
							editor = editor || (host.BundleID == app.BundleID && host.Kind == "editor")
						}
						if !editor {
							ambiguous = true
						}
					}
					key := site + "\x00" + app.BundleID
					if matchedOwner != "" && matchedOwner != key {
						ambiguous = true
					}
					matchedOwner = key
				}
			}
			if matchedOwner == "" || (owner != "" && owner != matchedOwner) {
				ambiguous = true
			}
			owner = matchedOwner
			matches = append(matches, i)
		}
		if !ambiguous && owner != "" {
			for _, i := range matches {
				processes[i].Waiting = processes[i].Waiting || thread.Waiting
				processes[i].Working = processes[i].Working || thread.Working
			}
		}
	}
	for i := range processes {
		if processes[i].Name == "Codex" && processes[i].Waiting {
			processes[i].Working = false
		}
	}
}
