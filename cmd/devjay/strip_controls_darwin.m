#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <QuartzCore/QuartzCore.h>

extern void devjay_strip_control(int action);
extern void devjay_shortcut_hints(unsigned int mask);

static const OSType shortcutSignature = 'DvJy';
static EventHotKeyRef shortcutRefs[13];
static EventHandlerRef shortcutHandler;
static unsigned int shortcutsDown;
static NSTimer *shortcutHintTimer;
static unsigned int shortcutHints;
static BOOL siteWindowsHidden; // Main-thread mirror for the compact strip menu.
static void update_shortcut_hints(NSEventModifierFlags flags);
static UInt32 shortcutCodes[] = {kVK_ANSI_M, kVK_ANSI_1, kVK_ANSI_2,
    kVK_ANSI_3, kVK_ANSI_4, kVK_ANSI_5, kVK_ANSI_6, kVK_ANSI_7,
    kVK_ANSI_8, kVK_ANSI_9, kVK_ANSI_0, kVK_ANSI_D, kVK_ANSI_S};
static const int shortcutActions[] = {0,1,2,3,4,5,6,7,8,9,10,16,17};
static unsigned int shortcutModifiers[] = {11,11,11,11,11,11,11,11,11,11,11,11,11};
static NSArray *shortcutKeyLabels;
static NSWindow *shortcutRecordingWindow;
static id shortcutRecordingMonitor;
static NSMutableArray *shortcutRecordingObservers;
static NSTimer *shortcutRecordingTimer;
static unsigned long long shortcutRecordingToken;
static int recordedShortcutCode = -1;
static unsigned int recordedShortcutModifiers;
extern void devjay_shortcut_recorded(unsigned long long token, int code, unsigned int modifiers);
unsigned int devjay_start_shortcuts(void);
static unsigned int shortcut_modifiers(NSEventModifierFlags flags) {
    return ((flags & NSEventModifierFlagControl) ? 1 : 0) |
           ((flags & NSEventModifierFlagOption) ? 2 : 0) |
           ((flags & NSEventModifierFlagShift) ? 4 : 0) |
           ((flags & NSEventModifierFlagCommand) ? 8 : 0);
}
static UInt32 shortcut_carbon_modifiers(unsigned int flags) {
    return ((flags & 1) ? controlKey : 0) | ((flags & 2) ? optionKey : 0) |
           ((flags & 4) ? shiftKey : 0) | ((flags & 8) ? cmdKey : 0);
}
static void unregister_shortcuts(void) {
    for (unsigned int i = 0; i < 13; i++) {
        if (shortcutRefs[i]) UnregisterEventHotKey(shortcutRefs[i]);
        shortcutRefs[i] = NULL;
    }
    shortcutsDown = 0;
    update_shortcut_hints(0);
}
static void finish_shortcut_recording(BOOL cancelled) {
    if (!shortcutRecordingWindow) return;
    unsigned long long token = shortcutRecordingToken;
    int code = cancelled ? -1 : recordedShortcutCode;
    unsigned int modifiers = recordedShortcutModifiers;
    if (shortcutRecordingMonitor) [NSEvent removeMonitor:shortcutRecordingMonitor];
    shortcutRecordingMonitor = nil;
    for (id observer in shortcutRecordingObservers) [NSNotificationCenter.defaultCenter removeObserver:observer];
    [shortcutRecordingObservers release]; shortcutRecordingObservers = nil;
    [shortcutRecordingTimer invalidate]; [shortcutRecordingTimer release]; shortcutRecordingTimer = nil;
    [shortcutRecordingWindow release]; shortcutRecordingWindow = nil;
    recordedShortcutCode = -1;
    devjay_start_shortcuts();
    devjay_shortcut_recorded(token, code, modifiers);
}
void devjay_cancel_shortcut_recording(void) {
    if ([NSThread isMainThread]) finish_shortcut_recording(YES);
}
static NSEvent *capture_shortcut_event(NSEvent *event) {
    if (!shortcutRecordingWindow) return event;
    if (!shortcutRecordingWindow.isKeyWindow || !shortcutRecordingWindow.visible ||
        (event.window && event.window != shortcutRecordingWindow) ||
        event.type == NSEventTypeLeftMouseDown || event.type == NSEventTypeRightMouseDown) {
        finish_shortcut_recording(YES); return event;
    }
    if (event.type == NSEventTypeKeyDown) {
        if (event.keyCode == kVK_Escape) { finish_shortcut_recording(YES); return nil; }
        if (!event.isARepeat && recordedShortcutCode < 0) {
            recordedShortcutCode = event.keyCode;
            recordedShortcutModifiers = shortcut_modifiers(event.modifierFlags);
        }
        return nil; // Includes menu key equivalents, such as Command-Q.
    }
    if (event.type == NSEventTypeKeyUp && recordedShortcutCode == event.keyCode) {
        // Re-register only after release so recording never fires the action.
        finish_shortcut_recording(NO);
    }
    return nil;
}
int devjay_record_shortcut(void *pointer, unsigned long long token) {
    if (![NSThread isMainThread] || !pointer) return 0;
    NSWindow *window = (NSWindow *)pointer;
    if (![window.identifier isEqual:@"settings"] || !window.visible || !window.isKeyWindow) return 0;
    finish_shortcut_recording(YES);
    shortcutRecordingWindow = [window retain]; shortcutRecordingToken = token;
    recordedShortcutCode = -1; recordedShortcutModifiers = 0;
    unregister_shortcuts();
    shortcutRecordingMonitor = [NSEvent addLocalMonitorForEventsMatchingMask:
        NSEventMaskKeyDown | NSEventMaskKeyUp | NSEventMaskLeftMouseDown | NSEventMaskRightMouseDown
        handler:^NSEvent *(NSEvent *event) { return capture_shortcut_event(event); }];
    shortcutRecordingObservers = [NSMutableArray new];
    for (NSString *name in @[NSWindowDidResignKeyNotification, NSWindowWillCloseNotification, NSApplicationDidResignActiveNotification]) {
        id object = [name isEqual:NSApplicationDidResignActiveNotification] ? (id)NSApp : window;
        id observer = [NSNotificationCenter.defaultCenter addObserverForName:name object:object queue:nil
            usingBlock:^(NSNotification *notification) { (void)notification; finish_shortcut_recording(YES); }];
        [shortcutRecordingObservers addObject:observer];
    }
    shortcutRecordingTimer = [[NSTimer scheduledTimerWithTimeInterval:30 repeats:NO block:^(NSTimer *timer) {
        (void)timer; finish_shortcut_recording(YES);
    }] retain];
    return 1;
}
unsigned int devjay_configure_shortcuts(const char *value) {
    if (![NSThread isMainThread] || !value) return 0x1fff;
    NSData *data = [[NSString stringWithUTF8String:value] dataUsingEncoding:NSUTF8StringEncoding];
    id bindings = data ? [NSJSONSerialization JSONObjectWithData:data options:0 error:nil] : nil;
    if (![bindings isKindOfClass:NSArray.class] || [bindings count] != 13) return 0x1fff;
    for (id binding in bindings) {
        if (![binding isKindOfClass:NSDictionary.class] || ![binding[@"keyCode"] isKindOfClass:NSNumber.class] ||
            ![binding[@"modifiers"] isKindOfClass:NSNumber.class] || ![binding[@"label"] isKindOfClass:NSString.class] ||
            [binding[@"keyCode"] intValue] < 0 || [binding[@"keyCode"] intValue] > 126 ||
            [binding[@"modifiers"] unsignedIntValue] > 15 || !([binding[@"modifiers"] unsignedIntValue] & 11) ||
            [binding[@"label"] length] == 0 || [binding[@"label"] length] > 8) return 0x1fff;
    }
    finish_shortcut_recording(YES);
    unregister_shortcuts();
    NSMutableArray *labels = [NSMutableArray array];
    for (unsigned int i = 0; i < 13; i++) {
        shortcutCodes[i] = [bindings[i][@"keyCode"] unsignedIntValue];
        shortcutModifiers[i] = [bindings[i][@"modifiers"] unsignedIntValue];
        [labels addObject:bindings[i][@"label"]];
    }
    [shortcutKeyLabels release]; shortcutKeyLabels = [labels copy];
    return devjay_start_shortcuts();
}


