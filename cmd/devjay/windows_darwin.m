#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#import <WebKit/WebKit.h>
#import "focus_trace_darwin.h"

// WebKit's CSS hover tracking can stop when its window is inactive. An
// always-active AppKit tracking area reports position without activating it.
@interface DevJayHoverTracker : NSObject {
@public
    WKWebView *view; // Owned by the window; this tracker is associated with it.
    NSPoint position;
    NSUInteger revision;
    BOOL pending;
}
- (void)publish;
@end

@implementation DevJayHoverTracker
- (void)publish {
    if (pending) return;
    pending = YES;
    NSUInteger sent = revision;
    NSString *script = [NSString stringWithFormat:
        @"window.dispatchEvent(new CustomEvent('devjay:native-hover',{detail:{x:%.17g,y:%.17g}}))",
        position.x, position.y];
    // Keep the view alive until delivery finishes, including during closing.
    WKWebView *target = view;
    [target evaluateJavaScript:script completionHandler:^(id result, NSError *error) {
        pending = NO;
        if (sent != revision && target.window != nil) [self publish];
    }];
}
- (void)mouseMoved:(NSEvent *)event {
    NSRect bounds = view.bounds;
    if (bounds.size.width <= 0 || bounds.size.height <= 0) return;
    NSPoint point = [view convertPoint:event.locationInWindow fromView:nil];
    position.x = (point.x - NSMinX(bounds)) / bounds.size.width;
    position.y = view.isFlipped ? (point.y - NSMinY(bounds)) / bounds.size.height
                               : (NSMaxY(bounds) - point.y) / bounds.size.height;
    revision++;
    [self publish];
}
- (void)mouseEntered:(NSEvent *)event { [self mouseMoved:event]; }
- (void)mouseExited:(NSEvent *)event {
    position = NSMakePoint(-1, -1);
    revision++;
    [self publish];
}
@end

static WKWebView *devjay_webview(NSView *view) {
    if ([view isKindOfClass:WKWebView.class]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *webview = devjay_webview(child);
        if (webview) return webview;
    }
    return nil;
}

void devjay_track_hover(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    WKWebView *view = devjay_webview(window.contentView);
    static const char trackerKey;
    if (!view || objc_getAssociatedObject(view, &trackerKey)) return;
    DevJayHoverTracker *tracker = [[[DevJayHoverTracker alloc] init] autorelease];
    tracker->view = view;
    objc_setAssociatedObject(view, &trackerKey, tracker, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    NSTrackingArea *area = [[NSTrackingArea alloc] initWithRect:NSZeroRect
        options:NSTrackingMouseEnteredAndExited | NSTrackingMouseMoved | NSTrackingActiveAlways | NSTrackingInVisibleRect
        owner:tracker userInfo:nil];
    [view addTrackingArea:area];
    [area release];
    window.acceptsMouseMovedEvents = YES;
}

// Called on the AppKit main thread through application.InvokeSync.
unsigned int devjay_window_state(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    if (window == nil) return 0;
    unsigned int flags = 0;
    if (window.isOnActiveSpace && window.isVisible && !window.isMiniaturized) flags |= 1;
    if (window.isKeyWindow && NSApp.isActive) flags |= 2;
    if (window.isMiniaturized) flags |= 4;
    if (window.isVisible) flags |= 8;
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
        if (![window.identifier isEqual:@"control-panel"] && ![window.identifier isEqual:@"settings"] && ![window.identifier isEqual:@"git-viewer"] && ![window.identifier isEqual:@"suggested-actions"] && ![window.identifier hasPrefix:@"site-"]) continue;
        if ([window isKindOfClass:NSPanel.class]) [(NSPanel *)window setFloatingPanel:enabled];
        window.level = level;
        if (window.level == level) updated++;
    }
    [NSUserDefaults.standardUserDefaults setBool:enabled forKey:@"DevJay.View.OnTop"];
    NSLog(@"DevJay: keep all windows on top %@; %lu windows at level %ld",
          enabled ? @"enabled" : @"disabled", (unsigned long)updated, (long)level);
}

