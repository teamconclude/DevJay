package main

import "strings"

func trackedPullRequestBranch(remote, merge string) string {
	if remote != "" && remote != "." && strings.HasPrefix(merge, "refs/heads/") {
		return strings.TrimPrefix(merge, "refs/heads/")
	}
	return ""
}

func (m gitMeta) pullRequestBranch() string {
	if branch := trackedPullRequestBranch(m.Remote, m.Merge); branch != "" {
		return branch
	}
	// Without a remote upstream, retain the same-name origin lookup. This
	// does not establish tracking or enable a Git mutation.
	return m.Branch
}
