package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

type PRPendingComment struct {
	PRReviewDraftComment
	ID        int64  `json:"id"`
	NodeID    string `json:"nodeID"`
	RequestID string `json:"requestID,omitempty"`
	Outdated  bool   `json:"outdated,omitempty"`
}
type PRPendingReview struct {
	ID       int64              `json:"id"`
	NodeID   string             `json:"nodeID"`
	Head     string             `json:"head"`
	Body     string             `json:"body"`
	Comments []PRPendingComment `json:"comments"`
	Version  string             `json:"version"`
}
type PRCommentRequest struct {
	Token        string               `json:"token"`
	RequestID    string               `json:"requestID"`
	Action       string               `json:"action"`
	ReviewID     int64                `json:"reviewID"`
	CommentID    int64                `json:"commentID,omitempty"`
	OriginalBody string               `json:"originalBody,omitempty"`
	Comment      PRReviewDraftComment `json:"comment"`
}
type PRCommentResult struct {
	RequestID string `json:"requestID"`
	Done      bool   `json:"done"`
	Unknown   bool   `json:"unknown"`
	Error     string `json:"error,omitempty"`
}

var prCommentMarkerPattern = regexp.MustCompile(`\n*<!-- devjay-comment:([a-zA-Z0-9-]{16,80}) -->\s*$`)

func prCommentBody(body string) (string, string) {
	match := prCommentMarkerPattern.FindStringSubmatch(body)
	if match == nil {
		return body, ""
	}
	return prCommentMarkerPattern.ReplaceAllString(body, ""), match[1]
}
func loadPendingPRReview(ctx context.Context, s *PRReviewSnapshot) (*PRPendingReview, error) {
	var pending *prReviewReceipt
	for page := 1; page <= 20; page++ {
		var reviews []prReviewReceipt
		if err := prReviewAPI(ctx, fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100&page=%d", s.Repo, s.Number, page), nil, &reviews); err != nil {
			return nil, errors.New(prReviewError(err))
		}
		for i := range reviews {
			r := reviews[i]
			if r.State == "PENDING" && strings.EqualFold(r.User.Login, s.Viewer) {
				if pending != nil {
					return nil, errors.New("More than one pending review was returned. Check your reviews on GitHub.")
				}
				if r.ID <= 0 || r.NodeID == "" {
					return nil, errors.New("GitHub returned an incomplete pending review.")
				}
				pending = &r
			}
		}
		if len(reviews) < 100 {
			break
		}
		if page == 20 {
			return nil, errors.New("Review history exceeds the verification limit.")
		}
	}
	if pending == nil {
		return nil, nil
	}
	p := &PRPendingReview{ID: pending.ID, NodeID: pending.NodeID, Head: pending.Commit, Body: pending.Body, Comments: []PRPendingComment{}}
	for page := 1; page <= 10; page++ {
		var comments []struct {
			PRReviewDraftComment
			ID           int64  `json:"id"`
			NodeID       string `json:"node_id"`
			OriginalLine int    `json:"original_line"`
		}
		if err := prReviewAPI(ctx, fmt.Sprintf("repos/%s/pulls/%d/reviews/%d/comments?per_page=100&page=%d", s.Repo, s.Number, p.ID, page), nil, &comments); err != nil {
			return nil, errors.New(prReviewError(err))
		}
		for _, c := range comments {
			body, request := prCommentBody(c.Body)
			c.Body = body
			outdated := c.Line == 0
			if outdated {
				c.Line = c.OriginalLine
			}
			p.Comments = append(p.Comments, PRPendingComment{PRReviewDraftComment: c.PRReviewDraftComment, ID: c.ID, NodeID: c.NodeID, RequestID: request, Outdated: outdated})
		}
		if len(comments) < 100 {
			data, _ := json.Marshal(p)
			p.Version = fmt.Sprintf("%x", sha256.Sum256(data))
			return p, nil
		}
	}
	return nil, errors.New("The pending review exceeds the 1,000-comment limit. Continue it on GitHub.")
}

func verifyPRReviewAccount(ctx context.Context, s *PRReviewSnapshot) error {
	var user struct{ Login string }
	if err := prReviewAPI(ctx, "user", nil, &user); err != nil {
		return errors.New(prReviewError(err))
	}
	if !strings.EqualFold(user.Login, s.Viewer) {
		return errors.New("The signed-in GitHub account changed. Refresh PR details before continuing.")
	}
	return nil
}
func verifyPRReviewRevision(ctx context.Context, s *PRReviewSnapshot) error {
	pr, err := prReviewMetadataRead(ctx, s.Repo, s.Number, s.Branch)
	if err != nil {
		return err
	}
	if pr.Head.SHA != s.Head || pr.Base.SHA != s.Base {
		return errors.New("The PR changed. Refresh PR details and recheck comment locations before continuing.")
	}
	return nil
}
func prReviewWriteFailure(err error) (bool, error) {
	var failure *commandFailure
	if errors.As(err, &failure) {
		for _, status := range []string{"401", "403", "404", "422"} {
			if strings.Contains(failure.stderr, "HTTP "+status) {
				return false, errors.New(prReviewError(err))
			}
		}
	}
	return true, errors.New("GitHub may have saved this change, but confirmation was lost. Check its status before retrying.")
}

