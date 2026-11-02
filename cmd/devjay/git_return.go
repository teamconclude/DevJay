package main

import (
	"context"
	"errors"
	"fmt"
)

// A confirmed return can replace a diverged default branch. Its old tip may
// contain unique work, so keep it outside the rolling successful-action backups.
// No patch-equivalence claim or destructive checkout is needed for preservation.
func replaceDefaultForReturn(ctx context.Context, p *CheckoutPlan, result *CheckoutAction) error {
	if p.Kind != "merged" || !p.ReplaceDefault || p.targetLocalHead == "" {
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
		return fmt.Errorf("Local %s changed during the return. The checkout is detached; the merged branch and recovery references were kept. Finish manually.", p.Target)
	}
	if _, err := actionGit(ctx, p.path, "switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules", "--", p.Target); err != nil {
		return fmt.Errorf("Local %s was updated, but could not be checked out. Its previous tip is preserved in the default-branch recovery reference. The merged branch was kept; finish manually.", p.Target)
	}
	return nil
}
