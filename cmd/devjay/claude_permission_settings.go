package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const claudeHookLabel = "Waiting for your response in DevJay"

func shellQuote(value string) string { return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'" }
func claudeHookCommand(executable string) string {
	return shellQuote(executable) + " --claude-permission-hook"
}
func ownClaudeHook(h map[string]any, command string) bool {
	return h["type"] == "command" && h["command"] == command && h["statusMessage"] == claudeHookLabel
}
func editClaudeHookSettings(data []byte, command string, enabled bool) ([]byte, bool, error) {
	root := map[string]any{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if len(data) > 0 && (!json.Valid(data) || decoder.Decode(&root) != nil) {
		return nil, false, errors.New("Claude settings contain invalid JSON; they were not changed.")
	}
	if root == nil {
		return nil, false, errors.New("Claude settings must be a JSON object.")
	}
	hooks, ok := root["hooks"].(map[string]any)
	if root["hooks"] != nil && !ok {
		return nil, false, errors.New("Claude hooks have an unfamiliar format.")
	}
	if hooks == nil {
		hooks = map[string]any{}
	}
	groups, ok := hooks["PermissionRequest"].([]any)
	if hooks["PermissionRequest"] != nil && !ok {
		return nil, false, errors.New("Claude permission hooks have an unfamiliar format.")
	}
	present := false
	var kept []any
	for _, value := range groups {
		group, ok := value.(map[string]any)
		if !ok {
			return nil, false, errors.New("Claude permission hook group has an unfamiliar format.")
		}
		handlers, ok := group["hooks"].([]any)
		if !ok {
			return nil, false, errors.New("Claude permission hook handlers have an unfamiliar format.")
		}
		retained := make([]any, 0, len(handlers))
		removed := false
		for _, handler := range handlers {
			h, ok := handler.(map[string]any)
			if !ok {
				return nil, false, errors.New("Claude permission hook has an unfamiliar format.")
			}
			ours := ownClaudeHook(h, command)
			if ours {
				present = true
			}
			oldCommand, _ := h["command"].(string)
			managed := h["type"] == "command" && h["statusMessage"] == claudeHookLabel && strings.HasPrefix(oldCommand, "'") && strings.HasSuffix(oldCommand, "' --claude-permission-hook")
			if ours || (enabled && managed) {
				removed = true
				continue
			}
			retained = append(retained, handler)
		}
		if !removed {
			kept = append(kept, value)
		} else if len(retained) > 0 {
			group["hooks"] = retained
			kept = append(kept, group)
		}
	}
	if enabled {
		kept = append(kept, map[string]any{"matcher": "Bash|Edit|Write|MultiEdit", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": claudeHookTimeoutSeconds, "statusMessage": claudeHookLabel}}})
	}
	if !present && !enabled {
		return data, false, nil
	}
	if len(kept) > 0 {
		hooks["PermissionRequest"] = kept
	} else {
		delete(hooks, "PermissionRequest")
	}
	if len(hooks) > 0 {
		root["hooks"] = hooks
	} else {
		delete(root, "hooks")
	}
	result, err := json.MarshalIndent(root, "", "  ")
	return append(result, '\n'), present, err
}
func readClaudeHookSettings(path string) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var st unix.Stat_t
	if unix.Fstat(fd, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != uint32(os.Getuid()) || st.Size > 1024*1024 {
		return nil, errors.New("Claude settings must be an owned regular file smaller than 1 MiB.")
	}
	data, err := io.ReadAll(io.LimitReader(file, 1024*1024+1))
	if len(data) > 1024*1024 {
		return nil, errors.New("Claude settings are too large.")
	}
	return data, err
}
func claudeHookEnabled(path, command string) (bool, error) {
	data, err := readClaudeHookSettings(path)
	if err != nil {
		return false, err
	}
	_, present, err := editClaudeHookSettings(data, command, false)
	return present, err
}
func setClaudeHookSettings(path, command string, enabled bool) error {
	parent := filepath.Dir(path)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return err
	}
	data, err := readClaudeHookSettings(path)
	if err != nil {
		return err
	}
	next, _, err := editClaudeHookSettings(data, command, enabled)
	if err != nil {
		return err
	}
	return writeClaudeHookSettings(path, data, next)
}

// Upgrade only this executable's existing, previously enabled hook. Never
// enable a missing hook, replace another build's hook, or alter custom timeouts.
func upgradeClaudeHookTimeout(path, command string) error {
	data, err := readClaudeHookSettings(path)
	if err != nil || len(data) == 0 {
		return err
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if !json.Valid(data) || decoder.Decode(&root) != nil || root == nil {
		return errors.New("Claude settings must contain a valid JSON object.")
	}
	hooks, _ := root["hooks"].(map[string]any)
	groups, _ := hooks["PermissionRequest"].([]any)
	changed := false
	for _, value := range groups {
		group, _ := value.(map[string]any)
		if group["matcher"] != "Bash|Edit|Write|MultiEdit" {
			continue
		}
		handlers, _ := group["hooks"].([]any)
		for _, value := range handlers {
			h, _ := value.(map[string]any)
			if ownClaudeHook(h, command) && h["timeout"] == json.Number("310") {
				h["timeout"] = claudeHookTimeoutSeconds
				changed = true
			}
		}
	}
	if !changed {
		return nil
	}
	next, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeClaudeHookSettings(path, data, append(next, '\n'))
}

func writeClaudeHookSettings(path string, data, next []byte) error {
	parent := filepath.Dir(path)
	st, err := os.Lstat(parent)
	if err != nil {
		return err
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() || int(stat.Uid) != os.Getuid() {
		return errors.New("Claude settings directory must be owned by you and cannot be a symlink.")
	}
	if bytes.Equal(data, next) {
		return nil
	}
	// Keep an exact recoverable copy, including unrelated hooks and preferences.
	if len(data) > 0 {
		backup := fmt.Sprintf("%s.devjay-backup-%d", path, time.Now().UnixNano())
		f, err := os.OpenFile(backup, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	temp, err := os.CreateTemp(parent, ".devjay-hooks-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(next); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err = temp.Close(); err != nil {
		return err
	}
	current, err := readClaudeHookSettings(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(current, data) {
		return errors.New("Claude settings changed while saving. Please try again.")
	}
	return os.Rename(temp.Name(), path)
}
