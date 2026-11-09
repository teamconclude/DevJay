#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <assert.h>
#include <stdio.h>

static BOOL loaded;
@interface BranchReviewLayoutDelegate : NSObject <WKNavigationDelegate>
@end
@implementation BranchReviewLayoutDelegate
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
static void check(WKWebView *view, NSString *script) {
    if (![evaluate(view, script) boolValue]) {
        NSLog(@"Branch review check failed: %@", script);
        abort();
    }
}
static void waitForCheck(WKWebView *view, NSString *script) {
    // Hidden WebKit windows pause animation frames. Flush the fixture's
    // frame queue explicitly while retaining real DOM scroll/layout behavior.
    evaluate(view,@"flushReviewFrames();void 0");
    NSDate *deadline = [NSDate dateWithTimeIntervalSinceNow:3];
    while (![evaluate(view, script) boolValue] && deadline.timeIntervalSinceNow > 0)
        [NSRunLoop.currentRunLoop runUntilDate:[NSDate dateWithTimeIntervalSinceNow:.02]];
    if (![evaluate(view, script) boolValue]) NSLog(@"Scroll diagnostics: %@",evaluate(view,@"JSON.stringify({position:filePosition(),path:selectedPath(),top:scrollArea.scrollTop,height:scrollArea.clientHeight,total:scrollArea.scrollHeight,rect:scrollArea.getBoundingClientRect(),cards:[...document.querySelectorAll('.br-file-card')].map(n=>({path:n.dataset.path,rect:n.getBoundingClientRect()}))})"));
    check(view, script);
}

