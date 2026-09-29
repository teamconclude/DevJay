package main

// Branch selection starts in a registered Changes window for this Site. The shared dialog may reopen
// the chooser only while it is still bound to that Site's switch operation.
func branchPickerSenderAllowed(sender, siteID, selectedSite, viewerSite string, action *CheckoutAction) bool {
	if siteID == "" {
		return false
	}
	if viewerSite == siteID {
		return true
	}
	return sender == "suggested-actions" && selectedSite == siteID && action != nil &&
		action.SiteID == siteID && action.Kind == "switch" && action.Status != "running"
}

func branchSwitchPreparationAllowed(sender, siteID, selectedSite string, action *CheckoutAction) bool {
	return siteID != "" && sender == "suggested-actions" && selectedSite == siteID &&
		action != nil && action.SiteID == siteID && action.Kind == "switch" &&
		(action.Status == "choosing" || action.Status == "ready")
}
