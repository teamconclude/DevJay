package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed native_scripts/pr_browser.js
var prBrowserScript string

// Browser navigation has its own capacity; an Automation prompt must not hold
// up local Git reads, remote refreshes, or confirmed Git actions.
var browserCommandSlots = make(chan struct{}, 1)

type prBrowserResult struct {
	Handled     *bool  `json:"handled"`
	Unavailable bool   `json:"unavailable"`
	Error       string `json:"error"`
}

func openPullRequestBrowser(ctx context.Context, url string, newTab bool) (string, error) {
	return openPullRequestBrowserWith(ctx, url, newTab, func(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
		return runCommandWithSlots(ctx, browserCommandSlots, timeout, name, args...)
	})
}

func openPullRequestBrowserWith(ctx context.Context, url string, newTab bool, run func(context.Context, time.Duration, string, ...string) ([]byte, error)) (string, error) {
	// The script is fixed source. The URL and mode are separate argv values and
	// no web page scripts are executed. Only tab URLs and window IDs are read.
	data, err := run(ctx, 45*time.Second, "/usr/bin/osascript",
		"-l", "JavaScript", "-e", prBrowserScript, "--", url, strconv.FormatBool(newTab))
	if err != nil {
		return "", errors.New("The browser did not confirm opening the PR. Check it before trying again.")
	}
	var result prBrowserResult
	if json.Unmarshal(data, &result) != nil {
		return "", errors.New("The browser did not confirm opening the PR. Check it before trying again.")
	}
	if result.Error != "" {
		return "", errors.New(result.Error)
	}
	if result.Handled == nil {
		return "", errors.New("The browser did not confirm opening the PR. Check it before trying again.")
	}
	if *result.Handled {
		return "", nil
	}
	// The script explicitly reports no navigation attempt before fallback.
	if _, err := run(ctx, 10*time.Second, "/usr/bin/open", url); err != nil {
		return "", errors.New("The PR could not be opened in your default browser.")
	}
	if result.Unavailable {
		return "Opened in your default browser. Existing tabs could not be checked; allow DevJay access in System Settings → Privacy & Security → Automation to reuse supported browser tabs.", nil
	}
	if newTab {
		return "Opened in your default browser. Your browser settings decide whether to use a tab or a window.", nil
	}
	return "", nil
}

func (m *siteManager) emitPROpenResult(req prOpenRequest, sender, message string, err error) {
	result := struct {
		SiteID  string `json:"siteID"`
		Number  int    `json:"number"`
		Sender  string `json:"sender"`
		Message string `json:"message,omitempty"`
		Error   string `json:"error,omitempty"`
	}{SiteID: req.SiteID, Number: req.Number, Sender: sender, Message: message}
	if err != nil {
		result.Error = err.Error()
	}
	m.app.Event.Emit("devjay:pr-open-result", result)
}

// Call only on the manager queue, including native context menu callbacks.
func (m *siteManager) openPR(req prOpenRequest, sender string) {
	if !validPRSender(sender, req.SiteID) {
		return
	}
	site := m.runtime[req.SiteID]
	if site == nil || !site.Ready {
		return
	}
	pr, ok := requestedPR(site.Git, req.Number)
	if !ok {
		m.emitPROpenResult(req, sender, "", errors.New("This PR is no longer listed for the Site. Check the updated card."))
		return
	}
	if m.prOpening {
		m.emitPROpenResult(req, sender, "Another PR is opening. Complete any browser permission prompt, then try again.", nil)
		return
	}
	m.prOpening = true
	go func() {
		message, err := openPullRequestBrowser(m.app.Context(), pr.URL, req.NewTab)
		m.send(func() {
			m.prOpening = false
			if m.runtime[req.SiteID] != site {
				return
			}
			m.emitPROpenResult(req, sender, message, err)
		})
	}()
}

func (m *siteManager) registerPREvents() {
	m.app.Event.On("devjay:pr-open", func(e *application.CustomEvent) {
		var req prOpenRequest
		data, err := json.Marshal(e.Data)
		if err != nil || json.Unmarshal(data, &req) != nil {
			return
		}
		sender := e.Sender
		m.send(func() { m.openPR(req, sender) })
	})
}
