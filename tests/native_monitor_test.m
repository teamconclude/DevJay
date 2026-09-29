#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/native_monitor_darwin.h"

static unsigned int receivedFlags, receivedCalls, invalidations;
void devjay_native_monitor_changed(unsigned int flags) { receivedFlags |= flags; receivedCalls++; }
void dp_invalidate_space_cache(void) { invalidations++; }
BOOL dp_spaces_available(void) { return NO; }
NSString *dp_window_space(NSWindow *window) { (void)window; return nil; }
BOOL dp_move_to_space(NSWindow *window, NSString *space) { (void)window; (void)space; return NO; }

#include "../cmd/devjay/native_monitor_darwin.m"
#include "../cmd/devjay/layout_darwin.m"

// No actual window is shown or activated by these notification/visibility tests.
@interface MonitorTestWindow : NSObject {
@public
    NSString *name;
    BOOL shown, mini, active;
    NSWindowOcclusionState occlusion;
}
@end
@implementation MonitorTestWindow
- (BOOL)isKindOfClass:(Class)class { return class == NSWindow.class || [super isKindOfClass:class]; }
- (NSString *)identifier { return name; }
- (BOOL)isVisible { return shown; }
- (BOOL)isMiniaturized { return mini; }
- (BOOL)isOnActiveSpace { return active; }
- (NSWindowOcclusionState)occlusionState { return occlusion; }
- (void)orderFrontRegardless { assert(!"monitor must not order windows"); }
- (void)makeKeyAndOrderFront:(id)sender { (void)sender; assert(!"monitor must not change focus"); }
@end

@interface LayoutTestObserver : DevJayLayoutObserver {
@public
    unsigned int saves;
}
@end
@implementation LayoutTestObserver
- (void)save { saves++; }
@end

@interface LayoutTestScreen : NSObject
@end
@implementation LayoutTestScreen
- (NSDictionary *)deviceDescription { return @{}; }
- (NSRect)frame { return NSMakeRect(0, 0, 2000, 1200); }
- (NSRect)visibleFrame { return self.frame; }
@end

@interface LayoutSaveTestWindow : MonitorTestWindow {
@public
    NSRect placement;
    LayoutTestScreen *display;
}
@end
@implementation LayoutSaveTestWindow
- (NSRect)frame { return placement; }
- (NSScreen *)screen { return (NSScreen *)display; }
- (NSWindowCollectionBehavior)collectionBehavior { return 0; }
@end

// Geometry persistence tests use an in-memory defaults stand-in. No real
// window, screen assignment, or user's preferences are changed.
@interface LayoutTestDefaults : NSObject {
@public
    NSDictionary *layout;
    unsigned int writes;
}
@end
@implementation LayoutTestDefaults
- (NSDictionary *)dictionaryForKey:(NSString *)key { assert([key isEqual:layoutKey]); return layout; }
- (void)setObject:(id)value forKey:(NSString *)key {
    assert([key isEqual:layoutKey]);
    [layout release]; layout = [value copy]; writes++;
}
- (void)dealloc { [layout release]; [super dealloc]; }
@end
static LayoutTestDefaults *testDefaults;
static id memoryDefaults(id cls, SEL selector) { (void)cls; (void)selector; return testDefaults; }

static void resetEvents(void) { receivedFlags = receivedCalls = invalidations = 0; }

