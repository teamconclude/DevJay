package main

import (
	"context"
	"errors"
	"strings"
)

// Replacing a branch is deliberately narrower than recognizing equivalent
// patches: the exact local tip must have been observed on this same upstream.
// This also handles rewritten merge commits and repeated remote rewrites.
func replacementUpstream(m gitMeta) bool {
	return m.Remote != "" && m.Remote != "." && m.Ref == "refs/remotes/"+m.Remote+"/"+m.Branch && m.Merge == "refs/heads/"+m.Branch
}

func verifyReplacementHistory(ctx context.Context, path, head, target, tracking string) error {
	if head == "" || target == "" || head == target || !strings.HasPrefix(tracking, "refs/remotes/") {
		return errors.New("There is no rewritten upstream to replace this branch from.")
	}
	current, err := refHead(ctx, path, tracking)
	if err != nil || current != target {
		return errors.New("The upstream changed. Review the replacement again.")
	}
	_, err = git(ctx, path, "merge-base", "--is-ancestor", head, target)
	if !exitedWith(err, 1) {
		return errors.New("Could not verify a remote rewrite. Use a normal update or handle this branch manually.")
	}
	// Limit the evidence search and fail closed when history has expired. Dates,
	// subjects, and patch similarity cannot establish that work was published.
	out, err := git(ctx, path, "reflog", "show", "--format=%H", "--max-count=256", tracking, "--")
	if err == nil {
		for _, observed := range strings.Fields(string(out)) {
			if observed == head {
				return nil
			}
		}
	}
	return errors.New("The local tip is not a recorded former upstream tip. It may contain unpublished commits. Handle this branch manually; no local work was discarded.")
}

func canReplaceFromRemote(ctx context.Context, s GitStatus, m gitMeta) bool {
	if m.ReadError != nil || !m.Comparable || m.Initial || s.Ahead <= 0 || s.Changes != 0 || s.Conflicts != 0 || !replacementUpstream(m) {
		return false
	}
	target := gitString(ctx, m.Path, "rev-parse", "--verify", m.Ref+"^{commit}")
	return verifyReplacementHistory(ctx, m.Path, m.OID, target, m.Ref) == nil
}

func validateReplacement(ctx context.Context, p *CheckoutPlan, m gitMeta) error {
	if !replacementUpstream(m) {
		return errors.New("Replacement requires the same-named branch on its configured remote.")
	}
	if branchInOtherWorktree(ctx, p.path, p.Branch) {
		return errors.New("This branch is checked out in another worktree. Handle it manually.")
	}
	return verifyReplacementHistory(ctx, p.path, p.Head, p.TargetHead, p.trackingRef)
}

// Keep branch identity and tracking instead of deleting and recreating its
// configuration. No forced checkout/reset: Git protects new edits and ignored
// collisions, and the ref move refuses any concurrent change to the old tip.
func replaceCheckout(ctx context.Context, p *CheckoutPlan) error {
	if _, err := actionGit(ctx, p.path, "switch", "--detach", "--no-overwrite-ignore", "--no-recurse-submodules", p.TargetHead); err != nil {
		return errors.New("Could not load the remote files safely. Local work was not discarded; inspect the checkout before retrying.")
	}
	if branchInOtherWorktree(ctx, p.path, p.Branch) {
		return errors.New("The branch is now in use elsewhere. The checkout is detached; the old branch and recovery reference are preserved.")
	}
	if _, err := actionGit(ctx, p.path, "update-ref", "-m", "DevJay: replace from checked remote", "refs/heads/"+p.Branch, p.TargetHead, p.Head); err != nil {
		return errors.New("The branch changed during replacement. The checkout is detached; original commits remain in the recovery reference. Finish manually.")
	}
	if _, err := actionGit(ctx, p.path, "switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules", "--", p.Branch); err != nil {
		return errors.New("The branch was replaced, but could not be reattached. The checkout is detached; switch to the branch manually. Original commits remain in the recovery reference.")
	}
	return nil
}
