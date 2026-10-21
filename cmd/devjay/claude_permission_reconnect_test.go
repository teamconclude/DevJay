package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startPermissionTestHook(t *testing.T, path string, lifetime, grace time.Duration) (<-chan string, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	input, _ := json.Marshal(permissionFixture())
	result := make(chan string, 1)
	go func() {
		var output bytes.Buffer
		runClaudePermissionHookWithTiming(ctx, bytes.NewReader(input), &output, path, lifetime, grace, 10*time.Millisecond)
		result <- output.String()
	}()
	return result, cancel
}

func permissionHookResult(t *testing.T, result <-chan string) string {
	t.Helper()
	select {
	case output := <-result:
		return output
	case <-time.After(3 * time.Second):
		t.Fatal("hook did not finish")
		return ""
	}
}

func restartPermissionTestServer(t *testing.T, path string, verify func(int, claudePermissionInput) (observedAgentProcess, error)) *claudePermissionServer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s, err := startClaudePermissionServer(ctx, path, verify)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestClaudePermissionHookReconnectsOriginalInvocation(t *testing.T) {
	for _, decision := range []string{"allow", "deny"} {
		t.Run(decision, func(t *testing.T) {
			first, path, shutdown := permissionTestServer(t)
			result, _ := startPermissionTestHook(t, path, 5*time.Second, time.Second)
			original := awaitPermissions(t, first, 1)[0]
			shutdown()
			first.listener.Close()
			awaitPermissions(t, first, 0)
			if first.decide(original.Token, "allow") {
				t.Fatal("old token remained usable")
			}
			// The app is absent for several retry intervals; Claude stays blocked.
			select {
			case output := <-result:
				t.Fatalf("restart lost the invocation: %q", output)
			case <-time.After(50 * time.Millisecond):
			}
			var checks atomic.Int32
			next := restartPermissionTestServer(t, path, func(pid int, input claudePermissionInput) (observedAgentProcess, error) {
				checks.Add(1)
				return first.verify(pid, input)
			})
			restored := awaitPermissions(t, next, 1)[0]
			if restored.Token == original.Token || restored.Session != original.Session || restored.Details != original.Details || restored.Cwd != original.Cwd || restored.peer != original.peer {
				t.Fatal("reconnect changed invocation or reused a token")
			}
			select {
			case output := <-result:
				t.Fatalf("reconnect answered automatically: %q", output)
			default:
			}
			if next.decide(original.Token, decision) || !next.decide(restored.Token, decision) {
				t.Fatal("wrong token accepted")
			}
			output := permissionHookResult(t, result)
			if !strings.Contains(output, `"behavior":"`+decision+`"`) {
				t.Fatal(output)
			}
			if checks.Load() != 2 {
				t.Fatal("identity was not checked on reconnect and before answer")
			}
			awaitPermissions(t, next, 0)
		})
	}
}

