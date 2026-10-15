import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
const source=await readFile(new URL('../cmd/devjay/frontend/card-sizing.js',import.meta.url),'utf8');
const {requiredContentHeight}=await import(`data:text/javascript;base64,${Buffer.from(source).toString('base64')}`);
test('card overflow grows the viewport by the missing content height',()=>{
 assert.equal(requiredContentHeight(174,220,157),237);
 assert.equal(requiredContentHeight(500,800,470),830);
 // Preserve extra space and tolerate subpixel rounding.
 assert.equal(requiredContentHeight(300,270,270),null);
 assert.equal(requiredContentHeight(300,271,270),null);
 assert.equal(requiredContentHeight(0,100,80),null);
 assert.equal(requiredContentHeight(174,NaN,150),null);
});

function sizingHarness(workspace=false) {
 const requests=[],frames=[],observers=[];
 let busy=false;
 const list={children:[{}],style:{flex:'',height:''},natural:140,clientHeight:261,
  // WebKit may clip a card internally while reporting no list overflow.
  get scrollHeight(){return this.style.height==='max-content'?this.natural:this.clientHeight;}};
 const root={clientHeight:300,scrollHeight:300};
 const body={dataset:{decoration:'minimal'},classList:{contains:()=>workspace}};
 const window={innerWidth:900,innerHeight:300,addEventListener(){}};
 const context={window,document:{body},requestAnimationFrame:fn=>{frames.push(fn);return frames.length;},
  MutationObserver:class {constructor(fn){observers.push(fn);}observe(){}},ResizeObserver:class {observe(){}}};
 vm.createContext(context);
 vm.runInContext(source.replace(/export /g,''),context);
 const schedule=context.installCardSizing(root,list,value=>requests.push(value),()=>busy);
 return {list,root,body,window,requests,setBusy:value=>busy=value,
  resize:height=>{list.clientHeight+=height-window.innerHeight;window.innerHeight=height;},
  measure:()=>{schedule();while(frames.length)frames.shift()();}};
}

test('panel follows its content height with extra room and refits after decoration changes',()=>{
 const view=sizingHarness();view.measure();
 assert.equal(view.requests[0].height,191); // 140 content + 39 chrome + 12 breathing room.
 assert.equal(view.requests[0].refit,true);
 assert.equal(view.requests[0].decoration,'minimal');
 assert.deepEqual(view.list.style,{flex:'',height:''}); // Measuring never leaves the row stretched differently.
 view.resize(191);view.measure();
 assert.equal(view.requests.length,1); // A fitted panel settles without another request.
 view.resize(400);view.measure();
 assert.equal(view.requests[1].height,191); // Height stays automatic; width is untouched.
 view.body.dataset.decoration='none';view.list.clientHeight=383;view.measure();
 assert.equal(view.requests[2].height,169);
 assert.equal(view.requests[2].refit,true);
 view.list.natural=420;view.measure();
 assert.equal(view.requests[3].height,449);
 assert.equal(view.requests[3].refit,true);
 view.list.natural=100;view.measure();
 assert.equal(view.requests[4].height,132); // Shorter status shrinks to the increased floor.
 view.resize(132);view.measure();
 view.body.dataset.layout='vertical';view.measure();
 assert.equal(view.requests.length,5); // Same fitted height needs no native work.
});

test('panel waits for initial cards before refitting, and Sites remain grow-only',()=>{
 const view=sizingHarness();view.list.children=[];view.measure();
 assert.equal(view.requests.length,0);
 view.list.children=[{}];view.list.natural=10;view.measure();
 assert.equal(view.requests[0].height,132);
 const site=sizingHarness(true);site.measure();
 assert.equal(site.requests.length,0);
 site.body.dataset.decoration='none';site.root.scrollHeight=340;site.measure();
 assert.equal(site.requests[0].height,340);
 assert.equal(site.requests[0].refit,false);
});

test('status and app rows grow the panel, then disappearing rows shrink it without changing decoration',()=>{
 const view=sizingHarness();
 view.list.natural=86;view.measure();
 assert.equal(view.requests[0].height,137);
 view.resize(137);view.measure();
 view.list.natural=172;view.measure();
 assert.equal(view.list.scrollHeight,view.list.clientHeight); // No outer overflow signal.
 assert.equal(view.requests.at(-1).height,223);
 assert.equal(view.requests.at(-1).refit,true);
 const count=view.requests.length;
 view.measure();assert.equal(view.requests.length,count); // Screen-limited requests are deduplicated.
 view.resize(223);view.measure();
 view.list.natural=86;view.measure();
 assert.equal(view.requests.length,count+1);
 assert.equal(view.requests.at(-1).height,137);
 view.resize(137);view.measure();view.measure();
 assert.equal(view.requests.length,count+1); // No oscillation after shrinking.
});

test('content fitting waits for card dragging to finish and retries after a stale viewport',()=>{
 const view=sizingHarness();view.measure();view.resize(view.requests.at(-1).height);view.measure();
 const count=view.requests.length;
 view.setBusy(true);view.list.natural=90;view.measure();
 assert.equal(view.requests.length,count);
 view.setBusy(false);view.measure();
 assert.equal(view.requests.at(-1).height,141);
 view.resize(250);view.measure(); // Native rejected the preceding measurement during a resize.
 assert.equal(view.requests.at(-1).height,141);
 assert.equal(view.requests.at(-1).viewportHeight,250);
 assert.equal(view.requests.at(-1).viewportWidth,900);
 view.resize(141);view.measure();
 assert.equal(view.requests.length,count+2);
});
