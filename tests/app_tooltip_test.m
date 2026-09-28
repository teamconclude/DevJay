#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#import <objc/runtime.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/focus_trace_darwin.m"
#include "../cmd/devjay/windows_darwin.m"

@interface AppTooltipTestView : WKWebView {
@public
    NSMutableArray *callbacks;
    BOOL loadingForTest;
}
- (void)complete:(id)payload error:(NSError *)error;
@end
@implementation AppTooltipTestView
- (void)evaluateJavaScript:(NSString *)script completionHandler:(void (^)(id, NSError *))completion {
    assert([script containsString:@"data-app-tooltip"]);
    if (!callbacks) callbacks = [[NSMutableArray alloc] init];
    [callbacks addObject:[[completion copy] autorelease]];
}
- (void)complete:(id)payload error:(NSError *)error {
    assert(callbacks.count > 0);
    void (^completion)(id, NSError *) = [[callbacks objectAtIndex:0] copy];
    [callbacks removeObjectAtIndex:0];
    completion(payload, error);
    [completion release];
}
- (NSRect)convertRect:(NSRect)rect toView:(NSView *)other { return rect; }
- (BOOL)isLoading { return loadingForTest; }
- (void)dealloc { [callbacks release]; [super dealloc]; }
@end

@interface AppTooltipTestTracker : DevJayHoverTracker {
@public
    NSUInteger shows;
}
@end
@implementation AppTooltipTestTracker
- (BOOL)eligible { return YES; }
- (void)showTooltip { shows++; }
@end

@interface AppTooltipTestScreen : NSScreen
@end
@implementation AppTooltipTestScreen
- (NSRect)frame { return NSMakeRect(-1200, -800, 1200, 800); }
- (NSRect)visibleFrame { return NSMakeRect(-1200, -800, 1200, 800); }
@end

static NSScreen *testScreen;
@interface AppTooltipTestWindow : NSWindow {
@public
    BOOL visibleForTest, activeSpaceForTest, miniaturizedForTest;
}
@end
@implementation AppTooltipTestWindow
- (NSScreen *)screen { return testScreen; }
- (NSRect)convertRectToScreen:(NSRect)rect { return NSOffsetRect(rect, -1000, -600); }
- (BOOL)isVisible { return visibleForTest; }
- (BOOL)isOnActiveSpace { return activeSpaceForTest; }
- (BOOL)isMiniaturized { return miniaturizedForTest; }
@end

@interface AppTooltipPanelTracker : DevJayHoverTracker
@end
@implementation AppTooltipPanelTracker
- (BOOL)eligible { return YES; }
@end

