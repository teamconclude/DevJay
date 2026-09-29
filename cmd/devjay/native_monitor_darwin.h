#ifndef DEVJAY_NATIVE_MONITOR_H
#define DEVJAY_NATIVE_MONITOR_H

enum {
    DevJayMonitorSpace = 1u << 0,
    DevJayMonitorPlacement = 1u << 1,
    DevJayMonitorVisibility = 1u << 2,
    DevJayMonitorApplications = 1u << 3,
    DevJayMonitorPower = 1u << 4
};

void devjay_start_native_monitor(void);
int devjay_monitor_window_visible(void *pointer);
void devjay_native_monitor_changed(unsigned int flags);

#ifdef __OBJC__
#define DevJayLayoutChangedNotification @"DevJayLayoutChangedNotification"
#endif

#endif
