#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
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

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
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
