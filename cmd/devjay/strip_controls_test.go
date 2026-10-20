package main

import (
	"encoding/json"
	"github.com/wailsapp/wails/v3/pkg/application"
	"math"
	"strings"
	"testing"
)

func TestStripMenuRequiresStripSenderAndNoSitePayload(t *testing.T) {
	req := cardMenuRequest{Kind: "strip", X: 12, Y: 5}
	if _, ok := normalizeCardMenuRequest(req); !ok {
		t.Fatal("valid strip menu rejected")
	}
	for _, change := range []func(*cardMenuRequest){
		func(r *cardMenuRequest) { r.SiteID = "other" },
		func(r *cardMenuRequest) { r.Number = 1 },
		func(r *cardMenuRequest) { r.BundleID = "app" },
		func(r *cardMenuRequest) { r.BaseBranch = "main" },
		func(r *cardMenuRequest) { r.PublishRebased = true },
		func(r *cardMenuRequest) { r.PublishButton = true },
		func(r *cardMenuRequest) { r.GitAction = "push" },
		func(r *cardMenuRequest) { r.Anchor = &cardAnchor{Width: 10, Height: 10} },
		func(r *cardMenuRequest) { r.X = math.NaN() },
		func(r *cardMenuRequest) { r.Y = -1 },
	} {
		invalid := req
		change(&invalid)
		if _, ok := normalizeCardMenuRequest(invalid); ok {
			t.Fatalf("accepted %+v", invalid)
		}
	}
	strip := &application.WebviewWindow{}
	m := &siteManager{strip: strip, stripReady: true}
	for _, sender := range []string{"strip", "settings", "site-other", "git-viewer"} {
		window, site := m.cardMenuSource(req, sender)
		if (window == strip) != (sender == "strip") || site != nil {
			t.Fatalf("wrong source %q", sender)
		}
	}
	m.stripReady = false
	if window, _ := m.cardMenuSource(req, "strip"); window != nil {
		t.Fatal("unready strip accepted")
	}
}

func TestShortcutFailureMessageIdentifiesOnlyUnavailableKeys(t *testing.T) {
	if shortcutFailureMessage(0) != "" {
		t.Fatal("success should be quiet")
	}
	message := shortcutFailureMessage(1 | 1<<4 | 1<<10 | 1<<11 | 1<<12)
	for _, key := range []string{"⌃⌥⌘M", "⌃⌥⌘4", "⌃⌥⌘0", "⌃⌥⌘D", "⌃⌥⌘S"} {
		if !strings.Contains(message, key) {
			t.Fatalf("missing %s in %s", key, message)
		}
	}
	if strings.Contains(message, "⌃⌥⌘1") {
		t.Fatal("reported an available key")
	}
}

func TestCompactStripCardsFollowSnapshotOrderAndIgnoreGitChurn(t *testing.T) {
	sites := []SiteView{
		{Site: Site{ID: "b", Path: "/tmp/B"}, Name: "B", Window: cardState{OnDesktop: true}},
		{Site: Site{ID: "a", Path: "/tmp/A"}, Name: "A"},
	}
	payload := compactStripCards(sites)
	var cards []struct {
		ID, Name, Path string
		Active         bool
	}
	if err := json.Unmarshal([]byte(payload), &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards) != 2 || cards[0].ID != "b" || !cards[0].Active || cards[1].ID != "a" || cards[1].Active {
		t.Fatalf("wrong cards: %+v", cards)
	}
	sites[0].Git = GitStatus{Branch: "other", Summary: "new Git reading"}
	if compactStripCards(sites) != payload {
		t.Fatal("unrelated Git reading rebuilt the native row")
	}
	sites[0].Window.OnDesktop = false
	if compactStripCards(sites) == payload {
		t.Fatal("desktop highlight failed to change")
	}
	if compactStripCards(nil) != "[]" {
		t.Fatal("empty row needs an array")
	}
}

func TestShortcutHintsCoalesceWithoutLosingRelease(t *testing.T) {
	for range 100 {
		queueShortcutHints(0x1ff)
		queueShortcutHints(0)
	}
	if len(shortcutHintEvents) != 1 {
		t.Fatal("modifier changes must use a single nonblocking wake")
	}
	<-shortcutHintEvents
	if pendingShortcutHints.Load() != 0 {
		t.Fatal("a busy manager must receive the latest release")
	}
	before := snapshotSignature(appSnapshot{})
	if snapshotSignature(appSnapshot{ShortcutHints: 1}) == before {
		t.Fatal("modifier changes must reach the full strip")
	}
}

func TestStripVisibilityRestoresPreviousMode(t *testing.T) {
	if restoredStripMode(stripCompact) != stripCompact || restoredStripMode(stripFull) != stripFull || restoredStripMode(stripHidden) != stripFull {
		t.Fatal("invalid restore mode")
	}
	if stripVisibilityLabel(stripHidden) != "Show strip" || stripVisibilityLabel(stripCompact) != "Hide strip" {
		t.Fatal("wrong visibility label")
	}
}