static OSStatus strip_shortcut(EventHandlerCallRef next, EventRef event, void *context) {
    (void)next; (void)context;
    EventHotKeyID key = {0};
    if (GetEventParameter(event, kEventParamDirectObject, typeEventHotKeyID,
        NULL, sizeof(key), NULL, &key) != noErr || key.signature != shortcutSignature ||
        key.id >= 13 || !shortcutRefs[key.id] || shortcutRecordingWindow) return eventNotHandledErr;
    unsigned int bit = 1u << key.id;
    if (GetEventKind(event) == kEventHotKeyReleased) {
        shortcutsDown &= ~bit;
    } else if (GetEventKind(event) == kEventHotKeyPressed) {
        if (!(shortcutsDown & bit)) devjay_strip_control(shortcutActions[key.id]);
        shortcutsDown |= bit;
    } else return eventNotHandledErr;
    return noErr;
}

unsigned int devjay_start_shortcuts(void) {
    if (![NSThread isMainThread]) return 0x1fff;
    if (shortcutRecordingWindow) return 0;
    const EventTypeSpec types[] = {{kEventClassKeyboard, kEventHotKeyPressed},
                                  {kEventClassKeyboard, kEventHotKeyReleased}};
    if (!shortcutHandler && InstallEventHandler(GetApplicationEventTarget(), strip_shortcut,
        2, types, NULL, &shortcutHandler) != noErr) return 0x1fff;
    unsigned int failed = 0;
    for (UInt32 i = 0; i < 13; i++) {
        if (shortcutRefs[i]) continue;
        EventHotKeyID key = {shortcutSignature, i};
        OSStatus status = RegisterEventHotKey(shortcutCodes[i], shortcut_carbon_modifiers(shortcutModifiers[i]),
            key, GetApplicationEventTarget(), kEventHotKeyExclusive, &shortcutRefs[i]);
        if (status != noErr) { shortcutRefs[i] = NULL; failed |= 1u << i; }
    }
    if (!shortcutHintTimer) {
        // Read only current modifier state. Unlike a global key event monitor,
        // this works without Accessibility/Input Monitoring permission and
        // never observes characters typed in another app. Notify only on change.
        shortcutHintTimer = [[NSTimer timerWithTimeInterval:0.1 repeats:YES block:^(NSTimer *timer) {
            (void)timer;
            update_shortcut_hints(NSEvent.modifierFlags);
        }] retain];
        shortcutHintTimer.tolerance = 0.02;
        [NSRunLoop.mainRunLoop addTimer:shortcutHintTimer forMode:NSRunLoopCommonModes];
    }
    return failed;
}

