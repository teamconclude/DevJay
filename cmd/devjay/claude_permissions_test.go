package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func permissionFixture() claudePermissionInput {
	return claudePermissionInput{Session: "session-one", Cwd: "/tmp", Event: "PermissionRequest", Tool: "Bash", Input: json.RawMessage(`{"command":"printf 'test'","description":"A test command"}`)}
}
func permissionTestServer(t *testing.T) (*claudePermissionServer, string, context.CancelFunc) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "devjay-permission-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	path := filepath.Join(dir, "claude.sock")
	s, err := startClaudePermissionServer(ctx, path, func(pid int, r claudePermissionInput) (observedAgentProcess, error) {
		return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: r.Cwd}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return s, path, cancel
}
func awaitPermissions(t *testing.T, s *claudePermissionServer, count int) []*claudePermissionRequest {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r := s.list()
		if len(r) == count {
			return r
		}
		time.Sleep(time.Millisecond * 5)
	}
	t.Fatalf("wanted %d pending requests, got %d", count, len(s.list()))
	return nil
}
func connectPermission(t *testing.T, path string, input claudePermissionInput) net.Conn {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err = json.NewEncoder(conn).Encode(input); err != nil {
		t.Fatal(err)
	}
	return conn
}
func TestClaudePermissionValidation(t *testing.T) {
	if !permissionFixture().valid() {
		t.Fatal("valid request rejected")
	}
	for _, mutate := range []func(*claudePermissionInput){func(r *claudePermissionInput) { r.Tool = "AskUserQuestion" }, func(r *claudePermissionInput) { r.Tool = "ExitPlanMode" }, func(r *claudePermissionInput) { r.Event = "PreToolUse" }, func(r *claudePermissionInput) { r.Cwd = "relative" }, func(r *claudePermissionInput) { r.Session = "" }, func(r *claudePermissionInput) { r.Input = json.RawMessage(`null`) }, func(r *claudePermissionInput) { r.Input = json.RawMessage(`[]`) }, func(r *claudePermissionInput) {
		r.Input = json.RawMessage(`{"body":"` + strings.Repeat("x", claudePermissionLimit) + `"}`)
	}} {
		r := permissionFixture()
		mutate(&r)
		if r.valid() {
			t.Fatal("invalid request accepted")
		}
	}
	if _, err := readPermissionLine(bufio.NewReaderSize(strings.NewReader(strings.Repeat("x", claudePermissionLimit+1)+"\n"), claudePermissionLimit+1)); err == nil {
		t.Fatal("oversize accepted")
	}
}
func TestClaudePermissionSeparateInvocationsAndStaleDecisions(t *testing.T) {
	s, path, _ := permissionTestServer(t)
	first := connectPermission(t, path, permissionFixture())
	r1 := awaitPermissions(t, s, 1)[0]
	second := connectPermission(t, path, permissionFixture())
	requests := awaitPermissions(t, s, 2)
	r2 := requests[1]
	if r1.Token == r2.Token {
		t.Fatal("same-session calls shared identity")
	}
	if !s.decide(r1.Token, "allow") || s.decide(r1.Token, "deny") {
		t.Fatal("decision not single use")
	}
	var answer map[string]string
	if json.NewDecoder(first).Decode(&answer) != nil || answer["decision"] != "allow" {
		t.Fatal("wrong first response", answer)
	}
	if !s.decide(r2.Token, "deny") {
		t.Fatal("second request lost")
	}
	answer = nil
	if json.NewDecoder(second).Decode(&answer) != nil || answer["decision"] != "deny" {
		t.Fatal("wrong second response", answer)
	}
	awaitPermissions(t, s, 0)
}
func TestClaudePermissionDisconnectFallbackAndShutdown(t *testing.T) {
	s, path, cancel := permissionTestServer(t)
	conn := connectPermission(t, path, permissionFixture())
	token := awaitPermissions(t, s, 1)[0].Token
	conn.Close()
	awaitPermissions(t, s, 0)
	if s.decide(token, "allow") {
		t.Fatal("disconnected request accepted")
	}
	conn = connectPermission(t, path, permissionFixture())
	token = awaitPermissions(t, s, 1)[0].Token
	if !s.decide(token, "fallback") {
		t.Fatal("fallback failed")
	}
	data, err := io.ReadAll(conn)
	if err != nil || len(data) != 0 {
		t.Fatal("fallback must not authorize", string(data), err)
	}
	conn = connectPermission(t, path, permissionFixture())
	awaitPermissions(t, s, 1)
	cancel()
	data, err = io.ReadAll(conn)
	if err != nil || len(data) != 0 {
		t.Fatal("shutdown must not authorize", string(data), err)
	}
}
func TestClaudePermissionRevalidatesBeforeReply(t *testing.T) {
	s, path, _ := permissionTestServer(t)
	var ended atomic.Bool
	s.verify = func(_ int, input claudePermissionInput) (observedAgentProcess, error) {
		if ended.Load() {
			return observedAgentProcess{}, errors.New("session ended")
		}
		return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: input.Cwd}, nil
	}
	conn := connectPermission(t, path, permissionFixture())
	token := awaitPermissions(t, s, 1)[0].Token
	ended.Store(true)
	s.decide(token, "allow")
	data, err := io.ReadAll(conn)
	if err != nil || len(data) != 0 {
		t.Fatal("stale identity authorized", string(data), err)
	}
}
func TestClaudePermissionHookOutput(t *testing.T) {
	for _, decision := range []string{"allow", "deny", "fallback"} {
		t.Run(decision, func(t *testing.T) {
			s, path, _ := permissionTestServer(t)
			input, _ := json.Marshal(permissionFixture())
			var output bytes.Buffer
			done := make(chan struct{})
			go func() {
				runClaudePermissionHook(context.Background(), bytes.NewReader(input), &output, path)
				close(done)
			}()
			r := awaitPermissions(t, s, 1)[0]
			s.decide(r.Token, decision)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("helper stuck")
			}
			if decision == "fallback" {
				if output.Len() != 0 {
					t.Fatal("fallback emitted decision")
				}
				return
			}
			var actual struct {
				Hook struct {
					Event    string         `json:"hookEventName"`
					Decision map[string]any `json:"decision"`
				} `json:"hookSpecificOutput"`
			}
			if json.Unmarshal(output.Bytes(), &actual) != nil || actual.Hook.Event != "PermissionRequest" || actual.Hook.Decision["behavior"] != decision || len(actual.Hook.Decision) != 1 {
				t.Fatal("wrong hook output", output.String())
			}
		})
	}
}
func TestClaudePermissionHookCancellation(t *testing.T) {
	s, path, _ := permissionTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	input, _ := json.Marshal(permissionFixture())
	var output bytes.Buffer
	done := make(chan struct{})
	go func() { runClaudePermissionHook(ctx, bytes.NewReader(input), &output, path); close(done) }()
	awaitPermissions(t, s, 1)
	cancel()
	<-done
	awaitPermissions(t, s, 0)
	if output.Len() != 0 {
		t.Fatal("cancel authorized")
	}
}
func TestClaudePermissionPrivateSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "socket")
	os.Chmod(dir, 0755)
	if privateClaudeSocketDirectory(path, false) == nil {
		t.Fatal("nonprivate directory accepted")
	}
}
func TestClaudePermissionWaitingDoesNotMutateScan(t *testing.T) {
	apps := []DevelopmentApp{{BundleID: "host", Agents: []string{"Claude Code"}, WorkingAgents: []string{"Claude Code"}}}
	views := map[string][]claudePermissionView{permissionHostKey("site", "host"): {{Token: "one"}}}
	got := withClaudePermissions(apps, "site", views)
	if len(got[0].WaitingAgents) != 1 || len(got[0].WorkingAgents) != 0 || got[0].ClaudePermissions != 1 {
		t.Fatal("missing pending state")
	}
	if len(apps[0].WaitingAgents) != 0 || len(apps[0].WorkingAgents) != 1 {
		t.Fatal("mutated observer cache")
	}
}

