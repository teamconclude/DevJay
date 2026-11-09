#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <assert.h>
#include <stdio.h>

static BOOL loaded;
@interface PRReviewDelegate : NSObject <WKNavigationDelegate>
@end
@implementation PRReviewDelegate
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
    __block BOOL done = NO; __block id value;
    [view evaluateJavaScript:script completionHandler:^(id result, NSError *error) {
        if (error) NSLog(@"%@", error);
        assert(!error); value = [result retain]; done = YES;
    }];
    spin(&done); return [value autorelease];
}
static void check(WKWebView *view, NSString *script) {
    if (![evaluate(view,script) boolValue]) {
        NSLog(@"PR review check failed: %@",script);
        NSLog(@"Layout: %@",evaluate(view,@"JSON.stringify({width:innerWidth,height:innerHeight,theme:document.documentElement.dataset.theme,files:document.querySelectorAll('.br-file').length,split:!!document.querySelector('.br-split'),footer:control('Finish PR review').getBoundingClientRect().toJSON(),panel:document.querySelector('.pr-submit-panel').getBoundingClientRect().toJSON()})"));
        abort();
    }
}
int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication]; [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        NSMutableString *css=[NSMutableString string], *js=[NSMutableString string];
        for (NSString *name in @[@"style",@"branch-review",@"pr-review"]) {
            NSString *source=[NSString stringWithContentsOfFile:[NSString stringWithFormat:@"cmd/devjay/frontend/%@.css",name] encoding:NSUTF8StringEncoding error:nil];assert(source);[css appendString:source];
        }
        NSRegularExpression *imports=[NSRegularExpression regularExpressionWithPattern:@"^import .*;\\n" options:NSRegularExpressionAnchorsMatchLines error:nil];
        for (NSString *name in @[@"syntax-highlight",@"diff-search",@"file-ignore",@"view-preferences",@"branch-review",@"pr-review"]) {
            NSString *source=[NSString stringWithContentsOfFile:[NSString stringWithFormat:@"cmd/devjay/frontend/%@.js",name] encoding:NSUTF8StringEncoding error:nil];assert(source);
            source=[imports stringByReplacingMatchesInString:source options:0 range:NSMakeRange(0,source.length) withTemplate:@""];
            [js appendFormat:@"\n%@\n",[source stringByReplacingOccurrencesOfString:@"export " withString:@""]];
        }
        [js appendString:@"\nconst saved=new Map();Object.defineProperty(window,'localStorage',{value:{getItem:k=>saved.get(k)||null,setItem:(k,v)=>saved.set(k,v)}});"
         "const requests=[];let pr,pending=null;const request=r=>{requests.push(r);if(r.operation==='pr-review-anchor')pr.receive({loadID:1,anchor:{id:r.prAnchor.id,location:{path:r.prAnchor.path,side:r.prAnchor.side,line:r.prAnchor.line,summary:r.prAnchor.line===1}}});"
         "if(r.operation==='pr-review-comment'){const c=r.prComment;pending||={id:70,comments:[],version:'1'};pending={...pending,version:String(Number(pending.version)+1)};if(c.action==='delete')pending.comments=pending.comments.filter(n=>n.id!==c.commentID);else pending.comments.push({...c.comment,id:100,requestID:c.requestID});pr.receive({loadID:1,pending,comment:{requestID:c.requestID,done:true}});}return true;};"
         "const root=document.querySelector('#app');const review=createBranchReview(root,request,{commentLine:(node,info)=>pr.decorateGutter(node,info),beforeRender:card=>pr.beforeRender(card),afterRender:()=>pr.afterRender()});pr=createPRReview(root,request);"
         "const file={path:'app.go',oldText:'before\\nold\\nafter\\n',newText:'before\\nnew\\nafter\\n',hunks:[{oldStart:2,oldLines:1,newStart:2,newLines:1}],commits:[]};"
         "const view={kind:'review',version:1,siteID:'fixture',path:'/repo',branch:'feature',head:'head',captured:'snapshot',file:0,files:[{path:'app.go',status:'M',added:1,removed:1,statsKnown:true}],reviewPRs:[{number:12}],review:{baseRef:'main',base:'base',refs:['main'],selected:file}};pr.sync(view);review.render(view);"
         "const snapshot={token:'token',repo:'team/project',number:12,url:'https://github.com/team/project/pull/12',head:'aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa',base:'bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb',author:'author',viewer:'reviewer',files:Array.from({length:114},(_,i)=>({path:'unrelated-'+i+'.go'}))};pr.receive({loadID:1,snapshot});"
         "const control=label=>[...document.querySelectorAll('[aria-label]')].find(n=>n.getAttribute('aria-label')===label);"
         "const visible=n=>{const r=n.getBoundingClientRect();return r.width>0&&r.height>0&&r.left>=0&&r.right<=innerWidth+1&&r.top>=0&&r.bottom<=innerHeight+1};void 0;"];
        NSString *html=[NSString stringWithFormat:@"<!doctype html><meta charset='utf-8'><style>%@</style><body class='branch-review'><main id='app'></main><script>%@</script></body>",css,js];
        NSWindow *window=[[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,1100,740) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration=[[[WKWebViewConfiguration alloc] init] autorelease];configuration.websiteDataStore=WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *web=[[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,1100,740) configuration:configuration] autorelease];
        PRReviewDelegate *delegate=[[[PRReviewDelegate alloc] init] autorelease];web.navigationDelegate=delegate;window.contentView=web;[web loadHTMLString:html baseURL:nil];spin(&loaded);
        for(NSString *theme in @[@"dark",@"light"]) {
            evaluate(web,[NSString stringWithFormat:@"document.documentElement.dataset.theme='%@';void 0",theme]);
            for(NSNumber *width in @[@1100,@720]) {
                [window setContentSize:NSMakeSize(width.doubleValue,width.intValue==720?500:740)];
                check(web,@"document.querySelectorAll('.br-file').length===1&&!document.querySelector('.pr-files')&&document.querySelector('.br-split')&&visible(control('Finish PR review'))");
                evaluate(web,@"control('Add PR comment on app.go, new line 2').click();document.querySelector('.pr-comment-text').value='Inline feedback';document.querySelector('.pr-comment-text').dispatchEvent(new Event('input'));void 0");
                check(web,@"visible(document.querySelector('.pr-composer'))&&document.querySelector('.pr-composer').previousElementSibling.classList.contains('br-row')&&document.activeElement===document.querySelector('.pr-comment-text')");
                evaluate(web,@"review.render({...view,version:2});void 0");
                check(web,@"document.activeElement===document.querySelector('.pr-comment-text')&&document.querySelector('.pr-comment-text').value==='Inline feedback'");
                evaluate(web,@"control('Save inline comment to draft review').click();control('Finish PR review').click();void 0");
                check(web,@"visible(document.querySelector('.pr-submit-panel'))&&document.querySelector('.pr-inline-draft').textContent.includes('Inline feedback')&&document.querySelectorAll('.br-file').length===1");
                check(web,@"[...document.querySelectorAll('.pr-draft')].every(n=>getComputedStyle(n).borderRadius==='10px'&&getComputedStyle(n).backgroundColor!==getComputedStyle(document.querySelector('.br-file-card')).backgroundColor&&n.querySelector('.pr-comment-header').textContent==='app.goNew · 2'&&n.querySelector('.pr-comment-content').textContent==='Inline feedback'&&n.querySelector('.pr-comment-actions').textContent.includes('EditDelete'))");
                if (getenv("DEVJAY_REVIEW_SNAPSHOT")) {
                    __block BOOL captured=NO;
                    NSString *path=[NSString stringWithFormat:@"%s-%@-%@.png",getenv("DEVJAY_REVIEW_SNAPSHOT"),theme,width];
                    [web takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error) {
                        assert(!error && image);NSBitmapImageRep *bitmap=[NSBitmapImageRep imageRepWithData:image.TIFFRepresentation];
                        assert([[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:path atomically:YES]);captured=YES;
                    }];spin(&captured);
                }
                evaluate(web,@"control('Close review summary').click();control('Delete draft comment on app.go · new 2').click();void 0");
                evaluate(web,@"control('Close review summary').click();void 0");
            }
        }
        evaluate(web,@"control('Unified').click();control('Add PR comment on app.go, original line 2').click();void 0");
        check(web,@"document.querySelector('.br-unified')&&document.querySelector('.pr-composer').textContent.includes('Original · 2')&&visible(document.querySelector('.pr-composer'))");
        evaluate(web,@"control('Cancel inline comment').click();control('Add PR comment on app.go, new line 1').click();const comment=document.querySelector('.pr-comment-text');comment.value='Check this caller';comment.dispatchEvent(new Event('input'));control('Add to review summary').click();void 0");
        check(web,@"visible(document.querySelector('.pr-submit-panel'))&&document.querySelector('.pr-summary').value.includes('app.go:1 (working copy, local line numbers)')&&document.activeElement===document.querySelector('.pr-summary')");
        evaluate(web,@"control('Submit review to GitHub').click();void 0");
        check(web,@"requests.at(-1).operation==='pr-review-submit'&&requests.at(-1).prSubmission.body.endsWith('Check this caller')&&control('Submit review to GitHub').disabled");
        puts("Integrated PR comment WebKit layout and interaction checks passed");
    }
    return 0;
}
