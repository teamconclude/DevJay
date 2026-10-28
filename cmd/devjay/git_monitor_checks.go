package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// Fast activity uses lightweight queries for every PR state. Normal and
// background cadences are supplied by the full refresh, including pending CI,
// so a second timer cannot bypass those intervals or duplicate the same query.
func nextPullRequestCheck(meta gitMeta, cache remoteCache, cadence gitPollCadence) time.Time {
	if meta.GitHubRepo == "" || meta.Initial || meta.Branch == "(detached)" || cadence.status >= cadence.full {
		return time.Time{}
	}
	interval := cadence.status
	if cache.ChecksRetry {
		interval = max(interval, githubRetryMinimum)
	}
	return cache.ChecksAttempted.Add(interval)
}

func pullRequestHeads(data []byte, repo string) map[int]string {
	var prs []pullRequest
	if json.Unmarshal(data, &prs) != nil {
		return nil
	}
	owner, _, _ := strings.Cut(repo, "/")
	heads := map[int]string{}
	for _, pr := range prs {
		if pr.Number > 0 && strings.EqualFold(pr.HeadRepositoryOwner.Login, owner) {
			heads[pr.Number] = pr.HeadRefOid
		}
	}
	return heads
}

// Recent interaction uses a small status query without a
// fetch or review bodies. A changed PR/head requests a full refresh.
func refreshPullRequestChecks(ctx context.Context, m gitMeta, cache *remoteCache) bool {
	out, err := runCommand(ctx, 15*time.Second, "gh", "pr", "list", "--repo", m.GitHubRepo, "--head", m.pullRequestBranch(),
		"--state", "open", "--limit", "20", "--json", "number,isDraft,reviewDecision,headRepositoryOwner,headRefOid,statusCheckRollup")
	cache.ChecksAttempted = time.Now()
	heads := pullRequestHeads(out, m.GitHubRepo)
	if err != nil || heads == nil {
		cache.ChecksRetry = true
		cache.PullRequests = slices.Clone(cache.PullRequests)
		for i := range cache.PullRequests {
			cache.PullRequests[i].CIState = "unknown"
		}
		return false
	}
	cache.ChecksRetry = false
	full := len(heads) != len(cache.CheckHeads)
	for number, head := range heads {
		full = full || head == "" || head != cache.CheckHeads[number]
	}
	// Reports for another pushed head cannot retain a current-finding claim.
	var reviews []ReviewFindings
	for _, review := range cache.Reviews {
		if head := heads[review.Number]; head != "" && head == cache.CheckHeads[review.Number] {
			reviews = append(reviews, review)
		}
	}
	cache.Reviews = reviews
	cache.CheckHeads = heads
	links := openPullRequestLinks(out, m.GitHubRepo)
	// Authorship follows the PR, not its commits or CI runs. Reuse the full
	// refresh's confirmed author without adding avatar queries to CI polling.
	for i := range links {
		for _, previous := range cache.PullRequests {
			if links[i].Number == previous.Number && links[i].URL == previous.URL {
				links[i].OtherAuthor = previous.OtherAuthor
				break
			}
		}
	}
	cache.PullRequests = links
	owner, _, _ := strings.Cut(m.GitHubRepo, "/")
	cache.GitHub = prSummary(out, owner)
	return full
}
