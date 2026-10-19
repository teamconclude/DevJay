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

static CGFloat settleHeight(WKWebView *view, NSWindow *window, CGFloat width) {
    // WebKit can finish a width/layout change after the first measurement.
    // Apply each requested size and prove that follow-up fits converge.
    for (int attempt = 0; attempt < 4; attempt++) {
        NSUInteger count = [evaluate(view, @"requests.length") unsignedIntegerValue];
        CGFloat height = [evaluate(view, @"requests.at(-1).height") doubleValue];
        [window setContentSize:NSMakeSize(width,height)];
        evaluate(view, @"measure()");
        if ([evaluate(view, @"requests.length") unsignedIntegerValue] == count) {
            assertReadable(view, YES);
            return height;
        }
    }
    NSLog(@"Unstable card fitting: %@", evaluate(view, @"JSON.stringify(requests)"));
    assert(NO);
    return 0;
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
        evaluate(view, @"var frames=[],requests=[],now=0,nextTimer=1,timers=new Map(),list=document.querySelector('.cards');"
            "window.requestAnimationFrame=fn=>{frames.push(fn);return frames.length;};"
            "performance.now=()=>now;"
            "window.setTimeout=(fn,delay)=>{let id=nextTimer++;timers.set(id,{fn,at:now+delay});return id;};"
            "window.clearTimeout=id=>timers.delete(id);"
            "var schedule=installCardSizing(document.querySelector('#app'),list,size=>requests.push(size));"
            "function measureNow(){schedule();while(frames.length)frames.shift()();}"
            "function advance(ms){let end=now+ms;while(true){let next=[...timers].sort((a,b)=>a[1].at-b[1].at)[0];"
            "if(!next||next[1].at>end)break;let [id,t]=next;now=t.at;timers.delete(id);t.fn();"
            "while(frames.length)frames.shift()();}now=end;}"
            "function measure(){measureNow();advance(3000);}"
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
            "list.querySelectorAll('.card').forEach(c=>c.innerHTML=richCard);measureNow();");
        assert([evaluate(view, @"requests.length === 0") boolValue]);
        assertReadable(view, NO);
        evaluate(view, @"advance(150);list.querySelectorAll('.card').forEach(c=>c.innerHTML=shortCard);measureNow();advance(3000)");
        assert([evaluate(view, @"requests.length === 0") boolValue]);
        evaluate(view, @"list.querySelectorAll('.card').forEach(c=>c.innerHTML=richCard);measureNow();advance(299)");
        assert([evaluate(view, @"requests.length === 0") boolValue]);
        evaluate(view, @"advance(1)");
        assert([evaluate(view, @"requests.length === 1 && requests[0].refit") boolValue]);
        assert([evaluate(view, @"requests[0].height === Math.max(120, fittedPanelHeight(list)) + 12") boolValue]);
        CGFloat fullHeight = [evaluate(view, @"requests[0].height") doubleValue];
        assert(fullHeight > initialHeight);
        assertReadable(view, NO); // While growing, contents stay in the outer scroll area.
        evaluate(view, @"measure()");
        assert([evaluate(view, @"requests.length === 1") boolValue]);
        [window setContentSize:NSMakeSize(920, fullHeight)];
        evaluate(view, @"measure()");
        assertReadable(view, YES);
        evaluate(view, @"[...list.children].slice(0,-1).forEach(c=>c.innerHTML=shortCard);measure()");
        assert([evaluate(view, @"requests.length === 1") boolValue]);
        // Keep the tallest remaining card readable, then shrink when its rows disappear.
        evaluate(view, @"list.lastElementChild.innerHTML=shortCard;measureNow();advance(2999)");
        assert([evaluate(view, @"requests.length === 1") boolValue]);
        evaluate(view, @"advance(1)");
        assert([evaluate(view, @"requests.length === 2 && requests.at(-1).refit") boolValue]);
        assert([evaluate(view, @"requests.at(-1).height") doubleValue] == initialHeight);
        [window setContentSize:NSMakeSize(920, initialHeight)];
        evaluate(view, @"measure()");
        assertReadable(view, YES);
        assert([evaluate(view, @"requests.length === 2") boolValue]);
        [window setContentSize:NSMakeSize(920, 400)];
        evaluate(view, @"measure()");
        assert([evaluate(view, @"requests.length === 3") boolValue]);
        assert([evaluate(view, @"requests.at(-1).height") doubleValue] == initialHeight);

        evaluate(view, @"list.querySelectorAll('.card').forEach(c=>c.innerHTML=richCard)");
        for (NSString *layout in @[@"horizontal", @"vertical", @"custom"]) {
            CGFloat width = [layout isEqual:@"horizontal"] ? 920 : [layout isEqual:@"vertical"] ? 234 : 470;
            CGFloat minimalHeight = 0;
            for (NSString *decoration in @[@"minimal", @"none", @"normal"]) {
                [window setContentSize:NSMakeSize(width,125)];
                evaluate(view, [NSString stringWithFormat:@"document.body.dataset.layout='%@';document.body.dataset.decoration='%@';requests=[]",layout,decoration]);
                // Let WebKit apply the new viewport/layout before pumping the
                // animation frame, as the production ResizeObserver does.
                evaluate(view, @"measure()");
                assert([evaluate(view, @"requests.length === 1 && requests[0].refit") boolValue]);
                assertReadable(view, NO);
                CGFloat height = settleHeight(view, window, width);
                if ([decoration isEqual:@"minimal"]) minimalHeight = height;
                else assert(fabs(minimalHeight - height - 22) <= 1);
                evaluate(view, @"requests=[];list.querySelectorAll('.card').forEach(c=>c.innerHTML=shortCard);measure()");
                assert([evaluate(view, @"requests.length === 1 && requests.at(-1).refit") boolValue]);
                CGFloat shortHeight = settleHeight(view, window, width);
                assert(shortHeight < height && shortHeight >= 132);
                evaluate(view, @"requests=[];list.querySelectorAll('.card').forEach(c=>c.innerHTML=richCard);measure()");
                assert([evaluate(view, @"requests.length === 1 && requests.at(-1).refit") boolValue]);
                assert(settleHeight(view, window, width) == height);
            }
        }
        puts("card sizing WebKit regressions passed");
    }
    return 0;
}
