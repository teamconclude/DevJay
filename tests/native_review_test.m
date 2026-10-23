#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/focus_trace_darwin.m"
#include "../cmd/devjay/windows_darwin.m"
#include "../cmd/devjay/spaces_darwin.m"
#include "../cmd/devjay/layout_darwin.m"
#include "../cmd/devjay/editors_darwin.m"
#include "../cmd/devjay/profile_prompt_darwin.m"

static NSString *chosenProfileSignature;
void devjay_profile_prompt_chosen(const char *value) {
    [chosenProfileSignature release]; chosenProfileSignature = [[NSString stringWithUTF8String:value] copy];
}

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
    strip.identifier = @"strip";
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
    strip.identifier = @"strip";
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
    // Separate dialogs preserve their own card anchors and remain independently usable.
    NSRect firstFrame = popup.frame;
    DevJaySiteTestWindow *second = [[[DevJaySiteTestWindow alloc] initWithContentRect:NSMakeRect(0,0,460,330)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    devjay_show_suggestions(second,strip,600,10,200,140);
    assert(NSEqualRects(popup.frame, firstFrame));
    assert(second->keyOrders == 1 && popup->keyOrders == 2);
    assert(!NSApp.modalWindow && !popup.sheetParent && !second.sheetParent);
    [second orderOut:nil];
    assert(NSEqualRects(popup.frame, firstFrame));
    method_setImplementation(activation, original);
}

static void test_message_dialog_card_anchor(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    if (area.size.width < 1000 || area.size.height < 550) return;
    NSWindow *strip = [[[NSWindow alloc] initWithContentRect:NSMakeRect(area.origin.x+30,area.origin.y+10,900,174)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    strip.identifier = @"strip";
    strip.contentView = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,900,174)] autorelease];
    NSRect before = strip.frame;
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation, (IMP)recordActivation);
    devjay_show_message(strip,400,10,200,140,"Could not open PR #25","Test browser failure");
    NSWindow *window = nil;
    for (NSWindow *candidate in NSApp.windows) {
        if ([candidate.identifier isEqual:@"message-dialog"]) { window = candidate; break; }
    }
    assert(window && window.isVisible && NSApp.modalWindow == nil);
    assert(fabs(NSMidX(window.frame)-(area.origin.x+530)) < 1);
    assert(fabs(NSMinY(window.frame)-(area.origin.y+184)) < 1);
    assert(NSEqualRects(strip.frame,before));
    DevJayMessageDialog *controller = (DevJayMessageDialog *)window.delegate;
    [window retain];
    [controller cancelOperation:nil];
    assert(!window.isVisible && window.delegate == nil);
    [window release];
    devjay_show_message(strip,200,10,200,140,"Could not open PR #26","Another test failure");
    window = nil;
    for (NSWindow *candidate in NSApp.windows) {
        if ([candidate.identifier isEqual:@"message-dialog"] && candidate.isVisible) { window = candidate; break; }
    }
    assert(window && fabs(NSMidX(window.frame)-(area.origin.x+330)) < 1);
    controller = (DevJayMessageDialog *)window.delegate;
    NSAlert *alert = [controller valueForKey:@"alert"];
    [window retain];
    [alert.buttons.firstObject performClick:nil];
    assert(!window.isVisible && window.delegate == nil);
    [window release];
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
    parent.identifier = @"strip";
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
    panel.identifier = @"strip";
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
    panel.identifier = @"strip";
    panel.styleMask = NSWindowStyleMaskTitled;
    [panel setContentSize:NSMakeSize(500, 300)];
    devjay_fit_card_window(panel, 160, 500, 300, 1);
    assert([panel contentRectForFrameRect:panel.frame].size.height == 160);
    assert(NSHeight(panel.frame) > 160); // Native chrome is added once.
}

static void test_strip_layout_name_migration(void) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    NSString *key = @"DevJay.WindowLayout.Profile.test-strip-name";
    NSDictionary *oldRecord = @{@"frame":@"{{30, 40}, {700, 180}}", @"spaceUUID":@"saved-space"};
    [defaults setObject:@{legacyStripLayoutIdentifier:oldRecord} forKey:key];
    migrate_strip_layout(key);
    assert([[[defaults dictionaryForKey:key] objectForKey:@"strip"] isEqual:oldRecord]);
    assert([[[defaults dictionaryForKey:key] objectForKey:legacyStripLayoutIdentifier] isEqual:oldRecord]);
    NSDictionary *newRecord = @{@"frame":@"{{50, 60}, {800, 200}}"};
    [defaults setObject:@{legacyStripLayoutIdentifier:oldRecord, @"strip":newRecord} forKey:key];
    migrate_strip_layout(key);
    assert([[[defaults dictionaryForKey:key] objectForKey:@"strip"] isEqual:newRecord]);
    [defaults removeObjectForKey:key];
}

