package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func previewInput(t *testing.T, tool string, fields any) *claudePermissionPreview {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	result := makeClaudePermissionPreview(tool, string(raw))
	if result == nil {
		t.Fatal("missing preview")
	}
	return result
}
func previewSection(t *testing.T, p *claudePermissionPreview, label string) claudePermissionSection {
	t.Helper()
	for _, s := range p.Sections {
		if s.Label == label {
			return s
		}
	}
	t.Fatalf("missing %q in %+v", label, p.Sections)
	return claudePermissionSection{}
}
func TestClaudePermissionPreviewBashPreservesCommandAndAdditionalInput(t *testing.T) {
	command := "printf '%s\\n' \"$VALUE\" && cat <<'EOF'\n  untouched input\nEOF\n" + strings.Repeat("# long command line ", 1200)
	p := previewInput(t, "Bash", map[string]any{"command": command, "description": "Inspect the current checkout", "timeout": 5000, "run_in_background": true, "dangerouslyDisableSandbox": true, "future_option": json.RawMessage(`{"id":9007199254740993}`)})
	if p.Title != "Bash command" || p.Description != "Inspect the current checkout" {
		t.Fatal("missing action description", p)
	}
	if s := previewSection(t, p, "Command"); s.Text != command || s.Kind != "code" {
		t.Fatal("shell input changed or truncated")
	}
	if previewSection(t, p, "Timeout").Text != "5000 ms" || previewSection(t, p, "Run in background").Text != "Yes" {
		t.Fatal("execution options lost")
	}
	if s := previewSection(t, p, "Disable sandbox for this command"); s.Text != "Yes" || s.Kind != "warning" {
		t.Fatal("sandbox option lost")
	}
	if !strings.Contains(previewSection(t, p, "Additional input").Text, "9007199254740993") {
		t.Fatal("unknown input or integer precision lost")
	}
}
func TestClaudePermissionPreviewEditsAndEmptyWrites(t *testing.T) {
	p := previewInput(t, "Edit", map[string]any{"file_path": "/work/a file.go", "old_string": "before\n\tline\n", "new_string": "after\n", "replace_all": true})
	if previewSection(t, p, "File").Text != "/work/a file.go" || previewSection(t, p, "Replace all matches").Text != "Yes" {
		t.Fatal("edit scope missing")
	}
	if s := previewSection(t, p, "Before"); s.Text != "before\n\tline\n" || s.Kind != "removed" {
		t.Fatal("old text altered")
	}
	if s := previewSection(t, p, "After"); s.Text != "after\n" || s.Kind != "added" {
		t.Fatal("new text altered")
	}
	p = previewInput(t, "Write", map[string]any{"file_path": "/work/empty", "content": ""})
	if s := previewSection(t, p, "Contents to write"); s.Text != "" || s.Kind != "added" {
		t.Fatal("empty write lost")
	}
	p = previewInput(t, "MultiEdit", map[string]any{"file_path": "/work/a", "edits": []any{map[string]any{"old_string": "one", "new_string": "two"}, map[string]any{"old_string": "three", "new_string": "", "future_flag": false}}})
	if previewSection(t, p, "Change 1 · Before").Text != "one" || previewSection(t, p, "Change 2 · After").Text != "" {
		t.Fatal("multiple edits lost or reordered")
	}
	if !strings.Contains(previewSection(t, p, "Change 2 · Additional input").Text, "false") {
		t.Fatal("nested extra input lost")
	}
}
func TestClaudePermissionPreviewMalformedFieldsRemainVisible(t *testing.T) {
	for _, tool := range []string{"Bash", "Edit", "Write", "MultiEdit"} {
		p := previewInput(t, tool, map[string]any{"command": nil, "description": 123, "file_path": false, "content": []string{"x"}, "edits": []any{nil}, "replace_all": "true"})
		extra := previewSection(t, p, "Additional input").Text
		for _, value := range []string{"command", "description", "file_path", "content", "edits", "replace_all"} {
			if !strings.Contains(extra, value) {
				t.Fatalf("%s disappeared for %s", value, tool)
			}
		}
	}
	for _, raw := range []string{"null", "{}", "[]", "bad JSON"} {
		if makeClaudePermissionPreview("Bash", raw) != nil {
			t.Fatal("expected raw fallback", raw)
		}
	}
	if makeClaudePermissionPreview("Unknown", `{"command":"echo test"}`) != nil {
		t.Fatal("unknown tool rendered as a known action")
	}
}
func TestClaudePermissionPreviewMakesHiddenControlsVisible(t *testing.T) {
	p := previewInput(t, "Bash", map[string]any{"command": "echo ok\r\x1b[2J\u202ereversed\u2066text", "description": "title\x00hidden"})
	if previewSection(t, p, "Command").Text != `echo ok\u000d\u001b[2J\u202ereversed\u2066text` || p.Description != `title\u0000hidden` {
		t.Fatal("invisible controls can disguise input")
	}
}
