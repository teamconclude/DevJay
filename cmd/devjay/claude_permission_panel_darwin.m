#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <QuartzCore/QuartzCore.h>

extern void goDevJayClaudePermissionDecision(char *, char *);
extern void devjay_set_permission_hover(BOOL (*show)(NSWindow *, NSRect, NSString *, NSString *), void (*close)(NSWindow *));

static NSDictionary *permissionViews;
static BOOL permissionHoverKeysFailed;
static NSDictionary *permission_find(NSString *token) {
    for (NSArray *requests in permissionViews.allValues)
        for (NSDictionary *request in requests)
            if ([request[@"token"] isEqual:token]) return request;
    return nil;
}

static BOOL permission_can_answer(NSString *token) {
    NSDictionary *request = permission_find(token);
    return request && ![request[@"unavailable"] boolValue];
}
static NSString *permission_hint(NSString *token) {
    if (permissionHoverKeysFailed) return permission_can_answer(token) ? @"Hover shortcuts unavailable. Click the panel to use Y, N, C or Esc." : @"Hover shortcuts unavailable. Click the panel to use C or Esc.";
    return permission_can_answer(token) ? @"While hovering: Y = Yes, N = No, C = Claude. Esc dismisses." : @"While hovering: C goes to Claude. Esc dismisses without answering.";
}

// Even after an explicit click or Tab gives a response button focus, Space
// and Return must not invoke AppKit's implicit button action. Responses use
// the advertised Y/N/C handler or an explicit mouse/accessibility click.
@interface DevJayPermissionButton : NSButton
@end
@implementation DevJayPermissionButton
- (void)keyDown:(NSEvent *)event {
    if ([@[@" ",@"\r",@"\n"] containsObject:event.charactersIgnoringModifiers]) return;
    [super keyDown:event];
}
@end

// A mnemonic is part of the label, not an AppKit key equivalent: the panel's
// shared shortcut handler owns Y, N, C and Escape.
static NSButton *permission_shortcut_button(NSString *title, id target, SEL action) {
    NSButton *button = [DevJayPermissionButton buttonWithTitle:title target:target action:action];
    NSMutableParagraphStyle *paragraph = [[[NSMutableParagraphStyle alloc] init] autorelease];
    paragraph.alignment = NSTextAlignmentCenter;
    NSMutableAttributedString *label = [[[NSMutableAttributedString alloc] initWithString:title attributes:@{
        NSFontAttributeName:button.font, NSForegroundColorAttributeName:NSColor.labelColor,
        NSParagraphStyleAttributeName:paragraph}] autorelease];
    NSColor *shortcut = [NSColor colorWithName:@"DevJayPromptShortcut" dynamicProvider:^NSColor *(NSAppearance *appearance) {
        BOOL dark = [[appearance bestMatchFromAppearancesWithNames:@[NSAppearanceNameAqua,NSAppearanceNameDarkAqua]] isEqual:NSAppearanceNameDarkAqua];
        return dark ? NSColor.systemYellowColor : [NSColor colorWithSRGBRed:.48 green:.32 blue:0 alpha:1];
    }];
    [label addAttributes:@{NSForegroundColorAttributeName:shortcut,
        NSFontAttributeName:[NSFont boldSystemFontOfSize:button.font.pointSize],
        NSUnderlineStyleAttributeName:@(NSUnderlineStyleSingle)} range:NSMakeRange(0,1)];
    button.attributedTitle = label;
    return button;
}