// Reduced views are separate panels. The full strip keeps its frame, style and
// layout observer, including when it is saved at shutdown or a display changes.
extern void devjay_compact_site(const char *siteID);
enum { DevJayStripFull, DevJayStripCompact, DevJayStripHidden };

@interface DevJayMinimalStrip : NSPanel
@end
@implementation DevJayMinimalStrip
- (BOOL)canBecomeKeyWindow { return NO; }
- (BOOL)canBecomeMainWindow { return NO; }
@end

@interface DevJayStripRestoreButton : NSButton
@property(nonatomic, copy) NSString *siteID;
@property(nonatomic) NSUInteger shortcutNumber;
@end
@implementation DevJayStripRestoreButton
- (BOOL)acceptsFirstMouse:(NSEvent *)event { (void)event; return YES; }
- (BOOL)acceptsFirstResponder { return NO; }
- (void)drawRect:(NSRect)dirtyRect {
    if (!self.shortcutNumber) { [super drawRect:dirtyRect]; return; }
    // Reserve room for the keycap so long names remain readable and truncated.
    NSMutableParagraphStyle *paragraph = [[[NSMutableParagraphStyle alloc] init] autorelease];
    paragraph.alignment = NSTextAlignmentCenter;
    paragraph.lineBreakMode = NSLineBreakByTruncatingMiddle;
    NSDictionary *titleStyle = @{NSFontAttributeName:self.font, NSForegroundColorAttributeName:self.contentTintColor,
        NSParagraphStyleAttributeName:paragraph};
    CGFloat height = [self.title sizeWithAttributes:titleStyle].height;
    [self.title drawInRect:NSMakeRect(64, (NSHeight(self.bounds) - height) / 2, NSWidth(self.bounds) - 72, height)
        withAttributes:titleStyle];
    NSRect keyFrame = NSMakeRect(8, (NSHeight(self.bounds) - 22) / 2, 48, 22);
    [[NSColor colorWithRed:0.788 green:0.867 blue:1 alpha:1] setFill];
    [[NSBezierPath bezierPathWithRoundedRect:keyFrame xRadius:4 yRadius:4] fill];
    NSString *number = shortcutKeyLabels ? shortcutKeyLabels[self.shortcutNumber] : [NSString stringWithFormat:@"%lu", (unsigned long)self.shortcutNumber];
    NSDictionary *numberStyle = @{NSFontAttributeName:[NSFont monospacedDigitSystemFontOfSize:12 weight:NSFontWeightBold],
        NSForegroundColorAttributeName:[NSColor colorWithRed:0.078 green:0.125 blue:0.220 alpha:1]};
    NSSize size = [number sizeWithAttributes:numberStyle];
    [number drawAtPoint:NSMakePoint(NSMidX(keyFrame) - size.width / 2, NSMidY(keyFrame) - size.height / 2)
        withAttributes:numberStyle];
}
- (void)dealloc { [_siteID release]; [super dealloc]; }
@end

