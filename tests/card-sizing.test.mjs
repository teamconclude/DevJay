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
 const schedule=context.installCardSizing(root,list,value=>requests.push(value));
 return {list,root,body,window,requests,measure:()=>{schedule();while(frames.length)frames.shift()();}};
}

test('panel refits once on opening and decoration changes, preserving later manual height',()=>{
 const view=sizingHarness();view.measure();
 assert.equal(view.requests[0].height,179);
 assert.equal(view.requests[0].refit,true);
 assert.equal(view.requests[0].decoration,'minimal');
 assert.deepEqual(view.list.style,{flex:'',height:''}); // Measuring never leaves the row stretched differently.
 view.window.innerHeight=400;view.list.clientHeight=361;view.measure();
 assert.equal(view.requests.length,1); // A manual resize is not undone.
 view.body.dataset.decoration='none';view.list.clientHeight=383;view.measure();
 assert.equal(view.requests[1].height,157);
 assert.equal(view.requests[1].refit,true);
 view.list.natural=420;view.measure();
 assert.equal(view.requests[2].height,437);
 assert.equal(view.requests[2].refit,false); // Longer status still grows the panel.
 view.list.natural=100;view.measure();
 assert.equal(view.requests.length,3); // Shorter transient status doesn't cause jumps.
 view.body.dataset.layout='vertical';view.measure();
 assert.equal(view.requests[3].height,120);
 assert.equal(view.requests[3].refit,true); // Replacing the layout removes its old row-height estimate.
});

test('panel waits for initial cards before refitting, and Sites remain grow-only',()=>{
 const view=sizingHarness();view.list.children=[];view.measure();
 assert.equal(view.requests.length,0);
 view.list.children=[{}];view.list.natural=10;view.measure();
 assert.equal(view.requests[0].height,120);
 const site=sizingHarness(true);site.measure();
 assert.equal(site.requests.length,0);
 site.body.dataset.decoration='none';site.root.scrollHeight=340;site.measure();
 assert.equal(site.requests[0].height,340);
 assert.equal(site.requests[0].refit,false);
});

test('status and app rows arriving after the initial refit grow a short panel',()=>{
 const view=sizingHarness();
 view.list.natural=86;view.measure();
 assert.equal(view.requests[0].height,125);
 view.window.innerHeight=125;view.list.clientHeight=86;view.measure();
 view.list.natural=172;view.measure();
 assert.equal(view.list.scrollHeight,view.list.clientHeight); // No outer overflow signal.
 assert.equal(view.requests.at(-1).height,211);
 assert.equal(view.requests.at(-1).refit,false);
 const count=view.requests.length;
 view.measure();assert.equal(view.requests.length,count); // Screen-limited requests are deduplicated.
 view.window.innerHeight=211;view.list.clientHeight=172;view.measure();
 view.list.natural=86;view.measure();
 assert.equal(view.requests.length,count); // Disappearing rows don't shrink the window again.
});
