package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const approvedPRFields = "number,state,isDraft,reviewDecision,headRefName,baseRefName,headRepository,headRepositoryOwner,headRefOid"

// The confirmation displays the number. The backend pins the repository, refs,
// and commit so it can recheck the same PR before deleting a local branch.
type approvedBranchPR struct {
	Number                   int `json:"number"`
	repo, branch, base, head string
}

func (pr pullRequest) approvedReturn(repo string) *approvedBranchPR {
	if pr.State != "OPEN" || pr.IsDraft || pr.ReviewDecision != "APPROVED" || pr.Number <= 0 ||
		pr.HeadRefName == "" || pr.BaseRefName == "" || pr.HeadRefName == pr.BaseRefName || pr.HeadRefOid == "" ||
		repo == "" || !strings.EqualFold(pr.HeadRepository.NameWithOwner, repo) {
		return nil
	}
	return &approvedBranchPR{Number: pr.Number, repo: repo, branch: pr.HeadRefName, base: pr.BaseRefName, head: pr.HeadRefOid}
}

func (p *CheckoutPlan) returnsToDefault() bool {
	return p.Kind == "merged" || p.Kind == "approved"
}

func (pr *approvedBranchPR) matchesCheckout(s GitStatus, m gitMeta) bool {
	return pr != nil && pr.head == m.OID && pr.base == m.DefaultBranch && strings.EqualFold(pr.repo, m.GitHubRepo) &&
		pr.branch == trackedPullRequestBranch(m.Remote, m.Merge) && strings.HasPrefix(m.Ref, "refs/remotes/") &&
		m.Comparable && s.Ahead == 0 && s.Behind == 0 && !s.IsMainBranch
}

// Approval can be withdrawn while the dialog is open, independently of Git.
// Verify both the pinned PR and the published feature tip before proceeding.
func (pr *approvedBranchPR) verifyPublished(ctx context.Context, p *CheckoutPlan) error {
	out, err := runCommand(ctx, 15*time.Second, "gh", "pr", "view", strconv.Itoa(pr.Number), "--repo", "github.com/"+pr.repo, "--json", approvedPRFields)
	var current pullRequest
	if err != nil || json.Unmarshal(out, &current) != nil {
		return fmt.Errorf("Could not verify PR #%d on GitHub. Refresh the Site and try again.", pr.Number)
	}
	if latest := current.approvedReturn(pr.repo); latest == nil || *latest != *pr {
		return fmt.Errorf("PR #%d changed. Review its approval, branches, and latest commit before returning.", pr.Number)
	}
	if !remoteBranchAt(ctx, p.path, p.upstreamRemote, p.upstreamMerge, p.Head) {
		return fmt.Errorf("Could not verify that the remote branch for PR #%d still matches the local commit.", pr.Number)
	}
	return nil
}

// A confirmed return can replace a diverged default branch. Its old tip may
// contain unique work, so keep it outside the rolling successful-action backups.
// No patch-equivalence claim or destructive checkout is needed for preservation.
func replaceDefaultForReturn(ctx context.Context, p *CheckoutPlan, result *CheckoutAction) error {
	if !p.returnsToDefault() || !p.ReplaceDefault || p.targetLocalHead == "" {
		return errors.New("The default branch replacement was not prepared. Review the return again.")
	}
	backup := "refs/devjay/default-backups/" + p.ID
	if _, err := actionGit(ctx, p.path, "update-ref", "--no-deref", "--create-reflog", "-m", "DevJay: preserve "+p.Target+" before return", backup, p.targetLocalHead, ""); err != nil {
		return fmt.Errorf("Could not preserve local %s. Both branches were kept; no checkout changes were made.", p.Target)
	}
	result.DefaultRecovery = backup
	// Recheck both branches and the remote after saving recovery, before loading
	// files. Git itself protects edits and ignored-file collisions during switch.
	if err := validateCheckoutPlan(ctx, p, true); err != nil {
		return err
	}
	if _, err := actionGit(ctx, p.path, "switch", "--detach", "--no-overwrite-ignore", "--no-recurse-submodules", p.TargetHead); err != nil {
		return errors.New("Could not load the default branch's remote files safely. Local work and both branches were kept; inspect the checkout before retrying.")
	}
	state, current := inspectLocal(ctx, p.path)
	if current.ReadError != nil || current.Branch != "(detached)" || current.OID != p.TargetHead || state.Changes != 0 || state.Conflicts != 0 {
		return errors.New("The checkout changed while loading the remote files. Stopped before moving or deleting either branch. Inspect the checkout manually.")
	}
	target, err := readSwitchBranch(ctx, p.path, p.Target)
	if err != nil || target != p.switchTarget || branchInOtherWorktree(ctx, p.path, p.Target) {
		return fmt.Errorf("Local %s changed or is now in use elsewhere. The checkout is detached; both branches and recovery references were kept.", p.Target)
	}
	if head, err := refHead(ctx, p.path, backup); err != nil || head != p.targetLocalHead {
		return errors.New("The default-branch recovery reference changed. The checkout is detached; both branches were kept. Finish manually.")
	}
	if _, err := actionGit(ctx, p.path, "update-ref", "--no-deref", "-m", "DevJay: return to confirmed default", "refs/heads/"+p.Target, p.TargetHead, p.targetLocalHead); err != nil {
		return fmt.Errorf("Local %s changed during the return. The checkout is detached; the feature branch and recovery references were kept. Finish manually.", p.Target)
	}
	if _, err := actionGit(ctx, p.path, "switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules", "--", p.Target); err != nil {
		return fmt.Errorf("Local %s was updated, but could not be checked out. Its previous tip is preserved in the default-branch recovery reference. The feature branch was kept; finish manually.", p.Target)
	}
	return nil
}
