package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
)

func validMergeMethod(method string) bool {
	return method == "rebase" || method == "squash" || method == "merge"
}

func (c SiteConfig) mergeMethod() string {
	if validMergeMethod(c.PreferredMergeMethod) {
		return c.PreferredMergeMethod
	}
	return "rebase"
}

// Read-only checks during preparation and again immediately before merging.
// GitHub still enforces its rules when the expected-head merge is submitted.
func checkPRMerge(ctx context.Context, p *CheckoutPlan) error {
	pr := p.ApprovedPR
	if pr == nil || !prReviewSHA.MatchString(pr.head) {
		return errors.New("The approved PR identity is unavailable. Reopen its card menu.")
	}
	if !strings.EqualFold(githubRepo(p.remoteURL), pr.repo) {
		return errors.New("The default branch's remote does not match the PR repository. Review the remote configuration before merging.")
	}
	var current struct {
		pullRequest
		MergeStateStatus, BaseRefOid string
	}
	out, err := runCommand(ctx, 15*time.Second, "gh", "pr", "view", strconv.Itoa(pr.Number), "--repo", "github.com/"+pr.repo,
		"--json", approvedPRFields+",mergeable,mergeStateStatus,baseRefOid")
	if err != nil || json.Unmarshal(out, &current) != nil {
		return errors.New("Could not check the PR's merge readiness on GitHub.")
	}
	if latest := current.approvedReturn(pr.repo); latest == nil || *latest != *pr || current.BaseRefOid != p.TargetHead {
		return errors.New("The PR, approval, or base branch changed. Reopen the action to review it again.")
	}
	if current.Mergeable != "MERGEABLE" || current.MergeStateStatus != "CLEAN" {
		return errors.New("GitHub has not marked this PR ready to merge with passing checks. Resolve conflicts, pending or failing checks, and branch requirements on GitHub, then reopen the action.")
	}
	var repo struct {
		FullName           string `json:"full_name"`
		AllowMergeCommit   bool   `json:"allow_merge_commit"`
		AllowSquashMerge   bool   `json:"allow_squash_merge"`
		AllowRebaseMerge   bool   `json:"allow_rebase_merge"`
		Archived, Disabled bool
		Permissions        struct{ Push bool }
	}
	if err := prReviewAPI(ctx, "repos/"+pr.repo, nil, &repo); err != nil || !strings.EqualFold(repo.FullName, pr.repo) {
		return errors.New("Could not verify the repository's merge settings and permissions.")
	}
	if repo.Archived || repo.Disabled || !repo.Permissions.Push {
		return errors.New("This account must have write access to an active repository. Review the PR on GitHub.")
	}
	methods := []string{}
	for _, method := range []struct {
		name    string
		allowed bool
	}{{"rebase", repo.AllowRebaseMerge}, {"squash", repo.AllowSquashMerge}, {"merge", repo.AllowMergeCommit}} {
		if method.allowed {
			methods = append(methods, method.name)
		}
	}
	if len(methods) == 0 {
		return errors.New("The repository does not allow any supported merge method. Review the PR on GitHub.")
	}
	if p.MergeMethod == "" {
		p.MergeMethod = methods[0]
		if slices.Contains(methods, p.preferredMergeMethod) {
			p.MergeMethod = p.preferredMergeMethod
		}
	}
	if !slices.Contains(methods, p.MergeMethod) {
		return errors.New("The selected merge method is no longer allowed. Reopen the action to review the repository's current merge methods.")
	}
	p.MergeMethods = methods
	return nil
}

type mergedPRRef struct {
	SHA, Ref string
	Repo     struct {
		FullName string `json:"full_name"`
	}
}

// Called only after a confirmed action has the repository lock and a recovery
// reference. Send one merge request; a lost reply is reconciled by reads only.
func mergePRAndRefreshTarget(ctx context.Context, p *CheckoutPlan) error {
	if err := validateCheckoutPlan(ctx, p, true); err != nil {
		return err
	}
	pr := p.ApprovedPR
	endpoint := fmt.Sprintf("repos/%s/pulls/%d", pr.repo, pr.Number)
	var response struct {
		Merged bool
		SHA    string
	}
	mergeErr := prReviewAPIMethod(ctx, "PUT", endpoint+"/merge", map[string]string{"sha": p.Head, "merge_method": p.MergeMethod}, &response)
	var receipt struct {
		Number      int
		Merged      bool
		State       string
		MergeCommit string `json:"merge_commit_sha"`
		Head, Base  mergedPRRef
	}
	if err := prReviewAPI(ctx, endpoint, nil, &receipt); err != nil {
		return errors.New("The merge outcome could not be verified. No local branch was switched or deleted. Check the PR on GitHub before trying again.")
	}
	if receipt.Number != pr.Number || !receipt.Merged || receipt.State != "closed" ||
		receipt.Head.SHA != pr.head || receipt.Head.Ref != pr.branch || receipt.Base.Ref != pr.base ||
		!strings.EqualFold(receipt.Head.Repo.FullName, pr.repo) || !strings.EqualFold(receipt.Base.Repo.FullName, pr.repo) ||
		!prReviewSHA.MatchString(receipt.MergeCommit) ||
		(mergeErr == nil && (!response.Merged || response.SHA != receipt.MergeCommit)) {
		return errors.New("GitHub did not confirm a merge of this exact PR and commit. No local branch was switched or deleted. Check its status and merge requirements on GitHub; the merge was not retried.")
	}
	p.mergeCommit = receipt.MergeCommit
	// From here on, any failure must disclose that the remote merge succeeded.
	// Local cleanup can stop independently and must never undo or retry a merge.
	if err := validateCheckoutPlan(ctx, p, false); err != nil {
		return err
	}
	if _, err := fetchTrackingRef(ctx, p.path, p.remote, p.remoteRef, p.trackingRef, true); err != nil {
		return fmt.Errorf("Could not fetch the merged %s. The local feature branch was kept; refresh and return to the default branch separately.", p.Target)
	}
	tip := gitString(ctx, p.path, "rev-parse", "--verify", p.trackingRef+"^{commit}")
	if tip == "" {
		return errors.New("The merged default branch is unavailable locally. The feature branch was kept.")
	}
	// Preserve the checked base and verified result. Squash and rebase create
	// new commit IDs; the original feature commits remain in the recovery ref.
	ancestors := []string{p.TargetHead, p.mergeCommit}
	if p.MergeMethod == "merge" {
		ancestors = append(ancestors, p.Head)
	}
	for _, ancestor := range ancestors {
		if _, err := git(ctx, p.path, "merge-base", "--is-ancestor", ancestor, tip); err != nil {
			return errors.New("The fetched default branch does not preserve the confirmed merge and branch history. The local feature branch was kept; inspect GitHub before returning.")
		}
	}
	p.TargetHead = tip
	return validateCheckoutPlan(ctx, p, true)
}
