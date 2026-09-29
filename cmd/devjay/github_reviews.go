package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type pullRequest struct {
	Number              int
	IsDraft             bool
	ReviewDecision      string
	HeadRefOid          string
	StatusCheckRollup   json.RawMessage
	HeadRepositoryOwner struct{ Login string }
	TitleEditedAt       time.Time
	BodyEditedAt        time.Time
}

type ReviewFindings struct {
	Number               int    `json:"number"`
	Blockers             int    `json:"blockers"`
	Warnings             int    `json:"warnings"`
	Earlier              bool   `json:"earlier"`
	ChangeNote           string `json:"changeNote,omitempty"`
	Unavailable          bool   `json:"unavailable"`
	FreshnessUnavailable bool   `json:"freshnessUnavailable,omitempty"`
	Detail               string `json:"detail"`
	postedAt             time.Time
}

type reviewPost struct {
	Body        string    `json:"body"`
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
	CreatedAt   time.Time `json:"created_at"`
	URL         string    `json:"html_url"`
	Commit      string    `json:"commit_id"`
}

func (p reviewPost) postedAt() time.Time {
	if !p.SubmittedAt.IsZero() {
		return p.SubmittedAt
	}
	return p.CreatedAt
}

type reviewCounts struct {
	Blockers, Warnings int
	Head               string
}

var reviewMarker = regexp.MustCompile(`(?s)<!--\s*review\s+(.+?)-->`)
var findingHeading = regexp.MustCompile(`(?i)^#{1,6}\s+(blockers|warnings)\s*:?\s*$`)
var findingItem = regexp.MustCompile(`^(?:[-*+] |[0-9]+[.)] )\S`)

// Only explicit report formats are counted. Quoted examples and code fences
// must not turn ordinary discussion into a new review report.
func parseReviewCounts(body string) (reviewCounts, bool) {
	var lines []string
	fence := ""
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			mark := trimmed[:3]
			if fence == "" {
				fence = mark
			} else if fence == mark {
				fence = ""
			}
			continue
		}
		if fence == "" && !strings.HasPrefix(trimmed, ">") {
			lines = append(lines, line)
		}
	}
	body = strings.Join(lines, "\n")
	markers := reviewMarker.FindAllStringSubmatch(body, -1)
	if len(markers) > 0 {
		if len(markers) != 1 {
			return reviewCounts{}, false
		}
		fields := map[string]string{}
		for _, field := range strings.Fields(markers[0][1]) {
			key, value, ok := strings.Cut(field, "=")
			if !ok || fields[key] != "" {
				return reviewCounts{}, false
			}
			fields[key] = value
		}
		c := reviewCounts{Head: fields["head"]}
		for _, key := range []string{"blockers", "warnings", "warnings_preexisting", "notes"} {
			raw, exists := fields[key]
			if !exists && key != "blockers" && key != "warnings" {
				continue
			}
			n, err := strconv.Atoi(raw)
			if err != nil || n < 0 {
				return reviewCounts{}, false
			}
			if key == "blockers" {
				c.Blockers = n
			}
			if key == "warnings" {
				c.Warnings = n
			}
		}
		return c, true
	}
	// Support unmarked Markdown reports with both sections and top-level bullets.
	counts := map[string]int{}
	none := map[string]bool{}
	section := ""
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if match := findingHeading.FindStringSubmatch(trimmed); match != nil {
			section = strings.ToLower(match[1])
			if _, duplicate := counts[section]; duplicate {
				return reviewCounts{}, false
			}
			counts[section] = 0
		} else if strings.HasPrefix(trimmed, "#") {
			section = ""
		} else if section != "" {
			if findingItem.MatchString(line) {
				counts[section]++
			}
			if strings.EqualFold(trimmed, "None.") || strings.EqualFold(trimmed, "None") {
				none[section] = true
			}
		}
	}
	for _, key := range []string{"blockers", "warnings"} {
		n, ok := counts[key]
		if !ok || (n == 0 && !none[key]) || (n > 0 && none[key]) {
			return reviewCounts{}, false
		}
	}
	return reviewCounts{Blockers: counts["blockers"], Warnings: counts["warnings"]}, true
}

