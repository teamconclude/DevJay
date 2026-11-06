package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PR metadata stays behind the existing working-tree review. Inline locations
// are mapped to this immutable pushed diff before a draft can be submitted.
type PRReviewLine struct {
	Kind string `json:"kind"`
	Text string `json:"text"`
	Old  int    `json:"old"`
	New  int    `json:"new"`
	Hunk int    `json:"hunk"`
}
type PRReviewFile struct {
	Path    string         `json:"path"`
	OldPath string         `json:"oldPath,omitempty"`
	Status  string         `json:"status"`
	Added   int            `json:"added"`
	Removed int            `json:"removed"`
	Lines   []PRReviewLine `json:"lines"`
	Note    string         `json:"note,omitempty"`
}
type PRReviewSnapshot struct {
	Token   string           `json:"token"`
	Repo    string           `json:"repo"`
	Number  int              `json:"number"`
	URL     string           `json:"url"`
	Title   string           `json:"title"`
	Head    string           `json:"head"`
	Base    string           `json:"base"`
	Branch  string           `json:"branch"`
	Author  string           `json:"author"`
	Viewer  string           `json:"viewer"`
	Files   []PRReviewFile   `json:"files"`
	Note    string           `json:"note,omitempty"`
	Pending *PRPendingReview `json:"pending"`
}
type PRReviewDraftComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"`
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}
type PRReviewSubmission struct {
	ReviewID       int64                  `json:"reviewID,omitempty"`
	PendingVersion string                 `json:"pendingVersion,omitempty"`
	Token          string                 `json:"token"`
	RequestID      string                 `json:"requestID"`
	Event          string                 `json:"event"`
	Body           string                 `json:"body"`
	Comments       []PRReviewDraftComment `json:"comments"`
}

type PRReviewAnchor struct {
	ID        uint64 `json:"id"`
	Path      string `json:"path"`
	Side      string `json:"side"`
	Line      int    `json:"line"`
	StartLine int    `json:"startLine,omitempty"`
	Text      string `json:"text"`
}

// Only native inline locations are sent to the comment API. Other locations
// become references in the editable review summary at the user's request.
type PRReviewLocation struct {
	PRReviewDraftComment
	Summary   bool   `json:"summary,omitempty"`
	Permalink string `json:"permalink,omitempty"`
}

