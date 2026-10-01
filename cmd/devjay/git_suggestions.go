package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// The default branch is compared with HEAD, independently of the branch's upstream.
type BranchBase struct {
	Branch   string `json:"branch"`
	Tracking string `json:"tracking"`
	Head     string `json:"head"`
	Behind   int    `json:"behind"`
	Note     string `json:"note,omitempty"`
}

func inspectBranchBase(ctx context.Context, s GitStatus, m gitMeta) *BranchBase {
	if m.DefaultRef == "" || s.IsMainBranch || m.Initial || m.Branch == "(detached)" {
		return nil
	}
	base := &BranchBase{Branch: m.DefaultBranch, Tracking: strings.TrimPrefix(m.DefaultRef, "refs/remotes/")}
	base.Head = gitString(ctx, m.Path, "rev-parse", "--verify", m.DefaultRef+"^{commit}")
	if base.Head == "" {
		base.Note = "Default branch comparison unavailable"
		return base
	}
	out, err := git(ctx, m.Path, "rev-list", "--count", m.OID+".."+base.Head, "--")
	if err != nil {
		base.Note = "Default branch comparison unavailable"
		return base
	}
	if _, err := fmt.Sscanf(string(out), "%d", &base.Behind); err != nil {
		base.Note = "Default branch comparison unavailable"
	}
	return base
}

func refreshDefaultBranch(ctx context.Context, m gitMeta, c *remoteCache) {
	if m.DefaultRef == "" {
		return
	}
	if m.DefaultRef == m.Ref {
		c.BaseChecked, c.BaseError = c.Checked, c.Error
		return
	}
	_, err := fetchTrackingRef(ctx, m.Path, m.DefaultRemote, "refs/heads/"+m.DefaultBranch, m.DefaultRef, false)
	if err != nil {
		c.BaseError = "Default branch unavailable · cached comparison"
		return
	}
	c.BaseError, c.BaseChecked = "", time.Now()
}

func hasSuggestedActions(s GitStatus) bool {
	push := s.Ahead > 0 && s.Behind == 0 && s.PushTarget != "" && !s.Rewritten
	return s.CanReplaceRemote || canReturnAfterMerge(s) || (s.Rebased && s.PushTarget != "") || (!s.Rebased && (push || s.Behind > 0 || (s.Base != nil && s.Base.Behind > 0)))
}

// The GitHub lookup already matches repository and exact branch name. Only the
// merged tip on a clean checkout is eligible to return to the default branch.
func canReturnAfterMerge(s GitStatus) bool {
	return s.MergedPR != nil && s.MergedPR.SameHead && s.DefaultBranch != "" &&
		s.MergedPR.Base == s.DefaultBranch && !s.IsMainBranch && s.Changes == 0 &&
		s.Conflicts == 0 && s.LocalNote == ""
}
