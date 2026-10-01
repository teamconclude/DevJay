package main

import (
	"context"
	"errors"
	"strconv"
	"strings"
)

const manualPublish = " Complete the rebase and push manually with Claude or Astra."

func isPublishAction(kind string) bool { return kind == "publish" || kind == "rebase-push" }

func isPushAction(kind string) bool { return kind == "push" || isPublishAction(kind) }

func preparePublishBase(ctx context.Context, p *CheckoutPlan, m gitMeta) error {
	if m.DefaultBranch == "" || m.DefaultBranch == p.Branch || m.DefaultRef == "" || m.DefaultRemote == "" || m.DefaultRemote == "." {
		return errors.New("Could not verify a separate remote default branch." + manualPublish)
	}
	p.PublishBase, p.publishBaseRef, p.publishBaseRemote = m.DefaultBranch, m.DefaultRef, m.DefaultRemote
	p.publishBaseURL = gitString(ctx, p.path, "remote", "get-url", m.DefaultRemote)
	if p.publishBaseURL == "" {
		return errors.New("Could not verify the default branch remote." + manualPublish)
	}
	if _, err := fetchTrackingRef(ctx, p.path, m.DefaultRemote, "refs/heads/"+m.DefaultBranch, m.DefaultRef, true); err != nil {
		return errors.New("Could not fetch the latest default branch. Nothing was pushed." + manualPublish)
	}
	p.publishBaseHead = gitString(ctx, p.path, "rev-parse", "--verify", m.DefaultRef+"^{commit}")
	return nil
}

func prepareRebasedPublish(ctx context.Context, p *CheckoutPlan, m gitMeta) error {
	if err := preparePublishBase(ctx, p, m); err != nil {
		return err
	}
	if err := checkRebasedPublish(ctx, p); err != nil {
		return err
	}
	count := gitString(ctx, p.path, "rev-list", "--count", p.publishBaseHead+".."+p.Head, "--")
	var err error
	p.Commits, err = strconv.Atoi(count)
	if err != nil || p.Commits <= 0 {
		return errors.New("Could not count the rebased commits." + manualPublish)
	}
	return nil
}

func validatePublishBase(ctx context.Context, p *CheckoutPlan, m gitMeta, checkRemote bool) error {
	if p.publishBaseHead == "" || m.DefaultBranch != p.PublishBase || m.DefaultRef != p.publishBaseRef || m.DefaultRemote != p.publishBaseRemote ||
		gitString(ctx, p.path, "remote", "get-url", p.publishBaseRemote) != p.publishBaseURL {
		return errors.New("The default branch configuration changed. Nothing was pushed." + manualPublish)
	}
	if checkRemote {
		out, err := actionGit(ctx, p.path, "ls-remote", "--exit-code", "--heads", "--", p.publishBaseURL, "refs/heads/"+p.PublishBase)
		fields := strings.Fields(string(out))
		if err != nil || len(fields) != 2 || fields[0] != p.publishBaseHead || fields[1] != "refs/heads/"+p.PublishBase {
			return errors.New("The default branch changed or could not be verified. Nothing was pushed." + manualPublish)
		}
	}
	return nil
}

// The status badge is only a patch comparison. Publication requires stronger
// evidence: replay the current remote history without conflict resolutions or
// custom drivers, then match every resulting tree, author and message against
// the beginning of the local rebased series. Additional local commits may
// follow that series. Ambiguous or non-linear histories require manual review.
func checkRebasedPublish(ctx context.Context, p *CheckoutPlan) error {
	return checkRebasedHistory(ctx, p, p.path, p.Head)
}

// actualPath may be the disposable trial: verify publication before touching the Site.
func checkRebasedHistory(ctx context.Context, p *CheckoutPlan, actualPath, newHead string) error {
	if p.publishBaseHead == "" || newHead == p.TargetHead {
		return errors.New("There is no verified rebase to publish." + manualPublish)
	}
	if _, err := git(ctx, actualPath, "merge-base", "--is-ancestor", p.TargetHead, newHead); !exitedWith(err, 1) {
		return errors.New("This is not a verified rewritten branch. Use the ordinary push action or review it manually.")
	}
	oldBases, err := git(ctx, p.path, "merge-base", "--all", p.TargetHead, p.publishBaseHead)
	if err != nil || len(strings.Fields(string(oldBases))) != 1 {
		return errors.New("The remote history has no unambiguous base." + manualPublish)
	}
	oldBase := strings.TrimSpace(string(oldBases))
	if _, err := publishSeries(ctx, p.path, oldBase, p.TargetHead); err != nil {
		return err
	}
	check := runRebaseTrialFrom(ctx, p.path, false, p.TargetHead, func(tmp string, check RebaseCheck) error {
		if check.Head != p.Head || check.TargetHead != p.publishBaseHead {
			return errors.New("The branches changed during the conflict check. Nothing was pushed." + manualPublish)
		}
		if _, err := git(ctx, actualPath, "merge-base", "--is-ancestor", p.publishBaseHead, newHead); err != nil {
			return errors.New("The local branch does not include the latest " + p.PublishBase + ". Nothing was pushed." + manualPublish)
		}
		expected, err := publishSeries(ctx, tmp, p.publishBaseHead, check.NewHead)
		if err != nil {
			return err
		}
		actual, err := publishSeries(ctx, actualPath, p.publishBaseHead, newHead)
		if err != nil {
			return err
		}
		if expected == "" || !strings.HasPrefix(actual, expected) {
			return errors.New("The remote work could not be reproduced exactly in the local rebased history. Nothing was pushed." + manualPublish)
		}
		return nil
	})
	if check.Status != "applied" {
		return &rebaseActionError{check}
	}
	return nil
}

func publishSeries(ctx context.Context, path, base, head string) (string, error) {
	rangeSpec := base + ".." + head
	count, err := strconv.Atoi(gitString(ctx, path, "rev-list", "--count", rangeSpec, "--"))
	if err != nil || count < 1 || count > 256 {
		return "", errors.New("The branch history is empty, unavailable or too large to verify safely." + manualPublish)
	}
	merges, err := git(ctx, path, "rev-list", "--merges", rangeSpec, "--")
	if err != nil || strings.TrimSpace(string(merges)) != "" {
		return "", errors.New("This history contains merges or could not be verified." + manualPublish)
	}
	// NUL-separated fields and a final record delimiter keep multiline messages
	// distinct. Commit IDs/committer timestamps change during an ordinary rebase.
	out, err := git(ctx, path, "log", "--reverse", "--no-decorate", "--no-show-signature", "--format=%T%x00%an%x00%ae%x00%at%x00%B%x00", rangeSpec, "--")
	if err != nil || len(out) == 0 {
		return "", errors.New("Could not verify the rebased commit contents." + manualPublish)
	}
	return string(out), nil
}