type PRReviewAnchorResult struct {
	ID       uint64            `json:"id"`
	Location *PRReviewLocation `json:"location,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// Map only unchanged lines. Local insertions/replacements have no published
// location, so they can only be referenced with displayed line numbers.
func prReviewMapLine(line int, hunks []BranchReviewHunk) int {
	offset := 0
	for _, h := range hunks {
		if h.NewLines > 0 && line >= h.NewStart && line < h.NewStart+h.NewLines {
			return 0
		}
		after := h.NewStart + h.NewLines
		if h.NewLines == 0 {
			after++
		}
		if line < after {
			break
		}
		offset += h.OldLines - h.NewLines
	}
	return line + offset
}

func resolvePRReviewAnchor(ctx context.Context, root string, s *PRReviewSnapshot, a PRReviewAnchor) (*PRReviewLocation, error) {
	if a.ID == 0 || len(a.Text) > branchReviewFileLimit || !branchReviewSafePath(a.Path) || (a.Side != "LEFT" && a.Side != "RIGHT") {
		return nil, errors.New("Choose a complete text line in the review.")
	}
	lineCount := strings.Count(strings.TrimSuffix(a.Text, "\n"), "\n") + 1
	start := a.StartLine
	if start == 0 {
		start = a.Line
	}
	if start < 1 || a.Line < start || a.Line > lineCount || a.Text == "" {
		return nil, errors.New("Choose a valid line or range.")
	}
	// The displayed location is usable even if this text exists only locally.
	location := &PRReviewLocation{PRReviewDraftComment: PRReviewDraftComment{Path: a.Path, Side: a.Side, Line: a.Line}, Summary: true}
	if start != a.Line {
		location.StartLine, location.StartSide = start, a.Side
	}
	var file *PRReviewFile
	for i := range s.Files {
		if s.Files[i].Path == a.Path {
			file = &s.Files[i]
			break
		}
	}
	revision, path := s.Head, a.Path
	if a.Side == "LEFT" {
		base, err := viewerGit(ctx, root, "merge-base", "--all", s.Base, s.Head)
		if err != nil || len(strings.Fields(string(base))) != 1 {
			return location, nil
		}
		revision = strings.TrimSpace(string(base))
		if file != nil && file.OldPath != "" {
			path = file.OldPath
		}
	}
	text, mode, err := branchReviewBlob(ctx, root, revision, path)
	if err != nil || mode == "" {
		return location, nil
	}
	var hunks []BranchReviewHunk
	if string(text) != a.Text {
		diff, err := branchReviewTextDiff(ctx, root, text, []byte(a.Text), false)
		if err != nil {
			return location, nil
		}
		hunks = branchReviewParseHunks(diff)
	}
	first, last := prReviewMapLine(start, hunks), prReviewMapLine(a.Line, hunks)
	if first < 1 || last < first || last-first != a.Line-start {
		return location, nil
	}
	for line := start; line <= a.Line; line++ {
		if prReviewMapLine(line, hunks) != first+line-start {
			return location, nil
		}
	}
	c := PRReviewDraftComment{Path: a.Path, Side: a.Side, Line: last, Body: "location check"}
	if first != last {
		c.StartLine, c.StartSide = first, a.Side
	}
	err = validatePRReviewSubmission(s, PRReviewSubmission{Token: s.Token, RequestID: "location-check-000000", Event: "COMMENT", Comments: []PRReviewDraftComment{c}})
	c.Body = ""
	location.PRReviewDraftComment = c
	if err == nil {
		location.Summary = false
		return location, nil
	}
	// The selected text exists in this commit, though GitHub cannot anchor
	// a thread here. A blob permalink can still identify the entire selection.
	location.Path = path
	parts := strings.Split(path, "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	location.Permalink = fmt.Sprintf("https://github.com/%s/blob/%s/%s#L%d", s.Repo, revision, strings.Join(parts, "/"), first)
	if first != last {
		location.Permalink += fmt.Sprintf("-L%d", last)
	}
	return location, nil
}

type prReviewMetadata struct {
	Number       int    `json:"number"`
	Title        string `json:"title"`
	State        string `json:"state"`
	ChangedFiles int    `json:"changed_files"`
	User         struct {
		Login string `json:"login"`
	} `json:"user"`
	Head struct {
		SHA, Ref string
		Repo     struct {
			FullName string `json:"full_name"`
		}
	} `json:"head"`
	Base struct {
		SHA  string
		Repo struct {
			FullName string `json:"full_name"`
		}
	} `json:"base"`
}
type prReviewReceipt struct {
	ID     int64  `json:"id"`
	NodeID string `json:"node_id"`
	Body   string `json:"body"`
	State  string `json:"state"`
	Commit string `json:"commit_id"`
	User   struct {
		Login string `json:"login"`
	} `json:"user"`
}

var prReviewSlots = make(chan struct{}, 2)
var prReviewSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var prReviewRequestID = regexp.MustCompile(`^[a-zA-Z0-9-]{16,80}$`)
var prReviewHunkHeader = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)

func prReviewRepository(link PullRequestLink) (string, bool) {
	url := strings.TrimPrefix(link.URL, "https://github.com/")
	repo, number, ok := strings.Cut(url, "/pull/")
	verified, valid := pullRequestLink(repo, link.Number, "", false)
	return repo, ok && valid && verified.URL == link.URL && number == strconv.Itoa(link.Number)
}

// All destinations are constructed from an already-validated repository/PR.
// JSON goes through stdin, never command arguments, a shell, or a temp file.
func prReviewAPI(ctx context.Context, endpoint string, input any, result any) error {
	method := "GET"
	if input != nil {
		method = "POST"
	}
	return prReviewAPIMethod(ctx, method, endpoint, input, result)
}

func prReviewAPIMethod(ctx context.Context, method, endpoint string, input any, result any) error {
	args := []string{"api", "--hostname", "github.com", "-H", "Accept: application/vnd.github+json", "-H", "X-GitHub-Api-Version: 2022-11-28", endpoint}
	if method != "GET" {
		args = append(args, "--method", method)
	}
	var body *bytes.Reader
	if input != nil {
		data, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
		args = append(args, "--input", "-")
	}
	var data []byte
	var err error
	if body == nil {
		data, err = runCommandWithSlots(ctx, prReviewSlots, 30*time.Second, "gh", args...)
	} else {
		data, err = runCommandInputOutput(ctx, prReviewSlots, 45*time.Second, nil, body, nil, "gh", args...)
	}
	if err != nil {
		return err
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(data, result); err != nil {
		return errors.New("GitHub returned an unreadable response")
	}
	return nil
}

func prReviewError(err error) string {
	if strings.Contains(err.Error(), "output exceeded limit") {
		return "GitHub's response exceeds the preview limit. Review this PR on github.com."
	}
	var failure *commandFailure
	if errors.As(err, &failure) {
		for _, status := range []string{"401", "403", "404", "422"} {
			if strings.Contains(failure.stderr, "HTTP "+status) {
				switch status {
				case "401":
					return "Sign in to GitHub with gh auth login, then try again."
				case "403":
					return "GitHub denied this request. Check repository permissions and API rate limits."
				case "404":
					return "This pull request is unavailable to your GitHub account."
				case "422":
					return "GitHub rejected the review. Check the comments and any pending review on github.com; your drafts are kept."
				}
			}
		}
	}
	return "Could not reach GitHub. Check your connection and gh authentication, then try again."
}

func prReviewMetadataRead(ctx context.Context, repo string, number int, branch string) (prReviewMetadata, error) {
	var pr prReviewMetadata
	err := prReviewAPI(ctx, fmt.Sprintf("repos/%s/pulls/%d", repo, number), nil, &pr)
	if err != nil {
		return pr, errors.New(prReviewError(err))
	}
	if pr.Number != number || pr.State != "open" || pr.Head.Ref != branch || !strings.EqualFold(pr.Base.Repo.FullName, repo) ||
		!strings.EqualFold(pr.Head.Repo.FullName, repo) || !prReviewSHA.MatchString(pr.Head.SHA) || !prReviewSHA.MatchString(pr.Base.SHA) {
		return pr, errors.New("The open pull request no longer matches this branch. Refresh its PR details first.")
	}
	return pr, nil
}

func loadPRReview(ctx context.Context, link PullRequestLink, branch string) (*PRReviewSnapshot, error) {
	repo, ok := prReviewRepository(link)
	if !ok {
		return nil, errors.New("This pull request has no supported GitHub destination.")
	}
	pr, err := prReviewMetadataRead(ctx, repo, link.Number, branch)
	if err != nil {
		return nil, err
	}
	s := &PRReviewSnapshot{Repo: repo, Number: pr.Number, URL: link.URL, Title: pr.Title, Head: pr.Head.SHA, Base: pr.Base.SHA, Branch: branch, Author: pr.User.Login, Files: []PRReviewFile{}}
	var user struct {
		Login string `json:"login"`
	}
	if err := prReviewAPI(ctx, "user", nil, &user); err != nil {
		return nil, errors.New(prReviewError(err))
	}
	if user.Login == "" {
		return nil, errors.New("GitHub did not identify the signed-in account.")
	}
	s.Viewer = user.Login
	s.Pending, err = loadPendingPRReview(ctx, s)
	if err != nil {
		return nil, err
	}
	totalBytes := 0
	for page := 1; page <= 20; page++ {
		var files []struct {
			Filename             string
			PreviousFilename     string `json:"previous_filename"`
			Status               string
			Additions, Deletions int
			Patch                string
		}
		if err := prReviewAPI(ctx, fmt.Sprintf("repos/%s/pulls/%d/files?per_page=100&page=%d", repo, pr.Number, page), nil, &files); err != nil {
			return nil, errors.New(prReviewError(err))
		}
		for _, f := range files {
			file := PRReviewFile{Path: f.Filename, OldPath: f.PreviousFilename, Status: f.Status, Added: f.Additions, Removed: f.Deletions}
			totalBytes += len(f.Patch)
			if len(f.Patch) > viewerDiffLimit || totalBytes > 16<<20 {
				file.Note = "This diff exceeds the preview limit. View this file on GitHub."
			} else {
				file.Lines = parsePRReviewPatch(f.Patch)
				if len(file.Lines) == 0 {
					file.Note = "GitHub supplied no complete text diff for this file. It may be binary, renamed, or too large."
				}
			}
			s.Files = append(s.Files, file)
		}
		if len(files) < 100 {
			break
		}
	}
	if len(s.Files) < pr.ChangedFiles {
		s.Note = "Only the first 2,000 changed files are shown. Review omitted files on GitHub."
	}
	latest, err := prReviewMetadataRead(ctx, repo, pr.Number, branch)
	if err != nil {
		return nil, err
	}
	if latest.Head.SHA != s.Head || latest.Base.SHA != s.Base {
		return nil, errors.New("The PR changed while loading. Refresh PR details to get a consistent diff.")
	}
	return s, nil
}

func parsePRReviewPatch(patch string) []PRReviewLine {
	if patch == "" {
		return nil
	}
	var rows []PRReviewLine
	old, next, oldLeft, newLeft, hunk := 0, 0, 0, 0, 0
	for _, text := range strings.Split(strings.TrimSuffix(patch, "\n"), "\n") {
		if match := prReviewHunkHeader.FindStringSubmatch(text); match != nil {
			if oldLeft != 0 || newLeft != 0 {
				return nil
			}
			old, _ = strconv.Atoi(match[1])
			next, _ = strconv.Atoi(match[3])
			oldLeft, newLeft = 1, 1
			if match[2] != "" {
				oldLeft, _ = strconv.Atoi(match[2])
			}
			if match[4] != "" {
				newLeft, _ = strconv.Atoi(match[4])
			}
			hunk++
			rows = append(rows, PRReviewLine{Kind: "hunk", Text: text, Hunk: hunk})
			continue
		}
		if strings.HasPrefix(text, "\\ No newline at end of file") {
			continue
		}
		if hunk == 0 || len(text) == 0 {
			return nil
		}
		row := PRReviewLine{Text: text[1:], Hunk: hunk}
		switch text[0] {
		case ' ':
			row.Kind = "context"
			row.Old = old
			row.New = next
			old++
			next++
			oldLeft--
			newLeft--
		case '-':
			row.Kind = "delete"
			row.Old = old
			old++
			oldLeft--
		case '+':
			row.Kind = "add"
			row.New = next
			next++
			newLeft--
		default:
			return nil
		}
		if oldLeft < 0 || newLeft < 0 || (row.Old == 0 && row.New == 0) {
			return nil
		}
		rows = append(rows, row)
	}
	if oldLeft != 0 || newLeft != 0 {
		return nil
	}
	return rows
}

func validatePRReviewSubmission(s *PRReviewSnapshot, req PRReviewSubmission) error {
	if s == nil || req.Token != s.Token || !prReviewRequestID.MatchString(req.RequestID) {
		return errors.New("Refresh PR details before submitting this review.")
	}
	if req.Event != "COMMENT" && req.Event != "APPROVE" && req.Event != "REQUEST_CHANGES" {
		return errors.New("Choose a review action.")
	}
	if req.Event != "COMMENT" && strings.EqualFold(s.Viewer, s.Author) {
		return errors.New("You can comment on your own PR, but cannot approve it or request changes.")
	}
	if len(req.Body) > 60000 || len(req.Comments) > 100 {
		return errors.New("A review is limited to 100 inline comments and a 60,000-byte summary.")
	}
	if (req.Event == "REQUEST_CHANGES" || (req.Event == "COMMENT" && req.ReviewID == 0 && len(req.Comments) == 0)) && strings.TrimSpace(req.Body) == "" {
		return errors.New("Write a review summary before submitting.")
	}
	bytes := len(req.Body)
	for _, c := range req.Comments {
		if strings.TrimSpace(c.Body) == "" || len(c.Body) > 60000 || (c.Side != "LEFT" && c.Side != "RIGHT") || c.Line <= 0 {
			return errors.New("Every inline comment needs text and a valid diff line.")
		}
		start := c.Line
		if c.StartLine != 0 {
			if c.StartSide != c.Side || c.StartLine >= c.Line {
				return errors.New("Comment ranges must stay on one side and run from top to bottom.")
			}
			start = c.StartLine
		} else if c.StartSide != "" {
			return errors.New("Invalid comment range.")
		}
		matched, hunk := 0, 0
		for _, f := range s.Files {
			if f.Path == c.Path {
				for _, row := range f.Lines {
					line := row.New
					if c.Side == "LEFT" {
						line = row.Old
					}
					if line >= start && line <= c.Line && row.Kind != "hunk" {
						if hunk != 0 && hunk != row.Hunk {
							return errors.New("Comment ranges cannot cross a hidden section of the diff.")
						}
						hunk = row.Hunk
						matched++
					}
				}
			}
		}
		if matched != c.Line-start+1 {
			return errors.New("An inline comment points outside the loaded PR diff. Reload and choose its line again.")
		}
		bytes += len(c.Body)
	}
	if bytes > 1<<20 {
		return errors.New("This review exceeds the 1 MiB submission limit.")
	}
	return nil
}

func prReviewMarker(id string) string { return "<!-- devjay-review:" + id + " -->" }

func findPRReviewReceipt(ctx context.Context, s *PRReviewSnapshot, id string) (*prReviewReceipt, error) {
	for page := 1; page <= 20; page++ {
		var reviews []prReviewReceipt
		if err := prReviewAPI(ctx, fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100&page=%d", s.Repo, s.Number, page), nil, &reviews); err != nil {
			return nil, err
		}
		for _, r := range reviews {
			if strings.Contains(r.Body, prReviewMarker(id)) && strings.EqualFold(r.User.Login, s.Viewer) && r.Commit == s.Head && r.State != "PENDING" {
				return &r, nil
			}
		}
		if len(reviews) < 100 {
			return nil, nil
		}
	}
	return nil, errors.New("review history exceeds the verification limit")
}

// No automatic write retries. An ambiguous response can only be reconciled by
// reading the review marker, including after an app restart.
func submitPRReview(ctx context.Context, s *PRReviewSnapshot, req PRReviewSubmission, checkOnly bool) (*prReviewReceipt, bool, error) {
	if err := validatePRReviewSubmission(s, req); err != nil {
		return nil, false, err
	}
	if receipt, err := findPRReviewReceipt(ctx, s, req.RequestID); err != nil {
		return nil, checkOnly, errors.New(prReviewError(err))
	} else if receipt != nil {
		return receipt, false, nil
	}
	if checkOnly {
		return nil, true, errors.New("Submission is not confirmed yet. Check the PR on GitHub before making another submission; your draft is kept.")
	}
	pr, err := prReviewMetadataRead(ctx, s.Repo, s.Number, s.Branch)
	if err != nil {
		return nil, false, err
	}
	if pr.Head.SHA != s.Head || pr.Base.SHA != s.Base {
		return nil, false, errors.New("The PR changed since you loaded it. Your draft is kept. Refresh PR details and recheck comment locations before submitting.")
	}
	var user struct{ Login string }
	if err := prReviewAPI(ctx, "user", nil, &user); err != nil {
		return nil, false, errors.New(prReviewError(err))
	}
	if !strings.EqualFold(user.Login, s.Viewer) {
		return nil, false, errors.New("The signed-in GitHub account changed. Refresh PR details before submitting.")
	}
	pending, err := loadPendingPRReview(ctx, s)
	if err != nil {
		return nil, false, err
	}
	if req.ReviewID > 0 {
		if pending == nil || pending.ID != req.ReviewID {
			return nil, false, errors.New("The pending review was submitted or removed elsewhere. Refresh PR details.")
		}
		if pending.Head != s.Head {
			return nil, false, errors.New("This pending review belongs to an earlier PR revision. Continue it on GitHub.")
		}
		if req.PendingVersion != pending.Version {
			return nil, false, errors.New("The pending review changed elsewhere. Refresh PR details and check its comments before submitting.")
		}
		if len(req.Comments) != 0 {
			return nil, false, errors.New("Save your local comments to the pending review before submitting.")
		}
		if req.Event == "COMMENT" && strings.TrimSpace(req.Body) == "" && len(pending.Comments) == 0 {
			return nil, false, errors.New("Add an inline comment or a review summary before submitting.")
		}
	} else if pending != nil {
		return nil, false, errors.New("You already have a pending review. Refresh PR details to continue it.")
	}
	body := strings.TrimSpace(req.Body)
	if body == "" && req.Event == "COMMENT" {
		body = "Review comments"
	}
	body += "\n\n" + prReviewMarker(req.RequestID)
	payload := struct {
		Commit   string                 `json:"commit_id"`
		Body     string                 `json:"body"`
		Event    string                 `json:"event"`
		Comments []PRReviewDraftComment `json:"comments,omitempty"`
	}{s.Head, body, req.Event, req.Comments}
	var receipt prReviewReceipt
	endpoint := fmt.Sprintf("repos/%s/pulls/%d/reviews", s.Repo, s.Number)
	var request any = payload
	if pending != nil {
		endpoint += fmt.Sprintf("/%d/events", pending.ID)
		request = map[string]string{"body": body, "event": req.Event}
	}
	if err := prReviewAPI(ctx, endpoint, request, &receipt); err != nil {
		var failure *commandFailure
		if errors.As(err, &failure) && (strings.Contains(failure.stderr, "HTTP 401") || strings.Contains(failure.stderr, "HTTP 403") || strings.Contains(failure.stderr, "HTTP 404") || strings.Contains(failure.stderr, "HTTP 422")) {
			return nil, false, errors.New(prReviewError(err))
		}
		return nil, true, errors.New("GitHub may have received the review, but confirmation was lost. Use Check submission before trying again.")
	}
	if receipt.ID <= 0 || receipt.Commit != s.Head || receipt.State == "PENDING" {
		return nil, true, errors.New("GitHub returned an unexpected submission result. Use Check submission to verify it.")
	}
	return &receipt, false, nil
}
