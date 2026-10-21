package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func (m *siteManager) registerClaudePermissionSettings() {
	m.app.Event.On("devjay:claude-permission-settings", func(e *application.CustomEvent) {
		if e.Sender != "settings" {
			return
		}
		var req struct {
			Operation string `json:"operation"`
		}
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		if req.Operation != "status" && req.Operation != "enable" && req.Operation != "disable" {
			return
		}
		m.send(func() { m.updateClaudePermissionSettings(req.Operation) })
	})
}

// Called on the manager queue. Read/migrate the single global hook off the UI
// thread, including at startup so enabled users do not need to enable it again.
func (m *siteManager) updateClaudePermissionSettings(operation string) {
	if m.claudePermissionSettingsBusy {
		return
	}
	if operation == "enable" && m.claudePermissions == nil {
		m.app.Event.Emit("devjay:claude-permission-settings-state", map[string]any{"available": false, "error": "The local Claude connection is unavailable. Restart DevJay to try again."})
		return
	}
	m.claudePermissionSettingsBusy = true
	go func() {
		home, err := os.UserHomeDir()
		executable, exeErr := os.Executable()
		if err == nil {
			err = exeErr
		}
		path := filepath.Join(home, ".claude", "settings.json")
		command := claudeHookCommand(executable)
		if err == nil {
			if operation == "status" {
				err = upgradeClaudeHookTimeout(path, command)
			} else {
				err = setClaudeHookSettings(path, command, operation == "enable")
			}
		}
		enabled, statusErr := claudeHookEnabled(path, command)
		if err == nil {
			err = statusErr
		}
		m.send(func() {
			m.claudePermissionSettingsBusy = false
			m.claudePermissionSettingsKnown = statusErr == nil
			m.claudePermissionSettingsEnabled = enabled
			message := ""
			if err != nil {
				message = err.Error()
			}
			if statusErr == nil {
				// Claude may keep using a previously loaded hook after the
				// settings file changes. Enforce the confirmed preference here.
				m.claudePermissions.setEnabled(enabled)
			}
			m.app.Event.Emit("devjay:claude-permission-settings-state", map[string]any{"enabled": enabled, "error": message, "available": m.claudePermissions != nil})
			m.publishSnapshot(false)
		})
	}()
}
