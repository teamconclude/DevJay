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
	GitAction      string      `json:"gitAction,omitempty"`
	appName        string
	hasViewer      bool    // Captured from manager state, never supplied by the frontend.
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
	cardMenuMinimize
	cardMenuHide
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
	if req.Kind == "strip" {
		return req, req.SiteID == "" && req.Number == 0 && req.BundleID == "" && req.BaseBranch == "" &&
			!req.PublishRebased && !req.PublishButton && req.GitAction == "" && req.Anchor == nil &&
			validCoordinate(req.X) && validCoordinate(req.Y)
	}
	return req, req.SiteID != "" && (validViewKind(req.Kind) || req.Kind == "auto" || req.Kind == "site") &&
		req.Number >= 0 && (req.BundleID == "" || (req.Number == 0 && validAppBundleID(req.BundleID))) &&
		(req.BaseBranch == "" || (req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && len(req.BaseBranch) <= 1024 && !strings.ContainsAny(req.BaseBranch, "\x00\r\n"))) &&
		(!req.PublishRebased || (req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && req.BaseBranch == "")) &&
		(!req.PublishButton || req.PublishRebased) &&
		(req.GitAction == "" || ((req.GitAction == "update" || req.GitAction == "push") && req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && req.BaseBranch == "" && !req.PublishRebased)) &&
		validCoordinate(req.X) && validCoordinate(req.Y) && req.Anchor.valid()
}

type cardGitOption struct {
	Kind, Branch, Head, Target, Tracking, DisabledReason string
	MergedNumber                                         int
	Commits                                              int
}

