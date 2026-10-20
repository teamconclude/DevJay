package main

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

const pendingChecksInterval = 30 * time.Second

func checksNeedRefresh(cache remoteCache) bool { return cache.ChecksRetry || hasPendingChecks(cache) }

func checkRefreshInterval(cache remoteCache) time.Duration {
	if cache.ChecksRetry {
		return time.Minute
	}
	return pendingChecksInterval
}

func hasPendingChecks(cache remoteCache) bool {
	for _, pr := range cache.PullRequests {
		if pr.CIState == "pending" {
			return true
		}
	}
	return false
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

// Pending checks need a small status query, not another fetch or a download of
// review bodies. A changed PR/head requests one full refresh to revalidate them.
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