// Render only the structured, literal tool input supplied by the bridge.
// Formatting never changes the request that the Yes/No action answers.
static NSAttributedString *permission_details(NSDictionary *request, BOOL raw) {
    NSMutableAttributedString *out = [[[NSMutableAttributedString alloc] init] autorelease];
    NSFont *code = [NSFont monospacedSystemFontOfSize:11 weight:NSFontWeightRegular];
    NSFont *body = [NSFont systemFontOfSize:12];
    NSFont *label = [NSFont boldSystemFontOfSize:11];
    void (^append)(NSString *,NSFont *,NSColor *) = ^(NSString *text,NSFont *font,NSColor *color) {
        [out appendAttributedString:[[[NSAttributedString alloc] initWithString:text ?: @""
            attributes:@{NSFontAttributeName:font,NSForegroundColorAttributeName:color}] autorelease]];
    };
    NSDictionary *preview = request[@"preview"];
    if (raw || ![preview isKindOfClass:NSDictionary.class]) {
        append(request[@"details"], [request[@"unavailable"] boolValue] ? body : code, NSColor.labelColor);
        return out;
    }
    NSString *description = preview[@"description"];
    if (description.length) { append(description,body,NSColor.secondaryLabelColor);append(@"\n\n",body,NSColor.labelColor); }
    for (NSDictionary *section in preview[@"sections"]) {
        NSString *kind = section[@"kind"], *text = section[@"text"];
        NSColor *color = [kind isEqual:@"removed"] ? NSColor.systemRedColor : [kind isEqual:@"added"] ? NSColor.systemGreenColor :
            [kind isEqual:@"warning"] ? NSColor.systemOrangeColor : NSColor.labelColor;
        append(section[@"label"],label,NSColor.secondaryLabelColor);append(@"\n",body,NSColor.labelColor);
        BOOL prose = [kind isEqual:@"note"] || [kind isEqual:@"warning"];
        append(text.length ? text : @"(empty text)",prose || !text.length ? body : code,color);
        append(@"\n\n",body,NSColor.labelColor);
    }
    return out;
}

@interface DevJayPermissionPanel : NSPanel {
@public
    NSString *token;
    NSString *hostKey;
    NSWindow *origin; // No child relationship: never moves another Space.
    NSRect anchor;
    NSButton *yes;
    NSButton *no;
    NSTextField *hint;
    NSTextView *details;
    NSScrollView *detailsScroll;
    NSButton *detailsToggle;
    BOOL showingRaw;
    NSTimer *hoverTimer;
    NSTimer *feedbackTimer;
    NSButton *feedbackButton;
    NSTimeInterval outsideSince;
    BOOL sent;
}
- (void)closePopover;
- (void)chooseYes:(id)sender;
- (void)chooseNo:(id)sender;
- (void)goToClaude:(id)sender;
- (void)promptSettings:(id)sender;
- (void)toggleDetails:(id)sender;
- (void)tick:(NSTimer *)timer;
- (void)finishFeedback:(NSTimer *)timer;
- (void)handlePermissionKey:(NSString *)key;
@end

static DevJayPermissionPanel *permissionPanel;
static void permission_sync_hover_keys(void);
static void permission_stop_hover_keys(void);
static void permission_start_hover_keys(void);

static BOOL permission_is_shortcut(NSEvent *event) {
    return !(event.modifierFlags & (NSEventModifierFlagCommand | NSEventModifierFlagControl | NSEventModifierFlagOption)) &&
        [@[@"y",@"n",@"c",@"\033"] containsObject:event.charactersIgnoringModifiers.lowercaseString];
}


