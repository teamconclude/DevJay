#import <AppKit/AppKit.h>

static NSRunningApplication *development_app(int pid, const char *bundle) {
    if (![NSThread isMainThread] || pid <= 0 || !bundle) return nil;
    NSString *identifier = [NSString stringWithUTF8String:bundle];
    if (!identifier.length) return nil;
    NSRunningApplication *app = [NSRunningApplication runningApplicationWithProcessIdentifier:pid];
    return app && !app.terminated && [app.bundleIdentifier isEqual:identifier] ? app : nil;
}

int devjay_development_app_available(int pid, const char *bundle) {
    @autoreleasepool { return development_app(pid, bundle) != nil; }
}

int devjay_activate_development_app(int pid, const char *bundle) {
    @autoreleasepool {
        NSRunningApplication *app = development_app(pid, bundle);
        if (!app) return 0;
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

// Main-thread, nonblocking handoff after explicit Site navigation. Returning
// pending keeps DevJay in front until AppKit has switched to the Site's Space.
// A user who changes focus while that switch settles cancels the handoff.
// 1 = activated, 0 = still switching, -1 = unavailable, -2 = focus moved away.
int devjay_activate_development_app_on_site(void *pointer, unsigned int number, int pid, const char *bundle) {
    if (![NSThread isMainThread]) return -1;
    NSWindow *site = (NSWindow *)pointer;
    if (!site || number == 0 || site.windowNumber != number || !site.visible || site.miniaturized ||
        !devjay_development_app_available(pid, bundle)) return -1;
    if (!NSApp.active || NSApp.keyWindow != site) return -2;
    if (!site.onActiveSpace) return 0;
    return devjay_activate_development_app(pid, bundle) ? 1 : -1;
}
