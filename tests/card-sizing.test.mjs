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
 let busy=false,now=0,nextTimer=1;
 const timers=new Map(),events={};
 const list={children:[{}],style:{flex:'',height:''},natural:140,clientHeight:261,
  // WebKit may clip a card internally while reporting no list overflow.
  get scrollHeight(){return this.style.height==='max-content'?this.natural:this.clientHeight;}};
 const root={clientHeight:300,scrollHeight:300};
 const body={dataset:{decoration:'minimal'},classList:{contains:()=>workspace}};
 const window={innerWidth:900,innerHeight:300,addEventListener(name,fn){events[name]=fn;}};
 const context={window,document:{body},performance:{now:()=>now},
  setTimeout:(fn,delay)=>{const id=nextTimer++;timers.set(id,{fn,at:now+delay});return id;},
  clearTimeout:id=>timers.delete(id),requestAnimationFrame:fn=>{frames.push(fn);return frames.length;},
  MutationObserver:class {constructor(fn){observers.push(fn);}observe(){}},ResizeObserver:class {observe(){}}};
 vm.createContext(context);
 vm.runInContext(source.replace(/export /g,''),context);
 const schedule=context.installCardSizing(root,list,value=>requests.push(value),()=>busy);
 const measure=()=>{schedule();while(frames.length)frames.shift()();};
 const advance=ms=>{
  const end=now+ms;
  while(true){
   const next=[...timers].sort((a,b)=>a[1].at-b[1].at)[0];
   if(!next || next[1].at>end)break;
   const [id,timer]=next;now=timer.at;timers.delete(id);timer.fn();
   while(frames.length)frames.shift()();
  }
  now=end;
 };
 return {list,root,body,window,requests,events,measure,advance,timers,setBusy:value=>busy=value,
  resize:height=>{list.clientHeight+=height-window.innerHeight;window.innerHeight=height;},
  settle:()=>{measure();advance(3000);}};
}

test('strip follows its content height with extra room and refits after decoration changes',()=>{
 const view=sizingHarness();view.settle();
 assert.equal(view.requests[0].height,191); // 140 content + 39 chrome + 12 breathing room.
 assert.equal(view.requests[0].refit,true);
 assert.equal(view.requests[0].decoration,'minimal');
 assert.deepEqual(view.list.style,{flex:'',height:''}); // Measuring never leaves the row stretched differently.
 view.resize(191);view.settle();
 assert.equal(view.requests.length,1); // A fitted strip settles without another request.
 view.resize(400);view.settle();
 assert.equal(view.requests[1].height,191); // Height stays automatic; width is untouched.
 view.body.dataset.decoration='none';view.list.clientHeight=383;view.settle();
 assert.equal(view.requests[2].height,169);
 assert.equal(view.requests[2].refit,true);
 view.list.natural=420;view.settle();
 assert.equal(view.requests[3].height,449);
 assert.equal(view.requests[3].refit,true);
 view.list.natural=100;view.settle();
 assert.equal(view.requests[4].height,132); // Shorter status shrinks to the increased floor.
 view.resize(132);view.settle();
 view.body.dataset.layout='vertical';view.settle();
 assert.equal(view.requests.length,5); // Same fitted height needs no native work.
});

test('strip waits for initial cards before refitting, and Sites remain grow-only',()=>{
 const view=sizingHarness();view.list.children=[];view.settle();
 assert.equal(view.requests.length,0);
 view.list.children=[{}];view.list.natural=10;view.settle();
 assert.equal(view.requests[0].height,132);
 const site=sizingHarness(true);site.settle();
 assert.equal(site.requests.length,0);
 site.body.dataset.decoration='none';site.root.scrollHeight=340;site.settle();
 assert.equal(site.requests[0].height,340);
 assert.equal(site.requests[0].refit,false);
});

test('status and app rows grow the strip, then disappearing rows shrink it without changing decoration',()=>{
 const view=sizingHarness();
 view.list.natural=86;view.settle();
 assert.equal(view.requests[0].height,137);
 view.resize(137);view.settle();
 view.list.natural=172;view.settle();
 assert.equal(view.list.scrollHeight,view.list.clientHeight); // No outer overflow signal.
 assert.equal(view.requests.at(-1).height,223);
 assert.equal(view.requests.at(-1).refit,true);
 const count=view.requests.length;
 view.settle();assert.equal(view.requests.length,count); // Screen-limited requests are deduplicated.
 view.resize(223);view.settle();
 view.list.natural=86;view.settle();
 assert.equal(view.requests.length,count+1);
 assert.equal(view.requests.at(-1).height,137);
 view.resize(137);view.settle();view.settle();
 assert.equal(view.requests.length,count+1); // No oscillation after shrinking.
});

