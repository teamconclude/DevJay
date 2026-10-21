#import <AppKit/AppKit.h>
#import <Carbon/Carbon.h>
#import <objc/runtime.h>
#include <assert.h>
#include <stdio.h>
static NSString *lastToken,*lastDecision;
static int replies;
void goDevJayClaudePermissionDecision(char *token,char *decision){
 [lastToken release];[lastDecision release];lastToken=[[NSString stringWithUTF8String:token] copy];lastDecision=[[NSString stringWithUTF8String:decision] copy];replies++;
}
void devjay_set_permission_hover(BOOL (*show)(NSWindow *,NSRect,NSString *,NSString *),void (*close)(NSWindow *)){(void)show;(void)close;}
// Never register real system shortcuts or post events from this unit harness.
static unsigned int liveHotkeys,hotkeyRegistrations;
static int failHotkeySlot=-1;
static NSPoint pointer;
static NSEventModifierFlags modifiers;
static BOOL heldKeys[128];
static EventHandlerUPP hotkeyHandler;
static OSStatus fakeInstall(EventTargetRef target,EventHandlerUPP handler,ItemCount count,const EventTypeSpec *types,void *context,EventHandlerRef *ref){
 (void)target;(void)context;assert(count==2&&types[0].eventKind==kEventHotKeyPressed&&types[1].eventKind==kEventHotKeyReleased);
 hotkeyHandler=handler;*ref=(EventHandlerRef)(uintptr_t)1;return noErr;
}
static OSStatus fakeRegister(UInt32 code,UInt32 flags,EventHotKeyID key,EventTargetRef target,OptionBits options,EventHotKeyRef *ref){
 (void)target;unsigned int slot=key.id&15;assert(code<128&&slot<8&&options==kEventHotKeyExclusive);
 assert(code!=kVK_Return&&code!=kVK_ANSI_KeypadEnter&&code!=kVK_Space&&code!=kVK_Tab);
 assert(flags==(slot<4?0:shiftKey));hotkeyRegistrations++;
 if((int)slot==failHotkeySlot)return eventHotKeyExistsErr;
 liveHotkeys++;*ref=(EventHotKeyRef)(uintptr_t)(key.id+1);return noErr;
}
static OSStatus fakeUnregister(EventHotKeyRef ref){assert(ref&&liveHotkeys>0);liveHotkeys--;return noErr;}
static bool fakeKeyState(CGEventSourceStateID source,CGKeyCode code){assert(source==kCGEventSourceStateCombinedSessionState&&code<128);return heldKeys[code];}
static NSPoint fakeMouse(id object,SEL selector){(void)object;(void)selector;return pointer;}
static NSEventModifierFlags fakeModifiers(id object,SEL selector){(void)object;(void)selector;return modifiers;}
#define InstallEventHandler fakeInstall
#define RegisterEventHotKey fakeRegister
#define UnregisterEventHotKey fakeUnregister
#define CGEventSourceKeyState fakeKeyState
#include "../cmd/devjay/claude_permission_panel_darwin.m"
@interface PermissionKeyTestPanel : DevJayPermissionPanel
@end
@implementation PermissionKeyTestPanel
- (BOOL)isKeyWindow{return YES;}
- (BOOL)isVisible{return token!=nil;}
@end
static NSEvent *key(NSString *value,BOOL repeat){return [NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:0 timestamp:0 windowNumber:permissionPanel.windowNumber context:nil characters:value charactersIgnoringModifiers:value isARepeat:repeat keyCode:0];}
static void arm(DevJayPermissionPanel *panel,NSString *token){
 [panel closePopover];panel->token=[token copy];panel->sent=NO;
 panel->yes=[NSButton buttonWithTitle:@"Yes" target:nil action:nil];panel->no=[NSButton buttonWithTitle:@"No" target:nil action:nil];panel->hint=[NSTextField labelWithString:@""];
}
@interface HoverTestWindow : NSWindow {
@public BOOL available,activeSpace,minimized;
}
@end
@implementation HoverTestWindow
- (BOOL)isVisible{return available;}
- (BOOL)isOnActiveSpace{return activeSpace;}
- (BOOL)isMiniaturized{return minimized;}
@end
@interface HoverTestPanel : DevJayPermissionPanel
@end
@implementation HoverTestPanel
- (BOOL)isVisible{return token!=nil;}
- (BOOL)isKeyWindow{return NO;}
- (void)makeKeyWindow{assert(!"Hover must not request keyboard focus");}
- (void)makeMainWindow{assert(!"Hover must not request main window status");}
@end
static OSStatus shortcut(UInt32 kind,UInt32 identifier){
 EventRef event;assert(CreateEvent(NULL,kEventClassKeyboard,kind,0,0,&event)==noErr);
 EventHotKeyID key={permissionShortcutSignature,identifier};
 assert(SetEventParameter(event,kEventParamDirectObject,typeEventHotKeyID,sizeof(key),&key)==noErr);
 OSStatus result=permission_hover_shortcut(NULL,event,NULL);ReleaseEvent(event);return result;
}
static UInt32 shortcutID(unsigned int slot){return(permissionShortcutGeneration<<4)|slot;}
static void press(unsigned int slot){assert(shortcut(kEventHotKeyPressed,shortcutID(slot))==noErr);}
static void releaseKey(unsigned int slot){assert(shortcut(kEventHotKeyReleased,shortcutID(slot))==noErr);}
static void setupHover(HoverTestWindow *origin,NSString *token){
 [permissionPanel closePopover];arm(permissionPanel,token);permissionPanel->origin=origin;
 permissionPanel->anchor=NSMakeRect(20,20,80,30);pointer=NSMakePoint(50,35);modifiers=0;
 origin->available=origin->activeSpace=YES;origin->minimized=NO;
}
static void hoverTests(void){
 Method mouse=class_getClassMethod(NSEvent.class,@selector(mouseLocation));
 Method flags=class_getClassMethod(NSEvent.class,@selector(modifierFlags));
 IMP oldMouse=method_setImplementation(mouse,(IMP)fakeMouse),oldFlags=method_setImplementation(flags,(IMP)fakeModifiers);
 HoverTestWindow *origin=[[[HoverTestWindow alloc]initWithContentRect:NSMakeRect(0,0,800,800) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO]autorelease];origin.releasedWhenClosed=NO;
 permissionPanel=[[HoverTestPanel alloc]initWithContentRect:NSMakeRect(300,100,400,300) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];permissionPanel.releasedWhenClosed=NO;
 devjay_update_claude_permissions("{\"site\\nhost\":[{\"token\":\"hover\"},{\"token\":\"waiting\",\"unavailable\":true}]}");
 int before=replies;
 // A pending request outside both regions must never register shortcuts.
 setupHover(origin,@"hover");pointer=NSMakePoint(850,850);permission_sync_hover_keys();assert(liveHotkeys==0);
 pointer=NSMakePoint(50,35);permission_sync_hover_keys();assert(liveHotkeys==8&&hotkeyHandler&&!permissionPanel.keyWindow);
 // Y answers immediately while another window retains focus. A modified
 // shortcut must not answer, and no Return/Space key is ever registered.
 modifiers=NSEventModifierFlagCommand;press(1);assert(replies==before);releaseKey(1);modifiers=0;
 UInt32 obsolete=shortcutID(0);press(0);assert(replies==++before&&[lastDecision isEqual:@"allow"]&&liveHotkeys==0);
 assert(shortcut(kEventHotKeyPressed,obsolete)==eventNotHandledErr&&replies==before);
 // A queued event from the previous registration cannot answer a new request.
 setupHover(origin,@"hover");permission_sync_hover_keys();assert(shortcut(kEventHotKeyPressed,obsolete)==eventNotHandledErr&&replies==before);
 // N works over the popup itself, even though a different window is key.
 pointer=NSMakePoint(400,200);press(1);assert(replies==++before&&[lastDecision isEqual:@"deny"]&&liveHotkeys==0);
 // Movement unregisters immediately, before the visual panel's exit grace.
 setupHover(origin,@"hover");permission_sync_hover_keys();
 pointer=NSMakePoint(250,60);permission_sync_hover_keys();assert(liveHotkeys==0&&permissionPanel.visible);
 pointer=NSMakePoint(400,200);permission_sync_hover_keys();press(4); // Shift Y also answers immediately.
 assert(replies==++before&&[lastDecision isEqual:@"allow"]&&liveHotkeys==0);
 // Escape and Shift Escape dismiss without sending any answer.
 for(unsigned int slot=3;slot<8;slot+=4){
  setupHover(origin,@"hover");permission_sync_hover_keys();press(slot);
  assert(replies==before&&liveHotkeys==0&&!permissionPanel->token);
 }
 // Scope is checked again when a queued Y is delivered, including pointer
 // exit, hidden/minimized origin and desktop changes.
 for(int condition=0;condition<4;condition++){
  setupHover(origin,@"hover");permission_sync_hover_keys();
  if(condition==0)pointer=NSMakePoint(0,0);
  if(condition==1)origin->available=NO;
  if(condition==2)origin->activeSpace=NO;
  if(condition==3)origin->minimized=YES;
  press(0);assert(replies==before&&liveHotkeys==0);
 }
 // Holding Y or N before opening cannot answer via repeat; only a fresh
 // release and press can respond to this request.
 UInt32 codes[4];assert(permission_shortcut_codes(codes));
 for(unsigned int slot=0;slot<2;slot++){
  setupHover(origin,@"hover");heldKeys[codes[slot]]=YES;
  permission_sync_hover_keys();press(slot);press(slot);assert(replies==before);
  heldKeys[codes[slot]]=NO;releaseKey(slot);press(slot);
  assert(replies==++before&&[lastDecision isEqual:slot==0?@"allow":@"deny"]&&liveHotkeys==0);
 }
 // Missing/uncaptured requests never accept Yes/No; C remains navigation only.
 setupHover(origin,@"waiting");permission_sync_hover_keys();assert(liveHotkeys==4);
 assert(shortcut(kEventHotKeyPressed,shortcutID(0))==eventNotHandledErr&&shortcut(kEventHotKeyPressed,shortcutID(1))==eventNotHandledErr);
 assert(replies==before);
 press(2);assert(replies==++before&&[lastDecision isEqual:@"open"]&&liveHotkeys==0);
 // A conflict rolls back all registrations, without a retry loop or approval.
 setupHover(origin,@"hover");failHotkeySlot=4;permission_sync_hover_keys();assert(liveHotkeys==0&&permissionHoverKeysFailed);
 unsigned int attempts=hotkeyRegistrations;permission_sync_hover_keys();assert(attempts==hotkeyRegistrations);
 assert([permissionPanel->hint.stringValue containsString:@"Click the panel"]);
 failHotkeySlot=-1;pointer=NSMakePoint(0,0);permission_sync_hover_keys();pointer=NSMakePoint(50,35);permission_sync_hover_keys();assert(liveHotkeys==8);
 devjay_update_claude_permissions("{}");assert(liveHotkeys==0&&!permissionPanel->token&&replies==before);
 [permissionPanel release];permissionPanel=nil;
 method_setImplementation(mouse,oldMouse);method_setImplementation(flags,oldFlags);
}
static void feedbackTests(void){
 HoverTestWindow *origin=[[[HoverTestWindow alloc]initWithContentRect:NSMakeRect(0,0,800,800) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO]autorelease];origin.releasedWhenClosed=NO;
 permissionPanel=[[HoverTestPanel alloc]initWithContentRect:NSMakeRect(300,100,400,300) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO];permissionPanel.releasedWhenClosed=NO;
 for(NSString *decision in @[@"allow",@"deny"]){
  devjay_update_claude_permissions("{\"site\\nhost\":[{\"token\":\"feedback\"}]}");
  setupHover(origin,@"feedback");int before=replies;
  if([decision isEqual:@"allow"])[permissionPanel chooseYes:nil];else[permissionPanel handlePermissionKey:@"n"];
  // The bridge callback is synchronous with selection; it never waits on the
  // one-second visual timer. Mouse and keyboard share the same feedback.
  assert(replies==before+1&&[lastDecision isEqual:decision]);
  assert(permissionPanel.visible&&permissionPanel->sent&&permissionPanel->feedbackTimer);
  assert(permissionPanel->feedbackTimer.fireDate.timeIntervalSinceNow>.8&&permissionPanel->feedbackTimer.fireDate.timeIntervalSinceNow<=1);
  NSButton *selected=[decision isEqual:@"allow"]?permissionPanel->yes:permissionPanel->no;
  assert(permissionPanel->feedbackButton==selected&&[selected.layer animationForKey:@"permission-response"]);
  assert(!permissionPanel->yes.enabled&&!permissionPanel->no.enabled&&liveHotkeys==0);
  NSEvent *event=key(@"y",NO);assert(permission_local_key_event(event)==event);
  [permissionPanel chooseYes:nil];[permissionPanel chooseNo:nil];[permissionPanel goToClaude:nil];
  assert(replies==before+1&&permissionPanel->feedbackTimer);
  // The manager removes answered requests immediately; that publication and
  // pointer exit must not cut the visual acknowledgment short.
  devjay_update_claude_permissions("{}");[permissionPanel tick:nil];
  assert(permissionPanel.visible&&permissionPanel->feedbackTimer&&replies==before+1);
  assert(permission_show(origin,NSZeroRect,@"different",@"host"));
  assert([permissionPanel->token isEqual:@"feedback"]);
  [permissionPanel->feedbackTimer fire];assert(!permissionPanel.visible&&!permissionPanel->feedbackTimer);
  assert(replies==before+1);
 }
 // Early closure cancels feedback; an old timer cannot dismiss a later prompt.
 devjay_update_claude_permissions("{\"site\\nhost\":[{\"token\":\"feedback\"}]}");
 setupHover(origin,@"feedback");[permissionPanel chooseYes:nil];
 NSTimer *obsolete=[permissionPanel->feedbackTimer retain];
 origin->activeSpace=NO;[permissionPanel tick:nil];assert(!permissionPanel.visible&&!obsolete.valid);
 setupHover(origin,@"feedback");[permissionPanel finishFeedback:obsolete];assert(permissionPanel.visible&&!permissionPanel->sent);
 [obsolete release];[permissionPanel closePopover];[permissionPanel release];permissionPanel=nil;
}
static void responseButtonTests(void){
 devjay_update_claude_permissions("{\"site\\nhost\":[{\"token\":\"button-only\"}]}");
 DevJayPermissionPanel *panel=[[[PermissionKeyTestPanel alloc]initWithContentRect:NSMakeRect(0,0,400,300) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO]autorelease];panel.releasedWhenClosed=NO;
 for(NSString *decision in @[@"allow",@"deny"]){
  arm(panel,@"button-only");int before=replies;
  SEL action=[decision isEqual:@"allow"]?@selector(chooseYes:):@selector(chooseNo:);
  NSButton *button=permission_shortcut_button([decision isEqual:@"allow"]?@"Yes":@"No",panel,action);
  [panel.contentView addSubview:button];
  for(NSString *value in @[@" ",@"\r",@"\n"])[button keyDown:key(value,NO)];
  assert(replies==before&&!panel->sent);
  [button performClick:nil];assert(replies==before+1&&[lastDecision isEqual:decision]);
  [panel closePopover];[button removeFromSuperview];
 }
}
int main(void){@autoreleasepool{
 [NSApplication sharedApplication];[NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
 devjay_update_claude_permissions("{\"site\\nhost\":[{\"token\":\"one\",\"title\":\"Test\",\"details\":\"printf test\"},{\"token\":\"two\",\"title\":\"Test2\",\"details\":\"printf test2\"}]}");
 DevJayPermissionPanel *panel=[[[PermissionKeyTestPanel alloc] initWithContentRect:NSMakeRect(0,0,540,400) styleMask:NSWindowStyleMaskBorderless|NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO] autorelease];panel.releasedWhenClosed=NO;
 // The local fallback returns unrelated events unchanged, even after an
 // explicit click focuses the panel. Hover never takes that focus.
 permissionPanel=panel;arm(panel,@"one");
 for(NSString *value in @[@"x",@" ",@"\r",@"\n",@"\t",@"1",@"\uf700"]){
  NSEvent *event=key(value,NO);assert(permission_local_key_event(event)==event&&replies==0);
 }
 for(NSNumber *flag in @[@(NSEventModifierFlagCommand),@(NSEventModifierFlagControl),@(NSEventModifierFlagOption)]){
  NSEvent *event=[NSEvent keyEventWithType:NSEventTypeKeyDown location:NSZeroPoint modifierFlags:flag.unsignedIntegerValue timestamp:0 windowNumber:panel.windowNumber context:nil characters:@"y" charactersIgnoringModifiers:@"y" isARepeat:NO keyCode:0];
  assert(permission_local_key_event(event)==event&&replies==0);
 }
 assert(permission_local_key_event(key(@"y",YES))==nil&&replies==0);
 assert(permission_local_key_event(key(@"y",NO))==nil&&replies==1&&[lastToken isEqual:@"one"]&&[lastDecision isEqual:@"allow"]);
 permissionPanel=nil;
 [panel chooseYes:nil];assert(replies==1);
 arm(panel,@"two");[panel keyDown:key(@"n",NO)];assert(replies==2&&[lastDecision isEqual:@"deny"]);
 // Escape dismisses without sending No (or any reply).
 arm(panel,@"two");[panel keyDown:key(@"\033",NO)];assert(replies==2&&!panel->token);
 arm(panel,@"two");[panel keyDown:key(@"c",YES)];assert(replies==2);[panel keyDown:key(@"c",NO)];assert(replies==3&&[lastDecision isEqual:@"open"]);
 [panel goToClaude:nil];assert(replies==3);
 arm(panel,@"two");[panel goToClaude:nil];assert(replies==4&&[lastDecision isEqual:@"open"]);
 arm(panel,@"two");[panel keyDown:key(@" ",NO)];assert(replies==4);[panel chooseNo:nil];assert(replies==5&&[lastDecision isEqual:@"deny"]);
 arm(panel,@"gone");[panel keyDown:key(@"c",NO)];assert(replies==5);
 arm(panel,@"two");[panel handlePermissionKey:@"\r"];[panel handlePermissionKey:@" "];assert(replies==5);
 DevJayPermissionPanel *unfocused=[[[DevJayPermissionPanel alloc] initWithContentRect:NSZeroRect styleMask:NSWindowStyleMaskNonactivatingPanel backing:NSBackingStoreBuffered defer:NO] autorelease];unfocused.releasedWhenClosed=NO;arm(unfocused,@"one");[unfocused keyDown:key(@"y",NO)];[unfocused keyDown:key(@"c",NO)];assert(replies==5);
 devjay_update_claude_permissions("{}");[panel chooseYes:nil];assert(replies==5);
 // Status-only popups must never manufacture a permission response.
 devjay_update_claude_permissions("{\"site\\nhost\":[{\"token\":\"waiting\",\"unavailable\":true}]}");
 arm(panel,@"waiting");[panel keyDown:key(@"y",NO)];assert(replies==5);
 [panel keyDown:key(@"\r",NO)];[panel keyDown:key(@"n",NO)];[panel chooseYes:nil];[panel chooseNo:nil];assert(replies==5&&panel->token);
 [panel keyDown:key(@"c",NO)];assert(replies==6&&[lastDecision isEqual:@"open"]);
 arm(panel,@"waiting");[panel promptSettings:nil];assert(replies==7&&[lastDecision isEqual:@"settings"]);
 // The readable preview and original JSON are two views of the same token.
 NSDictionary *request=@{@"token":@"preview",@"details":@"{\"command\":\"printf test\"}",@"preview":@{@"title":@"Bash command",@"description":@"Show the test text",@"sections":@[@{@"label":@"Command",@"kind":@"code",@"text":@"printf test"}]}};
 NSData *json=[NSJSONSerialization dataWithJSONObject:@{@"site\nhost":@[request]} options:0 error:nil];
 NSString *encoded=[[[NSString alloc] initWithData:json encoding:NSUTF8StringEncoding] autorelease];devjay_update_claude_permissions(encoded.UTF8String);
 arm(panel,@"preview");panel->details=[[[NSTextView alloc] initWithFrame:NSMakeRect(0,0,400,180)] autorelease];
 panel->detailsToggle=[NSButton buttonWithTitle:@"Show JSON" target:nil action:nil];panel->detailsScroll=nil;panel->showingRaw=NO;
 [panel->details.textStorage setAttributedString:permission_details(request,NO)];
 assert([panel->details.string containsString:@"Show the test text"]&&[panel->details.string containsString:@"Command\nprintf test"]);
 [panel toggleDetails:nil];assert(panel->showingRaw&&[panel->details.string isEqual:request[@"details"]]&&[panel->detailsToggle.title isEqual:@"Show preview"]);
 [panel toggleDetails:nil];assert(!panel->showingRaw&&[panel->details.string containsString:@"Command\nprintf test"]&&replies==7&&[panel->token isEqual:@"preview"]);
 permission_close(nil);
 hoverTests();
 feedbackTests();
 responseButtonTests();
 puts("Claude permission panel: hover scope, inactive focus, key repeats, lifecycle, conflicts and response tests passed");
}return 0;}
