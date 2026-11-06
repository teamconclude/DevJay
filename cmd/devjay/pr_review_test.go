package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const reviewTestHead = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const reviewTestBase = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
const reviewTestRequest = "12345678-1234-1234-1234-123456789012"
const reviewTestPatch = "@@ -1,3 +1,3 @@\n before\n-old\n+new\n after\n@@ -10 +10 @@\n-last\n+final"

func testPRReviewSnapshot() *PRReviewSnapshot {
	return &PRReviewSnapshot{Token: "token", Repo: "team/project", Number: 12, URL: "https://github.com/team/project/pull/12", Branch: "feature", Head: reviewTestHead, Base: reviewTestBase, Viewer: "reviewer", Author: "author", Files: []PRReviewFile{{Path: "new.go", OldPath: "old.go", Lines: parsePRReviewPatch(reviewTestPatch)}}}
}
func testPRReviewSubmission() PRReviewSubmission {
	return PRReviewSubmission{Token: "token", RequestID: reviewTestRequest, Event: "COMMENT", Comments: []PRReviewDraftComment{{Path: "new.go", Side: "RIGHT", Line: 2, Body: "Consider simplifying this."}}}
}

func TestPRReviewPatchAndLineValidation(t *testing.T) {
	s := testPRReviewSnapshot()
	if len(s.Files[0].Lines) != 8 {
		t.Fatalf("wrong patch rows: %+v", s.Files[0].Lines)
	}
	for _, patch := range []string{"", "@@ -1,3 +1,3 @@\n old", "@@ -1 +1 @@\n-old\n+new\n+extra", "truncated", "@@ -1 +1 @@\n?unknown"} {
		if parsePRReviewPatch(patch) != nil {
			t.Fatalf("accepted incomplete patch %q", patch)
		}
	}
	if rows := parsePRReviewPatch("@@ -0,0 +1 @@\n+new\n\\ No newline at end of file"); len(rows) != 2 || rows[1].Old != 0 || rows[1].New != 1 {
		t.Fatal(rows)
	}
	if rows := parsePRReviewPatch("@@ -1 +0,0 @@\n-old"); len(rows) != 2 || rows[1].Old != 1 || rows[1].New != 0 {
		t.Fatal(rows)
	}
	for _, side := range []string{"LEFT", "RIGHT"} {
		req := testPRReviewSubmission()
		req.Comments[0].Side = side
		req.Comments[0].StartSide = side
		req.Comments[0].StartLine = 1
		req.Comments[0].Line = 3
		if err := validatePRReviewSubmission(s, req); err != nil {
			t.Fatal(err)
		}
	}
	for _, change := range []string{"old-path", "outside", "gap", "cross-side", "backward", "empty", "wrong-side", "token", "event", "request-id", "summary", "own-approval", "too-many"} {
		t.Run(change, func(t *testing.T) {
			s := testPRReviewSnapshot()
			req := testPRReviewSubmission()
			c := &req.Comments[0]
			switch change {
			case "old-path":
				c.Path = "old.go"
			case "outside":
				c.Line = 50
			case "gap":
				c.StartLine = 2
				c.StartSide = "RIGHT"
				c.Line = 10
			case "cross-side":
				c.StartLine = 1
				c.StartSide = "LEFT"
			case "backward":
				c.StartLine = 3
				c.StartSide = "RIGHT"
			case "empty":
				c.Body = " \n"
			case "wrong-side":
				c.Side = "OTHER"
			case "token":
				req.Token = "stale"
			case "event":
				req.Event = "MERGE"
			case "request-id":
				req.RequestID = "</script>"
			case "summary":
				req.Event = "REQUEST_CHANGES"
			case "own-approval":
				s.Viewer = s.Author
				req.Event = "APPROVE"
			case "too-many":
				req.Comments = make([]PRReviewDraftComment, 101)
			}
			if validatePRReviewSubmission(s, req) == nil {
				t.Fatal("accepted invalid review")
			}
		})
	}
}

