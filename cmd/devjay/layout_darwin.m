#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <objc/runtime.h>
#import "layout_geometry.h"
#import "spaces_darwin.h"

static NSString * const layoutKey = @"DevJay.WindowLayout.v1";
static const char layoutObserverKey;
static DPRect dp_rect(NSRect r) { return (DPRect){r.origin.x, r.origin.y, r.size.width, r.size.height}; }
static NSRect ns_rect(DPRect r) { return NSMakeRect(r.x, r.y, r.width, r.height); }

static NSRect placement_bounds(NSScreen *screen) {
    return ns_rect(dp_placement_bounds(dp_rect(screen.frame), dp_rect(screen.visibleFrame)));
}

static NSString *screen_identifier(NSScreen *screen) {
    NSNumber *number = screen.deviceDescription[@"NSScreenNumber"];
    if (number == nil) return @"";
    CFUUIDRef uuid = CGDisplayCreateUUIDFromDisplayID(number.unsignedIntValue);
    if (uuid == NULL) return @"";
    CFStringRef string = CFUUIDCreateString(NULL, uuid);
    CFRelease(uuid);
    return [(NSString *)string autorelease];
}

static NSScreen *nearest_screen(NSRect frame) {
    NSArray<NSScreen *> *screens = NSScreen.screens;
    if (screens.count == 0) return nil;
    DPRect *rects = calloc(screens.count, sizeof(DPRect));
    if (rects == NULL) return screens.firstObject;
    for (NSUInteger i = 0; i < screens.count; i++) rects[i] = dp_rect(placement_bounds(screens[i]));
    NSUInteger index = dp_nearest_screen(dp_rect(frame), rects, screens.count);
    free(rects);
    return screens[index];
}


@interface DevJayLayoutObserver : NSObject {
@public
    NSWindow *window;
    BOOL adjusting;
    BOOL restoreFailed;
    NSString *desiredSpace;
    NSString *failedLaunchSpace;
    NSTimer *timer;
    NSString *lastScreen;
    NSRect lastFrame, lastBounds;
    BOOL hasPlacement;
}
- (void)save;
- (void)restoreDesktop;
- (void)finishRestoration;
- (void)windowMoved:(NSNotification *)notification;
- (void)screensChanged:(NSNotification *)notification;
- (void)spaceChanged:(NSNotification *)notification;
@end
@implementation DevJayLayoutObserver
- (void)save {
    if (adjusting || window == nil) return;
    // AppKit can move windows before sending the display-change notification.
    // Do not overwrite their old edge distances with that automatic position.
    if (hasPlacement) {
        NSScreen *previousScreen = nil;
        for (NSScreen *candidate in NSScreen.screens) {
            if ([screen_identifier(candidate) isEqual:lastScreen]) { previousScreen = candidate; break; }
        }
        if (!previousScreen || !NSEqualRects(placement_bounds(previousScreen), lastBounds)) {
            [self screensChanged:nil];
            return;
        }
    }
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    NSDictionary *stored = [defaults dictionaryForKey:layoutKey];
    NSMutableDictionary *all = stored ? [[stored mutableCopy] autorelease] : [NSMutableDictionary dictionary];
    NSScreen *screen = window.screen ?: nearest_screen(window.frame);
    if (!screen) return;
    [lastScreen release]; lastScreen = [screen_identifier(screen) copy];
    lastFrame = window.frame;
    lastBounds = placement_bounds(screen);
    hasPlacement = YES;
    NSMutableDictionary *state = [NSMutableDictionary dictionaryWithDictionary:@{
        @"frame": NSStringFromRect(window.frame),
        @"screen": screen_identifier(screen),
        @"screenFrame": NSStringFromRect(screen.frame),
        @"visibleFrame": NSStringFromRect(screen.visibleFrame)
    }];
    id previous = all[window.identifier];
    NSString *uuid = [previous isKindOfClass:NSDictionary.class] ? previous[@"spaceUUID"] : nil;
    NSString *current = dp_window_space(window);
    if (restoreFailed && !failedLaunchSpace && current.length && ![current isEqual:desiredSpace]) {
        failedLaunchSpace = [current copy];
    }
    if (current.length && (!restoreFailed || [current isEqual:desiredSpace] || ![current isEqual:failedLaunchSpace])) {
        restoreFailed = NO;
        uuid = current;
        [failedLaunchSpace release]; failedLaunchSpace = nil;
    }
    if ([uuid isKindOfClass:NSString.class] && uuid.length) state[@"spaceUUID"] = uuid;
    if (![state isEqual:previous]) {
        all[window.identifier] = state;
        [defaults setObject:all forKey:layoutKey];
    }
}
- (void)restoreDesktop {
    if (desiredSpace.length && !(window.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces)) {
        if (![dp_window_space(window) isEqual:desiredSpace]) dp_move_to_space(window, desiredSpace);
    }
    [self performSelector:@selector(finishRestoration) withObject:nil afterDelay:0.25];
}
- (void)finishRestoration {
    BOOL allDesktops = window.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces;
    NSString *current = dp_window_space(window);
    if (desiredSpace.length && !allDesktops) {
        BOOL restored = [current isEqual:desiredSpace];
        restoreFailed = !restored;
        if (!restored) failedLaunchSpace = [current copy];
        NSLog(@"DevJay: %@ desktop restore %@ (wanted %@, actual %@)", window.identifier,
              restored ? @"verified" : @"failed; retaining saved assignment", desiredSpace, current);
    }
    adjusting = NO;
    [self save];
    // Polling detects moves between Spaces even if the frame and active Space
    // did not change. Writes occur only when the placement actually changes.
    timer = [NSTimer scheduledTimerWithTimeInterval:1 target:self selector:@selector(save) userInfo:nil repeats:YES];
}
- (void)windowMoved:(NSNotification *)notification { [self save]; }
- (void)screensChanged:(NSNotification *)notification {
    dp_invalidate_space_cache();
    BOOL wasAdjusting = adjusting;
    adjusting = YES;
    NSScreen *screen = nil;
    for (NSScreen *candidate in NSScreen.screens) {
        if ([screen_identifier(candidate) isEqual:lastScreen]) { screen = candidate; break; }
    }
    screen = screen ?: window.screen ?: nearest_screen(window.frame);
    if (screen) {
        DPRect frame = hasPlacement
            ? dp_reanchor(dp_rect(lastFrame), dp_rect(lastBounds), dp_rect(placement_bounds(screen)))
            : dp_clamp(dp_rect(window.frame), dp_rect(placement_bounds(screen)));
        [window setFrame:ns_rect(frame) display:YES];
        [lastScreen release]; lastScreen = [screen_identifier(screen) copy];
        lastFrame = window.frame; lastBounds = placement_bounds(screen); hasPlacement = YES;
    }
    adjusting = wasAdjusting;
    if (screen) [self save];
}
- (void)spaceChanged:(NSNotification *)notification {
    dp_invalidate_space_cache();
    // Space membership can change without changing a window's frame.
    [NSObject cancelPreviousPerformRequestsWithTarget:self selector:@selector(save) object:nil];
    [self performSelector:@selector(save) withObject:nil afterDelay:0.5];
}
- (void)dealloc {
    [NSObject cancelPreviousPerformRequestsWithTarget:self];
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:self];
    [lastScreen release];
    [desiredSpace release];
    [failedLaunchSpace release];
    [super dealloc];
}
@end

