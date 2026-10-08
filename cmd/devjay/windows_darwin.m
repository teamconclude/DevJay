#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#import <WebKit/WebKit.h>
#import <QuartzCore/QuartzCore.h>
#import "focus_trace_darwin.h"
#include "layout_geometry.h"

// A tooltip must never become the app's remembered key/main window. In
// particular, hovering on an empty Space must not recreate the desktop bounce.
@interface DevJayAppTooltipPanel : NSPanel
@end
@implementation DevJayAppTooltipPanel
- (BOOL)canBecomeKeyWindow { return NO; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

static BOOL devjay_tooltip_payload(id payload, NSString **text, NSRect *rect) {
    if (![payload isKindOfClass:NSDictionary.class] ||
        ![payload[@"text"] isKindOfClass:NSString.class]) return NO;
    NSString *value = payload[@"text"];
    if (!value.length || value.length > 4096) return NO;
    for (NSString *key in @[@"x", @"y", @"width", @"height"]) {
        if (![payload[key] isKindOfClass:NSNumber.class] || !isfinite([payload[key] doubleValue])) return NO;
    }
    NSRect normalized = NSMakeRect([payload[@"x"] doubleValue], [payload[@"y"] doubleValue],
        [payload[@"width"] doubleValue], [payload[@"height"] doubleValue]);
    if (normalized.size.width <= 0 || normalized.size.height <= 0 ||
        normalized.size.width > 10 || normalized.size.height > 10) return NO;
    normalized = NSIntersectionRect(normalized, NSMakeRect(0, 0, 1, 1));
    if (NSIsEmptyRect(normalized)) return NO;
    *text = value;
    *rect = normalized;
    return YES;
}

static NSRect devjay_tooltip_frame(NSRect anchor, NSSize size, NSRect visible) {
    NSRect safe = NSInsetRect(visible, 8, 8);
    size.width = MIN(size.width, safe.size.width);
    size.height = MIN(size.height, safe.size.height);
    CGFloat x = NSMinX(anchor), y = NSMaxY(anchor) + 8;
    if (y + size.height > NSMaxY(safe)) y = NSMinY(anchor) - size.height - 8;
    return NSMakeRect(MAX(NSMinX(safe), MIN(x, NSMaxX(safe) - size.width)),
        MAX(NSMinY(safe), MIN(y, NSMaxY(safe) - size.height)), size.width, size.height);
}

// WebKit's CSS hover tracking can stop when its window is inactive. An
// always-active AppKit tracking area reports position without activating it.
@interface DevJayHoverTracker : NSObject {
@public
    WKWebView *view; // Owned by the window; this tracker is associated with it.
    NSPoint position;
    NSUInteger revision;
    BOOL pending;
    DevJayAppTooltipPanel *tooltip;
    NSString *tooltipText;
    NSRect tooltipRect;
    NSUInteger tooltipGeneration;
    BOOL tooltipReady;
    id eventMonitor;
}
- (void)publish;
- (void)hideTooltip;
- (void)invalidate:(NSNotification *)notification;
- (void)receiveTooltip:(id)payload;
- (void)showTooltip;
@end

@implementation DevJayHoverTracker
- (void)dealloc {
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:self];
    if (eventMonitor) [NSEvent removeMonitor:eventMonitor];
    [tooltip orderOut:nil];
    [tooltip release];
    [tooltipText release];
    [super dealloc];
}
- (void)hideTooltip {
    tooltipGeneration++;
    tooltipReady = NO;
    [tooltipText release]; tooltipText = nil;
    tooltipRect = NSZeroRect;
    if (tooltip.visible) [tooltip orderOut:nil];
}
- (void)invalidate:(NSNotification *)notification {
    revision++;
    position = NSMakePoint(-1, -1);
    [self hideTooltip];
    if ([notification.name isEqual:NSWindowWillCloseNotification]) view = nil;
}
- (BOOL)eligible {
    NSWindow *window = view.window;
    return window && window.visible && window.isOnActiveSpace && !window.miniaturized &&
        !view.loading && NSEvent.pressedMouseButtons == 0;
}
- (void)receiveTooltip:(id)payload {
    NSString *text = nil;
    NSRect rect;
    if (![self eligible] || !devjay_tooltip_payload(payload, &text, &rect)) {
        [self hideTooltip];
        return;
    }
    if (![text isEqual:tooltipText] || !NSEqualRects(rect, tooltipRect)) {
        [self hideTooltip];
        tooltipText = [text copy];
        tooltipRect = rect;
        NSUInteger generation = tooltipGeneration;
        // Retain the WebView in the delayed block, so its associated tracker
        // cannot outlive a dangling raw view pointer during window teardown.
        WKWebView *expected = view;
        dispatch_after(dispatch_time(DISPATCH_TIME_NOW, (int64_t)(0.4 * NSEC_PER_SEC)), dispatch_get_main_queue(), ^{
            if (view != expected || generation != tooltipGeneration || ![self eligible]) return;
            tooltipReady = YES;
            revision++;
            // Read the DOM again after the delay. A snapshot may have changed
            // or removed the target while the pointer was stationary.
            [self publish];
        });
    } else if (tooltipReady) {
        [self showTooltip];
    }
}
- (void)showTooltip {
    if (![self eligible] || !tooltipText.length) { [self hideTooltip]; return; }
    NSRect bounds = view.bounds;
    NSRect anchor = NSMakeRect(NSMinX(bounds) + tooltipRect.origin.x * bounds.size.width,
        view.isFlipped ? NSMinY(bounds) + tooltipRect.origin.y * bounds.size.height
                       : NSMaxY(bounds) - NSMaxY(tooltipRect) * bounds.size.height,
        tooltipRect.size.width * bounds.size.width, tooltipRect.size.height * bounds.size.height);
    NSWindow *window = view.window;
    anchor = [window convertRectToScreen:[view convertRect:anchor toView:nil]];
    if (!NSPointInRect(NSEvent.mouseLocation, anchor)) { [self hideTooltip]; return; }
    NSScreen *screen = window.screen;
    for (NSScreen *candidate in NSScreen.screens) {
        if (NSPointInRect(NSMakePoint(NSMidX(anchor), NSMidY(anchor)), candidate.frame)) { screen = candidate; break; }
    }
    if (!screen || screen.visibleFrame.size.width < 64 || screen.visibleFrame.size.height < 64) {
        [self hideTooltip]; return;
    }
    // The text and anchor are unchanged while this panel is visible. Avoid
    // rebuilding its content on every mouse-move callback within one icon.
    if (tooltip.visible) return;
    if (!tooltip) {
        tooltip = [[DevJayAppTooltipPanel alloc] initWithContentRect:NSZeroRect
            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
            backing:NSBackingStoreBuffered defer:NO];
        tooltip.identifier = @"app-status-tooltip";
        tooltip.releasedWhenClosed = NO;
        tooltip.hidesOnDeactivate = NO;
        tooltip.ignoresMouseEvents = YES;
        tooltip.excludedFromWindowsMenu = YES;
        tooltip.restorable = NO;
        tooltip.tabbingMode = NSWindowTabbingModeDisallowed;
        tooltip.animationBehavior = NSWindowAnimationBehaviorNone;
        tooltip.level = CGWindowLevelForKey(kCGHelpWindowLevelKey);
        tooltip.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
            NSWindowCollectionBehaviorStationary | NSWindowCollectionBehaviorIgnoresCycle |
            NSWindowCollectionBehaviorFullScreenNone;
        tooltip.opaque = NO;
        tooltip.backgroundColor = NSColor.clearColor;
        tooltip.hasShadow = YES;
    }
    NSTextField *label = [NSTextField wrappingLabelWithString:tooltipText];
    label.font = [NSFont systemFontOfSize:12];
    label.textColor = NSColor.labelColor;
    label.selectable = NO;
    label.cell.wraps = YES;
    label.cell.lineBreakMode = NSLineBreakByWordWrapping;
    // Short app labels should fit their content; retain a cap for long names.
    CGFloat naturalWidth = ceil([label.cell cellSizeForBounds:NSMakeRect(0, 0, CGFLOAT_MAX, CGFLOAT_MAX)].width);
    CGFloat width = MIN(MIN(380, screen.visibleFrame.size.width - 32), naturalWidth + 24);
    CGFloat height = ceil([label.cell cellSizeForBounds:NSMakeRect(0, 0, width - 24, CGFLOAT_MAX)].height) + 24;
    NSRect frame = devjay_tooltip_frame(anchor, NSMakeSize(width, height), screen.visibleFrame);
    NSView *content = [[[NSView alloc] initWithFrame:NSMakeRect(0, 0, frame.size.width, frame.size.height)] autorelease];
    content.wantsLayer = YES;
    content.layer.cornerRadius = 8;
    content.layer.backgroundColor = NSColor.windowBackgroundColor.CGColor;
    label.frame = NSInsetRect(content.bounds, 12, 12);
    [content addSubview:label];
    tooltip.contentView = content;
    [tooltip setFrame:frame display:YES];
    // This is intentionally the only ordering call. No activation, first
    // responder, key/main request, child-window link, or Space move is made.
    if (!tooltip.visible) [tooltip orderFrontRegardless];
}
- (void)publish {
    if (pending || !view) return;
    pending = YES;
    NSUInteger sent = revision;
    NSString *script = [NSString stringWithFormat:
        @"(()=>{const x=%.17g,y=%.17g;window.dispatchEvent(new CustomEvent('devjay:native-hover',{detail:{x,y}}));"
        "const n=document.elementFromPoint(x*innerWidth,y*innerHeight)?.closest('[data-app-tooltip]');"
        "if(!n)return null;const r=n.getBoundingClientRect();return {text:n.dataset.appTooltip,"
        "x:r.x/innerWidth,y:r.y/innerHeight,width:r.width/innerWidth,height:r.height/innerHeight};})()",
        position.x, position.y];
    // Keep the view alive until delivery finishes, including during closing.
    WKWebView *target = view;
    NSWindow *parent = target.window;
    [target evaluateJavaScript:script completionHandler:^(id result, NSError *error) {
        pending = NO;
        if (view == target && target.window == parent && sent == revision) [self receiveTooltip:error ? nil : result];
        if (sent != revision && view && view.window != nil) [self publish];
    }];
}
- (void)mouseMoved:(NSEvent *)event {
    if (!view) return;
    NSRect bounds = view.bounds;
    if (bounds.size.width <= 0 || bounds.size.height <= 0) return;
    NSPoint point = [view convertPoint:event.locationInWindow fromView:nil];
    position.x = (point.x - NSMinX(bounds)) / bounds.size.width;
    position.y = view.isFlipped ? (point.y - NSMinY(bounds)) / bounds.size.height
                               : (NSMaxY(bounds) - point.y) / bounds.size.height;
    if (!NSPointInRect(position, tooltipRect)) [self hideTooltip];
    revision++;
    [self publish];
}
- (void)mouseEntered:(NSEvent *)event { [self mouseMoved:event]; }
- (void)mouseExited:(NSEvent *)event {
    position = NSMakePoint(-1, -1);
    revision++;
    [self hideTooltip];
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

static const char devjay_hover_tracker_key;

// A frontend snapshot can replace a tooltip target beneath a stationary mouse.
// Only an already pending/visible tooltip needs a fresh native hit test.
void devjay_refresh_app_tooltip(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    WKWebView *webview = devjay_webview(window.contentView);
    DevJayHoverTracker *tracker = objc_getAssociatedObject(webview, &devjay_hover_tracker_key);
    if (!tracker || (!tracker->tooltipText && !tracker->pending)) return;
    tracker->revision++;
    [tracker publish];
}

void devjay_track_hover(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    WKWebView *view = devjay_webview(window.contentView);
    if (!view || objc_getAssociatedObject(view, &devjay_hover_tracker_key)) return;
    DevJayHoverTracker *tracker = [[[DevJayHoverTracker alloc] init] autorelease];
    tracker->view = view;
    objc_setAssociatedObject(view, &devjay_hover_tracker_key, tracker, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    NSTrackingArea *area = [[NSTrackingArea alloc] initWithRect:NSZeroRect
        options:NSTrackingMouseEnteredAndExited | NSTrackingMouseMoved | NSTrackingActiveAlways | NSTrackingInVisibleRect
        owner:tracker userInfo:nil];
    [view addTrackingArea:area];
    [area release];
    window.acceptsMouseMovedEvents = YES;
    for (NSNotificationName name in @[NSWindowWillCloseNotification, NSWindowDidMoveNotification,
        NSWindowDidResizeNotification, NSWindowDidMiniaturizeNotification,
        NSWindowDidChangeScreenNotification, NSWindowDidChangeOcclusionStateNotification]) {
        [NSNotificationCenter.defaultCenter addObserver:tracker selector:@selector(invalidate:) name:name object:window];
    }
    for (NSNotificationName name in @[NSWorkspaceActiveSpaceDidChangeNotification, NSWorkspaceDidActivateApplicationNotification,
        NSWorkspaceWillSleepNotification,
        NSWorkspaceScreensDidSleepNotification, NSWorkspaceSessionDidResignActiveNotification]) {
        [NSWorkspace.sharedWorkspace.notificationCenter addObserver:tracker selector:@selector(invalidate:) name:name object:nil];
    }
    // Under manual retain/release, __block does not retain the tracker. Remove
    // the monitor in dealloc instead of creating a tracker/monitor retain cycle.
    __block DevJayHoverTracker *unretainedTracker = tracker;
    tracker->eventMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown |
        NSEventMaskRightMouseDown | NSEventMaskOtherMouseDown | NSEventMaskScrollWheel handler:^NSEvent *(NSEvent *event) {
            if (event.window == unretainedTracker->view.window) [unretainedTracker invalidate:nil];
            return event;
        }];
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
static void devjay_show_context_window(NSWindow *window, NSWindow *parent, double x, double y, double width, double height, BOOL viewer) {
    NSRect anchor = parent.frame;
    BOOL cardParent = [parent.identifier isEqual:@"control-panel"];
    BOOL controlAnchor = NO;
    WKWebView *view = parent ? devjay_webview(parent.contentView) : nil;
    if (view && isfinite(x) && isfinite(y) && isfinite(width) && isfinite(height) && width > 0 && height > 0) {
        // Browser coordinates are top-left based even when AppKit is not.
        NSRect rect = NSMakeRect(x, view.isFlipped ? y : view.bounds.size.height-y-height, width, height);
        rect = NSIntersectionRect(rect, view.bounds);
        if (!NSIsEmptyRect(rect)) {
            anchor = [parent convertRectToScreen:[view convertRect:rect toView:nil]];
            controlAnchor = !viewer && [parent.identifier isEqual:@"git-viewer"];
        }
    }
    NSScreen *screen = parent.screen ?: NSScreen.mainScreen;
    CGFloat overlap = 0;
    for (NSScreen *candidate in NSScreen.screens) {
        NSRect intersection = NSIntersectionRect(anchor, candidate.frame);
        CGFloat area = intersection.size.width * intersection.size.height;
        if (area > overlap) { overlap = area; screen = candidate; }
    }
    NSRect area = screen.visibleFrame, frame = window.frame;
    if (cardParent) {
        DPRect placed = dp_card_popup((DPRect){frame.origin.x,frame.origin.y,frame.size.width,frame.size.height},
                                     (DPRect){anchor.origin.x,anchor.origin.y,anchor.size.width,anchor.size.height},
                                     (DPRect){area.origin.x,area.origin.y,area.size.width,area.size.height});
        frame = NSMakeRect(placed.x,placed.y,placed.width,placed.height);
    } else if (parent) {
        NSRect owner = parent.frame;
        DPRect control = controlAnchor ? (DPRect){anchor.origin.x,anchor.origin.y,anchor.size.width,anchor.size.height} : (DPRect){0};
        DPRect placed = dp_parent_popup((DPRect){frame.origin.x,frame.origin.y,frame.size.width,frame.size.height},
                                       (DPRect){owner.origin.x,owner.origin.y,owner.size.width,owner.size.height},
                                       control,(DPRect){area.origin.x,area.origin.y,area.size.width,area.size.height});
        frame = NSMakeRect(placed.x,placed.y,placed.width,placed.height);
    } else {
        frame.origin = NSMakePoint(NSMidX(area)-frame.size.width/2,NSMidY(area)-frame.size.height/2);
    }
    [window setFrame:frame display:NO];
    window.collectionBehavior = NSWindowCollectionBehaviorMoveToActiveSpace | NSWindowCollectionBehaviorFullScreenNone;
    window.level = [NSUserDefaults.standardUserDefaults boolForKey:@"DevJay.View.OnTop"] ? NSFloatingWindowLevel : NSNormalWindowLevel;
    if (window.isMiniaturized) [window deminiaturize:nil];
    [window makeKeyAndOrderFront:nil];
    if (window.canBecomeMainWindow) [window makeMainWindow];
    [NSApp activateIgnoringOtherApps:YES];
}

void devjay_show_suggestions(void *pointer, void *parentPointer, double x, double y, double width, double height) {
    NSWindow *window = (NSWindow *)pointer;
    window.identifier = @"suggested-actions";
    devjay_show_context_window(window,(NSWindow *)parentPointer,x,y,width,height,NO);
}

void devjay_show_viewer(void *pointer, void *parentPointer, double x, double y, double width, double height) {
    NSWindow *window = (NSWindow *)pointer;
    window.identifier = @"git-viewer";
    devjay_show_context_window(window,(NSWindow *)parentPointer,x,y,width,height,YES);
}
