package main

import (
	"encoding/json"
	"strings"
	"time"
)

// Matching the current branch is separate from finding historical PRs by name.
type MergedPullRequest struct {
	Number   int       `json:"number"`
	Base     string    `json:"base"`
	Head     string    `json:"head"`
	MergedAt time.Time `json:"mergedAt"`
	URL      string    `json:"url"`
	SameHead bool      `json:"sameHead"`
}

func latestMergedPR(data []byte, repo, branch string) (*MergedPullRequest, error) {
	var prs []struct {
		Number                                           int
		State, HeadRefName, HeadRefOid, BaseRefName, URL string
		HeadRepository                                   struct{ NameWithOwner string }
		MergedAt                                         time.Time
	}
	if err := json.Unmarshal(data, &prs); err != nil {
		return nil, err
	}
	var latest *MergedPullRequest
	for _, pr := range prs {
		// This finds a candidate only. The checkout must also match its tip and lifetime.
		if pr.State != "MERGED" || pr.HeadRefName != branch || pr.HeadRefOid == "" ||
			pr.Number <= 0 || pr.MergedAt.IsZero() || !strings.EqualFold(pr.HeadRepository.NameWithOwner, repo) {
			continue
		}
		if latest == nil || pr.MergedAt.After(latest.MergedAt) {
			latest = &MergedPullRequest{Number: pr.Number, Head: pr.HeadRefOid, Base: pr.BaseRefName, MergedAt: pr.MergedAt, URL: pr.URL}
		}
	}
	return latest, nil
}

func mergedPRMatchesBranch(pr *MergedPullRequest, m gitMeta) bool {
	if pr == nil || m.OID == "" || m.OID != pr.Head {
		return false
	}
	// Recreating the same name (even at the old SHA) starts a new local branch.
	// With no creation record, an exact tip match is the conservative fallback;
	// shared ancestry or a name match alone never attaches an old merged PR.
	return m.BranchCreated.IsZero() || (!pr.MergedAt.IsZero() && !m.BranchCreated.After(pr.MergedAt))
}
