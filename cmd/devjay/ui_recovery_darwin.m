#import <AppKit/AppKit.h>
#include <stdint.h>
#import <os/log.h>

// Accessed only on the main thread. Keep the public NSWorkspace observers for
// the app's lifetime; overlapping system/display/session suspension is tracked
// independently, so the first wake notification cannot resume a sleeping UI.
static unsigned int devjay_sleep_reasons;
static uint64_t devjay_wake_serial;
static NSMutableArray *devjay_power_observers;

void devjay_ui_power_state(unsigned int *sleepReasons, uint64_t *wakeSerial) {
    if (!devjay_power_observers) {
        devjay_power_observers = [NSMutableArray new];
        NSNotificationCenter *center = [[NSWorkspace sharedWorkspace] notificationCenter];
        NSArray *sleepNames = @[NSWorkspaceWillSleepNotification,
            NSWorkspaceScreensDidSleepNotification, NSWorkspaceSessionDidResignActiveNotification];
        NSArray *wakeNames = @[NSWorkspaceDidWakeNotification,
            NSWorkspaceScreensDidWakeNotification, NSWorkspaceSessionDidBecomeActiveNotification];
        for (NSUInteger i = 0; i < sleepNames.count; i++) {
            unsigned int reason = 1u << i;
            id asleep = [center addObserverForName:sleepNames[i] object:nil
                queue:[NSOperationQueue mainQueue] usingBlock:^(NSNotification *note) {
                    (void)note;
                    devjay_sleep_reasons |= reason;
                }];
            id awake = [center addObserverForName:wakeNames[i] object:nil
                queue:[NSOperationQueue mainQueue] usingBlock:^(NSNotification *note) {
                    (void)note;
                    devjay_sleep_reasons &= ~reason;
                    devjay_wake_serial++;
                }];
            [devjay_power_observers addObject:asleep];
            [devjay_power_observers addObject:awake];
        }
    }
    *sleepReasons = devjay_sleep_reasons;
    *wakeSerial = devjay_wake_serial;
}

void devjay_log_ui_recovery(const char *message) {
    os_log(OS_LOG_DEFAULT, "UI recovery: %{public}s", message);
}