// Reads never create a review. The first explicit Add creates a pending review
// with its inline comment; later Adds append to the viewer's existing review.
func changePendingPRComment(ctx context.Context, s *PRReviewSnapshot, req PRCommentRequest, checkOnly bool) (*PRPendingReview, bool, error) {
	if req.Token != s.Token || !prReviewRequestID.MatchString(req.RequestID) || (req.Action != "add" && req.Action != "update" && req.Action != "delete") {
		return nil, false, errors.New("Refresh PR details before changing this comment.")
	}
	if req.Action == "add" {
		err := validatePRReviewSubmission(s, PRReviewSubmission{Token: s.Token, RequestID: req.RequestID, Event: "COMMENT", Comments: []PRReviewDraftComment{req.Comment}})
		if err != nil {
			return nil, false, err
		}
	} else if req.ReviewID <= 0 || req.CommentID <= 0 {
		return nil, false, errors.New("Select a comment in your pending review.")
	}
	if req.Action == "update" && (strings.TrimSpace(req.Comment.Body) == "" || len(req.Comment.Body) > 60000) {
		return nil, false, errors.New("Write a comment of at most 60,000 bytes.")
	}
	if err := verifyPRReviewAccount(ctx, s); err != nil {
		return nil, checkOnly, err
	}
	p, err := loadPendingPRReview(ctx, s)
	if err != nil {
		return nil, checkOnly, err
	}
	if p != nil {
		for _, c := range p.Comments {
			if c.RequestID == req.RequestID {
				return p, false, nil
			}
		}
	}
	if req.ReviewID > 0 && (p == nil || p.ID != req.ReviewID) {
		return p, checkOnly, errors.New("That pending review was submitted or removed elsewhere. Check the review on GitHub before continuing.")
	}
	var previous *PRPendingComment
	if p != nil {
		for i := range p.Comments {
			if p.Comments[i].ID == req.CommentID {
				previous = &p.Comments[i]
				break
			}
		}
	}
	if req.Action == "delete" && previous == nil {
		return p, false, nil
	}
	if checkOnly {
		return p, true, errors.New("This comment change is not confirmed. Check your pending review on GitHub before retrying.")
	}
	if req.Action != "add" && (previous == nil || previous.Body != req.OriginalBody) {
		return p, false, errors.New("This comment changed elsewhere. Refresh the pending review before editing it.")
	}
	if err := verifyPRReviewRevision(ctx, s); err != nil {
		return p, false, err
	}
	if p != nil && p.Head != s.Head {
		return p, false, errors.New("Your pending review belongs to an earlier PR revision. Continue that review on GitHub before adding comments here.")
	}
	body := req.Comment.Body + "\n\n<!-- devjay-comment:" + req.RequestID + " -->"
	switch req.Action {
	case "add":
		if p == nil {
			c := req.Comment
			c.Body = body
			payload := struct {
				Commit   string                 `json:"commit_id"`
				Comments []PRReviewDraftComment `json:"comments"`
			}{s.Head, []PRReviewDraftComment{c}}
			var result prReviewReceipt
			err = prReviewAPI(ctx, fmt.Sprintf("repos/%s/pulls/%d/reviews", s.Repo, s.Number), payload, &result)
			if err == nil && (result.ID <= 0 || result.State != "PENDING" || result.Commit != s.Head) {
				return p, true, errors.New("GitHub returned an unexpected draft-review result. Check the comment before retrying.")
			}
		} else {
			input := map[string]any{"pullRequestReviewId": p.NodeID, "body": body, "path": req.Comment.Path, "line": req.Comment.Line, "side": req.Comment.Side, "clientMutationId": req.RequestID}
			if req.Comment.StartLine > 0 {
				input["startLine"] = req.Comment.StartLine
				input["startSide"] = req.Comment.StartSide
			}
			payload := map[string]any{"query": "mutation($input:AddPullRequestReviewThreadInput!){addPullRequestReviewThread(input:$input){thread{id}}}", "variables": map[string]any{"input": input}}
			var result struct {
				Data struct {
					AddPullRequestReviewThread struct{ Thread struct{ ID string } }
				}
				Errors []any
			}
			err = prReviewAPI(ctx, "graphql", payload, &result)
			if err == nil && (len(result.Errors) > 0 || result.Data.AddPullRequestReviewThread.Thread.ID == "") {
				return p, true, errors.New("GitHub did not confirm the inline comment. Check its status before retrying.")
			}
		}
	case "update":
		err = prReviewAPIMethod(ctx, "PATCH", fmt.Sprintf("repos/%s/pulls/comments/%d", s.Repo, req.CommentID), map[string]string{"body": body}, nil)
	case "delete":
		err = prReviewAPIMethod(ctx, "DELETE", fmt.Sprintf("repos/%s/pulls/comments/%d", s.Repo, req.CommentID), nil, nil)
	}
	if err != nil {
		unknown, failure := prReviewWriteFailure(err)
		return p, unknown, failure
	}
	// Read back the saved pending review. A lost confirmation is never retried
	// as a write; persisted request identity supports explicit reconciliation.
	return changePendingPRComment(ctx, s, req, true)
}
