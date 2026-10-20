#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#import <objc/runtime.h>
#import "layout_geometry.h"
#import "spaces_darwin.h"
#import "native_monitor_darwin.h"

static NSString *layoutKey = @"DevJay.WindowLayout.v1";
static NSString *profileDisplaySignature;
static BOOL profileLayoutPaused;
static BOOL recoveringWindows;
static NSString *display_signature(void);
static const char layoutObserverKey;
// Compatibility only: retain older saved records while adopting the strip name.
static NSString *legacyStripLayoutIdentifier = @"control-panel";
static void migrate_strip_layout(NSString *key) {
    NSUserDefaults *defaults = NSUserDefaults.standardUserDefaults;
    NSDictionary *stored = [defaults dictionaryForKey:key];
    if (!stored[@"strip"] && [stored[legacyStripLayoutIdentifier] isKindOfClass:NSDictionary.class]) {
        NSMutableDictionary *updated = [[stored mutableCopy] autorelease];
        updated[@"strip"] = stored[legacyStripLayoutIdentifier];
        [defaults setObject:updated forKey:key];
    }
}

@class DevJayLayoutObserver;

// Most placement changes already have AppKit/Workspace notifications. Keep a
// single slow safety check for moves between inactive Spaces that change
// neither the frame nor the currently active Space. Weak membership avoids
// retaining windows/observers after they close.
@interface DevJayLayoutMonitor : NSObject {
@public
    NSHashTable *observers;
    NSTimer *timer;
    unsigned int sleepReasons;
    NSMutableArray *powerObservers;
}
- (void)add:(DevJayLayoutObserver *)observer;
- (void)remove:(DevJayLayoutObserver *)observer;
- (void)checkLayouts;
- (void)updateTimer;
- (void)screensChanged:(NSNotification *)notification;
- (void)recoverWindows;
- (void)recoverWindowsOnScreens:(NSArray<NSScreen *> *)screens;
- (void)windowExposed:(NSNotification *)notification;
@end

static DevJayLayoutMonitor *layoutMonitor;
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

static NSScreen *nearest_screen_in(NSRect frame, NSArray<NSScreen *> *screens) {
    if (screens.count == 0) return nil;
    DPRect *rects = calloc(screens.count, sizeof(DPRect));
    if (rects == NULL) return screens.firstObject;
    for (NSUInteger i = 0; i < screens.count; i++) rects[i] = dp_rect(placement_bounds(screens[i]));
    NSUInteger index = dp_nearest_screen(dp_rect(frame), rects, screens.count);
    free(rects);
    return screens[index];
}

static NSScreen *nearest_screen(NSRect frame) { return nearest_screen_in(frame, NSScreen.screens); }


static BOOL recoverable_window(NSWindow *window) {
    if (!window || !window.isVisible || window.isMiniaturized || window.alphaValue <= 0 ||
        (window.styleMask & NSWindowStyleMaskFullScreen)) return NO;
    NSString *name = window.identifier;
    return window.canBecomeKeyWindow || window.isSheet || [name isEqual:@"strip"] ||
        [name hasPrefix:@"site-"] || [name isEqual:@"compact-strip"] ||
        [name isEqual:@"settings"] || [name isEqual:@"git-viewer"] ||
        [name isEqual:@"suggested-actions"] || [name isEqual:@"message-dialog"] ||
        [name isEqual:@"profile-prompt"];
}

static NSRect recovery_bounds(NSWindow *window, NSScreen *screen) {
    // Floating cards can overlap the Dock; dialogs and editors must clear it.
    NSString *name = window.identifier;
    return [name isEqual:@"strip"] || [name hasPrefix:@"site-"] || [name isEqual:@"compact-strip"]
        ? placement_bounds(screen) : screen.visibleFrame;
}

static void recover_window(NSWindow *window, NSArray<NSScreen *> *screens) {
    if (!recoverable_window(window)) return;
    NSRect frame = window.frame;
    for (NSScreen *screen in screens) {
        if (NSContainsRect(recovery_bounds(window, screen), frame)) return;
    }
    NSScreen *screen = nearest_screen_in(frame, screens);
    if (!screen) return; // Display enumeration can be empty during a transition.
    NSRect fitted = ns_rect(dp_clamp(dp_rect(frame), dp_rect(recovery_bounds(window, screen))));
    if (!NSEqualRects(frame, fitted)) [window setFrame:fitted display:YES];
}