static NSColor *strip_background(void) {
    return [NSColor colorWithRed:0.078 green:0.094 blue:0.125 alpha:1];
}
static DevJayMinimalStrip *strip_panel(NSSize size, NSString *title) {
    DevJayMinimalStrip *panel = [[DevJayMinimalStrip alloc] initWithContentRect:NSMakeRect(0, 0, size.width, size.height)
        styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel
        backing:NSBackingStoreBuffered defer:NO];
    panel.title = title;
    panel.releasedWhenClosed = NO;
    panel.hidesOnDeactivate = NO;
    panel.floatingPanel = YES;
    panel.becomesKeyOnlyIfNeeded = YES;
    panel.opaque = NO;
    panel.backgroundColor = NSColor.clearColor;
    panel.hasShadow = YES;
    panel.excludedFromWindowsMenu = YES;
    panel.restorable = NO;
    panel.tabbingMode = NSWindowTabbingModeDisallowed;
    panel.animationBehavior = NSWindowAnimationBehaviorNone;
    return panel;
}
static void style_strip_button(NSButton *button, BOOL active) {
    button.bordered = NO;
    button.wantsLayer = YES;
    button.layer.cornerRadius = 7;
    button.layer.borderWidth = active ? 2 : 1;
    button.layer.backgroundColor = (active
        ? [NSColor colorWithRed:0.149 green:0.208 blue:0.294 alpha:1]
        : [NSColor colorWithRed:0.114 green:0.141 blue:0.192 alpha:1]).CGColor;
    button.layer.borderColor = (active
        ? [NSColor colorWithRed:0.541 green:0.710 blue:1 alpha:1]
        : [NSColor colorWithRed:0.235 green:0.282 blue:0.361 alpha:1]).CGColor;
    button.contentTintColor = [NSColor colorWithRed:0.93 green:0.95 blue:0.98 alpha:1];
    button.font = [NSFont systemFontOfSize:16 weight:NSFontWeightSemibold];
    button.cell.lineBreakMode = NSLineBreakByTruncatingMiddle;
}