func fakePRReviewGitHub(t *testing.T) (string, func(string, any)) {
	t.Helper()
	dir := t.TempDir()
	write := func(name string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
dir=$(dirname "$0")
method=GET
endpoint=
for arg in "$@"; do
 case "$arg" in
 POST) method=POST ;;
 repos/*|user) endpoint=$arg ;;
 esac
done
printf '%s %s\n' "$method" "$endpoint" >> "$dir/calls"
if test "$method" = POST; then
 cat > "$dir/submission.json"
 if test -f "$dir/deny"; then printf 'gh: Validation Failed (HTTP 422)\n' >&2; exit 1; fi
 if test -f "$dir/disconnect"; then exit 1; fi
 cat "$dir/receipt.json"; exit 0
fi
case "$endpoint" in
 user) cat "$dir/user.json" ;;
 */files\?*page=1) cat "$dir/files.json" ;;
 */files\?*) printf '[]' ;;
 */comments\?*) cat "$dir/comments.json" ;;
 */reviews\?*) cat "$dir/reviews.json" ;;
 repos/team/project/pulls/12)
 if test -f "$dir/seen-pr" && test -f "$dir/pr-after.json"; then cat "$dir/pr-after.json"; else touch "$dir/seen-pr"; cat "$dir/pr.json"; fi ;;
 *) printf 'unexpected endpoint %s\n' "$endpoint" >&2; exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	write("pr.json", map[string]any{"number": 12, "title": "A useful change", "state": "open", "changed_files": 1, "user": map[string]string{"login": "author"}, "head": map[string]any{"sha": reviewTestHead, "ref": "feature", "repo": map[string]string{"full_name": "team/project"}}, "base": map[string]any{"sha": reviewTestBase, "repo": map[string]string{"full_name": "team/project"}}})
	write("user.json", map[string]string{"login": "reviewer"})
	write("files.json", []map[string]any{{"filename": "new.go", "previous_filename": "old.go", "status": "renamed", "patch": reviewTestPatch, "additions": 2, "deletions": 2}})
	write("comments.json", []map[string]any{{"id": 4, "path": "new.go", "line": 2, "side": "RIGHT", "body": "Existing discussion", "commit_id": reviewTestHead, "user": map[string]string{"login": "someone"}}})
	write("reviews.json", []any{})
	write("receipt.json", map[string]any{"id": 44, "state": "COMMENTED", "commit_id": reviewTestHead, "body": prReviewMarker(reviewTestRequest), "user": map[string]string{"login": "reviewer"}})
	return dir, write
}

func TestPRReviewLoadsPushedDiffAndChecksIdentity(t *testing.T) {
	_, write := fakePRReviewGitHub(t)
	link := PullRequestLink{Number: 12, URL: "https://github.com/team/project/pull/12"}
	s, err := loadPRReview(context.Background(), link, "feature")
	if err != nil || s.Head != reviewTestHead || s.Viewer != "reviewer" || len(s.Files) != 1 || s.Files[0].OldPath != "old.go" {
		t.Fatalf("%+v %v", s, err)
	}
	link.URL = "https://untrusted.example/team/project/pull/12"
	if _, err := loadPRReview(context.Background(), link, "feature"); err == nil {
		t.Fatal("accepted unrelated destination")
	}
	link.URL = "https://github.com/team/project/pull/12"
	if _, err := loadPRReview(context.Background(), link, "another-branch"); err == nil {
		t.Fatal("accepted wrong branch")
	}
	write("pr-after.json", map[string]any{"number": 12, "state": "closed"})
	if _, err := loadPRReview(context.Background(), link, "feature"); err == nil {
		t.Fatal("accepted closed PR")
	}
}

func TestPRReviewSubmissionIsPinnedAndUsesJSONStdin(t *testing.T) {
	dir, _ := fakePRReviewGitHub(t)
	s, req := testPRReviewSnapshot(), testPRReviewSubmission()
	req.Body = "Summary with `quotes`, $HOME and \nUnicode: ø"
	req.Comments[0].Body = "<script>literal</script>\n```go\nvalue()\n```"
	r, unknown, err := submitPRReview(context.Background(), s, req, false)
	if err != nil || unknown || r.ID != 44 {
		t.Fatal(r, unknown, err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "submission.json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Commit   string `json:"commit_id"`
		Body     string
		Event    string
		Comments []PRReviewDraftComment
	}
	if json.Unmarshal(data, &payload) != nil || payload.Commit != reviewTestHead || payload.Event != "COMMENT" || payload.Comments[0].Body != req.Comments[0].Body || !strings.Contains(payload.Body, req.Body) || !strings.Contains(payload.Body, prReviewMarker(req.RequestID)) {
		t.Fatalf("wrong payload: %s", data)
	}
	if strings.Contains(string(data), "token") || strings.Contains(string(data), "requestID") {
		t.Fatal("sent internal fields to GitHub")
	}
}

func TestPRReviewChangedHeadAccountAndDuplicateCannotPublish(t *testing.T) {
	for _, kind := range []string{"head", "base", "account", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			dir, write := fakePRReviewGitHub(t)
			s, req := testPRReviewSnapshot(), testPRReviewSubmission()
			switch kind {
			case "head":
				s.Head = strings.Repeat("c", 40)
			case "base":
				s.Base = strings.Repeat("c", 40)
			case "account":
				write("user.json", map[string]string{"login": "another-account"})
			case "duplicate":
				write("reviews.json", []map[string]any{{"id": 44, "state": "COMMENTED", "commit_id": reviewTestHead, "body": prReviewMarker(req.RequestID), "user": map[string]string{"login": "reviewer"}}})
			}
			r, _, err := submitPRReview(context.Background(), s, req, false)
			if kind == "duplicate" {
				if err != nil || r == nil || r.ID != 44 {
					t.Fatal(r, err)
				}
			} else if err == nil {
				t.Fatal("accepted stale identity")
			}
			if _, err := os.Stat(filepath.Join(dir, "submission.json")); !os.IsNotExist(err) {
				t.Fatal("unexpected POST")
			}
		})
	}
}

