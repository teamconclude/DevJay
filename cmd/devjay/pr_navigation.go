package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// URLs are derived from verified repository identity and PR numbers, never
// supplied by a frontend event or copied from arbitrary GitHub response text.
type PullRequestLink struct {
	Number      int                `json:"number"`
	URL         string             `json:"url"`
	State       string             `json:"state"`
	IsDraft     bool               `json:"isDraft"`
	CIState     string             `json:"ciState,omitempty"`
	OtherAuthor *PullRequestAuthor `json:"otherAuthor,omitempty"`
}

type prOpenRequest struct {
	Anchor *cardAnchor `json:"anchor,omitempty"`
	SiteID string      `json:"siteID"`
	Number int         `json:"number"`
	NewTab bool        `json:"newTab"`
}

var githubRepositoryName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*/[A-Za-z0-9_.-]+$`)

func pullRequestLink(repo string, number int, state string, draft bool) (PullRequestLink, bool) {
	_, name, _ := strings.Cut(repo, "/")
	if !githubRepositoryName.MatchString(repo) || name == "." || name == ".." || number <= 0 {
		return PullRequestLink{}, false
	}
	return PullRequestLink{Number: number, URL: fmt.Sprintf("https://github.com/%s/pull/%d", repo, number), State: state, IsDraft: draft}, true
}

func openPullRequestLinks(data []byte, repo string) []PullRequestLink {
	var prs []pullRequest
	if json.Unmarshal(data, &prs) != nil {
		return nil
	}
	owner, _, _ := strings.Cut(repo, "/")
	var result []PullRequestLink
	seen := map[int]bool{}
	for _, pr := range prs {
		if !strings.EqualFold(pr.HeadRepositoryOwner.Login, owner) || seen[pr.Number] {
			continue
		}
		state := "Open"
		switch pr.ReviewDecision {
		case "APPROVED":
			state = "Approved"
		case "CHANGES_REQUESTED":
			state = "Changes requested"
		case "REVIEW_REQUIRED":
			state = "Review required"
		}
		if pr.IsDraft {
			state = "Draft"
		}
		if link, ok := pullRequestLink(repo, pr.Number, state, pr.IsDraft); ok {
			link.CIState = pullRequestCIState(pr)
			result = append(result, link)
			seen[pr.Number] = true
		}
	}
	return result
}

func requestedPR(status GitStatus, number int) (PullRequestLink, bool) {
	for _, pr := range status.PullRequests {
		if pr.Number == number && pr.Number > 0 {
			return pr, true
		}
	}
	return PullRequestLink{}, false
}

func validPRSender(sender, siteID string) bool {
	return siteID != "" && (sender == "strip" || sender == "site-"+siteID)
}
