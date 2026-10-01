package main

import (
	"fmt"
	"math"
	"strings"
)

type cardMenuRequest struct {
	Anchor         *cardAnchor `json:"anchor,omitempty"`
	SiteID         string      `json:"siteID"`
	Kind           string      `json:"kind"`
	Number         int         `json:"number"`
	BundleID       string      `json:"bundleID"`
	BaseBranch     string      `json:"baseBranch,omitempty"`
	PublishRebased bool        `json:"publishRebased,omitempty"`
	PublishButton  bool        `json:"publishButton,omitempty"`
	appName        string
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
}

// Coordinates in the sending WebView, checked again against native bounds.
type cardAnchor struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func (a *cardAnchor) valid() bool {
	if a == nil {
		return true
	}
	for _, v := range []float64{a.X, a.Y, a.Width, a.Height} {
		if math.IsNaN(v) || math.IsInf(v, 0) || math.Abs(v) > 32768 {
			return false
		}
	}
	return a.Width > 0 && a.Height > 0
}

type cardMenuAction int

const (
	cardMenuDismissed cardMenuAction = iota
	cardMenuView
	cardMenuViewNew
	cardMenuSettings
	cardMenuQuit
	cardMenuGitAction
)

func normalizeCardMenuRequest(req cardMenuRequest) (cardMenuRequest, bool) {
	if req.Kind == "" {
		req.Kind = "site"
	}
	// The native bridge also checks the actual webview bounds. This outer limit
	// rejects unreasonable or non-finite input before dispatching to AppKit.
	validCoordinate := func(v float64) bool {
		return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 32768
	}
	return req, req.SiteID != "" && (validViewKind(req.Kind) || req.Kind == "auto" || req.Kind == "site") &&
		req.Number >= 0 && (req.BundleID == "" || (req.Number == 0 && validAppBundleID(req.BundleID))) &&
		(req.BaseBranch == "" || (req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && len(req.BaseBranch) <= 1024 && !strings.ContainsAny(req.BaseBranch, "\x00\r\n"))) &&
		(!req.PublishRebased || (req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && req.BaseBranch == "")) &&
		(!req.PublishButton || req.PublishRebased) &&
		validCoordinate(req.X) && validCoordinate(req.Y) && req.Anchor.valid()
}

type cardGitOption struct {
	Kind, Branch, Head, Target, Tracking, DisabledReason string
}

func makeCardGitOption(req cardMenuRequest, git GitStatus) *cardGitOption {
	if req.PublishRebased {
		if req.Kind != "branch" || req.Number != 0 || req.BundleID != "" || req.BaseBranch != "" || !git.Rebased || git.IsMainBranch {
			return nil
		}
		option := &cardGitOption{Kind: "publish", Branch: git.Branch, Head: git.Head, Tracking: git.Tracking}
		switch {
		case git.LocalNote != "" || git.Head == "" || git.Branch == "":
			option.DisabledReason = "Local branch status is unavailable. Refresh the Site."
		case git.Conflicts > 0:
			option.DisabledReason = "Resolve the rebase and push manually with Claude or Astra."
		case git.Changes > 0:
			option.DisabledReason = "Commit or stash local edits before checking a rebased push."
		case git.PushTarget == "" || git.CanReplaceRemote:
			option.DisabledReason = "The remote destination or history needs manual review with Claude or Astra."
		}
		return option
	}
	base := git.Base
	if req.Kind != "branch" || req.Number != 0 || req.BundleID != "" || req.BaseBranch == "" ||
		base == nil || base.Branch != req.BaseBranch || base.Behind <= 0 || base.Tracking == "" || git.IsMainBranch {
		return nil
	}
	option := &cardGitOption{Kind: "rebase-push", Branch: git.Branch, Head: git.Head, Target: base.Branch, Tracking: base.Tracking}
	switch {
	case git.LocalNote != "" || git.Head == "" || git.Branch == "" || git.Branch == "(detached)" || git.Branch == "Detached HEAD":
		option.DisabledReason = "Local branch status is unavailable. Refresh the Site before continuing."
	case git.Conflicts > 0:
		option.DisabledReason = "Resolve the local conflicts before rebasing."
	case git.Changes > 0:
		option.DisabledReason = "Commit or stash local work before rebasing."
	case git.PushTarget == "":
		option.DisabledReason = "This branch needs a matching origin upstream. Rebase and push manually with Claude or Astra."
	case git.Rebased || git.CanReplaceRemote:
		option.DisabledReason = "Review the rewritten branch history in the Changes window first."
	case git.Behind > 0 && git.Tracking != base.Tracking:
		option.DisabledReason = "Update this branch from its own remote before rebasing."
	}
	return option
}

