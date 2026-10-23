#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <assert.h>
#include <stdio.h>

static BOOL loaded;
@interface AuthorLayoutDelegate : NSObject <WKNavigationDelegate>
@end
@implementation AuthorLayoutDelegate
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
        NSString *css=[NSString stringWithContentsOfFile:@"cmd/devjay/frontend/style.css" encoding:NSUTF8StringEncoding error:nil];
        NSRegularExpression *imports=[NSRegularExpression regularExpressionWithPattern:@"^import .*;\n" options:NSRegularExpressionAnchorsMatchLines error:nil];
        for(NSString *search in @[@"",@"?site=a"]) {
            NSMutableString *js=[NSMutableString stringWithString:@"const Events={On:()=>{},Emit:()=>Promise.resolve()},Window={};const installCardHover=()=>{},installDragOriginGuard=()=>{},uiReady=()=>()=>{},installPathLabels=()=>{},installStripDrag=()=>{},installCardSizing=()=>()=>{};"];
            for(NSString *name in @[@"notice",@"bridge",@"pr-status",@"card-status",@"card-interaction",@"card-menu",@"updater",@"app"]) {
                NSString *code=[NSString stringWithContentsOfFile:[NSString stringWithFormat:@"cmd/devjay/frontend/%@.js",name] encoding:NSUTF8StringEncoding error:nil];assert(code);
                code=[imports stringByReplacingMatchesInString:code options:0 range:NSMakeRange(0,code.length) withTemplate:@""];
                code=[code stringByReplacingOccurrencesOfString:@"export " withString:@""];
                code=[code stringByReplacingOccurrencesOfString:@"new URLSearchParams(location.search)" withString:[NSString stringWithFormat:@"new URLSearchParams('%@')",search]];
                [js appendFormat:@"\n%@\n",code];
            }
            NSString *html=[NSString stringWithFormat:@"<!doctype html><meta charset='utf-8'><style>%@</style><body><main id='app'></main><script>%@</script></body>",css,js];
            NSWindow *window=[[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,600,260) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
            WKWebViewConfiguration *configuration=[[[WKWebViewConfiguration alloc] init] autorelease];configuration.websiteDataStore=WKWebsiteDataStore.nonPersistentDataStore;
            WKWebView *view=[[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,600,260) configuration:configuration] autorelease];
            AuthorLayoutDelegate *delegate=[[[AuthorLayoutDelegate alloc] init] autorelease];view.navigationDelegate=delegate;window.contentView=view;
            loaded=NO;[view loadHTMLString:html baseURL:nil];spin(&loaded);
            evaluate(view,@"var item={id:'a',path:'/tmp/c1',prefix:'~/',name:'c1',position:{label:'Desktop 5'},window:{},developmentAppsKnown:true,developmentApps:[{name:'GoLand',bundleID:'goland',agents:['Codex','Claude Code']}],git:{branch:'lars/customer-api-plan',summary:'Up to date',pullRequests:[{number:2865,state:'Approved',ciState:'success',otherAuthor:{login:'laknoll'}}]}};var fixture={sites:[item],preferences:{showIDEStatus:true},layout:'horizontal'};render(fixture)");
            // Measure actual card renderer and CSS in both layouts. One fixed-size
            // avatar shares the existing app row and must not change its height.
            for(NSString *layout in @[@"horizontal",@"vertical",@"custom"]) {
                evaluate(view,[NSString stringWithFormat:@"fixture.layout='%@';render(fixture)",layout]);
                assert([evaluate(view,@"(()=>{const a=document.querySelector('.pr-author').getBoundingClientRect(),f=document.querySelector('.card-footer').getBoundingClientRect(),apps=document.querySelector('.development-app').getBoundingClientRect();return a.width===24&&a.height===24&&a.right<=f.right+.5&&Math.abs(a.right-f.right)<1&&a.left>=apps.right+7&&a.bottom<=f.bottom+.5;})()") boolValue]);
                evaluate(view,@"var before=document.querySelector('.card-footer').getBoundingClientRect().height;delete item.git.pullRequests[0].otherAuthor;render(fixture)");
                assert([evaluate(view,@"document.querySelector('.card-footer').getBoundingClientRect().height===before") boolValue]);
                evaluate(view,@"item.git.pullRequests[0].otherAuthor={login:'laknoll'};render(fixture)");
            }
            if ([search isEqualToString:@""]) {
                [window setContentSize:NSMakeSize(234,400)];
                for (NSString *state in @[@"available",@"downloading",@"ready",@"installing",@"error"]) {
                    evaluate(view,[NSString stringWithFormat:@"render({...fixture,update:{state:'%@',version:'0.8.1',percent:42}})",state]);
                    assert([evaluate(view,@"(()=>{const r=document.querySelector('.app-update button').getBoundingClientRect();return r.left>=0&&r.right<=innerWidth+1&&r.bottom<=innerHeight+1})()") boolValue]);
                }
                [window setContentSize:NSMakeSize(600,260)];
                evaluate(view,@"render(fixture)");
            }
            // Slow image success/failure cannot resize the footer or card.
            evaluate(view,@"var footerHeight=document.querySelector('.card-footer').getBoundingClientRect().height;item.git.pullRequests[0].otherAuthor.avatarURL='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aP9sAAAAASUVORK5CYII=';render(fixture);document.querySelector('.pr-author img').onload()");
            assert([evaluate(view,@"document.querySelector('.card-footer').getBoundingClientRect().height===footerHeight") boolValue]);
            evaluate(view,@"document.querySelector('.pr-author img').onerror()");
            assert([evaluate(view,@"document.querySelector('.card-footer').getBoundingClientRect().height===footerHeight&&!document.querySelector('.pr-author .development-app-initials').hidden") boolValue]);
            // Both rows wrap within their own bounds with many apps/authors.
            evaluate(view,@"item.developmentApps.push({name:'iTerm',bundleID:'iterm',agents:['Codex']});item.git.pullRequests=Array.from({length:8},(_,i)=>({number:2865+i,otherAuthor:{login:'author'+i}}));render(fixture)");
            assert([evaluate(view,@"(()=>{const f=document.querySelector('.card-footer').getBoundingClientRect(),apps=document.querySelector('.development-app').getBoundingClientRect();return [...document.querySelectorAll('.pr-author')].every(n=>{const r=n.getBoundingClientRect();return r.right<=f.right+1&&r.left>=apps.right+7&&r.bottom<=f.bottom+1;});})()") boolValue]);
            evaluate(view,@"fixture.preferences.showIDEStatus=false;item.git.pullRequests=item.git.pullRequests.slice(0,1);render(fixture)");
            assert([evaluate(view,@"!document.querySelector('.development-app')&&Math.abs(document.querySelector('.pr-author').getBoundingClientRect().right-document.querySelector('.card-footer').getBoundingClientRect().right)<1") boolValue]);
            NSString *output=NSProcessInfo.processInfo.environment[@"DEVJAY_AUTHOR_SNAPSHOT"];
            if(output && search.length==0) {
                evaluate(view,@"fixture.preferences.showIDEStatus=true;fixture.layout='horizontal';item.developmentApps=item.developmentApps.slice(0,1);item.git.pullRequests[0].otherAuthor.login='laknoll';render(fixture)");
                __block BOOL done=NO;
                [view takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){
                    assert(image&&!error);NSBitmapImageRep *bitmap=[NSBitmapImageRep imageRepWithData:image.TIFFRepresentation];
                    assert([[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:output atomically:YES]);done=YES;
                }];spin(&done);
            }
        }
        puts("PR author WebKit layout checks passed");
    }
    return 0;
}
