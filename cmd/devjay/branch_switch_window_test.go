package main

import "testing"

func TestBranchPickerAndPreparationRequireBoundSite(t *testing.T) {
	action := &CheckoutAction{SiteID: "one", Kind: "switch", Status: "choosing"}
	if !branchPickerSenderAllowed("viewer-1", "one", "", "one", nil) {
		t.Fatal("registered Changes window cannot open its chooser")
	}
	if !branchPickerSenderAllowed("suggested-actions", "one", "one", "", action) ||
		!branchSwitchPreparationAllowed("suggested-actions", "one", "one", action) {
		t.Fatal("bound chooser cannot select a branch")
	}
	for _, sender := range []string{"control-panel", "settings", "site-one", "site-two", "viewer-1", ""} {
		if branchPickerSenderAllowed(sender, "one", "one", "", action) || branchSwitchPreparationAllowed(sender, "one", "one", action) {
			t.Fatalf("unrelated sender %q was accepted", sender)
		}
	}
	if branchSwitchPreparationAllowed("site-one", "one", "one", action) {
		t.Fatal("Site window bypassed the chooser")
	}
	for _, state := range []string{"loading-branches", "preparing", "running", "complete", "failed"} {
		action.Status = state
		if branchSwitchPreparationAllowed("suggested-actions", "one", "one", action) {
			t.Fatalf("preparation accepted from %q", state)
		}
	}
	action.Status = "ready"
	if !branchSwitchPreparationAllowed("suggested-actions", "one", "one", action) {
		t.Fatal("remote option could not prepare a new plan")
	}
	for _, context := range []struct{ selected, id, kind string }{{"two", "one", "switch"}, {"one", "two", "switch"}, {"one", "one", "merged"}} {
		action.SiteID, action.Kind = context.id, context.kind
		if branchSwitchPreparationAllowed("suggested-actions", "one", context.selected, action) ||
			branchPickerSenderAllowed("suggested-actions", "one", context.selected, "", action) {
			t.Fatalf("unrelated dialog accepted: %#v", context)
		}
	}
	if branchSwitchPreparationAllowed("suggested-actions", "one", "one", nil) || branchPickerSenderAllowed("suggested-actions", "one", "one", "", nil) {
		t.Fatal("closed dialog was accepted")
	}
}

func TestBranchPickerRejectsViewerForAnotherSite(t *testing.T) {
	if branchPickerSenderAllowed("viewer-1", "one", "", "two", nil) {
		t.Fatal("viewer for another Site opened the chooser")
	}
}
