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
static BOOL singleAction, stripMenu, stripBackground, chooseHide, chooseSites, sitesHidden;
static BOOL rebaseAction, rebaseDisabled, publishAction, directPublish, mergedAction;
static NSString *syncTitle, *singleTitle;

// Exercise the real menu, targets and cleanup without displaying or tracking
// a menu in the user's desktop. Actual inactive-window behavior needs UI QA.
static BOOL selectTestItem(NSMenu *menu, SEL selector, NSMenuItem *item, NSPoint point, NSView *view) {
    (void)selector; (void)item;
    if (stripMenu) {
        assert(menu.numberOfItems == (stripBackground ? 5 : 8));
        NSMenuItem *minimal = [menu itemAtIndex:stripBackground ? 0 : 3];
        assert([minimal.title isEqual:@"Minimize strip"] && minimal.tag == 6 && minimal.toolTip.length);
        NSMenuItem *hide = [menu itemAtIndex:stripBackground ? 1 : 4];
        assert([hide.title isEqual:@"Hide strip"] && hide.tag == 7);
        NSMenuItem *sites = [menu itemAtIndex:stripBackground ? 2 : 5];
        assert([sites.title isEqual:sitesHidden ? @"Show site windows" : @"Hide site windows"]);
        assert(sites.tag == (sitesHidden ? 9 : 8));
        [menu performActionForItemAtIndex:(stripBackground ? 0 : 3) + (chooseSites ? 2 : chooseHide ? 1 : 0)];
        return YES;
    }
    assert(menu.numberOfItems == (directPublish ? 4 : (singleAction ? 4 : 5) + (rebaseAction ? 1 : 0)));
    if (!directPublish) {
        assert([[menu itemAtIndex:0].title isEqualToString:mergedAction ? @"Open PR #42 in browser" : singleAction ? (singleTitle ?: @"Show GoLand") : @"View changes"]);
        if (!singleAction) assert([[menu itemAtIndex:1].title isEqualToString:mergedAction ? @"Open PR #42 in new browser tab" : @"View changes in new window"]);
    }
    NSInteger separator = directPublish ? 1 : (singleAction ? 1 : 2) + (rebaseAction ? 1 : 0);
    if (rebaseAction) {
        NSMenuItem *rebase = [menu itemAtIndex:directPublish ? 0 : singleAction ? 1 : 2];
        assert([rebase.title isEqualToString:syncTitle ?: (mergedAction ? @"Return to develop & delete branch…" : publishAction ? @"Push rebased branch…" : @"Rebase and push…")]);
        assert(rebase.tag == 5 && rebase.enabled == !rebaseDisabled);
        assert(rebase.toolTip.length > 0);
        if (!rebaseDisabled) assert([rebase.toolTip isEqualToString:@"Check remote before confirmation."]);
    }
    assert([menu itemAtIndex:separator].separatorItem);
    assert([[menu itemAtIndex:separator + 1].title isEqualToString:@"DevJay settings…"]);
    assert([[menu itemAtIndex:separator + 2].title isEqualToString:@"Quit DevJay"]);
    assert([view isKindOfClass:WKWebView.class]);
    assert(point.x == expectedPoint.x && point.y == expectedPoint.y);
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
        const char *first = "View changes", *second = "View changes in new window", *settings = "DevJay settings…", *quit = "Quit DevJay";
        expectedPoint = NSMakePoint(22, view.isFlipped ? 41 : 139);
        for (NSInteger index = -1; index < 5; index++) {
            if (index == 2) continue;
            chosenIndex = index;
            int expected = index < 0 ? 0 : (index >= 3 ? (int)index : (int)index + 1);
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == expected);
        }
        assert(popupCount == 5);
        singleAction = YES;
        for (NSInteger index = 0; index < 4; index++) {
            if (index == 1) continue;
            chosenIndex = index;
            assert(devjay_card_menu(window, 22, 41, "Show GoLand", "", settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == index + 1);
        }
        assert(popupCount == 8);
        singleAction = NO;
        assert(devjay_card_menu(NULL, 22, 41, first, second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(devjay_card_menu(window, -1, 41, first, second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(devjay_card_menu(window, 301, 41, first, second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(devjay_card_menu(window, 22, 181, first, second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(devjay_card_menu(window, NAN, 41, first, second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(devjay_card_menu(window, 22, 41, "\xff", second, settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(popupCount == 8);
        rebaseAction = YES;
        const int results[] = {1, 2, 5, 0, 3, 4};
        for (NSInteger index = 0; index < 6; index++) {
            if (index == 3) continue;
            chosenIndex = index;
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Rebase and push…", "", "Check remote before confirmation.", 0, sitesHidden) == results[index]);
        }
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Rebase and push…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(popupCount == 14);
        publishAction = YES; rebaseDisabled = NO; chosenIndex = 2;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Push rebased branch…", "", "Check remote before confirmation.", 0, sitesHidden) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Push rebased branch…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(popupCount == 16);
        directPublish = YES; rebaseDisabled = NO; chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "Push rebased branch…", "", "Check remote before confirmation.", 0, sitesHidden) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "Push rebased branch…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "", "", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(popupCount == 18);
        directPublish = NO; mergedAction = YES; rebaseDisabled = NO; chosenIndex = 2;
        assert(devjay_card_menu(window, 22, 41, "Open PR #42 in browser", "Open PR #42 in new browser tab", settings, quit, "Return to develop & delete branch…", "", "Check remote before confirmation.", 0, sitesHidden) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, "Open PR #42 in browser", "Open PR #42 in new browser tab", settings, quit, "Return to develop & delete branch…", "Commit or stash local work first.", "Check remote before confirmation.", 0, sitesHidden) == 0);
        assert(popupCount == 20);
        mergedAction = NO; publishAction = NO; rebaseDisabled = NO; chosenIndex = 2;
        syncTitle = @"Fetch and update develop…";
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden) == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, syncTitle.UTF8String, "Commit or stash local edits before updating this branch.", "", 0, sitesHidden) == 0);
        syncTitle = @"Push 2 commits…"; rebaseDisabled = NO; chosenIndex = 2;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden) == 5);
        assert(popupCount == 23);
        singleAction = YES; singleTitle = @"View branch history"; syncTitle = @"Replace from remote…";
        // Omitting the second viewer choice must preserve the Git action tag.
        chosenIndex = 1;
        assert(devjay_card_menu(window, 22, 41, singleTitle.UTF8String, "", settings, quit, syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden) == 5);
        chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, singleTitle.UTF8String, "", settings, quit, syncTitle.UTF8String, "", "Check remote before confirmation.", 0, sitesHidden) == 1);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, singleTitle.UTF8String, "", settings, quit, syncTitle.UTF8String, "Commit or stash local edits before replacing this branch.", "", 0, sitesHidden) == 0);
        singleAction = NO; rebaseDisabled = NO;
        stripMenu = YES;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "", "", "", 1, sitesHidden) == 6);
        stripBackground = YES;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "", "", "", 1, sitesHidden) == 6);
        chooseHide = YES;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "", "", "", 1, sitesHidden) == 7);
        chooseSites = YES;
        for (int background = 0; background < 2; background++) {
            stripBackground = background;
            for (int hidden = 0; hidden < 2; hidden++) {
                sitesHidden = hidden;
                assert(devjay_card_menu(window, 22, 41, background ? "" : first, background ? "" : second,
                    settings, quit, "", "", "", 1, sitesHidden) == (hidden ? 9 : 8));
            }
        }
        method_setImplementation(method, original);
        puts("Native card menu regressions passed");
    }
    return 0;
}
