package main

// The caller resolves both dialog and viewer from the sending window's registry
// entry. A chooser may only reopen or prepare its own Site's switch operation.
func branchPickerSenderAllowed(dialog *actionDialog, siteID, viewerSite string) bool {
	if siteID == "" {
		return false
	}
	if viewerSite == siteID {
		return true
	}
	return dialog != nil && dialog.siteID == siteID && dialog.action != nil &&
		dialog.action.SiteID == siteID && dialog.action.Kind == "switch" &&
		(dialog.action.Status == "ready" || dialog.action.Status == "failed")
}

func branchSwitchPreparationAllowed(dialog *actionDialog, siteID string) bool {
	return siteID != "" && dialog != nil && dialog.siteID == siteID &&
		dialog.action != nil && dialog.action.SiteID == siteID && dialog.action.Kind == "switch" &&
		(dialog.action.Status == "choosing" || dialog.action.Status == "ready")
}
