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
static BOOL singleAction;
static BOOL rebaseAction, rebaseDisabled, publishAction, directPublish;

// Exercise the real menu, targets and cleanup without displaying or tracking
// a menu in the user's desktop. Actual inactive-window behavior needs UI QA.
static BOOL selectTestItem(NSMenu *menu, SEL selector, NSMenuItem *item, NSPoint point, NSView *view) {
    (void)selector; (void)item;
    assert(menu.numberOfItems == (directPublish ? 4 : (singleAction ? 4 : 5) + (rebaseAction ? 1 : 0)));
    if (!directPublish) {
        assert([[menu itemAtIndex:0].title isEqualToString:singleAction ? @"Show GoLand" : @"View changes"]);
        if (!singleAction) assert([[menu itemAtIndex:1].title isEqualToString:@"View changes in new window"]);
    }
    NSInteger separator = directPublish ? 1 : (singleAction ? 1 : 2) + (rebaseAction ? 1 : 0);
    if (rebaseAction) {
        NSMenuItem *rebase = [menu itemAtIndex:directPublish ? 0 : 2];
        assert([rebase.title isEqualToString:publishAction ? @"Push rebased branch…" : @"Rebase and push…"]);
        assert(rebase.tag == 5 && rebase.enabled == !rebaseDisabled);
        assert(rebase.toolTip.length > 0);
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
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "", "") == expected);
        }
        assert(popupCount == 5);
        singleAction = YES;
        for (NSInteger index = 0; index < 4; index++) {
            if (index == 1) continue;
            chosenIndex = index;
            assert(devjay_card_menu(window, 22, 41, "Show GoLand", "", settings, quit, "", "") == index + 1);
        }
        assert(popupCount == 8);
        singleAction = NO;
        assert(devjay_card_menu(NULL, 22, 41, first, second, settings, quit, "", "") == 0);
        assert(devjay_card_menu(window, -1, 41, first, second, settings, quit, "", "") == 0);
        assert(devjay_card_menu(window, 301, 41, first, second, settings, quit, "", "") == 0);
        assert(devjay_card_menu(window, 22, 181, first, second, settings, quit, "", "") == 0);
        assert(devjay_card_menu(window, NAN, 41, first, second, settings, quit, "", "") == 0);
        assert(devjay_card_menu(window, 22, 41, "\xff", second, settings, quit, "", "") == 0);
        assert(popupCount == 8);
        rebaseAction = YES;
        const int results[] = {1, 2, 5, 0, 3, 4};
        for (NSInteger index = 0; index < 6; index++) {
            if (index == 3) continue;
            chosenIndex = index;
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Rebase and push…", "") == results[index]);
        }
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Rebase and push…", "Commit or stash local work first.") == 0);
        assert(popupCount == 14);
        publishAction = YES; rebaseDisabled = NO; chosenIndex = 2;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Push rebased branch…", "") == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, first, second, settings, quit, "Push rebased branch…", "Commit or stash local work first.") == 0);
        assert(popupCount == 16);
        directPublish = YES; rebaseDisabled = NO; chosenIndex = 0;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "Push rebased branch…", "") == 5);
        rebaseDisabled = YES; chosenIndex = -1;
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "Push rebased branch…", "Commit or stash local work first.") == 0);
        assert(devjay_card_menu(window, 22, 41, "", "", settings, quit, "", "") == 0);
        assert(popupCount == 18);
        method_setImplementation(method, original);
        puts("Native card menu regressions passed");
    }
    return 0;
}
