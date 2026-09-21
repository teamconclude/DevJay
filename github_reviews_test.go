package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewCounts(t *testing.T) {
	tests := []struct {
		name, body         string
		blockers, warnings int
		ok                 bool
	}{
		{"metadata", "<!-- review reviewer=agent head=abc base=def blockers=1 warnings=7 warnings_preexisting=0 notes=0 -->", 1, 7, true},
		{"clean", "<!-- review blockers=0 warnings=0 -->", 0, 0, true},
		{"missing count", "<!-- review blockers=1 -->", 0, 0, false},
		{"negative", "<!-- review blockers=-1 warnings=7 -->", 0, 0, false},
		{"invalid", "<!-- review blockers=0 warnings=many -->", 0, 0, false},
		{"overflow", "<!-- review blockers=9999999999999999999999 warnings=0 -->", 0, 0, false},
		{"duplicate", "<!-- review blockers=0 blockers=1 warnings=0 -->", 0, 0, false},
		{"ambiguous", "<!-- review blockers=0 warnings=0 --><!-- review blockers=1 warnings=7 -->", 0, 0, false},
		{"markdown", "## Blockers\n\n- First\n  - Evidence\n\n## Warnings\n\n- One\n- Two\n## Notes\n- Ignored", 1, 2, true},
		{"none", "## Blockers\nNone.\n## Warnings\n1. One\n2. Two", 0, 2, true},
		{"missing section", "## Warnings\n- One", 0, 0, false},
		{"empty unknown", "## Blockers\nStill checking\n## Warnings\nNone.", 0, 0, false},
		{"discussion", "I fixed 1 blocker and 7 warnings", 0, 0, false},
		{"quoted", "> <!-- review blockers=1 warnings=7 -->", 0, 0, false},
		{"fenced", "```html\n<!-- review blockers=1 warnings=7 -->\n```", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseReviewCounts(tt.body)
			if ok != tt.ok || got.Blockers != tt.blockers || got.Warnings != tt.warnings {
				t.Fatalf("got %+v, %v", got, ok)
			}
		})
	}
}
func TestLatestReportAcrossPagesAndPostTypes(t *testing.T) {
	// Each gh page is a separate JSON array, not one concatenated array.
	posts, err := decodeReviewPosts([]byte(`[{"body":"<!-- review blockers=0 warnings=1 -->","submitted_at":"2026-09-21T08:39:03Z","commit_id":"head","state":"COMMENTED"}]
 [{"body":"<!-- review blockers=1 warnings=7 -->","submitted_at":"2026-09-21T08:53:23Z","commit_id":"head","state":"COMMENTED","html_url":"https://example.invalid/review"}]`))
	if err != nil {
		t.Fatal(err)
	}
	pr := pullRequest{Number: 2812, HeadRefOid: "head"}
	assertCounts := func(b, w int) *ReviewFindings {
		t.Helper()
		got := latestReviewFindings(posts, pr)
		if got == nil || got.Blockers != b || got.Warnings != w {
			t.Fatalf("got %+v", got)
		}
		return got
	}
	got := assertCounts(1, 7)
	if got.Earlier || !strings.Contains(got.Detail, "https://example.invalid/review") {
		t.Fatal(got)
	}
	later := time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC)
	posts = append(posts, reviewPost{Body: "Fixed warnings", CreatedAt: later}, reviewPost{Body: "<!-- review blockers=0 warnings=0 -->", SubmittedAt: later, State: "DISMISSED"})
	assertCounts(1, 7)
	posts = append(posts, reviewPost{Body: "<!-- review head=head blockers=0 warnings=2 -->", CreatedAt: later, URL: "https://example.invalid/comment"})
	assertCounts(0, 2)
	pr.HeadRefOid = "new-head"
	if !assertCounts(0, 2).Earlier {
		t.Fatal("old review shown as current")
	}
	posts = append(posts, reviewPost{Body: "<!-- review head=new-head blockers=0 warnings=0 -->", CreatedAt: later.Add(time.Hour)})
	assertCounts(0, 0)
	if _, err := decodeReviewPosts([]byte(`[] {broken`)); err == nil {
		t.Fatal("partial response accepted")
	}
}
func TestFetchReviewFindingsUsesPaginationAndBothEndpoints(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 *--paginate*) ;;
 *) exit 1 ;;
esac
case "$*" in
 */reviews\?*)
 printf '%s\n' '[{"body":"<!-- review blockers=0 warnings=1 -->","state":"COMMENTED","submitted_at":"2026-09-21T08:39:03Z"}]' '[{"body":"<!-- review blockers=1 warnings=7 -->","state":"COMMENTED","submitted_at":"2026-09-21T08:53:23Z"}]' ;;
 */comments\?*)
 if test -f "$(dirname "$0")/fail"; then exit 1; fi
 printf '%s\n' '[{"body":"Thanks for the report","created_at":"2026-09-21T09:00:00Z"}]' ;;
 *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	pr := pullRequest{Number: 2812}
	got := fetchReviewFindings(context.Background(), "example/repo", pr)
	if got == nil || got.Unavailable || got.Blockers != 1 || got.Warnings != 7 {
		t.Fatalf("got %+v", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	got = fetchReviewFindings(context.Background(), "example/repo", pr)
	if got == nil || !got.Unavailable {
		t.Fatalf("partial fetch must be unavailable: %+v", got)
	}
}
