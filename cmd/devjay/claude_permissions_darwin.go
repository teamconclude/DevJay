package main

/*
#include <stdlib.h>
char *devjay_claude_hook_process(int pid);
void devjay_update_claude_permissions(const char *json);
void devjay_install_claude_popover(void);
*/
import "C"

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"golang.org/x/sys/unix"
)

type claudePermissionChoice struct{ token, decision string }

var claudePermissionChoices = make(chan claudePermissionChoice, 16)

//export goDevJayClaudePermissionDecision
func goDevJayClaudePermissionDecision(token, decision *C.char) {
	select {
	case claudePermissionChoices <- claudePermissionChoice{C.GoString(token), C.GoString(decision)}:
	default:
	}
}
func verifyClaudePermissionPeer(pid int, input claudePermissionInput) (observedAgentProcess, error) {
	raw := C.devjay_claude_hook_process(C.int(pid))
	if raw == nil {
		return observedAgentProcess{}, errors.New("Missing live Claude parent")
	}
	defer C.free(unsafe.Pointer(raw))
	var peer struct {
		observedAgentProcess
		HookStarted uint64 `json:"hookStarted"`
	}
	if json.Unmarshal([]byte(C.GoString(raw)), &peer) != nil || peer.HookStarted == 0 {
		return observedAgentProcess{}, errors.New("Invalid Claude hook identity")
	}
	process := peer.observedAgentProcess
	if process.Name != "Claude Code" || process.PID <= 1 || process.Started == 0 {
		return process, errors.New("Invalid Claude parent")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return process, err
	}
	fd, err := unix.Open(filepath.Join(home, ".claude", "sessions"), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return process, err
	}
	defer unix.Close(fd)
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Uid != uint32(os.Getuid()) {
		return process, errors.New("Unowned Claude registry")
	}
	record := readClaudeWaitingRecord(fd, process.PID)
	if !claudePermissionSessionMatches(record, process, input, time.Now()) {
		return process, errors.New("Claude session does not match this hook")
	}
	if record.endedAfterClaudeHook(peer.HookStarted, time.Now()) {
		return process, errors.New("Claude has finished this approval")
	}
	// The native reader checks executable, UID, process start and ancestry.
	// Registry status may still be busy while PermissionRequest is running.
	return process, nil
}

type claudePermissionView struct {
	Preview     *claudePermissionPreview `json:"preview,omitempty"`
	Unavailable bool                     `json:"unavailable,omitempty"`
	SiteID      string                   `json:"siteID"`
	BundleID    string                   `json:"bundleID"`
	Token       string                   `json:"token"`
	Title       string                   `json:"title"`
	Details     string                   `json:"details"`
	Session     string                   `json:"session"`
}

func permissionOwner(targets map[string]developmentAppTarget, windows developmentAppWindowScan, p observedAgentProcess) (string, string) {
	p.Waiting = true
	p.Working = false
	scan := matchDevelopmentAgents(targets, windows, []observedAgentProcess{p})
	site, bundle := "", ""
	for id, apps := range scan.Sites {
		for _, app := range apps {
			if !slices.Contains(app.Agents, "Claude Code") {
				continue
			}
			if site != "" {
				return "", ""
			}
			site, bundle = id, app.BundleID
		}
	}
	return site, bundle
}

// Called only on the manager queue, alongside the regular desktop snapshot.
// Prompt bodies stay in native memory, never in frontend snapshots or logs.
func (m *siteManager) permissionViews() map[string][]claudePermissionView {
	views := map[string][]claudePermissionView{}
	if !m.config.showIDEStatus() {
		return views
	}
	targets := developmentAppTargets(m.monitor.windowNumbers, m.monitor.positions, m.config.Sites)
	for _, r := range m.claudePermissions.list() {
		site, bundle := permissionOwner(targets, m.developmentApps.windows, r.Process)
		if site == "" || !siteEnabled(m.config, site) || m.runtime[site] == nil {
			continue
		}
		key := permissionHostKey(site, bundle)
		title := "Claude · " + r.Tool + " · " + filepath.Base(r.Process.Cwd)
		if r.Preview != nil {
			title = r.Preview.Title + " · " + filepath.Base(r.Process.Cwd)
		}
		views[key] = append(views[key], claudePermissionView{Preview: r.Preview, SiteID: site, BundleID: bundle, Token: r.Token, Title: title, Details: safePermissionText(r.Details), Session: r.Session})
	}
	// A waiting indicator is not a captured permission request. Give those
	// hosts an explanatory panel, with navigation but never Yes/No controls.
	for site, reading := range m.developmentApps.readings {
		if !reading.Known || reading.Target != targets[site] || reading.Target.WindowNumber == 0 || !siteEnabled(m.config, site) || m.runtime[site] == nil {
			continue
		}
		for _, app := range reading.Apps {
			key := permissionHostKey(site, app.BundleID)
			if len(views[key]) != 0 || !slices.Contains(app.WaitingAgents, "Claude Code") {
				continue
			}
			views[key] = []claudePermissionView{{
				SiteID: site, BundleID: app.BundleID, Unavailable: true,
				Token:   fmt.Sprintf("waiting:%s:%s:%d", site, app.BundleID, reading.Target.WindowNumber),
				Title:   "Claude needs input · " + filepath.Base(reading.Target.SitePath),
				Details: m.claudeUnavailableDetails(),
			}}
		}
	}

	return views
}
func (m *siteManager) claudeUnavailableDetails() string {
	intro := "This question is waiting in Claude, but DevJay has no live request to answer. Click Claude to respond there.\n\n"
	if m.claudePermissionSettingsKnown && m.claudePermissionSettingsEnabled {
		return intro + "Claude prompts are already enabled globally for all folders. No per-folder setup is needed.\n\nRequests return to Claude if DevJay is unavailable for more than 30 seconds, the connection cannot be restored, or they expire. General questions, plan approval and sandbox network requests also stay in Claude."
	}
	if m.claudePermissionSettingsKnown {
		return intro + "Enable Claude prompts once in Settings → IDEs & editors. This applies to all folders through your global Claude settings. Questions already open and unsupported prompt types stay in Claude."
	}
	return intro + "Claude prompt answering uses one global setting for all folders. Open Prompt settings to check the integration. Questions already open and unsupported prompt types stay in Claude."
}