@interface DevJayStripControls : NSObject {
@public
    NSWindow *strip;
    DevJayMinimalStrip *compact;
    NSScrollView *scroll;
    int mode;
}
- (void)activateSite:(DevJayStripRestoreButton *)sender;
- (void)selectControl:(NSMenuItem *)sender;
- (NSMenu *)menuForCompact:(BOOL)isCompact;
- (BOOL)placeControls;
- (BOOL)updateCards:(const char *)json;
- (void)updateHints;
- (void)showReducedPanel;
- (void)placementChanged:(NSNotification *)note;
@end

@implementation DevJayStripControls
- (instancetype)init {
    self = [super init];
    if (self) {
        compact = strip_panel(NSMakeSize(900, 40), @"DevJay · Minimized strip");
        compact.identifier = @"compact-strip";
        scroll = [[[NSScrollView alloc] initWithFrame:NSMakeRect(0, 0, 900, 40)] autorelease];
        scroll.borderType = NSNoBorder;
        scroll.drawsBackground = YES;
        scroll.backgroundColor = strip_background();
        scroll.hasHorizontalScroller = YES;
        scroll.hasVerticalScroller = NO;
        scroll.autohidesScrollers = YES;
        scroll.scrollerStyle = NSScrollerStyleOverlay;
        scroll.verticalScrollElasticity = NSScrollElasticityNone;
        scroll.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
        scroll.wantsLayer = YES;
        scroll.layer.cornerRadius = 8;
        scroll.menu = [self menuForCompact:YES];
        NSView *row = [[[NSView alloc] initWithFrame:scroll.bounds] autorelease];
        row.menu = scroll.menu;
        scroll.documentView = row;
        compact.contentView = scroll;
        for (NSString *name in @[NSApplicationDidChangeScreenParametersNotification,
            NSWindowDidMoveNotification, NSWindowDidResizeNotification]) {
            [NSNotificationCenter.defaultCenter addObserver:self selector:@selector(placementChanged:) name:name object:nil];
        }
    }
    return self;
}
- (NSMenu *)menuForCompact:(BOOL)isCompact {
    NSMenu *menu = [[[NSMenu alloc] initWithTitle:@""] autorelease];
    NSArray *titles = @[@"Restore strip", isCompact ? @"Hide strip" : @"Minimize strip", siteWindowsHidden ? @"Show site windows" : @"Hide site windows", @"DevJay settings…", @"Quit DevJay"];
    const NSInteger actions[] = {10, isCompact ? 12 : 13, siteWindowsHidden ? 19 : 18, 11, 14};
    for (NSUInteger i = 0; i < titles.count; i++) {
        NSMenuItem *item = [menu addItemWithTitle:titles[i] action:@selector(selectControl:) keyEquivalent:@""];
        item.target = self; item.tag = actions[i];
    }
    return menu;
}
- (void)selectControl:(NSMenuItem *)sender { devjay_strip_control((int)sender.tag); }
- (void)activateSite:(DevJayStripRestoreButton *)sender {
    if (sender.siteID.length) devjay_compact_site(sender.siteID.UTF8String);
    else devjay_strip_control(11); // Empty strip opens Settings.
}
- (BOOL)updateCards:(const char *)json {
    if (!json) return NO;
    NSString *text = [NSString stringWithUTF8String:json];
    id cards = text ? [NSJSONSerialization JSONObjectWithData:[text dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil] : nil;
    if (![cards isKindOfClass:NSArray.class]) return NO;
    for (id card in cards) {
        if (![card isKindOfClass:NSDictionary.class] || ![card[@"id"] isKindOfClass:NSString.class] ||
            ![card[@"name"] isKindOfClass:NSString.class] || ![card[@"path"] isKindOfClass:NSString.class] ||
            ![card[@"active"] isKindOfClass:NSNumber.class]) return NO;
    }
    NSPoint offset = scroll.contentView.bounds.origin;
    NSView *row = scroll.documentView;
    for (NSView *child in [[row.subviews copy] autorelease]) [child removeFromSuperview];
    const CGFloat cardWidth = 215, gap = 8, inset = 5;
    CGFloat width = MAX(scroll.contentSize.width, MAX((NSUInteger)1, [cards count]) * (cardWidth + gap) - gap + inset * 2);
    row.frame = NSMakeRect(0, 0, width, 40);
    NSUInteger count = MAX((NSUInteger)1, [cards count]);
    for (NSUInteger i = 0; i < count; i++) {
        NSDictionary *card = [cards count] ? cards[i] : nil;
        BOOL active = [card[@"active"] boolValue];
        DevJayStripRestoreButton *button = [DevJayStripRestoreButton buttonWithTitle:card[@"name"] ?: @"Add a Site in Settings"
            target:self action:@selector(activateSite:)];
        button.siteID = card[@"id"];
        button.shortcutNumber = card && i < 9 && (shortcutHints & (1u << i)) ? i + 1 : 0;
        button.frame = NSMakeRect(inset + i * (cardWidth + gap), 5, cardWidth, 30);
        style_strip_button(button, active);
        button.menu = scroll.menu;
        button.toolTip = card ? [NSString stringWithFormat:@"%@%@%@", card[@"path"], active ? @" · Current desktop" : @"",
            i < 9 ? [NSString stringWithFormat:@" · ⌃⌥⌘%lu", (unsigned long)i + 1] : @""] : @"Open DevJay settings";
        button.accessibilityLabel = card ? [NSString stringWithFormat:@"Go to %@ desktop%@", card[@"name"], active ? @", current desktop" : @""] : @"Add a Site in Settings";
        [row addSubview:button];
    }
    offset.x = MAX(0, MIN(offset.x, width - scroll.contentSize.width));
    offset.y = 0;
    [scroll.contentView scrollToPoint:offset];
    [scroll reflectScrolledClipView:scroll.contentView];
    return YES;
}
- (void)updateHints {
    NSUInteger index = 0;
    for (DevJayStripRestoreButton *button in scroll.documentView.subviews) {
        button.shortcutNumber = button.siteID.length && index < 9 && (shortcutHints & (1u << index)) ? index + 1 : 0;
        button.needsDisplay = YES;
        index++;
    }
}
- (void)showReducedPanel {
    if (mode == DevJayStripCompact) [compact orderFrontRegardless];
    else [compact orderOut:nil]; // Hidden means no badge or modifier preview.
}
- (BOOL)placeControls {
    if (!strip) return NO;
    NSScreen *screen = strip.screen;
    // A hidden window may have no screen; locate its saved frame first.
    if (!screen) for (NSScreen *candidate in NSScreen.screens) {
        if (NSPointInRect(NSMakePoint(NSMidX(strip.frame), NSMidY(strip.frame)), candidate.frame)) { screen = candidate; break; }
    }
    screen = screen ?: NSScreen.mainScreen;
    if (!screen) return NO;
    NSRect area = screen.visibleFrame;
    for (NSPanel *panel in @[compact]) {
        NSSize size = NSMakeSize(MIN(NSWidth(strip.frame), NSWidth(area)), 40);
        // AppKit coordinates start at the bottom: the compact row keeps the
        // full strip's lower-left anchor, with an on-screen clamp after unplugging.
        NSRect frame = NSMakeRect(NSMinX(strip.frame), NSMinY(strip.frame), size.width, size.height);
        frame.origin.x = MAX(NSMinX(area), MIN(frame.origin.x, NSMaxX(area) - frame.size.width));
        frame.origin.y = MAX(NSMinY(area), MIN(frame.origin.y, NSMaxY(area) - frame.size.height));
        [panel setFrame:frame display:NO];
        panel.level = strip.level;
        panel.collectionBehavior = strip.collectionBehavior;
    }
    return YES;
}
- (void)placementChanged:(NSNotification *)note {
    if (mode == DevJayStripFull || (note.object != strip && ![note.name isEqual:NSApplicationDidChangeScreenParametersNotification])) return;
    // Let the full strip's layout observer finish reanchoring first.
    [NSObject cancelPreviousPerformRequestsWithTarget:self selector:@selector(placeControls) object:nil];
    [self performSelector:@selector(placeControls) withObject:nil afterDelay:0];
}
- (void)dealloc {
    [NSObject cancelPreviousPerformRequestsWithTarget:self];
    [NSNotificationCenter.defaultCenter removeObserver:self];
    [compact orderOut:nil]; [compact release];
    [strip release];
    [super dealloc];
}
@end

static DevJayStripControls *stripControls;

void devjay_set_site_windows_hidden(int hidden) {
    if (![NSThread isMainThread]) return;
    siteWindowsHidden = hidden != 0;
    if (!stripControls) return;
    NSMenu *menu = stripControls->scroll.menu;
    NSMenuItem *item = [menu itemWithTag:18] ?: [menu itemWithTag:19];
    item.title = siteWindowsHidden ? @"Show site windows" : @"Hide site windows";
    item.tag = siteWindowsHidden ? 19 : 18;
}


static void update_shortcut_hints(NSEventModifierFlags flags) {
    unsigned int mask = 0, modifiers = shortcut_modifiers(flags);
    if (!shortcutRecordingWindow && modifiers) {
        for (unsigned int i = 1; i <= 9; i++) {
            if (shortcutRefs[i] && shortcutModifiers[i] == modifiers) mask |= 1u << (i - 1);
        }
    }
    if (shortcutHints == mask) return;
    shortcutHints = mask;
    [stripControls updateHints];
    devjay_shortcut_hints(mask);
}

int devjay_set_strip_mode(void *pointer, int mode, const char *cards) {
    if (![NSThread isMainThread] || !pointer || mode < DevJayStripFull || mode > DevJayStripHidden) return 0;
    NSWindow *window = (NSWindow *)pointer;
    if (![window.identifier isEqual:@"strip"]) return 0;
    if (mode != DevJayStripFull) {
        if (!stripControls) stripControls = [DevJayStripControls new];
        if (stripControls->strip != window) {
            [stripControls->strip release];
            stripControls->strip = [window retain];
        }
        if (mode == DevJayStripCompact && (![stripControls placeControls] || ![stripControls updateCards:cards])) return 0;
        stripControls->mode = mode;
        [window orderOut:nil];
        [stripControls showReducedPanel];
    } else {
        if (stripControls) {
            stripControls->mode = mode;
            [stripControls->compact orderOut:nil];
        }
        if (window.isMiniaturized) [window deminiaturize:nil];
        [window orderFrontRegardless];
    }
    return 1;
}

void devjay_update_compact_strip(const char *cards) {
    if ([NSThread isMainThread] && stripControls && stripControls->mode == DevJayStripCompact) [stripControls updateCards:cards];
}

void devjay_refresh_minimal_strip(void) {
    if ([NSThread isMainThread] && stripControls && stripControls->mode == DevJayStripCompact) [stripControls placeControls];
}

void devjay_stop_strip_controls(void) {
    if (![NSThread isMainThread]) return;
    finish_shortcut_recording(YES);
    [shortcutHintTimer invalidate]; [shortcutHintTimer release]; shortcutHintTimer = nil;
    update_shortcut_hints(0);
    unregister_shortcuts();
    [shortcutKeyLabels release]; shortcutKeyLabels = nil;
    if (shortcutHandler) RemoveEventHandler(shortcutHandler);
    shortcutHandler = NULL;
    shortcutsDown = 0;
    [stripControls release]; stripControls = nil;
}