func TestPRReviewAmbiguousSubmissionOnlyChecksAndKeepsDraft(t *testing.T) {
	dir, write := fakePRReviewGitHub(t)
	s, req := testPRReviewSnapshot(), testPRReviewSubmission()
	if err := os.WriteFile(filepath.Join(dir, "disconnect"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, unknown, err := submitPRReview(context.Background(), s, req, false)
	if err == nil || !unknown {
		t.Fatal(unknown, err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, "calls"))
	_, unknown, err = submitPRReview(context.Background(), s, req, true)
	if err == nil || !unknown {
		t.Fatal(unknown, err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if strings.Count(string(before), "POST") != strings.Count(string(after), "POST") {
		t.Fatal("check retried an ambiguous POST")
	}
	write("reviews.json", []map[string]any{{"id": 44, "state": "COMMENTED", "commit_id": reviewTestHead, "body": prReviewMarker(req.RequestID), "user": map[string]string{"login": "reviewer"}}})
	r, unknown, err := submitPRReview(context.Background(), s, req, true)
	if err != nil || unknown || r.ID != 44 {
		t.Fatal(r, unknown, err)
	}
}

func TestPRReviewRejectedSubmissionIsSafeToRetryExplicitly(t *testing.T) {
	dir, _ := fakePRReviewGitHub(t)
	if err := os.WriteFile(filepath.Join(dir, "deny"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, unknown, err := submitPRReview(context.Background(), testPRReviewSnapshot(), testPRReviewSubmission(), false)
	if err == nil || unknown || !strings.Contains(err.Error(), "pending review") {
		t.Fatal(unknown, err)
	}
}

func TestPRReviewWindowRoutingAndCloseGuard(t *testing.T) {
	viewer := &gitViewerWindow{state: &GitView{Kind: "review", SiteID: "site", Path: "/repo", Branch: "feature"}}
	m := &siteManager{viewers: map[string]*gitViewerWindow{"viewer": viewer}}
	m.handlePRReviewRequest("missing", viewerRequest{Operation: "pr-review-load", PRLoadID: 1})
	m.handlePRReviewRequest("viewer", viewerRequest{Operation: "pr-review-load", PRLoadID: 1, PRNumber: 12})
	if viewer.prReview == nil || viewer.prReview.state.Error == "" || viewer.prReview.state.Loading {
		t.Fatal("unavailable Site not rejected")
	}
	viewer.prReview.state.Submitting = true
	m.requestCloseViewer("viewer")
	m.closeViewer("viewer")
	if m.viewers["viewer"] != viewer || !strings.Contains(viewer.state.Error, "submission") {
		t.Fatal("closed active submission")
	}
	viewer.prReview.state.Submitting = false
	viewer.prReview.state.Snapshot = testPRReviewSnapshot()
	m.handlePRReviewRequest("viewer", viewerRequest{Operation: "pr-review-draft", Token: "token", Editing: true})
	if !m.lifecycle.hasDrafts() || !viewer.prReview.draftGuard {
		t.Fatal("lost unpersisted draft guard")
	}
	m.requestCloseViewer("viewer")
	m.closeViewer("viewer")
	if m.viewers["viewer"] != viewer {
		t.Fatal("closed unpersisted PR draft")
	}
	m.handlePRReviewRequest("viewer", viewerRequest{Operation: "pr-review-draft", Token: "wrong", Editing: false})
	if !m.lifecycle.hasDrafts() {
		t.Fatal("unrelated token released draft guard")
	}
	m.handlePRReviewRequest("viewer", viewerRequest{Operation: "pr-review-draft", Token: "token", Editing: false})
	if m.lifecycle.hasDrafts() || viewer.prReview.draftGuard {
		t.Fatal("persisted draft retained quit guard")
	}
}

func TestPRReviewMapsWorkingLinesWithoutPublishingLocalEdits(t *testing.T) {
	ctx := context.Background()
	repo := branchReviewFixture(t)
	base := fixtureGit(t, repo, "rev-parse", "HEAD")
	original := "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"
	pushed := strings.Replace(original, "hello", "pushed", 1)
	writeFixture(t, repo, "app.go", pushed)
	fixtureGit(t, repo, "mv", "rename.txt", "renamed.txt")
	fixtureGit(t, repo, "rm", "deleted.txt")
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Published changes")
	head := fixtureGit(t, repo, "rev-parse", "HEAD")
	patch := fixtureGit(t, repo, "diff", "--no-ext-diff", "--unified=3", base, head, "--", "app.go")
	patch = patch[strings.Index(patch, "@@ "):]
	s := testPRReviewSnapshot()
	s.Head, s.Base = head, base
	s.Files = []PRReviewFile{{Path: "app.go", Lines: parsePRReviewPatch(patch)}, {Path: "deleted.txt", Lines: parsePRReviewPatch("@@ -1 +0,0 @@\n-remove this")}}
	check := func(text, side string, line, start, want int, valid bool) {
		t.Helper()
		c, err := resolvePRReviewAnchor(ctx, repo, s, PRReviewAnchor{ID: 1, Path: "app.go", Side: side, Line: line, StartLine: start, Text: text})
		if valid && (err != nil || c.Summary || c.Line != want) {
			t.Fatalf("line %d: %+v %v", line, c, err)
		}
		if !valid && (err != nil || !c.Summary || c.Permalink != "" || c.Line != line) {
			t.Fatalf("unpublished range should use local line numbers: %+v %v", c, err)
		}
	}
	check(pushed, "RIGHT", 4, 0, 4, true)
	check(original, "LEFT", 4, 0, 4, true)
	check("// local header\n"+pushed, "RIGHT", 5, 0, 4, true)
	check("// local header\n"+pushed, "RIGHT", 1, 0, 0, false)
	check(strings.Replace(pushed, "pushed", "local", 1), "RIGHT", 4, 0, 0, false)
	check("// local header\n"+pushed, "RIGHT", 5, 1, 0, false)
	check(strings.Replace(pushed, "\n\n", "\n", 1), "RIGHT", 3, 0, 4, true)
	check(strings.Replace(pushed, "\n\n", "\n", 1), "RIGHT", 3, 1, 0, false)
	c, err := resolvePRReviewAnchor(ctx, repo, s, PRReviewAnchor{ID: 2, Path: "deleted.txt", Side: "LEFT", Line: 1, Text: "remove this\n"})
	if err != nil || c.Line != 1 {
		t.Fatalf("deletion: %+v %v", c, err)
	}
	_, err = resolvePRReviewAnchor(ctx, repo, s, PRReviewAnchor{ID: 3, Path: "../app.go", Side: "RIGHT", Line: 1, Text: pushed})
	if err == nil {
		t.Fatal("accepted outside path")
	}
}

func TestPRReviewSummaryReferencesPinnedLinesOutsidePatch(t *testing.T) {
	ctx := context.Background()
	repo := branchReviewFixture(t)
	const originalName = "old guide #1.txt"
	const renamed = "new guide #1.txt"
	const original = "one\ntwo\nthree\nfour\nfive\nsix\n"
	writeFixture(t, repo, originalName, original)
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Base text")
	base := fixtureGit(t, repo, "rev-parse", "HEAD")
	fixtureGit(t, repo, "mv", originalName, renamed)
	pushed := strings.Replace(original, "one", "edit", 1)
	writeFixture(t, repo, renamed, pushed)
	fixtureGit(t, repo, "add", ".")
	fixtureGit(t, repo, "commit", "-m", "Rename and revise")
	s := testPRReviewSnapshot()
	s.Base, s.Head = base, fixtureGit(t, repo, "rev-parse", "HEAD")
	s.Files = []PRReviewFile{{Path: renamed, OldPath: originalName, Lines: parsePRReviewPatch("@@ -1 +1 @@\n-one\n+edit")}}
	for _, tc := range []struct {
		path, side, text  string
		start, end        int
		wantPath, wantURL string
	}{
		{renamed, "RIGHT", "local\n" + pushed, 5, 7, renamed, s.Head + "/new%20guide%20%231.txt#L4-L6"},
		{renamed, "LEFT", original, 2, 2, originalName, base + "/old%20guide%20%231.txt#L2"},
		{"deleted.txt", "RIGHT", "remove this\n", 1, 1, "deleted.txt", s.Head + "/deleted.txt#L1"},
		{"untracked.go", "RIGHT", "local only\n", 1, 1, "untracked.go", ""},
	} {
		c, err := resolvePRReviewAnchor(ctx, repo, s, PRReviewAnchor{ID: 1, Path: tc.path, Side: tc.side, StartLine: tc.start, Line: tc.end, Text: tc.text})
		want := tc.wantURL
		if want != "" {
			want = "https://github.com/team/project/blob/" + want
		}
		if err != nil || !c.Summary || c.Path != tc.wantPath || c.Permalink != want {
			t.Fatalf("reference for %s %s: %+v %v", tc.path, tc.side, c, err)
		}
	}
}
