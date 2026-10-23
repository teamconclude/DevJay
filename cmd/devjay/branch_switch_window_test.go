package main

import "testing"

func TestBranchPickerAndPreparationRequireBoundSite(t *testing.T) {
	action := &CheckoutAction{SiteID: "one", Kind: "switch", Status: "choosing"}
	dialog := &actionDialog{siteID: "one", action: action}
	if !branchPickerSenderAllowed(nil, "one", "one") || !branchSwitchPreparationAllowed(dialog, "one") {
		t.Fatal("registered viewer/chooser rejected")
	}
	for _, status := range []string{"loading-branches", "choosing", "preparing", "running", "complete", "failed", "ready"} {
		action.Status = status
		if got, want := branchSwitchPreparationAllowed(dialog, "one"), status == "choosing" || status == "ready"; got != want {
			t.Fatalf("preparation in %q: %t", status, got)
		}
		if got, want := branchPickerSenderAllowed(dialog, "one", ""), status == "ready" || status == "failed"; got != want {
			t.Fatalf("reopen in %q: %t", status, got)
		}
	}
	for _, mismatch := range []struct{ selected, id, kind string }{{"two", "one", "switch"}, {"one", "two", "switch"}, {"one", "one", "merged"}} {
		dialog.siteID, action.SiteID, action.Kind = mismatch.selected, mismatch.id, mismatch.kind
		if branchSwitchPreparationAllowed(dialog, "one") || branchPickerSenderAllowed(dialog, "one", "") {
			t.Fatalf("unrelated dialog accepted: %#v", mismatch)
		}
	}
	if branchSwitchPreparationAllowed(nil, "one") || branchPickerSenderAllowed(nil, "one", "") || branchPickerSenderAllowed(nil, "one", "two") {
		t.Fatal("unregistered dialog or another Site's viewer accepted")
	}
}
