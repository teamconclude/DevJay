import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import {nextNotice} from '../cmd/devjay/frontend/notice.js';
const source=(await readFile(new URL('../cmd/devjay/frontend/app.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'');

// Exercise the real snapshot renderer with a small DOM and the Wails boundary.
function app(search='') {
 let document;
 class Element {
  constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.style={};this.className='';this.attrs={};this.textContent='';
   this.classList={contains:c=>this.className.split(' ').includes(c),toggle:(c,on)=>{const all=new Set(this.className.split(' '));on?all.add(c):all.delete(c);this.className=[...all].join(' ');}};
  }
  append(...nodes){for(const n of nodes){n.remove();n.parent=this;this.children.push(n);}}
  remove(){if(this.parent){const a=this.parent.children;a.splice(a.indexOf(this),1);this.parent=null;}}
  replaceChildren(...nodes){if(this.contains(document.activeElement))document.activeElement=document.body;for(const n of this.children)n.parent=null;this.children=[];this.append(...nodes);}
  insertBefore(n,next){n.remove();n.parent=this;const i=this.children.indexOf(next);this.children.splice(i<0?this.children.length:i,0,n);}
  contains(n){return n===this || this.children.some(c=>c.contains(n));}
  setAttribute(k,v){this.attrs[k]=v;}
  addEventListener(){}
  matches(s){if(s.startsWith('.'))return this.classList.contains(s.slice(1));if(s==='[data-pref]')return this.dataset.pref!==undefined;if(s==='[name=sorting]')return this.name==='sorting';return false;}
  closest(s){return this.matches(s)?this:this.parent?.closest(s);}
  querySelectorAll(s){return this.children.flatMap(n=>[...(n.matches(s)?[n]:[]),...n.querySelectorAll(s)]);}
  querySelector(s){return this.querySelectorAll(s)[0]||null;}
  focus(){document.activeElement=this;}
  get firstElementChild(){return this.children[0];}
 }
 const root=new Element('main'),body=new Element('body');
 document={body,activeElement:body,createElement:tag=>new Element(tag),createTextNode:text=>{const n=new Element('text');n.textContent=text;return n;},querySelector:s=>s==='#app'?root:body.querySelector(s),querySelectorAll:s=>body.querySelectorAll(s),addEventListener(){}};
 body.append(root);
 const events={},calls=[];
 let drag, bridgeReport;
 const noop=()=>{};
 const context={document,location:{search},URLSearchParams,setTimeout,clearTimeout,nextNotice,
  window:{addEventListener:noop,getSelection:()=>({isCollapsed:true})},
  Events:{On:(name,fn)=>events[name]=fn,Emit:(...args)=>{calls.push(args);return Promise.resolve();}},Window:{},
  createBridge:(send,report)=>{bridgeReport=report;return {emit:async(n,d)=>{await send(`devjay:${n}`,d);report('');},reply:noop};},
  uiReady:()=>noop,installCardSizing:()=>noop,mountEditorSettings:()=>({renderEditor:noop}),
  mountGitViewer:()=>noop,cardStatus:()=>({local:[],remote:[],reviews:[]}),
  mountSuggestedActions:()=>noop,mountViewerSuggestions:()=>noop,
  installPanelDrag:(_list,options)=>{drag=options;},installCardActivation:noop,installDragOriginGuard:noop,installCardHover:noop,
 };
 vm.runInNewContext(source,context);
 return {document,root,calls,reportBridge:(text,error)=>bridgeReport(text,error),get drag(){return drag;},snapshot:s=>events['devjay:snapshot']({data:s})};
}
const site=(id,branch='main')=>({id,path:`/tmp/${id}`,prefix:'/tmp/',name:id,git:{branch},position:{label:'Desktop 1'},window:{}});
const state=sites=>({sites,preferences:{},layout:'horizontal',settingsOpen:true});

test('ending a panel drag renders the latest snapshot even after cancellation',()=>{
 const view=app();
 view.snapshot(state([site('a','old')]));
 view.drag.busy(true);
 view.snapshot(state([site('a','new')]));
 assert.equal(view.root.querySelector('.branch').textContent,'old');
 view.drag.busy(false);
 assert.equal(view.root.querySelector('.branch').textContent,'new');
});

test('keyboard reordering preserves the same Site row focus across snapshots',async()=>{
 const view=app('?settings');
 view.snapshot(state([site('a'),site('b')]));
 const row=view.root.querySelectorAll('.site-row')[0];row.focus();
 row.onkeydown({target:row,key:'ArrowDown',preventDefault(){}});
 await Promise.resolve();
 assert.deepEqual(Array.from(view.calls.find(([name])=>name==='devjay:order')[1]),['b','a']);
 view.snapshot(state([site('b'),site('a')]));
 assert.equal(view.document.activeElement.dataset.id,'a');
 assert.notEqual(view.document.activeElement,row);
});

test('Changes and confirmation views contain the shared error footer',()=>{
 for(const search of ['?viewer=viewer-1','?actions'])assert.ok(app(search).root.querySelector('.notice'));
});

test('snapshot notices clear when resolved in every view',()=>{
 for(const search of ['', '?site=a', '?settings', '?viewer=viewer-1', '?actions']) {
  const view=app(search), snapshot=state([site('a')]);
  const notice=view.root.querySelector('.notice');
  view.snapshot({...snapshot,quitMessage:'Waiting for the confirmed action to finish before quitting…',error:'An earlier error'});
  assert.match(notice.textContent,/Waiting/);
  assert.equal(notice.classList.contains('error'),false);
  view.snapshot(snapshot);
  assert.equal(notice.textContent,'',search);
  view.snapshot({...snapshot,error:'Could not read Sites'});
  assert.equal(notice.textContent,'Could not read Sites');
  assert.equal(notice.classList.contains('error'),true);
  view.snapshot(snapshot);
  assert.equal(notice.textContent,'',search);
  assert.equal(notice.classList.contains('error'),false);
 }
});

test('unrelated snapshots preserve bridge errors until a successful request',()=>{
 const view=app('?settings'), snapshot=state([site('a')]);
 const notice=view.root.querySelector('.notice');
 view.reportBridge('Could not save Sites',true);
 view.snapshot(snapshot);
 assert.equal(notice.textContent,'Could not save Sites');
 assert.equal(notice.classList.contains('error'),true);
 view.reportBridge('',false);
 assert.equal(notice.textContent,'');
 assert.equal(notice.classList.contains('error'),false);
});

test('successful bridge calls do not clear an active snapshot notice',()=>{
 const view=app('?settings');
 view.snapshot({...state([site('a')]),quitMessage:'Finishing the action…'});
 view.reportBridge('',false);
 assert.equal(view.root.querySelector('.notice').textContent,'Finishing the action…');
});
