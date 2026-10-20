#import <AppKit/AppKit.h>

extern char *devjay_profile_displays(void);
extern void devjay_show_context_dialog(void *window, void *parent);
extern void devjay_profile_prompt_chosen(const char *signature);

// The strip can sit at any screen edge. A standalone, modeless alert can be
// clamped independently and cannot trap the app behind an offscreen sheet.
@interface DevJayProfilePrompt : NSResponder <NSWindowDelegate> {
@public
    NSAlert *alert;
    NSString *signature;
}
- (id)initWithSignature:(NSString *)value;
- (void)dismiss:(id)sender;
- (void)choose:(id)sender;
- (void)screensChanged:(NSNotification *)notification;
@end
static DevJayProfilePrompt *profilePrompt;

@implementation DevJayProfilePrompt
- (id)initWithSignature:(NSString *)value {
    if ((self = [super init])) {
        signature = [value copy];
        alert = [NSAlert new];
        alert.alertStyle = NSAlertStyleInformational;
        alert.messageText = @"Choose a Profile for these displays";
        alert.informativeText = @"DevJay detected a different monitor combination. Create a Profile or choose an existing one. Your saved window arrangements are kept until you choose.";
        for (NSString *title in @[@"Create Profile…", @"Choose Profile…", @"Not now"]) {
            NSButton *button = [alert addButtonWithTitle:title];
            button.target = self;
            button.action = [title isEqual:@"Not now"] ? @selector(dismiss:) : @selector(choose:);
        }
        alert.buttons.firstObject.keyEquivalent = @"\r";
        alert.buttons.lastObject.keyEquivalent = @"\e";
        [alert layout];
        alert.window.identifier = @"profile-prompt";
        alert.window.releasedWhenClosed = NO;
        alert.window.delegate = self;
        self.nextResponder = alert.window.nextResponder;
        alert.window.nextResponder = self;
        [NSNotificationCenter.defaultCenter addObserver:self selector:@selector(screensChanged:)
            name:NSApplicationDidChangeScreenParametersNotification object:nil];
    }
    return self;
}
- (void)dismiss:(id)sender { [alert.window close]; }
- (void)choose:(id)sender {
    devjay_profile_prompt_chosen(signature.UTF8String);
    [self dismiss:sender];
}
- (void)cancelOperation:(id)sender { [self dismiss:sender]; }
- (void)screensChanged:(NSNotification *)notification {
    char *raw = devjay_profile_displays();
    if (!raw) return;
    NSData *data = [[NSString stringWithUTF8String:raw] dataUsingEncoding:NSUTF8StringEncoding];
    free(raw);
    NSDictionary *displays = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    NSString *current = displays[@"signature"];
    if (current.length && ![signature isEqual:current]) [self dismiss:nil];
}
- (void)windowWillClose:(NSNotification *)notification {
    [NSNotificationCenter.defaultCenter removeObserver:self];
    alert.window.delegate = nil;
    alert.window.nextResponder = self.nextResponder;
    if (profilePrompt == self) profilePrompt = nil;
    [self autorelease];
}
- (void)dealloc { [signature release]; [alert release]; [super dealloc]; }
@end

void devjay_sync_profile_prompt(const char *value) {
    NSString *current = value ? [NSString stringWithUTF8String:value] : @"";
    if (profilePrompt && ![profilePrompt->signature isEqual:current]) [profilePrompt dismiss:nil];
}

void devjay_show_profile_prompt(void *parent, const char *value) {
    NSString *signature = value ? [NSString stringWithUTF8String:value] : @"";
    if (!signature.length) return;
    devjay_sync_profile_prompt(value);
    if (profilePrompt) return;
    profilePrompt = [[DevJayProfilePrompt alloc] initWithSignature:signature];
    devjay_show_context_dialog(profilePrompt->alert.window, parent);
}