@implementation DevJayPermissionPanel
- (BOOL)canBecomeKeyWindow { return YES; }
- (BOOL)canBecomeMainWindow { return NO; }
- (BOOL)needsPanelToBecomeKey { return YES; }
- (void)dealloc { [feedbackTimer invalidate]; [hoverTimer invalidate]; [token release]; [hostKey release]; [super dealloc]; }
- (void)closePopover {
    if (self == permissionPanel) permission_stop_hover_keys();
    [hoverTimer invalidate]; hoverTimer = nil;
    [feedbackTimer invalidate]; feedbackTimer = nil;
    [feedbackButton.layer removeAnimationForKey:@"permission-response"];
    feedbackButton.layer.borderWidth = 0;
    [feedbackButton highlight:NO]; feedbackButton = nil;
    sent = YES;
    [self orderOut:nil];
    [token release]; token = nil;
    [hostKey release]; hostKey = nil;
    origin = nil;
}
- (void)submit:(NSString *)decision {
    if (sent) return;
    if (!token || !permission_find(token)) { [self closePopover]; return; }
    if (([decision isEqual:@"allow"] || [decision isEqual:@"deny"]) && !permission_can_answer(token)) return;
    sent = YES;
    // Invalidate the local controls immediately, before the queue handles this.
    yes.enabled = no.enabled = NO;
    if (self == permissionPanel) permission_stop_hover_keys();
    // The response leaves now; only the visual dismissal waits. Release a
    // deliberately focused panel as well, so further typing returns to its app.
    goDevJayClaudePermissionDecision((char *)token.UTF8String, (char *)decision.UTF8String);
    if (self.keyWindow) [self resignKeyWindow];
    if (![decision isEqual:@"allow"] && ![decision isEqual:@"deny"]) { [self closePopover]; return; }
    feedbackButton = [decision isEqual:@"allow"] ? yes : no;
    hint.stringValue = [decision isEqual:@"allow"] ? @"Yes — sent to Claude." : @"No — sent to Claude.";
    detailsToggle.enabled = NO;
    feedbackButton.wantsLayer = YES;
    feedbackButton.layer.cornerRadius = 6;
    feedbackButton.layer.borderWidth = 2;
    feedbackButton.layer.borderColor = NSColor.controlAccentColor.CGColor;
    [feedbackButton highlight:YES];
    CABasicAnimation *pulse = [CABasicAnimation animationWithKeyPath:@"opacity"];
    pulse.fromValue = @1; pulse.toValue = @.45;
    pulse.duration = .25; pulse.autoreverses = YES; pulse.repeatCount = 2;
    [feedbackButton.layer addAnimation:pulse forKey:@"permission-response"];
    feedbackTimer = [NSTimer timerWithTimeInterval:1 target:self selector:@selector(finishFeedback:) userInfo:nil repeats:NO];
    [NSRunLoop.mainRunLoop addTimer:feedbackTimer forMode:NSRunLoopCommonModes];
}
- (void)finishFeedback:(NSTimer *)timer {
    // An old timer cannot dismiss a different request after an early close.
    if (timer == feedbackTimer) [self closePopover];
}
- (void)chooseYes:(id)sender { [self submit:@"allow"]; }
- (void)chooseNo:(id)sender { [self submit:@"deny"]; }
- (void)goToClaude:(id)sender { [self submit:@"open"]; }
- (void)promptSettings:(id)sender { [self submit:@"settings"]; }
- (void)toggleDetails:(id)sender {
    NSDictionary *request = permission_find(token);
    if (sent || !request || ![request[@"preview"] isKindOfClass:NSDictionary.class]) return;
    showingRaw = !showingRaw;
    [details.textStorage setAttributedString:permission_details(request,showingRaw)];
    detailsToggle.title = showingRaw ? @"Show preview" : @"Show JSON";
    [details scrollPoint:NSZeroPoint];
    [detailsScroll reflectScrolledClipView:detailsScroll.contentView];
}
- (void)keyDown:(NSEvent *)event {
    if (sent || !self.keyWindow || event.isARepeat ||
        (event.modifierFlags & (NSEventModifierFlagCommand | NSEventModifierFlagControl | NSEventModifierFlagOption))) {
        [super keyDown:event]; return;
    }
    NSString *key = event.charactersIgnoringModifiers.lowercaseString;
    if (permission_is_shortcut(event)) [self handlePermissionKey:key];
    else [super keyDown:event];
}
- (void)handlePermissionKey:(NSString *)key {
    if (sent) return;
    if ([key isEqual:@"y"]) {
        [self chooseYes:nil];
    } else if ([key isEqual:@"c"]) {
        [self goToClaude:nil];
    } else if ([key isEqual:@"n"]) {
        [self chooseNo:nil];
    } else if ([key isEqual:@"\033"]) {
        [self closePopover];
    }
}
- (void)tick:(NSTimer *)timer {
    if (!self.visible || (!feedbackTimer && !permission_find(token)) || !origin.visible || !origin.isOnActiveSpace || origin.miniaturized) {
        [self closePopover]; return;
    }
    if (feedbackTimer) return; // Keep the one-second response feedback across pointer exit.
    permission_sync_hover_keys();
    NSPoint point = NSEvent.mouseLocation;
    // A short grace period bridges the gap from the icon into the panel.
    if (NSPointInRect(point, NSInsetRect(self.frame,-5,-5)) || NSPointInRect(point,NSInsetRect(anchor,-5,-5)) || self.keyWindow) {
        outsideSince = 0; return;
    }
    NSTimeInterval now = NSProcessInfo.processInfo.systemUptime;
    if (!outsideSince) outsideSince = now;
    if (now - outsideSince > .45) [self closePopover];
}
@end

