package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// One bounded query per repository, only while its chooser is open. Review
// comments are not needed; the same conservative CI rollup is used by cards.
func readBranchPullRequests(ctx context.Context, path string, choices []BranchChoice) []BranchChoice {
	return branchPullRequests(choices, func(remote string) string {
		return githubRepo(gitString(ctx, path, "remote", "get-url", remote))
	}, func(repo string) ([]byte, error) {
		return runCommand(ctx, 15*time.Second, "gh", "pr", "list", "--repo", repo, "--state", "open", "--limit", "100",
			"--json", "number,isDraft,reviewDecision,headRepositoryOwner,headRepository,headRefName,headRefOid,statusCheckRollup")
	})
}

func decodeBranchPullRequests(data []byte, repo string) (map[string][]PullRequestLink, bool, error) {
	var records []struct {
		Number         int
		HeadRefName    string
		HeadRepository struct{ NameWithOwner string }
	}
	if err := json.Unmarshal(data, &records); err != nil || records == nil {
		return nil, false, errors.New("could not read branch pull requests")
	}
	links := map[int]PullRequestLink{}
	for _, link := range openPullRequestLinks(data, repo) {
		links[link.Number] = link
	}
	result := map[string][]PullRequestLink{}
	for _, record := range records {
		// A fork or another repository with a matching branch name is not
		// evidence that this checkout owns the PR.
		if record.HeadRefName == "" || !strings.EqualFold(record.HeadRepository.NameWithOwner, repo) {
			continue
		}
		if link, ok := links[record.Number]; ok {
			result[record.HeadRefName] = append(result[record.HeadRefName], link)
		}
	}
	return result, len(records) >= 100, nil
}

func branchPullRequests(choices []BranchChoice, repository func(string) string, fetch func(string) ([]byte, error)) []BranchChoice {
	result := append([]BranchChoice(nil), choices...)
	type reading struct {
		branches   map[string][]PullRequestLink
		incomplete bool
		err        error
	}
	readings := map[string]reading{}
	remotes := map[string]string{}
	for i := range result {
		choice := &result[i]
		choice.PullRequests = nil
		choice.PRStatus = "unavailable"
		if choice.prRemote == "" || choice.prRemote == "." || choice.prBranch == "" {
			choice.PRStatus = "no-upstream"
			continue
		}
		repo, known := remotes[choice.prRemote]
		if !known {
			repo = repository(choice.prRemote)
			remotes[choice.prRemote] = repo
		}
		if !githubRepositoryName.MatchString(repo) {
			continue
		}
		key := strings.ToLower(repo)
		read, known := readings[key]
		if !known {
			// Bound work for checkouts with many configured remotes.
			if len(readings) >= 4 {
				continue
			}
			data, err := fetch(repo)
			read.err = err
			if err == nil {
				read.branches, read.incomplete, read.err = decodeBranchPullRequests(data, repo)
			}
			readings[key] = read
		}
		if read.err != nil {
			continue
		}
		choice.PullRequests = read.branches[choice.prBranch]
		choice.PRStatus = "known"
		if read.incomplete {
			choice.PRStatus = "partial"
		}
	}
	return result
}
