#ifndef DEVJAY_SITE_APPS_DARWIN_H
#define DEVJAY_SITE_APPS_DARWIN_H

// Capture only on the AppKit thread. The returned ID can be passed to the
// background metadata reader without retaining or accessing the NSWindow.
unsigned int devjay_site_app_window_number(void *window);

// Read-only background scan. targets is JSON {siteID: windowNumber}.
// Returns owned JSON {available, sites: {siteID: [{name,bundleID,icon,kind,windowCount,processIDs}]}}.
// Unknown/ambiguous Site desktops are omitted; a known desktop can have [].
// The caller frees the returned string. No window titles or contents are read.
// Installed app icons are cached; first access briefly visits the AppKit thread.
char *devjay_site_apps(const char *targets);

#endif
