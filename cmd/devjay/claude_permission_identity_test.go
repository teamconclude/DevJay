package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestClaudePermissionSessionWorkingDirectory(t *testing.T) {
	_, process, record, now := claudeWaitingFixture(t)
	record.SessionID = "session-one"
	root := process.Cwd
	child := filepath.Join(root, "services", "frontend")
	outside := t.TempDir()
	sibling := root + "-other"
	for _, path := range []string{child, sibling} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(sibling) })
	for name, target := range map[string]string{"inside": child, "outside": outside} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	input := permissionFixture()
	for _, tc := range []struct {
		name, cwd string
		want      bool
	}{
		{"root", root, true},
		{"nested working directory", child, true},
		{"internal symlink", filepath.Join(root, "inside"), true},
		{"escaping symlink", filepath.Join(root, "outside"), false},
		{"sibling sharing prefix", sibling, false},
		{"parent", filepath.Dir(root), false},
		{"other site", outside, false},
		{"missing", filepath.Join(root, "missing"), false},
		{"file", file, false},
		{"relative", "services/frontend", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input.Cwd = tc.cwd
			if got := claudePermissionSessionMatches(&record, process, input, now); got != tc.want {
				t.Fatalf("working directory %q accepted = %v, want %v", tc.cwd, got, tc.want)
			}
		})
	}
	input.Cwd = child
	for _, mutate := range []func(*claudeWaitingRecord){
		func(r *claudeWaitingRecord) { r.SessionID = "another-session" },
		func(r *claudeWaitingRecord) { r.PID++ },
		func(r *claudeWaitingRecord) { r.ProcStart = "replaced process" },
		func(r *claudeWaitingRecord) { r.Cwd = outside },
	} {
		changed := record
		mutate(&changed)
		if claudePermissionSessionMatches(&changed, process, input, now) {
			t.Fatal("nested directory bypassed session identity validation")
		}
	}
	if claudePermissionSessionMatches(nil, process, input, now) {
		t.Fatal("missing registry accepted")
	}
}

func TestClaudePermissionSubdirectoryRoundTrip(t *testing.T) {
	_, process, record, now := claudeWaitingFixture(t)
	record.SessionID = "session-one"
	input := permissionFixture()
	input.Cwd = filepath.Join(process.Cwd, "services", "frontend")
	if err := os.MkdirAll(input.Cwd, 0700); err != nil {
		t.Fatal(err)
	}
	s, path, _ := permissionTestServer(t)
	s.verify = func(_ int, input claudePermissionInput) (observedAgentProcess, error) {
		if !claudePermissionSessionMatches(&record, process, input, now) {
			return process, errors.New("session mismatch")
		}
		return process, nil
	}
	conn := connectPermission(t, path, input)
	pending := awaitPermissions(t, s, 1)[0]
	if pending.Process.Cwd != process.Cwd || pending.Cwd != input.Cwd {
		t.Fatal("request changed its owning session or working directory")
	}
	if got := previewSection(t, pending.Preview, "Working directory"); got.Text != input.Cwd || got.Kind != "path" {
		t.Fatal("changed working directory missing from preview", got)
	}
	if !s.decide(pending.Token, "deny") {
		t.Fatal("answerable request lost")
	}
	var response claudePermissionWireResponse
	if err := json.NewDecoder(conn).Decode(&response); err != nil || response.Decision != "deny" {
		t.Fatal("answer did not reach its original invocation", response, err)
	}
	awaitPermissions(t, s, 0)
}