int main(void) {
    @autoreleasepool {
        [NSApplication sharedApplication];
        [NSApp setActivationPolicy:NSApplicationActivationPolicyProhibited];
        NSMutableString *css = [NSMutableString string];
        for (NSString *name in @[@"style", @"branch-review"]) {
            NSString *source = [NSString stringWithContentsOfFile:[NSString stringWithFormat:@"cmd/devjay/frontend/%@.css", name] encoding:NSUTF8StringEncoding error:nil];
            assert(source); [css appendString:source];
        }
        NSMutableString *js = [NSMutableString string];
        NSRegularExpression *imports=[NSRegularExpression regularExpressionWithPattern:@"^import .*;\\n" options:NSRegularExpressionAnchorsMatchLines error:nil];
        for (NSString *name in @[@"syntax-highlight",@"diff-search",@"file-ignore",@"view-preferences",@"file-editor",@"git-viewer",@"branch-review"]) {
            NSString *source=[NSString stringWithContentsOfFile:[NSString stringWithFormat:@"cmd/devjay/frontend/%@.js",name] encoding:NSUTF8StringEncoding error:nil];assert(source);
            source=[imports stringByReplacingMatchesInString:source options:0 range:NSMakeRange(0,source.length) withTemplate:@""];
            [js appendFormat:@"\n%@\n",[source stringByReplacingOccurrencesOfString:@"export " withString:@""]];
        }
        [js appendString:@"\nconst reviewFrames=new Map();let reviewFrameID=0;window.requestAnimationFrame=fn=>{reviewFrames.set(++reviewFrameID,fn);return reviewFrameID;};window.cancelAnimationFrame=id=>reviewFrames.delete(id);const flushReviewFrames=()=>{const pending=[...reviewFrames.values()];reviewFrames.clear();for(const fn of pending)fn();};\nconst requests=[];const review=createBranchReview(document.querySelector('#app'),payload=>{requests.push(payload);return Promise.resolve()});\n"
         "const before=Array.from({length:55},(_,i)=>i===20?'  return oldValue;':i===40?'  // previous behavior':`  // context line ${i+1}`);const after=[...before];after[20]='  return currentValue;';after[40]='  // branch-wide review';\n"
         "const commit={id:'1234567890abcdef1234567890abcdef12345678',author:'Alex Developer',date:'2026-11-01T09:00:00+01:00',subject:'Add branch review navigation'};\n"
         "const fixture={kind:'review',siteID:'fixture',name:'DevJay',branch:'feature/branch-review',head:'abcdef0123456789',version:1,captured:'fixture-1',file:0,files:[{path:'cmd/devjay/review.go',status:'M',group:'Branch',added:2,removed:2,statsKnown:true},{path:'cmd/devjay/frontend/review.js',status:'A',group:'Branch',added:12,removed:0,statsKnown:true}],review:{baseRef:'origin/main',base:'567890abcdef',refs:['main','origin/main'],ignoreWhitespace:false,selected:{path:'cmd/devjay/review.go',oldText:before.join('\\n')+'\\n',newText:after.join('\\n')+'\\n',diff:'@@ -21 +21 @@\\n-  return oldValue;\\n+  return currentValue;\\n@@ -41 +41 @@\\n-  // previous behavior\\n+  // branch-wide review\\n',editable:true,token:'fixture-token',hunks:[{oldStart:21,oldLines:1,newStart:21,newLines:1,commits:[commit],uncommitted:false},{oldStart:41,oldLines:1,newStart:41,newLines:1,commits:[],uncommitted:true}],commits:[commit]}}};review.render(fixture);\n"
         "const button=text=>[...document.querySelectorAll('button')].find(b=>b.textContent.trim()===text);\n"];
        NSString *html = [NSString stringWithFormat:@"<!doctype html><meta charset='utf-8'><style>%@</style><body class='branch-review'><main id='app'></main><script>%@</script></body>", css, js];
        NSWindow *window = [[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,1280,800) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration = [[[WKWebViewConfiguration alloc] init] autorelease];
        configuration.websiteDataStore = WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *view = [[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,1280,800) configuration:configuration] autorelease];
        BranchReviewLayoutDelegate *delegate = [[[BranchReviewLayoutDelegate alloc] init] autorelease];
        view.navigationDelegate=delegate; window.contentView=view;
        [view loadHTMLString:html baseURL:nil]; spin(&loaded);
        // Real WebKit styles: syntax must remain legible on both diff fills.
        evaluate(view,@"document.documentElement.dataset.theme='light';"
            "const luminance=color=>{const c=color.match(/[\\d.]+/g).slice(0,3).map(n=>{n=Number(n)/255;return n<=.04045?n/12.92:((n+.055)/1.055)**2.4});return c[0]*.2126+c[1]*.7152+c[2]*.0722;};"
            "const contrast=(a,b)=>{a=luminance(a);b=luminance(b);return (Math.max(a,b)+.05)/(Math.min(a,b)+.05);};"
            "const sample=document.createElement('div');sample.className='br-code';document.body.append(sample);"
            "window.appearanceContrasts=[];for(const fill of ['br-add','br-delete']){sample.className='br-code '+fill;for(const syntax of ['syntax-comment','syntax-keyword','syntax-string','syntax-number','syntax-function','syntax-tag']){sample.innerHTML='<span class='+syntax+'>Sample</span>';appearanceContrasts.push(contrast(getComputedStyle(sample.firstChild).color,getComputedStyle(sample).backgroundColor));}}sample.remove();");
        check(view,@"appearanceContrasts.every(value=>value>=4.5)");
        check(view,@"getComputedStyle(document.body).backgroundColor==='rgb(255, 255, 255)' && getComputedStyle(document.body).colorScheme==='light'");
        evaluate(view,@"document.documentElement.dataset.theme='dark'");
        check(view,@"getComputedStyle(document.body).backgroundColor==='rgb(30, 34, 42)' && getComputedStyle(document.body).colorScheme==='dark'");

        for (NSNumber *width in @[@1280,@900,@720]) {
            [window setContentSize:NSMakeSize(width.doubleValue,width.intValue == 720 ? 460 : 700)];
            check(view, @"document.body.scrollWidth <= innerWidth+1 && document.body.scrollHeight <= innerHeight+1");
            check(view, @"!!(document.querySelector('.br-files') && document.querySelector('.br-commit-panel') && document.querySelector('.br-code'))");
            check(view, @"(()=>{const sidebar=document.querySelector('.br-sidebar').getBoundingClientRect(),content=document.querySelector('.br-content').getBoundingClientRect();return sidebar.right<=content.left+1&&content.right<=innerWidth+1&&content.bottom<=innerHeight+1})()");
            check(view, @"[...document.querySelectorAll('.br-toolbar button,.br-toolbar select')].filter(n=>n.getBoundingClientRect().width).every(n=>{const r=n.getBoundingClientRect();return r.left>=0&&r.right<=innerWidth+1})");
        }
        [window setContentSize:NSMakeSize(1280,800)];
        check(view, @"document.querySelectorAll('.br-code .br-row').length===55 && document.querySelector('.br-commit-panel').textContent.includes('Alex Developer')");
        check(view, @"document.querySelector('.br-code .syntax-keyword').textContent==='return' && !!document.querySelector('.br-code .syntax-comment')");
        check(view, @"getComputedStyle(document.querySelector('.br-code .syntax-keyword')).color!==getComputedStyle(document.querySelector('.br-code')).color && getComputedStyle(document.querySelector('.br-add')).backgroundColor!==getComputedStyle(document.querySelector('.br-delete')).backgroundColor");
        // A newly opened macOS web view can focus BODY rather than a review
        // descendant. Dispatch real bubbling DOM events along that path.
        evaluate(view, @"var optionKey=(target,key,extra={})=>{const event=new KeyboardEvent('keydown',{key,altKey:true,bubbles:true,cancelable:true,...extra});target.dispatchEvent(event);return event.defaultPrevented};document.activeElement.blur();void 0");
        check(view, @"document.activeElement===document.body && optionKey(document.body,'ArrowDown') && document.querySelector('.br-change-position').textContent==='2 / 2' && document.querySelectorAll('.br-nav-group')[1].textContent==='←1 / 2→'");
        check(view, @"optionKey(document.querySelector('.br-code'),'ArrowUp') && document.querySelector('.br-change-position').textContent==='1 / 2'");
        check(view, @"!optionKey(document.querySelector('.br-filter'),'ArrowRight') && !optionKey(document.body,'ArrowDown',{shiftKey:true}) && document.querySelector('.br-change-position').textContent==='1 / 2'");
        evaluate(view, @"var commandFind=()=>{const event=new KeyboardEvent('keydown',{key:'f',metaKey:true,bubbles:true,cancelable:true});document.body.dispatchEvent(event);return event.defaultPrevented};commandFind();var findInput=document.querySelector('.diff-find-input');findInput.value='currentValue';findInput.dispatchEvent(new Event('input'));void 0");
        check(view, @"document.activeElement===findInput && !document.querySelector('.diff-find').hidden && document.querySelectorAll('.br-row').length===7 && document.querySelector('.diff-search-hit').textContent==='currentValue' && document.querySelector('.diff-find-status').textContent==='1 matching line'");
        check(view, @"document.body.scrollWidth<=innerWidth && !!document.querySelector('.syntax-keyword') && getComputedStyle(document.querySelector('.diff-search-hit')).backgroundColor!=='rgba(0, 0, 0, 0)'");
        evaluate(view, @"findInput.value='unfindable text';findInput.dispatchEvent(new Event('input'));void 0");
        check(view, @"document.querySelectorAll('.br-row').length===0 && document.querySelector('.diff-search-empty').textContent.includes('No matching')");
        evaluate(view, @"findInput.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));void 0");
        check(view, @"document.querySelector('.diff-find').hidden && document.querySelectorAll('.br-row').length===55");
        check(view, @"(()=>{for(const [path,text,query,kind] of [['x.json','{\"name\":1}','name','property'],['x.js','a + b','+','operator'],['x.html','<div title=\"x\">','div','tag'],['x.html','<div title=\"x\">','title','attribute']]){const probe=document.createElement('div');probe.className='br-code';appendSearchSyntax(probe,text,syntaxLines(path,text)[0],query);document.body.append(probe);const hit=probe.querySelector('mark.diff-search-hit .syntax-'+kind),readable=hit&&getComputedStyle(hit).color==='rgb(25, 28, 36)';probe.remove();if(!readable)return false;}return true;})()");
        evaluate(view, @"var displayedSource=[...document.querySelectorAll('.br-source')].map(n=>n.textContent);var syntaxOption=[...document.querySelectorAll('.br-options label')].find(n=>n.textContent==='Syntax highlighting').querySelector('input');syntaxOption.click();void 0");
        check(view, @"!document.querySelector('.br-code .syntax-keyword') && JSON.stringify(displayedSource)===JSON.stringify([...document.querySelectorAll('.br-source')].map(n=>n.textContent))");
        evaluate(view, @"syntaxOption.click();void 0");
        evaluate(view, @"document.querySelector('.br-filter').value='frontend';document.querySelector('.br-filter').dispatchEvent(new Event('input'));void 0");
        check(view, @"document.querySelectorAll('.br-file').length===1 && document.querySelector('.br-files').textContent.includes('review.js')");
        evaluate(view, @"document.querySelector('.br-filter').value='';document.querySelector('.br-filter').dispatchEvent(new Event('input'));document.querySelector('.br-file').click();document.querySelector('[aria-label=Unified]').click();void 0");
        check(view, @"document.querySelectorAll('.br-code.br-unified .br-row').length===57");
        check(view, @"!!document.querySelector('.br-code.br-unified .syntax-keyword') && !!document.querySelector('.br-code.br-unified .syntax-comment')");
        evaluate(view, @"document.querySelector('[aria-label=\"Side by side\"]').click();document.querySelector('.br-presentation').value='continuous';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));void 0");
        check(view, @"document.querySelectorAll('.br-file-card').length===2 && document.querySelectorAll('.br-gap').length===3 && document.querySelectorAll('.br-row').length<55");
        check(view, @"!!document.querySelector('.br-continuous .syntax-keyword') && !!document.querySelector('.br-continuous .syntax-comment')");
        evaluate(view, @"var rowCount=document.querySelectorAll('.br-row').length;document.querySelector('.br-gap button').click();void 0");
        check(view, @"document.querySelectorAll('.br-row').length>rowCount");
        evaluate(view, @"document.querySelector('[data-change=\"1\"]').dispatchEvent(new MouseEvent('mouseenter'));void 0");
        check(view, @"document.querySelector('.br-change-position').textContent==='1 / 2'");
        evaluate(view, @"document.querySelector('[data-change=\"1\"]').click();void 0");
        check(view, @"document.querySelector('.br-commit-panel').textContent.includes('Uncommitted changes')");
        evaluate(view, @"document.querySelector('.br-options input').click();void 0");
        check(view, @"requests.some(r=>r.operation==='review-options'&&r.ignoreWhitespace===true&&r.baseRef==='origin/main')");
        evaluate(view, @"document.querySelector('.br-presentation').value='file';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));review.render(fixture);void 0");

        // Editing must first read the selected file and acquire the native
        // refresh/close guard. A rejected save keeps the exact draft available.
        evaluate(view, @"var editFixture={...fixture,captured:'edit-fixture',files:[fixture.files[0]]};review.render(editFixture);document.querySelector('.br-edit-button').click();review.render({...editFixture,version:2});void 0");
        check(view, @"!!document.querySelector('.br-edit-text') && document.querySelector('.br-edit-text').readOnly && requests.some(r=>r.operation==='review-edit'&&r.editing)");
        evaluate(view, @"review.render({...editFixture,version:2,reviewEditing:true});document.querySelector('.br-edit-text').value='draft from WebKit\\n';document.querySelector('.br-edit-text').dispatchEvent(new Event('input'));document.querySelector('.br-save').click();void 0");
        check(view, @"requests.some(r=>r.operation==='review-save'&&r.token==='fixture-token'&&r.content==='draft from WebKit\\n') && document.querySelector('.br-edit-text').readOnly");
        evaluate(view, @"review.render({...editFixture,version:3,reviewEditing:true,error:'File changed in another editor.'});void 0");
        check(view, @"document.querySelector('.br-edit-text').value==='draft from WebKit\\n' && !document.querySelector('.br-edit-text').readOnly && document.body.textContent.includes('File changed in another editor.')");
        evaluate(view, @"document.querySelector('.br-cancel').click();void 0");
        check(view, @"!!document.querySelector('.br-discard-confirm') && document.querySelector('.br-edit-text').value==='draft from WebKit\\n'");
        evaluate(view, @"document.querySelector('.br-discard-confirm').click();review.render({...editFixture,version:3,reviewEditing:false});void 0");
        check(view, @"!document.querySelector('.br-edit-text') && !!document.querySelector('.br-code')");
        evaluate(view, @"var crlfFixture={...fixture,captured:'crlf-unchanged',files:[fixture.files[0]],review:{...fixture.review,selected:{...fixture.review.selected,newText:'one\\r\\ntwo\\r\\n'}}};review.render(crlfFixture);document.querySelector('.br-edit-button').click();review.render({...crlfFixture,version:2});review.render({...crlfFixture,version:2,reviewEditing:true});void 0");
        check(view, @"document.querySelector('.br-edit-text').value==='one\\ntwo\\n'");
        evaluate(view, @"document.querySelector('.br-save').click();void 0");
        check(view, @"requests.at(-1).operation==='review-save' && requests.at(-1).content==='one\\r\\ntwo\\r\\n'");
        evaluate(view, @"review.render({...crlfFixture,version:3,reviewEditing:true});review.render({...crlfFixture,version:3,reviewEditing:false});void 0");
        check(view, @"!document.querySelector('.br-edit-text')");
        evaluate(view, @"var longText='文字'.repeat(150);review.render({...fixture,captured:'long-lines',review:{...fixture.review,selected:{...fixture.review.selected,oldText:longText+'x\\n',newText:longText+'y\\n',hunks:[{oldStart:1,oldLines:1,newStart:1,newLines:1,commits:[commit]}]}}});var horizontal=document.querySelector('.br-horizontal-scroll');horizontal.scrollLeft=250;horizontal.dispatchEvent(new Event('scroll'));void 0");
        check(view, @"horizontal.scrollWidth>horizontal.clientWidth && getComputedStyle(document.querySelector('.br-source')).transform!=='none' && document.body.scrollWidth<=innerWidth+1");
        evaluate(view, @"var literalText='const value = '+JSON.stringify('<script>literal</script>')+';\\n';review.render({...fixture,captured:'literal-source',review:{...fixture.review,selected:{...fixture.review.selected,oldText:'const value = 1;\\n',newText:literalText,hunks:[{oldStart:1,oldLines:1,newStart:1,newLines:1,commits:[commit]}]}}});void 0");
        check(view, @"!document.querySelector('.br-code script') && document.querySelector('.br-add .br-source').textContent===literalText.trimEnd() && !!document.querySelector('.br-add .syntax-string')");
        evaluate(view, @"review.render(fixture);void 0");

        // Check real WebKit scrolling: loading a distant first hunk, jumping
        // between hunks, changing layouts and receiving a delayed reread.
        evaluate(view, @"var navBefore=Array.from({length:300},(_,i)=>`// line ${i+1}`),navAfter=[...navBefore];navAfter[183]='// first change';navAfter[249]='// second change';var navData={...fixture.review.selected,path:'navigation.go',oldText:navBefore.join('\\n')+'\\n',newText:navAfter.join('\\n')+'\\n',hunks:[{oldStart:184,oldLines:1,newStart:184,newLines:1,commits:[commit]},{oldStart:250,oldLines:1,newStart:250,newLines:1,commits:[]}]};var navFixture={...fixture,captured:'navigation',version:40,files:[{...fixture.files[0],path:navData.path},fixture.files[1]],review:{...fixture.review,selected:navData}};review.render(navFixture);var content=document.querySelector('.br-content');var selectedVisible=()=>{const row=document.querySelector('.br-current-change').getBoundingClientRect(),area=content.getBoundingClientRect();return row.top>=area.top+60&&row.bottom<=area.bottom};void 0");
        check(view, @"document.querySelector('.br-change-position').textContent==='1 / 2' && content.scrollTop>0 && selectedVisible()");
        check(view, @"[...document.querySelectorAll('.br-nav-group')].map(n=>[...n.children].map(c=>c.textContent).join('|')).join(';')==='↑|1 / 2|↓;←|1 / 2|→'");
        evaluate(view, @"var firstTop=content.scrollTop;button('↓').click();void 0");
        check(view, @"document.querySelector('.br-change-position').textContent==='2 / 2' && content.scrollTop>firstTop && selectedVisible() && !button('↓').disabled && !button('↑').disabled");
        evaluate(view, @"document.querySelector('[aria-label=Unified]').click();void 0");
        check(view, @"document.querySelector('.br-current-change').dataset.change==='1' && selectedVisible()");
        evaluate(view, @"document.querySelector('[aria-label=\"Side by side\"]').click();document.querySelector('.br-presentation').value='continuous';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));void 0");
        check(view, @"document.querySelector('.br-current-change').dataset.change==='1' && selectedVisible()");
        evaluate(view, @"document.querySelector('.br-presentation').value='file';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));button('↓').click();var navSecond={...navData,path:fixture.files[1].path,hunks:navData.hunks.slice(0,1)};review.render({...navFixture,version:41,file:1,review:{...navFixture.review,selected:navSecond}});void 0");
        check(view, @"document.querySelector('.br-change-position').textContent==='1 / 1' && selectedVisible() && !button('↑').disabled && button('↓').disabled");
        evaluate(view, @"button('↑').click();var beforeReplyTop=content.scrollTop;review.render({...navFixture,version:42});void 0");
        check(view, @"beforeReplyTop>0 && Math.abs(content.scrollTop-beforeReplyTop)<1 && document.querySelector('.br-change-position').textContent==='2 / 2' && selectedVisible()");
        evaluate(view, @"button('↑').click();void 0");
        check(view, @"document.querySelector('.br-change-position').textContent==='1 / 2' && selectedVisible() && content.scrollTop<beforeReplyTop");
        // Crossing backward must wait for an uncached file and then reveal its
        // last change, including in the continuous presentation.
        evaluate(view, @"var backData={...navData,path:'previous.go'},backSecond={...navSecond,path:'next.go'};var backFixture={...navFixture,captured:'backward-navigation',version:50,file:1,files:[{path:backData.path,status:'M'},{path:backSecond.path,status:'M'}],review:{...navFixture.review,selected:backSecond}};review.render(backFixture);document.querySelector('.br-presentation').value='continuous';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));button('↑').click();void 0");
        check(view, @"button('↑').disabled && button('↓').disabled && document.querySelectorAll('.br-nav-group')[1].textContent==='←1 / 2→'");
        evaluate(view, @"review.render({...backFixture,version:51,file:0,review:{...backFixture.review,selected:backData}});void 0");
        check(view, @"document.querySelector('.br-change-position').textContent==='2 / 2' && document.querySelector('.br-current-change').dataset.path==='previous.go' && selectedVisible()");
        evaluate(view, @"button('↓').click();void 0");
        check(view, @"document.querySelector('.br-current-change').dataset.path==='next.go' && document.querySelector('.br-change-position').textContent==='1 / 1' && selectedVisible() && button('↓').disabled");
        // Scroll the actual WebKit viewport, without selecting a file. Both
        // layouts must follow it while preserving focus and the scroll offset.
        evaluate(view, @"var scrollFiles=Array.from({length:8},(_,i)=>({path:`scroll-${i}.go`,status:'M'}));var scrollFixture={...fixture,captured:'scroll-tracking',version:70,files:scrollFiles};var scrollData=i=>({...fixture.review.selected,path:scrollFiles[i].path,oldText:'old\\n'.repeat(i===7?1:35),newText:'new\\n'.repeat(i===7?1:35),hunks:[{oldStart:1,oldLines:i===7?1:35,newStart:1,newLines:i===7?1:35,commits:[{...commit,subject:`File ${i} commit`}]}]});for(let i=0;i<8;i++)review.render({...scrollFixture,version:70+i,file:i,review:{...fixture.review,selected:scrollData(i)}});var scrollArea=document.querySelector('.br-content');var filePosition=()=>document.querySelectorAll('.br-nav-group')[1].textContent;var selectedPath=()=>document.querySelector('.br-file[aria-selected=true]')?.dataset.path;var scrollToCard=i=>{const card=document.querySelectorAll('.br-file-card')[i];scrollArea.scrollTop+=card.getBoundingClientRect().top-scrollArea.getBoundingClientRect().top;scrollArea.focus();scrollArea.dispatchEvent(new Event('scroll'));};void 0");
        for (NSString *layout in @[@"Side by side",@"Unified"]) {
            evaluate(view,[NSString stringWithFormat:@"document.querySelector('[aria-label=\"%@\"]').click();scrollToCard(4);var scrollOffset=scrollArea.scrollTop;void 0",layout]);
            waitForCheck(view,@"filePosition()==='←5 / 8→' && selectedPath()==='scroll-4.go'");
            check(view,@"scrollArea.scrollTop===scrollOffset && document.activeElement===scrollArea && document.querySelector('.br-commit-panel').textContent.includes('File 4 commit')");
            evaluate(view,@"scrollToCard(1);void 0");
            waitForCheck(view,@"filePosition()==='←2 / 8→' && selectedPath()==='scroll-1.go'");
            evaluate(view,@"scrollArea.scrollTop=scrollArea.scrollHeight;scrollArea.dispatchEvent(new Event('scroll'));void 0");
            waitForCheck(view,@"filePosition()==='←8 / 8→' && selectedPath()==='scroll-7.go'");
            check(view,@"(()=>{const row=document.querySelector('.br-file[aria-selected=true]').getBoundingClientRect(),list=document.querySelector('.br-files').getBoundingClientRect();return row.top>=list.top-1&&row.bottom<=list.bottom+1})()");
        }
        // Larger counts must fit between the arrows at the minimum width.
        evaluate(view, @"document.querySelector('.br-change-position').textContent='234 / 234';document.querySelectorAll('.br-nav-group')[1].querySelector('.br-position').textContent='2000 / 2000';void 0");
        for (NSNumber *width in @[@1280,@900,@720]) {
            [window setContentSize:NSMakeSize(width.doubleValue,600)];
            check(view, @"document.body.scrollWidth<=innerWidth+1 && [...document.querySelectorAll('.br-nav-group')].every(g=>{const [up,count,down]=[...g.children].map(n=>n.getBoundingClientRect());return up.right<=count.left&&count.right<=down.left&&up.left>=0&&down.right<=innerWidth})");
            check(view, @"(()=>{const nav=document.querySelector('.br-nav').getBoundingClientRect(),controls=document.querySelector('.br-controls').getBoundingClientRect();return nav.right<=controls.left||nav.bottom<=controls.top})()");
        }
        [window setContentSize:NSMakeSize(1280,800)];
        evaluate(view, @"review.render(fixture);void 0");

        // File-list focus survives clicking, list replacement and response rendering.
        evaluate(view,@"var navigationFixture={...fixture,path:'/fixture/ignore-navigation',version:100,files:[...fixture.files,{path:'cmd/devjay/review_test.go'},{path:'cmd/devjay/client_gen.go'}]};review.render(navigationFixture);var presentation=document.querySelector('.br-presentation');presentation.value='file';presentation.onchange();document.querySelector('.br-file').click();var plainArrow=key=>document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key,bubbles:true,cancelable:true}));void 0");
        check(view,@"document.activeElement===document.querySelector('.br-files') && document.querySelectorAll('.br-file').length===2 && document.querySelector('.br-files-title').textContent==='2 / 4 files'");
        evaluate(view,@"plainArrow('ArrowDown');review.render({...navigationFixture,version:101});void 0");
        check(view,@"document.activeElement===document.querySelector('.br-files') && document.querySelector('.br-file[aria-selected=true]').dataset.path==='cmd/devjay/frontend/review.js'");
        evaluate(view,@"document.activeElement.blur();plainArrow('ArrowUp');document.querySelector('.br-ignore-options').open=true;void 0");
        check(view,@"document.activeElement===document.querySelector('.br-files') && document.querySelector('.br-file[aria-selected=true]').dataset.path==='cmd/devjay/review.go'");
        for (NSNumber *width in @[@1280,@720]) {
            [window setContentSize:NSMakeSize(width.doubleValue,600)];
            check(view,@"document.body.scrollWidth<=innerWidth+1 && document.querySelector('.br-ignore-patterns').getBoundingClientRect().right<=document.querySelector('.br-sidebar').getBoundingClientRect().right && document.querySelector('.br-files').clientHeight>=80");
        }
        evaluate(view,@"var ignores=document.querySelector('.br-ignore-patterns');ignores.value='*.go';ignores.dispatchEvent(new Event('input'));void 0");
        check(view,@"document.querySelectorAll('.br-file').length===1 && document.querySelector('.br-file[aria-selected=true]').dataset.path.endsWith('review.js')");
        evaluate(view,@"var showIgnored=document.querySelector('.br-show-ignored input');showIgnored.checked=true;showIgnored.dispatchEvent(new Event('change'));void 0");
        check(view,@"document.querySelectorAll('.br-file').length===4");
        evaluate(view,@"showIgnored.checked=false;showIgnored.dispatchEvent(new Event('change'));document.querySelector('.br-ignore-options').open=false;review.render(fixture);void 0");
        [window setContentSize:NSMakeSize(1280,800)];

        // Optional data captured by a disposable-repository Go test verifies
        // the Go JSON contract against the production renderer without opening
        // the app or loading the user's configuration.
        NSString *fixturePath=NSProcessInfo.processInfo.environment[@"DEVJAY_REVIEW_FIXTURE"];
        if (fixturePath) {
            NSString *data=[NSString stringWithContentsOfFile:fixturePath encoding:NSUTF8StringEncoding error:nil]; assert(data);
            evaluate(view,[NSString stringWithFormat:@"review.render(%@);void 0",data]);
            check(view,@"document.querySelectorAll('.br-file').length>0 && document.querySelectorAll('.br-change').length>0");
        }
        NSString *output = NSProcessInfo.processInfo.environment[@"DEVJAY_REVIEW_SNAPSHOT"];
        if (NSProcessInfo.processInfo.environment[@"DEVJAY_LIGHT_PREVIEW"]) evaluate(view,@"document.documentElement.dataset.theme='light'");
        if (output) {
            __block BOOL done=NO;
            [view takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error) {
                assert(image&&!error);
                NSBitmapImageRep *bitmap=[NSBitmapImageRep imageRepWithData:image.TIFFRepresentation];
                assert([[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:output atomically:YES]);done=YES;
            }]; spin(&done);
        }
        // Keep the previous file mounted while a newly selected file loads.
        evaluate(view,@"document.querySelector('.br-presentation').value='file';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));var waitingFixture={...fixture,captured:'waiting-file',version:180};review.render(waitingFixture);var retainedCard=document.querySelector('.br-file-card');var retainedContent=document.querySelector('.br-content');retainedContent.scrollTop=120;var retainedTop=retainedContent.scrollTop;document.querySelectorAll('.br-file')[1].click();void 0");
        check(view,@"document.querySelector('.br-file-card')===retainedCard && retainedContent.scrollTop===retainedTop && !document.querySelector('.br-load-file') && document.querySelector('.br-file-loading').textContent==='Loading…'");
        evaluate(view,@"flushReviewFrames();var waitingRequest=requests.filter(r=>r.operation==='review-prefetch'&&r.version===180).at(-1);review.prefetch({...waitingFixture,prefetchID:waitingRequest.prefetchID,review:{...fixture.review,selected:{...fixture.review.selected,path:fixture.files[1].path}}});void 0");
        check(view,@"document.querySelector('.br-file-card').dataset.path===fixture.files[1].path && document.querySelector('.br-file-loading').hidden");

        // Full preload and held-key navigation must not create loading-state
        // cycles or leave a queue of repaints after key repeat stops.
        evaluate(view,@"var preloadFiles=Array.from({length:40},(_,i)=>({path:`preload-${i}.go`,status:'M'}));var preloadFixture={...fixture,path:'/fixture/full-preload',captured:'full-preload',version:200,files:preloadFiles,review:{...fixture.review,selected:{...fixture.review.selected,path:preloadFiles[0].path}}};document.querySelector('.br-presentation').value='file';document.querySelector('.br-presentation').dispatchEvent(new Event('change'));review.render(preloadFixture);var preloaded=1;var preloadBatches=0;while(preloaded<40){flushReviewFrames();var batch=requests.filter(r=>r.operation==='review-prefetch'&&r.version===200).at(-1);if(!batch||++preloadBatches>10)throw Error('Preload did not drain');for(const index of batch.prefetch){review.prefetch({...preloadFixture,prefetchID:batch.prefetchID,review:{...preloadFixture.review,selected:{...fixture.review.selected,path:preloadFiles[index].path}}});preloaded++;}review.prefetch({...preloadFixture,prefetchID:batch.prefetchID,done:true});}flushReviewFrames();var preloadRequestCount=requests.length;var heldList=document.querySelector('.br-files');heldList.focus();var heldOldCard=document.querySelector('.br-file-card');var heldToolbar=document.querySelector('.br-toolbar');var heldControls=[...heldToolbar.querySelectorAll('select,.br-layouts button,.br-options button')];var heldControlFrame=()=>JSON.stringify(heldControls.map(n=>[n.disabled,getComputedStyle(n).opacity,n.getBoundingClientRect().width,n.getBoundingClientRect().height]));var beforeHeldControls=heldControlFrame();for(let i=0;i<30;i++)heldList.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowDown',repeat:true,bubbles:true,cancelable:true}));void 0");
        check(view,@"document.querySelector('.br-preload-status').textContent==='40 files ready' && document.querySelector('.br-file[aria-selected=true]').dataset.path==='preload-30.go' && document.querySelector('.br-file-card')===heldOldCard && requests.length===preloadRequestCount");
        evaluate(view,@"flushReviewFrames();heldList.dispatchEvent(new KeyboardEvent('keyup',{key:'ArrowDown',bubbles:true}));var heldFinalCard=document.querySelector('.br-file-card');flushReviewFrames();flushReviewFrames();void 0");
        check(view,@"heldFinalCard.dataset.path==='preload-30.go' && document.querySelector('.br-file-card')===heldFinalCard && document.querySelector('.br-toolbar')===heldToolbar && heldControlFrame()===beforeHeldControls && requests.length===preloadRequestCount && !document.querySelector('.branch-review').classList.contains('br-loading')");

        // Branch controls are centered in the header. Refresh stays square
        // after Switch, including alongside warnings and suggested actions.
        evaluate(view,@"review.destroy();void 0");
        check(view,@"!optionKey(document.body,'ArrowRight')");
        evaluate(view,@"document.body.className='git-viewer';var legacyRoot=document.querySelector('#app');var renderLegacy=mountGitViewer(legacyRoot,data=>requests.push(data),null,async()=>{});renderLegacy({...fixture,kind:'edits',counts:{edits:2},diff:fixture.review.selected.diff,commits:[]});var checkBranchControls=()=>{const nodes=[...document.querySelector('.git-branch-name').children,document.querySelector('.git-review-branch')],rects=nodes.map(n=>n.getBoundingClientRect()),[branch,change,refresh,review]=rects,header=document.querySelector('.git-view-header').getBoundingClientRect();return !document.querySelector('.git-toolbar-label')&&Math.abs((document.querySelector('.git-view-title strong').getBoundingClientRect().top+document.querySelector('.git-view-title strong').getBoundingClientRect().bottom)-(header.top+header.bottom))<2&&nodes.length===4&&Math.abs(review.right-(innerWidth-18))<1&&nodes[1].classList.contains('git-switch-branch')&&nodes[2].classList.contains('git-refresh')&&nodes[3].classList.contains('git-review-branch')&&branch.width>20&&Math.abs(refresh.width-refresh.height)<1&&refresh.width===28&&rects.every((r,i)=>r.left>=0&&r.right<=innerWidth&&(i===0||rects[i-1].right<=r.left)&&Math.abs((r.top+r.bottom)-(header.top+header.bottom))<2)};void 0");
        evaluate(view,@"document.activeElement.blur();void 0");
        check(view,@"document.activeElement===document.body && optionKey(document.body,'ArrowDown') && document.querySelector('.git-diff-position').textContent.includes('2 / 2')");
        check(view,@"optionKey(document.querySelector('.git-diff'),'ArrowUp') && document.querySelector('.git-diff-position').textContent.includes('1 / 2')");
        evaluate(view,@"commandFind();var legacyFind=document.querySelector('.diff-find-input');legacyFind.value='currentValue';legacyFind.dispatchEvent(new Event('input'));void 0");
        check(view,@"document.activeElement===legacyFind && document.querySelector('.diff-search-hit').textContent==='currentValue' && document.querySelector('.diff-find-status').textContent.includes('1 matching line')");
        evaluate(view,@"document.querySelector('.diff-find-close').click();void 0");
        check(view,@"document.querySelector('.diff-find').hidden && document.activeElement===document.querySelector('.git-diff')");
        for (NSNumber *width in @[@1100,@900,@720]) {
            [window setContentSize:NSMakeSize(width.doubleValue,600)];
            check(view,@"checkBranchControls() && document.body.scrollWidth<=innerWidth && document.querySelector('.git-view-header').getBoundingClientRect().height<=50");
        }
        evaluate(view,@"var suggestionFixture=document.createElement('section');suggestionFixture.className='git-view-suggestions';var suggestionWarning=document.createElement('p');suggestionWarning.className='git-suggestion-warning';var warningCopy=document.createElement('span');warningCopy.textContent='Local edits must be committed before rebasing this branch.';suggestionWarning.append(warningCopy);var actionHeader=document.createElement('div');actionHeader.className='git-suggestion-header';var actionButtons=document.createElement('div');actionButtons.className='git-suggestion-buttons';for(const title of ['Fetch updates','Rebase and push','Push 3 commits']){const action=document.createElement('button');action.textContent=title;actionButtons.append(action)}actionHeader.append(actionButtons);suggestionFixture.append(actionHeader,suggestionWarning);document.querySelector('.git-view-header').append(suggestionFixture);void 0");
        for (NSNumber *width in @[@1100,@900,@720]) {
            [window setContentSize:NSMakeSize(width.doubleValue,600)];
            check(view,@"checkBranchControls() && document.body.scrollWidth<=innerWidth");
            check(view,@"(()=>{const branch=document.querySelector('.git-view-branch').getBoundingClientRect(),warning=suggestionWarning.getBoundingClientRect(),actions=actionHeader.getBoundingClientRect();return branch.right<=warning.left&&warning.width>30&&warning.right<=actions.left&&actions.right<=innerWidth&&[...actionButtons.children].every(n=>{const r=n.getBoundingClientRect();return r.left>=actions.left&&r.right<=actions.right})})()");
        }
        evaluate(view,@"suggestionWarning.remove();void 0");
        for (NSNumber *width in @[@1100,@720]) {
            [window setContentSize:NSMakeSize(width.doubleValue,600)];
            check(view,@"checkBranchControls() && document.body.scrollWidth<=innerWidth && actionHeader.getBoundingClientRect().right<=innerWidth");
        }
        // Rebase and Review must share the branch controls' vertical center,
        // both with and without guidance between them.
        evaluate(view,@"var rebaseButton=actionButtons.firstElementChild;rebaseButton.textContent='Rebase';actionButtons.replaceChildren(rebaseButton);void 0");
        for (NSNumber *warning in @[@NO,@YES]) {
            evaluate(view,warning.boolValue ? @"suggestionFixture.append(suggestionWarning);void 0" : @"suggestionWarning.remove();void 0");
            for (NSNumber *width in @[@1100,@900,@720]) {
                [window setContentSize:NSMakeSize(width.doubleValue,600)];
                check(view,@"checkBranchControls() && document.body.scrollWidth<=innerWidth");
                check(view,@"(()=>{const rebase=rebaseButton.getBoundingClientRect(),review=document.querySelector('.git-review-branch').getBoundingClientRect();return document.querySelector('.git-view-header').getBoundingClientRect().height<=50&&rebase.right<=review.left&&Math.abs((rebase.top+rebase.bottom)-(review.top+review.bottom))<2})()");
            }
        }
        evaluate(view,@"document.querySelector('.git-review-branch').click();void 0");
        check(view,@"requests.some(r=>r.operation==='review-open')");
        check(view,@"!!document.querySelector('.git-diff .syntax-keyword') && !!document.querySelector('.git-diff .syntax-comment')");
        // History selection must retain the frame's real WebKit contrast and
        // geometry during the read, then update only the selected details.
        evaluate(view,@"var historyFixture={...fixture,version:100,kind:'branch',branchOnly:true,branchCommits:2,branchDiff:{base:'base',target:'origin/main'},commit:'first',message:'First commit',counts:{edits:2,branch:2},commits:[{...commit,id:'first',subject:'First commit',presence:'shared'},{...commit,id:'second',subject:'Second commit',presence:'shared'}],diff:fixture.review.selected.diff,editor:{resolved:{name:'Editor'},canOpen:true}};renderLegacy(historyFixture);var historyList=document.querySelector('.git-view-commits .git-scroll-list');historyList.querySelector('button').click();var historyChrome=[...document.querySelectorAll('.git-view-header button,.git-history-scope button,.git-branch-diff,.git-history-order,.git-diff-toolbar button,.file-editor-controls button')];var chromeFrame=()=>historyChrome.map(node=>{const r=node.getBoundingClientRect();return [getComputedStyle(node).opacity,r.x,r.y,r.width,r.height,node.textContent]});var historyFrame=JSON.stringify(chromeFrame()),historyContext=document.querySelector('.git-view-context').firstChild,historyTitle=document.querySelector('.git-current-branch').firstChild,historyLine=document.querySelector('.git-diff').firstChild;historyList.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowDown',bubbles:true,cancelable:true}));renderLegacy({...historyFixture,version:101,loading:true,editor:null});void 0");
        check(view,@"requests.at(-1).operation==='commit' && requests.at(-1).commit==='second' && document.activeElement===historyList && JSON.stringify(chromeFrame())===historyFrame && document.querySelector('.git-diff').firstChild===historyLine && document.querySelector('.git-full-history').disabled && document.querySelector('.git-review-branch').disabled");
        evaluate(view,@"var secondCommit={...historyFixture,version:101,commit:'second',message:'Second commit',diff:'@@ -1 +1 @@\\n-old\\n+second commit',editor:null};renderLegacy(secondCommit);void 0");
        check(view,@"document.activeElement===historyList && historyList.querySelector('.selected').textContent.includes('Second commit') && document.querySelector('.git-view-context').firstChild===historyContext && document.querySelector('.git-current-branch').firstChild===historyTitle && document.querySelector('.git-diff').textContent.includes('second commit') && !document.querySelector('.git-full-history').disabled && !document.querySelector('.git-read-pending')");
        evaluate(view,@"var secondLine=document.querySelector('.git-diff').firstChild;renderLegacy({...secondCommit,editor:historyFixture.editor});historyList.blur();document.body.dispatchEvent(new KeyboardEvent('keydown',{key:'ArrowUp',bubbles:true,cancelable:true}));void 0");
        check(view,@"requests.at(-1).commit==='first' && document.querySelector('.git-diff').firstChild===secondLine && document.querySelector('.git-view-context').firstChild===historyContext && document.querySelector('.git-current-branch').firstChild===historyTitle");
        // The aggregate action and sort choice share one box at minimum width.
        for (NSString *theme in @[@"dark",@"light"]) {
            evaluate(view,[NSString stringWithFormat:@"document.documentElement.dataset.theme='%@';renderLegacy({...historyFixture,version:120,branchCommits:1234});void 0",theme]);
            for (NSNumber *width in @[@1100,@720]) {
                [window setContentSize:NSMakeSize(width.doubleValue,600)];
                check(view,@"(()=>{const row=document.querySelector('.git-branch-summary'),count=row.querySelector('.git-branch-diff'),sort=row.querySelector('select'),r=row.getBoundingClientRect(),c=count.getBoundingClientRect(),s=sort.getBoundingClientRect();return count.textContent==='All 1,234 commits'&&sort.value==='newest'&&c.right<=s.left&&Math.abs((c.top+c.bottom)-(s.top+s.bottom))<2&&Math.abs(s.right-(r.right-8))<2&&document.querySelector('.git-view-context').getBoundingClientRect().height===0&&document.body.scrollWidth<=innerWidth})()");
            }
        }
        evaluate(view,@"var orderChoice=document.querySelector('.git-history-order');orderChoice.focus();orderChoice.value='oldest';orderChoice.dispatchEvent(new Event('change'));renderLegacy({...historyFixture,version:121,oldestFirst:true,commit:'second',commits:[...historyFixture.commits].reverse()});void 0");
        check(view,@"requests.at(-1).operation==='history-sort'&&requests.at(-1).order==='oldest'&&document.activeElement===orderChoice&&orderChoice.value==='oldest'&&document.querySelector('.git-commit-subject').textContent==='Second commit'");
        evaluate(view,@"renderLegacy({...historyFixture,version:122,branchOnly:false,oldestFirst:true});void 0");
        check(view,@"document.querySelector('.git-history-order').hidden");
        evaluate(view,@"renderLegacy.destroy();void 0");
        check(view,@"!optionKey(document.body,'ArrowRight')");
        puts("Branch review WebKit layout checks passed");
    }
    return 0;
}
