package main

import (
	"context"
	"errors"
	"strings"
)

// A branch comparison uses only captured commits, never a moving ref or the
// working tree. The target is a display label; Base is the unique merge base.
type ViewBranchDiff struct {
	Target string `json:"target"`
	Base   string `json:"base"`
	Note   string `json:"note,omitempty"`
}

func captureViewBranchDiff(ctx context.Context, v *GitView, status GitStatus) *ViewBranchDiff {
	comparison := &ViewBranchDiff{}
	tip := ""
	if status.Base != nil {
		comparison.Target, tip = status.Base.Tracking, status.Base.Head
	} else if status.IsMainBranch {
		comparison.Target, tip = v.Tracking, v.Upstream
	}
	if tip == "" || v.Head == "" {
		comparison.Note = "No cached base is available for this branch. Open Full history to inspect individual commits."
		return comparison
	}
	out, err := viewerGit(ctx, v.root, "merge-base", "--all", tip, v.Head)
	bases := strings.Fields(string(out))
	if err != nil || len(bases) != 1 {
		comparison.Note = "A unique common ancestor could not be found. Open Full history to inspect individual commits."
		return comparison
	}
	comparison.Base = bases[0]
	return comparison
}

func selectViewHistoryScope(ctx context.Context, v *GitView, scope string) error {
	if v.Kind != "branch" || (scope != "branch" && scope != "full") {
		return errors.New("Choose Branch history or Full history in History.")
	}
	branchOnly := scope == "branch"
	if v.BranchOnly == branchOnly {
		return nil
	}
	v.BranchOnly, v.AllChanges = branchOnly, false
	v.Page, v.BranchCommits = 0, nil
	return loadGitView(ctx, v, false)
}

func selectViewBranchDiff(ctx context.Context, v *GitView) error {
	if v.Kind != "branch" {
		return errors.New("Open History to compare branch changes.")
	}
	if !v.BranchOnly {
		// Enter the comparison from any full-history page. The commit list and
		// aggregate diff must describe the same captured base..HEAD range.
		v.BranchOnly, v.AllChanges = true, true
		v.Page, v.BranchCommits = 0, nil
		return loadGitView(ctx, v, false)
	}
	return showViewBranchDiff(ctx, v)
}

func showViewBranchDiff(ctx context.Context, v *GitView) error {
	v.AllChanges = true
	v.Commit, v.Parent, v.Message, v.Diff, v.DiffNote = "", "", "", "", ""
	v.Files, v.File, v.Merge, v.FilesLimited = []ViewFile{}, -1, false, false
	if v.BranchDiff == nil || v.BranchDiff.Base == "" || v.Head == "" {
		return errors.New("Branch comparison unavailable. Refresh or open Full history.")
	}
	v.Parent = v.BranchDiff.Base
	return loadViewRevisionFiles(ctx, v, v.Head, "Branch")
}

func viewDiffRevision(v *GitView) string {
	if v.AllChanges {
		return v.Head
	}
	return v.Commit
}