func TestClaudePermissionReconnectStopsWithoutAnswer(t *testing.T) {
	for _, reason := range []string{"grace expires", "Claude interrupts", "lifetime expires", "identity changes", "unsafe socket"} {
		t.Run(reason, func(t *testing.T) {
			first, path, shutdown := permissionTestServer(t)
			lifetime, grace := 5*time.Second, 500*time.Millisecond
			if reason == "grace expires" {
				grace = 80 * time.Millisecond
			}
			if reason == "lifetime expires" {
				lifetime = 150 * time.Millisecond
			}
			result, interrupt := startPermissionTestHook(t, path, lifetime, grace)
			awaitPermissions(t, first, 1)
			shutdown()
			first.listener.Close()
			awaitPermissions(t, first, 0)
			switch reason {
			case "Claude interrupts":
				interrupt()
			case "identity changes":
				restartPermissionTestServer(t, path, func(int, claudePermissionInput) (observedAgentProcess, error) {
					return observedAgentProcess{}, errors.New("different Claude session")
				})
			case "unsafe socket":
				if err := os.WriteFile(path, []byte("not a socket"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if output := permissionHookResult(t, result); output != "" {
				t.Fatalf("unexpected authorization: %s", output)
			}
		})
	}
}

func TestClaudePermissionExplicitHandoffDoesNotReconnect(t *testing.T) {
	for _, release := range []string{"Claude button", "disable"} {
		t.Run(release, func(t *testing.T) {
			server, path, _ := permissionTestServer(t)
			result, _ := startPermissionTestHook(t, path, 5*time.Second, time.Second)
			token := awaitPermissions(t, server, 1)[0].Token
			if release == "disable" {
				server.setEnabled(false)
			} else {
				server.decide(token, "fallback")
			}
			if output := permissionHookResult(t, result); output != "" {
				t.Fatal(output)
			}
			awaitPermissions(t, server, 0)
			if server.decide(token, "allow") {
				t.Fatal("handoff token remained usable")
			}
		})
	}
}

func TestClaudePermissionServerExpiryDoesNotReconnect(t *testing.T) {
	server, path, stop := permissionTestServer(t)
	stop()
	server.listener.Close()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	go func() {
		conn, err := listener.AcceptUnix()
		if err == nil {
			server.serveWithTimeout(context.Background(), conn, 80*time.Millisecond)
		}
	}()
	result, _ := startPermissionTestHook(t, path, 5*time.Second, time.Second)
	token := awaitPermissions(t, server, 1)[0].Token
	if output := permissionHookResult(t, result); output != "" {
		t.Fatal(output)
	}
	awaitPermissions(t, server, 0)
	if server.decide(token, "allow") {
		t.Fatal("expired token remained usable")
	}
}

func TestClaudePermissionWireCompatibilityAndFailedReplies(t *testing.T) {
	for _, tc := range []struct {
		name, reply, decision string
		reconnect             bool
	}{
		{"legacy allow", `{"decision":"allow"}`, "allow", false},
		{"legacy deny", `{"decision":"deny"}`, "deny", false},
		{"contradictory response", `{"state":"pending","decision":"allow"}`, "", false},
		{"unknown response state", `{"state":"unknown","decision":"allow"}`, "", false},
		{"repeated pending with verdict", "{\"state\":\"pending\"}\n{\"state\":\"pending\",\"decision\":\"allow\"}\n", "", false},
		{"legacy fallback or rejected", "", "", false},
		{"accepted then disconnected", "{\"state\":\"pending\"}\n", "", true},
		{"explicit fallback", "{\"state\":\"pending\"}\n{\"decision\":\"fallback\"}\n", "fallback", false},
		{"partial verdict", "{\"state\":\"pending\"}\n{\"decision\":\"allow", "", false},
		{"malformed verdict", "{\"state\":\"pending\"}\nnot json\n", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, server := net.Pipe()
			defer server.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				var request claudePermissionWireRequest
				if json.NewDecoder(server).Decode(&request) == nil {
					io.WriteString(server, tc.reply)
				}
				server.Close()
			}()
			deadline := time.Now().Add(time.Second)
			decision, reconnect := awaitClaudePermissionDecision(context.Background(), client, claudePermissionWireRequest{permissionFixture(), 1}, deadline, deadline)
			<-done
			if decision != tc.decision || reconnect != tc.reconnect {
				t.Fatalf("got %q %v", decision, reconnect)
			}
		})
	}
}

func TestClaudePermissionReconnectWaitsForSiteDiscovery(t *testing.T) {
	server, path, _ := permissionTestServer(t)
	root := t.TempDir()
	server.verify = func(int, claudePermissionInput) (observedAgentProcess, error) {
		return observedAgentProcess{Name: "Claude Code", PID: 1234, Started: 99, Cwd: root, Ancestors: []int{42}}, nil
	}
	input := permissionFixture()
	input.Cwd = root
	conn := connectPermission(t, path, input)
	request := awaitPermissions(t, server, 1)[0]
	manager := &siteManager{
		claudePermissions: server,
		config:            SiteConfig{Sites: []Site{{ID: "one", Path: root}}},
		runtime:           map[string]*siteRuntime{"one": {}},
	}
	manager.releaseUnmappedClaudePermissions()
	if len(server.list()) != 1 {
		t.Fatal("startup discovery returned the reconnecting request")
	}
	manager.runtime["one"].Ready = true
	manager.monitor.windowNumbers = map[string]uint32{"one": 12}
	manager.developmentApps.windows = developmentAppWindowScan{Available: true, Sites: map[string][]observedDevelopmentApp{
		"one": {{DevelopmentApp: DevelopmentApp{Name: "GoLand", BundleID: "com.jetbrains.goland"}, Kind: "editor", ProcessIDs: []int{42}}},
	}}
	request.Created = time.Now().Add(-6 * time.Second)
	manager.releaseUnmappedClaudePermissions()
	views := manager.permissionViews()[permissionHostKey("one", "com.jetbrains.goland")]
	if len(views) != 1 || views[0].Token != request.Token {
		t.Fatal("restored request was not presented")
	}
	manager.developmentApps.windows = developmentAppWindowScan{}
	manager.releaseUnmappedClaudePermissions()
	if data, err := io.ReadAll(conn); err != nil || len(data) != 0 {
		t.Fatalf("unmapped request did not fall through: %q %v", data, err)
	}
	awaitPermissions(t, server, 0)
}