int main(void) {
    @autoreleasepool {
        NSNotificationCenter *workspace = NSWorkspace.sharedWorkspace.notificationCenter;
        NSNotificationCenter *windows = NSNotificationCenter.defaultCenter;
        devjay_start_native_monitor();
        devjay_start_native_monitor();
        resetEvents();
        [workspace postNotificationName:NSWorkspaceActiveSpaceDidChangeNotification object:nil];
        assert(receivedCalls == 1 && invalidations == 1);
        assert(receivedFlags == (DevJayMonitorSpace | DevJayMonitorVisibility));
        resetEvents();
        [workspace postNotificationName:NSWorkspaceDidActivateApplicationNotification object:nil];
        assert(receivedFlags == (DevJayMonitorApplications | DevJayMonitorVisibility));
        assert(invalidations == 0);
        resetEvents();
        [workspace postNotificationName:NSWorkspaceScreensDidSleepNotification object:nil];
        assert(receivedFlags == (DevJayMonitorPower | DevJayMonitorVisibility));

        MonitorTestWindow *window = [[[MonitorTestWindow alloc] init] autorelease];
        window->name = @"site-one";
        window->shown = window->active = YES;
        window->occlusion = NSWindowOcclusionStateVisible;
        resetEvents();
        [windows postNotificationName:NSWindowDidMoveNotification object:window];
        assert(receivedFlags == (DevJayMonitorPlacement | DevJayMonitorVisibility));
        assert(invalidations == 1);
        resetEvents();
        [windows postNotificationName:NSWindowDidChangeOcclusionStateNotification object:window];
        assert(receivedFlags == DevJayMonitorVisibility && invalidations == 0);
        resetEvents();
        window->name = @"app-status-tooltip";
        [windows postNotificationName:NSWindowDidMoveNotification object:window];
        assert(receivedCalls == 0); // Tooltip motion does not restart monitoring.
        window->name = @"site-one";

        assert(devjay_monitor_window_visible(window));
        window->active = NO;
        assert(!devjay_monitor_window_visible(window));
        window->active = YES; window->shown = NO;
        assert(!devjay_monitor_window_visible(window));
        window->shown = YES; window->mini = YES;
        assert(!devjay_monitor_window_visible(window));
        window->mini = NO; window->occlusion = 0;
        assert(!devjay_monitor_window_visible(window));
        assert(!devjay_monitor_window_visible(NULL));

        testDefaults = [[LayoutTestDefaults alloc] init];
        Method defaultsMethod = class_getClassMethod(NSUserDefaults.class, @selector(standardUserDefaults));
        IMP originalDefaults = method_setImplementation(defaultsMethod, (IMP)memoryDefaults);
        LayoutSaveTestWindow *savedWindow = [[[LayoutSaveTestWindow alloc] init] autorelease];
        savedWindow->name = @"site-layout-test";
        savedWindow->display = [[[LayoutTestScreen alloc] init] autorelease];
        savedWindow->placement = NSMakeRect(20, 30, 200, 100);
        DevJayLayoutObserver *savedObserver = [[DevJayLayoutObserver alloc] init];
        savedObserver->window = (NSWindow *)savedWindow;
        resetEvents();
        [savedObserver save];
        assert(testDefaults->writes == 1 && receivedCalls == 1);
        assert(receivedFlags == (DevJayMonitorPlacement | DevJayMonitorVisibility));
        resetEvents();
        savedObserver->hasPlacement = NO; // Fake screen is not in NSScreen.screens.
        [savedObserver save];
        assert(testDefaults->writes == 1 && receivedCalls == 0);
        savedWindow->placement.origin.x += 10;
        savedObserver->hasPlacement = NO;
        [savedObserver save];
        assert(testDefaults->writes == 2 && receivedCalls == 1);
        resetEvents();
        savedObserver->hasPlacement = NO;
        [savedObserver finishRestoration];
        assert(testDefaults->writes == 2 && receivedCalls == 1);
        assert(receivedFlags == (DevJayMonitorPlacement | DevJayMonitorVisibility));
        [savedObserver stopObserving];
        [savedObserver release];
        method_setImplementation(defaultsMethod, originalDefaults);
        [testDefaults release]; testDefaults = nil;

        // N windows share one tolerant fallback, and overlapping sleep causes
        // suspend it until every cause clears. Directly firing avoids a wait.
        DevJayLayoutMonitor *monitor = [[DevJayLayoutMonitor alloc] init];
        LayoutTestObserver *first = [[LayoutTestObserver alloc] init];
        LayoutTestObserver *second = [[LayoutTestObserver alloc] init];
        [monitor add:first];
        NSTimer *fallback = monitor->timer;
        [monitor add:second];
        assert(fallback == monitor->timer && fallback.timeInterval == 60 && fallback.tolerance == 10);
        [fallback fire];
        assert(first->saves == 1 && second->saves == 1);
        [workspace postNotificationName:NSWorkspaceScreensDidSleepNotification object:nil];
        [workspace postNotificationName:NSWorkspaceSessionDidResignActiveNotification object:nil];
        assert(monitor->timer == nil);
        [monitor checkLayouts];
        assert(first->saves == 1 && second->saves == 1);
        [workspace postNotificationName:NSWorkspaceScreensDidWakeNotification object:nil];
        assert(monitor->timer == nil);
        [workspace postNotificationName:NSWorkspaceSessionDidBecomeActiveNotification object:nil];
        assert(monitor->timer != nil);
        [monitor remove:first];
        [monitor->timer fire];
        assert(first->saves == 1 && second->saves == 2);
        [monitor remove:second];
        assert(monitor->timer == nil);
        [first release]; [second release]; [monitor release];

        // An observer disappearing without an explicit close must not leave a
        // strong timer/observer/window cycle. The shared fallback retires itself.
        monitor = [[DevJayLayoutMonitor alloc] init];
        @autoreleasepool {
            first = [[LayoutTestObserver alloc] init];
            [monitor add:first];
            [first release];
        }
        assert(monitor->observers.allObjects.count == 0);
        [monitor checkLayouts];
        assert(monitor->timer == nil);
        [monitor release];
        puts("Native monitoring: event, visibility, shared layout fallback and sleep checks passed");
    }
    return 0;
}