// Keep WebKit's runtime class intact: changing it breaks AppKit's computed
// properties and KVO teardown. Only marked panel views accept the first click;
// all other web views retain their original behavior.
static const char firstMouseKey;
static BOOL (*originalFirstMouse)(id, SEL, NSEvent *);
static BOOL devjay_accepts_first_mouse(id view, SEL selector, NSEvent *event) {
    if (objc_getAssociatedObject(view, &firstMouseKey)) return YES;
    return originalFirstMouse(view, selector, event);
}
static BOOL devjay_enable_first_mouse(NSView *view) {
    if (![view isKindOfClass:WKWebView.class]) return NO;
    if (!originalFirstMouse) {
        SEL selector = @selector(acceptsFirstMouse:);
        Method method = class_getInstanceMethod(WKWebView.class, selector);
        if (!method) return NO;
        originalFirstMouse = (BOOL (*)(id, SEL, NSEvent *))method_getImplementation(method);
        // Add an override when inherited, so NSView's implementation is never
        // replaced. Otherwise replace WKWebView's own method with our guard.
        if (!class_addMethod(WKWebView.class, selector, (IMP)devjay_accepts_first_mouse, method_getTypeEncoding(method))) {
            method_setImplementation(class_getInstanceMethod(WKWebView.class, selector), (IMP)devjay_accepts_first_mouse);
        }
    }
    objc_setAssociatedObject(view, &firstMouseKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    return YES;
}

// Wails' WebviewPanel permits keyboard focus by default. Override its public
// eligibility method only for our marked strip; preserve the native class and
// every other window's behavior. All setup and callbacks run on AppKit's thread.
static const char stripNoKeyboardKey, stripKeyboardClassKey;
static BOOL devjay_make_strip_mouse_only(NSPanel *panel) {
    if (![panel isKindOfClass:NSPanel.class]) return NO;
    Class cls = object_getClass(panel);
    if (cls == NSPanel.class) return NO; // Do not alter the AppKit base class.
    if (!objc_getAssociatedObject(cls, &stripKeyboardClassKey)) {
        SEL selector = @selector(canBecomeKeyWindow);
        Method method = class_getInstanceMethod(cls, selector);
        if (!method) return NO;
        BOOL (*original)(id, SEL) = (BOOL (*)(id, SEL))method_getImplementation(method);
        IMP replacement = imp_implementationWithBlock(^BOOL(id candidate) {
            if (objc_getAssociatedObject(candidate, &stripNoKeyboardKey)) return NO;
            return original(candidate, selector);
        });
        // Add on this class if inherited, never replace NSPanel's base method.
        class_replaceMethod(cls, selector, replacement, method_getTypeEncoding(method));
        objc_setAssociatedObject(cls, &stripKeyboardClassKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    }
    objc_setAssociatedObject(panel, &stripNoKeyboardKey, @YES, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    panel.becomesKeyOnlyIfNeeded = YES;
    // The strip is an overlay, while ordinary Site windows stay managed by Spaces.
    NSWindowCollectionBehavior behavior = panel.collectionBehavior;
    behavior &= ~(NSWindowCollectionBehaviorManaged | NSWindowCollectionBehaviorTransient |
                  NSWindowCollectionBehaviorParticipatesInCycle);
    panel.collectionBehavior = behavior | NSWindowCollectionBehaviorStationary |
                                         NSWindowCollectionBehaviorIgnoresCycle;
    return YES;
}

int devjay_prepare_panel(void *pointer) {
    devjay_start_focus_trace();
    NSPanel *window = (NSPanel *)pointer;
    if (window == nil) return 0;
    if (!devjay_enable_first_mouse(devjay_webview(window.contentView))) return 0;
    if (!devjay_make_strip_mouse_only(window)) return 0;
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
    if (!window) return;
    devjay_trace_focus(@"explicit-focus-request", window);
    if (window.isMiniaturized) [window deminiaturize:nil];
    if ([window.identifier isEqual:@"control-panel"]) {
        // Showing the strip must not activate the app's remembered Site.
        [window orderFrontRegardless];
        return;
    }
    // Original Site navigation: establish the target before activating the app.
    [window makeKeyAndOrderFront:nil];
    if (window.canBecomeMainWindow) [window makeMainWindow];
    [NSApp activateIgnoringOtherApps:YES];
}

void devjay_show_popup(void *pointer, void *parentPointer) {
    NSWindow *window = (NSWindow *)pointer;
    devjay_trace_focus(@"explicit-popup-request", window);
    NSWindow *parent = (NSWindow *)parentPointer;
    window.collectionBehavior = NSWindowCollectionBehaviorMoveToActiveSpace | NSWindowCollectionBehaviorFullScreenNone;
    NSRect frame = window.frame;
    NSRect area = (parent.screen ?: NSScreen.mainScreen).visibleFrame;
    NSRect anchor = parent ? parent.frame : area;
    frame.origin.x = MAX(NSMinX(area) + 8, MIN(NSMidX(anchor) - frame.size.width / 2, NSMaxX(area) - frame.size.width - 8));
    frame.origin.y = MAX(NSMinY(area) + 8, MIN(NSMidY(anchor) - frame.size.height / 2, NSMaxY(area) - frame.size.height - 8));
    [window setFrame:frame display:NO];
    window.level = [NSUserDefaults.standardUserDefaults boolForKey:@"DevJay.View.OnTop"] ? NSFloatingWindowLevel : NSNormalWindowLevel;
    if (window.isMiniaturized) [window deminiaturize:nil];
    [window makeKeyAndOrderFront:nil];
    if (window.canBecomeMainWindow) [window makeMainWindow];
    [NSApp activateIgnoringOtherApps:YES];
}

void devjay_apply_top_preference(void) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    devjay_all_windows_on_top([defaults objectForKey:@"DevJay.View.OnTop"] == nil || [defaults boolForKey:@"DevJay.View.OnTop"]);
}

void devjay_show_settings(void *pointer, void *parentPointer) {
    NSWindow *window = (NSWindow *)pointer;
    NSWindow *parent = (NSWindow *)parentPointer;
    window.identifier = @"settings";
    WKWebView *view = devjay_webview(window.contentView);
    if (!view) { devjay_show_popup(window, parent); return; }
    // Measure natural content height, excluding the viewport minimum and the
    // footer's spare space. This also respects the user's current window width.
    NSString *script = @"(() => { const root = document.querySelector('#app'); if (!root) return 0; const saved = root.style.minHeight; root.style.minHeight = '0'; const height = Math.ceil(root.getBoundingClientRect().height); root.style.minHeight = saved; window.scrollTo(0, 0); return height; })()";
    [view evaluateJavaScript:script completionHandler:^(id result, NSError *error) {
        if (!error && [result isKindOfClass:NSNumber.class]) {
            CGFloat height = [result doubleValue];
            NSRect area = (parent.screen ?: NSScreen.mainScreen).visibleFrame;
            if (isfinite(height) && height > 0) {
                NSRect frame = window.frame;
                NSRect fitted = [window frameRectForContentRect:NSMakeRect(0, 0, view.bounds.size.width, height + 2)];
                frame.size.height = MIN(MAX(fitted.size.height, window.minSize.height), MAX(1, area.size.height - 16));
                [window setFrame:frame display:NO];
            }
        }
        devjay_show_popup(window, parent);
    }];
}
void devjay_show_suggestions(void *pointer, void *parentPointer) {
    ((NSWindow *)pointer).identifier = @"suggested-actions";
    devjay_show_popup(pointer, parentPointer);
}

void devjay_show_viewer(void *pointer, void *parentPointer) {
    NSWindow *window = (NSWindow *)pointer;
    NSWindow *parent = (NSWindow *)parentPointer;
    window.identifier = @"git-viewer";
    NSRect frame = window.frame;
    NSRect area = (parent.screen ?: NSScreen.mainScreen).visibleFrame;
    frame.size.width = MIN(frame.size.width, area.size.width - 16);
    frame.size.height = MIN(frame.size.height, area.size.height - 16);
    [window setFrame:frame display:NO];
    devjay_show_popup(pointer, parentPointer);
}
