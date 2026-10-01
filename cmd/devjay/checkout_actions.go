package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type ActionRequest struct {
	Anchor       *cardAnchor `json:"anchor,omitempty"`
	RequestID    string      `json:"requestID,omitempty"`
	SiteID       string      `json:"siteID"`
	Kind         string      `json:"kind"`
	Branch       string      `json:"branch"`
	Head         string      `json:"head"`
	DeleteBranch bool        `json:"deleteBranch"`
	TargetBranch string      `json:"targetBranch,omitempty"`
	TargetRemote string      `json:"targetRemote,omitempty"`
	UpdateTarget bool        `json:"updateTarget,omitempty"`
}

type CheckoutPlan struct {
	Commits                                                            int `json:"commits,omitempty"`
	branchGeneration                                                   string
	ID                                                                 string    `json:"id"`
	Kind                                                               string    `json:"kind"`
	Branch                                                             string    `json:"branch"`
	Head                                                               string    `json:"head"`
	Target                                                             string    `json:"target"`
	TargetHead                                                         string    `json:"targetHead"`
	DeleteBranch                                                       bool      `json:"deleteBranch"`
	Expires                                                            time.Time `json:"expires"`
	path, remote, remoteURL, remoteRef, trackingRef, targetLocalHead   string
	gitDir                                                             string
	gitDirInfo                                                         os.FileInfo
	upstreamRef, upstreamRemote, upstreamMerge                         string
	CanUpdateTarget                                                    bool   `json:"canUpdateTarget,omitempty"`
	UpdateTarget                                                       bool   `json:"updateTarget,omitempty"`
	CreateTarget                                                       bool   `json:"createTarget,omitempty"`
	RemoteAhead                                                        int    `json:"remoteAhead,omitempty"`
	LocalAhead                                                         int    `json:"localAhead,omitempty"`
	RemoteTarget                                                       string `json:"remoteTarget,omitempty"`
	RemoteNote                                                         string `json:"remoteNote,omitempty"`
	TargetRemote                                                       string `json:"targetRemote,omitempty"`
	PublishBase                                                        string `json:"publishBase,omitempty"`
	publishBaseHead, publishBaseRef, publishBaseRemote, publishBaseURL string
	preparedSeries                                                     string
	switchTarget                                                       switchBranchState
}

type CheckoutAction struct {
	Kind           string         `json:"kind,omitempty"`
	CurrentBranch  string         `json:"currentBranch,omitempty"`
	CurrentHead    string         `json:"currentHead,omitempty"`
	SiteID         string         `json:"siteID"`
	Status         string         `json:"status"`
	Message        string         `json:"message"`
	Plan           *CheckoutPlan  `json:"plan,omitempty"`
	Files          []string       `json:"files,omitempty"`
	RemoteRecovery string         `json:"remoteRecovery,omitempty"`
	Recovery       string         `json:"recovery,omitempty"`
	Branches       []BranchChoice `json:"branches,omitempty"`
}

func actionGit(ctx context.Context, path string, args ...string) ([]byte, error) {
	// Git still enforces index/worktree locks and refuses overwritten local work.
	// Hooks and recursive submodule changes are excluded from these narrow actions.
	prefix := []string{"-C", path, "-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-c", "submodule.recurse=false", "-c", "gc.auto=0", "-c", "maintenance.auto=false"}
	if isNetworkGit(args) {
		return networkGit(ctx, actionCommandSlots, 45*time.Second, path, append(prefix[2:], args...)...)
	}
	// Once a local mutation starts, let Git finish and clean up its locks.
	return runCommandWithSlots(context.WithoutCancel(ctx), actionCommandSlots, 0, "git", append(prefix, args...)...)
}

func cleanCheckout(ctx context.Context, path string) (GitStatus, gitMeta, error) {
	return inspectActionCheckout(ctx, path, false)
}

