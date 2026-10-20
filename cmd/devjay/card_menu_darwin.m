#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <math.h>

@interface DevJayCardMenuSelection : NSObject
@property(nonatomic) NSInteger selected;
- (void)selectItem:(NSMenuItem *)item;
@end

@implementation DevJayCardMenuSelection
- (void)selectItem:(NSMenuItem *)item { self.selected = item.tag; }
@end

static WKWebView *card_menu_webview(NSView *view) {
    if ([view isKindOfClass:WKWebView.class]) return (WKWebView *)view;
    for (NSView *child in view.subviews) {
        WKWebView *found = card_menu_webview(child);
        if (found) return found;
    }
    return nil;
}

// Invoked on AppKit's main thread. The target and menu belong to this single
// tracking loop; no registrations or callbacks survive its return.
int devjay_card_menu(void *pointer, double x, double y, const char *first, const char *second, const char *settings, const char *quit, const char *action, const char *actionDisabled, const char *hint, int strip, int siteWindowsHidden) {
    if (![NSThread isMainThread] || !pointer || !isfinite(x) || !isfinite(y)) return 0;
    @autoreleasepool {
        NSWindow *window = (NSWindow *)pointer;
        WKWebView *view = card_menu_webview(window.contentView);
        if (!view || !window.visible || x < 0 || y < 0 || x > NSWidth(view.bounds) || y > NSHeight(view.bounds)) return 0;
        NSString *firstTitle = first ? [NSString stringWithUTF8String:first] : nil;
        NSString *secondTitle = second ? [NSString stringWithUTF8String:second] : nil;
        NSString *settingsTitle = settings ? [NSString stringWithUTF8String:settings] : nil;
        NSString *quitTitle = quit ? [NSString stringWithUTF8String:quit] : nil;
        NSString *actionTitle = action ? [NSString stringWithUTF8String:action] : @"";
        NSString *disabledReason = actionDisabled ? [NSString stringWithUTF8String:actionDisabled] : @"";
        NSString *hintText = hint ? [NSString stringWithUTF8String:hint] : @"";
        if (!firstTitle || !secondTitle || !settingsTitle.length || !quitTitle.length || !actionTitle || !disabledReason || !hintText) return 0;
        if (!firstTitle.length && (secondTitle.length || (!actionTitle.length && !strip))) return 0;

        DevJayCardMenuSelection *selection = [[[DevJayCardMenuSelection alloc] init] autorelease];
        NSMenu *menu = [[[NSMenu alloc] initWithTitle:@""] autorelease];
        menu.autoenablesItems = NO;
        NSArray<NSString *> *titles = @[firstTitle, secondTitle, actionTitle, strip ? @"Minimize strip" : @"", strip ? @"Hide strip" : @"", strip ? (siteWindowsHidden ? @"Show site windows" : @"Hide site windows") : @"", settingsTitle, quitTitle];
        const NSInteger tags[] = {1, 2, 5, 6, 7, siteWindowsHidden ? 9 : 8, 3, 4};
        for (NSUInteger index = 0; index < titles.count; index++) {
            if (!titles[index].length) continue;
            if ((index == 3 || (index == 6 && !strip)) && menu.numberOfItems) [menu addItem:[NSMenuItem separatorItem]];
            NSMenuItem *item = [[[NSMenuItem alloc] initWithTitle:titles[index] action:@selector(selectItem:) keyEquivalent:@""] autorelease];
            item.target = selection;
            item.tag = tags[index];
            if (index == 3) item.toolTip = @"Show a single row of Site names";
            if (index == 4) item.toolTip = @"Hide the strip completely";
            if (index == 5) item.toolTip = @"Hide or restore independent Site windows without changing the strip";
            if (index == 2) {
                item.enabled = !disabledReason.length;
                item.toolTip = disabledReason.length ? disabledReason : hintText;
            }
            [menu addItem:item];
        }
        NSPoint point = NSMakePoint(NSMinX(view.bounds) + x,
            view.isFlipped ? NSMinY(view.bounds) + y : NSMaxY(view.bounds) - y);
        // Keep the hosting view/window alive if the Site is removed while the
        // native menu tracks; Go revalidates membership before acting.
        [window retain];
        [view retain];
        [menu popUpMenuPositioningItem:nil atLocation:point inView:view];
        NSInteger selected = selection.selected;
        [view release];
        [window release];
        return (int)selected;
    }
}
