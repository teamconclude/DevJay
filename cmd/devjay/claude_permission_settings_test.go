package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudePermissionSettingsPreserveOthers(t *testing.T) {
	input := []byte(`{"permissions":{"ask":["Bash(git rebase:*)"]},"hooks":{"Stop":[{"hooks":[{"type":"command","command":"other"}]}],"PermissionRequest":[{"matcher":"Bash","hooks":[{"type":"command","command":"existing"}]}]},"unknown":{"number":42}}`)
	command := claudeHookCommand("/Applications/DevJay's app.app/Contents/MacOS/DevJay")
	enabled, was, err := editClaudeHookSettings(input, command, true)
	if err != nil || was {
		t.Fatal(err)
	}
	again, was, err := editClaudeHookSettings(enabled, command, true)
	if err != nil || !was || string(again) != string(enabled) {
		t.Fatal("enable not idempotent", err)
	}
	disabled, was, err := editClaudeHookSettings(enabled, command, false)
	if err != nil || !was {
		t.Fatal(err)
	}
	var before, after any
	json.Unmarshal(input, &before)
	json.Unmarshal(disabled, &after)
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("unrelated settings changed", string(b))
	}
	if !strings.Contains(command, `'"'"'`) {
		t.Fatal("executable not shell quoted")
	}
}
func TestClaudePermissionSettingsRejectUnknownAndBackup(t *testing.T) {
	for _, input := range []string{`{`, `null`, `[]`, `{"hooks":[]}`, `{"hooks":{"PermissionRequest":{}}}`, `{"hooks":{"PermissionRequest":[{}]}}`} {
		if _, _, err := editClaudeHookSettings([]byte(input), "command", true); err == nil {
			t.Fatalf("accepted %s", input)
		}
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	input := []byte(`{"custom":true}`)
	os.WriteFile(path, input, 0600)
	if err := setClaudeHookSettings(path, "command", true); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(path + ".devjay-backup-*")
	if len(backups) != 1 {
		t.Fatal("missing backup")
	}
	saved, _ := os.ReadFile(backups[0])
	if string(saved) != string(input) {
		t.Fatal("backup differs")
	}
	present, err := claudeHookEnabled(path, "command")
	if err != nil || !present {
		t.Fatal("not enabled", err)
	}
	if err := setClaudeHookSettings(path, "command", false); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	os.Symlink(path, link)
	if err := setClaudeHookSettings(link, "command", true); err == nil {
		t.Fatal("followed symlink")
	}
}

func TestClaudePermissionSettingsPreservesLargeNumbers(t *testing.T) {
	out, _, err := editClaudeHookSettings([]byte(`{"customID":9007199254740993}`), "command", true)
	if err != nil || !strings.Contains(string(out), "9007199254740993") {
		t.Fatal("rounded unknown setting", string(out), err)
	}
	if _, _, err := editClaudeHookSettings([]byte(`{} trailing`), "command", true); err == nil {
		t.Fatal("accepted trailing junk")
	}
}

func TestClaudePermissionSettingsReplacesPreviousApp(t *testing.T) {
	first := claudeHookCommand("/old/DevJay")
	second := claudeHookCommand("/new/DevJay")
	data, _, _ := editClaudeHookSettings(nil, first, true)
	data, _, err := editClaudeHookSettings(data, second, true)
	if err != nil || strings.Contains(string(data), "/old/DevJay") || !strings.Contains(string(data), "/new/DevJay") {
		t.Fatal("kept duplicate app integration", err)
	}
	untouched, _, err := editClaudeHookSettings(data, first, false)
	if err != nil || string(untouched) != string(data) {
		t.Fatal("old app disabled the new app hook")
	}
}

func TestClaudePermissionTimeoutUpgradePreservesExistingGlobalSettings(t *testing.T) {
	command := claudeHookCommand("/Applications/DevJay.app/Contents/MacOS/DevJay")
	old, _, err := editClaudeHookSettings([]byte(`{"customID":9007199254740993,"permissions":{"ask":["Bash(git tag -d:*)"]},"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"other","timeout":310}]}]}}`), command, true)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(old)))
	decoder.UseNumber()
	if err := decoder.Decode(&data); err != nil {
		t.Fatal(err)
	}
	groups := data["hooks"].(map[string]any)["PermissionRequest"].([]any)
	handler := groups[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
	handler["timeout"] = 310
	handler["customField"] = "keep me"
	old, _ = json.Marshal(data)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, old, 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateClaudeHookSettings(path, command); err != nil {
		t.Fatal(err)
	}
	updated, _ := os.ReadFile(path)
	handler["timeout"] = claudeHookTimeoutSeconds
	want, _ := json.MarshalIndent(data, "", "  ")
	if string(updated) != string(append(want, '\n')) {
		t.Fatalf("changed more than the old timeout: %s", updated)
	}
	backups, _ := filepath.Glob(path + ".devjay-backup-*")
	if len(backups) != 1 {
		t.Fatal("missing migration backup")
	}
	backup, _ := os.ReadFile(backups[0])
	if string(backup) != string(old) {
		t.Fatal("backup differs")
	}
	if err := migrateClaudeHookSettings(path, command); err != nil {
		t.Fatal(err)
	}
	backups, _ = filepath.Glob(path + ".devjay-backup-*")
	if len(backups) != 1 {
		t.Fatal("migration repeated")
	}
}

