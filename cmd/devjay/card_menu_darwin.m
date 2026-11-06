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
int devjay_card_menu(void *pointer, double x, double y, const char *itemsJSON) {
    if (![NSThread isMainThread] || !pointer || !isfinite(x) || !isfinite(y)) return 0;
    @autoreleasepool {
        NSWindow *window = (NSWindow *)pointer;
        WKWebView *view = card_menu_webview(window.contentView);
        if (!view || !window.visible || x < 0 || y < 0 || x > NSWidth(view.bounds) || y > NSHeight(view.bounds)) return 0;
        if (!itemsJSON) return 0;
        NSData *data = [NSData dataWithBytes:itemsJSON length:strlen(itemsJSON)];
        NSArray *rows = [NSJSONSerialization JSONObjectWithData:data options:0 error:NULL];
        if (![rows isKindOfClass:NSArray.class] || !rows.count) return 0;

        DevJayCardMenuSelection *selection = [[[DevJayCardMenuSelection alloc] init] autorelease];
        NSMenu *menu = [[[NSMenu alloc] initWithTitle:@""] autorelease];
        menu.autoenablesItems = NO;
        menu.allowsContextMenuPlugIns = NO;
        menu.delegate = selection;
        for (NSDictionary *row in rows) {
            if (![row isKindOfClass:NSDictionary.class] || ![row[@"action"] isKindOfClass:NSNumber.class]) return 0;
            NSInteger action = [row[@"action"] integerValue];
            if (action == 0) {
                [menu addItem:[NSMenuItem separatorItem]];
                continue;
            }
            NSString *title = row[@"title"];
            if (![title isKindOfClass:NSString.class] || !title.length) return 0;
            NSMenuItem *item = [menu addItemWithTitle:title action:@selector(selectItem:) keyEquivalent:@""];
            item.target = selection;
            item.tag = action;
            item.enabled = ![row[@"disabled"] boolValue];
            item.toolTip = row[@"hint"];
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
