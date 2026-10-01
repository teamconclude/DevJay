package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

// Both the local rebase and publication are proven in disposable checkouts.
// The callback runs only after the remote series matches the rebased prefix.
func checkRebaseAndPush(ctx context.Context, p *CheckoutPlan, apply func(string, RebaseCheck) error) error {
	if branchInOtherWorktree(ctx, p.path, p.Branch) {
		return errors.New("The branch is in use in another worktree." + manualPublish)
	}
	if _, err := git(ctx, p.path, "merge-base", "--is-ancestor", p.TargetHead, p.Head); err != nil {
		return errors.New("The branch has incoming or rewritten remote commits." + manualPublish)
	}
	bases, err := git(ctx, p.path, "merge-base", "--all", p.Head, p.publishBaseHead)
	if err != nil || len(strings.Fields(string(bases))) != 1 {
		return errors.New("The branch history could not be verified." + manualPublish)
	}
	if _, err := publishSeries(ctx, p.path, strings.TrimSpace(string(bases)), p.Head); err != nil {
		return err
	}
	var historyErr error
	check := runRebaseTrial(ctx, p.path, true, func(tmp string, check RebaseCheck) error {
		if check.Head != p.Head || check.TargetHead != p.publishBaseHead {
			return errors.New("The branches changed during the check." + manualPublish)
		}
		// A remote tip already contained in the base is preserved by ancestry.
		// Otherwise reproduce every remote commit and compare the rebased prefix.
		if _, err := git(ctx, p.path, "merge-base", "--is-ancestor", p.TargetHead, p.publishBaseHead); err != nil {
			historyErr = checkRebasedHistory(ctx, p, tmp, check.NewHead)
			if historyErr != nil {
				return historyErr
			}
		}
		series, err := publishSeries(ctx, tmp, p.publishBaseHead, check.NewHead)
		if err != nil {
			return err
		}
		if p.preparedSeries != "" && series != p.preparedSeries {
			return errors.New("The rebase result changed since confirmation." + manualPublish)
		}
		if apply != nil {
			return apply(tmp, check)
		}
		p.preparedSeries = series
		p.Commits, err = strconv.Atoi(gitString(ctx, tmp, "rev-list", "--count", p.publishBaseHead+".."+check.NewHead, "--"))
		return err
	})
	if historyErr != nil {
		return historyErr
	}
	if check.Status != "applied" {
		return &rebaseActionError{check}
	}
	return nil
}

func applyRebaseAndPush(ctx context.Context, p *CheckoutPlan, expectedHead *string) CheckoutAction {
	result := CheckoutAction{Status: "failed", Kind: p.Kind}
	rebased := false
	err := checkRebaseAndPush(ctx, p, func(tmp string, check RebaseCheck) error {
		if err := validateCheckoutPlan(ctx, p, true); err != nil {
			return err
		}
		backup := "refs/devjay/backups/" + p.ID
		remoteBackup := backup + "-remote"
		for _, entry := range []struct{ ref, head string }{{backup, p.Head}, {remoteBackup, p.TargetHead}} {
			if _, err := actionGit(ctx, p.path, "update-ref", "--no-deref", "--create-reflog", "-m", "DevJay: before rebase and push", entry.ref, entry.head, ""); err != nil {
				return errors.New("Could not save recovery commits. The branch is unchanged.")
			}
			if entry.ref == backup {
				result.Recovery = backup
			} else {
				result.RemoteRecovery = remoteBackup
			}
		}
		incoming := "refs/devjay/prepared/" + p.ID
		defer actionGit(context.Background(), p.path, "update-ref", "--no-deref", "-d", incoming, check.NewHead)
		if _, err := actionGit(ctx, p.path, "fetch", "--no-tags", "--no-write-fetch-head", "--no-recurse-submodules", "--no-auto-maintenance", "--refmap=", "--", tmp, check.NewHead); err != nil {
			return errors.New("Could not load the checked rebase. The branch is unchanged.")
		}
		if _, err := actionGit(ctx, p.path, "update-ref", "--no-deref", incoming, check.NewHead, ""); err != nil {
			return errors.New("Could not retain the checked rebase. The branch is unchanged.")
		}
		if err := validateCheckoutPlan(ctx, p, true); err != nil {
			return err
		}
		if branchInOtherWorktree(ctx, p.path, p.Branch) {
			return errors.New("The branch is in use in another worktree." + manualPublish)
		}
		ctx = context.WithoutCancel(ctx)
		// No rebase runs in the real checkout. Git's non-forced switch protects edits
		// and ignored collisions; the named branch moves only by compare-and-swap.
		if _, err := actionGit(ctx, p.path, "switch", "--detach", "--no-overwrite-ignore", "--no-recurse-submodules", check.NewHead); err != nil {
			return errors.New("Could not load the rebased files safely. Inspect the checkout manually.")
		}
		if branchInOtherWorktree(ctx, p.path, p.Branch) {
			return errors.New("The branch became active elsewhere. The checkout is detached; original commits are preserved.")
		}
		if _, err := actionGit(ctx, p.path, "update-ref", "--no-deref", "-m", "DevJay: apply checked rebase and push", "refs/heads/"+p.Branch, check.NewHead, p.Head); err != nil {
			return errors.New("The branch changed. The checkout is detached; original commits are preserved.")
		}
		if _, err := actionGit(ctx, p.path, "switch", "--no-guess", "--no-overwrite-ignore", "--no-recurse-submodules", "--", p.Branch); err != nil {
			return errors.New("The branch was rebased but the checkout is detached. Reattach it manually.")
		}
		rebased = true
		*expectedHead = check.NewHead
		_, meta, err := cleanCheckout(ctx, p.path)
		if err != nil || meta.OID != check.NewHead || meta.Branch != p.Branch {
			return errors.New("The checkout changed after rebasing. Nothing was pushed.")
		}
		// A fresh immutable push plan targets exactly the checked new commit, while
		// retaining the original confirmed remote SHA as its server-side lease.
		push := *p
		push.Kind, push.Head, push.branchGeneration = "publish", check.NewHead, meta.BranchGeneration
		if err := validateCheckoutPlan(ctx, &push, true); err != nil {
			return err
		}
		pushed := applyPushWithRecovery(ctx, &push, remoteBackup)
		if pushed.Status != "complete" {
			return errors.New(pushed.Message)
		}
		result.Status, result.Message = "complete", "Rebased onto "+p.PublishBase+" and pushed to "+p.Target+"."
		return nil
	})
	if err != nil {
		result.Message = err.Error()
		if rebased {
			result.Message = "Local rebase completed. " + result.Message
		}
		if !strings.Contains(result.Message, "Claude or Astra") {
			result.Message += manualPublish
		}
		var conflict *rebaseActionError
		if errors.As(err, &conflict) {
			result.Files = conflict.check.Files
		}
	}
	return result
}