static NSUInteger siteBackOrders;
static void recordSiteBack(id window, SEL selector, id sender) { siteBackOrders++; }
static void test_site_visibility_restore_does_not_focus(void) {
    DevJaySiteTestWindow *site = [[[DevJaySiteTestWindow alloc] initWithContentRect:NSMakeRect(30,40,290,174)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    site.identifier = @"site-visibility";
    NSRect frame = site.frame;
    NSWindowCollectionBehavior behavior = site.collectionBehavior;
    Method back = class_getInstanceMethod(NSWindow.class, @selector(orderBack:));
    IMP originalBack = method_setImplementation(back, (IMP)recordSiteBack);
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP originalActivation = method_setImplementation(activation, (IMP)recordActivation);
    NSUInteger previousActivations = activationRequests;
    devjay_show_site_without_focus(site);
    assert(siteBackOrders == 1 && site->keyOrders == 0 && site->mainRequests == 0);
    assert(activationRequests == previousActivations && NSEqualRects(frame,site.frame) && behavior == site.collectionBehavior);
    site.identifier = @"strip";
    devjay_show_site_without_focus(site);
    assert(siteBackOrders == 1); // The Site toggle cannot show the strip.
    method_setImplementation(back, originalBack);
    method_setImplementation(activation, originalActivation);
}

// Drive native visibility and delayed cleanup without showing real windows.
@interface DevJayHiddenSiteTestWindow : DevJaySiteTestWindow {
@public
    BOOL shown, mini, active;
    NSUInteger outs;
}
@end
@implementation DevJayHiddenSiteTestWindow
- (BOOL)isVisible { return shown; }
- (BOOL)isMiniaturized { return mini; }
- (BOOL)isOnActiveSpace { return active; }
- (void)makeKeyAndOrderFront:(id)sender { (void)sender; keyOrders++; shown=YES; }
- (void)orderOut:(id)sender { (void)sender; shown=NO; outs++; }
- (void)deminiaturize:(id)sender { (void)sender; mini=NO; }
- (void)miniaturize:(id)sender { (void)sender; mini=YES; }
@end
static void test_hidden_site_navigation_retains_visibility(void) {
    DevJayHiddenSiteTestWindow *site=[[[DevJayHiddenSiteTestWindow alloc] initWithContentRect:NSMakeRect(30,40,290,174)
        styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO] autorelease];
    site.identifier=@"site-hidden";
    NSRect frame=site.frame;
    NSWindowCollectionBehavior behavior=site.collectionBehavior;
    Method activation=class_getInstanceMethod(NSApplication.class,@selector(activateIgnoringOtherApps:));
    IMP original=method_setImplementation(activation,(IMP)recordActivation);
    devjay_set_site_window_hidden(site,1);
    assert(!site->shown && site.alphaValue==0 && site.ignoresMouseEvents);
    site->mini=YES;
    devjay_focus_site(site);
    id first=objc_getAssociatedObject(site,&siteNavigationKey);
    assert(first && site->shown && !site->mini && site.alphaValue==0 && site.ignoresMouseEvents);
    assert(site->keyOrders==1 && site->mainRequests==1);
    assert(!(devjay_window_state(site)&8));
    site->active=YES; // A desktop notification must not turn visibility back on.
    assert((devjay_window_state(site)&1) && !(devjay_window_state(site)&8));
    assert(devjay_site_window_hidden(site) && site.alphaValue==0);
    devjay_focus_site(site);
    id second=objc_getAssociatedObject(site,&siteNavigationKey);
    assert(second!=first);
    NSUInteger outs=site->outs;
    devjay_finish_hidden_navigation(site,first);
    assert(site->outs==outs && site->shown);
    devjay_finish_hidden_navigation(site,second);
    assert(!site->shown && site->mini && site.alphaValue==0);
    devjay_focus_site(site);
    id third=objc_getAssociatedObject(site,&siteNavigationKey);
    devjay_set_site_window_hidden(site,0);
    assert(site->mini && site.alphaValue==1 && !site.ignoresMouseEvents && !devjay_site_window_hidden(site));
    outs=site->outs;
    devjay_finish_hidden_navigation(site,third);
    assert(site->outs==outs && site.alphaValue==1); // Old timer cannot undo Show.
    assert(NSEqualRects(frame,site.frame) && behavior==site.collectionBehavior);
    site.identifier=@"strip";
    devjay_set_site_window_hidden(site,1);
    assert(site.alphaValue==1); // The group setting cannot hide other window kinds.
    method_setImplementation(activation,original);
}

static void test_profile_layout_isolation(void) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    NSDictionary *legacy = [[defaults dictionaryForKey:@"DevJay.WindowLayout.v1"] retain];
    NSDictionary *initial = @{@"strip": @{@"frame": @"{{20, 20}, {500, 180}}"}};
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


@interface RecoveryTestScreen : NSScreen {
@public
    NSRect rectangle, usable;
}
@end
@implementation RecoveryTestScreen
- (NSRect)frame { return rectangle; }
- (NSRect)visibleFrame { return usable; }
- (NSDictionary *)deviceDescription { return @{@"NSScreenNumber":@(CGMainDisplayID())}; }
@end
static NSArray *recoveryTestScreens;

static void test_disconnected_window_recovery(void) {
    RecoveryTestScreen *left = [[[RecoveryTestScreen alloc] init] autorelease];
    left->rectangle = NSMakeRect(-1200,-400,1200,900); left->usable = NSMakeRect(-1200,-350,1200,820);
    RecoveryTestScreen *right = [[[RecoveryTestScreen alloc] init] autorelease];
    right->rectangle = NSMakeRect(0,0,1200,900); right->usable = NSMakeRect(0,50,1200,820);
    recoveryTestScreens = @[left,right];
    DevJayLayoutMonitor *monitor = [DevJayLayoutMonitor new];
    DevJayPlacementTestWindow *window = [[[DevJayPlacementTestWindow alloc]
        initWithContentRect:NSMakeRect(-1100,-200,500,330) styleMask:NSWindowStyleMaskBorderless
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    window.identifier = @"settings"; window->visibleForPlacement = YES;
    NSRect original = window.frame;
    [monitor recoverWindowsOnScreens:recoveryTestScreens]; assert(NSEqualRects(window.frame,original)); // Valid negative coordinates.
    // Disconnect the left display while writes to the active Profile are paused.
    NSString *oldKey = layoutKey;
    layoutKey = @"DevJay.WindowLayout.RecoveryTest";
    NSDictionary *saved = @{@"settings":@{@"frame":NSStringFromRect(original)}};
    [NSUserDefaults.standardUserDefaults setObject:saved forKey:layoutKey];
    DevJayLayoutObserver *observer = [DevJayLayoutObserver new]; observer->window = window;
    [NSNotificationCenter.defaultCenter addObserver:observer selector:@selector(windowMoved:) name:NSWindowDidMoveNotification object:window];
    BOOL oldPaused = profileLayoutPaused; profileLayoutPaused = YES;
    recoveryTestScreens = @[right];
    NSUInteger beforeActivations = activationRequests;
    [monitor recoverWindowsOnScreens:recoveryTestScreens];
    assert(NSContainsRect(right.visibleFrame,window.frame));
    assert([[NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey] isEqual:saved]);
    assert(activationRequests == beforeActivations && window->keyOrders == 0);
    // A reconnected screen can have a new origin. Existing good positions stay put.
    NSRect recovered = window.frame;
    recoveryTestScreens = @[left,right];
    [monitor recoverWindowsOnScreens:recoveryTestScreens]; assert(NSEqualRects(window.frame,recovered));
    // A large window is resized; even an excessive minimum size cannot strand buttons.
    window.minSize = NSMakeSize(1800,1200);
    [window setFrame:NSMakeRect(1000,800,1800,1200) display:NO];
    [monitor recoverWindowsOnScreens:recoveryTestScreens]; assert(NSContainsRect(right.visibleFrame,window.frame));
    window.minSize = NSMakeSize(0,0);
    // Recovery never reveals hidden/transparent or minimized Sites.
    window.identifier = @"site-hidden-recovery";
    NSRect outside = NSMakeRect(2400,-1500,290,174);
    window->visibleForPlacement = NO; [window setFrame:outside display:NO];
    [monitor recoverWindowsOnScreens:recoveryTestScreens]; assert(NSEqualRects(window.frame,outside));
    window->visibleForPlacement = YES; window.alphaValue = 0;
    [monitor recoverWindowsOnScreens:recoveryTestScreens]; assert(NSEqualRects(window.frame,outside));
    window.alphaValue = 1; window->minimizedForPlacement = YES;
    [monitor recoverWindowsOnScreens:recoveryTestScreens]; assert(NSEqualRects(window.frame,outside));
    window->minimizedForPlacement = NO;
    // Exposing a previously hidden window also recovers it, without changing saved layouts.
    [monitor windowExposed:[NSNotification notificationWithName:NSWindowDidBecomeKeyNotification object:window]];
    assert(NSContainsRect(placement_bounds(nearest_screen(window.frame)),window.frame));
    assert([[NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey] isEqual:saved]);
    // Actual notification delivery and a later AppKit move are both recovered.
    [window setFrame:outside display:NO];
    [NSNotificationCenter.defaultCenter postNotificationName:NSApplicationDidChangeScreenParametersNotification object:nil];
    assert(NSContainsRect(placement_bounds(nearest_screen(window.frame)),window.frame));
    [window setFrame:outside display:NO];
    [[NSRunLoop currentRunLoop] runUntilDate:[NSDate dateWithTimeIntervalSinceNow:0.35]];
    assert(NSContainsRect(placement_bounds(nearest_screen(window.frame)),window.frame));
    assert([[NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey] isEqual:saved]);
    // Test persistence suppression even after the Profile is known again.
    [window setFrame:outside display:NO];
    profileLayoutPaused = NO;
    [monitor recoverWindowsOnScreens:recoveryTestScreens];
    assert([[NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey] isEqual:saved]);
    profileLayoutPaused = oldPaused;
    [observer stopObserving]; [observer release];
    [NSUserDefaults.standardUserDefaults removeObjectForKey:layoutKey]; layoutKey = oldKey;
    [monitor release];
    recoveryTestScreens = nil;
}

static void test_profile_prompt_recovery(void) {
    NSRect area = NSScreen.mainScreen.visibleFrame;
    NSWindow *strip = [[[NSWindow alloc] initWithContentRect:NSMakeRect(NSMinX(area),NSMinY(area),area.size.width,100)
        styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
    strip.identifier = @"strip";
    NSRect original = strip.frame;
    NSString *signature = display_signature();
    Method activation = class_getInstanceMethod(NSApplication.class,@selector(activateIgnoringOtherApps:));
    IMP originalActivation = method_setImplementation(activation,(IMP)recordActivation);
    devjay_show_profile_prompt(strip,signature.UTF8String);
    DevJayProfilePrompt *prompt = profilePrompt;
    assert(prompt && prompt->alert.window.isVisible && !prompt->alert.window.isSheet && !NSApp.modalWindow);
    assert(NSContainsRect(area,prompt->alert.window.frame));
    assert(NSEqualRects(strip.frame,original));
    devjay_show_profile_prompt(strip,signature.UTF8String); assert(profilePrompt == prompt);
    devjay_sync_profile_prompt(signature.UTF8String); assert(profilePrompt == prompt);
    [prompt->alert.buttons[1] performClick:nil];
    assert(!profilePrompt && [chosenProfileSignature isEqual:signature]);
    [chosenProfileSignature release]; chosenProfileSignature = nil;
    devjay_show_profile_prompt(strip,"disconnected-combination");
    [NSNotificationCenter.defaultCenter postNotificationName:NSApplicationDidChangeScreenParametersNotification object:nil];
    assert(!profilePrompt && !chosenProfileSignature); // Reconnect closes obsolete choices.
    devjay_show_profile_prompt(strip,signature.UTF8String);
    [profilePrompt cancelOperation:nil]; assert(!profilePrompt && !chosenProfileSignature);
    devjay_show_profile_prompt(strip,signature.UTF8String);
    devjay_sync_profile_prompt(""); assert(!profilePrompt && !chosenProfileSignature);
    method_setImplementation(activation,originalActivation);
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        test_strip_focus_isolation();
        test_confirmation_card_anchor();
        test_message_dialog_card_anchor();
        test_confirmation_site_center();
        test_viewer_placement();
        test_switch_placement();
        test_card_height_refit();
        test_profile_layout_isolation();
        test_strip_layout_name_migration();
        test_disconnected_window_recovery();
        test_profile_prompt_recovery();
        test_site_visibility_restore_does_not_focus();
        test_hidden_site_navigation_retains_visibility();
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

        // The single-instance reopen callback focuses the strip.
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
