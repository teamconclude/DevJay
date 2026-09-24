package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

func nonInteractiveSSH(command string) string {
	if command == "" {
		command = "/usr/bin/ssh"
	}
	return command + " -oBatchMode=yes -oStrictHostKeyChecking=yes"
}

func effectiveSSH(ctx context.Context, path string) (string, error) {
	if command := os.Getenv("GIT_SSH_COMMAND"); command != "" {
		return nonInteractiveSSH(command), nil
	}
	out, err := git(ctx, path, "config", "--get", "core.sshCommand")
	if err != nil && !exitedWith(err, 1) {
		return "", err
	}
	return nonInteractiveSSH(strings.TrimSpace(string(out))), nil
}

func gitVerb(args []string) string {
	for len(args) > 0 {
		if args[0] == "-c" || args[0] == "-C" {
			args = args[2:]
			continue
		}
		return args[0]
	}
	return ""
}
func isNetworkGit(args []string) bool {
	return slices.Contains([]string{"fetch", "ls-remote", "push"}, gitVerb(args))
}

// Linked worktrees share remote-tracking refs. Serialize fetches before taking
// a command slot, and allow cancellation while waiting for either resource.
var fetchLocks sync.Map

func networkGit(ctx context.Context, slots chan struct{}, timeout time.Duration, path string, args ...string) ([]byte, error) {
	ssh, err := effectiveSSH(ctx, path)
	if err != nil {
		return nil, err
	}
	if gitVerb(args) == "fetch" {
		common, err := git(ctx, path, "rev-parse", "--path-format=absolute", "--git-common-dir")
		if err != nil {
			return nil, err
		}
		value, _ := fetchLocks.LoadOrStore(strings.TrimSpace(string(common)), make(chan struct{}, 1))
		lock := value.(chan struct{})
		select {
		case lock <- struct{}{}:
			defer func() { <-lock }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	prefix := []string{"-C", path, "-c", "core.hooksPath=/dev/null"}
	return runCommandWithEnv(ctx, slots, timeout, []string{"GIT_SSH_COMMAND=" + ssh}, "git", append(prefix, args...)...)
}

func isSSHRemote(remote string) bool {
	return strings.HasPrefix(remote, "ssh://") || (!strings.Contains(remote, "://") && !filepath.IsAbs(remote) && strings.Contains(remote, ":"))
}

// Drain arbitrarily large indexes while retaining only one NUL-delimited entry.
type gitRecordCheck struct {
	pending  []byte
	reject   func(string) bool
	rejected bool
}

func (w *gitRecordCheck) Write(data []byte) (int, error) {
	n := len(data)
	for len(data) > 0 {
		end := bytes.IndexByte(data, 0)
		if end < 0 {
			w.pending = append(w.pending, data...)
			if len(w.pending) > 1<<20 {
				return 0, errors.New("invalid index entry")
			}
			break
		}
		w.pending = append(w.pending, data[:end]...)
		w.rejected = w.rejected || w.reject(string(w.pending))
		w.pending = w.pending[:0]
		data = data[end+1:]
	}
	return n, nil
}

// Retain ten verified successful recoveries. Unmarked refs (including every
// failed/partial action and older versions' backups) are never pruned.
func finishRecovery(ctx context.Context, p *CheckoutPlan, result *CheckoutAction) {
	if result.Recovery == "" {
		return
	}
	marker := fmt.Sprintf("refs/devjay/completed/%020d/%s", time.Now().UnixNano(), p.ID)
	if _, err := actionGit(ctx, p.path, "update-ref", marker, p.Head, ""); err != nil {
		return
	}
	out, err := git(ctx, p.path, "for-each-ref", "--sort=refname", "--format=%(refname) %(objectname)", "refs/devjay/completed/")
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines[:max(0, len(lines)-10)] {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		parts := strings.Split(fields[0], "/")
		if len(parts) != 5 {
			continue
		}
		backup := "refs/devjay/backups/" + parts[4]
		if _, err := actionGit(ctx, p.path, "update-ref", "-d", backup, fields[1]); err == nil {
			_, _ = actionGit(ctx, p.path, "update-ref", "-d", fields[0], fields[1])
		}
	}
}