// A push publishes a pinned commit and never touches the index or worktree.
// Edits may continue, but conflicts and unfinished Git operations still block it.
func inspectActionCheckout(ctx context.Context, path string, allowEdits bool) (GitStatus, gitMeta, error) {
	s, m := inspectLocal(ctx, path)
	if m.ReadError != nil || m.OID == "" || m.Initial || m.Branch == "(detached)" {
		return s, m, errors.New("Could not read a checked-out branch. Refresh the Site and try again.")
	}
	if s.Conflicts != 0 {
		return s, m, errors.New("The checkout has conflicts. Resolve the rebase and push manually with Claude or Astra before continuing.")
	}
	if !allowEdits && s.Changes != 0 {
		return s, m, errors.New("Commit or stash local work first. No checkout changes were made.")
	}
	for _, name := range []string{"rebase-merge", "rebase-apply", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "sequencer", "index.lock", "BISECT_LOG"} {
		state := gitString(ctx, path, "rev-parse", "--path-format=absolute", "--git-path", name)
		if state == "" {
			return s, m, errors.New("Could not verify Git operation state.")
		}
		if _, err := os.Stat(state); !os.IsNotExist(err) {
			return s, m, errors.New("Another Git operation is in progress. Finish it before continuing.")
		}
	}
	if gitString(ctx, path, "config", "--bool", "core.sparseCheckout") == "true" {
		return s, m, errors.New("Handle sparse checkouts manually.")
	}
	for _, check := range []struct {
		args    []string
		reject  func(string) bool
		message string
	}{
		{[]string{"ls-files", "--stage", "-z"}, func(entry string) bool { return strings.HasPrefix(entry, "160000 ") }, "This checkout contains submodules. Update it manually."},
		{[]string{"ls-files", "-v", "-z"}, func(entry string) bool {
			return len(entry) > 0 && (entry[0] == 'S' || (entry[0] >= 'a' && entry[0] <= 'z'))
		}, "The index hides files with skip-worktree or assume-unchanged. Handle this checkout manually."},
	} {
		records := &gitRecordCheck{reject: check.reject}
		_, err := runCommandOutput(ctx, commandSlots, 30*time.Second, nil, records, "git", append([]string{"-C", path}, check.args...)...)
		if err != nil || len(records.pending) != 0 {
			return s, m, errors.New("Could not inspect tracked files.")
		}
		if records.rejected {
			return s, m, errors.New(check.message)
		}
	}
	return s, m, nil
}

