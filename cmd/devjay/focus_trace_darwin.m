#import "focus_trace_darwin.h"
#import <os/log.h>

// Opt-in, observational diagnostics. All callers and observers run on AppKit's
// main thread. In particular, tracing must never order or activate a window.
static BOOL focus_trace_enabled(void) {
    static BOOL checked, enabled;
    if (!checked) {
        checked = YES;
        enabled = [NSProcessInfo.processInfo.environment[@"DEVJAY_TRACE_FOCUS"] isEqual:@"1"];
    }
    return enabled;
}

static NSString *focus_window_description(NSWindow *window) {
    if (!window) return @"none";
    return [NSString stringWithFormat:@"%@#%ld(onSpace=%d,visible=%d,minimized=%d)",
        window.identifier ?: @"unidentified", (long)window.windowNumber,
        window.isOnActiveSpace, window.isVisible, window.isMiniaturized];
}

void devjay_trace_focus(NSString *event, NSWindow *window) {
    static BOOL logging;
    if (!focus_trace_enabled() || logging) return;
    logging = YES;
    @try {
        os_log(OS_LOG_DEFAULT,
            "DevJay focus: %{public}@ target=%{public}@ appActive=%d key=%{public}@ main=%{public}@ frontmost=%{public}@",
            event, focus_window_description(window), NSApp.isActive,
            focus_window_description(NSApp.keyWindow), focus_window_description(NSApp.mainWindow),
            NSWorkspace.sharedWorkspace.frontmostApplication.bundleIdentifier ?: @"none");
    } @finally { logging = NO; }
}

void devjay_trace_navigation(void *pointer) {
    // Wails beta.23 may Show() an ordinary window after this event. Recording
    // before its handler distinguishes that path from an explicit card click.
    devjay_trace_focus(@"web-navigation-finished", (NSWindow *)pointer);
}

void devjay_start_focus_trace(void) {
    static NSMutableArray *observers;
    if (!focus_trace_enabled() || observers) return;
    observers = [NSMutableArray new];
    NSArray *names = @[NSApplicationWillBecomeActiveNotification,
        NSApplicationDidBecomeActiveNotification, NSApplicationDidResignActiveNotification,
        NSWindowDidBecomeKeyNotification, NSWindowDidResignKeyNotification,
        NSWindowDidBecomeMainNotification, NSWindowDidResignMainNotification];
    for (NSString *name in names) {
        id token = [NSNotificationCenter.defaultCenter addObserverForName:name object:nil
            queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
                NSWindow *window = [note.object isKindOfClass:NSWindow.class] ? note.object : nil;
                devjay_trace_focus(note.name, window);
                if (window && !window.isOnActiveSpace &&
                    ([note.name isEqual:NSWindowDidBecomeMainNotification] ||
                     [note.name isEqual:NSWindowDidBecomeKeyNotification])) {
                    os_log(OS_LOG_DEFAULT, "DevJay focus: off-desktop-activation-stack %{public}@ %{public}@",
                        note.name, [NSThread.callStackSymbols componentsJoinedByString:@" | "]);
                }
            }];
        [observers addObject:token];
    }
    for (NSString *name in @[NSWorkspaceActiveSpaceDidChangeNotification,
                            NSWorkspaceDidActivateApplicationNotification]) {
        id token = [NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:name object:nil
            queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note) {
                devjay_trace_focus(note.name, nil);
                if ([note.name isEqual:NSWorkspaceActiveSpaceDidChangeNotification]) {
                    for (NSWindow *window in NSApp.windows) {
                        if (window.identifier) devjay_trace_focus(@"space-window-state", window);
                    }
                }
            }];
        [observers addObject:token];
    }
    devjay_trace_focus(@"trace-started", nil);
}