func TestClaudePermissionMigrationNeverEnablesOrReplacesCustomHook(t *testing.T) {
	command := claudeHookCommand("/Applications/DevJay.app/Contents/MacOS/DevJay")
	for _, input := range []string{
		`{}`, `{"hooks":{"PermissionRequest":[]}}`,
		`{"hooks":{"PermissionRequest":[{"matcher":"Bash|Edit|Write|MultiEdit","hooks":[{"type":"command","command":"other","timeout":310,"statusMessage":"Waiting for your response in DevJay"}]}]}}`,
	} {
		path := filepath.Join(t.TempDir(), "settings.json")
		os.WriteFile(path, []byte(input), 0600)
		if err := migrateClaudeHookSettings(path, command); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(path)
		if string(got) != input {
			t.Fatal("changed an unrelated/disabled hook")
		}
	}
	path := filepath.Join(t.TempDir(), "missing", "settings.json")
	if err := migrateClaudeHookSettings(path, command); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatal("created missing Claude configuration")
	}
	data, _, _ := editClaudeHookSettings(nil, command, true)
	data = []byte(strings.Replace(string(data), `"timeout": 86410`, `"timeout": 90`, 1))
	path = filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(path, data, 0600)
	if err := migrateClaudeHookSettings(path, command); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("replaced a custom timeout")
	}
	link := filepath.Join(t.TempDir(), "link")
	os.Symlink(path, link)
	if err := migrateClaudeHookSettings(link, command); err == nil {
		t.Fatal("followed symlink")
	}
}

func TestClaudePermissionMigrationFollowsActiveAppBundle(t *testing.T) {
	installed := claudeHookCommand("/Applications/DevJay.app/Contents/MacOS/DevJay")
	development := claudeHookCommand("/Users/test/DevJay/bin/DevJay.app/Contents/MacOS/DevJay")
	for _, tc := range []struct{ name, old, current string }{
		{"install", development, installed},
		{"development", installed, development},
		{"moved", claudeHookCommand("/Volumes/DevJay's disk/DevJay.app/Contents/MacOS/DevJay"), installed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			original, _, err := editClaudeHookSettings([]byte(`{"customID":9007199254740993,"permissions":{"ask":["Bash(git push:*)"]},"hooks":{"PermissionRequest":[{"hooks":[{"type":"command","command":"other","timeout":310}]}],"Stop":[{"hooks":[{"type":"command","command":"status"}]}]}}`), tc.old, true)
			if err != nil {
				t.Fatal(err)
			}
			var expected map[string]any
			decoder := json.NewDecoder(strings.NewReader(string(original)))
			decoder.UseNumber()
			if err := decoder.Decode(&expected); err != nil {
				t.Fatal(err)
			}
			groups := expected["hooks"].(map[string]any)["PermissionRequest"].([]any)
			handler := groups[1].(map[string]any)["hooks"].([]any)[0].(map[string]any)
			handler["timeout"] = 90
			handler["customField"] = "keep me"
			original, err = json.Marshal(expected)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, original, 0600); err != nil {
				t.Fatal(err)
			}
			enabled, err := claudeHookEnabled(path, tc.current)
			if err != nil || enabled {
				t.Fatalf("fixture not associated with previous app: %v %v", enabled, err)
			}
			if err := migrateClaudeHookSettings(path, tc.current); err != nil {
				t.Fatal(err)
			}
			enabled, err = claudeHookEnabled(path, tc.current)
			if err != nil || !enabled {
				t.Fatalf("active app not enabled: %v %v", enabled, err)
			}
			if enabled, err := claudeHookEnabled(path, tc.old); err != nil || enabled {
				t.Fatalf("old app still owns hook: %v %v", enabled, err)
			}
			handler["command"] = tc.current
			want, _ := json.MarshalIndent(expected, "", "  ")
			got, _ := os.ReadFile(path)
			if string(got) != string(append(want, '\n')) {
				t.Fatalf("changed unrelated settings: %s", got)
			}
			backups, _ := filepath.Glob(path + ".devjay-backup-*")
			if len(backups) != 1 {
				t.Fatal("expected one recoverable migration backup")
			}
			backup, _ := os.ReadFile(backups[0])
			if string(backup) != string(original) {
				t.Fatal("backup differs")
			}
			if err := migrateClaudeHookSettings(path, tc.current); err != nil {
				t.Fatal(err)
			}
			backups, _ = filepath.Glob(path + ".devjay-backup-*")
			if len(backups) != 1 {
				t.Fatal("migration repeated")
			}
		})
	}
}