static AppTooltipTestView *testView(void) {
    NSWindow *window = [[[AppTooltipTestWindow alloc] initWithContentRect:NSMakeRect(0, 0, 400, 200)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    WKWebViewConfiguration *configuration = [[[WKWebViewConfiguration alloc] init] autorelease];
    configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
    AppTooltipTestView *view = [[[AppTooltipTestView alloc] initWithFrame:NSMakeRect(0, 0, 400, 200)
        configuration:configuration] autorelease];
    window.contentView = view;
    return view;
}

static AppTooltipTestTracker *testTracker(AppTooltipTestView *view) {
    AppTooltipTestTracker *tracker = [[[AppTooltipTestTracker alloc] init] autorelease];
    tracker->view = view;
    tracker->position = NSMakePoint(.2, .2);
    return tracker;
}

static NSDictionary *payload(NSString *text) {
    return @{@"text": text, @"x": @.1, @"y": @.1, @"width": @.6, @"height": @.5};
}

static void test_payload_and_geometry(void) {
    NSString *text = nil;
    NSRect rect;
    assert(devjay_tooltip_payload(payload(@"Claude Code · iTerm2"), &text, &rect));
    assert([text isEqual:@"Claude Code · iTerm2"]);
    assert(NSEqualRects(rect, NSMakeRect(.1, .1, .6, .5)));
    for (id invalid in @[NSNull.null, @"text", @{}, payload(@""),
        payload([@"x" stringByPaddingToLength:4097 withString:@"x" startingAtIndex:0])]) {
        assert(!devjay_tooltip_payload(invalid, &text, &rect));
    }
    for (NSDictionary *change in @[@{@"x": @(NAN)}, @{@"y": @(INFINITY)}, @{@"x": @"0.1"},
        @{@"width": @0}, @{@"height": @-1}, @{@"width": @11}, @{@"x": @2}, @{@"y": @-2}]) {
        NSMutableDictionary *invalid = [[payload(@"Agent") mutableCopy] autorelease];
        [invalid addEntriesFromDictionary:change];
        assert(!devjay_tooltip_payload(invalid, &text, &rect));
    }
    NSMutableDictionary *partial = [[payload(@"Agent") mutableCopy] autorelease];
    partial[@"x"] = @-.2;
    assert(devjay_tooltip_payload(partial, &text, &rect));
    assert(rect.origin.x == 0 && fabs(rect.size.width - .4) < 1e-12);

    NSRect screen = NSMakeRect(-1200, -800, 1200, 800), safe = NSInsetRect(screen, 8, 8);
    NSRect frame = devjay_tooltip_frame(NSMakeRect(-1150, -700, 100, 30), NSMakeSize(380, 80), screen);
    assert(NSEqualRects(frame, NSMakeRect(-1150, -662, 380, 80)));
    frame = devjay_tooltip_frame(NSMakeRect(-100, -40, 90, 20), NSMakeSize(380, 80), screen);
    assert(NSEqualRects(frame, NSMakeRect(-388, -128, 380, 80)));
    for (NSValue *anchor in @[[NSValue valueWithRect:NSMakeRect(-3000, -3000, 50, 20)],
        [NSValue valueWithRect:NSMakeRect(1000, 1000, 50, 20)]]) {
        frame = devjay_tooltip_frame(anchor.rectValue, NSMakeSize(2000, 2000), screen);
        assert(NSEqualRects(frame, safe));
    }
}

static void test_latest_result_and_invalidations(void) {
    AppTooltipTestView *view = testView();
    AppTooltipTestTracker *tracker = testTracker(view);
    [tracker publish];
    [tracker publish];
    assert(view->callbacks.count == 1 && tracker->pending);
    [view complete:payload(@"Claude Code") error:nil];
    assert([tracker->tooltipText isEqual:@"Claude Code"] && tracker->shows == 0);
    tracker->tooltipReady = YES;
    [tracker publish];
    [view complete:payload(@"Claude Code") error:nil];
    assert(tracker->shows == 1);
    [tracker hideTooltip];
    assert(!tracker->tooltipText && !tracker->tooltipReady);

    // Exiting while WebKit is reading must clear immediately and reject the
    // old result. The coalesced replacement publishes the outside position.
    [tracker publish];
    [tracker mouseExited:nil];
    [view complete:payload(@"Stale exit") error:nil];
    assert(!tracker->tooltipText && tracker->shows == 1 && view->callbacks.count == 1);
    [view complete:nil error:nil];
    assert(!tracker->pending && !tracker->tooltipText);

    for (NSNotificationName name in @[NSWorkspaceActiveSpaceDidChangeNotification,
        NSWindowDidMoveNotification, NSWindowDidResizeNotification, NSWindowDidMiniaturizeNotification]) {
        [tracker publish];
        [tracker invalidate:[NSNotification notificationWithName:name object:view.window]];
        [view complete:payload(@"Stale invalidation") error:nil];
        assert(!tracker->tooltipText && view->callbacks.count == 1 && tracker->shows == 1);
        [view complete:nil error:nil];
    }

    [tracker publish];
    [view complete:payload(@"Ignored error") error:[NSError errorWithDomain:@"test" code:1 userInfo:nil]];
    assert(!tracker->tooltipText && !tracker->pending);

    // A different DOM result after the delay starts a new delay; it must not
    // display the old text just because an earlier target became ready.
    [tracker receiveTooltip:payload(@"Old agent")];
    tracker->tooltipReady = YES;
    [tracker publish];
    [view complete:payload(@"New agent") error:nil];
    assert([tracker->tooltipText isEqual:@"New agent"] && !tracker->tooltipReady && tracker->shows == 1);
    [tracker hideTooltip];

    [tracker publish];
    [tracker invalidate:[NSNotification notificationWithName:NSWindowWillCloseNotification object:view.window]];
    [view complete:payload(@"Closed window") error:nil];
    assert(!tracker->view && !tracker->tooltipText && !tracker->pending && view->callbacks.count == 0);

    view = testView();
    tracker = testTracker(view);
    [tracker publish];
    [view removeFromSuperview];
    [view complete:payload(@"Detached view") error:nil];
    assert(!tracker->tooltipText && !tracker->pending && view->callbacks.count == 0);
    [tracker invalidate:nil];
}

static NSUInteger frontOrders, keyRequests, mainRequests, activations;
static NSUInteger pressedButtons;
static void orderTooltip(id object, SEL selector) { frontOrders++; }
static void keyTooltip(id object, SEL selector, id sender) { keyRequests++; }
static void mainTooltip(id object, SEL selector) { mainRequests++; }
static void activateApp(id object, SEL selector, BOOL ignore) { activations++; }
static NSPoint fixedMouseLocation(id object, SEL selector) { return NSMakePoint(-900, -550); }
static NSUInteger fixedPressedButtons(id object, SEL selector) { return pressedButtons; }

static void test_window_eligibility(void) {
    AppTooltipTestView *view = testView();
    AppTooltipTestWindow *window = (AppTooltipTestWindow *)view.window;
    DevJayHoverTracker *tracker = [[[DevJayHoverTracker alloc] init] autorelease];
    tracker->view = view;
    Method buttons = class_getClassMethod(NSEvent.class, @selector(pressedMouseButtons));
    IMP originalButtons = method_setImplementation(buttons, (IMP)fixedPressedButtons);
    assert(![tracker eligible]);
    window->visibleForTest = YES;
    assert(![tracker eligible]);
    window->activeSpaceForTest = YES;
    assert([tracker eligible]);
    window->miniaturizedForTest = YES;
    assert(![tracker eligible]);
    window->miniaturizedForTest = NO;
    view->loadingForTest = YES;
    assert(![tracker eligible]);
    view->loadingForTest = NO;
    pressedButtons = 1;
    assert(![tracker eligible]);
    pressedButtons = 0;
    assert([tracker eligible]);
    window->activeSpaceForTest = NO;
    tracker->tooltipText = [@"Old tooltip" copy];
    [tracker receiveTooltip:payload(@"Off-Space tooltip")];
    assert(!tracker->tooltipText);
    method_setImplementation(buttons, originalButtons);
}

static void overrideTooltip(SEL selector, IMP implementation) {
    Method method = class_getInstanceMethod(DevJayAppTooltipPanel.class, selector);
    class_addMethod(DevJayAppTooltipPanel.class, selector, implementation, method_getTypeEncoding(method));
}

static void test_panel_never_activates(void) {
    AppTooltipTestView *view = testView();
    testScreen = [[[AppTooltipTestScreen alloc] init] autorelease];
    Method mouse = class_getClassMethod(NSEvent.class, @selector(mouseLocation));
    IMP originalMouse = method_setImplementation(mouse, (IMP)fixedMouseLocation);
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP originalActivation = method_setImplementation(activation, (IMP)activateApp);
    overrideTooltip(@selector(orderFrontRegardless), (IMP)orderTooltip);
    overrideTooltip(@selector(makeKeyAndOrderFront:), (IMP)keyTooltip);
    overrideTooltip(@selector(makeMainWindow), (IMP)mainTooltip);
    AppTooltipPanelTracker *tracker = [[[AppTooltipPanelTracker alloc] init] autorelease];
    tracker->view = view;
    tracker->tooltipText = [@"GoLand with Claude Code and Codex" copy];
    tracker->tooltipRect = NSMakeRect(.1, .1, .6, .5);
    [tracker showTooltip];
    DevJayAppTooltipPanel *panel = tracker->tooltip;
    assert(panel && frontOrders == 1);
    assert(keyRequests == 0 && mainRequests == 0 && activations == 0);
    assert(!panel.canBecomeKeyWindow && !panel.canBecomeMainWindow);
    assert(panel.styleMask & NSWindowStyleMaskNonactivatingPanel);
    assert(panel.ignoresMouseEvents && !panel.hidesOnDeactivate && panel.excludedFromWindowsMenu);
    assert(panel.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces);
    assert(panel.collectionBehavior & NSWindowCollectionBehaviorStationary);
    assert(panel.collectionBehavior & NSWindowCollectionBehaviorIgnoresCycle);
    assert(!(panel.collectionBehavior & (NSWindowCollectionBehaviorMoveToActiveSpace |
        NSWindowCollectionBehaviorManaged | NSWindowCollectionBehaviorParticipatesInCycle)));
    assert(!panel.parentWindow && !panel.isVisible);
    assert(panel.frame.size.width < 380);
    NSTextField *label = panel.contentView.subviews.firstObject;
    assert(label && [label.stringValue isEqual:tracker->tooltipText]);
    NSSize naturalSize = [label.cell cellSizeForBounds:NSMakeRect(0, 0, CGFLOAT_MAX, CGFLOAT_MAX)];
    assert(label.frame.size.width >= ceil(naturalSize.width));
    assert(panel.frame.size.height == ceil(naturalSize.height) + 24); // No accidental second line from field insets.
    CGFloat shortHeight = panel.frame.size.height;
    [tracker hideTooltip];
    tracker->tooltipText = [[@"Long application name " stringByPaddingToLength:240 withString:@"and another agent " startingAtIndex:0] copy];
    tracker->tooltipRect = NSMakeRect(.1, .1, .6, .5);
    [tracker showTooltip];
    assert(tracker->tooltip.frame.size.width == 380);
    assert(tracker->tooltip.frame.size.height > shortHeight);
    assert(keyRequests == 0 && mainRequests == 0 && activations == 0);
    [tracker hideTooltip];
    method_setImplementation(mouse, originalMouse);
    method_setImplementation(activation, originalActivation);
    testScreen = nil;
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        test_payload_and_geometry();
        test_latest_result_and_invalidations();
        test_window_eligibility();
        test_panel_never_activates();
        // Every scheduled generation was invalidated. Let delayed blocks run
        // before releasing fixture views so regressions surface in this test.
        NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:.5];
        while ([deadline timeIntervalSinceNow] > 0) {
            [NSRunLoop.currentRunLoop runMode:NSDefaultRunLoopMode beforeDate:deadline];
        }
        puts("Native app tooltip regressions passed");
    }
    return 0;
}
