#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <assert.h>
#include <stdio.h>

static BOOL loaded;
@interface CardSizingDelegate : NSObject <WKNavigationDelegate>
@end
@implementation CardSizingDelegate
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

static void assertReadable(WKWebView *view, BOOL fitsWindow) {
    // Checking only the list's scrollHeight missed internally clipped cards.
    assert([evaluate(view, @"[...list.children].every(c => c.scrollHeight <= c.clientHeight + 1)") boolValue]);
    if (fitsWindow) assert([evaluate(view, @"list.scrollHeight <= list.clientHeight + 1") boolValue]);
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        NSString *css = [NSString stringWithContentsOfFile:@"cmd/devjay/frontend/style.css" encoding:NSUTF8StringEncoding error:nil];
        NSString *sizing = [NSString stringWithContentsOfFile:@"cmd/devjay/frontend/card-sizing.js" encoding:NSUTF8StringEncoding error:nil];
        assert(css && sizing);
        sizing = [sizing stringByReplacingOccurrencesOfString:@"export " withString:@""];
        NSString *html = [NSString stringWithFormat:@"<!doctype html><style>%@</style>"
            "<body class='strip' data-decoration='none' data-layout='horizontal'><main id='app'>"
            "<header><span class='grip'></span><button class='close-window'>×</button></header>"
            "<nav class='cards'></nav><footer class='notice'></footer></main><script>%@</script></body>", css, sizing];
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,920,125)
            styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration = [[[WKWebViewConfiguration alloc] init] autorelease];
        configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *view = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,920,125) configuration:configuration] autorelease];
        CardSizingDelegate *delegate = [[[CardSizingDelegate alloc] init] autorelease];
        view.navigationDelegate = delegate;
        window.contentView = view;
        [view loadHTMLString:html baseURL:nil];
        spin(&loaded);
        // Pump animation frames explicitly: this fixture never displays a window.
        evaluate(view, @"var frames=[],requests=[],list=document.querySelector('.cards');"
            "window.requestAnimationFrame=fn=>{frames.push(fn);return frames.length;};"
            "var schedule=installCardSizing(document.querySelector('#app'),list,size=>requests.push(size));"
            "function measure(){schedule();while(frames.length)frames.shift()();}"
            "var shortCard=`<div class='card-heading'><button class='card-title'><div class='path'>~/Project</div>"
            "<div class='branch'>main</div></button></div><div class='card-status'><div class='status-line'>Checking repository…</div></div>`;"
            "list.innerHTML=Array.from({length:4},()=>`<div class='card'>${shortCard}</div>`).join('');measure();");
        CGFloat initialHeight = [evaluate(view, @"requests.at(-1).height") doubleValue];
        [window setContentSize:NSMakeSize(920, initialHeight)];
        evaluate(view, @"measure(); requests=[];"
            "var richCard=`<div class='card-heading'><button class='card-title'><div class='path'>~/Project</div>"
            "<div class='branch'>feature/card-sizing</div></button><div class='card-prs'><button class='pr-tag'>#123</button></div></div>"
            "<div class='card-status'><div class='metric-row'><span>Edits</span><button class='metric'>3 changes</button></div>"
            "<div class='status-line'><span>↑</span><span>2 commits not pushed</span></div>"
            "<div class='status-line'><span>↓</span><span>4 newer commits on main</span></div>"
            "<div class='status-line good'><span>✓</span><span>Review approved</span></div></div>"
            "<div class='development-app'><button class='development-app-group has-agents'>"
            "<span class='development-app-icon'><span class='development-app-initials'>GL</span></span><span>Claude Code</span></button>"
            "<button class='development-app-group has-agents'><span class='development-app-icon'>T</span><span>Terminal agent</span></button></div>`;"
            "list.querySelectorAll('.card').forEach(c=>c.innerHTML=richCard);measure();");
        assert([evaluate(view, @"requests.length === 1 && !requests[0].refit") boolValue]);
        CGFloat fullHeight = [evaluate(view, @"requests[0].height") doubleValue];
        assert(fullHeight > initialHeight);
        assertReadable(view, NO); // While growing, contents stay in the outer scroll area.
        evaluate(view, @"measure()");
        assert([evaluate(view, @"requests.length === 1") boolValue]);
        [window setContentSize:NSMakeSize(920, fullHeight)];
        evaluate(view, @"measure()");
        assertReadable(view, YES);
        evaluate(view, @"list.querySelectorAll('.card').forEach(c=>c.innerHTML=shortCard);measure()");
        assert([evaluate(view, @"requests.length === 1") boolValue]);
        [window setContentSize:NSMakeSize(920, 400)];
        evaluate(view, @"measure()");
        assert([evaluate(view, @"requests.length === 1") boolValue]); // Manual extra height survives.

        evaluate(view, @"list.querySelectorAll('.card').forEach(c=>c.innerHTML=richCard)");
        for (NSString *layout in @[@"horizontal", @"vertical", @"custom"]) {
            CGFloat width = [layout isEqual:@"horizontal"] ? 920 : [layout isEqual:@"vertical"] ? 234 : 470;
            CGFloat minimalHeight = 0;
            for (NSString *decoration in @[@"minimal", @"none", @"normal"]) {
                [window setContentSize:NSMakeSize(width,125)];
                evaluate(view, [NSString stringWithFormat:@"document.body.dataset.layout='%@';document.body.dataset.decoration='%@';requests=[];measure()",layout,decoration]);
                assert([evaluate(view, @"requests.length === 1 && requests[0].refit") boolValue]);
                CGFloat height = [evaluate(view, @"requests[0].height") doubleValue];
                if ([decoration isEqual:@"minimal"]) minimalHeight = height;
                else assert(fabs(minimalHeight - height - 22) <= 1);
                assertReadable(view, NO);
                [window setContentSize:NSMakeSize(width,height)];
                evaluate(view, @"measure()");
                assertReadable(view, YES);
                assert([evaluate(view, @"requests.length === 1") boolValue]);
            }
        }
        puts("card sizing WebKit regressions passed");
    }
    return 0;
}
