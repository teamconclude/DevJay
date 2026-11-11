package main

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

type cardMenuRequest struct {
	RequestID      string      `json:"requestID,omitempty"`
	Branch         string      `json:"branch,omitempty"`
	Head           string      `json:"head,omitempty"`
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
	X              float64 `json:"x"`
	Y              float64 `json:"y"`
}

func (req cardMenuRequest) isPRStatus() bool {
	return req.Kind == "review-status" || req.Kind == "pr-status"
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
	cardMenuOpenPRNew
	cardMenuSettings
	cardMenuQuit
	cardMenuGitAction
	cardMenuMinimize
	cardMenuHide
	cardMenuHideSites
	cardMenuShowSites
	cardMenuViewDiffs
	cardMenuOpenPR
	cardMenuShowApp
	cardMenuGoToSite
	cardMenuSwitchBranch
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
	return req, req.SiteID != "" && (validViewKind(req.Kind) || req.Kind == "auto" || req.Kind == "site" || req.isPRStatus() || req.Kind == "return-status" || req.Kind == "sync-status") &&
		(!req.isPRStatus() || req.Number > 0) &&
		((req.Kind != "return-status" && req.Kind != "sync-status") || (req.Number == 0 && req.BundleID == "")) &&
		req.Number >= 0 && (req.BundleID == "" || (req.Number == 0 && validAppBundleID(req.BundleID))) &&
		(req.BaseBranch == "" || (req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && len(req.BaseBranch) <= 1024 && !strings.ContainsAny(req.BaseBranch, "\x00\r\n"))) &&
		(!req.PublishRebased || (req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && req.BaseBranch == "")) &&
		(!req.PublishButton || req.PublishRebased) &&
		(req.GitAction == "" || ((req.GitAction == "update" || req.GitAction == "push") && req.Kind == "branch" && req.Number == 0 && req.BundleID == "" && req.BaseBranch == "" && !req.PublishRebased)) &&
		validCoordinate(req.X) && validCoordinate(req.Y) && req.Anchor.valid()
}

type cardGitOption struct {
	source                                               cardMenuRequest
	approvedPR                                           *approvedBranchPR
	Kind, Branch, Head, Target, Tracking, DisabledReason string
	MergedNumber                                         int
	Commits                                              int
}

// A branch menu has no PR number. Only use an unambiguous approved PR for
// this published tip and default branch. PR status rows identify their own PR.
func approvedCardOption(number int, git GitStatus) *cardGitOption {
	if git.Summary != "Up to date" || git.LocalNote != "" || git.RemoteNote != "" || git.Changes != 0 ||
		git.Conflicts != 0 || git.Ahead != 0 || git.Behind != 0 || git.IsMainBranch || git.Branch == git.DefaultBranch ||
		git.MergedPR != nil || git.Tracking == "" {
		return nil
	}
	var match *approvedBranchPR
	for _, link := range git.PullRequests {
		pr := link.approvedReturn
		if pr == nil || (number != 0 && pr.Number != number) || pr.head != git.Head || pr.base != git.DefaultBranch {
			continue
		}
		if match != nil {
			return nil
		}
		match = pr
	}
	if match == nil {
		return nil
	}
	return &cardGitOption{Kind: "approved", Branch: git.Branch, Head: git.Head, Target: git.DefaultBranch, Tracking: git.Tracking, approvedPR: match}
}

func makeCardGitOption(req cardMenuRequest, git GitStatus) *cardGitOption {
	option := suggestedCardGitOption(req, git)
	if option != nil {
		option.source = req // Repeat the same lookup if the user selects this item.
	}
	return option
}

