#import <AppKit/AppKit.h>
#import <objc/runtime.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/card_menu_darwin.m"

@interface CardMenuTestWindow : NSWindow
@end
@implementation CardMenuTestWindow
- (BOOL)isVisible { return YES; }
@end

static NSInteger chosenIndex;
static int popupCount;
static NSPoint expectedPoint;
static NSRect expectedScreenFrame;
static BOOL singleAction, stripMenu, stripBackground, chooseHide, chooseSites, sitesHidden;
static BOOL rebaseAction, rebaseDisabled, publishAction, directPublish, mergedAction;
static NSString *syncTitle, *singleTitle;
static BOOL viewDiffs, reviewStatus, primaryView;

// Exercise the real menu, targets and cleanup without displaying or tracking
// a menu in the user's desktop. Actual inactive-window behavior needs UI QA.
static BOOL selectTestItem(NSMenu *menu, SEL selector, NSMenuItem *item, NSPoint point, NSView *view) {
    (void)selector; (void)item;
    assert([view.window isKindOfClass:DevJayCardMenuHost.class]);
    assert(view.window.canBecomeKeyWindow && !view.window.canBecomeMainWindow);
    assert(!view.window.opaque && !view.window.hasShadow);
    assert(NSWidth(view.bounds) >= MIN(menu.size.width, NSWidth(expectedScreenFrame)));
    assert(NSHeight(view.bounds) >= MIN(menu.size.height, NSHeight(expectedScreenFrame)));
    assert(menu.allowsContextMenuPlugIns == NO);
    assert(NSEqualRects([menu.delegate confinementRectForMenu:menu onScreen:NSScreen.mainScreen], expectedScreenFrame));
    NSPoint expected = card_menu_screen_point(expectedPoint, menu.size, expectedScreenFrame);
    assert(NSEqualPoints([view.window convertPointToScreen:[view convertPoint:point toView:nil]], expected));
    if (primaryView) {
        assert([menu itemAtIndex:0].tag == 1);
        assert([[menu itemAtIndex:0].title isEqualToString:@"View branch"]);
        assert([menu itemAtIndex:1].tag == 5);
        assert([menu itemAtIndex:2].tag == 10);
        [menu performActionForItemAtIndex:0];
        return YES;
    }
    if (reviewStatus) {
        assert(menu.numberOfItems == (stripMenu ? 10 : 7) - (viewDiffs ? 0 : 1));
        assert([[menu itemAtIndex:viewDiffs ? 1 : 0].title isEqualToString:@"View branch"]);
        assert([menu itemAtIndex:viewDiffs ? 1 : 0].tag == 1);
        if (viewDiffs) {
            assert([[menu itemAtIndex:0].title isEqualToString:@"Review"]);
            assert([menu itemAtIndex:0].tag == 10);
        } else assert([menu itemWithTag:10] == nil);
        NSMenuItem *pr = [menu itemAtIndex:viewDiffs ? 2 : 1];
        assert([pr.title isEqualToString:@"Open PR #42 in browser"] && pr.tag == 11);
        NSMenuItem *newTab = [menu itemAtIndex:viewDiffs ? 3 : 2];
        assert([newTab.title isEqualToString:@"Open PR #42 in new browser tab"] && newTab.tag == 2);
        assert([menu itemWithTag:5] == nil);
        assert([menu itemAtIndex:viewDiffs ? 4 : 3].separatorItem);
        [menu performActionForItemAtIndex:chosenIndex];
        return YES;
    }
    if (rebaseAction) {
        NSMenuItem *rebase = [menu itemAtIndex:0];
        assert([rebase.title isEqualToString:syncTitle ?: (mergedAction ? @"Return to develop & delete branch…" : publishAction ? @"Push rebased branch…" : @"Rebase and push…")]);
        assert(rebase.tag == 5 && rebase.enabled == !rebaseDisabled);
        assert(rebase.toolTip.length > 0);
        if (!rebaseDisabled) assert([rebase.toolTip isEqualToString:@"Check remote before confirmation."]);
    }
    if (viewDiffs) {
        NSInteger diffsIndex = (directPublish ? 0 : singleAction ? 1 : 2) + (rebaseAction ? 1 : 0);
        NSMenuItem *diffs = [menu itemAtIndex:diffsIndex];
        assert([diffs.title isEqualToString:@"Review"] && diffs.tag == 10 && diffs.enabled);
        assert(diffs.toolTip.length > 0);
    } else {
        assert([menu itemWithTag:10] == nil);
    }
    if (stripMenu) {
        assert(menu.numberOfItems == (stripBackground ? 5 : 8) + (rebaseAction ? 1 : 0) + (viewDiffs ? 1 : 0) - (singleAction ? 1 : 0));
        NSInteger controls = (stripBackground ? 0 : 3) + (rebaseAction ? 1 : 0) + (viewDiffs ? 1 : 0) - (singleAction ? 1 : 0);
        NSMenuItem *minimal = [menu itemAtIndex:controls];
        assert([minimal.title isEqual:@"Minimize strip"] && minimal.tag == 6 && minimal.toolTip.length);
        NSMenuItem *hide = [menu itemAtIndex:controls + 1];
        assert([hide.title isEqual:@"Hide strip"] && hide.tag == 7);
        NSMenuItem *sites = [menu itemAtIndex:controls + 2];
        assert([sites.title isEqual:sitesHidden ? @"Show site windows" : @"Hide site windows"]);
        assert(sites.tag == (sitesHidden ? 9 : 8));
        [menu performActionForItemAtIndex:viewDiffs ? chosenIndex : controls + (chooseSites ? 2 : chooseHide ? 1 : 0)];
        return YES;
    }
    assert(menu.numberOfItems == (directPublish ? 4 : (singleAction ? 4 : 5) + (rebaseAction ? 1 : 0)) + (viewDiffs ? 1 : 0));
    if (!directPublish) {
        NSInteger navigation = rebaseAction ? 1 : 0;
        assert([[menu itemAtIndex:navigation].title isEqualToString:mergedAction ? @"Open PR #42 in browser" : singleAction ? (singleTitle ?: @"Show GoLand") : @"View branch"]);
        if (!singleAction) assert([[menu itemAtIndex:navigation + 1].title isEqualToString:mergedAction ? @"Open PR #42 in new browser tab" : @"View branch in new window"]);
    }
    NSInteger separator = (directPublish ? 1 : (singleAction ? 1 : 2) + (rebaseAction ? 1 : 0)) + (viewDiffs ? 1 : 0);
    assert([menu itemAtIndex:separator].separatorItem);
    assert([[menu itemAtIndex:separator + 1].title isEqualToString:@"DevJay settings…"]);
    assert([[menu itemAtIndex:separator + 2].title isEqualToString:@"Quit DevJay"]);
    popupCount++;
    if (chosenIndex >= 0) [menu performActionForItemAtIndex:chosenIndex];
    return chosenIndex >= 0;
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        CardMenuTestWindow *window = [[[CardMenuTestWindow alloc] initWithContentRect:NSMakeRect(0, 0, 300, 180)
            styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration = [[[WKWebViewConfiguration alloc] init] autorelease];
        configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *view = [[[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 300, 180) configuration:configuration] autorelease];
        window.contentView = view;
        SEL selector = @selector(popUpMenuPositioningItem:atLocation:inView:);
        Method method = class_getInstanceMethod(NSMenu.class, selector);
        IMP original = method_setImplementation(method, (IMP)selectTestItem);
        const char *first = "View branch", *second = "View branch in new window", *settings = "DevJay settings…", *quit = "Quit DevJay";
        expectedPoint = [window convertPointToScreen:[view convertPoint:NSMakePoint(22, view.isFlipped ? 41 : 139) toView:nil]];
        for (NSScreen *screen in NSScreen.screens) {
            if (NSPointInRect(expectedPoint, screen.frame)) expectedScreenFrame = screen.visibleFrame;
        }
        // Headless checks have no NSScreen and use the source frame fallback.
        if (NSIsEmptyRect(expectedScreenFrame)) expectedScreenFrame = window.frame;
        // Full-sized menus fit inside the visible screen, including displays
        // left of/below the primary screen and strips overlapping the Dock.
        NSSize menuSize = NSMakeSize(240, 260);
        for (NSValue *value in @[[NSValue valueWithRect:NSMakeRect(0, 80, 1440, 796)],
                                [NSValue valueWithRect:NSMakeRect(-1920, -600, 1920, 1056)]]) {
            NSRect frame = value.rectValue, safe = NSInsetRect(frame, 8, 8);
            for (NSValue *location in @[[NSValue valueWithPoint:NSMakePoint(NSMinX(frame), NSMinY(frame) - 40)],
                                       [NSValue valueWithPoint:NSMakePoint(NSMaxX(frame) - 2, NSMaxY(frame))],
                                       [NSValue valueWithPoint:NSMakePoint(NSMidX(frame), NSMidY(frame))]]) {
                NSPoint top = card_menu_screen_point(location.pointValue, menuSize, frame);
                assert(NSContainsRect(safe, NSMakeRect(top.x, top.y - menuSize.height, menuSize.width, menuSize.height)));
            }
        }
        for (NSInteger index = -1; index < 5; index++) {
            if (index == 2) continue;
            chosenIndex = index;
            int expected = index < 0 ? 0 : (index >= 3 ? (int)index : (int)index + 1);
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == expected);
        }
        assert(popupCount == 5);
        assert(window.childWindows.count == 0);
        singleAction = YES;
        for (NSInteger index = 0; index < 4; index++) {
            if (index == 1) continue;
            chosenIndex = index;
            assert(devjay_card_menu(window, 22, 41, "Show GoLand", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == index + 1);
        }
        assert(popupCount == 8);
        singleAction = NO;
        assert(devjay_card_menu(NULL, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(devjay_card_menu(window, -1, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(devjay_card_menu(window, 301, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(devjay_card_menu(window, 22, 181, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(devjay_card_menu(window, NAN, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(devjay_card_menu(window, 22, 41, "\xff", second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(popupCount == 8);
        rebaseAction = YES;
        const int results[] = {5, 1, 2, 0, 3, 4};
        for (NSInteger index = 0; index < 6; index++) {
            if (index == 3) continue;
            chosenIndex = index;
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Rebase and push…", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == results[index]);
        }
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Rebase and push…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(popupCount == 14);
        publishAction = YES; rebaseDisabled = NO; chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Push rebased branch…", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Push rebased branch…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(popupCount == 16);
        directPublish = YES; rebaseDisabled = NO; chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Push rebased branch…", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Push rebased branch…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(popupCount == 18);
        directPublish = NO; mergedAction = YES; rebaseDisabled = NO; chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, "Open PR #42 in browser", "Open PR #42 in new browser tab", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Return to develop & delete branch…", "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, "Open PR #42 in browser", "Open PR #42 in new browser tab", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "Return to develop & delete branch…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        assert(popupCount == 20);
        mergedAction = NO; publishAction = NO; rebaseDisabled = NO; chosenIndex = 0;
        syncTitle = @"Fetch and update develop…";
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String, "Commit or stash local edits before updating this branch.", "", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        syncTitle = @"Push 2 commits…"; rebaseDisabled = NO; chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 5);
        assert(popupCount == 23);
        singleAction = YES; singleTitle = @"View branch"; syncTitle = @"Replace from remote…";
        // Omitting the second viewer choice must preserve the Git action tag.
        chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, singleTitle.UTF8String, "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 5);
        chosenIndex = 1;
        assert(devjay_card_menu(window, 22, 41, singleTitle.UTF8String, "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden, viewDiffs, 0, 0) == 1);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, singleTitle.UTF8String, "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String, "Commit or stash local edits before replacing this branch.", "", 0, sitesHidden, viewDiffs, 0, 0) == 0);
        singleAction = NO; rebaseAction = NO; rebaseDisabled = NO;
        stripMenu = YES;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "", 1, sitesHidden, viewDiffs, 0, 0) == 6);
        stripBackground = YES;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "", 1, sitesHidden, viewDiffs, 0, 0) == 6);
        chooseHide = YES;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "", 1, sitesHidden, viewDiffs, 0, 0) == 7);
        chooseSites = YES;
        for (int background = 0; background < 2; background++) {
            stripBackground = background;
            for (int hidden = 0; hidden < 2; hidden++) {
                sitesHidden = hidden;
                assert(devjay_card_menu(window, 22, 41, background ? "" : first, background ? "" : second,
                    settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", "", "", "", 1, sitesHidden, viewDiffs, 0, 0) == (hidden ? 9 : 8));
            }
        }
        rebaseAction = YES; stripBackground = NO;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "", syncTitle.UTF8String,
            "", "Check remote before confirmation.", 1, sitesHidden, viewDiffs, 0, 0) == 9);
        // The review item has its own stable action tag with or without a
        // Git action, an existing viewer, and the strip's window controls.
        stripBackground = NO; singleTitle = @"View branch"; viewDiffs = YES;
        syncTitle = @"Push 2 commits…";
        for (int strip = 0; strip < 2; strip++) {
            stripMenu = strip;
            for (int single = 0; single < 2; single++) {
                singleAction = single;
                for (int action = 0; action < 2; action++) {
                    rebaseAction = action;
                    chosenIndex = (singleAction ? 1 : 2) + (rebaseAction ? 1 : 0);
                    assert(devjay_card_menu(window, 22, 41, first, singleAction ? "" : second, settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "",
                        rebaseAction ? syncTitle.UTF8String : "", "", "Check remote before confirmation.",
                        stripMenu, sitesHidden, viewDiffs, 0, 0) == 10);
                }
            }
        }
        reviewStatus = YES;
        for (int strip = 0; strip < 2; strip++) {
            stripMenu = strip;
            for (int diffs = 0; diffs < 2; diffs++) {
                viewDiffs = diffs;
                const int actions[] = {diffs ? 10 : 1, diffs ? 1 : 11, diffs ? 11 : 2, 2};
                for (chosenIndex = 0; chosenIndex < (diffs ? 4 : 3); chosenIndex++) {
                    assert(devjay_card_menu(window, 22, 41, "View branch", "Open PR #42 in browser", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "",
                        "", "", "", stripMenu, sitesHidden, viewDiffs, 1, viewDiffs ? 10 : 1) == actions[chosenIndex]);
                }
            }
        }
        primaryView = YES;
        assert(devjay_card_menu(window, 22, 41, "View branch", "", settings, quit, reviewStatus ? "Open PR #42 in new browser tab" : "",
            "Fetch and update main…", "", "", 1, sitesHidden, 1, 0, 1) == 1);
        method_setImplementation(method, original);
        puts("Native card menu regressions passed");
    }
    return 0;
}
