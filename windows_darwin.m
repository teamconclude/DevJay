#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#import <WebKit/WebKit.h>

// Called on the AppKit main thread through application.InvokeSync.
unsigned int devjay_window_state(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    if (window == nil) return 0;
    unsigned int flags = 0;
    if (window.isOnActiveSpace && window.isVisible && !window.isMiniaturized) flags |= 1;
    if (window.isKeyWindow && NSApp.isActive) flags |= 2;
    if (window.isMiniaturized) flags |= 4;
    return flags;
}

void devjay_all_desktops(void *pointer, int enabled) {
    NSWindow *window = (NSWindow *)pointer;
    NSWindowCollectionBehavior behavior = window.collectionBehavior;
    if (enabled) behavior |= NSWindowCollectionBehaviorCanJoinAllSpaces;
    else behavior &= ~NSWindowCollectionBehaviorCanJoinAllSpaces;
    window.collectionBehavior = behavior;
    [NSUserDefaults.standardUserDefaults setBool:(enabled != 0) forKey:@"DevJay.View.AllDesktops"];
}


void devjay_all_windows_on_top(int enabled) {
    NSInteger level = enabled ? NSFloatingWindowLevel : NSNormalWindowLevel;
    NSUInteger updated = 0;
    for (NSWindow *window in NSApp.windows) {
        if (![window.identifier isEqual:@"control-panel"] && ![window.identifier isEqual:@"settings"] && ![window.identifier hasPrefix:@"site-"]) continue;
        if ([window isKindOfClass:NSPanel.class]) [(NSPanel *)window setFloatingPanel:enabled];
        window.level = level;
        if (window.level == level) updated++;
    }
    [NSUserDefaults.standardUserDefaults setBool:enabled forKey:@"DevJay.View.OnTop"];
    NSLog(@"DevJay: keep all windows on top %@; %lu windows at level %ld",
          enabled ? @"enabled" : @"disabled", (unsigned long)updated, (long)level);
}

// Match Claude's first-mouse handling at the WKWebView class level. This affects
// only web views in this process and lets an inactive panel receive the click.
static BOOL devjay_accepts_first_mouse(id view, SEL selector, NSEvent *event) { return YES; }

int devjay_prepare_panel(void *pointer) {
    NSPanel *window = (NSPanel *)pointer;
    if (window == nil) return 0;
    Method method = class_getInstanceMethod(WKWebView.class, @selector(acceptsFirstMouse:));
    if (method == NULL) return 0;
    method_setImplementation(method, (IMP)devjay_accepts_first_mouse);
    window.becomesKeyOnlyIfNeeded = YES;
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    // New View preferences start enabled; subsequent menu changes are retained.
    [defaults registerDefaults:@{@"DevJay.View.AllDesktops": @YES, @"DevJay.View.OnTop": @YES}];
    devjay_all_desktops(window, [defaults boolForKey:@"DevJay.View.AllDesktops"]);
    devjay_all_windows_on_top([defaults boolForKey:@"DevJay.View.OnTop"]);
    return 1;
}

unsigned int devjay_panel_preferences(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    unsigned int flags = 0;
    if (window.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces) flags |= 1;
    if ([NSUserDefaults.standardUserDefaults boolForKey:@"DevJay.View.OnTop"]) flags |= 2;
    return flags;
}

void devjay_focus_site(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    if (window.isMiniaturized) [window deminiaturize:nil];
    // Establish the destination before application activation chooses a Space.
    [window makeKeyAndOrderFront:nil];
    [window makeMainWindow];
    [NSApp activateIgnoringOtherApps:YES];
}

void devjay_show_settings(void *pointer, void *parentPointer) {
    NSWindow *window = (NSWindow *)pointer;
    NSWindow *parent = (NSWindow *)parentPointer;
    window.identifier = @"settings";
    window.collectionBehavior = NSWindowCollectionBehaviorMoveToActiveSpace | NSWindowCollectionBehaviorFullScreenNone;
    NSRect frame = window.frame;
    NSRect area = (parent.screen ?: NSScreen.mainScreen).visibleFrame;
    frame.origin.x = MAX(NSMinX(area) + 8, MIN(NSMidX(parent.frame) - frame.size.width / 2, NSMaxX(area) - frame.size.width - 8));
    frame.origin.y = MAX(NSMinY(area) + 8, MIN(NSMidY(parent.frame) - frame.size.height / 2, NSMaxY(area) - frame.size.height - 8));
    [window setFrame:frame display:NO];
    window.level = [NSUserDefaults.standardUserDefaults boolForKey:@"DevJay.View.OnTop"] ? NSFloatingWindowLevel : NSNormalWindowLevel;
    if (window.isMiniaturized) [window deminiaturize:nil];
    [window makeKeyAndOrderFront:nil];
    [window makeMainWindow];
    [NSApp activateIgnoringOtherApps:YES];
}

void devjay_apply_top_preference(void) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    devjay_all_windows_on_top([defaults objectForKey:@"DevJay.View.OnTop"] == nil || [defaults boolForKey:@"DevJay.View.OnTop"]);
}