func suggestedCardGitOption(req cardMenuRequest, git GitStatus) *cardGitOption {
	if req.Kind == "pr-status" {
		if approved := approvedCardOption(req.Number, git); approved != nil {
			return approved
		}
		// Otherwise use the usual numbered PR menu (including merged returns).
	}
	if req.Kind == "review-status" {
		return nil
	}
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
	if req.Kind == "return-status" {
		// A removed remote does not prove a merge. Without that proof, offer the
		// existing confirmed branch switch and retain the local feature branch.
		if (git.Summary != "Remote branch removed" && git.RemoteNote != "Remote branch no longer exists") ||
			git.DefaultBranch == "" || git.IsMainBranch || git.Branch == git.DefaultBranch {
			return nil
		}
		option := &cardGitOption{Kind: "return", Branch: git.Branch, Head: git.Head, Target: git.DefaultBranch, Tracking: git.Tracking}
		switch {
		case git.LocalNote != "" || git.Head == "" || git.Branch == "" || git.Branch == "(detached)" || git.Branch == "Detached HEAD":
			option.DisabledReason = "Local branch status is unavailable. Refresh the Site."
		case git.Conflicts > 0:
			option.DisabledReason = "Resolve local conflicts before returning to the default branch."
		case git.Changes > 0:
			option.DisabledReason = "Commit or stash local edits before returning to the default branch."
		}
		return option
	}
	// Ordinary card/branch menus offer one next step. Specific status rows keep
	// their own action, and app/PR menus retain their dedicated purpose.
	if req.Number == 0 && req.BundleID == "" && req.BaseBranch == "" && !req.PublishRebased {
		if req.Kind == "auto" || req.Kind == "branch" || req.Kind == "sync-status" {
			if option := approvedCardOption(0, git); option != nil {
				return option
			}
		}
		switch {
		case git.CanReplaceRemote:
			return makeCardSyncOption("replace", git)
		case git.Rebased:
			req.Kind, req.PublishRebased = "branch", true
		case git.Behind > 0:
			return makeCardSyncOption("update", git)
		case git.Base != nil && git.Base.Behind > 0 && !git.IsMainBranch:
			rebase := suggestedCardGitOption(cardMenuRequest{Kind: "branch", BaseBranch: git.Base.Branch}, git)
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
	case "approved":
		return "Return to " + option.Target + " & delete local branch…"
	case "merged", "return":
		return "Return to " + option.Target + "…"
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
	case "approved":
		return fmt.Sprintf("Verify approved PR #%d and its remote tip, then confirm local branch cleanup.", option.approvedPR.Number)
	case "merged":
		return "Check the default branch, then confirm returning to it. The confirmation lets you keep or delete the merged local branch."
	case "return":
		return "Check the default branch, then confirm switching to it while keeping this local branch."
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
	current := suggestedCardGitOption(option.source, git)
	if current == nil || current.DisabledReason != "" || current.Kind != option.Kind || current.Branch != option.Branch ||
		current.Head != option.Head || current.Tracking != option.Tracking || current.Target != option.Target || current.MergedNumber != option.MergedNumber {
		return ActionRequest{}, false
	}
	req := ActionRequest{SiteID: siteID, Kind: option.Kind, Branch: option.Branch, Head: option.Head}
	switch option.Kind {
	case "approved", "merged":
		if option.Kind == "approved" && *current.approvedPR != *option.approvedPR {
			return ActionRequest{}, false
		}
		req.TargetBranch, req.DeleteBranch, req.approvedPR = option.Target, true, option.approvedPR
	case "return":
		req.Kind, req.TargetBranch = "switch", option.Target
	case "rebase-push":
		remote, found := strings.CutSuffix(option.Tracking, "/"+option.Target)
		if !found || remote == "" {
			return ActionRequest{}, false
		}
		req.TargetBranch, req.TargetRemote = option.Target, remote
	case "update", "push", "replace", "publish":
		// These operations act on the current branch.
	default:
		return ActionRequest{}, false
	}
	return req, true
}

func validAppBundleID(bundle string) bool {
	return bundle != "" && len(bundle) <= 255 && !strings.ContainsAny(bundle, "\x00\r\n")
}

// View branch chooses a tab from current local status without changing the
// clicked metric's identity, which is also used to prepare suggested Git actions.
func cardMenuViewKind(kind string, git GitStatus) string {
	if kind == "auto" || kind == "branch" || kind == "edits" || kind == "review-status" || kind == "pr-status" || kind == "return-status" || kind == "sync-status" {
		if git.Changes > 0 || git.Conflicts > 0 || git.Branch == "" || git.LocalNote != "" {
			return "edits"
		}
		return "branch"
	}
	return kind
}

func cardMenuOffersDiffs(req cardMenuRequest, git GitStatus) bool {
	if req.Kind == "strip" || req.Kind == "return-status" || (req.Number > 0 && !req.isPRStatus()) || req.BundleID != "" || git.Head == "" || git.LocalNote != "" {
		return false
	}
	if req.isPRStatus() {
		return true
	}
	// Feature branches can differ from their base even when their upstream is
	// in sync. Incoming commits alone on a default branch are an update, not
	// local work to review. Keep Review for edits, outgoing or rewritten history.
	return !git.IsMainBranch || git.Changes > 0 || git.Conflicts > 0 || git.Ahead > 0 || git.Rewritten || git.Rebased || git.CanReplaceRemote ||
		(git.Comparison != nil && git.Comparison.Ahead > 0)
}

type cardMenuItem struct {
	Action   cardMenuAction `json:"action"`
	Title    string         `json:"title,omitempty"`
	Hint     string         `json:"hint,omitempty"`
	Disabled bool           `json:"disabled,omitempty"`
}

// The first content item is also the ordinary-click action. List the items
// in display order here. AppKit only renders them and returns the selected ID.
func cardMenuActions(req cardMenuRequest, git GitStatus, option *cardGitOption) []cardMenuItem {
	if req.Kind == "strip" || (req.PublishButton && option == nil) {
		return nil
	}
	view := cardMenuItem{Action: cardMenuView, Title: "View branch"}
	switchBranch := cardMenuItem{Action: cardMenuSwitchBranch, Title: "Switch branch…", Hint: "Choose another branch, then review a separate confirmation"}
	var review, gitAction cardMenuItem
	if cardMenuOffersDiffs(req, git) {
		review = cardMenuItem{Action: cardMenuViewDiffs, Title: "Review", Hint: "Compare the branch fork point with current files, including uncommitted edits"}
	}
	if option != nil {
		gitAction = cardMenuItem{Action: cardMenuGitAction, Title: option.title(), Hint: option.tooltip(), Disabled: option.DisabledReason != ""}
	}

	var items []cardMenuItem
	var browser, newTab cardMenuItem
	if req.Number > 0 {
		browser = cardMenuItem{Action: cardMenuOpenPR, Title: fmt.Sprintf("Open PR #%d in browser", req.Number)}
		newTab = cardMenuItem{Action: cardMenuOpenPRNew, Title: fmt.Sprintf("Open PR #%d in new browser tab", req.Number)}
	}
	switch {
	case req.PublishButton:
		items = []cardMenuItem{gitAction, review}
	case req.Kind == "sync-status":
		items = []cardMenuItem{view, switchBranch, review, gitAction}
	case req.Kind == "return-status" && git.MergedPR != nil && git.MergedPR.SameHead:
		items = []cardMenuItem{gitAction, view, switchBranch}
	case option != nil && option.Kind == "approved":
		items = []cardMenuItem{view, review, gitAction, browser, newTab}
	case req.Number > 0 && req.Kind == "review-status":
		items = []cardMenuItem{review, view, browser, newTab}
	case req.Number > 0:
		items = []cardMenuItem{browser, gitAction, newTab}
	case req.BundleID != "":
		view = cardMenuItem{Action: cardMenuShowApp, Title: "Show " + req.appName}
		items = []cardMenuItem{view}
	case req.Kind == "site":
		view = cardMenuItem{Action: cardMenuGoToSite, Title: "Go to Site desktop"}
		items = []cardMenuItem{view, gitAction, review}
	case req.Kind == "auto" || req.Kind == "edits":
		items = []cardMenuItem{view, gitAction, review}
	default:
		if req.Kind == "local" || req.Kind == "remote" {
			view.Title = "View " + req.Kind + " commits"
		}
		items = []cardMenuItem{gitAction, view, review}
	}
	return slices.DeleteFunc(items, func(item cardMenuItem) bool { return item.Action == cardMenuDismissed })
}

func withCardMenuControls(items []cardMenuItem, strip, sitesHidden bool) []cardMenuItem {
	if len(items) > 0 {
		items = append(items, cardMenuItem{}) // Separator before the window and app controls.
	}
	if strip {
		sites := cardMenuItem{Action: cardMenuHideSites, Title: "Hide site windows", Hint: "Hide or restore independent Site windows without changing the strip"}
		if sitesHidden {
			sites.Action, sites.Title = cardMenuShowSites, "Show site windows"
		}
		items = append(items,
			cardMenuItem{Action: cardMenuMinimize, Title: "Minimize strip", Hint: "Show a single row of Site names"},
			cardMenuItem{Action: cardMenuHide, Title: "Hide strip", Hint: "Hide the strip completely"}, sites)
	}
	return append(items, cardMenuItem{Action: cardMenuSettings, Title: "DevJay settings…"}, cardMenuItem{Action: cardMenuQuit, Title: "Quit DevJay"})
}

// Selection must come from the displayed, enabled items. Navigation also
// rechecks the PR and checkout because either may change while AppKit tracks.
func validCardMenuSelection(items []cardMenuItem, req cardMenuRequest, action cardMenuAction, originalPR PullRequestLink, status GitStatus) bool {
	if action == cardMenuDismissed || !slices.ContainsFunc(items, func(item cardMenuItem) bool { return item.Action == action && !item.Disabled }) {
		return false
	}
	switch action {
	case cardMenuView, cardMenuViewDiffs, cardMenuOpenPR, cardMenuOpenPRNew:
		if req.Number > 0 {
			current, ok := requestedPR(status, req.Number)
			if !ok || current.URL != originalPR.URL {
				return false
			}
		}
		return action != cardMenuViewDiffs || cardMenuOffersDiffs(req, status)
	}
	return true
}