func TestClaudePermissionMigrationDoesNotAdoptCustomOrDisabledHooks(t *testing.T) {
	current := claudeHookCommand("/Applications/DevJay.app/Contents/MacOS/DevJay")
	old := claudeHookCommand("/old/DevJay.app/Contents/MacOS/DevJay")
	for _, tc := range []struct {
		name, command, matcher, label string
		disabled                      bool
	}{
		{"disabled", old, "Bash|Edit|Write|MultiEdit", claudeHookLabel, true},
		{"custom matcher", old, "Bash", claudeHookLabel, false},
		{"custom label", old, "Bash|Edit|Write|MultiEdit", "Custom handler", false},
		{"wrapper", claudeHookCommand("/scripts/DevJay"), "Bash|Edit|Write|MultiEdit", claudeHookLabel, false},
		{"empty command", "' --claude-permission-hook", "Bash|Edit|Write|MultiEdit", claudeHookLabel, false},
		{"compound", old + "; echo extra", "Bash|Edit|Write|MultiEdit", claudeHookLabel, false},
		{"injected", `'/old/DevJay.app/Contents/MacOS/DevJay'; echo extra; '' --claude-permission-hook`, "Bash|Edit|Write|MultiEdit", claudeHookLabel, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data, _ := json.Marshal(map[string]any{"disableAllHooks": tc.disabled, "hooks": map[string]any{"PermissionRequest": []any{map[string]any{"matcher": tc.matcher, "hooks": []any{map[string]any{"type": "command", "command": tc.command, "statusMessage": tc.label, "timeout": 310}}}}}})
			path := filepath.Join(t.TempDir(), "settings.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := migrateClaudeHookSettings(path, current); err != nil {
				t.Fatal(err)
			}
			got, _ := os.ReadFile(path)
			if string(got) != string(data) {
				t.Fatalf("changed customized/disabled setup: %s", got)
			}
			backups, _ := filepath.Glob(path + ".devjay-backup-*")
			if len(backups) != 0 {
				t.Fatal("wrote unnecessary backup")
			}
		})
	}
}

func TestClaudePermissionMigrationRejectsAmbiguousManagedHooks(t *testing.T) {
	current := claudeHookCommand("/Applications/DevJay.app/Contents/MacOS/DevJay")
	data, _, _ := editClaudeHookSettings(nil, claudeHookCommand("/old/DevJay.app/Contents/MacOS/DevJay"), true)
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	hooks := root["hooks"].(map[string]any)
	groups := hooks["PermissionRequest"].([]any)
	hooks["PermissionRequest"] = append(groups, groups[0])
	data, _ = json.Marshal(root)
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := migrateClaudeHookSettings(path, current); err == nil {
		t.Fatal("accepted duplicate handlers")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(data) {
		t.Fatal("changed ambiguous setup")
	}
}