func TestClaudePermissionGoToHostLeavesRequestUnanswered(t *testing.T) {
	for _, mode := range []string{"open", "stale", "unmapped", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, path, _ := permissionTestServer(t)
			s.verify = func(pid int, input claudePermissionInput) (observedAgentProcess, error) {
				return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: input.Cwd, Ancestors: []int{42}}, nil
			}
			root := t.TempDir()
			input := permissionFixture()
			input.Cwd = root
			conn := connectPermission(t, path, input)
			token := awaitPermissions(t, s, 1)[0].Token
			m := &siteManager{
				claudePermissions: s,
				config:            SiteConfig{Sites: []Site{{ID: "one", Path: root}}},
				runtime:           map[string]*siteRuntime{"one": {Ready: true}},
				monitor:           monitorSchedule{windowNumbers: map[string]uint32{"one": 12}},
				developmentApps: developmentAppMonitor{windows: developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{
					"one": {{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}}},
				}}},
			}
			choice := claudePermissionChoice{token: token, decision: "open"}
			switch mode {
			case "stale":
				s.decide(token, "fallback")
			case "unmapped":
				m.developmentApps.windows.Available = false
			case "unknown":
				choice.decision = "escape"
			}
			req, navigate := m.consumeClaudePermissionChoice(choice)
			if navigate != (mode == "open") {
				t.Fatalf("unexpected navigation: %+v %v", req, navigate)
			}
			if navigate && (req.SiteID != "one" || req.BundleID != "com.jetbrains.goland") {
				t.Fatalf("wrong destination: %+v", req)
			}
			if mode == "unknown" {
				if len(s.list()) != 1 {
					t.Fatal("Escape/unknown action consumed a request")
				}
				s.decide(token, "fallback")
			}
			if _, again := m.consumeClaudePermissionChoice(choice); again {
				t.Fatal("reused token navigated again")
			}
			data, err := io.ReadAll(conn)
			if err != nil || len(data) != 0 {
				t.Fatalf("navigation/cancellation sent a verdict: %q %v", data, err)
			}
		})
	}
}