void devjay_restore_layout(void *pointer, const char *identifier) {
    NSWindow *window = (NSWindow *)pointer;
    if (window == nil) return;
    NSString *key = identifier ? [NSString stringWithUTF8String:identifier] : nil;
    if (!key.length) return;
    window.identifier = key;
    NSDictionary *all = [NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey];
    id state = all[window.identifier];
    NSScreen *screen = nil;
    NSRect frame = window.frame;
    if ([state isKindOfClass:NSDictionary.class]) {
        if ([state[@"frame"] isKindOfClass:NSString.class]) {
            NSRect saved = NSRectFromString(state[@"frame"]);
            if (isfinite(saved.origin.x) && isfinite(saved.origin.y) && saved.size.width > 0 && saved.size.height > 0) {
                frame.origin = saved.origin;
                if ([window.identifier isEqual:@"control-panel"] &&
                    isfinite(saved.size.width) && isfinite(saved.size.height)) {
                    frame.size = NSMakeSize(fmax(saved.size.width, window.minSize.width),
                                            fmax(saved.size.height, window.minSize.height));
                }
                for (NSScreen *candidate in NSScreen.screens) {
                    if ([screen_identifier(candidate) isEqual:state[@"screen"]]) { screen = candidate; break; }
                }
                // Existing records contain both rectangles. Reconstruct bounds
                // without the Dock exclusion, including for pre-fix placements.
                NSString *oldBounds = [state[@"screenFrame"] isKindOfClass:NSString.class]
                    ? state[@"screenFrame"] : state[@"visibleFrame"];
                if (screen && [oldBounds isKindOfClass:NSString.class]) {
                    NSRect oldScreen = NSRectFromString(oldBounds);
                    if (isfinite(oldScreen.origin.x) && isfinite(oldScreen.origin.y) &&
                        isfinite(oldScreen.size.width) && isfinite(oldScreen.size.height) &&
                        oldScreen.size.width > 0 && oldScreen.size.height > 0) {
                        if ([state[@"visibleFrame"] isKindOfClass:NSString.class]) {
                            NSRect visible = NSRectFromString(state[@"visibleFrame"]);
                            if (isfinite(NSMaxY(visible)) && visible.size.height > 0) {
                                oldScreen = ns_rect(dp_placement_bounds(dp_rect(oldScreen), dp_rect(visible)));
                            }
                        }
                        frame = ns_rect(dp_reanchor(dp_rect(frame), dp_rect(oldScreen), dp_rect(placement_bounds(screen))));
                    }
                }
            }
        }
    }
    screen = screen ?: nearest_screen(frame);
    if (screen) [window setFrame:ns_rect(dp_clamp(dp_rect(frame), dp_rect(placement_bounds(screen)))) display:YES];
    DevJayLayoutObserver *observer = [[[DevJayLayoutObserver alloc] init] autorelease];
    observer->window = window;
    observer->adjusting = YES;
    if ([state isKindOfClass:NSDictionary.class] && [state[@"spaceUUID"] isKindOfClass:NSString.class]) {
        observer->desiredSpace = [state[@"spaceUUID"] copy];
    }
    objc_setAssociatedObject(window, &layoutObserverKey, observer, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
    NSNotificationCenter *center = NSNotificationCenter.defaultCenter;
    [center addObserver:observer selector:@selector(windowMoved:) name:NSWindowDidMoveNotification object:window];
    [center addObserver:observer selector:@selector(windowMoved:) name:NSWindowDidResizeNotification object:window];
    [center addObserver:observer selector:@selector(screensChanged:) name:NSApplicationDidChangeScreenParametersNotification object:nil];
    [NSWorkspace.sharedWorkspace.notificationCenter addObserver:observer selector:@selector(spaceChanged:) name:NSWorkspaceActiveSpaceDidChangeNotification object:nil];
    [observer performSelector:@selector(restoreDesktop) withObject:nil afterDelay:0.4];
    NSLog(@"DevJay: restored %@ to %@ on %@", window.identifier, NSStringFromRect(window.frame), screen.localizedName);
}

void devjay_save_layout(void *pointer) {
    NSWindow *window = (NSWindow *)pointer;
    DevJayLayoutObserver *observer = objc_getAssociatedObject(window, &layoutObserverKey);
    if (observer == nil) return;
    [observer save];
    [observer->timer invalidate];
    observer->timer = nil;
    [NSObject cancelPreviousPerformRequestsWithTarget:observer];
}

// Resize around the top-left corner, keeping the result on the same display.
void devjay_fit_panel(void *pointer, int mode, int count, int header, double extentWidth, double extentHeight) {
    NSWindow *window = (NSWindow *)pointer;
    NSScreen *screen = window.screen ?: nearest_screen(window.frame);
    if (!window || !screen) return;
    NSRect available = placement_bounds(screen);
    NSRect chrome = [window frameRectForContentRect:NSMakeRect(0, 0, 0, 0)];
    DPRect size = dp_panel_size(mode, count, available.size.width - 16 - chrome.size.width,
                               available.size.height - 16 - chrome.size.height, header, extentWidth, extentHeight);
    NSRect frame = [window frameRectForContentRect:NSMakeRect(0, 0, size.width, size.height)];
    frame.origin = NSMakePoint(window.frame.origin.x, NSMaxY(window.frame) - frame.size.height);
    [window setFrame:ns_rect(dp_clamp(dp_rect(frame), dp_rect(available))) display:YES];
}


void devjay_grow_card_window(void *pointer, double height, double viewportWidth, double viewportHeight) {
    NSWindow *window = (NSWindow *)pointer;
    NSScreen *screen = window.screen ?: nearest_screen(window.frame);
    if (!window || !screen || !isfinite(height) || height <= 0 || height > 1000000) return;
    // Ignore measurements from before a user resize or decoration change.
    NSRect content = [window contentRectForFrameRect:window.frame];
    if (fabs(content.size.width - viewportWidth) > 2 || fabs(content.size.height - viewportHeight) > 2) return;
    NSRect wanted = [window frameRectForContentRect:NSMakeRect(0, 0, content.size.width, height)];
    NSRect frame = ns_rect(dp_grow_height(dp_rect(window.frame), dp_rect(placement_bounds(screen)), wanted.size.height));
    if (!NSEqualRects(frame, window.frame)) [window setFrame:frame display:YES];
}
