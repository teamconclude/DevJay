#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <assert.h>
#include <stdio.h>

static BOOL loaded;
@interface ActionLayoutDelegate : NSObject <WKNavigationDelegate>
@end
@implementation ActionLayoutDelegate
- (void)webView:(WKWebView *)view didFinishNavigation:(WKNavigation *)navigation {
    (void)view; (void)navigation; loaded = YES;
}
@end
static void spin(BOOL *done) {
    NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:15];
    while (!*done && deadline.timeIntervalSinceNow > 0)
        [NSRunLoop.currentRunLoop runUntilDate:[NSDate dateWithTimeIntervalSinceNow:.01]];
    assert(*done);
}
static id evaluate(WKWebView *view, NSString *script) {
    __block BOOL done = NO;
    __block id value;
    [view evaluateJavaScript:script completionHandler:^(id result, NSError *error) {
        if (error) NSLog(@"%@", error);
        assert(!error); value = [result retain]; done = YES;
    }];
    spin(&done);
    return [value autorelease];
}
int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        NSString *css = [NSString stringWithContentsOfFile:@"cmd/devjay/frontend/style.css" encoding:NSUTF8StringEncoding error:nil];
        NSMutableString *js = [NSMutableString stringWithString:@"let clock=1000,timers=new Map(),timerID=0,closed=[];Date.now=()=>clock;window.setTimeout=(fn,delay)=>{timers.set(++timerID,{fn,at:clock+delay});return timerID};window.clearTimeout=id=>timers.delete(id);function advance(ms){clock+=ms;for(const [id,t] of [...timers])if(t.at<=clock){timers.delete(id);t.fn()}};"];
        NSRegularExpression *imports = [NSRegularExpression regularExpressionWithPattern:@"^import .*;\\n" options:NSRegularExpressionAnchorsMatchLines error:nil];
        for (NSString *name in @[@"pr-status",@"suggested-actions"]) {
            NSString *code = [NSString stringWithContentsOfFile:[NSString stringWithFormat:@"cmd/devjay/frontend/%@.js",name] encoding:NSUTF8StringEncoding error:nil]; assert(code);
            code = [imports stringByReplacingMatchesInString:code options:0 range:NSMakeRange(0,code.length) withTemplate:@""];
            [js appendFormat:@"\n%@\n",[code stringByReplacingOccurrencesOfString:@"export " withString:@""]];
        }
        [js appendString:@"const confirmed=[];const render=mountSuggestedActions(document.querySelector('#app'),data=>closed.push(data),(...args)=>confirmed.push(args));const site={id:'c1',name:'c1',path:'/tmp/c1',git:{branch:'develop'}};function complete(kind,id='result'){render({suggestedSite:'c1',actionGeneration:7,sites:[site],action:{id,siteID:'c1',kind,status:'complete',message:'Updated develop from the remote. Nothing was pushed.',currentBranch:'develop',currentHead:'1234567890abcdef',recovery:'refs/devjay/backups/0123456789abcdef0123456789abcdef'}})};function update(id){render({suggestedSite:'c1',actionGeneration:7,sites:[site],action:{id,siteID:'c1',kind:'update',status:'ready',plan:{id,kind:'update',branch:'develop',target:'origin/develop',head:'1234567890abcdef',targetHead:'abcdef1234567890'}}})};"];
        NSString *html = [NSString stringWithFormat:@"<!doctype html><meta charset='utf-8'><style>%@</style><body class='suggestions'><main id='app'></main><script>%@</script></body>",css,js];
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,460,470) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *config = [[[WKWebViewConfiguration alloc] init] autorelease]; config.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *view = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,460,470) configuration:config] autorelease];
        ActionLayoutDelegate *delegate = [[[ActionLayoutDelegate alloc] init] autorelease]; view.navigationDelegate = delegate; window.contentView = view;
        [view loadHTMLString:html baseURL:nil]; spin(&loaded);
        for (NSString *kind in @[@"update",@"merged",@"publish",@"rebase-push",@"switch"]) {
            BOOL compact = [kind isEqual:@"publish"] || [kind isEqual:@"rebase-push"];
            for (NSNumber *width in @[@460,@400]) {
                [window setContentSize:NSMakeSize(width.doubleValue,compact ? 330 : 470)];
                evaluate(view,[NSString stringWithFormat:@"complete('%@','%@-%@')",kind,kind,width]);
                assert([evaluate(view,@"document.body.scrollWidth<=innerWidth+1 && document.body.scrollHeight<=innerHeight+1") boolValue]);
                assert([evaluate(view,@"(()=>{const footer=document.querySelector('.suggestion-footer').getBoundingClientRect(),note=document.querySelector('.action-countdown').getBoundingClientRect(),done=[...document.querySelectorAll('button')].find(b=>b.textContent==='Done').getBoundingClientRect();return footer.bottom<=innerHeight && note.right<done.left && note.top>=done.top && note.bottom<=done.bottom && note.left>=0})()") boolValue]);
                assert([evaluate(view,@"document.querySelector('.action-countdown').textContent==='Auto closing in 5 seconds.' && document.querySelector('.action-auto-countdown').checked") boolValue]);
                evaluate(view,@"advance(3000);render.escape();advance(10000)");
                assert([evaluate(view,@"closed.length===0 && document.querySelector('.action-countdown').textContent==='Auto closing stopped.' && !document.querySelector('.action-auto-countdown').checked && [...document.querySelectorAll('button')].some(b=>b.textContent==='Done'&&!b.disabled)") boolValue]);
                evaluate(view,@"document.querySelector('.action-countdown').click();advance(4000)");
                assert([evaluate(view,@"document.querySelector('.action-auto-countdown').checked && closed.length===0") boolValue]);
                evaluate(view,@"document.querySelector('.action-countdown').click();advance(10000)");
                assert([evaluate(view,@"!document.querySelector('.action-auto-countdown').checked && closed.length===0 && document.querySelector('.action-countdown').textContent==='Auto closing stopped.'") boolValue]);
            }
        }
        evaluate(view,@"complete('update','last');advance(4999)");
        assert([evaluate(view,@"closed.length===0") boolValue]);
        evaluate(view,@"advance(1)");
        assert([evaluate(view,@"closed.length===1 && closed[0].generation===7") boolValue]);
        for (NSNumber *width in @[@460,@400]) {
            [window setContentSize:NSMakeSize(width.doubleValue,470)];
            evaluate(view,[NSString stringWithFormat:@"closed=[];confirmed.length=0;update('update-%@')",width]);
            assert([evaluate(view,@"document.body.scrollWidth<=innerWidth+1 && document.body.scrollHeight<=innerHeight+1") boolValue]);
            assert([evaluate(view,@"(()=>{const footer=document.querySelector('.suggestion-footer').getBoundingClientRect(),note=document.querySelector('.action-countdown').getBoundingClientRect(),start=document.querySelector('.suggestion-primary').getBoundingClientRect();return footer.bottom<=innerHeight && note.right<start.left && note.top>=start.top && note.bottom<=start.bottom && note.left>=0})()") boolValue]);
            assert([evaluate(view,@"document.querySelector('.action-countdown').textContent==='Auto updating in 5 seconds.' && document.querySelector('.action-auto-countdown').checked") boolValue]);
            evaluate(view,@"advance(3000);render.escape();advance(10000)");
            assert([evaluate(view,@"closed.length===0 && confirmed.length===0 && document.querySelector('.action-countdown').textContent==='Auto updating stopped.' && !document.querySelector('.action-auto-countdown').checked") boolValue]);
            evaluate(view,@"document.querySelector('.action-countdown').click();advance(4999)");
            assert([evaluate(view,@"confirmed.length===0") boolValue]);
            evaluate(view,@"advance(1)");
            assert([evaluate(view,@"confirmed.length===1 && confirmed[0][0]==='action-confirm' && document.querySelector('.suggestion-primary').disabled") boolValue]);
            evaluate(view,@"render({suggestedSite:'c1',actionGeneration:7,sites:[site],action:{id:confirmed[0][1],siteID:'c1',kind:'update',status:'running'}})");
            assert([evaluate(view,@"document.querySelector('.confirmation-title').textContent==='Updating branch…' && document.querySelector('.action-countdown').hidden && document.querySelector('.suggestion-plan').textContent.includes('applying the update') && document.body.scrollHeight<=innerHeight+1") boolValue]);
            evaluate(view,@"complete('update',confirmed[0][1]);advance(4999)");
            assert([evaluate(view,@"document.querySelector('.confirmation-title').textContent==='Branch updated' && document.querySelector('.suggestion-preview').hidden") boolValue]);
            assert([evaluate(view,@"closed.length===0 && document.querySelector('.action-countdown').textContent==='Auto closing in 1 second.'") boolValue]);
            evaluate(view,@"advance(1)");
            assert([evaluate(view,@"closed.length===1 && closed[0].generation===7") boolValue]);
        }
        puts("Action dialog WebKit layout and countdown checks passed");
    }
    return 0;
}
