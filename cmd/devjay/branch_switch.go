package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type BranchChoice struct {
	PullRequests       []PullRequestLink `json:"pullRequests,omitempty"`
	PRStatus           string            `json:"prStatus,omitempty"`
	prRemote, prBranch string
	Name               string `json:"name"`
	Remote             string `json:"remote,omitempty"`
	Local              bool   `json:"local"`
	Current            bool   `json:"current"`
}

type BranchCatalog struct {
	Branch   string         `json:"branch"`
	Head     string         `json:"head"`
	Branches []BranchChoice `json:"branches"`
	Message  string         `json:"message"`
}

type switchBranchState struct {
	head, generation, upstream, remote, merge string
	remoteConfig, mergeConfig                 string
}

func validSwitchBranch(ctx context.Context, path, name string) bool {
	if name == "" || name == "HEAD" || name == "@" || strings.HasPrefix(name, "-") {
		return false
	}
	// Validate the literal full ref before --branch, which otherwise expands
	// shorthand such as @{-1}. User input is never a revision expression.
	if _, err := git(ctx, path, "check-ref-format", "refs/heads/"+name); err != nil {
		return false
	}
	_, err := git(ctx, path, "check-ref-format", "--branch", name)
	return err == nil
}

func readSwitchBranch(ctx context.Context, path, name string) (switchBranchState, error) {
	var result switchBranchState
	for _, setting := range []struct {
		name  string
		value *string
	}{{"remote", &result.remoteConfig}, {"merge", &result.mergeConfig}} {
		out, err := git(ctx, path, "config", "--get-all", "branch."+name+"."+setting.name)
		if err != nil && !exitedWith(err, 1) {
			return result, errors.New("Could not read the destination branch configuration.")
		}
		*setting.value = string(out)
	}
	ref := "refs/heads/" + name
	out, err := git(ctx, path, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(symref)%00%(upstream)%00%(upstream:remotename)%00%(upstream:remoteref)", "--", ref)
	if err != nil {
		return result, errors.New("Could not inspect the destination branch.")
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		parts := strings.Split(line, "\x00")
		if len(parts) != 6 || parts[0] != ref {
			continue
		}
		if parts[2] != "" {
			return result, errors.New("Symbolic destination branches must be handled manually.")
		}
		result.head, result.upstream, result.remote, result.merge = parts[1], parts[3], parts[4], parts[5]
		result.generation, _ = branchCreation(ctx, path, name)
		return result, nil
	}
	return result, nil
}

func safeSwitchTree(ctx context.Context, path, head string) error {
	records := &gitRecordCheck{reject: func(record string) bool { return strings.HasPrefix(record, "160000 ") }}
	_, err := runCommandOutput(ctx, commandSlots, 30*time.Second, nil, records, "git", "-C", path, "ls-tree", "-r", "-z", head, "--")
	if err != nil || len(records.pending) != 0 {
		return errors.New("Could not inspect the destination files.")
	}
	if records.rejected {
		return errors.New("The destination contains submodules. Switch to it manually.")
	}
	return nil
}

// A configured remote-tracking ref can be symbolic, even to a local branch.
// Never fetch into it during preparation. The random, app-owned destination is
// removed with an expected OID and without dereferencing symbolic replacements.
func fetchSwitchTarget(ctx context.Context, p *CheckoutPlan) (string, error) {
	temporary := "refs/devjay/prepared/switch-" + p.ID
	if head, err := refHead(ctx, p.path, temporary); err != nil || head != "" {
		return "", errors.New("The private fetch reference is already in use.")
	}
	if _, err := actionGit(ctx, p.path, "fetch", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", "--no-auto-maintenance", "--refmap=", "--", p.remote, "+"+p.remoteRef+":"+temporary); err != nil {
		return "", err
	}
	head, err := refHead(ctx, p.path, temporary)
	if head != "" {
		_, _ = actionGit(context.Background(), p.path, "update-ref", "--no-deref", "-d", temporary, head)
	}
	if err != nil || head == "" {
		return "", errors.New("The fetched branch could not be read.")
	}
	return head, nil
}

func prepareBranchSwitch(ctx context.Context, p *CheckoutPlan, req ActionRequest) (*CheckoutPlan, error) {
	if !validSwitchBranch(ctx, p.path, req.TargetBranch) {
		return nil, errors.New("Choose a valid branch name from the branch list.")
	}
	if req.TargetBranch == p.Branch {
		return nil, errors.New("This branch is already checked out.")
	}
	p.Target, p.TargetRemote, p.UpdateTarget = req.TargetBranch, req.TargetRemote, req.UpdateTarget
	target, err := readSwitchBranch(ctx, p.path, p.Target)
	if err != nil {
		return nil, err
	}
	p.switchTarget, p.targetLocalHead = target, target.head
	if branchInOtherWorktree(ctx, p.path, p.Target) {
		return nil, errors.New("The destination branch is checked out in another worktree. Switch to that Site instead.")
	}
	if req.TargetRemote != "" {
		if target.head != "" {
			return nil, errors.New("A local branch with this name already exists. Choose its local entry instead.")
		}
		if req.UpdateTarget {
			return nil, errors.New("Creating a tracking branch does not require updating an existing local branch.")
		}
		tracking, err := switchTrackingRef(ctx, p.path, req.TargetRemote, p.Target)
		if err != nil {
			return nil, err
		}
		p.CreateTarget = true
		p.remote, p.remoteRef, p.trackingRef = req.TargetRemote, "refs/heads/"+p.Target, tracking
	} else {
		if target.head == "" {
			return nil, errors.New("The local branch disappeared. Refresh the branch list.")
		}
		p.TargetHead = target.head
		if target.remote != "" && target.remote != "." && strings.HasPrefix(target.upstream, "refs/remotes/") && strings.HasPrefix(target.merge, "refs/heads/") {
			p.remote, p.remoteRef, p.trackingRef = target.remote, target.merge, target.upstream
		} else {
			p.RemoteNote = "No remote upstream is configured. The existing local branch will be used."
		}
	}
	if p.remote != "" {
		p.remoteURL = gitString(ctx, p.path, "remote", "get-url", p.remote)
		p.RemoteTarget = p.remote + "/" + strings.TrimPrefix(p.remoteRef, "refs/heads/")
		if p.remoteURL == "" {
			p.RemoteNote = "The remote configuration is unavailable. The existing local branch will be used."
		} else {
			remoteHead, err := fetchSwitchTarget(ctx, p)
			if err != nil {
				p.RemoteNote = "The remote could not be checked. The existing local branch will be used."
			} else if p.CreateTarget {
				p.TargetHead = remoteHead
			} else {
				out, err := git(ctx, p.path, "rev-list", "--left-right", "--count", target.head+"..."+remoteHead, "--")
				if err != nil {
					return nil, errors.New("Could not compare the destination with its remote.")
				}
				if n, err := fmt.Sscanf(string(out), "%d %d", &p.LocalAhead, &p.RemoteAhead); err != nil || n != 2 || p.LocalAhead < 0 || p.RemoteAhead < 0 {
					return nil, errors.New("Could not read destination commit counts.")
				}
				if p.LocalAhead == 0 && p.RemoteAhead > 0 {
					_, ancestryErr := git(ctx, p.path, "merge-base", "--is-ancestor", target.head, remoteHead)
					p.CanUpdateTarget = ancestryErr == nil
					if p.CanUpdateTarget && safeSwitchTree(ctx, p.path, remoteHead) != nil {
						p.CanUpdateTarget = false
						p.RemoteNote = "The newer remote files cannot be checked out safely by DevJay. The local branch will be used."
					}
				}
				if p.UpdateTarget && p.CanUpdateTarget {
					p.TargetHead = remoteHead
				}
			}
		}
	}
	if p.CreateTarget && p.TargetHead == "" {
		return nil, errors.New("The remote branch could not be checked. Check your connection and refresh the branch list.")
	}
	if p.UpdateTarget && !p.CanUpdateTarget {
		return nil, errors.New("This branch cannot safely use a newer remote version. Switch to the existing local branch instead.")
	}
	if err := safeSwitchTree(ctx, p.path, p.TargetHead); err != nil {
		return nil, err
	}
	if err := validateCheckoutPlan(ctx, p, false); err != nil {
		return nil, err
	}
	return p, nil
}

func validateBranchSwitch(ctx context.Context, p *CheckoutPlan, checkRemote bool) error {
	if !validSwitchBranch(ctx, p.path, p.Target) {
		return errors.New("The destination branch name is invalid.")
	}
	target, err := readSwitchBranch(ctx, p.path, p.Target)
	if err != nil {
		return err
	}
	if target != p.switchTarget || branchInOtherWorktree(ctx, p.path, p.Target) {
		return errors.New("The destination branch changed or is in use in another worktree. Review the switch again.")
	}
	if p.remote != "" && gitString(ctx, p.path, "remote", "get-url", p.remote) != p.remoteURL {
		return errors.New("The destination remote configuration changed. Review the switch again.")
	}
	if p.CreateTarget {
		tracking, err := switchTrackingRef(ctx, p.path, p.TargetRemote, p.Target)
		if err != nil || tracking != p.trackingRef {
			return errors.New("The remote tracking configuration changed. Review the switch again.")
		}
	}
	if checkRemote && (p.CreateTarget || p.UpdateTarget) {
		out, err := actionGit(ctx, p.path, "ls-remote", "--exit-code", "--heads", "--", p.remote, p.remoteRef)
		fields := strings.Fields(string(out))
		if err != nil || len(fields) != 2 || fields[0] != p.TargetHead || fields[1] != p.remoteRef {
			return errors.New("The destination remote changed or could not be verified. Review the switch again.")
		}
		if p.UpdateTarget {
			if _, err := git(ctx, p.path, "merge-base", "--is-ancestor", p.targetLocalHead, p.TargetHead); err != nil {
				return errors.New("The destination no longer permits a safe fast-forward.")
			}
		}
	}
	return nil
}

func applyBranchSwitch(ctx context.Context, p *CheckoutPlan) error {
	base := []string{"switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules"}
	if p.UpdateTarget {
		if _, err := actionGit(ctx, p.path, append(base, "--detach", p.TargetHead)...); err != nil {
			return errors.New("Could not load the destination files safely. Local work and both branches were kept; inspect the checkout before retrying.")
		}
		target, err := readSwitchBranch(ctx, p.path, p.Target)
		if err != nil || target != p.switchTarget || branchInOtherWorktree(ctx, p.path, p.Target) {
			return errors.New("The destination changed or is now in use elsewhere. The checkout is detached; both branches and the recovery reference were kept.")
		}
		if _, err := actionGit(ctx, p.path, "update-ref", "--no-deref", "-m", "DevJay: fast-forward destination before switch", "refs/heads/"+p.Target, p.TargetHead, p.targetLocalHead); err != nil {
			return errors.New("The destination changed during the switch. The checkout is detached; no local branch was overwritten. Finish manually.")
		}
	}
	args := append([]string(nil), base...)
	if p.CreateTarget {
		args = append(args, "--create", p.Target, "--no-track", p.TargetHead)
	} else {
		args = append(args, "--", p.Target)
	}
	if _, err := actionGit(ctx, p.path, args...); err != nil {
		return errors.New("Could not switch branches safely. Local work was not discarded; inspect the checkout before retrying.")
	}
	if p.CreateTarget {
		if _, err := actionGit(ctx, p.path, "branch", "--set-upstream-to="+p.trackingRef, "--", p.Target); err != nil {
			return errors.New("The branch was created and checked out, but tracking could not be configured. Set its upstream manually; no rollback was attempted.")
		}
		state, err := readSwitchBranch(ctx, p.path, p.Target)
		if err != nil || state.head != p.TargetHead || state.remote != p.remote || state.merge != p.remoteRef || state.upstream != p.trackingRef {
			return errors.New("The branch was created, but its tracking changed during the switch. Inspect its upstream manually; no rollback was attempted.")
		}
	}
	return nil
}
