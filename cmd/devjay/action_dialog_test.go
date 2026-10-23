package main

import (
	"context"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestActionDialogsIsolateStateAndConfirmationSenders(t *testing.T) {
	a := &actionDialog{id: "suggested-actions-1", siteID: "a", generation: 1, action: &CheckoutAction{SiteID: "a", Status: "running"}}
	b := &actionDialog{id: "suggested-actions-2", siteID: "b", generation: 2, action: &CheckoutAction{SiteID: "b", Status: "ready", Plan: &CheckoutPlan{ID: "b-token"}}}
	m := &siteManager{actionDialogs: map[string]*actionDialog{a.id: a, b.id: b}}
	if !m.actionBusy() || !m.siteActionBusy("a") || m.siteActionBusy("b") || m.siteActionBusy("c") {
		t.Fatal("busy state leaked between Sites")
	}
	if m.readyActionDialog(b.id, "b-token") != b {
		t.Fatal("another Site's running action blocked confirmation")
	}
	for _, sender := range []string{a.id, "suggested-actions", "suggested-actions-3", "strip", "site-b", "viewer-b", ""} {
		if m.readyActionDialog(sender, "b-token") != nil {
			t.Fatalf("wrong sender %q confirmed another dialog", sender)
		}
	}
	if m.readyActionDialog(b.id, "a-token") != nil {
		t.Fatal("accepted stale/other token")
	}
	b.action = &CheckoutAction{SiteID: "b", Status: "running"}
	if m.readyActionDialog(b.id, "b-token") != nil {
		t.Fatal("consumed token reused")
	}
	a.action = &CheckoutAction{SiteID: "a", Status: "complete"}
	if !m.actionBusy() || m.siteActionBusy("a") || !m.siteActionBusy("b") {
		t.Fatal("one completion released another action")
	}
}

func TestDialogDismissalRejectsStaleRequestsAndOnlyCleansItsOwnWindow(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unhooked := false
	a := &actionDialog{id: "dialog-a", siteID: "a", generation: 4, window: &application.WebviewWindow{}, cancel: cancel,
		removeCloseHook: func() { unhooked = true }, action: &CheckoutAction{SiteID: "a", Status: "running"}}
	b := &actionDialog{id: "dialog-b", siteID: "b", generation: 2, window: &application.WebviewWindow{}, action: &CheckoutAction{SiteID: "b", Status: "ready"}}
	m := &siteManager{actionDialogs: map[string]*actionDialog{a.id: a, b.id: b}, recoveryWindows: map[string]*application.WebviewWindow{a.id: a.window, b.id: b.window}}
	m.dismissAction(a.id, 4)
	if len(m.actionDialogs) != 2 || ctx.Err() != nil {
		t.Fatal("running action dismissed")
	}
	a.action.Status = "complete"
	for _, request := range []struct {
		sender     string
		generation uint64
	}{{a.id, 3}, {b.id, 4}, {"strip", 4}, {"dialog-closed", 4}} {
		m.dismissAction(request.sender, request.generation)
	}
	if len(m.actionDialogs) != 2 || ctx.Err() != nil {
		t.Fatal("stale or unrelated dismissal accepted")
	}
	m.dismissAction(a.id, 4)
	if len(m.actionDialogs) != 1 || m.actionDialogs[b.id] != b || m.recoveryWindows[b.id] != b.window ||
		m.recoveryWindows[a.id] != nil || ctx.Err() == nil || !unhooked {
		t.Fatal("dialog cleanup leaked or affected another operation")
	}
	m.dismissAction(a.id, 4)
	if m.actionDialogs[b.id] != b {
		t.Fatal("late close affected another dialog")
	}
}

func TestActionSnapshotsOnlyExposeOwningSite(t *testing.T) {
	a := &actionDialog{id: "suggested-actions-1", siteID: "a", generation: 3, action: &CheckoutAction{SiteID: "a", Status: "running"}}
	b := &actionDialog{id: "suggested-actions-2", siteID: "b", generation: 7, action: &CheckoutAction{SiteID: "b", Status: "ready"}}
	m := &siteManager{actionDialogs: map[string]*actionDialog{a.id: a, b.id: b}}
	snapshot := appSnapshot{Sites: []SiteView{{Site: Site{ID: "a"}}, {Site: Site{ID: "b"}}}}
	for _, test := range []struct {
		id, viewerSite string
		want           *CheckoutAction
		generation     uint64
	}{
		{a.id, "", a.action, 3}, {b.id, "", b.action, 7}, {"site-a", "", a.action, 0}, {"viewer-b", "b", b.action, 0},
		{"site-c", "", nil, 0}, {"strip", "", nil, 0}, {"settings", "", nil, 0}, {"suggested-actions-closed", "", nil, 0},
	} {
		got := m.actionSnapshot(snapshotForWindow(snapshot, test.id, test.viewerSite), test.id, test.viewerSite)
		if got.Action != test.want || got.ActionGeneration != test.generation {
			t.Fatalf("%s leaked action state: %+v", test.id, got)
		}
		if test.generation != 0 && (len(got.Sites) != 1 || got.Sites[0].ID != test.want.SiteID || got.SuggestedSite != test.want.SiteID) {
			t.Fatalf("%s received other Sites", test.id)
		}
	}
	if len(snapshot.Sites) != 2 || snapshot.Action != nil {
		t.Fatal("snapshot mutated")
	}
}

func TestActionCancellationAndLateResultsAreIndependent(t *testing.T) {
	ctxA, cancelA := context.WithCancel(context.Background())
	defer cancelA()
	ctxB, cancelB := context.WithCancel(context.Background())
	defer cancelB()
	rA, rB := &siteRuntime{}, &siteRuntime{}
	a := &actionDialog{id: "a-window", siteID: "a", generation: 1, cancel: cancelA}
	b := &actionDialog{id: "b-window", siteID: "b", generation: 1, cancel: cancelB}
	m := &siteManager{actionDialogs: map[string]*actionDialog{a.id: a, b.id: b}, runtime: map[string]*siteRuntime{"a": rA, "b": rB}}
	a.cancelPreparation()
	if ctxA.Err() == nil || ctxB.Err() != nil || m.currentActionDialog(a, 1, rA) || !m.currentActionDialog(b, 1, rB) {
		t.Fatal("cancel affected another dialog or accepted old results")
	}
	delete(m.actionDialogs, b.id)
	if m.currentActionDialog(b, 1, rB) {
		t.Fatal("closed dialog received a result")
	}
	m.actionDialogs[b.id] = &actionDialog{id: b.id, siteID: "b", generation: 1}
	if m.currentActionDialog(b, 1, rB) {
		t.Fatal("replacement dialog received an old result")
	}
	m.actionDialogs[b.id] = b
	m.runtime["b"] = &siteRuntime{}
	if m.currentActionDialog(b, 1, rB) {
		t.Fatal("replaced folder received an old result")
	}
}

func TestQuitWaitsForEveryParallelAction(t *testing.T) {
	var life actionLifetime
	if !life.begin() || !life.begin() {
		t.Fatal("parallel actions refused")
	}
	ready, drained := life.quit()
	if ready || drained == nil || life.begin() {
		t.Fatal("quit admission failed")
	}
	life.finish()
	select {
	case <-drained:
		t.Fatal("quit while another action is running")
	default:
	}
	life.finish()
	select {
	case <-drained:
	default:
		t.Fatal("quit did not finish after both actions")
	}
}