@interface DevJayLayoutObserver : NSObject {
@public
    NSWindow *window;
    BOOL adjusting;
    BOOL restoreFailed;
    NSString *desiredSpace;
    NSString *failedLaunchSpace;
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
- (void)windowClosed:(NSNotification *)notification;
- (void)stopObserving;
@end

@implementation DevJayLayoutMonitor
- (instancetype)init {
    self = [super init];
    if (self) {
        observers = [[NSHashTable weakObjectsHashTable] retain];
        powerObservers = [NSMutableArray new];
        NSNotificationCenter *windows = NSNotificationCenter.defaultCenter;
        [windows addObserver:self selector:@selector(screensChanged:) name:NSApplicationDidChangeScreenParametersNotification object:nil];
        for (NSNotificationName name in @[NSWindowDidBecomeKeyNotification, NSWindowDidDeminiaturizeNotification,
            NSWindowDidChangeOcclusionStateNotification]) {
            [windows addObserver:self selector:@selector(windowExposed:) name:name object:nil];
        }
        NSNotificationCenter *center = NSWorkspace.sharedWorkspace.notificationCenter;
        NSArray *sleepNames = @[NSWorkspaceWillSleepNotification,
            NSWorkspaceScreensDidSleepNotification, NSWorkspaceSessionDidResignActiveNotification];
        NSArray *wakeNames = @[NSWorkspaceDidWakeNotification,
            NSWorkspaceScreensDidWakeNotification, NSWorkspaceSessionDidBecomeActiveNotification];
        __block DevJayLayoutMonitor *monitor = self;
        for (NSUInteger i = 0; i < sleepNames.count; i++) {
            unsigned int reason = 1u << i;
            id asleep = [center addObserverForName:sleepNames[i] object:nil queue:NSOperationQueue.mainQueue
                usingBlock:^(NSNotification *note) {
                    (void)note;
                    monitor->sleepReasons |= reason;
                    [monitor updateTimer];
                }];
            id awake = [center addObserverForName:wakeNames[i] object:nil queue:NSOperationQueue.mainQueue
                usingBlock:^(NSNotification *note) {
                    (void)note;
                    monitor->sleepReasons &= ~reason;
                    [monitor updateTimer];
                    [monitor screensChanged:nil];
                }];
            [powerObservers addObject:asleep];
            [powerObservers addObject:awake];
        }
    }
    return self;
}
- (void)recoverWindows { [self recoverWindowsOnScreens:NSScreen.screens]; }
- (void)recoverWindowsOnScreens:(NSArray<NSScreen *> *)screens {
    if (sleepReasons || recoveringWindows) return;
    recoveringWindows = YES;
    // Move parents before sheets, whose positions AppKit may recompute.
    for (NSWindow *window in NSApp.windows) if (!window.isSheet) recover_window(window, screens);
    for (NSWindow *window in NSApp.windows) if (window.isSheet) recover_window(window, screens);
    recoveringWindows = NO;
}
- (void)screensChanged:(NSNotification *)notification {
    (void)notification;
    [NSObject cancelPreviousPerformRequestsWithTarget:self selector:@selector(recoverWindows) object:nil];
    [self recoverWindows];
    // AppKit and Profile restoration can move windows after the notification.
    // Two bounded passes cover settling, including while a sheet is open.
    for (NSNumber *delay in @[@0.25, @2.5]) {
        [self performSelector:@selector(recoverWindows) withObject:nil afterDelay:delay.doubleValue
            inModes:@[NSRunLoopCommonModes, NSModalPanelRunLoopMode]];
    }
}
- (void)windowExposed:(NSNotification *)notification {
    if (sleepReasons || recoveringWindows || ![notification.object isKindOfClass:NSWindow.class]) return;
    recoveringWindows = YES;
    recover_window(notification.object, NSScreen.screens);
    recoveringWindows = NO;
}
- (void)add:(DevJayLayoutObserver *)observer {
    [observers addObject:observer];
    [self updateTimer];
}
- (void)remove:(DevJayLayoutObserver *)observer {
    [observers removeObject:observer];
    [self updateTimer];
}
- (void)updateTimer {
    // NSHashTable.count can include zeroed weak slots until its next rehash.
    if (sleepReasons || observers.allObjects.count == 0) {
        [timer invalidate]; timer = nil;
    } else if (!timer) {
        timer = [NSTimer scheduledTimerWithTimeInterval:60 target:self
            selector:@selector(checkLayouts) userInfo:nil repeats:YES];
        timer.tolerance = 10;
    }
}
- (void)checkLayouts {
    if (!sleepReasons) for (DevJayLayoutObserver *observer in observers.allObjects) [observer save];
    [self updateTimer];
}
- (void)dealloc {
    [NSObject cancelPreviousPerformRequestsWithTarget:self];
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [timer invalidate];
    for (id observer in powerObservers) [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:observer];
    [powerObservers release];
    [observers release];
    [super dealloc];
}
@end

@implementation DevJayLayoutObserver
- (void)save {
    if (adjusting || recoveringWindows || window == nil || profileLayoutPaused ||
        (profileDisplaySignature && ![profileDisplaySignature isEqual:display_signature()])) return;
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
        // Restoring/moving between inactive Spaces can change membership
        // without a frame, occlusion, or active-Space notification.
        [NSNotificationCenter.defaultCenter postNotificationName:DevJayLayoutChangedNotification object:window];
    }
}
- (void)restoreDesktop {
    if (!window || profileLayoutPaused ||
        (profileDisplaySignature && ![profileDisplaySignature isEqual:display_signature()])) return;
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
    // The manager may have sampled the provisional launch Space before this
    // asynchronous restore. Returning to the saved placement does not change
    // preferences, so save's changed-only notification is not sufficient here.
    [NSNotificationCenter.defaultCenter postNotificationName:DevJayLayoutChangedNotification object:window];
    if (!layoutMonitor) layoutMonitor = [DevJayLayoutMonitor new];
    [layoutMonitor add:self];
}
- (void)windowMoved:(NSNotification *)notification { [self save]; }
- (void)screensChanged:(NSNotification *)notification {
    dp_invalidate_space_cache();
    if (profileLayoutPaused || (profileDisplaySignature && ![profileDisplaySignature isEqual:display_signature()])) return;
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
- (void)windowClosed:(NSNotification *)notification {
    (void)notification;
    [self save];
    [self stopObserving];
}
- (void)stopObserving {
    [NSObject cancelPreviousPerformRequestsWithTarget:self];
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [NSWorkspace.sharedWorkspace.notificationCenter removeObserver:self];
    [layoutMonitor remove:self];
    window = nil;
}
- (void)dealloc {
    [self stopObserving];
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
    if (!layoutMonitor) layoutMonitor = [DevJayLayoutMonitor new];
    DevJayLayoutObserver *previous = objc_getAssociatedObject(window, &layoutObserverKey);
    [previous stopObserving];
    window.identifier = key;
    migrate_strip_layout(layoutKey);
    NSDictionary *all = [NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey];
    id state = all[window.identifier];
    NSScreen *screen = nil;
    NSRect frame = window.frame;
    if ([state isKindOfClass:NSDictionary.class]) {
        if ([state[@"frame"] isKindOfClass:NSString.class]) {
            NSRect saved = NSRectFromString(state[@"frame"]);
            if (isfinite(saved.origin.x) && isfinite(saved.origin.y) && saved.size.width > 0 && saved.size.height > 0) {
                frame.origin = saved.origin;
                if ([window.identifier isEqual:@"strip"] &&
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
    [center addObserver:observer selector:@selector(windowClosed:) name:NSWindowWillCloseNotification object:window];
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
    [observer stopObserving];
}

// Resize around the top-left corner, keeping the result on the same display.
void devjay_fit_strip(void *pointer, int mode, int count, int header, double extentWidth, double extentHeight) {
    NSWindow *window = (NSWindow *)pointer;
    NSScreen *screen = window.screen ?: nearest_screen(window.frame);
    if (!window || !screen) return;
    NSRect available = placement_bounds(screen);
    NSRect chrome = [window frameRectForContentRect:NSMakeRect(0, 0, 0, 0)];
    DPRect size = dp_strip_size(mode, count, available.size.width - 16 - chrome.size.width,
                               available.size.height - 16 - chrome.size.height, header, extentWidth, extentHeight);
    NSRect frame = [window frameRectForContentRect:NSMakeRect(0, 0, size.width, size.height)];
    frame.origin = NSMakePoint(window.frame.origin.x, NSMaxY(window.frame) - frame.size.height);
    [window setFrame:ns_rect(dp_clamp(dp_rect(frame), dp_rect(available))) display:YES];
}


void devjay_fit_card_window(void *pointer, double height, double viewportWidth, double viewportHeight, int refit) {
    NSWindow *window = (NSWindow *)pointer;
    NSScreen *screen = window.screen ?: nearest_screen(window.frame);
    if (!window || !screen || !isfinite(height) || height <= 0 || height > 1000000 ||
        !isfinite(viewportWidth) || !isfinite(viewportHeight)) return;
    // Ignore measurements from before a user resize or decoration change.
    NSRect content = [window contentRectForFrameRect:window.frame];
    if (fabs(content.size.width - viewportWidth) > 2 || fabs(content.size.height - viewportHeight) > 2) return;
    BOOL fitStrip = refit && [window.identifier isEqual:@"strip"];
    if (fitStrip) height = MAX(132, height);
    NSRect wanted = [window frameRectForContentRect:NSMakeRect(0, 0, content.size.width, height)];
    DPRect available = dp_rect(placement_bounds(screen));
    NSRect frame = ns_rect(fitStrip ? dp_fit_height(dp_rect(window.frame), available, wanted.size.height)
        : dp_grow_height(dp_rect(window.frame), available, wanted.size.height));
    if (!NSEqualRects(frame, window.frame)) [window setFrame:frame display:YES];
}

// Display IDs identify hardware; names, resolution and position are presentation.
static NSString *display_signature(void) {
    NSMutableArray *ids = [NSMutableArray array];
    for (NSScreen *screen in NSScreen.screens) { NSString *identifier = screen_identifier(screen); if (identifier.length) [ids addObject:identifier]; }
    return [[ids sortedArrayUsingSelector:@selector(compare:)] componentsJoinedByString:@","];
}

char *devjay_profile_displays(void) {
    NSMutableArray *displays = [NSMutableArray array];
    for (NSScreen *screen in NSScreen.screens) {
        [displays addObject:@{@"id":screen_identifier(screen), @"name":screen.localizedName ?: @"Display",
            @"x":@(screen.frame.origin.x), @"y":@(screen.frame.origin.y), @"width":@(screen.frame.size.width), @"height":@(screen.frame.size.height)}];
    }
    NSData *data = [NSJSONSerialization dataWithJSONObject:@{@"signature":display_signature(), @"displays":displays} options:0 error:nil];
    NSString *json = data ? [[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] autorelease] : nil;
    return json ? strdup(json.UTF8String) : NULL;
}

void devjay_profile_pause(int paused) { profileLayoutPaused = paused != 0; }

int devjay_profile_capture(void *pointer) {
    DevJayLayoutObserver *observer = objc_getAssociatedObject((NSWindow *)pointer, &layoutObserverKey);
    if (!observer || !observer->window || observer->adjusting || profileLayoutPaused ||
        ![profileDisplaySignature isEqual:display_signature()]) return 0;
    [observer save];
    return 1;
}

int devjay_profile_seed_site(const char *identifier, const char *after, int desktop, const char *enabledJSON) {
    if (profileLayoutPaused || ![profileDisplaySignature isEqual:display_signature()]) return 0;
    NSString *key = [@"site-" stringByAppendingString:[NSString stringWithUTF8String:identifier]];
    NSDictionary *saved = [NSUserDefaults.standardUserDefaults dictionaryForKey:layoutKey] ?: @{};
    if (saved[key]) return 0; // Never displace an already saved Site.
    NSString *neighbor = [@"site-" stringByAppendingString:[NSString stringWithUTF8String:after]];
    NSDictionary *anchor = saved[neighbor] ?: saved[@"strip"];
    if (![anchor isKindOfClass:NSDictionary.class] || ![anchor[@"screen"] isKindOfClass:NSString.class]) return 0;
    NSString *space = dp_space_for_index(anchor[@"screen"],desktop);
    if (!space.length) return 0;
    NSData *data = [[NSString stringWithUTF8String:enabledJSON] dataUsingEncoding:NSUTF8StringEncoding];
    NSArray *enabled = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    for (NSString *site in enabled) {
        NSDictionary *placement = saved[[@"site-" stringByAppendingString:site]];
        if ([placement[@"spaceUUID"] isEqual:space]) return 0;
    }
    NSMutableDictionary *placement = [[anchor mutableCopy] autorelease];
    placement[@"spaceUUID"] = space;
    NSMutableDictionary *all = [[saved mutableCopy] autorelease];all[key] = placement;
    [NSUserDefaults.standardUserDefaults setObject:all forKey:layoutKey];
    return 1;
}

void devjay_profile_activate(const char *identifier, const char *signature, int migrate) {
    // Freeze outgoing records before replacing observers. A changed display set
    // already prevents save from recording macOS's automatic fallback positions.
    for (NSWindow *window in NSApp.windows) {
        DevJayLayoutObserver *observer = objc_getAssociatedObject(window, &layoutObserverKey);
        [observer save]; [observer stopObserving];
    }
    NSString *key = [@"DevJay.WindowLayout.Profile." stringByAppendingString:[NSString stringWithUTF8String:identifier]];
    if (migrate && ![NSUserDefaults.standardUserDefaults objectForKey:key]) {
        NSDictionary *legacy = [NSUserDefaults.standardUserDefaults dictionaryForKey:@"DevJay.WindowLayout.v1"];
        if (legacy) [NSUserDefaults.standardUserDefaults setObject:legacy forKey:key];
    }
    if (![layoutKey isEqual:@"DevJay.WindowLayout.v1"]) [layoutKey release];
    layoutKey = [key copy];
    migrate_strip_layout(layoutKey);
    [profileDisplaySignature release]; profileDisplaySignature = [[NSString stringWithUTF8String:signature] copy];
    profileLayoutPaused = NO;
}

void devjay_profile_delete(const char *identifier) {
    if (!identifier || !identifier[0]) return;
    NSString *key = [@"DevJay.WindowLayout.Profile." stringByAppendingString:[NSString stringWithUTF8String:identifier]];
    if (![key isEqual:layoutKey]) [NSUserDefaults.standardUserDefaults removeObjectForKey:key];
}
