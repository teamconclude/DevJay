package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RebaseCheck struct {
	SiteID     string   `json:"siteID"`
	Branch     string   `json:"branch"`
	Head       string   `json:"head"`
	BaseHead   string   `json:"baseHead"`
	NewHead    string   `json:"-"`
	TargetHead string   `json:"targetHead,omitempty"`
	Target     string   `json:"target"`
	Status     string   `json:"status"`
	Message    string   `json:"message"`
	Files      []string `json:"files,omitempty"`
}

// Use a private Git directory and index, with read-only access to the Site's
// objects. No worktree registration, local refs, config, hooks or files are changed.
// Repository hooks, filters, rerere and user-configured merge drivers must not
// turn a conflict into a false success (or run side effects) during the trial.
func rebasePreview(ctx context.Context, path string) RebaseCheck {
	return runRebaseTrial(ctx, path, false, nil)
}

func runRebaseTrial(ctx context.Context, path string, realIdentity bool, apply func(string, RebaseCheck) error) RebaseCheck {
	return runRebaseTrialFrom(ctx, path, realIdentity, "", apply)
}

// sourceHead is only used to reproduce remote history for publication checks.
// The caller supplies a verified commit ID; the Site still remains untouched.
func runRebaseTrialFrom(ctx context.Context, path string, realIdentity bool, sourceHead string, apply func(string, RebaseCheck) error) RebaseCheck {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result := RebaseCheck{Status: "unavailable", Message: "Could not verify the rebase safely." + manualPublish}
	s, m := inspectLocal(ctx, path)
	if sourceHead == "" {
		sourceHead = m.OID
	}
	result.Branch, result.Head, result.Target = s.Branch, m.OID, m.DefaultBranch
	if s.Base != nil {
		result.BaseHead = s.Base.Head
	}
	if m.ReadError != nil || m.Initial || m.Branch == "(detached)" || m.DefaultRef == "" || s.IsMainBranch {
		return result
	}
	if s.Changes > 0 || s.Conflicts > 0 {
		result.Message = "Commit or stash local work before checking a rebase. Your checkout has not been changed."
		return result
	}
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "BISECT_LOG"} {
		statePath := gitString(ctx, path, "rev-parse", "--path-format=absolute", "--git-path", name)
		if statePath == "" {
			return result
		}
		if _, err := os.Stat(statePath); !os.IsNotExist(err) {
			result.Message = "A Git operation is in progress. Finish it manually before checking a rebase."
			return result
		}
	}
	// An alternate object store is only a read source. New objects and all
	// temporary refs belong to the disposable repository.
	objects := gitString(ctx, path, "rev-parse", "--path-format=absolute", "--git-path", "objects")
	format := gitString(ctx, path, "rev-parse", "--show-object-format")
	remote := gitString(ctx, path, "remote", "get-url", m.DefaultRemote)
	if objects == "" || strings.ContainsAny(objects, "\r\n") || remote == "" || (format != "sha1" && format != "sha256") {
		return result
	}
	// Relative local remote URLs are relative to the Site, not the temporary repo.
	if !filepath.IsAbs(remote) && !strings.Contains(remote, ":") {
		remote = filepath.Join(path, remote)
	}
	tmp, err := os.MkdirTemp("", "devjay-rebase-preview-")
	if err != nil {
		return result
	}
	defer os.RemoveAll(tmp)
	isolatedEnv := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=0", "GIT_ATTR_NOSYSTEM=1", "GIT_LFS_SKIP_SMUDGE=1"}
	name, email := "DevJay preview", "preview@devjay.invalid"
	if realIdentity {
		if gitString(ctx, path, "config", "--bool", "commit.gpgsign") == "true" {
			result.Message = "This repository signs commits. Rebase manually to preserve your signing workflow."
			return result
		}
		ident := gitString(ctx, path, "var", "GIT_COMMITTER_IDENT")
		start, end := strings.LastIndex(ident, " <"), strings.LastIndex(ident, "> ")
		if start < 1 || end <= start+2 {
			result.Message = "Configure your Git name and email before rebasing."
			return result
		}
		name, email = ident[:start], ident[start+2:end]
	}
	trial := func(args ...string) ([]byte, error) {
		prefix := []string{"-C", tmp, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "rerere.enabled=false", "-c", "merge.union.driver=false", "-c", "commit.gpgsign=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false", "-c", "user.name=" + name, "-c", "user.email=" + email}
		return runCommandWithEnv(ctx, actionCommandSlots, 60*time.Second, isolatedEnv, "git", append(prefix, args...)...)
	}
	if _, err := trial("init", "--quiet", "--template=", "--object-format="+format); err != nil {
		return result
	}
	if err := os.WriteFile(filepath.Join(tmp, ".git", "objects", "info", "alternates"), []byte(objects+"\n"), 0600); err != nil {
		return result
	}
	// Fetch only the destination into the disposable repository. Normal global
	// credential helpers remain available for this network-only command.
	ssh, sshErr := effectiveSSH(ctx, path)
	if sshErr != nil {
		return result
	}
	if _, err := runCommandWithEnv(ctx, actionCommandSlots, 30*time.Second, []string{"GIT_SSH_COMMAND=" + ssh}, "git", "-C", tmp, "-c", "core.hooksPath=/dev/null", "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "--", remote, "+refs/heads/"+m.DefaultBranch+":refs/remotes/preview/base"); err != nil {
		result.Message = "Could not fetch the latest default branch. Check network access and authentication, then try again."
		return result
	}
	target, err := trial("rev-parse", "--verify", "refs/remotes/preview/base^{commit}")
	if err != nil {
		return result
	}
	result.TargetHead = strings.TrimSpace(string(target))
	if _, err := trial("merge-base", sourceHead, result.TargetHead); err != nil {
		result.Message = "Could not establish shared history with the default branch. Review the branch and rebase manually."
		return result
	}
	if _, err := trial("checkout", "--quiet", "--detach", sourceHead); err != nil {
		return result
	}
	_, rebaseErr := trial("rebase", "--rebase-merges", "--no-autostash", "--no-update-refs", "--no-gpg-sign", result.TargetHead)
	if ctx.Err() != nil || errors.Is(rebaseErr, context.DeadlineExceeded) || errors.Is(rebaseErr, context.Canceled) {
		return result
	}
	if rebaseErr != nil {
		conflicts, err := trial("diff", "--name-only", "--diff-filter=U", "-z")
		if exitedWith(rebaseErr, 1) && err == nil && len(conflicts) > 0 {
			result.Status = "conflicts"
			result.Message = "Conflicts found. Resolve the rebase and push manually with Claude or Astra; DevJay will not continue. More conflicts may appear after resolving this first stop."
			result.Files = strings.Split(strings.TrimSuffix(string(conflicts), "\x00"), "\x00")
		}
	} else {
		head, err := trial("rev-parse", "HEAD")
		if err != nil {
			return result
		}
		result.NewHead = strings.TrimSpace(string(head))
		result.Status = "clean"
		result.Message = "No conflicts found against the latest fetched default branch. This check used a temporary checkout; your Site is unchanged. Recheck before applying if either branch changes."
	}
	// A result for a checkout that changed while the trial ran is not actionable.
	after, meta := inspectLocal(ctx, path)
	if meta.ReadError != nil || meta.OID != m.OID || meta.Branch != m.Branch || meta.DefaultRef != m.DefaultRef || after.Changes > 0 || after.Conflicts > 0 {
		result.Status, result.Files = "stale", nil
		result.Message = "The checkout changed during the check. Check the rebase again."
	}
	if result.Status == "clean" && apply != nil {
		if err := apply(tmp, result); err != nil {
			result.Status, result.Message = "unavailable", fmt.Sprint(err)
		} else {
			result.Status, result.Message = "applied", "Rebase completed. Nothing was pushed."
		}
	}
	return result
}