// gh emits one JSON array per page. Decode every page before choosing the newest
// report across submitted reviews and PR discussion comments.
func decodeReviewPosts(data []byte) ([]reviewPost, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var result []reviewPost
	for {
		var page []reviewPost
		err := decoder.Decode(&page)
		if err == io.EOF {
			return result, nil
		}
		if err != nil {
			return nil, err
		}
		result = append(result, page...)
	}
}
func latestReviewFindings(posts []reviewPost, pr pullRequest) *ReviewFindings {
	var latest time.Time
	var result *ReviewFindings
	for _, post := range posts {
		if post.State == "PENDING" || post.State == "DISMISSED" || post.postedAt().IsZero() {
			continue
		}
		counts, ok := parseReviewCounts(post.Body)
		if !ok || post.postedAt().Before(latest) {
			continue
		}
		latest = post.postedAt()
		head := counts.Head
		if head == "" {
			head = post.Commit
		} else if post.Commit != "" && sameReviewCommit(head, post.Commit) {
			// GitHub supplies the full ID for submitted reviews. Prefer it over
			// a matching abbreviation in the report, including ambiguous prefixes.
			head = post.Commit
		}
		codeChanged := head != "" && pr.HeadRefOid != "" && !sameReviewCommit(head, pr.HeadRefOid)
		titleChanged, bodyChanged := pr.TitleEditedAt.After(latest), pr.BodyEditedAt.After(latest)
		result = &ReviewFindings{Number: pr.Number, Blockers: counts.Blockers, Warnings: counts.Warnings,
			Earlier: codeChanged || titleChanged || bodyChanged, FreshnessUnavailable: head == "" || pr.HeadRefOid == "", postedAt: latest}
		result.Detail = fmt.Sprintf("Latest reported findings: %d blockers, %d warnings\nPosted %s", counts.Blockers, counts.Warnings, latest.UTC().Format(time.RFC3339))
		if codeChanged {
			result.Detail += "\nReview is for an earlier commit"
		}
		if titleChanged {
			result.Detail += "\nPR title changed since review"
		}
		if bodyChanged {
			result.Detail += "\nPR description changed since review"
		}
		// Name what changed so a PR edit does not imply another push.
		switch {
		case codeChanged && (titleChanged || bodyChanged):
			result.ChangeNote = "Commits and PR details changed since review"
		case codeChanged:
			result.ChangeNote = "Commits changed since review"
		case titleChanged && bodyChanged:
			result.ChangeNote = "PR title and description changed since review"
		case titleChanged:
			result.ChangeNote = "PR title changed since review"
		case bodyChanged:
			result.ChangeNote = "PR description changed since review"
		}
		if head == "" {
			result.Detail += "\nReviewed commit not specified"
		}
		if post.URL != "" {
			result.Detail += "\n" + post.URL
		}
	}
	return result
}

var reviewCommitID = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

func sameReviewCommit(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	if strings.EqualFold(a, b) {
		return true
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	return (len(a) == 40 || len(a) == 64) && reviewCommitID.MatchString(a) && reviewCommitID.MatchString(b) && strings.HasPrefix(strings.ToLower(a), strings.ToLower(b))
}

// Read the current pushed head and the PR's own title/description edits after
// loading reports. General updatedAt timestamps, labels, and conversation
// activity do not indicate a change to the reviewed content.
const reviewChangesQuery = `query($owner:String!,$name:String!,$number:Int!,$since:DateTime!,$endCursor:String) {
  repository(owner:$owner,name:$name) {
    pullRequest(number:$number) {
      headRefOid lastEditedAt
      timelineItems(first:100,after:$endCursor,since:$since,itemTypes:[RENAMED_TITLE_EVENT]) {
        nodes { __typename ... on RenamedTitleEvent { createdAt } }
        pageInfo { hasNextPage endCursor }
      }
    }
  }
}`

func decodeReviewChanges(data []byte, pr pullRequest) (pullRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	pages := 0
	for {
		var page *struct {
			HeadRefOid    string
			LastEditedAt  time.Time
			TimelineItems *struct {
				Nodes []struct {
					Type      string `json:"__typename"`
					CreatedAt time.Time
				}
			}
		}
		err := decoder.Decode(&page)
		if err == io.EOF && pages > 0 {
			return pr, nil
		}
		if err != nil {
			return pr, err
		}
		if page == nil || page.HeadRefOid == "" || page.TimelineItems == nil {
			return pr, errors.New("incomplete pull request change history")
		}
		pages++
		pr.HeadRefOid = page.HeadRefOid
		if page.LastEditedAt.After(pr.BodyEditedAt) {
			pr.BodyEditedAt = page.LastEditedAt
		}
		for _, node := range page.TimelineItems.Nodes {
			if node.Type == "RenamedTitleEvent" && node.CreatedAt.After(pr.TitleEditedAt) {
				pr.TitleEditedAt = node.CreatedAt
			}
		}
	}
}

func fetchReviewFindings(ctx context.Context, repo string, pr pullRequest) *ReviewFindings {
	var posts []reviewPost
	endpoints := []struct{ path, fields string }{
		{fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100", repo, pr.Number), "map({body,state,submitted_at,html_url,commit_id})"},
		{fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", repo, pr.Number), "map({body,created_at,html_url})"},
	}
	for _, endpoint := range endpoints {
		out, err := runCommand(ctx, 15*time.Second, "gh", "api", "--paginate", endpoint.path, "--jq", endpoint.fields)
		if err == nil {
			var page []reviewPost
			page, err = decodeReviewPosts(out)
			posts = append(posts, page...)
		}
		if err != nil {
			return &ReviewFindings{Number: pr.Number, Unavailable: true, Detail: "Review posts unavailable; findings could not be checked"}
		}
	}
	result := latestReviewFindings(posts, pr)
	if result == nil {
		return nil
	}
	owner, name, _ := strings.Cut(repo, "/")
	out, err := runCommand(ctx, 15*time.Second, "gh", "api", "graphql", "--paginate", "-f", "query="+reviewChangesQuery,
		"-f", "owner="+owner, "-f", "name="+name, "-F", "number="+strconv.Itoa(pr.Number),
		"-f", "since="+result.postedAt.UTC().Format(time.RFC3339), "--jq", ".data.repository.pullRequest")
	if err == nil {
		pr, err = decodeReviewChanges(out, pr)
	}
	if err != nil {
		result.FreshnessUnavailable = true
		result.Detail += "\nPR changes could not be checked"
		return result
	}
	return latestReviewFindings(posts, pr)
}