// Register only Y, N, C and Escape, only while this exact prompt is
// hovered. Carbon delivers these shortcuts without changing the active app or
// installing a keyboard event tap. No unrelated typing is observed.
static const OSType permissionShortcutSignature = 'DjCp';
static EventHotKeyRef permissionShortcutRefs[8]; // Plain and Shift variants.
static EventHandlerRef permissionShortcutHandler;
static UInt32 permissionShortcutCodes[4];
static UInt32 permissionShortcutGeneration;
static unsigned int permissionShortcutsDown;
static BOOL permissionHoverKeysArmed;
static id permissionLocalMouseMonitor, permissionGlobalMouseMonitor;

static BOOL permission_pointer_eligible(DevJayPermissionPanel *panel, NSPoint point) {
    return panel && panel.visible && !panel->sent && permission_find(panel->token) &&
        panel->origin.visible && panel->origin.isOnActiveSpace && !panel->origin.miniaturized &&
        (NSPointInRect(point,panel.frame) || NSPointInRect(point,panel->anchor));
}

static void permission_unregister_hover_keys(void) {
    for (unsigned int i=0;i<8;i++) {
        if (permissionShortcutRefs[i]) UnregisterEventHotKey(permissionShortcutRefs[i]);
        permissionShortcutRefs[i]=NULL;
    }
    permissionHoverKeysArmed=NO;
    permissionShortcutsDown=0;
}

// Match the displayed mnemonic to the current keyboard layout, rather than
// assuming that a physical US-layout key always produces Y, N or C.
static BOOL permission_shortcut_codes(UInt32 codes[4]) {
    codes[0]=codes[1]=codes[2]=UINT32_MAX;
    codes[3]=kVK_Escape;
    TISInputSourceRef source=TISCopyCurrentKeyboardLayoutInputSource();
    CFDataRef data=source ? TISGetInputSourceProperty(source,kTISPropertyUnicodeKeyLayoutData) : NULL;
    if (data) for (UInt16 code=0;code<128;code++) {
        UInt32 dead=0;UniChar chars[4];UniCharCount count=0;
        if (UCKeyTranslate((const UCKeyboardLayout *)CFDataGetBytePtr(data),code,kUCKeyActionDisplay,0,
            LMGetKbdType(),kUCKeyTranslateNoDeadKeysBit,&dead,4,&count,chars)!=noErr || count!=1) continue;
        NSString *value=[[[NSString alloc] initWithCharacters:chars length:count] autorelease].lowercaseString;
        NSUInteger index=[@[@"y",@"n",@"c"] indexOfObject:value];
        if (index!=NSNotFound && codes[index]==UINT32_MAX) codes[index]=code;
    }
    if(source)CFRelease(source);
    return codes[0]!=UINT32_MAX && codes[1]!=UINT32_MAX && codes[2]!=UINT32_MAX;
}

static OSStatus permission_hover_shortcut(EventHandlerCallRef next, EventRef event, void *context) {
    (void)next;(void)context;
    EventHotKeyID hotkey={0};
    if (GetEventParameter(event,kEventParamDirectObject,typeEventHotKeyID,NULL,sizeof(hotkey),NULL,&hotkey)!=noErr ||
        hotkey.signature!=permissionShortcutSignature) return eventNotHandledErr;
    unsigned int slot=hotkey.id&15, key=slot%4;
    if (slot>=8 || (hotkey.id>>4)!=permissionShortcutGeneration || !permissionShortcutRefs[slot]) return eventNotHandledErr;
    unsigned int bit=1u<<key;
    if (GetEventKind(event)==kEventHotKeyReleased) {
        permissionShortcutsDown&=~bit;return noErr;
    }
    if (GetEventKind(event)!=kEventHotKeyPressed) return eventNotHandledErr;
    BOOL repeated=(permissionShortcutsDown&bit)!=0;
    permissionShortcutsDown|=bit;
    // Recheck at delivery, not just at registration: a queued key may outlive
    // the pointer region, the request, or the original Site window.
    if (!permission_pointer_eligible(permissionPanel,NSEvent.mouseLocation)) {
        permission_sync_hover_keys();return noErr;
    }
    if (repeated || (NSEvent.modifierFlags&(NSEventModifierFlagCommand|NSEventModifierFlagControl|NSEventModifierFlagOption))) return noErr;
    [permissionPanel handlePermissionKey:@[@"y",@"n",@"c",@"\033"][key]];
    return noErr;
}

