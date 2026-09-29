package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func checkRun(status, conclusion string) string {
	data, _ := json.Marshal(map[string]string{"__typename": "CheckRun", "status": status, "conclusion": conclusion})
	return string(data)
}

func statusContext(state string) string {
	data, _ := json.Marshal(map[string]string{"__typename": "StatusContext", "state": state})
	return string(data)
}

func TestPullRequestCIStates(t *testing.T) {
	for _, tt := range []struct {
		name, data, want string
	}{
		{"missing", "", "unknown"},
		{"null", "null", "unknown"},
		{"empty", "[]", "none"},
		{"malformed", "[", "unknown"},
		{"unexpected schema", `{}`, "unknown"},
		{"null entry", `[null]`, "unknown"},
		{"new context type", `[{"__typename":"NewKind","state":"FAILURE"}]`, "unknown"},
		{"failed", "[" + checkRun("COMPLETED", "FAILURE") + "]", "failure"},
		{"timed out", "[" + checkRun("COMPLETED", "TIMED_OUT") + "]", "failure"},
		{"startup failed", "[" + checkRun("COMPLETED", "STARTUP_FAILURE") + "]", "failure"},
		{"action required", "[" + checkRun("COMPLETED", "ACTION_REQUIRED") + "]", "action_required"},
		{"cancelled", "[" + checkRun("COMPLETED", "CANCELLED") + "]", "cancelled"},
		{"stale", "[" + checkRun("COMPLETED", "STALE") + "]", "unknown"},
		{"success", "[" + checkRun("COMPLETED", "SUCCESS") + "]", "success"},
		{"skipped", "[" + checkRun("COMPLETED", "SKIPPED") + "]", "neutral"},
		{"neutral", "[" + checkRun("COMPLETED", "NEUTRAL") + "]", "neutral"},
		{"missing conclusion", "[" + checkRun("COMPLETED", "") + "]", "unknown"},
		{"new conclusion", "[" + checkRun("COMPLETED", "NEW_RESULT") + "]", "unknown"},
		{"missing run status", "[" + checkRun("", "FAILURE") + "]", "unknown"},
		{"new run status", "[" + checkRun("NEW_STATUS", "FAILURE") + "]", "unknown"},
		{"rerun ignores old conclusion", "[" + checkRun("IN_PROGRESS", "FAILURE") + "]", "pending"},
		{"failure while running", "[" + checkRun("IN_PROGRESS", "") + "," + checkRun("COMPLETED", "FAILURE") + "]", "failure"},
		{"failed with unknown", "[null," + checkRun("COMPLETED", "FAILURE") + "]", "failure"},
		{"attention while running", "[" + checkRun("COMPLETED", "ACTION_REQUIRED") + "," + checkRun("QUEUED", "") + "]", "action_required"},
		{"cancelled with running", "[" + checkRun("COMPLETED", "CANCELLED") + "," + checkRun("QUEUED", "") + "]", "pending"},
		{"cancelled with pass", "[" + checkRun("COMPLETED", "CANCELLED") + "," + checkRun("COMPLETED", "SUCCESS") + "]", "cancelled"},
		{"pass with skipped", "[" + checkRun("COMPLETED", "SKIPPED") + "," + checkRun("COMPLETED", "SUCCESS") + "]", "success"},
		{"pass with unknown", "[null," + checkRun("COMPLETED", "SUCCESS") + "]", "unknown"},
		{"typed invalid with pass", `[{"__typename":"CheckRun","status":42},` + checkRun("COMPLETED", "SUCCESS") + "]", "unknown"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := pullRequestCIState(pullRequest{HeadRefOid: "pushed-head", StatusCheckRollup: json.RawMessage(tt.data)})
			if got != tt.want {
				t.Fatalf("state = %q, want %q", got, tt.want)
			}
		})
	}
	for _, status := range []string{"QUEUED", "IN_PROGRESS", "PENDING", "REQUESTED", "WAITING"} {
		if got := pullRequestCheckState(json.RawMessage(checkRun(status, ""))); got != "pending" {
			t.Errorf("run status %s = %s", status, got)
		}
	}
	for status, want := range map[string]string{"FAILURE": "failure", "ERROR": "failure", "EXPECTED": "pending", "PENDING": "pending", "SUCCESS": "success", "": "unknown", "NEW": "unknown"} {
		if got := pullRequestCheckState(json.RawMessage(statusContext(status))); got != want {
			t.Errorf("commit status %s = %s, want %s", status, got, want)
		}
	}
	if got := pullRequestCIState(pullRequest{StatusCheckRollup: json.RawMessage("[" + checkRun("COMPLETED", "FAILURE") + "]")}); got != "unknown" {
		t.Fatalf("missing pushed head accepted: %s", got)
	}
}