func publishClaudePermissionViews(views map[string][]claudePermissionView) {
	data, _ := json.Marshal(views)
	value := C.CString(string(data))
	defer C.free(unsafe.Pointer(value))
	application.InvokeSync(func() { C.devjay_update_claude_permissions(value) })
}
func installClaudePopover() { application.InvokeSync(func() { C.devjay_install_claude_popover() }) }
func withClaudePermissions(apps []DevelopmentApp, site string, views map[string][]claudePermissionView) []DevelopmentApp {
	out := slices.Clone(apps)
	for i, app := range out {
		count := 0
		for _, view := range views[permissionHostKey(site, app.BundleID)] {
			if !view.Unavailable {
				count++
			}
		}
		out[i].ClaudePermissions = count
		if count == 0 {
			continue
		}
		out[i].Agents = slices.Clone(app.Agents)
		if !slices.Contains(out[i].Agents, "Claude Code") {
			out[i].Agents = append(out[i].Agents, "Claude Code")
		}
		out[i].WaitingAgents = slices.Clone(app.WaitingAgents)
		if !slices.Contains(out[i].WaitingAgents, "Claude Code") {
			out[i].WaitingAgents = append(out[i].WaitingAgents, "Claude Code")
		}
		out[i].WorkingAgents = slices.DeleteFunc(slices.Clone(app.WorkingAgents), func(name string) bool { return name == "Claude Code" })
	}
	return out
}

// Once discovery has completed, do not hold a prompt we cannot offer on a card.
func (m *siteManager) releaseUnmappedClaudePermissions() {
	visible := map[string]bool{}
	for _, requests := range m.permissionViews() {
		for _, r := range requests {
			visible[r.Token] = true
		}
	}
	for _, r := range m.claudePermissions.list() {
		// On relaunch, hook helpers can reconnect before the Site windows and
		// their host inventory are ready. Let startup settle before returning
		// an unmapped request; later normal discovery checks still release it.
		if !visible[r.Token] && time.Since(r.Created) >= 5*time.Second {
			m.claudePermissions.decide(r.Token, "fallback")
		}
	}
}

// Consume the native action on the manager queue. Navigation releases only the
// displayed invocation without an allow/deny verdict, then uses its observed
// owner. Never navigate for a stale token or trust a Site supplied by the UI.
func (m *siteManager) consumeClaudePermissionChoice(choice claudePermissionChoice) (appActivateRequest, bool) {
	if choice.decision != "allow" && choice.decision != "deny" && choice.decision != "open" {
		return appActivateRequest{}, false
	}
	for _, requests := range m.permissionViews() {
		for _, request := range requests {
			if request.Token != choice.token {
				continue
			}
			if request.Unavailable {
				if choice.decision == "open" {
					return appActivateRequest{SiteID: request.SiteID, BundleID: request.BundleID}, true
				}
				return appActivateRequest{}, false
			}
			if choice.decision == "open" {
				if m.claudePermissions.decide(choice.token, "fallback") {
					return appActivateRequest{SiteID: request.SiteID, BundleID: request.BundleID}, true
				}
				return appActivateRequest{}, false
			}
			m.claudePermissions.decide(choice.token, choice.decision)
			return appActivateRequest{}, false
		}
	}
	// If ownership vanished after rendering, still return the request to
	// Claude, but do not switch to an unverified desktop or app.
	if choice.decision == "open" {
		m.claudePermissions.decide(choice.token, "fallback")
	}
	return appActivateRequest{}, false
}

func (m *siteManager) claudePromptSettingsRequested(choice claudePermissionChoice) bool {
	if choice.decision != "settings" {
		return false
	}
	for _, requests := range m.permissionViews() {
		for _, request := range requests {
			if request.Token == choice.token && request.Unavailable {
				return true
			}
		}
	}
	return false
}
