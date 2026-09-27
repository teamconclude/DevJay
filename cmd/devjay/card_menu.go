package main

import (
	"fmt"
	"math"
)

type cardMenuRequest struct {
	SiteID string  `json:"siteID"`
	Kind   string  `json:"kind"`
	Number int     `json:"number"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
}

type cardMenuAction int

const (
	cardMenuDismissed cardMenuAction = iota
	cardMenuView
	cardMenuViewNew
	cardMenuSettings
	cardMenuQuit
)

func normalizeCardMenuRequest(req cardMenuRequest) (cardMenuRequest, bool) {
	if req.Kind == "" {
		req.Kind = "edits"
	}
	// The native bridge also checks the actual webview bounds. This outer limit
	// rejects unreasonable or non-finite input before dispatching to AppKit.
	validCoordinate := func(v float64) bool {
		return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 32768
	}
	return req, req.SiteID != "" && validViewKind(req.Kind) && req.Number >= 0 && validCoordinate(req.X) && validCoordinate(req.Y)
}

func cardMenuLabels(req cardMenuRequest) [4]string {
	if req.Number > 0 {
		return [4]string{fmt.Sprintf("Open PR #%d", req.Number), fmt.Sprintf("Open PR #%d in new tab", req.Number), "DevJay settings…", "Quit DevJay"}
	}
	subject := map[string]string{"edits": "local edits", "local": "local commits", "remote": "remote commits"}[req.Kind]
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
	if req.Number == 0 {
		return true
	}
	current, ok := requestedPR(status, req.Number)
	return ok && current.URL == originalPR.URL
}