func refHead(ctx context.Context, path, ref string) (string, error) {
	out, err := git(ctx, path, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if exitedWith(err, 1) {
		return "", nil
	}
	return strings.TrimSpace(string(out)), err
}

func branchInOtherWorktree(ctx context.Context, path, branch string) bool {
	out, err := git(ctx, path, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return true
	}
	root := gitString(ctx, path, "rev-parse", "--show-toplevel")
	root, _ = filepath.EvalSymlinks(root)
	current := ""
	for _, item := range strings.Split(string(out), "\x00") {
		if strings.HasPrefix(item, "worktree ") {
			current, _ = filepath.EvalSymlinks(strings.TrimPrefix(item, "worktree "))
		}
		if item == "branch refs/heads/"+branch && current != root {
			return true
		}
	}
	return false
}

func prepareCheckoutAction(ctx context.Context, path string, req ActionRequest, merged *MergedPullRequest) (*CheckoutPlan, error) {
	s, m, err := inspectActionCheckout(ctx, path, req.Kind == "push")
	if err != nil {
		return nil, err
	}
	if m.OID != req.Head || m.Branch != req.Branch {
		return nil, errors.New("The checkout changed. Review the current branch and try again.")
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	p := &CheckoutPlan{ID: hex.EncodeToString(b), Kind: req.Kind, Branch: m.Branch, Head: m.OID, path: path, Expires: time.Now().Add(5 * time.Minute)}
	p.branchGeneration = m.BranchGeneration
	p.upstreamRef, p.upstreamRemote, p.upstreamMerge = m.Ref, m.Remote, m.Merge
	p.gitDir = gitString(ctx, path, "rev-parse", "--absolute-git-dir")
	p.gitDirInfo, err = os.Stat(p.gitDir)
	if err != nil {
		return nil, errors.New("Could not verify the checkout identity.")
	}
	if req.Kind == "switch" {
		return prepareBranchSwitch(ctx, p, req)
	}
	switch req.Kind {
	case "update", "push", "publish", "rebase-push", "replace":
		if !strings.HasPrefix(m.Ref, "refs/remotes/") || !strings.HasPrefix(m.Merge, "refs/heads/") || m.Remote == "" || m.Remote == "." {
			return nil, errors.New("A remote upstream is required to update this branch.")
		}
		if isPushAction(req.Kind) && s.PushTarget == "" {
			return nil, errors.New("A matching origin upstream is required. Publish this branch manually.")
		}
		p.Target, p.remote, p.remoteRef, p.trackingRef = s.Tracking, m.Remote, m.Merge, m.Ref
	case "merged", "rebase":
		if m.DefaultBranch == "" || s.IsMainBranch {
			return nil, errors.New("Could not determine a different default branch.")
		}
		if (req.TargetBranch != "" && req.TargetBranch != m.DefaultBranch) || (req.TargetRemote != "" && req.TargetRemote != m.DefaultRemote) {
			return nil, errors.New("The default branch changed. Open the action again.")
		}
		p.Target, p.remote, p.remoteRef, p.trackingRef = m.DefaultBranch, m.DefaultRemote, "refs/heads/"+m.DefaultBranch, m.DefaultRef
		if _, err := git(ctx, path, "check-ref-format", "--branch", p.Target); err != nil {
			return nil, errors.New("The default branch name cannot be checked out safely.")
		}
		if req.Kind == "merged" {
			if merged == nil {
				return nil, errors.New("The merged PR is no longer available. Refresh the Site.")
			}
			p.DeleteBranch = req.DeleteBranch
			if !mergedPRMatchesBranch(merged, m) || merged.Base != p.Target {
				return nil, errors.New("This branch does not exactly match a PR merged into the default branch. No checkout changes were made.")
			}
			p.targetLocalHead, err = refHead(ctx, path, "refs/heads/"+p.Target)
			if err != nil {
				return nil, errors.New("Could not read the local default branch.")
			}
			if branchInOtherWorktree(ctx, path, p.Target) {
				return nil, errors.New("The default branch is checked out in another worktree. Update it there.")
			}
		}
	default:
		return nil, errors.New("Unknown suggested action.")
	}
	p.remoteURL = gitString(ctx, path, "remote", "get-url", p.remote)
	if p.remoteURL == "" {
		return nil, errors.New("Could not read the remote URL.")
	}
	if isPushAction(p.Kind) {
		if err := validatePushDestination(ctx, p); err != nil {
			return nil, err
		}
	}
	if _, err := fetchTrackingRef(ctx, path, p.remote, p.remoteRef, p.trackingRef, true); err != nil {
		return nil, errors.New("Could not fetch the remote. Check your connection and authentication, then try again.")
	}
	p.TargetHead = gitString(ctx, path, "rev-parse", "--verify", p.trackingRef+"^{commit}")
	if p.TargetHead == "" {
		return nil, errors.New("The remote branch is unavailable.")
	}
	old := p.Head
	if p.Kind == "merged" {
		old = p.targetLocalHead
	}
	if p.Kind == "push" {
		if err := preparePush(ctx, p); err != nil {
			return nil, err
		}
	}
	if p.Kind == "rebase-push" {
		if (req.TargetBranch != "" && req.TargetBranch != m.DefaultBranch) || (req.TargetRemote != "" && req.TargetRemote != m.DefaultRemote) {
			return nil, errors.New("The base branch changed." + manualPublish)
		}
		if err := preparePublishBase(ctx, p, m); err != nil {
			return nil, err
		}
		if err := checkRebaseAndPush(ctx, p, nil); err != nil {
			return nil, err
		}
	}
	if p.Kind == "publish" {
		if err := prepareRebasedPublish(ctx, p, m); err != nil {
			return nil, err
		}
	}
	if p.Kind != "rebase" && !isPushAction(p.Kind) && p.Kind != "replace" && old != "" {
		if _, err := git(ctx, path, "merge-base", "--is-ancestor", old, p.TargetHead); err != nil {
			return nil, errors.New("The local branch cannot be fast-forwarded. Update it manually; no checkout changes were made.")
		}
	}
	if p.Kind == "rebase" {
		if m.Ref != "" && m.Ref != p.trackingRef {
			if m.Remote == "." || !strings.HasPrefix(m.Ref, "refs/remotes/") {
				return nil, errors.New("Handle a branch with a local upstream manually.")
			}
			if _, err := fetchTrackingRef(ctx, path, m.Remote, m.Merge, m.Ref, true); err != nil {
				return nil, errors.New("Could not check the branch’s own upstream. Try again before rebasing.")
			}
			upstream := gitString(ctx, path, "rev-parse", "--verify", m.Ref+"^{commit}")
			if _, err := git(ctx, path, "merge-base", "--is-ancestor", upstream, m.OID); err != nil {
				return nil, errors.New("The branch has incoming or rewritten upstream commits." + manualPublish)
			}
		}
		check := rebasePreview(ctx, path)
		if check.Status != "clean" {
			return nil, &rebaseActionError{check}
		}
		if check.Head != p.Head || check.TargetHead != p.TargetHead {
			return nil, errors.New("The branches changed during the check. Review the action again.")
		}
	}
	if err := validateCheckoutPlan(ctx, p, false); err != nil {
		return nil, err
	}
	return p, nil
}

type rebaseActionError struct{ check RebaseCheck }

func (e *rebaseActionError) Error() string { return e.check.Message }

func validateCheckoutPlan(ctx context.Context, p *CheckoutPlan, checkRemote bool) error {
	if time.Now().After(p.Expires) {
		return errors.New("The confirmation expired. Review the action again.")
	}
	info, err := os.Stat(p.gitDir)
	if err != nil || p.gitDirInfo == nil || !os.SameFile(info, p.gitDirInfo) || gitString(ctx, p.path, "rev-parse", "--absolute-git-dir") != p.gitDir {
		return errors.New("The checkout folder was replaced. Review the action again.")
	}
	_, m, err := inspectActionCheckout(ctx, p.path, p.Kind == "push")
	if err != nil {
		return err
	}
	if m.Ref != p.upstreamRef || m.Remote != p.upstreamRemote || m.Merge != p.upstreamMerge {
		return errors.New("The branch upstream changed. Review the action again.")
	}
	if m.Branch != p.Branch || m.OID != p.Head || m.BranchGeneration != p.branchGeneration {
		return errors.New("The checkout changed since confirmation was prepared. Review the action again.")
	}
	if p.Kind == "switch" {
		return validateBranchSwitch(ctx, p, checkRemote)
	}
	if gitString(ctx, p.path, "remote", "get-url", p.remote) != p.remoteURL {
		return errors.New("The remote configuration changed. Review the action again.")
	}
	if (p.Kind == "update" || isPushAction(p.Kind) || p.Kind == "replace") && (m.Ref != p.trackingRef || m.Merge != p.remoteRef || m.Remote != p.remote) {
		return errors.New("The upstream changed. Review the action again.")
	}
	if p.Kind != "update" && !isPushAction(p.Kind) && p.Kind != "replace" && (m.DefaultBranch != p.Target || m.DefaultRef != p.trackingRef) {
		return errors.New("The default branch changed. Review the action again.")
	}
	if p.Kind == "replace" {
		if err := validateReplacement(ctx, p, m); err != nil {
			return err
		}
	}
	if p.Kind == "merged" {
		head, err := refHead(ctx, p.path, "refs/heads/"+p.Target)
		if err != nil || head != p.targetLocalHead || branchInOtherWorktree(ctx, p.path, p.Target) {
			return errors.New("The local default branch changed or is in use elsewhere. Review the action again.")
		}
	}
	if isPushAction(p.Kind) {
		if err := validatePushDestination(ctx, p); err != nil {
			return err
		}
	}
	if isPublishAction(p.Kind) {
		if err := validatePublishBase(ctx, p, m, checkRemote); err != nil {
			return err
		}
	}
	if checkRemote && p.Kind == "rebase" && m.Ref != "" && m.Ref != p.trackingRef {
		if m.Remote == "." || !strings.HasPrefix(m.Ref, "refs/remotes/") {
			return errors.New("The upstream changed. Review the action again.")
		}
		if _, err := fetchTrackingRef(ctx, p.path, m.Remote, m.Merge, m.Ref, true); err != nil {
			return errors.New("Could not verify the branch’s own upstream. Try again.")
		}
		upstream := gitString(ctx, p.path, "rev-parse", "--verify", m.Ref+"^{commit}")
		if _, err := git(ctx, p.path, "merge-base", "--is-ancestor", upstream, p.Head); err != nil {
			return errors.New("The branch’s own remote has new or rewritten commits." + manualPublish)
		}
	}
	if checkRemote {
		out, err := actionGit(ctx, p.path, "ls-remote", "--exit-code", "--heads", "--", p.remote, p.remoteRef)
		fields := strings.Fields(string(out))
		if err != nil || len(fields) != 2 || fields[0] != p.TargetHead || fields[1] != p.remoteRef {
			if isPublishAction(p.Kind) || p.Kind == "rebase" {
				return errors.New("The remote changed or could not be verified. The action was stopped." + manualPublish)
			}
			return errors.New("The remote changed or could not be verified. Review the action again.")
		}
	}
	return nil
}

func applyCheckoutAction(ctx context.Context, p *CheckoutPlan) (result CheckoutAction) {
	result = CheckoutAction{Status: "failed", Kind: p.Kind}
	expectedResultHead := p.TargetHead
	if isPushAction(p.Kind) {
		expectedResultHead = p.Head
	}
	defer func() {
		result.Kind = p.Kind
		verifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s, meta := inspectLocal(verifyCtx, p.path)
		if isPushAction(p.Kind) && p.Kind != "rebase-push" {
			// applyPush verifies the remote SHA; concurrent local edits cannot undo it.
			if meta.ReadError == nil {
				result.CurrentBranch, result.CurrentHead = s.Branch, meta.OID
			}
			return
		}
		if meta.ReadError != nil || meta.OID == "" {
			result.Status = "failed"
			result.Message += " Could not verify the resulting checkout; inspect it manually."
			return
		}
		result.CurrentBranch, result.CurrentHead = s.Branch, meta.OID
		branch := p.Branch
		if p.Kind == "merged" || p.Kind == "switch" {
			branch = p.Target
		}
		if result.Status == "complete" && (meta.Branch != branch || meta.OID != expectedResultHead) {
			result.Status, result.Message = "failed", "The resulting checkout differs from the expected result. Inspect it manually; no rollback was attempted."
		}
		if result.Status == "complete" && (s.Changes != 0 || s.Conflicts != 0) {
			result.Message += " Local edits appeared during the action; review them in Changes."
		}
		if result.Status == "complete" {
			finishRecovery(verifyCtx, p, &result)
		}
	}()
	fail := func(err error) CheckoutAction { result.Message = err.Error(); return result }
	common := gitString(ctx, p.path, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if common == "" {
		return fail(errors.New("Could not locate the repository."))
	}
	lock, err := os.OpenFile(filepath.Join(common, "devjay-action.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fail(err)
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fail(errors.New("Another DevJay action is running for this repository."))
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	if err := validateCheckoutPlan(ctx, p, true); err != nil {
		return fail(err)
	}
	if p.Kind == "publish" {
		if err := checkRebasedPublish(ctx, p); err != nil {
			var conflict *rebaseActionError
			if errors.As(err, &conflict) {
				result.Files = conflict.check.Files
			}
			return fail(err)
		}
		if err := validateCheckoutPlan(ctx, p, true); err != nil {
			return fail(err)
		}
	}
	if p.Kind == "rebase-push" {
		return applyRebaseAndPush(ctx, p, &expectedResultHead)
	}
	if isPushAction(p.Kind) {
		return applyPush(ctx, p)
	}
	backup := "refs/devjay/backups/" + p.ID
	backupOriginal := func() error {
		if _, err := actionGit(ctx, p.path, "update-ref", "--create-reflog", "-m", "DevJay: before "+p.Kind, backup, p.Head, ""); err != nil {
			return errors.New("Could not save a recovery reference. No action was applied.")
		}
		result.Recovery = backup
		return nil
	}
	if p.Kind == "rebase" {
		check := runRebaseTrial(ctx, p.path, true, func(tmp string, check RebaseCheck) error {
			expectedResultHead = check.NewHead
			if check.Head != p.Head || check.TargetHead != p.TargetHead {
				return errors.New("The branches changed. Review the rebase again.")
			}
			if err := validateCheckoutPlan(ctx, p, true); err != nil {
				return err
			}
			if err := backupOriginal(); err != nil {
				return err
			}
			incoming := "refs/devjay/prepared/" + p.ID
			defer actionGit(context.Background(), p.path, "update-ref", "-d", incoming)
			if _, err := actionGit(ctx, p.path, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--no-auto-maintenance", "--refmap=", "--", tmp, check.NewHead+":"+incoming); err != nil {
				return errors.New("Could not import the checked rebase. Your branch is unchanged.")
			}
			if err := validateCheckoutPlan(ctx, p, false); err != nil {
				return err
			}
			ctx = context.WithoutCancel(ctx)
			// Switch without force so ignored files and newly edited files are protected.
			// Only then move the named branch with a compare-and-swap, never reset HEAD.
			if _, err := actionGit(ctx, p.path, "switch", "--detach", "--no-overwrite-ignore", "--no-recurse-submodules", check.NewHead); err != nil {
				return errors.New("Could not load the rebased files safely. Your branch is unchanged; inspect the checkout before retrying.")
			}
			if branchInOtherWorktree(ctx, p.path, p.Branch) {
				return errors.New("The original branch is now in use elsewhere. The checkout is detached; the original branch and recovery reference are preserved.")
			}
			if _, err := actionGit(ctx, p.path, "update-ref", "-m", "DevJay: apply checked rebase", "refs/heads/"+p.Branch, check.NewHead, p.Head); err != nil {
				return errors.New("The branch changed during the operation. The checkout is detached; your original commits remain in the recovery reference. Finish manually.")
			}
			if _, err := actionGit(ctx, p.path, "switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules", "--", p.Branch); err != nil {
				return errors.New("The branch was rebased, but could not be reattached. The checkout is detached; switch to the branch manually.")
			}
			return nil
		})
		if check.Status != "applied" {
			result.Files = check.Files
			return fail(errors.New(check.Message))
		}
		result.Status, result.Message = "complete", "Rebased "+p.Branch+" onto "+p.Target+". Nothing was pushed."
		return result
	}
	if err := validateCheckoutPlan(ctx, p, false); err != nil {
		return fail(err)
	}
	if err := backupOriginal(); err != nil {
		return fail(err)
	}
	ctx = context.WithoutCancel(ctx)
	if p.Kind == "switch" {
		if err := applyBranchSwitch(ctx, p); err != nil {
			return fail(err)
		}
		result.Status, result.Message = "complete", "Switched to "+p.Target+". Nothing was pushed."
		if p.UpdateTarget {
			result.Message = "Switched to " + p.Target + " and fast-forwarded to the confirmed remote version. Nothing was pushed."
		} else if p.CreateTarget {
			result.Message = "Created and switched to " + p.Target + ", tracking " + p.RemoteTarget + ". Nothing was pushed."
		}
		return result
	}
	if p.Kind == "replace" {
		if err := replaceCheckout(ctx, p); err != nil {
			return fail(err)
		}
		result.Status, result.Message = "complete", "Replaced "+p.Branch+" with the confirmed "+p.Target+" history. Original commits remain in the recovery reference. Nothing was pushed."
		return result
	}
	if p.Kind == "merged" {
		args := []string{"switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules"}
		if p.targetLocalHead == "" {
			args = append(args, "--create", p.Target, "--no-track", p.TargetHead)
		} else {
			args = append(args, "--", p.Target)
		}
		if _, err := actionGit(ctx, p.path, args...); err != nil {
			return fail(errors.New("Could not switch safely to the default branch. Local work was not discarded; inspect the checkout before retrying."))
		}
		if p.targetLocalHead == "" {
			if _, err := actionGit(ctx, p.path, "branch", "--set-upstream-to="+strings.TrimPrefix(p.trackingRef, "refs/remotes/"), "--", p.Target); err != nil {
				return fail(errors.New("The default branch was created and checked out, but tracking could not be configured. Set its upstream manually. The old branch was kept."))
			}
		}
	}
	expectedBranch, expectedHead := p.Branch, p.Head
	if p.Kind == "merged" {
		expectedBranch, expectedHead = p.Target, p.targetLocalHead
		if expectedHead == "" {
			expectedHead = p.TargetHead
		}
	}
	_, current, err := cleanCheckout(ctx, p.path)
	if err != nil || current.Branch != expectedBranch || current.OID != expectedHead {
		return fail(errors.New("The checkout changed during the operation. Stopped before updating or deleting any branch."))
	}
	if _, err := actionGit(ctx, p.path, "-c", "branch."+expectedBranch+".mergeOptions=", "merge", "--ff-only", "--no-squash", "--no-autostash", "--no-overwrite-ignore", "--no-edit", p.TargetHead); err != nil {
		return fail(fmt.Errorf("Could not fast-forward %s. Local work was not discarded and the old branch was kept. Inspect the checkout before retrying.", expectedBranch))
	}
	if p.DeleteBranch {
		state, current, err := cleanCheckout(ctx, p.path)
		if err != nil && current.ReadError == nil && current.Branch == p.Target && current.OID == p.TargetHead && state.Changes > 0 {
			result.Status, result.Message = "complete", "Updated "+p.Target+" from the remote. Kept local branch "+p.Branch+" because edits appeared before deletion. Nothing was pushed."
			return result
		}
		if err != nil || current.Branch != p.Target || current.OID != p.TargetHead || branchInOtherWorktree(ctx, p.path, p.Branch) {
			return fail(errors.New("The default branch was updated, but the old branch could not safely be deleted. It was kept."))
		}
		if _, err := actionGit(ctx, p.path, "update-ref", "-d", "refs/heads/"+p.Branch, p.Head); err != nil {
			return fail(errors.New("The default branch was updated, but the old branch changed and was kept."))
		}
		// Removing the ref is compare-and-swap guarded. Leave branch config alone:
		// another Git client may already be recreating a branch with the same name.
	}
	result.Status, result.Message = "complete", "Updated "+expectedBranch+" from the remote. Nothing was pushed."
	if p.DeleteBranch {
		result.Message += " Deleted local branch " + p.Branch + "; its commits remain in the recovery reference."
	}
	return result
}
