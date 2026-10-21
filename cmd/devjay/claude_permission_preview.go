package main

import (
	"encoding/json"
	"fmt"
)

// Presentation only: never rewrite the tool input or generate a command from
// its description. Unknown or malformed fields remain visible as extra input.
type claudePermissionPreview struct {
	Title       string                    `json:"title"`
	Description string                    `json:"description,omitempty"`
	Sections    []claudePermissionSection `json:"sections"`
}
type claudePermissionSection struct {
	Label string `json:"label"`
	Kind  string `json:"kind"`
	Text  string `json:"text"`
}

func makeClaudePermissionPreview(tool, input string) *claudePermissionPreview {
	titles := map[string]string{"Bash": "Bash command", "Edit": "Edit file", "Write": "Write file", "MultiEdit": "Edit file"}
	title, known := titles[tool]
	var fields map[string]json.RawMessage
	if !known || json.Unmarshal([]byte(input), &fields) != nil || fields == nil {
		return nil
	}
	preview := &claudePermissionPreview{Title: title}
	appendSection := func(label, kind, text string) {
		preview.Sections = append(preview.Sections, claudePermissionSection{label, kind, safePermissionText(text)})
	}
	takeString := func(fields map[string]json.RawMessage, name string) (string, bool) {
		raw := fields[name]
		var text string
		if len(raw) == 0 || raw[0] != '"' || json.Unmarshal(raw, &text) != nil {
			return "", false
		}
		delete(fields, name)
		return text, true
	}
	showString := func(fields map[string]json.RawMessage, name, label, kind string) {
		if text, ok := takeString(fields, name); ok {
			appendSection(label, kind, text)
		}
	}
	showBool := func(fields map[string]json.RawMessage, name, label string, warn bool) {
		raw := string(fields[name])
		if raw != "true" && raw != "false" {
			return
		}
		delete(fields, name)
		value, kind := "No", "note"
		if raw == "true" {
			value = "Yes"
			if warn {
				kind = "warning"
			}
		}
		appendSection(label, kind, value)
	}
	showRemaining := func(fields map[string]json.RawMessage, label string) {
		if len(fields) == 0 {
			return
		}
		data, _ := json.MarshalIndent(fields, "", "  ")
		appendSection(label, "code", string(data))
	}
	if description, ok := takeString(fields, "description"); ok {
		preview.Description = safePermissionText(description)
	}
	switch tool {
	case "Bash":
		showString(fields, "command", "Command", "code")
		if raw, ok := fields["timeout"]; ok && len(raw) > 0 && raw[0] != '"' && string(raw) != "null" {
			var number json.Number
			if json.Unmarshal(raw, &number) == nil {
				delete(fields, "timeout")
				appendSection("Timeout", "note", number.String()+" ms")
			}
		}
		showBool(fields, "run_in_background", "Run in background", false)
		showBool(fields, "dangerouslyDisableSandbox", "Disable sandbox for this command", true)
	case "Write":
		showString(fields, "file_path", "File", "path")
		showString(fields, "content", "Contents to write", "added")
	case "Edit", "MultiEdit":
		showString(fields, "file_path", "File", "path")
		showEdit := func(edit map[string]json.RawMessage, prefix string) {
			showBool(edit, "replace_all", prefix+"Replace all matches", false)
			showString(edit, "old_string", prefix+"Before", "removed")
			showString(edit, "new_string", prefix+"After", "added")
		}
		if tool == "Edit" {
			showEdit(fields, "")
		} else {
			var edits []map[string]json.RawMessage
			raw := fields["edits"]
			if len(raw) > 0 && raw[0] == '[' && json.Unmarshal(raw, &edits) == nil {
				// Retain malformed/null edits as raw input instead of losing them.
				valid := len(edits) > 0
				for _, edit := range edits {
					if edit == nil {
						valid = false
					}
				}
				if valid {
					delete(fields, "edits")
					for i, edit := range edits {
						prefix := fmt.Sprintf("Change %d · ", i+1)
						showEdit(edit, prefix)
						showRemaining(edit, prefix+"Additional input")
					}
				}
			}
		}
	}
	showRemaining(fields, "Additional input")
	if len(preview.Sections) == 0 {
		return nil
	}
	return preview
}
