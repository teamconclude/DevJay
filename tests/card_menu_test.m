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

// Exercise the real menu, targets and cleanup without displaying or tracking
// a menu in the user's desktop. Actual inactive-window behavior needs UI QA.
static BOOL selectTestItem(NSMenu *menu, SEL selector, NSMenuItem *item, NSPoint point, NSView *view) {
    (void)selector; (void)item;
    assert(menu.numberOfItems == 5);
    assert([[menu itemAtIndex:0].title isEqualToString:@"View local edits"]);
    assert([[menu itemAtIndex:1].title isEqualToString:@"View local edits in new window"]);
    assert([menu itemAtIndex:2].separatorItem);
    assert([[menu itemAtIndex:3].title isEqualToString:@"DevJay settings…"]);
    assert([[menu itemAtIndex:4].title isEqualToString:@"Quit DevJay"]);
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
        const char *first = "View local edits", *second = "View local edits in new window", *settings = "DevJay settings…", *quit = "Quit DevJay";
        expectedPoint = NSMakePoint(22, view.isFlipped ? 41 : 139);
        for (NSInteger index = -1; index < 5; index++) {
            if (index == 2) continue;
            chosenIndex = index;
            int expected = index < 0 ? 0 : (index >= 3 ? (int)index : (int)index + 1);
            assert(devjay_card_menu(window, 22, 41, first, second, settings, quit) == expected);
        }
        assert(popupCount == 5);
        assert(devjay_card_menu(NULL, 22, 41, first, second, settings, quit) == 0);
        assert(devjay_card_menu(window, -1, 41, first, second, settings, quit) == 0);
        assert(devjay_card_menu(window, 301, 41, first, second, settings, quit) == 0);
        assert(devjay_card_menu(window, 22, 181, first, second, settings, quit) == 0);
        assert(devjay_card_menu(window, NAN, 41, first, second, settings, quit) == 0);
        assert(devjay_card_menu(window, 22, 41, "\xff", second, settings, quit) == 0);
        assert(popupCount == 5);
        method_setImplementation(method, original);
        puts("Native card menu regressions passed");
    }
    return 0;
}
