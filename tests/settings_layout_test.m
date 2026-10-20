#import <AppKit/AppKit.h>
#import <WebKit/WebKit.h>
#include <assert.h>
#include <stdio.h>

static BOOL loaded;
@interface SettingsLayoutDelegate : NSObject <WKNavigationDelegate>
@end
@implementation SettingsLayoutDelegate
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
        NSMutableString *js = [NSMutableString stringWithString:@"const callbacks={};const Events={On:(n,f)=>callbacks[n]=f,Emit:()=>Promise.resolve()},Window={};const installCardHover=()=>{},installDragOriginGuard=()=>{},uiReady=()=>()=>{};"];
        NSRegularExpression *imports=[NSRegularExpression regularExpressionWithPattern:@"^import .*;\\n" options:NSRegularExpressionAnchorsMatchLines error:nil];
        for (NSString *name in @[@"notice",@"bridge",@"path-label",@"settings-navigation",@"profile-settings",@"editor-settings",@"shortcut-settings",@"app"]) {
            NSString *path=[NSString stringWithFormat:@"cmd/devjay/frontend/%@.js",name];
            NSString *code=[NSString stringWithContentsOfFile:path encoding:NSUTF8StringEncoding error:nil];assert(code);
            code=[imports stringByReplacingMatchesInString:code options:0 range:NSMakeRange(0,code.length) withTemplate:@""];
            code=[code stringByReplacingOccurrencesOfString:@"export " withString:@""];
            code=[code stringByReplacingOccurrencesOfString:@"new URLSearchParams(location.search)" withString:@"new URLSearchParams('?settings')"];
            [js appendFormat:@"\n%@\n",code];
        }
        NSString *html=[NSString stringWithFormat:@"<!doctype html><meta charset='utf-8'><style>%@</style><body><main id='app'></main><script>%@</script></body>",css,js];
        NSWindow *window=[[[NSWindow alloc] initWithContentRect:NSMakeRect(0,0,880,620) styleMask:NSWindowStyleMaskBorderless backing:NSBackingStoreBuffered defer:NO] autorelease];
        WKWebViewConfiguration *configuration=[[[WKWebViewConfiguration alloc] init] autorelease];configuration.websiteDataStore=WKWebsiteDataStore.nonPersistentDataStore;
        WKWebView *view=[[[WKWebView alloc] initWithFrame:NSMakeRect(0,0,880,620) configuration:configuration] autorelease];
        SettingsLayoutDelegate *delegate=[[[SettingsLayoutDelegate alloc] init] autorelease];view.navigationDelegate=delegate;window.contentView=view;
        [view loadHTMLString:html baseURL:nil];spin(&loaded);
        evaluate(view,@"render({sites:['DevJay','Conclude','c0','c1','c2','c3','Conclude-webdoc','Conclude-webdoc2'].map((name,i)=>({id:String(i),name,path:'~/'+name,prefix:'~/',enabled:i!==7,folderMissing:i===4,needsPlacement:i===5,git:{branch:'hanord/feature-layout'},position:{label:'Desktop '+(i+1)},window:{}})),profiles:{current:'home',profiles:[{id:'home',name:'Home'},{id:'cabin',name:'Cabin'},{id:'laptop',name:'Laptop'}],targets:['home'],automatic:true,displays:[{name:'LG UltraFine',X:-100},{name:'Studio Display',X:0},{name:'ASUS PA27JCV',X:100}]},preferences:{onTop:true,allDesktops:true,showIDEStatus:true},layout:'horizontal',settingsOpen:true});editorSettings.renderEditor({editor:{apps:[],choice:'',useProject:true}})");
        evaluate(view,@"shortcutSettings.render({items:Array.from({length:13},(_,i)=>({id:'shortcut-'+i,title:i<2?'Hide/show Site windows':'Go to Site '+(i+1),label:i===0?'⌃⌥⇧⌘Space':'⌃⌥⌘'+(i%10),default:'⌃⌥⌘D',custom:i===0}))})");
        for (NSNumber *width in @[@880,@760]) {
            [window setContentSize:NSMakeSize(width.doubleValue,width.intValue==760?440:620)];
            for (NSString *page in @[@"profiles",@"sites",@"windows",@"editors",@"shortcuts"]) {
                evaluate(view,[NSString stringWithFormat:@"settingsNavigation.show('%@')",page]);
                assert([evaluate(view,@"document.body.scrollWidth<=innerWidth+1 && document.body.scrollHeight<=innerHeight+1") boolValue]);
                assert([evaluate(view,@"document.querySelector('.settings-footer').getBoundingClientRect().bottom<=innerHeight+1") boolValue]);
                assert([evaluate(view,@"[...document.querySelectorAll('.settings-nav-item')].every(n=>n.getBoundingClientRect().bottom<=innerHeight)") boolValue]);
                assert([evaluate(view,@"[...document.querySelectorAll('.settings-page:not([hidden]) button,.settings-page:not([hidden]) select,.settings-page:not([hidden]) input')].filter(n=>n.getClientRects().length).every(n=>{const r=n.getBoundingClientRect();return r.width>0&&r.left>=195&&r.right<=innerWidth+1;})") boolValue]);
            }
            evaluate(view,@"settingsNavigation.show('sites');document.querySelector('.profile-scope-picker').open=true");
            assert([evaluate(view,@"document.querySelector('.settings-footer').getBoundingClientRect().bottom<=innerHeight+1 && document.querySelector('.settings-pages').clientHeight>0") boolValue]);
            evaluate(view,@"document.querySelector('.profile-scope-picker').open=false");
            evaluate(view,@"settingsNavigation.show('profiles');document.querySelector('.profile-intro button').click()");
            assert([evaluate(view,@"!document.querySelector('.profile-modal').hidden && document.activeElement===document.querySelector('.profile-dialog input')") boolValue]);
            assert([evaluate(view,@"(()=>{const r=document.querySelector('.profile-dialog').getBoundingClientRect();return r.left>=0&&r.top>=0&&r.right<=innerWidth&&r.bottom<=innerHeight;})()") boolValue]);
            assert([evaluate(view,@"(()=>{document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Tab',shiftKey:true,bubbles:true,cancelable:true}));return document.activeElement===document.querySelector('.profile-dialog-actions button');})()") boolValue]);
            assert([evaluate(view,@"(()=>{let escaped=false;const listener=()=>escaped=true;document.addEventListener('keydown',listener);document.activeElement.dispatchEvent(new KeyboardEvent('keydown',{key:'Escape',bubbles:true,cancelable:true}));document.removeEventListener('keydown',listener);return !escaped&&document.querySelector('.profile-modal').hidden&&!document.querySelector('#app').inert;})()") boolValue]);
            evaluate(view,@"document.querySelector('.profile-intro button').click()");
            evaluate(view,@"document.querySelector('.profile-dialog-actions button').click()");
            assert([evaluate(view,@"document.querySelector('.profile-modal').hidden") boolValue]);
        }
        // Exercise the real font measurement and card/PR flex widths offscreen.
        evaluate(view,@"var originalRAF=window.requestAnimationFrame,pathFrames=[];window.requestAnimationFrame=fn=>{pathFrames.push(fn);return pathFrames.length;};"
            "var fixture=document.createElement('div');fixture.className='card-heading';fixture.style.cssText='position:fixed;left:0;top:0;width:150px;font-family:monospace';document.body.append(fixture);"
            "var fixtureTitle=document.createElement('div');fixtureTitle.className='card-title';fixture.append(fixtureTitle);"
            "var pathNode=pathLabel({prefix:'~/abcxyz/',name:'longname',path:'/Users/test/abcxyz/longname'});fixtureTitle.append(pathNode);"
            "var schedulePaths=installPathLabels(fixture);function fitPaths(){schedulePaths();while(pathFrames.length)pathFrames.shift()();}fitPaths();");
        assert([evaluate(view,@"pathNode.querySelector('.prefix').textContent.startsWith('...') && pathNode.querySelector('.prefix').textContent.endsWith('/') && pathNode.querySelector('strong').textContent==='longname'") boolValue]);
        evaluate(view,@"pathNode.dataset.pathName='extremelylongname';fixture.style.width='100px';fitPaths()");
        assert([evaluate(view,@"pathNode.querySelector('.prefix').textContent==='' && /^e.*\\.\\.\\..*e$/.test(pathNode.querySelector('strong').textContent) && pathNode.scrollWidth<=pathNode.clientWidth+1") boolValue]);
        evaluate(view,@"fixture.style.width='500px';fitPaths()");
        assert([evaluate(view,@"pathNode.textContent==='~/abcxyz/extremelylongname' && pathNode.title==='/Users/test/abcxyz/longname'") boolValue]);
        evaluate(view,@"var pr=document.createElement('div');pr.className='card-prs';pr.style.width='100px';pr.textContent='#1234';fixture.append(pr);fixture.style.width='200px';fitPaths()");
        assert([evaluate(view,@"pathNode.querySelector('.prefix').textContent==='' && pathNode.querySelector('strong').textContent.includes('...') && pathNode.scrollWidth<=pathNode.clientWidth+1") boolValue]);
        evaluate(view,@"fixture.remove();fitPaths();window.requestAnimationFrame=originalRAF;void 0");
        // Optional image for visual inspection; this fixture never shows a window.
        NSString *output=NSProcessInfo.processInfo.environment[@"DEVJAY_SETTINGS_SNAPSHOT"];
        if(output) {
            [window setContentSize:NSMakeSize(880,620)];evaluate(view,@"settingsNavigation.show('shortcuts')");
            __block BOOL done=NO;
            [view takeSnapshotWithConfiguration:nil completionHandler:^(NSImage *image,NSError *error){
                assert(image&&!error);NSBitmapImageRep *bitmap=[NSBitmapImageRep imageRepWithData:image.TIFFRepresentation];
                assert([[bitmap representationUsingType:NSBitmapImageFileTypePNG properties:@{}] writeToFile:output atomically:YES]);done=YES;
            }];spin(&done);
        }
        puts("Settings sidebar WebKit layout checks passed");
    }
    return 0;
}
