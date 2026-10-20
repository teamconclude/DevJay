#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <objc/runtime.h>
#include <assert.h>
#include <stdio.h>

static unsigned int registered, unregistered, failMask, callbacks, lastAction, frontOrders, outOrders;
static unsigned int hintCallbacks, lastHints;
static id lastFront, lastOut;
static EventHandlerUPP installedHandler;
static BOOL customBindings;
static unsigned int captureCount, capturedModifiers;
static int capturedCode;
static unsigned long long capturedToken;
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
                           kVK_ANSI_6,kVK_ANSI_7,kVK_ANSI_8,kVK_ANSI_9,kVK_ANSI_0,kVK_ANSI_D,kVK_ANSI_S};
    assert(key.id < 13);
    if (!customBindings) assert(code == codes[key.id]);
    else if (key.id == 1) assert(code == kVK_F1);
    assert(modifiers == (customBindings && key.id == 1 ? controlKey | cmdKey : controlKey | optionKey | cmdKey));
    assert(options == kEventHotKeyExclusive);
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
void devjay_shortcut_recorded(unsigned long long token, int code, unsigned int modifiers) {
    captureCount++; capturedToken = token; capturedCode = code; capturedModifiers = modifiers;
}
@interface ShortcutRecordingTestWindow : NSWindow
@end
@implementation ShortcutRecordingTestWindow
- (BOOL)isVisible { return YES; }
- (BOOL)isKeyWindow { return YES; }
@end
static NSEvent *recordingEvent(NSEventType type, unsigned short code, NSEventModifierFlags flags) {
    return [NSEvent keyEventWithType:type location:NSZeroPoint modifierFlags:flags timestamp:0 windowNumber:0
        context:nil characters:@"" charactersIgnoringModifiers:@"" isARepeat:NO keyCode:code];
}

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
        assert(devjay_start_shortcuts() == failMask && registered == 13 && installedHandler);
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
        for (UInt32 i = 0; i < 13; i++) {
            unsigned int before = callbacks;
            OSStatus result = keyEvent(kEventHotKeyPressed, shortcutSignature, i);
            if (failMask & (1 << i)) { assert(result == eventNotHandledErr && callbacks == before); continue; }
            assert(result == noErr && callbacks == before + 1 && lastAction == (unsigned int)shortcutActions[i]);
            assert(keyEvent(kEventHotKeyPressed, shortcutSignature, i) == noErr && callbacks == before + 1);
            assert(keyEvent(kEventHotKeyReleased, shortcutSignature, i) == noErr);
            assert(keyEvent(kEventHotKeyPressed, shortcutSignature, i) == noErr && callbacks == before + 2);
            assert(keyEvent(kEventHotKeyReleased, shortcutSignature, i) == noErr);
        }
        assert(keyEvent(kEventHotKeyPressed, 'Oops', 0) == eventNotHandledErr);
        assert(keyEvent(kEventHotKeyPressed, shortcutSignature, 99) == eventNotHandledErr);
        failMask = 0;
        NSTimer *originalTimer = shortcutHintTimer;
        assert(devjay_start_shortcuts() == 0 && registered == 15);
        assert(devjay_start_shortcuts() == 0 && registered == 15);
        assert(shortcutHintTimer == originalTimer);

        NSMutableArray *bindings = [NSMutableArray array];
        for (unsigned int i = 0; i < 13; i++) [bindings addObject:@{@"keyCode":@(shortcutCodes[i]),@"modifiers":@11,@"label":@(i).stringValue}];
        bindings[1] = @{@"keyCode":@(kVK_F1),@"modifiers":@9,@"label":@"F1"};
        NSData *data = [NSJSONSerialization dataWithJSONObject:bindings options:0 error:nil];
        NSString *json = [[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] autorelease];
        customBindings = YES;
        assert(devjay_configure_shortcuts(json.UTF8String) == 0);
        update_shortcut_hints(NSEventModifierFlagControl | NSEventModifierFlagCommand);
        assert(shortcutHints == 1);
        update_shortcut_hints(modifiers);
        assert(shortcutHints == (0x1ff & ~1));
        unsigned int beforeInvalid = registered;
        assert(devjay_configure_shortcuts("[]") == 0x1fff && registered == beforeInvalid);
        NSWindow *recorder = [[[ShortcutRecordingTestWindow alloc] initWithContentRect:NSMakeRect(0,0,300,100)
            styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        recorder.identifier = @"settings";
        assert(devjay_record_shortcut(recorder, 42));
        assert(!shortcutRefs[1] && shortcutHints == 0);
        unsigned int beforeCapture = callbacks;
        assert(capture_shortcut_event(recordingEvent(NSEventTypeKeyDown,kVK_ANSI_K,modifiers)) == nil);
        assert(captureCount == 0 && callbacks == beforeCapture && shortcutRecordingWindow);
        assert(capture_shortcut_event(recordingEvent(NSEventTypeKeyUp,kVK_ANSI_K,modifiers)) == nil);
        assert(captureCount == 1 && capturedToken == 42 && capturedCode == kVK_ANSI_K && capturedModifiers == 11);
        assert(!shortcutRecordingWindow && shortcutRefs[1] && callbacks == beforeCapture);
        assert(devjay_record_shortcut(recorder,43));
        assert(capture_shortcut_event(recordingEvent(NSEventTypeKeyDown,kVK_Escape,0)) == nil);
        assert(captureCount == 2 && capturedCode == -1 && capturedToken == 43 && shortcutRefs[1]);
        assert(devjay_record_shortcut(recorder,44));
        [NSNotificationCenter.defaultCenter postNotificationName:NSWindowDidResignKeyNotification object:recorder];
        assert(captureCount == 3 && capturedCode == -1 && !shortcutRecordingWindow);
        assert(devjay_record_shortcut(recorder,45));
        [shortcutRecordingTimer fire];
        assert(captureCount == 4 && capturedCode == -1 && shortcutRefs[1]);
        assert(devjay_record_shortcut(recorder,46));
        NSEvent *click=[NSEvent mouseEventWithType:NSEventTypeLeftMouseDown location:NSZeroPoint modifierFlags:0 timestamp:0
            windowNumber:0 context:nil eventNumber:0 clickCount:1 pressure:1];
        assert(capture_shortcut_event(click)==click && captureCount==5 && capturedCode==-1);
        bindings[1] = @{@"keyCode":@(kVK_ANSI_1),@"modifiers":@11,@"label":@"1"};
        data = [NSJSONSerialization dataWithJSONObject:bindings options:0 error:nil];
        json = [[[NSString alloc] initWithData:data encoding:NSUTF8StringEncoding] autorelease];
        customBindings = NO; assert(devjay_configure_shortcuts(json.UTF8String)==0);
        unsigned int registeredBeforeStop = unregistered;

        // Record window ordering without displaying anything on the user's desktop.
        Method front = class_getInstanceMethod(NSWindow.class, @selector(orderFrontRegardless));
        Method out = class_getInstanceMethod(NSWindow.class, @selector(orderOut:));
        IMP originalFront = method_setImplementation(front, (IMP)recordFront);
        IMP originalOut = method_setImplementation(out, (IMP)recordOut);
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(100, 100, 900, 174)
            styleMask:NSWindowStyleMaskTitled backing:NSBackingStoreBuffered defer:NO] autorelease];
        window.identifier = @"strip";
        window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary;
        window.level = NSFloatingWindowLevel;
        NSRect originalFrame = window.frame;
        NSWindowStyleMask originalStyle = window.styleMask;
        assert(devjay_set_strip_mode(NULL, DevJayStripHidden, NULL) == 0);
        devjay_set_site_windows_hidden(1); // State can change before the compact row exists.
        assert(devjay_set_strip_mode(window, DevJayStripFull, NULL) == 1); // Restore before the first minimize is safe.
        const char *cards = "[{\"id\":\"site-a\",\"name\":\"Alpha\",\"path\":\"/tmp/Alpha\",\"active\":true},{\"id\":\"site-b\",\"name\":\"Beta\",\"path\":\"/tmp/Beta\",\"active\":false}]";
        for (int i = 0; i < 3; i++) {
            assert(devjay_set_strip_mode(window, DevJayStripHidden, cards) == 1);
            assert(NSEqualRects(window.frame, originalFrame) && window.styleMask == originalStyle);
            assert(lastOut == stripControls->compact);
            NSRect area = (window.screen ?: NSScreen.mainScreen).visibleFrame;
            assert(devjay_set_strip_mode(window, DevJayStripCompact, cards) == 1);
            assert(stripControls->mode == DevJayStripCompact);
            assert(!stripControls->compact.canBecomeKeyWindow && !stripControls->compact.canBecomeMainWindow);
            assert(NSHeight(stripControls->compact.frame) == 40);
            assert(NSWidth(stripControls->compact.frame) == MIN(NSWidth(originalFrame), NSWidth(area)));
            assert(NSMinY(stripControls->compact.frame) == MAX(NSMinY(area), NSMinY(originalFrame)));
            NSArray *buttons = stripControls->scroll.documentView.subviews;
            assert(buttons.count == 2);
            DevJayStripRestoreButton *first = buttons[0], *second = buttons[1];
            assert([first.title isEqual:@"Alpha"] && [second.title isEqual:@"Beta"]);
            assert(first.cell.lineBreakMode == NSLineBreakByTruncatingMiddle);
            assert(first.layer.borderWidth == 2 && second.layer.borderWidth == 1);
            assert([first acceptsFirstMouse:nil] && !first.acceptsFirstResponder);
            [second performClick:nil]; assert([selectedSite isEqual:@"site-b"]);
            [first.menu performActionForItemAtIndex:1]; assert(lastAction == 12);
            NSMenu *visibilityMenu = first.menu;
            assert(visibilityMenu == stripControls->scroll.menu && visibilityMenu.numberOfItems == 5);
            assert([[visibilityMenu itemAtIndex:2].title isEqual:@"Show site windows"]);
            [visibilityMenu performActionForItemAtIndex:2]; assert(lastAction == 19);
            devjay_set_site_windows_hidden(0);
            assert(first.menu == visibilityMenu && [[visibilityMenu itemAtIndex:2].title isEqual:@"Hide site windows"]);
            [visibilityMenu performActionForItemAtIndex:2]; assert(lastAction == 18);
            devjay_set_site_windows_hidden(1);
            assert([[visibilityMenu itemAtIndex:2].title isEqual:@"Show site windows"]);
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
        unsigned int beforeOrders = frontOrders, before = callbacks;
        assert(devjay_set_strip_mode(window, DevJayStripHidden, NULL) == 1);
        assert(frontOrders == beforeOrders && lastOut == stripControls->compact);
        update_shortcut_hints(modifiers);
        assert(lastHints == 0x1ff && callbacks == before);
        assert(frontOrders == beforeOrders); // Even held modifiers cannot show a hidden strip.
        assert(stripControls->mode == DevJayStripHidden && NSEqualRects(window.frame, originalFrame));
        devjay_update_compact_strip(cards);
        update_shortcut_hints(0);
        assert(frontOrders == beforeOrders && stripControls->mode == DevJayStripHidden);
        DevJayStripRestoreButton *hintButton;
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
        assert(unregistered == registeredBeforeStop + 13 && !installedHandler && !stripControls && shortcutsDown == 0 && !shortcutHintTimer && shortcutHints == 0);
        assert(keyEvent(kEventHotKeyPressed, shortcutSignature, 0) == eventNotHandledErr);
        method_setImplementation(front, originalFront);
        method_setImplementation(out, originalOut);
        [selectedSite release];
        puts("Native strip controls regressions passed");
    }
    return 0;
}
