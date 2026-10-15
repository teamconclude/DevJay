#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/focus_trace_darwin.m"
#include "../cmd/devjay/windows_darwin.m"
#include "../cmd/devjay/spaces_darwin.m"
#include "../cmd/devjay/layout_darwin.m"
#include "../cmd/devjay/editors_darwin.m"

static BOOL gotPathError;
void devjay_editor_opened(unsigned long long token, const char *error) {
    assert(token == 42);
    gotPathError = error && strstr(error, "UTF-8");
}
static int tag = 73, movedWithTag, moveResult;
static int readTag(int connection, uint64_t sid) { return tag; }
static int writeTag(int connection, uint64_t sid, int value) { tag = value; return 0; }
static int moveWindow(int connection, uint32_t *window, int count, int value) { movedWithTag = tag; return moveResult; }

@interface DevJayStripTestPanel : NSPanel {
@public
    NSUInteger orders, keyOrders;
}
@end
@implementation DevJayStripTestPanel
- (BOOL)canBecomeKeyWindow { return YES; } // Wails' default panel behavior.
- (void)orderFrontRegardless { orders++; }
- (void)makeKeyAndOrderFront:(id)sender { keyOrders++; }
@end

@interface DevJaySiteTestWindow : NSWindow {
@public
    NSUInteger keyOrders, mainRequests;
}
@end
@implementation DevJaySiteTestWindow
- (BOOL)canBecomeMainWindow { return YES; }
- (void)makeKeyAndOrderFront:(id)sender { keyOrders++; }
- (void)makeMainWindow { mainRequests++; }
@end

@interface DevJayPlacementTestWindow : DevJaySiteTestWindow {
@public
    BOOL visibleForPlacement, minimizedForPlacement, activeForPlacement;
}
@end
@implementation DevJayPlacementTestWindow
- (BOOL)isVisible { return visibleForPlacement; }
- (BOOL)isMiniaturized { return minimizedForPlacement; }
- (BOOL)isOnActiveSpace { return activeForPlacement; }
@end

static NSUInteger activationRequests;
static void recordActivation(id app, SEL selector, BOOL ignoringOthers) { activationRequests++; }

