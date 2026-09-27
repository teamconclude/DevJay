#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
#include "../cmd/devjay/focus_trace_darwin.m"
#include "../cmd/devjay/windows_darwin.m"
#include "../cmd/devjay/spaces_darwin.m"
#include "../cmd/devjay/layout_darwin.m"
#include "../cmd/devjay/editors_darwin.m"

static BOOL gotPathError;
void devjay_editor_opened(unsigned long long token, const char *error) {
    assert(token == 42);
    gotPathError = error && strstr(error, "UTF-8");
}
static int tag = 73, movedWithTag, moveResult;
static int readTag(int connection, uint64_t sid) { return tag; }
static int writeTag(int connection, uint64_t sid, int value) { tag = value; return 0; }
static int moveWindow(int connection, uint32_t *window, int count, int value) { movedWithTag = tag; return moveResult; }

@interface DevJayStripTestPanel : NSPanel {
@public
    NSUInteger orders, keyOrders;
}
@end
@implementation DevJayStripTestPanel
- (BOOL)canBecomeKeyWindow { return YES; } // Wails' default panel behavior.
- (void)orderFrontRegardless { orders++; }
- (void)makeKeyAndOrderFront:(id)sender { keyOrders++; }
@end

@interface DevJaySiteTestWindow : NSWindow {
@public
    NSUInteger keyOrders, mainRequests;
}
@end
@implementation DevJaySiteTestWindow
- (BOOL)canBecomeMainWindow { return YES; }
- (void)makeKeyAndOrderFront:(id)sender { keyOrders++; }
- (void)makeMainWindow { mainRequests++; }
@end

static NSUInteger activationRequests;
static void recordActivation(id app, SEL selector, BOOL ignoringOthers) { activationRequests++; }

