#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <objc/runtime.h>
#include <assert.h>
#include <stdio.h>

static unsigned int registered, unregistered, failMask, callbacks, lastAction, frontOrders, outOrders;
static unsigned int hintCallbacks, lastHints;
static id lastFront, lastOut;
static EventHandlerUPP installedHandler;
static OSStatus testInstall(EventTargetRef target, EventHandlerUPP handler, ItemCount count,
                           const EventTypeSpec *types, void *context, EventHandlerRef *ref) {
    (void)target; (void)context;
    assert(count == 2 && types[0].eventKind == kEventHotKeyPressed && types[1].eventKind == kEventHotKeyReleased);
    installedHandler = handler; *ref = (EventHandlerRef)(uintptr_t)1;
    return noErr;
}
static OSStatus testRegister(UInt32 code, UInt32 modifiers, EventHotKeyID key,
                            EventTargetRef target, OptionBits options, EventHotKeyRef *ref) {
    (void)target;
    const UInt32 codes[] = {kVK_ANSI_M,kVK_ANSI_1,kVK_ANSI_2,kVK_ANSI_3,kVK_ANSI_4,kVK_ANSI_5,
                           kVK_ANSI_6,kVK_ANSI_7,kVK_ANSI_8,kVK_ANSI_9,kVK_ANSI_0};
    assert(key.id < 11 && code == codes[key.id]);
    assert(modifiers == (controlKey | optionKey | cmdKey) && options == kEventHotKeyExclusive);
    registered++;
    if (failMask & (1u << key.id)) return eventHotKeyExistsErr;
    *ref = (EventHotKeyRef)(uintptr_t)(key.id + 1);
    return noErr;
}
static OSStatus testUnregister(EventHotKeyRef ref) { assert(ref); unregistered++; return noErr; }
static OSStatus testRemove(EventHandlerRef ref) { assert(ref); installedHandler = NULL; return noErr; }
#define InstallEventHandler testInstall
#define RegisterEventHotKey testRegister
#define UnregisterEventHotKey testUnregister
#define RemoveEventHandler testRemove
#include "../cmd/devjay/strip_controls_darwin.m"