func TestPullRequestCIPartialRollupCannotProveSuccess(t *testing.T) {
	for _, count := range []int{99, 100, 101} {
		checks := make([]string, count)
		for i := range checks {
			checks[i] = checkRun("COMPLETED", "SUCCESS")
		}
		pr := pullRequest{HeadRefOid: "head", StatusCheckRollup: json.RawMessage("[" + strings.Join(checks, ",") + "]")}
		want := "unknown"
		if count < 100 {
			want = "success"
		}
		if got := pullRequestCIState(pr); got != want {
			t.Errorf("%d contexts = %s, want %s", count, got, want)
		}
		checks[count-1] = statusContext("FAILURE")
		pr.StatusCheckRollup = json.RawMessage("[" + strings.Join(checks, ",") + "]")
		if got := pullRequestCIState(pr); got != "failure" {
			t.Errorf("%d contexts with explicit failure = %s", count, got)
		}
	}
}

func TestPullRequestCIPreservesReviewStateAndTrustedLink(t *testing.T) {
	data := []byte(`[
		{"number":12,"url":"https://untrusted.invalid/","headRefOid":"pushed-head","headRepositoryOwner":{"login":"Team"},"reviewDecision":"APPROVED","statusCheckRollup":[{"__typename":"CheckRun","status":"COMPLETED","conclusion":"FAILURE"}]},
		{"number":13,"headRefOid":"another-head","headRepositoryOwner":{"login":"team"},"isDraft":true,"statusCheckRollup":[{"__typename":"StatusContext","state":"PENDING"}]},
		{"number":14,"headRefOid":"head","headRepositoryOwner":{"login":"team"},"statusCheckRollup":{}},
		{"number":15,"headRefOid":"head","headRepositoryOwner":{"login":"another-owner"},"statusCheckRollup":[{"__typename":"StatusContext","state":"FAILURE"}]}
	]`)
	links := openPullRequestLinks(data, "team/project")
	if len(links) != 3 || links[0].State != "Approved" || links[0].CIState != "failure" || links[0].URL != "https://github.com/team/project/pull/12" ||
		links[1].State != "Draft" || links[1].CIState != "pending" || links[2].CIState != "unknown" {
		t.Fatalf("unexpected links: %+v", links)
	}
	if got := prSummary(data, "team"); !strings.Contains(got, "Approved") || strings.Contains(got, "CI") {
		t.Fatalf("CI overwrote independent review summary: %s", got)
	}
}

func TestRefreshRemoteReadsCIWithExistingPRLookupAndClearsOldResult(t *testing.T) {
	root := t.TempDir()
	log, fixture := filepath.Join(root, "calls"), filepath.Join(root, "open.json")
	t.Setenv("DEVJAY_TEST_CI_CALLS", log)
	t.Setenv("DEVJAY_TEST_CI_FIXTURE", fixture)
	t.Setenv("PATH", root+":"+os.Getenv("PATH"))
	script := `#!/bin/sh
printf '%s\n' "$*" >> "$DEVJAY_TEST_CI_CALLS"
case "$*" in
 *'pr list'*'--state open'*) cat "$DEVJAY_TEST_CI_FIXTURE" ;;
 *) printf '[]' ;;
esac
`
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	cache := remoteCache{}
	for _, stage := range []struct{ conclusion, want string }{{"FAILURE", "failure"}, {"SUCCESS", "success"}} {
		data := `[{"number":12,"headRefOid":"pushed-head","headRepositoryOwner":{"login":"team"},"reviewDecision":"APPROVED","statusCheckRollup":[` + checkRun("COMPLETED", stage.conclusion) + `]}]`
		if err := os.WriteFile(fixture, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		refreshRemote(context.Background(), gitMeta{GitHubRepo: "team/project", Branch: "feature", OID: "unpushed-local-head"}, &cache)
		if len(cache.PullRequests) != 1 || cache.PullRequests[0].CIState != stage.want || cache.PullRequests[0].State != "Approved" {
			t.Fatalf("CI %s refresh = %+v", stage.conclusion, cache.PullRequests)
		}
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(calls), "pr list ") != 2 || strings.Count(string(calls), "headRefOid,statusCheckRollup") != 2 || strings.Contains(string(calls), "pr checks") {
		t.Fatalf("CI did not use only the existing PR lookup: %s", calls)
	}
	// Failed GitHub reads must not leave a known failure/success attached as if
	// it came from this attempt. The existing unavailable state handles the UI.
	if err := os.WriteFile(filepath.Join(root, "gh"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	refreshRemote(context.Background(), gitMeta{GitHubRepo: "team/project", Branch: "feature"}, &cache)
	if len(cache.PullRequests) != 0 || cache.GitHub != "GitHub unavailable" {
		t.Fatalf("failed refresh retained CI state: %+v", cache)
	}
}
