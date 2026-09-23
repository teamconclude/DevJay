#import <AppKit/AppKit.h>
#include <assert.h>
#include <stdio.h>
#include "../ui_recovery_darwin.m"

int main(void) {
    @autoreleasepool {
        NSNotificationCenter *center = [[NSWorkspace sharedWorkspace] notificationCenter];
        unsigned int asleep; uint64_t serial;
        devjay_ui_power_state(&asleep, &serial);
        assert(asleep == 0 && serial == 0);
        [center postNotificationName:NSWorkspaceScreensDidSleepNotification object:nil];
        [center postNotificationName:NSWorkspaceWillSleepNotification object:nil];
        [center postNotificationName:NSWorkspaceSessionDidResignActiveNotification object:nil];
        devjay_ui_power_state(&asleep, &serial);
        assert(asleep == 7 && serial == 0);
        [center postNotificationName:NSWorkspaceDidWakeNotification object:nil];
        devjay_ui_power_state(&asleep, &serial);
        assert(asleep == 6 && serial == 1);
        [center postNotificationName:NSWorkspaceScreensDidWakeNotification object:nil];
        devjay_ui_power_state(&asleep, &serial);
        assert(asleep == 4 && serial == 2);
        [center postNotificationName:NSWorkspaceSessionDidBecomeActiveNotification object:nil];
        devjay_ui_power_state(&asleep, &serial);
        assert(asleep == 0 && serial == 3);
        // Repeated reads must not register duplicate observers.
        devjay_ui_power_state(&asleep, &serial);
        [center postNotificationName:NSWorkspaceScreensDidWakeNotification object:nil];
        devjay_ui_power_state(&asleep, &serial);
        assert(asleep == 0 && serial == 4);
        puts("UI power notifications: 6 checks passed");
    }
    return 0;
}