static NSString *selectedSite;
void devjay_compact_site(const char *siteID) { [selectedSite release]; selectedSite = [[NSString stringWithUTF8String:siteID] copy]; }
void devjay_strip_control(int action) { callbacks++; lastAction = (unsigned int)action; }
void devjay_shortcut_hints(unsigned int mask) { hintCallbacks++; lastHints = mask; }
static void recordFront(id window, SEL selector) { (void)selector; frontOrders++; lastFront = window; }
static void recordOut(id window, SEL selector, id sender) { (void)selector; (void)sender; outOrders++; lastOut = window; }
static OSStatus keyEvent(UInt32 kind, OSType signature, UInt32 action) {
    EventRef event;
    assert(CreateEvent(NULL, kEventClassKeyboard, kind, 0, 0, &event) == noErr);
    EventHotKeyID key = {signature, action};
    assert(SetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID, sizeof(key), &key) == noErr);
    OSStatus status = strip_shortcut(NULL, event, NULL);
    ReleaseEvent(event);
    return status;
}
int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        // No system hotkeys are registered and no key events are posted.
        failMask = (1 << 2) | (1 << 10);
        assert(devjay_start_shortcuts() == failMask && registered == 11 && installedHandler);
        assert(shortcutHintTimer && shortcutHintTimer.valid);
        const NSEventModifierFlags modifiers = NSEventModifierFlagControl | NSEventModifierFlagOption | NSEventModifierFlagCommand;
        for (NSNumber *flag in @[@(NSEventModifierFlagControl), @(NSEventModifierFlagOption), @(NSEventModifierFlagCommand)]) {
            update_shortcut_hints(modifiers & ~flag.unsignedIntegerValue);
            assert(hintCallbacks == 0 && lastHints == 0);
        }
        update_shortcut_hints(modifiers);
        assert(hintCallbacks == 1 && lastHints == (0x1ff & ~(1 << 1))); // Failed shortcut 2 has no hint.
        update_shortcut_hints(modifiers | NSEventModifierFlagCapsLock);
        assert(hintCallbacks == 1); // Identical state does not wake Go/web views.
        update_shortcut_hints(modifiers | NSEventModifierFlagShift);
        assert(hintCallbacks == 2 && lastHints == 0);
        for (NSNumber *flag in @[@(NSEventModifierFlagControl), @(NSEventModifierFlagOption), @(NSEventModifierFlagCommand)]) {
            update_shortcut_hints(modifiers);
            update_shortcut_hints(modifiers & ~flag.unsignedIntegerValue);
            assert(lastHints == 0);
        }
        for (UInt32 i = 0; i < 11; i++) {
            unsigned int before = callbacks;
            OSStatus result = keyEvent(kEventHotKeyPressed, shortcutSignature, i);
            if (failMask & (1 << i)) { assert(result == eventNotHandledErr && callbacks == before); continue; }
            assert(result == noErr && callbacks == before + 1 && lastAction == i);
            assert(keyEvent(kEventHotKeyPressed, shortcutSignature, i) == noErr && callbacks == before + 1);
            assert(keyEvent(kEventHotKeyReleased, shortcutSignature, i) == noErr);
            assert(keyEvent(kEventHotKeyPressed, shortcutSignature, i) == noErr && callbacks == before + 2);
            assert(keyEvent(kEventHotKeyReleased, shortcutSignature, i) == noErr);
        }
        assert(keyEvent(kEventHotKeyPressed, 'Oops', 0) == eventNotHandledErr);
        assert(keyEvent(kEventHotKeyPressed, shortcutSignature, 99) == eventNotHandledErr);
        failMask = 0;
        NSTimer *originalTimer = shortcutHintTimer;
        assert(devjay_start_shortcuts() == 0 && registered == 13);
        assert(devjay_start_shortcuts() == 0 && registered == 13);
        assert(shortcutHintTimer == originalTimer);

        // Record window ordering without displaying anything on the user's desktop.
        Method front = class_getInstanceMethod(NSWindow.class, @selector(orderFrontRegardless));
        Method out = class_getInstanceMethod(NSWindow.class, @selector(orderOut:));
        IMP originalFront = method_setImplementation(front, (IMP)recordFront);
        IMP originalOut = method_setImplementation(out, (IMP)recordOut);
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(100, 100, 900, 174)
            styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO] autorelease];
        window.identifier = @"control-panel";
        window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary;
        window.level = NSFloatingWindowLevel;
        NSRect originalFrame = window.frame;
        NSWindowStyleMask originalStyle = window.styleMask;
        assert(devjay_set_strip_mode(NULL, DevJayStripHidden, NULL) == 0);
        assert(devjay_set_strip_mode(window, DevJayStripFull, NULL) == 1); // Restore before the first minimize is safe.
        const char *cards = "[{\"id\":\"site-a\",\"name\":\"Alpha\",\"path\":\"/tmp/Alpha\",\"active\":true},{\"id\":\"site-b\",\"name\":\"Beta\",\"path\":\"/tmp/Beta\",\"active\":false}]";
        for (int i = 0; i < 3; i++) {
            assert(devjay_set_strip_mode(window, DevJayStripHidden, cards) == 1);
            assert(NSEqualRects(window.frame, originalFrame) && window.styleMask == originalStyle);
            assert(!stripControls->badge.canBecomeKeyWindow && !stripControls->badge.canBecomeMainWindow);
            assert(stripControls->badge.frame.size.width == 52 && stripControls->badge.frame.size.height == 28);
            assert(stripControls->badge.level == window.level && stripControls->badge.collectionBehavior == window.collectionBehavior);
            assert(NSContainsRect((window.screen ?: NSScreen.mainScreen).visibleFrame, stripControls->badge.frame));
            NSButton *button = (NSButton *)stripControls->badge.contentView;
            assert([button acceptsFirstMouse:nil] && !button.acceptsFirstResponder);
            [button performClick:nil]; assert(lastAction == 10);
            [button.menu performActionForItemAtIndex:1]; assert(lastAction == 13);
            [button.menu performActionForItemAtIndex:2]; assert(lastAction == 11);
            NSRect area = (window.screen ?: NSScreen.mainScreen).visibleFrame;
            assert(NSMinX(stripControls->badge.frame) == MAX(NSMinX(area), NSMinX(originalFrame)));
            assert(NSMinY(stripControls->badge.frame) == MAX(NSMinY(area), NSMinY(originalFrame)));
            assert(devjay_set_strip_mode(window, DevJayStripCompact, cards) == 1);
            assert(stripControls->mode == DevJayStripCompact);
            assert(!stripControls->compact.canBecomeKeyWindow && !stripControls->compact.canBecomeMainWindow);
            assert(NSHeight(stripControls->compact.frame) == 40);
            assert(NSWidth(stripControls->compact.frame) == MIN(NSWidth(originalFrame), NSWidth(area)));
            assert(NSMinY(stripControls->compact.frame) == NSMinY(stripControls->badge.frame));
            NSArray *buttons = stripControls->scroll.documentView.subviews;
            assert(buttons.count == 2);
            DevJayStripRestoreButton *first = buttons[0], *second = buttons[1];
            assert([first.title isEqual:@"Alpha"] && [second.title isEqual:@"Beta"]);
            assert(first.layer.borderWidth == 2 && second.layer.borderWidth == 1);
            assert([first acceptsFirstMouse:nil] && !first.acceptsFirstResponder);
            [second performClick:nil]; assert([selectedSite isEqual:@"site-b"]);
            [first.menu performActionForItemAtIndex:1]; assert(lastAction == 12);
            devjay_update_compact_strip("[{\"id\":\"site-b\",\"name\":\"Beta updated\",\"path\":\"/tmp/Beta\",\"active\":true}]");
            buttons = stripControls->scroll.documentView.subviews;
            assert(buttons.count == 1 && [[buttons[0] title] isEqual:@"Beta updated"]);
            devjay_update_compact_strip("[{\"id\":3}]");
            assert(stripControls->scroll.documentView.subviews.count == 1);
            devjay_update_compact_strip("[]");
            first = stripControls->scroll.documentView.subviews[0];
            assert([first.title isEqual:@"Add a Site in Settings"]);
            [first performClick:nil]; assert(lastAction == 11);
            assert(NSEqualRects(window.frame, originalFrame) && window.styleMask == originalStyle);
            assert(devjay_set_strip_mode(window, DevJayStripFull, NULL) == 1);
            assert(NSEqualRects(window.frame, originalFrame) && window.styleMask == originalStyle);
        }
        assert(frontOrders == 10 && outOrders == 18);
        assert(devjay_set_strip_mode(window, DevJayStripHidden, cards) == 1);
        assert(lastFront == stripControls->badge);
        unsigned int before = callbacks;
        update_shortcut_hints(modifiers);
        assert(lastHints == 0x1ff && callbacks == before); // Hints never navigate.
        assert(lastFront == stripControls->compact && lastOut == stripControls->badge);
        assert(stripControls->mode == DevJayStripHidden && NSEqualRects(window.frame, originalFrame));
        DevJayStripRestoreButton *hintButton = stripControls->scroll.documentView.subviews[0];
        assert(hintButton.shortcutNumber == 1 && [hintButton.title isEqual:@"Alpha"]);
        // Live order changes while hidden update the peek and keep stable click IDs.
        devjay_update_compact_strip("[{\"id\":\"site-b\",\"name\":\"Beta\",\"path\":\"/tmp/Beta\",\"active\":false}]");
        hintButton = stripControls->scroll.documentView.subviews[0];
        assert(hintButton.shortcutNumber == 1 && [hintButton.siteID isEqual:@"site-b"]);
        [hintButton performClick:nil]; assert([selectedSite isEqual:@"site-b"]);
        update_shortcut_hints(0);
        assert(hintButton.shortcutNumber == 0 && lastFront == stripControls->badge && lastOut == stripControls->compact);
        assert(stripControls->mode == DevJayStripHidden);
        // Entering another mode while held retains its hints; releasing keeps that mode.
        update_shortcut_hints(modifiers);
        assert(devjay_set_strip_mode(window, DevJayStripCompact, cards) == 1);
        hintButton = stripControls->scroll.documentView.subviews[1];
        assert(hintButton.shortcutNumber == 2);
        unsigned int previousOrders = frontOrders;
        update_shortcut_hints(0);
        assert(hintButton.shortcutNumber == 0 && frontOrders == previousOrders);
        assert(stripControls->mode == DevJayStripCompact);
        update_shortcut_hints(modifiers);
        assert(devjay_set_strip_mode(window, DevJayStripFull, NULL) == 1);
        update_shortcut_hints(0);
        assert(lastFront == window && stripControls->mode == DevJayStripFull);
        NSRect narrow = originalFrame; narrow.size.width = 270;
        [window setFrame:narrow display:NO];
        assert(devjay_set_strip_mode(window, DevJayStripCompact, cards) == 1);
        [stripControls->scroll.contentView scrollToPoint:NSMakePoint(100, 0)];
        devjay_update_compact_strip(cards);
        assert(stripControls->scroll.contentView.bounds.origin.x == 100);
        devjay_update_compact_strip("[]");
        assert(stripControls->scroll.contentView.bounds.origin.x == 0);
        assert(devjay_set_strip_mode(window, DevJayStripFull, NULL) == 1);
        assert(NSEqualRects(window.frame, narrow));
        [window setFrame:originalFrame display:NO];
        assert(devjay_set_strip_mode(window, -1, NULL) == 0);
        assert(devjay_set_strip_mode(window, DevJayStripCompact, "{}") == 0);
        assert(stripControls->mode == DevJayStripFull);
        window.identifier = @"site-other";
        assert(devjay_set_strip_mode(window, DevJayStripHidden, NULL) == 0);
        devjay_stop_strip_controls();
        assert(unregistered == 11 && !installedHandler && !stripControls && shortcutsDown == 0 && !shortcutHintTimer && shortcutHints == 0);
        assert(keyEvent(kEventHotKeyPressed, shortcutSignature, 0) == eventNotHandledErr);
        method_setImplementation(front, originalFront);
        method_setImplementation(out, originalOut);
        [selectedSite release];
        puts("Native strip controls regressions passed");
    }
    return 0;
}