static void permission_sync_hover_keys(void) {
    if (!permission_pointer_eligible(permissionPanel,NSEvent.mouseLocation)) {
        permission_unregister_hover_keys();
        permissionHoverKeysFailed=NO;
        return;
    }
    if (permissionHoverKeysArmed) return;
    const EventTypeSpec types[]={{kEventClassKeyboard,kEventHotKeyPressed},{kEventClassKeyboard,kEventHotKeyReleased}};
    BOOL ready=permission_shortcut_codes(permissionShortcutCodes);
    if (ready && !permissionShortcutHandler) ready=InstallEventHandler(GetApplicationEventTarget(),permission_hover_shortcut,2,types,NULL,&permissionShortcutHandler)==noErr;
    permissionShortcutGeneration=(permissionShortcutGeneration+1)&0x0fffffff;
    // A key already held when the popup appears cannot answer it via repeat.
    for (unsigned int i=0;i<4;i++) if (ready && CGEventSourceKeyState(kCGEventSourceStateCombinedSessionState,(CGKeyCode)permissionShortcutCodes[i])) permissionShortcutsDown|=1u<<i;
    BOOL answerable=permission_can_answer(permissionPanel->token);
    for (UInt32 i=0;ready && i<8;i++) {
        if (!answerable && i%4!=2 && i%4!=3) continue;
        EventHotKeyID hotkey={permissionShortcutSignature,(permissionShortcutGeneration<<4)|i};
        ready=RegisterEventHotKey(permissionShortcutCodes[i%4],i<4 ? 0 : shiftKey,hotkey,GetApplicationEventTarget(),
            kEventHotKeyExclusive,&permissionShortcutRefs[i])==noErr;
    }
    if (!ready) permission_unregister_hover_keys(); // Never leave a partial set.
    permissionHoverKeysArmed=YES; // Do not retry a conflict until hover reentry.
    permissionHoverKeysFailed=!ready;
    permissionPanel->hint.stringValue=permission_hint(permissionPanel->token);
}

static void permission_stop_hover_keys(void) {
    permission_unregister_hover_keys();
    permissionHoverKeysFailed=NO;
    if(permissionLocalMouseMonitor)[NSEvent removeMonitor:permissionLocalMouseMonitor];
    if(permissionGlobalMouseMonitor)[NSEvent removeMonitor:permissionGlobalMouseMonitor];
    permissionLocalMouseMonitor=permissionGlobalMouseMonitor=nil;
}
static void permission_start_hover_keys(void) {
    permission_stop_hover_keys();
    NSEventMask moves=NSEventMaskMouseMoved|NSEventMaskLeftMouseDragged|NSEventMaskRightMouseDragged|NSEventMaskOtherMouseDragged;
    permissionLocalMouseMonitor=[NSEvent addLocalMonitorForEventsMatchingMask:moves handler:^NSEvent *(NSEvent *event){
        permission_sync_hover_keys();return event;
    }];
    permissionGlobalMouseMonitor=[NSEvent addGlobalMonitorForEventsMatchingMask:moves|NSEventMaskLeftMouseDown|NSEventMaskRightMouseDown handler:^(NSEvent *event){
        if(event.type==NSEventTypeLeftMouseDown || event.type==NSEventTypeRightMouseDown)[permissionPanel closePopover];
        else permission_sync_hover_keys();
    }];
    permission_sync_hover_keys();
}

@interface DevJayPermissionContent : NSView
@end
@implementation DevJayPermissionContent
- (BOOL)acceptsFirstMouse:(NSEvent *)event { return YES; }
- (void)mouseDown:(NSEvent *)event {
    [self.window makeKeyWindow];
    [self.window makeFirstResponder:self.window];
}
@end

