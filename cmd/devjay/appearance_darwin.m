#import <AppKit/AppKit.h>
#import <QuartzCore/QuartzCore.h>

int devjay_system_appearance_dark(void) {
    @autoreleasepool {
        return [[NSUserDefaults.standardUserDefaults stringForKey:@"AppleInterfaceStyle"] isEqual:@"Dark"];
    }
}

// Called only on AppKit's main thread. New windows and native dialogs inherit
// this appearance; changing it never orders, activates or repositions a window.
void devjay_set_appearance(int mode) {
    NSApp.appearance = mode < 0 ? nil : [NSAppearance appearanceNamed:mode ? NSAppearanceNameAqua : NSAppearanceNameDarkAqua];
    [NSApp.effectiveAppearance performAsCurrentDrawingAppearance:^{
        // Layer colors are resolved CGColors, so refresh already-open popovers.
        for (NSWindow *window in NSApp.windows) {
            BOOL permission = [window.identifier isEqual:@"claude-permission-popover"];
            if (permission || [window.identifier isEqual:@"app-status-tooltip"]) {
                window.contentView.layer.backgroundColor = NSColor.windowBackgroundColor.CGColor;
                if (permission) window.contentView.layer.borderColor = NSColor.systemOrangeColor.CGColor;
            }
        }
    }];
}