func TestClaudePermissionWaitingWithoutHookOffersNavigationOnly(t *testing.T) {
	root := t.TempDir()
	target := developmentAppTarget{WindowNumber: 12, SitePath: root}
	app := DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland", Agents: []string{"Claude Code"}, WaitingAgents: []string{"Claude Code"}}
	m := &siteManager{
		config:          SiteConfig{Sites: []Site{{ID: "one", Path: root}}},
		runtime:         map[string]*siteRuntime{"one": {Ready: true}},
		monitor:         monitorSchedule{windowNumbers: map[string]uint32{"one": 12}},
		developmentApps: developmentAppMonitor{readings: map[string]developmentAppReading{"one": {Known: true, Target: target, Apps: []DevelopmentApp{app}}}},
	}
	key := permissionHostKey("one", app.BundleID)
	views := m.permissionViews()
	if len(views[key]) != 1 || !views[key][0].Unavailable {
		t.Fatal("waiting without hook must show explanatory popup")
	}
	token := views[key][0].Token
	overlaid := withClaudePermissions([]DevelopmentApp{app}, "one", views)
	if overlaid[0].ClaudePermissions != 0 {
		t.Fatal("status-only wait counted as answerable request")
	}
	for _, decision := range []string{"allow", "deny"} {
		if _, navigate := m.consumeClaudePermissionChoice(claudePermissionChoice{token, decision}); navigate {
			t.Fatal("status-only request accepted an answer")
		}
	}
	req, navigate := m.consumeClaudePermissionChoice(claudePermissionChoice{token, "open"})
	if !navigate || req.SiteID != "one" || req.BundleID != app.BundleID {
		t.Fatal("missing Go to Claude destination")
	}
	if !m.claudePromptSettingsRequested(claudePermissionChoice{token, "settings"}) {
		t.Fatal("missing setup action")
	}
	m.developmentApps.readings["one"] = developmentAppReading{Known: true, Target: target, Apps: []DevelopmentApp{{Name: app.Name, BundleID: app.BundleID, Agents: app.Agents}}}
	if len(m.permissionViews()) != 0 {
		t.Fatal("resumed Claude kept the popup")
	}
	if _, navigate := m.consumeClaudePermissionChoice(claudePermissionChoice{token, "open"}); navigate {
		t.Fatal("stale waiting state navigated")
	}
	if m.claudePromptSettingsRequested(claudePermissionChoice{token, "settings"}) {
		t.Fatal("stale popup opened settings")
	}
}

func TestClaudePermissionExpiryLeavesClaudeInControl(t *testing.T) {
	dir, err := os.MkdirTemp("/tmp", "devjay-expiry-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	path := filepath.Join(dir, "prompt.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	s := &claudePermissionServer{pending: map[string]*claudePermissionRequest{}, changed: make(chan struct{}, 1), verify: func(_ int, r claudePermissionInput) (observedAgentProcess, error) {
		return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: r.Cwd}, nil
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.AcceptUnix()
		if err == nil {
			s.serveWithTimeout(context.Background(), conn, 150*time.Millisecond)
		}
	}()
	conn := connectPermission(t, path, permissionFixture())
	token := awaitPermissions(t, s, 1)[0].Token
	data, err := io.ReadAll(conn)
	if err != nil || len(data) != 0 {
		t.Fatalf("expired request emitted a verdict: %q %v", data, err)
	}
	<-done
	awaitPermissions(t, s, 0)
	if s.decide(token, "allow") {
		t.Fatal("expired token authorized")
	}
}

func TestClaudePermissionFallbackExplainsGlobalSetup(t *testing.T) {
	m := &siteManager{claudePermissionSettingsKnown: true, claudePermissionSettingsEnabled: true}
	got := m.claudeUnavailableDetails()
	if !strings.Contains(got, "already enabled globally for all folders") || strings.Contains(got, "Enable Claude prompts once") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "expire") || strings.Contains(got, "was not sent") {
		t.Fatal("missing honest fallback reason", got)
	}
	m.claudePermissionSettingsEnabled = false
	if !strings.Contains(m.claudeUnavailableDetails(), "Enable Claude prompts once") {
		t.Fatal("missing disabled setup guidance")
	}
	m.claudePermissionSettingsKnown = false
	if strings.Contains(m.claudeUnavailableDetails(), "already enabled") {
		t.Fatal("guessed global setting")
	}
}