func makeCardGitOption(req cardMenuRequest, git GitStatus) *cardGitOption {
	// A metric identifies the intended operation, never its eligibility.
	if req.GitAction != "" {
		if req.Kind != "branch" || req.Number != 0 || req.BundleID != "" || req.BaseBranch != "" || req.PublishRebased {
			return nil
		}
		return makeCardSyncOption(req.GitAction, git)
	}
	if !req.PublishRebased && req.BaseBranch == "" && req.BundleID == "" &&
		git.MergedPR != nil && git.MergedPR.SameHead && git.MergedPR.Number > 0 &&
		git.MergedPR.Head == git.Head && git.DefaultBranch != "" && git.MergedPR.Base == git.DefaultBranch &&
		!git.IsMainBranch && git.Branch != git.DefaultBranch && (req.Number == 0 || req.Number == git.MergedPR.Number) {
		option := &cardGitOption{Kind: "merged", Branch: git.Branch, Head: git.Head, Target: git.DefaultBranch, Tracking: git.Tracking, MergedNumber: git.MergedPR.Number}
		switch {
		case git.LocalNote != "" || git.Head == "" || git.Branch == "" || git.Branch == "(detached)" || git.Branch == "Detached HEAD":
			option.DisabledReason = "Local branch status is unavailable. Refresh the Site."
		case git.Conflicts > 0:
			option.DisabledReason = "Resolve local conflicts before returning to the default branch."
		case git.Changes > 0:
			option.DisabledReason = "Commit or stash local edits before deleting this branch."
		}
		return option
	}
	// Ordinary card/branch menus offer one next step. Specific status rows keep
	// their own action, and app/PR menus retain their dedicated purpose.
	if req.Number == 0 && req.BundleID == "" && req.BaseBranch == "" && !req.PublishRebased {
		switch {
		case git.CanReplaceRemote:
			return makeCardSyncOption("replace", git)
		case git.Rebased:
			req.Kind, req.PublishRebased = "branch", true
		case git.Behind > 0:
			return makeCardSyncOption("update", git)
		case git.Base != nil && git.Base.Behind > 0 && !git.IsMainBranch:
			rebase := makeCardGitOption(cardMenuRequest{Kind: "branch", BaseBranch: git.Base.Branch}, git)
			push := makeCardSyncOption("push", git)
			if rebase != nil && rebase.DisabledReason != "" && push != nil && push.DisabledReason == "" {
				return push
			}
			return rebase
		default:
			return makeCardSyncOption("push", git)
		}
	}
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

func makeCardSyncOption(kind string, git GitStatus) *cardGitOption {
	option := &cardGitOption{Kind: kind, Branch: git.Branch, Head: git.Head, Tracking: git.Tracking}
	switch kind {
	case "replace":
		if !git.CanReplaceRemote || git.Tracking == "" {
			return nil
		}
		option.Target = git.Tracking
	case "update":
		if git.Behind <= 0 || git.Tracking == "" || git.Rebased || git.CanReplaceRemote {
			return nil
		}
		option.Target = git.Tracking
	case "push":
		if git.Ahead <= 0 || git.Behind != 0 || git.PushTarget == "" || git.Rewritten || git.Rebased || git.CanReplaceRemote {
			return nil
		}
		option.Target, option.Commits = git.PushTarget, git.Ahead
	default:
		return nil
	}
	switch {
	case git.LocalNote != "" || git.Head == "" || git.Branch == "" || git.Branch == "(detached)" || git.Branch == "Detached HEAD":
		option.DisabledReason = "Local branch status is unavailable. Refresh the Site."
	case git.Conflicts > 0:
		option.DisabledReason = "Resolve local conflicts before continuing."
	case kind == "update" && (git.Ahead > 0 || git.Rewritten):
		option.DisabledReason = "Local and remote history differ. Review branch history and update manually."
	case kind == "update" && git.Changes > 0:
		option.DisabledReason = "Commit or stash local edits before updating this branch."
	case kind == "replace" && git.Changes > 0:
		option.DisabledReason = "Commit or stash local edits before replacing this branch."
	}
	return option
}

func (option *cardGitOption) title() string {
	if option == nil {
		return ""
	}
	switch option.Kind {
	case "replace":
		return "Replace from remote…"
	case "update":
		return "Fetch and update " + option.Branch + "…"
	case "push":
		if option.Commits == 1 {
			return "Push 1 commit…"
		}
		return fmt.Sprintf("Push %d commits…", option.Commits)
	case "merged":
		return "Return to " + option.Target + " & delete branch…"
	case "publish":
		return "Push rebased branch…"
	default:
		return "Rebase and push…"
	}
}

func (option *cardGitOption) tooltip() string {
	if option.DisabledReason != "" {
		return option.DisabledReason
	}
	switch option.Kind {
	case "replace":
		return "Fetch " + option.Target + " and verify that the local tip was previously published, then confirm replacement with a recovery copy of the old commits."
	case "push":
		return "Check the remote, then confirm pushing committed work. Local edits stay on your computer."
	case "update":
		return "Fetch the latest commits, then confirm a fast-forward update. Requires a clean checkout and no local-only commits."
	default:
		return "Fetch the latest commits and check for conflicts before a separate confirmation."
	}
}

// A newer remote tip is expected: preparation fetches it. A different local
// branch/commit or default-branch identity requires reopening the menu.
func (option *cardGitOption) request(siteID string, git GitStatus) (ActionRequest, bool) {
	if option == nil || option.DisabledReason != "" {
		return ActionRequest{}, false
	}
	menu := cardMenuRequest{Kind: "branch", BaseBranch: option.Target, PublishRebased: option.Kind == "publish"}
	if option.Kind == "update" || option.Kind == "push" || option.Kind == "replace" {
		menu.BaseBranch, menu.GitAction = "", option.Kind
	}
	if option.Kind == "merged" {
		menu.BaseBranch, menu.Number = "", option.MergedNumber
	}
	current := makeCardGitOption(menu, git)
	if current == nil || current.DisabledReason != "" || current.Kind != option.Kind || current.Branch != option.Branch || current.Head != option.Head || current.Tracking != option.Tracking || current.Target != option.Target || current.MergedNumber != option.MergedNumber {
		return ActionRequest{}, false
	}
	if option.Kind == "merged" {
		return ActionRequest{SiteID: siteID, Kind: "merged", Branch: option.Branch, Head: option.Head, TargetBranch: option.Target, DeleteBranch: true}, true
	}
	if option.Kind == "publish" {
		return ActionRequest{SiteID: siteID, Kind: "publish", Branch: option.Branch, Head: option.Head}, true
	}
	if option.Kind == "update" || option.Kind == "push" || option.Kind == "replace" {
		return ActionRequest{SiteID: siteID, Kind: option.Kind, Branch: option.Branch, Head: option.Head}, true
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
func (m *siteManager) resolveCardMenuRequest(req cardMenuRequest, git GitStatus) cardMenuRequest {
	if req.Kind == "auto" {
		req.Kind = "branch"
		if git.Changes > 0 || git.Conflicts > 0 || git.Branch == "" || git.LocalNote != "" {
			req.Kind = "edits"
		}
	}
	req.hasViewer = m.existingViewer(viewerRequest{SiteID: req.SiteID, Kind: req.Kind}) != nil
	return req
}

func cardMenuLabels(req cardMenuRequest) [4]string {
	if req.Kind == "strip" {
		return [4]string{"", "", "DevJay settings…", "Quit DevJay"}
	}
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
	second := ""
	if req.hasViewer {
		second = "View " + subject + " in new window"
	}
	return [4]string{"View " + subject, second, "DevJay settings…", "Quit DevJay"}
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
		return action == cardMenuView || req.hasViewer
	}
	current, ok := requestedPR(status, req.Number)
	return ok && current.URL == originalPR.URL
}
