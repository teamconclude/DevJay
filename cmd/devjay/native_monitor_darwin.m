#import <AppKit/AppKit.h>
#import "native_monitor_darwin.h"
#import "spaces_darwin.h"

// These observers read native state and wake the manager; they never order a
// window, change focus, or activate an application. Access is AppKit-only.
static NSMutableArray *devjay_monitor_observers;
static id devjay_interaction_monitor;
static NSTimeInterval devjay_last_interaction;
static BOOL devjay_has_interaction;
static const NSEventMask devjay_interaction_events = NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown |
    NSEventMaskOtherMouseDown | NSEventMaskLeftMouseDragged | NSEventMaskRightMouseDragged |
    NSEventMaskOtherMouseDragged | NSEventMaskMouseMoved | NSEventMaskMouseEntered |
    NSEventMaskScrollWheel | NSEventMaskKeyDown;

static BOOL devjay_monitored_window(NSWindow *window) {
    if (![window isKindOfClass:NSWindow.class]) return NO;
    NSString *name = window.identifier;
    return [name isEqual:@"strip"] || [name hasPrefix:@"site-"] ||
        [name isEqual:@"settings"] || [name isEqual:@"git-viewer"] ||
        [name isEqual:@"suggested-actions"];
}

// Observe only input delivered to our own visible windows, including the
// nonactivating strip. No global input monitor, key contents or focus changes.
// Throttle before waking Go so pointer motion cannot flood the manager queue.
static NSEvent *devjay_monitor_interaction(NSEvent *event, NSTimeInterval now) {
    if (!(NSEventMaskFromType(event.type) & devjay_interaction_events)) return event;
    NSWindow *window = event.window;
    if (!devjay_monitored_window(window) && ![window.identifier isEqual:@"compact-strip"] &&
        ![window.identifier isEqual:@"claude-permission-popover"]) return event;
    if (!devjay_monitor_window_visible(window)) return event;
    if (devjay_has_interaction && now - devjay_last_interaction < 5) return event;
    devjay_has_interaction = YES;
    devjay_last_interaction = now;
    devjay_native_monitor_changed(DevJayMonitorInteraction);
    return event;
}

static void devjay_observe_native(NSNotificationCenter *center, NSNotificationName name,
                                 unsigned int flags, BOOL windowOnly) {
    id observer = [center addObserverForName:name object:nil queue:NSOperationQueue.mainQueue
        usingBlock:^(NSNotification *notification) {
            if (windowOnly && !devjay_monitored_window(notification.object)) return;
            if (flags & (DevJayMonitorSpace | DevJayMonitorPlacement)) dp_invalidate_space_cache();
            devjay_native_monitor_changed(flags);
        }];
    [devjay_monitor_observers addObject:observer];
}

void devjay_start_native_monitor(void) {
    if (devjay_monitor_observers) return;
    devjay_monitor_observers = [NSMutableArray new];
    devjay_interaction_monitor = [NSEvent addLocalMonitorForEventsMatchingMask:devjay_interaction_events
        handler:^NSEvent *(NSEvent *event) {
            return devjay_monitor_interaction(event, NSProcessInfo.processInfo.systemUptime);
        }];
    NSNotificationCenter *workspace = NSWorkspace.sharedWorkspace.notificationCenter;
    NSNotificationCenter *windows = NSNotificationCenter.defaultCenter;
    devjay_observe_native(workspace, NSWorkspaceActiveSpaceDidChangeNotification,
        DevJayMonitorSpace | DevJayMonitorVisibility, NO);
    for (NSNotificationName name in @[NSWorkspaceDidLaunchApplicationNotification,
        NSWorkspaceDidTerminateApplicationNotification, NSWorkspaceDidActivateApplicationNotification,
        NSWorkspaceDidHideApplicationNotification, NSWorkspaceDidUnhideApplicationNotification]) {
        devjay_observe_native(workspace, name, DevJayMonitorApplications | DevJayMonitorVisibility, NO);
    }
    for (NSNotificationName name in @[NSWorkspaceWillSleepNotification, NSWorkspaceDidWakeNotification,
        NSWorkspaceScreensDidSleepNotification, NSWorkspaceScreensDidWakeNotification,
        NSWorkspaceSessionDidResignActiveNotification, NSWorkspaceSessionDidBecomeActiveNotification]) {
        devjay_observe_native(workspace, name, DevJayMonitorPower | DevJayMonitorVisibility, NO);
    }
    devjay_observe_native(windows, NSApplicationDidChangeScreenParametersNotification,
        DevJayMonitorDisplays | DevJayMonitorPlacement | DevJayMonitorVisibility, NO);
    devjay_observe_native(windows, DevJayLayoutChangedNotification,
        DevJayMonitorPlacement | DevJayMonitorVisibility, YES);
    for (NSNotificationName name in @[NSApplicationDidBecomeActiveNotification,
        NSApplicationDidResignActiveNotification, NSApplicationDidHideNotification,
        NSApplicationDidUnhideNotification]) {
        devjay_observe_native(windows, name, DevJayMonitorVisibility, NO);
    }
    for (NSNotificationName name in @[NSWindowDidMoveNotification, NSWindowDidResizeNotification,
        NSWindowDidChangeScreenNotification]) {
        devjay_observe_native(windows, name, DevJayMonitorPlacement | DevJayMonitorVisibility, YES);
    }
    for (NSNotificationName name in @[NSWindowDidChangeOcclusionStateNotification,
        NSWindowDidMiniaturizeNotification, NSWindowDidDeminiaturizeNotification,
        NSWindowDidBecomeKeyNotification, NSWindowDidResignKeyNotification,
        NSWindowDidBecomeMainNotification, NSWindowDidResignMainNotification,
        NSWindowWillCloseNotification]) {
        devjay_observe_native(windows, name, DevJayMonitorVisibility, YES);
    }
}

int devjay_monitor_window_visible(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    return window && window.alphaValue > 0 && window.visible && !window.miniaturized && window.onActiveSpace &&
        (window.occlusionState & NSWindowOcclusionStateVisible) != 0;
}
