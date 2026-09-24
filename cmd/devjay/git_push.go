package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// A separate push URL could publish to a repository other than the one checked.
// Keep the first version scoped to an existing, same-name origin upstream.
func validatePushDestination(ctx context.Context, p *CheckoutPlan) error {
	if p.remote != "origin" || p.remoteRef != "refs/heads/"+p.Branch || p.trackingRef != "refs/remotes/origin/"+p.Branch {
		return errors.New("The push destination does not match this branch’s origin upstream.")
	}
	out, err := git(ctx, p.path, "remote", "get-url", "--push", "--all", p.remote)
	urls := strings.Split(strings.TrimSpace(string(out)), "\n")
	if err != nil || len(urls) != 1 || urls[0] != p.remoteURL {
		return errors.New("Origin has a different or multiple push destinations. Push manually.")
	}
	return nil
}

func preparePush(ctx context.Context, p *CheckoutPlan) error {
	if p.Head == p.TargetHead {
		return errors.New("There are no local commits to push.")
	}
	if _, err := git(ctx, p.path, "merge-base", "--is-ancestor", p.TargetHead, p.Head); err != nil {
		return errors.New("Origin has incoming or rewritten commits. Update the branch or push manually; DevJay will not force-push.")
	}
	out, err := git(ctx, p.path, "rev-list", "--count", p.TargetHead+".."+p.Head, "--")
	if err == nil {
		p.Commits, err = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	if err != nil || p.Commits <= 0 {
		return errors.New("Could not verify the commits to push.")
	}
	return nil
}

func applyPush(ctx context.Context, p *CheckoutPlan) CheckoutAction {
	// Use the confirmed SHA and destination, never push.default, configured
	// refspecs, matching branches, tags, or a branch that could move during push.
	_, pushErr := actionGit(ctx, p.path, "push", "--porcelain", "--no-force", "--no-mirror", "--no-follow-tags", "--recurse-submodules=no", "--", p.remoteURL, p.Head+":"+p.remoteRef)
	// A timeout or lost connection can occur after the server accepted the push.
	// Always inspect the remote independently and never retry automatically.
	verifyCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := actionGit(verifyCtx, p.path, "ls-remote", "--exit-code", "--heads", "--", p.remoteURL, p.remoteRef)
	fields := strings.Fields(string(out))
	if err == nil && len(fields) == 2 && fields[0] == p.Head && fields[1] == p.remoteRef {
		// Do not overwrite a newer observation from another Git client.
		_, _ = publishTrackingRef(verifyCtx, p.path, p.trackingRef, p.Head, p.TargetHead, "DevJay: confirmed push")
		return CheckoutAction{Status: "complete", Message: fmt.Sprintf("Pushed %d %s to %s. Local files and branch are unchanged.", p.Commits, commitWord(p.Commits), p.Target)}
	}
	message := "Could not confirm the push result. Check origin before trying again; no automatic retry was attempted."
	if pushErr != nil && err == nil && len(fields) == 2 {
		message = "Push was rejected or stopped. Origin does not point to the confirmed commit. Check remote changes, branch protection, and authentication before trying again. No force-push was attempted."
	}
	return CheckoutAction{Status: "failed", Message: message}
}

func commitWord(n int) string {
	if n == 1 {
		return "commit"
	}
	return "commits"
}