test('content fitting waits for card dragging to finish and retries after a stale viewport',()=>{
 const view=sizingHarness();view.settle();view.resize(view.requests.at(-1).height);view.settle();
 const count=view.requests.length;
 view.setBusy(true);view.list.natural=90;view.settle();
 assert.equal(view.requests.length,count);
 view.setBusy(false);view.settle();
 assert.equal(view.requests.at(-1).height,141);
 view.resize(250);view.settle(); // Native rejected the preceding measurement during a resize.
 assert.equal(view.requests.at(-1).height,141);
 assert.equal(view.requests.at(-1).viewportHeight,250);
 assert.equal(view.requests.at(-1).viewportWidth,900);
 view.resize(141);view.settle();
 assert.equal(view.requests.length,count+2);
});

function fittedStrip() {
 const view=sizingHarness();view.measure();
 view.resize(view.requests.at(-1).height);view.measure();
 view.requests.length=0;
 return view;
}

test('a fleeting status row causes no native resize, including after its timer expires',()=>{
 const view=fittedStrip();
 view.list.natural=180;view.measure();
 view.advance(150);assert.equal(view.requests.length,0);
 view.list.natural=140;view.measure();view.advance(2000);
 assert.equal(view.requests.length,0);
 assert.equal(view.timers.size,0);
});

test('persistent growth waits 300 ms without restarting on same-height updates',()=>{
 const view=fittedStrip();
 view.list.natural=180;view.measure();
 for(let i=0;i<2;i++){view.advance(100);view.measure();}
 view.advance(99);assert.equal(view.requests.length,0);
 view.advance(1);assert.equal(view.requests.length,1);
 assert.equal(view.requests[0].height,231);
 view.measure();view.advance(2000);
 assert.equal(view.requests.length,1); // Screen-limited fits still deduplicate.
 assert.equal(view.timers.size,0);
});

test('shrinking waits three full seconds and cancels if the rows come back',()=>{
 const view=fittedStrip();
 view.list.natural=100;view.measure();view.advance(2700);
 view.list.natural=140;view.measure();view.advance(3000);
 assert.equal(view.requests.length,0);
 view.list.natural=100;view.measure();view.advance(2999);
 assert.equal(view.requests.length,0);
 view.advance(1);assert.equal(view.requests[0].height,151);
 view.resize(151);view.measure();view.advance(2000);
 assert.equal(view.requests.length,1);
});

test('a changed target restarts the wait, and expiry remeasures current content',()=>{
 const view=fittedStrip();
 view.list.natural=180;view.measure();view.advance(200);
 view.list.natural=200;view.measure();view.advance(299);
 assert.equal(view.requests.length,0);
 // A timer must not publish an old measurement before the observer runs.
 view.list.natural=210;view.advance(1);
 assert.equal(view.requests.length,0);
 view.advance(300);assert.equal(view.requests[0].height,261);
});

test('layout changes cancel pending fits and use the current viewport immediately',()=>{
 const view=fittedStrip();
 view.list.natural=180;view.measure();view.advance(100);
 view.body.dataset.layout='vertical';view.list.natural=300;view.measure();
 assert.equal(view.requests[0].height,351);
 assert.equal(view.timers.size,0);
 view.resize(351);view.measure();view.advance(3000);
 assert.equal(view.requests.length,1);
 view.list.natural=160;view.measure();
 view.window.innerWidth=700;view.measure();
 assert.equal(view.requests[1].height,211);
 assert.equal(view.requests[1].viewportWidth,700);
 assert.equal(view.timers.size,0);
});

test('pending fitting is discarded during dragging or when the last card disappears',()=>{
 for(const empty of [false,true]){
  const view=fittedStrip();
  view.list.natural=180;view.measure();
  if(empty)view.list.children=[];else view.setBusy(true);
  view.advance(300);assert.equal(view.requests.length,0);
  assert.equal(view.timers.size,0);
  view.list.children=[{}];view.setBusy(false);
  view.events.pointerup();view.measure();
  assert.equal(view.requests[0].height,231);
 }
});


test('the native growth response does not bypass shrink delay if a row just disappeared',()=>{
 const view=fittedStrip();
 view.list.natural=180;view.measure();view.advance(300);
 assert.equal(view.requests[0].height,231);
 // The bridge applies the growth after newer content has arrived.
 view.list.natural=140;view.resize(231);view.measure();view.advance(2999);
 assert.equal(view.requests.length,1);
 view.advance(1);assert.equal(view.requests[1].height,191);
});