static void test_strip_focus_isolation(void) {
    IMP panelBaseIMP = class_getMethodImplementation(NSPanel.class, @selector(canBecomeKeyWindow));
    IMP windowBaseIMP = class_getMethodImplementation(NSWindow.class, @selector(canBecomeKeyWindow));
    DevJayStripTestPanel *strip = [[[DevJayStripTestPanel alloc]
        initWithContentRect:NSMakeRect(0, 0, 300, 100)
        styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    DevJayStripTestPanel *other = [[[DevJayStripTestPanel alloc]
        initWithContentRect:NSMakeRect(0, 0, 300, 100)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskNonactivatingPanel
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    strip.identifier = @"control-panel";
    strip.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
        NSWindowCollectionBehaviorManaged | NSWindowCollectionBehaviorParticipatesInCycle;
    Class nativeClass = object_getClass(strip);
    assert(strip.canBecomeKeyWindow && other.canBecomeKeyWindow);
    assert(devjay_make_strip_mouse_only(strip));
    assert(devjay_make_strip_mouse_only(strip));
    assert(object_getClass(strip) == nativeClass);
    assert(!strip.canBecomeKeyWindow && !strip.canBecomeMainWindow);
    assert(other.canBecomeKeyWindow);
    assert(strip.styleMask & NSWindowStyleMaskNonactivatingPanel);
    assert(strip.collectionBehavior & NSWindowCollectionBehaviorCanJoinAllSpaces);
    assert(strip.collectionBehavior & NSWindowCollectionBehaviorStationary);
    assert(strip.collectionBehavior & NSWindowCollectionBehaviorIgnoresCycle);
    assert(!(strip.collectionBehavior & (NSWindowCollectionBehaviorManaged |
        NSWindowCollectionBehaviorTransient | NSWindowCollectionBehaviorParticipatesInCycle)));
    assert(panelBaseIMP == class_getMethodImplementation(NSPanel.class, @selector(canBecomeKeyWindow)));
    assert(windowBaseIMP == class_getMethodImplementation(NSWindow.class, @selector(canBecomeKeyWindow)));

    DevJaySiteTestWindow *site = [[[DevJaySiteTestWindow alloc]
        initWithContentRect:NSMakeRect(0, 0, 100, 100) styleMask:NSWindowStyleMaskTitled
        backing:NSBackingStoreBuffered defer:NO] autorelease];
    assert(!devjay_make_strip_mouse_only((NSPanel *)site));
    assert(site.canBecomeKeyWindow && site.canBecomeMainWindow);
    Method activation = class_getInstanceMethod(NSApplication.class, @selector(activateIgnoringOtherApps:));
    IMP original = method_setImplementation(activation, (IMP)recordActivation);
    devjay_focus_site(strip);
    assert(strip->orders == 1 && strip->keyOrders == 0 && activationRequests == 0);
    devjay_focus_site(site);
    assert(site->keyOrders == 1 && site->mainRequests == 1 && activationRequests == 1);
    method_setImplementation(activation, original);
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        test_strip_focus_isolation();
        NSView *panel = [[[NSView alloc] init] autorelease];
        NSView *confirmation = [[[NSView alloc] init] autorelease];
        IMP original = class_getMethodImplementation(NSView.class, @selector(acceptsFirstMouse:));
        assert(![confirmation acceptsFirstMouse:nil]);
        assert(!devjay_enable_first_mouse(panel));
        assert(![panel acceptsFirstMouse:nil]);
        assert(![confirmation acceptsFirstMouse:nil]);
        assert(original == class_getMethodImplementation(NSView.class, @selector(acceptsFirstMouse:)));

        // A plain NSView misses WKWebView's computed corner-radius properties.
        // Changing its public class identity aborts inside AppKit on resizing.
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0, 0, 900, 174)
            styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskResizable
            backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration = [[[WKWebViewConfiguration alloc] init] autorelease];
        configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *webPanel = [[[WKWebView alloc] initWithFrame:NSMakeRect(0, 0, 900, 174) configuration:configuration] autorelease];
        WKWebView *webConfirmation = [[[WKWebView alloc] initWithFrame:NSZeroRect configuration:configuration] autorelease];
        window.contentView = webPanel;
        Class publicClass = [webPanel class];
        Class runtimeClass = object_getClass(webPanel);
        BOOL confirmationFirstMouse = [webConfirmation acceptsFirstMouse:nil];
        assert(!confirmationFirstMouse);
        assert(devjay_enable_first_mouse(webPanel));
        assert([webPanel class] == publicClass);
        assert(object_getClass(webPanel) == runtimeClass);
        assert([webPanel acceptsFirstMouse:nil]);
        assert([webConfirmation acceptsFirstMouse:nil] == confirmationFirstMouse);
        assert(original == class_getMethodImplementation(NSView.class, @selector(acceptsFirstMouse:)));
        assert(devjay_enable_first_mouse(webPanel)); // Repeated runtime-ready notification.
        WKWebView *newConfirmation = [[[WKWebView alloc] initWithFrame:NSZeroRect configuration:configuration] autorelease];
        assert(![newConfirmation acceptsFirstMouse:nil]);
        for (NSNumber *width in @[@1348, @500, @900, @1348]) {
            [window setContentSize:NSMakeSize(width.doubleValue, 174)];
            [webPanel layoutSubtreeIfNeeded];
            assert(webPanel.frame.size.width == width.doubleValue);
        }

        // The single-instance reopen callback focuses the control panel.
        // NSPanel refuses main-window status and raises if forced into it.
        NSPanel *strip = [[[NSPanel alloc] initWithContentRect:NSMakeRect(0, 0, 900, 174)
            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
            backing:NSBackingStoreBuffered defer:NO] autorelease];
        assert(!strip.canBecomeMainWindow);
        devjay_focus_site(strip);
        assert(strip.isVisible);
        [strip orderOut:nil];

        getCompatID = readTag; setCompatID = writeTag; setWorkspace = moveWindow;
        assert(move_with_compat_tag(123, 456));
        assert(tag == 73 && movedWithTag == 0x79616265);
        moveResult = -1;
        assert(!move_with_compat_tag(123, 456));
        assert(tag == 73);
        getCompatID = NULL;
        assert(!move_with_compat_tag(123, 456));
        assert(tag == 73);

        devjay_open_editor("\xff", "/Applications/Test.app", 42);
        assert(gotPathError);
        gotPathError = NO;
        devjay_open_editor("/tmp/file", "\xff", 42);
        assert(gotPathError);
        // Invalid identifiers are rejected before touching a window or defaults.
        devjay_restore_layout((void *)panel, "\xff");
        puts("Native review regressions passed");
    }
    return 0;
}