static NSString *permissionSuppressedHost;
static NSRect permissionSuppressedAnchor;
static BOOL permission_show(NSWindow *parent, NSRect anchor, NSString *site, NSString *bundle) {
    if (!site.length || !bundle.length) {
        if (!NSPointInRect(NSEvent.mouseLocation,permissionSuppressedAnchor)) { [permissionSuppressedHost release]; permissionSuppressedHost=nil; }
        return NO;
    }
    if (permissionPanel.visible && permissionPanel->feedbackTimer) return YES;
    NSString *key = [NSString stringWithFormat:@"%@\n%@",site,bundle];
    NSArray *requests = permissionViews[key];
    if (!requests.count) return NO;
    if (permissionPanel.visible && [permissionPanel->hostKey isEqual:key]) return YES;
    // Answering cannot reveal a new request beneath a resting pointer. Leave
    // the icon and enter it again to see the next invocation.
    if ([permissionSuppressedHost isEqual:key] && NSPointInRect(NSEvent.mouseLocation,permissionSuppressedAnchor)) return YES;
    [permissionSuppressedHost release]; permissionSuppressedHost = nil;
    if (!parent.visible || !parent.isOnActiveSpace || parent.miniaturized) return NO;
    NSScreen *screen = parent.screen;
    for (NSScreen *candidate in NSScreen.screens) if (NSPointInRect(NSMakePoint(NSMidX(anchor),NSMidY(anchor)),candidate.frame)) {screen=candidate;break;}
    if (!screen || screen.visibleFrame.size.width<200 || screen.visibleFrame.size.height<200) return NO;
    [permissionPanel closePopover];
    if (!permissionPanel) {
        permissionPanel = [[DevJayPermissionPanel alloc] initWithContentRect:NSZeroRect
            styleMask:NSWindowStyleMaskBorderless | NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];
        permissionPanel.identifier = @"claude-permission-popover";
        permissionPanel.releasedWhenClosed = NO;
        permissionPanel.hidesOnDeactivate = NO;
        permissionPanel.excludedFromWindowsMenu = YES;
        permissionPanel.restorable = NO;
        permissionPanel.tabbingMode = NSWindowTabbingModeDisallowed;
        permissionPanel.animationBehavior = NSWindowAnimationBehaviorNone;
        permissionPanel.level = NSFloatingWindowLevel + 2;
        permissionPanel.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces | NSWindowCollectionBehaviorStationary |
            NSWindowCollectionBehaviorIgnoresCycle | NSWindowCollectionBehaviorFullScreenNone;
        permissionPanel.opaque = NO;
        permissionPanel.backgroundColor = NSColor.clearColor;
        permissionPanel.hasShadow = YES;
    }
    NSDictionary *request = requests.firstObject;
    permissionPanel->token = [request[@"token"] copy];
    permissionPanel->hostKey = [key copy];
    permissionPanel->origin = parent;
    permissionPanel->anchor = anchor;
    permissionPanel->sent = NO;
    permissionPanel->outsideSince = 0;
    permissionPanel->showingRaw = NO;
    permissionSuppressedHost = [key copy]; permissionSuppressedAnchor = anchor;
    NSRect safe = NSInsetRect(screen.visibleFrame,8,8);
    BOOL unavailable = [request[@"unavailable"] boolValue];
    CGFloat width = MIN(unavailable ? 460 : 620,safe.size.width);
    NSAttributedString *initialDetails = permission_details(request,NO);
    CGFloat textHeight = ceil([initialDetails boundingRectWithSize:NSMakeSize(width-52,CGFLOAT_MAX)
        options:NSStringDrawingUsesLineFragmentOrigin | NSStringDrawingUsesFontLeading].size.height)+20;
    CGFloat height = MIN(unavailable ? 300 : MAX(300,MIN(440,textHeight+190)),safe.size.height);
    CGFloat x = MAX(NSMinX(safe),MIN(NSMinX(anchor),NSMaxX(safe)-width));
    CGFloat y = NSMaxY(anchor)+6;
    if(y+height>NSMaxY(safe))y=NSMinY(anchor)-height-6;
    y=MAX(NSMinY(safe),MIN(y,NSMaxY(safe)-height));
    // Keep response buttons on the edge nearest the icon, including a strip
    // near the top of the display where the popover has to open downwards.
    BOOL above = y >= NSMaxY(anchor);
    CGFloat controlsY = above ? 19 : height-51;
    CGFloat hintY = above ? 69 : height-95;
    CGFloat titleY = above ? height-36 : height-129;
    CGFloat questionY = above ? height-62 : height-155;
    NSRect detailsFrame = above ? NSMakeRect(16,112,width-32,height-184) : NSMakeRect(16,16,width-32,height-183);
    if (!unavailable && above) {
        questionY = 112;
        detailsFrame = NSMakeRect(16,142,width-32,height-190);
    }
    BOOL hasPreview = [request[@"preview"] isKindOfClass:NSDictionary.class];
    DevJayPermissionContent *content = [[[DevJayPermissionContent alloc] initWithFrame:NSMakeRect(0,0,width,height)] autorelease];
    content.wantsLayer = YES; content.layer.cornerRadius = 10;
    content.layer.backgroundColor = NSColor.windowBackgroundColor.CGColor;
    content.layer.borderWidth = 1; content.layer.borderColor = NSColor.systemOrangeColor.CGColor;
    NSTextField *title = [NSTextField labelWithString:request[@"title"]];
    title.font = [NSFont boldSystemFontOfSize:13];title.frame=NSMakeRect(16,titleY,width-(hasPreview ? 152 : 32),20);[content addSubview:title];
    NSString *summary = unavailable ? @"This question must be answered in Claude." : requests.count>1 ? [NSString stringWithFormat:@"Do you want to proceed? (%lu requests waiting)",(unsigned long)requests.count] : @"Do you want to proceed?";
    NSTextField *question=[NSTextField labelWithString:summary];question.frame=NSMakeRect(16,questionY,width-32,20);[content addSubview:question];
    NSScrollView *scroll=[[[NSScrollView alloc] initWithFrame:detailsFrame] autorelease];
    scroll.hasVerticalScroller=YES;scroll.hasHorizontalScroller=NO;scroll.borderType=NSBezelBorder;
    NSTextView *details=[[[NSTextView alloc] initWithFrame:NSMakeRect(0,0,scroll.contentSize.width,scroll.contentSize.height)] autorelease];
    details.editable=NO;details.selectable=YES;details.richText=YES;
    details.automaticLinkDetectionEnabled=NO;
    details.font=unavailable ? [NSFont systemFontOfSize:12] : [NSFont monospacedSystemFontOfSize:11 weight:NSFontWeightRegular];
    [details.textStorage setAttributedString:initialDetails];details.textContainerInset=NSMakeSize(10,10);
    details.verticallyResizable=YES;details.horizontallyResizable=NO;
    details.autoresizingMask=NSViewWidthSizable;
    details.textContainer.widthTracksTextView=YES;details.textContainer.containerSize=NSMakeSize(scroll.contentSize.width,CGFLOAT_MAX);
    scroll.documentView=details;[content addSubview:scroll];
    permissionPanel->details=details;permissionPanel->detailsScroll=scroll;permissionPanel->detailsToggle=nil;
    if (hasPreview) {
        NSButton *toggle=[NSButton buttonWithTitle:@"Show JSON" target:permissionPanel action:@selector(toggleDetails:)];
        toggle.frame=NSMakeRect(width-124,titleY-5,112,28);[content addSubview:toggle];
        toggle.toolTip=@"Switch between the readable preview and complete original tool input.";
        permissionPanel->detailsToggle=toggle;
    }
    NSTextField *hint=[NSTextField wrappingLabelWithString:permission_hint(permissionPanel->token)];
    hint.font=[NSFont systemFontOfSize:11];hint.textColor=NSColor.secondaryLabelColor;hint.frame=NSMakeRect(16,hintY,width-32,34);
    permissionPanel->hint=hint;[content addSubview:hint];
    // The first response sits beside the originating icon, at the left edge.
    // No default Return/Escape equivalents: only explicit responses submit.
    permissionPanel->yes=nil;permissionPanel->no=nil;
    NSButton *go=permission_shortcut_button(@"Claude",permissionPanel,@selector(goToClaude:));
    go.frame=NSMakeRect(unavailable ? 12 : width-108,controlsY,96,32);
    go.toolTip=@"Go to Claude without answering (C). Opens this Site's IDE or terminal; the exact terminal tab may need selecting.";
    if (unavailable) {
        [content addSubview:go];
        NSButton *settings=[NSButton buttonWithTitle:@"Prompt settings…" target:permissionPanel action:@selector(promptSettings:)];
        settings.frame=NSMakeRect(width-172,controlsY,160,32);[content addSubview:settings];
        go.nextKeyView=settings;settings.nextKeyView=go;
    } else {
        NSButton *yes=permission_shortcut_button(@"Yes",permissionPanel,@selector(chooseYes:));yes.frame=NSMakeRect(12,controlsY,80,32);
        yes.toolTip=@"Allow this request once. Shortcut: Y.";[content addSubview:yes];
        NSButton *no=permission_shortcut_button(@"No",permissionPanel,@selector(chooseNo:));no.frame=NSMakeRect(98,controlsY,80,32);
        no.toolTip=@"Deny this request. Shortcut: N.";[content addSubview:no];
        [content addSubview:go];
        yes.nextKeyView=no;no.nextKeyView=go;go.nextKeyView=yes;
        permissionPanel->yes=yes;permissionPanel->no=no;
    }
    permissionPanel.contentView=content;
    [permissionPanel setFrame:NSMakeRect(x,y,width,height) display:YES];
    [permissionPanel orderFrontRegardless]; // Shortcuts work without taking keyboard focus.
    permission_start_hover_keys();
    permissionPanel->hoverTimer=[NSTimer scheduledTimerWithTimeInterval:.15 target:permissionPanel selector:@selector(tick:) userInfo:nil repeats:YES];
    permissionPanel->hoverTimer.tolerance=.05;
    return YES;
}
static void permission_close(NSWindow *parent) {
    if (permissionPanel && (!parent || permissionPanel->origin == parent)) [permissionPanel closePopover];
}
void devjay_update_claude_permissions(const char *json) {
    NSString *value=json?[NSString stringWithUTF8String:json]:nil;
    id parsed=value?[NSJSONSerialization JSONObjectWithData:[value dataUsingEncoding:NSUTF8StringEncoding] options:0 error:nil]:nil;
    if(![parsed isKindOfClass:NSDictionary.class])parsed=@{};
    if([permissionViews isEqual:parsed])return;
    [permissionViews release];permissionViews=[parsed copy];
    if(permissionPanel.visible&&!permissionPanel->feedbackTimer&&!permission_find(permissionPanel->token))[permissionPanel closePopover];
    if(!permissionPanel.visible && !NSPointInRect(NSEvent.mouseLocation,permissionSuppressedAnchor)){
        [permissionSuppressedHost release];permissionSuppressedHost=nil;
    }
}
// Leave every unrelated event untouched, including Return and Space.
static NSEvent *permission_local_key_event(NSEvent *event) {
    if (permissionPanel.visible && !permissionPanel->sent && permissionPanel.keyWindow && event.window==permissionPanel && permission_is_shortcut(event)) {
        [permissionPanel keyDown:event];
        return nil;
    }
    return event;
}
void devjay_install_claude_popover(void) {
    devjay_set_permission_hover(permission_show,permission_close);
    for(NSNotificationName name in @[NSWorkspaceActiveSpaceDidChangeNotification, NSWorkspaceWillSleepNotification,
        NSWorkspaceScreensDidSleepNotification, NSWorkspaceSessionDidResignActiveNotification, NSWorkspaceDidActivateApplicationNotification]) {
        [NSWorkspace.sharedWorkspace.notificationCenter addObserverForName:name object:nil queue:NSOperationQueue.mainQueue usingBlock:^(NSNotification *note){(void)note;permission_close(nil);}];
    }
    [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskLeftMouseDown|NSEventMaskRightMouseDown handler:^NSEvent *(NSEvent *event){
        if(permissionPanel.visible&&event.window!=permissionPanel)permission_close(nil);
        return event;
    }];
    // Click-focused panels also retain local keyboard support, including when
    // system hotkey registration is unavailable. Modified shortcuts pass on.
    [NSEvent addLocalMonitorForEventsMatchingMask:NSEventMaskKeyDown handler:^NSEvent *(NSEvent *event){
        return permission_local_key_event(event);
    }];
}
