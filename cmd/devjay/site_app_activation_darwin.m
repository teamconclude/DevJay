#import <AppKit/AppKit.h>

int devjay_activate_development_app(int pid, const char *bundle) {
    if (![NSThread isMainThread] || pid <= 0 || !bundle) return 0;
    @autoreleasepool {
        NSString *identifier = [NSString stringWithUTF8String:bundle];
        if (!identifier.length) return 0;
        NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
        if (!app || app.terminated || ![app.bundleIdentifier isEqual:identifier]) return 0;
        // This is an explicit click. Background observation never calls this.
        // Do not bring every app window forward or launch a missing app.
        if (@available(macOS 14.0, *)) {
            if (NSApp.active) {
                [NSApp yieldActivationToApplication:app];
                return [app activateFromApplication:NSRunningApplication.currentApplication options:0] ? 1 : 0;
            }
            return [app activateWithOptions:0] ? 1 : 0;
        }
#pragma clang diagnostic push
#pragma clang diagnostic ignored "-Wdeprecated-declarations"
        return [app activateWithOptions:NSApplicationActivateIgnoringOtherApps] ? 1 : 0;
#pragma clang diagnostic pop
    }
}