// A newer remote tip is expected: preparation fetches it. A different local
// branch/commit or default-branch identity requires reopening the menu.
func (option *cardGitOption) request(siteID string, git GitStatus) (ActionRequest, bool) {
	if option == nil || option.DisabledReason != "" {
		return ActionRequest{}, false
	}
	current := makeCardGitOption(cardMenuRequest{Kind: "branch", BaseBranch: option.Target, PublishRebased: option.Kind == "publish"}, git)
	if current == nil || current.DisabledReason != "" || current.Branch != option.Branch || current.Head != option.Head || current.Tracking != option.Tracking {
		return ActionRequest{}, false
	}
	if option.Kind == "publish" {
		return ActionRequest{SiteID: siteID, Kind: "publish", Branch: option.Branch, Head: option.Head}, true
	}
	remote, found := strings.CutSuffix(option.Tracking, "/"+option.Target)
	if !found || remote == "" {
		return ActionRequest{}, false
	}
	return ActionRequest{SiteID: siteID, Kind: "rebase-push", Branch: option.Branch, Head: option.Head, TargetBranch: option.Target, TargetRemote: remote}, true
}

func validAppBundleID(bundle string) bool {
	return bundle != "" && len(bundle) <= 255 && !strings.ContainsAny(bundle, "\x00\r\n")
}

// Resolve the heading exception against backend state before showing the menu;
// keep that visible choice fixed if a later Git reading arrives while it tracks.
func resolveCardMenuRequest(req cardMenuRequest, git GitStatus) cardMenuRequest {
	if req.Kind == "auto" {
		req.Kind = "branch"
		if git.Changes > 0 || git.Conflicts > 0 || git.Branch == "" || git.LocalNote != "" {
			req.Kind = "edits"
		}
	}
	return req
}

func cardMenuLabels(req cardMenuRequest) [4]string {
	if req.PublishButton {
		// The guarded Git action becomes the first item for its direct button.
		return [4]string{"", "", "DevJay settings…", "Quit DevJay"}
	}
	if req.Number > 0 {
		return [4]string{fmt.Sprintf("Open PR #%d in browser", req.Number), fmt.Sprintf("Open PR #%d in new browser tab", req.Number), "DevJay settings…", "Quit DevJay"}
	}
	if req.BundleID != "" {
		return [4]string{"Show " + req.appName, "", "DevJay settings…", "Quit DevJay"}
	}
	if req.Kind == "site" {
		return [4]string{"Go to Site desktop", "", "DevJay settings…", "Quit DevJay"}
	}
	subject := map[string]string{"edits": "changes", "branch": "branch history", "local": "local commits", "remote": "remote commits"}[req.Kind]
	return [4]string{"View " + subject, "View " + subject + " in new window", "DevJay settings…", "Quit DevJay"}
}

// Site/window membership is checked separately for every action. App actions
// remain available even when the clicked PR disappears during menu tracking.
func validCardMenuSelection(req cardMenuRequest, action cardMenuAction, originalPR PullRequestLink, status GitStatus) bool {
	if action == cardMenuSettings || action == cardMenuQuit {
		return true
	}
	if action != cardMenuView && action != cardMenuViewNew {
		return false
	}
	if req.BundleID != "" || (req.Number == 0 && req.Kind == "site") {
		return action == cardMenuView
	}
	if req.Number == 0 {
		return true
	}
	current, ok := requestedPR(status, req.Number)
	return ok && current.URL == originalPR.URL
}