func TestClaudePermissionRetiresAbandonedConnectedHook(t *testing.T) {
	s, path, _ := permissionTestServer(t)
	var ended atomic.Bool
	s.verify = func(_ int, input claudePermissionInput) (observedAgentProcess, error) {
		if ended.Load() {
			return observedAgentProcess{}, errors.New("Claude is idle after this hook")
		}
		return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: input.Cwd}, nil
	}
	input, _ := json.Marshal(permissionFixture())
	var output bytes.Buffer
	done := make(chan struct{})
	go func() {
		runClaudePermissionHook(context.Background(), bytes.NewReader(input), &output, path)
		close(done)
	}()
	request := awaitPermissions(t, s, 1)[0]
	// Neither end closes its socket. The lifecycle recheck must retire the
	// stale token itself, return no verdict and stop the helper from retrying.
	ended.Store(true)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stale connected helper remained pending")
	}
	awaitPermissions(t, s, 0)
	if output.Len() != 0 || s.decide(request.Token, "allow") {
		t.Fatal("obsolete invocation received an approval")
	}
	// A later request is independent and still responds normally.
	ended.Store(false)
	conn := connectPermission(t, path, permissionFixture())
	fresh := awaitPermissions(t, s, 1)[0]
	if fresh.Token == request.Token || !s.decide(fresh.Token, "deny") {
		t.Fatal("new request was suppressed")
	}
	var response claudePermissionWireResponse
	if json.NewDecoder(conn).Decode(&response) != nil || response.Decision != "deny" {
		t.Fatal("new request lost its response")
	}
}

func TestClaudePermissionDisableRejectsCachedHooksUntilReenabled(t *testing.T) {
	s, path, _ := permissionTestServer(t)
	conn := connectPermission(t, path, permissionFixture())
	token := awaitPermissions(t, s, 1)[0].Token
	s.setEnabled(false)
	if len(s.list()) != 0 || s.decide(token, "allow") {
		t.Fatal("disabled request remained answerable")
	}
	if data, err := io.ReadAll(conn); err != nil || len(data) != 0 {
		t.Fatalf("disable sent a verdict: %q %v", data, err)
	}
	// A Claude session may keep the removed hook in memory. Its next call
	// must fall through without appearing in DevJay or reconnecting.
	result, _ := startPermissionTestHook(t, path, time.Second, 200*time.Millisecond)
	if output := permissionHookResult(t, result); output != "" {
		t.Fatal(output)
	}
	if len(s.list()) != 0 {
		t.Fatal("cached hook bypassed disable")
	}
	s.setEnabled(true)
	conn = connectPermission(t, path, permissionFixture())
	fresh := awaitPermissions(t, s, 1)[0]
	if !s.decide(fresh.Token, "deny") {
		t.Fatal("re-enabling did not restore answers")
	}
	var response claudePermissionWireResponse
	if json.NewDecoder(conn).Decode(&response) != nil || response.Decision != "deny" {
		t.Fatal("wrong fresh response")
	}
}

func TestClaudePermissionDisableDuringInitialVerification(t *testing.T) {
	s, path, _ := permissionTestServer(t)
	entered, resume := make(chan struct{}), make(chan struct{})
	s.verify = func(_ int, input claudePermissionInput) (observedAgentProcess, error) {
		close(entered)
		<-resume
		return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: input.Cwd}, nil
	}
	conn := connectPermission(t, path, permissionFixture())
	<-entered
	s.setEnabled(false)
	close(resume)
	if data, err := io.ReadAll(conn); err != nil || len(data) != 0 {
		t.Fatalf("in-flight request sent a verdict: %q %v", data, err)
	}
	if len(s.list()) != 0 {
		t.Fatal("in-flight request appeared after disabling")
	}
}

func TestClaudePermissionRejectsExtraRequestBytes(t *testing.T) {
	for _, buffered := range []bool{true, false} {
		t.Run(fmt.Sprint("buffered=", buffered), func(t *testing.T) {
			s, path, _ := permissionTestServer(t)
			conn, err := net.Dial("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(time.Second))
			body, _ := json.Marshal(permissionFixture())
			wire := append(body, '\n')
			if buffered {
				wire = append(wire, 'x')
			}
			if _, err = conn.Write(wire); err != nil {
				t.Fatal(err)
			}
			if !buffered {
				awaitPermissions(t, s, 1)
				if _, err = conn.Write([]byte("x")); err != nil {
					t.Fatal(err)
				}
			}
			data, err := io.ReadAll(conn)
			if err != nil || len(data) != 0 {
				t.Fatalf("invalid framing was not rejected without a verdict: %q %v", data, err)
			}
			awaitPermissions(t, s, 0)
		})
	}
}
