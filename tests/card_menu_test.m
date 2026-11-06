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
    assert(menu.numberOfItems == 4);
    NSMenuItem *first = [menu itemAtIndex:0], *disabled = [menu itemAtIndex:1];
    assert(first.tag == 51 && [first.title isEqualToString:@"First"] && first.enabled);
    assert(disabled.tag == 23 && !disabled.enabled && [disabled.toolTip isEqualToString:@"Busy"]);
    assert([menu itemAtIndex:2].separatorItem && [menu itemAtIndex:3].tag == 99);
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
        const char *items = "[{\"action\":51,\"title\":\"First\"},{\"action\":23,\"title\":\"Second\",\"disabled\":true,\"hint\":\"Busy\"},"
                            "{\"action\":0},{\"action\":99,\"title\":\"Last\"}]";
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
        const int results[] = {51, 0, 0, 99};
        for (chosenIndex = -1; chosenIndex < 4; chosenIndex++) {
            if (chosenIndex == 2) continue;
            assert(devjay_card_menu(window, 22, 41, items) == (chosenIndex < 0 ? 0 : results[chosenIndex]));
            assert(window.childWindows.count == 0);
        }
        assert(devjay_card_menu(NULL, 22, 41, items) == 0);
        assert(devjay_card_menu(window, -1, 41, items) == 0);
        assert(devjay_card_menu(window, 301, 41, items) == 0);
        assert(devjay_card_menu(window, 22, 181, items) == 0);
        assert(devjay_card_menu(window, NAN, 41, items) == 0);
        assert(devjay_card_menu(window, 22, 41, "\xff") == 0);
        assert(devjay_card_menu(window, 22, 41, "[]") == 0);
        assert(popupCount == 4);
        method_setImplementation(method, original);
        puts("Native card menu regressions passed");
    }
    return 0;
}
