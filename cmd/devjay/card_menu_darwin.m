#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <math.h>

@interface DevJayCardMenuHost : NSPanel
@end
@implementation DevJayCardMenuHost
- (BOOL)canBecomeKeyWindow { return YES; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

@interface DevJayCardMenuSelection : NSObject <NSMenuDelegate>
@property(nonatomic) NSInteger selected;
@property(nonatomic) NSRect screenBounds;
- (void)selectItem:(NSMenuItem *)item;
@end

@implementation DevJayCardMenuSelection
- (void)selectItem:(NSMenuItem *)item { self.selected = item.tag; }
- (NSRect)confinementRectForMenu:(NSMenu *)menu onScreen:(NSScreen *)screen {
    (void)menu; (void)screen;
    return self.screenBounds;
}
@end

static WKWebView *card_menu_webview(NSView *view) {
    if ([view isKindOfClass:WKWebView.class]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *found = card_menu_webview(child);
        if (found) return found;
    }
    return nil;
}

// Popup locations refer to the menu's top edge in bottom-up screen coordinates.
// Move that edge up far enough to fit every row, clear of the Dock/menu bar.
static NSPoint card_menu_screen_point(NSPoint point, NSSize size, NSRect visibleFrame) {
    NSRect area = NSInsetRect(visibleFrame, 8, 8);
    if (NSWidth(area) <= 0 || NSHeight(area) <= 0) return point;
    point.x = MAX(NSMinX(area), MIN(point.x, NSMaxX(area) - size.width));
    point.y = MIN(NSMaxY(area), MAX(point.y, NSMinY(area) + size.height));
    return point;
}

// Invoked on AppKit's main thread. The target and menu belong to this single
// tracking loop; no registrations or callbacks survive its return.
int devjay_card_menu(void *pointer, double x, double y, const char *first, const char *second, const char *settings, const char *quit, const char *newPRTab, const char *action, const char *actionDisabled, const char *hint, int strip, int siteWindowsHidden, int viewDiffs, int reviewStatus, int primaryAction) {
    if (![NSThread isMainThread] || !pointer || !isfinite(x) || !isfinite(y)) return 0;
    @autoreleasepool {
        NSWindow *window = (NSWindow *)pointer;
        WKWebView *view = card_menu_webview(window.contentView);
        if (!view || !window.visible || x < 0 || y < 0 || x > NSWidth(view.bounds) || y > NSHeight(view.bounds)) return 0;
        NSString *firstTitle = first ? [NSString stringWithUTF8String:first] : nil;
        NSString *secondTitle = second ? [NSString stringWithUTF8String:second] : nil;
        NSString *settingsTitle = settings ? [NSString stringWithUTF8String:settings] : nil;
        NSString *quitTitle = quit ? [NSString stringWithUTF8String:quit] : nil;
        NSString *newPRTabTitle = newPRTab ? [NSString stringWithUTF8String:newPRTab] : @"";
        NSString *actionTitle = action ? [NSString stringWithUTF8String:action] : @"";
        NSString *disabledReason = actionDisabled ? [NSString stringWithUTF8String:actionDisabled] : @"";
        NSString *hintText = hint ? [NSString stringWithUTF8String:hint] : @"";
        if (!firstTitle || !secondTitle || !settingsTitle.length || !quitTitle.length || !newPRTabTitle || !actionTitle || !disabledReason || !hintText) return 0;
        if (!firstTitle.length && (secondTitle.length || (!actionTitle.length && !strip))) return 0;

        DevJayCardMenuSelection *selection = [[[DevJayCardMenuSelection alloc] init] autorelease];
        NSMenu *menu = [[[NSMenu alloc] initWithTitle:@""] autorelease];
        menu.autoenablesItems = NO;
        menu.allowsContextMenuPlugIns = NO;
        menu.delegate = selection;
        NSArray<NSString *> *titles = @[actionTitle, firstTitle, reviewStatus ? @"" : secondTitle, viewDiffs ? @"Review" : @"", reviewStatus ? secondTitle : @"", reviewStatus ? newPRTabTitle : @"", strip ? @"Minimize strip" : @"", strip ? @"Hide strip" : @"", strip ? (siteWindowsHidden ? @"Show site windows" : @"Hide site windows") : @"", settingsTitle, quitTitle];
        const NSInteger tags[] = {5, 1, 2, 10, 11, 2, 6, 7, siteWindowsHidden ? 9 : 8, 3, 4};
        for (NSUInteger index = 0; index < titles.count; index++) {
            if (!titles[index].length) continue;
            if ((tags[index] == 6 || (tags[index] == 3 && !strip)) && menu.numberOfItems) [menu addItem:[NSMenuItem separatorItem]];
            NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:titles[index] action:@selector(selectItem:) keyEquivalent:@""] autorelease];
            item.target = selection;
            item.tag = tags[index];
            if (item.tag == 10) item.toolTip = @"Compare the branch fork point with current files, including uncommitted edits";
            if (item.tag == 6) item.toolTip = @"Show a single row of Site names";
            if (item.tag == 7) item.toolTip = @"Hide the strip completely";
            if (item.tag == 8 || item.tag == 9) item.toolTip = @"Hide or restore independent Site windows without changing the strip";
            if (item.tag == 5) {
                item.enabled = !disabledReason.length;
                item.toolTip = disabledReason.length ? disabledReason : hintText;
            }
            [menu addItem:item];
        }
        NSMenuItem *primary = primaryAction > 0 ? [menu itemWithTag:primaryAction] : nil;
        if (primary && [menu indexOfItem:primary] > 0) {
            [primary retain];
            [menu removeItem:primary];
            [menu insertItem:primary atIndex:0];
            [primary release];
        }
        NSPoint point = NSMakePoint(NSMinX(view.bounds) + x,
            view.isFlipped ? NSMinY(view.bounds) + y : NSMaxY(view.bounds) - y);
        point = [window convertPointToScreen:[view convertPoint:point toView:nil]];
        NSScreen *screen = window.screen;
        for (NSScreen *candidate in NSScreen.screens) {
            if (NSPointInRect(point, candidate.frame)) { screen = candidate; break; }
        }
        screen = screen ?: NSScreen.mainScreen;
        NSRect available = screen ? screen.visibleFrame : window.frame;
        selection.screenBounds = available;
        point = card_menu_screen_point(point, menu.size, available);
        // AppKit confines view-relative menus to their hosting window. A
        // temporary transparent panel gives tracking the full usable screen.
        // It accepts keyboard focus so Escape and menu navigation keep working.
        DevJayCardMenuHost *host = [[DevJayCardMenuHost alloc] initWithContentRect:available
            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
            backing:NSBackingStoreBuffered defer:NO];
        host.opaque = NO;
        host.backgroundColor = NSColor.clearColor;
        host.hasShadow = NO;
        host.hidesOnDeactivate = NO;
        host.releasedWhenClosed = NO;
        host.level = window.level;
        host.collectionBehavior = NSWindowCollectionBehaviorTransient | NSWindowCollectionBehaviorFullScreenAuxiliary;
        [window addChildWindow:host ordered:NSWindowAbove];
        [host orderFrontRegardless];
        [host makeKeyWindow];
        NSView *menuView = host.contentView;
        point = [menuView convertPoint:[host convertPointFromScreen:point] fromView:nil];
        // Keep the hosting view/window alive if the Site is removed while the
        // native menu tracks; Go revalidates membership before acting.
        [window retain];
        [view retain];
        [menu popUpMenuPositioningItem:nil atLocation:point inView:menuView];
        NSInteger selected = selection.selected;
        [window removeChildWindow:host];
        [host orderOut:nil];
        [host close];
        [host release];
        [view release];
        [window release];
        return (int)selected;
    }
}
