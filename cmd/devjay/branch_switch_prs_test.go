package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestBranchPRsMatchRepositoryAndUpstreamAndShareQueries(t *testing.T) {
	data := []byte(`[
 {"number":12,"headRefName":"remote-name","headRepository":{"nameWithOwner":"owner/repo"},"headRepositoryOwner":{"login":"owner"},"reviewDecision":"APPROVED","headRefOid":"abc","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]},
 {"number":13,"headRefName":"remote-name","headRepository":{"nameWithOwner":"owner/fork"},"headRepositoryOwner":{"login":"owner"},"reviewDecision":"APPROVED"}
]`)
	choices := []BranchChoice{
		{Name: "renamed-local", Local: true, prRemote: "origin", prBranch: "remote-name"},
		{Name: "remote-name", Remote: "other", prRemote: "other", prBranch: "remote-name"},
		{Name: "unpublished", Local: true},
		{Name: "no-pr", prRemote: "origin", prBranch: "no-pr"},
	}
	queries := 0
	result := branchPullRequests(choices, func(string) string { return "owner/repo" }, func(string) ([]byte, error) { queries++; return data, nil })
	if queries != 1 {
		t.Fatalf("queried same repository %d times", queries)
	}
	for _, index := range []int{0, 1} {
		prs := result[index].PullRequests
		if len(prs) != 1 || prs[0].Number != 12 || prs[0].State != "Approved" || prs[0].CIState != "failure" {
			t.Fatalf("wrong PR association: %+v", result[index])
		}
	}
	if result[2].PRStatus != "no-upstream" || result[3].PRStatus != "known" || len(result[3].PullRequests) != 0 {
		t.Fatalf("incorrect missing states: %+v", result)
	}
	if choices[0].PRStatus != "" || choices[0].PullRequests != nil {
		t.Fatal("mutated published choices")
	}
}

func TestBranchPRsDoNotClaimAbsenceForFailedOrTruncatedQueries(t *testing.T) {
	choices := []BranchChoice{{Name: "branch", prRemote: "origin", prBranch: "branch"}}
	for _, data := range [][]byte{[]byte(`null`), []byte(`broken`)} {
		result := branchPullRequests(choices, func(string) string { return "owner/repo" }, func(string) ([]byte, error) { return data, nil })
		if result[0].PRStatus != "unavailable" {
			t.Fatalf("malformed response: %+v", result)
		}
	}
	result := branchPullRequests(choices, func(string) string { return "owner/repo" }, func(string) ([]byte, error) { return nil, errors.New("offline") })
	if result[0].PRStatus != "unavailable" {
		t.Fatal("offline was treated as no PR")
	}
	data, _ := json.Marshal(make([]map[string]any, 100))
	result = branchPullRequests(choices, func(string) string { return "owner/repo" }, func(string) ([]byte, error) { return data, nil })
	if result[0].PRStatus != "partial" {
		t.Fatal("truncated list was treated as complete")
	}
}