static void test_strip_focus_isolation(void) {
    IMP panelBaseIMP = class_getMethodImplementation(NSPanel.class, @selector(canBecomeKeyWindow));
    IMP windowBaseIMP = class_getMethodImplementation(NSWindow.class, @selector(canBecomeKeyWindow));
    DevJayStripTestPanel *strip = [[[DevJayStripTestPanel alloc]
        initWithContentRect:NSMakeRect(0, 0, 300, 100)
        styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    DevJayStripTestPanel *other = [[[DevJayStripTestPanel alloc]
        initWithContentRect:NSMakeRect(0, 0, 300, 100)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskNonactivatingPanel
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    strip.identifier = @"control-panel";
    strip.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorManaged | NSWindowCollectionBehaviorParticipatesInCycle;
    Class nativeClass = object_getClass(strip);
    assert(strip.canBecomeKeyWindow && other.canBecomeKeyWindow);
    assert(devjay_make_strip_mouse_only(strip));
    assert(devjay_make_strip_mouse_only(strip));
    assert(object_getClass(strip) == nativeClass);
    assert(!strip.canBecomeKeyWindow && !strip.canBecomeMainWindow);
    assert(other.canBecomeKeyWindow);
    assert(strip.styleMask & NSWindowStyleMaskNonactivatingPanel);
    assert(strip.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces);
    assert(strip.collectionBehavior & NSWindowCollectionBehaviorStationary);
    assert(strip.collectionBehavior & NSWindowCollectionBehaviorIgnoresCycle);
    assert(!(strip.collectionBehavior & (NSWindowCollectionBehaviorManaged |
        NSWindowCollectionBehaviorTransient | NSWindowCollectionBehaviorParticipatesInCycle)));
    assert(panelBaseIMP == class_getMethodImplementation(NSPanel.class, @selector(canBecomeKeyWindow)));
    assert(windowBaseIMP == class_getMethodImplementation(NSWindow.class, @selector(canBecomeKeyWindow)));

    DevJaySiteTestWindow *site = [[[DevJaySiteTestWindow alloc]
        initWithContentRect:NSMakeRect(0, 0, 100, 100) styleMask:NSWindowStyleMaskTitled
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    assert(!devjay_make_strip_mouse_only((NSPanel *)site));
    assert(site.canBecomeKeyWindow && site.canBecomeMainWindow);
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation, (IMP)recordActivation);
    devjay_focus_site(strip);
    assert(strip->orders == 1 && strip->keyOrders == 0 && activationRequests == 0);
    devjay_focus_site(site);
    assert(site->keyOrders == 1 && site->mainRequests == 1 && activationRequests == 1);
    method_setImplementation(activation, original);
}

static void test_confirmation_card_anchor(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    if (area.size.width < 1000 || area.size.height < 550) return;
    NSWindow *strip = [[[NSWindow alloc] initWithContentRect:NSMakeRect(area.origin.x+30,area.origin.y+10,900,174)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    strip.identifier = @"control-panel";
    WKWebView *view = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,900,174)] autorelease];
    strip.contentView = view;
    DevJaySiteTestWindow *popup = [[[DevJaySiteTestWindow alloc] initWithContentRect:NSMakeRect(0,0,460,330)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation, (IMP)recordActivation);
    devjay_show_suggestions(popup,strip,400,10,200,140);
    // Browser's top-left rectangle becomes a screen rectangle above the strip.
    // Center on this card (x=530), not on the entire strip (x=480).
    assert(fabs(popup.frame.origin.x-(area.origin.x+300)) < 1);
    assert(fabs(popup.frame.origin.y-(area.origin.y+184)) < 1);
    assert(popup->keyOrders == 1 && popup->mainRequests == 1);
    // A second card in the same strip must produce a different popup anchor.
    devjay_show_suggestions(popup,strip,200,10,200,140);
    assert(fabs(popup.frame.origin.x-(area.origin.x+100)) < 1);
    assert(fabs(popup.frame.origin.y-(area.origin.y+184)) < 1);
    method_setImplementation(activation, original);
}

static void test_confirmation_site_center(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    if (area.size.width < 500 || area.size.height < 500) return;
    NSWindow *site = [[[NSWindow alloc] initWithContentRect:NSMakeRect(NSMidX(area)-145,NSMidY(area)-87,290,174)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    site.identifier = @"site-confirmation-test";
    site.contentView = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,290,174)] autorelease];
    DevJaySiteTestWindow *popup = [[[DevJaySiteTestWindow alloc] initWithContentRect:NSMakeRect(0,0,460,330)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation, (IMP)recordActivation);
    // Button requests omit bounds; context menus supply them. Both must center
    // over the Site window, including when the bounds are off-center within it.
    for (NSNumber *width in @[@0, @100]) {
        devjay_show_suggestions(popup,site,10,10,width.doubleValue,100);
        assert(fabs(NSMidX(popup.frame)-NSMidX(site.frame)) < 1);
        assert(fabs(NSMidY(popup.frame)-NSMidY(site.frame)) < 1);
        assert([popup.identifier isEqual:@"suggested-actions"]);
    }
    assert(popup->keyOrders == 2 && popup->mainRequests == 2);
    // A Changes/History viewer opened from the small Site overlays it too.
    devjay_show_viewer(popup,site,10,10,100,100);
    assert(fabs(NSMidX(popup.frame)-NSMidX(site.frame)) < 1);
    assert(fabs(NSMidY(popup.frame)-NSMidY(site.frame)) < 1);
    // Keep a centered confirmation reachable when its Site is at an edge.
    [site setFrameOrigin:NSMakePoint(NSMaxX(area)-290,NSMinY(area))];
    devjay_show_suggestions(popup,site,0,0,290,174);
    assert(NSContainsRect(area,popup.frame));
    assert(fabs(NSMaxX(popup.frame)-(NSMaxX(area)-8)) < 1);
    assert(fabs(NSMinY(popup.frame)-(NSMinY(area)+8)) < 1);
    method_setImplementation(activation, original);
}

static void test_viewer_placement(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    if (area.size.width < 1000 || area.size.height < 550) return;
    NSWindow *parent = [[[NSWindow alloc] initWithContentRect:NSMakeRect(area.origin.x+30,area.origin.y+10,900,174)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    parent.identifier = @"control-panel";
    parent.contentView = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,900,174)] autorelease];
    DevJayPlacementTestWindow *other = [[[DevJayPlacementTestWindow alloc] initWithContentRect:NSMakeRect(area.origin.x+300,area.origin.y+184,460,330)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    other.identifier = @"git-viewer"; other->visibleForPlacement = YES; other->activeForPlacement = YES;
    DevJaySiteTestWindow *popup = [[[DevJaySiteTestWindow alloc] initWithContentRect:NSMakeRect(0,0,460,330)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation,(IMP)recordActivation);
    devjay_show_viewer(popup,parent,400,10,200,140);
    // Another viewer must not displace this one from its source card.
    assert(NSEqualRects(popup.frame,other.frame));
    assert(NSContainsRect(area,popup.frame));
    assert([popup.identifier isEqual:@"git-viewer"]);
    // Reopening the same native viewer from another card updates its anchor.
    devjay_show_viewer(popup,parent,200,10,200,140);
    assert(fabs(NSMinX(popup.frame)-(area.origin.x+100)) < 1);
    assert(fabs(NSMinY(popup.frame)-(area.origin.y+184)) < 1);
    method_setImplementation(activation,original);
    // Keep this artificial visible obstacle out of later harness checks.
    other->visibleForPlacement = NO;
}

static void test_switch_placement(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    if (area.size.width < 1200 || area.size.height < 850) return;
    NSWindow *parent = [[[NSWindow alloc] initWithContentRect:NSMakeRect(NSMidX(area)-500,NSMidY(area)-350,1000,700)
        styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO] autorelease];
    parent.identifier = @"git-viewer";
    parent.contentView = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,1000,700)] autorelease];
    DevJaySiteTestWindow *popup = [[[DevJaySiteTestWindow alloc] initWithContentRect:NSMakeRect(0,0,620,740)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation, (IMP)recordActivation);
    devjay_show_suggestions(popup,parent,100,20,70,26);
    assert(fabs(NSMinX(popup.frame)-(NSMinX(parent.frame)+180)) < 1);
    assert(fabs(NSMidY(popup.frame)-NSMidY(parent.frame)) < 1);
    // Ordinary confirmations and invalid/out-of-view button bounds center.
    for (NSNumber *x in @[@0, @2000]) {
        devjay_show_suggestions(popup,parent,x.doubleValue,20,x.doubleValue ? 70 : 0,26);
        assert(fabs(NSMidX(popup.frame)-NSMidX(parent.frame)) < 1);
        assert(fabs(NSMidY(popup.frame)-NSMidY(parent.frame)) < 1);
    }
    method_setImplementation(activation, original);
}

static void test_card_height_refit(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    NSWindow *panel = [[[NSWindow alloc] initWithContentRect:NSMakeRect(area.origin.x+20, area.origin.y+10, 500, 300)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    panel.identifier = @"control-panel";
    NSRect original = panel.frame;
    devjay_fit_card_window(panel, 160, 500, 300, 0); // Grow-only requests cannot shrink.
    assert(NSEqualRects(panel.frame, original));
    devjay_fit_card_window(panel, 160, 400, 300, 1); // Reject pre-resize measurements.
    assert(NSEqualRects(panel.frame, original));
    devjay_fit_card_window(panel, 160, NAN, 300, 1);
    assert(NSEqualRects(panel.frame, original));
    devjay_fit_card_window(panel, 160, 500, 300, 1);
    assert(NSHeight(panel.frame) == 160 && NSWidth(panel.frame) == 500);
    assert(NSMinY(panel.frame) == NSMinY(original));
    devjay_fit_card_window(panel, 1, 500, 160, 1);
    assert(NSHeight(panel.frame) == 132);
    panel.identifier = @"site-height-test";
    devjay_fit_card_window(panel, 100, 500, 132, 1); // Site windows remain grow-only even with this flag.
    assert(NSHeight(panel.frame) == 132);
    devjay_fit_card_window(panel, 220, 500, 132, 0);
    assert(NSHeight(panel.frame) == 220 && NSMinY(panel.frame) == NSMinY(original));
    panel.identifier = @"control-panel";
    panel.styleMask = NSWindowStyleMaskTitled;
    [panel setContentSize:NSMakeSize(500, 300)];
    devjay_fit_card_window(panel, 160, 500, 300, 1);
    assert([panel contentRectForFrameRect:panel.frame].size.height == 160);
    assert(NSHeight(panel.frame) > 160); // Native chrome is added once.
}

static void test_profile_layout_isolation(void) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    NSDictionary *legacy = [[defaults dictionaryForKey:@"DevJay.WindowLayout.v1"] retain];
    NSDictionary *initial = @{@"control-panel": @{@"frame": @"{{20, 20}, {500, 180}}"}};
    [defaults setObject:initial forKey:@"DevJay.WindowLayout.v1"];
    NSString *signature = display_signature();
    assert(signature.length);
    NSString *a = @"DevJay.WindowLayout.Profile.test-home", *b = @"DevJay.WindowLayout.Profile.test-cabin";
    [defaults removeObjectForKey:a]; [defaults removeObjectForKey:b];
    devjay_profile_activate("test-home",signature.UTF8String,1);
    assert([[defaults dictionaryForKey:a] isEqual:initial]);
    NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(50,60,290,174)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    window.identifier = @"site-profile-fixture";
    DevJayLayoutObserver *observer = [[[DevJayLayoutObserver alloc] init] autorelease];observer->window=window;
    objc_setAssociatedObject(window, &layoutObserverKey, observer, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    [observer save];
    assert(devjay_profile_capture(window));
    NSDictionary *home = [[defaults dictionaryForKey:a] copy];
    devjay_profile_pause(1);
    assert(!devjay_profile_capture(window));
    [window setFrameOrigin:NSMakePoint(90,100)];[observer save];
    assert([[defaults dictionaryForKey:a] isEqual:home]);
    devjay_profile_pause(0);
    [profileDisplaySignature release];profileDisplaySignature=[@"disconnected-display-set" copy];
    [observer save];assert([[defaults dictionaryForKey:a] isEqual:home]);
    devjay_profile_activate("test-cabin",signature.UTF8String,0);
    assert(!observer->window); // Delayed outgoing callbacks cannot write Cabin.
    [observer save]; assert(![[defaults dictionaryForKey:b] objectForKey:window.identifier]);
    observer = [[[DevJayLayoutObserver alloc] init] autorelease]; observer->window=window;
    objc_setAssociatedObject(window, &layoutObserverKey, observer, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    [observer save];
    assert([[defaults dictionaryForKey:b] objectForKey:window.identifier]);
    assert([[defaults dictionaryForKey:a] isEqual:home]);
    assert([[defaults dictionaryForKey:@"DevJay.WindowLayout.v1"] isEqual:initial]);
    devjay_profile_delete("test-cabin"); // The active layout is protected even at the native boundary.
    assert([[defaults dictionaryForKey:b] objectForKey:window.identifier]);
    devjay_profile_delete("test-home");
    assert(![defaults objectForKey:a]);
    assert([[defaults dictionaryForKey:@"DevJay.WindowLayout.v1"] isEqual:initial]);
    [observer stopObserving];[home release];
    [defaults removeObjectForKey:a];[defaults removeObjectForKey:b];
    if(legacy)[defaults setObject:legacy forKey:@"DevJay.WindowLayout.v1"];else[defaults removeObjectForKey:@"DevJay.WindowLayout.v1"];
    [legacy release];[layoutKey release];layoutKey=@"DevJay.WindowLayout.v1";
    [profileDisplaySignature release];profileDisplaySignature=nil;profileLayoutPaused=NO;

    NSArray *displays=@[@{@"Display Identifier":@"left",@"Spaces":@[@{@"type":@0,@"uuid":@"one"},@{@"type":@4,@"uuid":@"full-screen"},@{@"type":@0,@"uuid":@"two"}]},
        @{@"Display Identifier":@"right",@"Spaces":@[@{@"type":@0,@"uuid":@"right-one"}]}];
    assert([space_at_index(displays,@"left",1) isEqual:@"two"]);
    assert([space_at_index(displays,@"right",0) isEqual:@"right-one"]);
    assert(!space_at_index(displays,@"left",2));assert(!space_at_index(displays,@"missing",0));
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        test_strip_focus_isolation();
        test_confirmation_card_anchor();
        test_confirmation_site_center();
        test_viewer_placement();
        test_switch_placement();
        test_card_height_refit();
        test_profile_layout_isolation();
        NSView *panel = [[[NSView alloc] init] autorelease];
        NSView *confirmation = [[[NSView alloc] init] autorelease];
        IMP original = class_getMethodImplementation(NSView.class, @selector(acceptsFirstMouse:));
        assert(![confirmation acceptsFirstMouse:nil]);
        assert(!devjay_enable_first_mouse(panel));
        assert(![panel acceptsFirstMouse:nil]);
        assert(![confirmation acceptsFirstMouse:nil]);
        assert(original == class_getMethodImplementation(NSView.class, @selector(acceptsFirstMouse:)));

        // A plain NSView misses WKWebView's computed corner-radius properties.
        // Changing its public class identity aborts inside AppKit on resizing.
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 900, 174)
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskResizable
            backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration = [[[WKWebViewConfiguration alloc] init] autorelease];
        configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *webPanel = [[[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 900, 174) configuration:configuration] autorelease];
        WKWebView *webConfirmation = [[[WKWebView alloc] initWithFrame:NSZeroRect configuration:configuration] autorelease];
        window.contentView = webPanel;
        Class publicClass = [webPanel class];
        Class runtimeClass = object_getClass(webPanel);
        BOOL confirmationFirstMouse = [webConfirmation acceptsFirstMouse:nil];
        assert(!confirmationFirstMouse);
        assert(devjay_enable_first_mouse(webPanel));
        assert([webPanel class] == publicClass);
        assert(object_getClass(webPanel) == runtimeClass);
        assert([webPanel acceptsFirstMouse:nil]);
        assert([webConfirmation acceptsFirstMouse:nil] == confirmationFirstMouse);
        assert(original == class_getMethodImplementation(NSView.class, @selector(acceptsFirstMouse:)));
        assert(devjay_enable_first_mouse(webPanel)); // Repeated runtime-ready notification.
        WKWebView *newConfirmation = [[[WKWebView alloc] initWithFrame:NSZeroRect configuration:configuration] autorelease];
        assert(![newConfirmation acceptsFirstMouse:nil]);
        for (NSNumber *width in @[@1348, @500, @900, @1348]) {
            [window setContentSize:NSMakeSize(width.doubleValue, 174)];
            [webPanel layoutSubtreeIfNeeded];
            assert(webPanel.frame.size.width == width.doubleValue);
        }

        // The single-instance reopen callback focuses the control panel.
        // NSPanel refuses main-window status and raises if forced into it.
        NSPanel *strip = [[[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 900, 174)
            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
            backing:NSBackingStoreBuffered defer:NO] autorelease];
        assert(!strip.canBecomeMainWindow);
        devjay_focus_site(strip);
        assert(strip.isVisible);
        [strip orderOut:nil];

        getCompatID = readTag; setCompatID = writeTag; setWorkspace = moveWindow;
        assert(move_with_compat_tag(123, 456));
        assert(tag == 73 && movedWithTag == 0x79616265);
        moveResult = -1;
        assert(!move_with_compat_tag(123, 456));
        assert(tag == 73);
        getCompatID = NULL;
        assert(!move_with_compat_tag(123, 456));
        assert(tag == 73);

        devjay_open_editor("\xff", "/Applications/Test.app", 42);
        assert(gotPathError);
        gotPathError = NO;
        devjay_open_editor("/tmp/file", "\xff", 42);
        assert(gotPathError);
        // Invalid identifiers are rejected before touching a window or defaults.
        devjay_restore_layout((void *)panel, "\xff");
        puts("Native review regressions passed");
    }
    return 0;
}
