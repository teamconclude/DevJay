import test from 'node:test';
import assert from 'node:assert/strict';
import {readFile} from 'node:fs/promises';
import vm from 'node:vm';
import {cardStatus} from '../cmd/devjay/frontend/card-status.js';
import {cardMenuTarget,cardBounds} from '../cmd/devjay/frontend/card-menu.js';
import {installCardActivation} from '../cmd/devjay/frontend/card-interaction.js';
import {prCheckIndicator,prTagTone} from '../cmd/devjay/frontend/pr-status.js';
import {nextNotice} from '../cmd/devjay/frontend/notice.js';
import {rebasedPublishWarning} from '../cmd/devjay/frontend/suggested-actions.js';
const navigationSource=(await readFile(new URL('../cmd/devjay/frontend/settings-navigation.js',import.meta.url),'utf8')).replace(/export /g,'');
const profilesSource=(await readFile(new URL('../cmd/devjay/frontend/profile-settings.js',import.meta.url),'utf8')).replace(/export /g,'');
const source=navigationSource+'\n'+profilesSource+'\n'+(await readFile(new URL('../cmd/devjay/frontend/app.js',import.meta.url),'utf8')).replace(/^import .*;\n/gm,'');

// Exercise the real snapshot renderer with a small DOM and the Wails boundary.
function app(search='', actionResult=true) {
 let document, clock=0, readyCount=0;
 class Element {
  constructor(tag){this.tag=tag;this.children=[];this.dataset={};this.style={};this.className='';this.attrs={};this.textContent='';this.listeners={};
   this.classList={add:c=>{this.className+=' '+c;},contains:c=>this.className.split(' ').includes(c),toggle:(c,on)=>{const all=new Set(this.className.split(' '));on?all.add(c):all.delete(c);this.className=[...all].join(' ');}};
  }
  get textContent(){return (this._textContent || '')+this.children.map(child=>child.textContent).join('');}
  set textContent(text){this._textContent=text;for(const child of this.children || [])child.parent=null;this.children=[];}
  append(...nodes){for(const n of nodes){n.remove();n.parent=this;this.children.push(n);}}
  prepend(...nodes){for(const n of [...nodes].reverse())this.insertBefore(n,this.children[0]);}
  remove(){if(this.parent){const a=this.parent.children;a.splice(a.indexOf(this),1);this.parent=null;}}
  replaceChildren(...nodes){if(this.contains(document.activeElement))document.activeElement=document.body;this.textContent='';this.append(...nodes);}
  insertBefore(n,next){n.remove();n.parent=this;const i=this.children.indexOf(next);this.children.splice(i<0?this.children.length:i,0,n);}
  contains(n){return n===this || this.children.some(c=>c.contains(n));}
  setAttribute(k,v){this.attrs[k]=v;}
  addEventListener(name,fn){(this.listeners[name] ||= []).push(fn);}
  matches(s){if(s.includes(','))return s.split(',').some(part=>this.matches(part.trim()));if(s.startsWith('.'))return this.classList.contains(s.slice(1));if(s==='[data-view-kind]')return this.dataset.viewKind!==undefined;if(s==='[data-pr-number]')return this.dataset.prNumber!==undefined;if(s==='[data-app-bundle]')return this.dataset.appBundle!==undefined;if(s==='[data-pref]')return this.dataset.pref!==undefined;if(s==='[data-app-tooltip]')return this.dataset.appTooltip!==undefined;if(s==='[name=sorting]')return this.name==='sorting';return this.tag===s;}
  closest(s){return this.matches(s)?this:this.parent?.closest(s);}
  querySelectorAll(s){return this.children.flatMap(n=>[...(n.matches(s)?[n]:[]),...n.querySelectorAll(s)]);}
  querySelector(s){return this.querySelectorAll(s)[0]||null;}
  getBoundingClientRect(){return {left:10,top:20,width:270,height:140};}
  focus(){document.activeElement=this;}
  select(){this.selectionStart=0;this.selectionEnd=(this.value||'').length;}
  get firstElementChild(){return this.children[0];}
 }
 const root=new Element('main'),body=new Element('body');
 document={body,activeElement:body,createElement:tag=>new Element(tag),createElementNS:(_namespace,tag)=>new Element(tag),createTextNode:text=>{const n=new Element('text');n.textContent=text;return n;},querySelector:s=>s==='#app'?root:body.querySelector(s),querySelectorAll:s=>body.querySelectorAll(s),addEventListener(){}};
 body.append(root);
 const events={},calls=[];
 let drag, bridgeReport;
 const noop=()=>{};
 const context={prCheckIndicator,prTagTone,document,location:{search},URLSearchParams,setTimeout,clearTimeout,nextNotice,performance:{now:()=>clock},
  window:{addEventListener:noop,getSelection:()=>({isCollapsed:true})},
  Events:{On:(name,fn)=>events[name]=fn,Emit:(...args)=>{calls.push(args);return Promise.resolve();}},Window:{},
  createBridge:(send,report)=>{bridgeReport=report;return {emit:async(n,d)=>{await send(`devjay:${n}`,d);report(actionResult===false?'Delivery failed':'',actionResult===false);return actionResult;},reply:noop};},
  uiReady:()=>()=>readyCount++,installCardSizing:()=>noop,installPathLabels:()=>noop,mountEditorSettings:()=>({renderEditor:noop}),
  mountGitViewer:()=>noop,cardStatus,rebasedPublishWarning,cardBounds,
  mountSuggestedActions:()=>noop,mountViewerSuggestions:()=>noop,
  installPanelDrag:(_list,options)=>{drag=options;},installCardActivation,installDragOriginGuard:noop,installCardHover:noop,installCardMenu:noop,
 };
 vm.runInNewContext(source,context);
 return {document,root,calls,get readyCount(){return readyCount;},advance:ms=>{clock+=ms;},reportBridge:(text,error)=>bridgeReport(text,error),event:(name,data)=>events[name]?.({data}),get drag(){return drag;},snapshot:s=>events['devjay:snapshot']({data:s}),click:target=>{
  const event={button:0,defaultPrevented:false,target};
  for(let node=target;node;node=node.parent)for(const listener of node.listeners.click || [])listener(event);
 }};
}
const site=(id,branch='main')=>({id,path:`/tmp/${id}`,prefix:'/tmp/',name:id,git:{branch},position:{label:'Desktop 1'},window:{}});
const state=sites=>({sites,preferences:{},layout:'horizontal',settingsOpen:true});
const appLabels=view=>view.root.querySelectorAll('.development-app-group').map(group=>group.attrs['aria-label']);

test('Settings sidebar switches pages without rebuilding their controls',()=>{
 const view=app('?settings');view.snapshot(state([site('a')]));
 const pages=view.root.querySelectorAll('.settings-page'),buttons=view.root.querySelectorAll('.settings-nav-item');
 assert.equal(pages.length,5);assert.equal(pages.filter(p=>!p.hidden).length,1);
 const row=view.root.querySelector('.site-row');
 buttons.find(b=>b.textContent==='Windows & Cards').onclick();
 assert.equal(pages.filter(p=>!p.hidden)[0].id,'settings-page-windows');
 buttons.find(b=>b.textContent==='Sites').onclick();
 assert.equal(view.root.querySelector('.site-row'),row);
 assert.equal(buttons.find(b=>b.textContent==='Sites').attrs['aria-current'],'page');
 view.event('devjay:settings-page','profiles');
 assert.equal(pages.filter(p=>!p.hidden)[0].id,'settings-page-profiles');
});

test('Locate folder retains the Site ID and renamed folders update Settings labels',async()=>{
 const view=app('?settings'),item={...site('a'),folderMissing:true};view.snapshot(state([item]));
 view.root.querySelector('.locate-site').onclick();await Promise.resolve();
 assert.equal(view.calls.find(([name])=>name==='devjay:locate-folder')[1],'a');
 assert.match(view.root.querySelector('.row-status').textContent,/Folder missing/);
 view.snapshot(state([{...item,path:'/tmp/renamed',name:'renamed',folderMissing:false}]));
 assert.match(view.root.querySelector('.path').textContent,/renamed/);
 assert.equal(view.root.querySelector('.site-row').dataset.id,'a');
});

test('Profile scope preserves the active Profile and sends only selected target IDs',async()=>{
 const view=app('?settings'),profiles={current:'home',profiles:[{id:'home',name:'Home'},{id:'cabin',name:'Cabin'}],targets:['home'],displays:[],automatic:true};
 view.snapshot({...state([site('a')]),profiles});
 const boxes=view.root.querySelector('.profile-scope-options').querySelectorAll('input');
 assert.equal(boxes[0].checked,true);assert.equal(boxes[0].disabled,true);
 boxes[1].checked=true;boxes[1].onchange();await Promise.resolve();
 const request=view.calls.find(([name,data])=>name==='devjay:profile'&&data.operation==='scope')[1];
 assert.deepEqual(Array.from(request.targets),['home','cabin']);
});

const profileState={current:'home',profiles:[{id:'home',name:'Home'},{id:'office',name:'Office'}],targets:['home'],displays:[],automatic:true};
test('New profile dialog requires a name, survives snapshots, and saves only on submit',async()=>{
 const view=app('?settings');view.snapshot({...state([]),profiles:profileState});
 const intro=view.root.querySelector('.profile-intro');intro.querySelector('button').onclick();
 const dialog=view.document.body.querySelector('.profile-dialog'),input=dialog.querySelector('input'),form=dialog.querySelector('form');
 const submit=dialog.querySelectorAll('button').find(b=>b.type==='submit');
 assert.equal(view.document.body.querySelector('.profile-modal').hidden,false);assert.equal(view.document.activeElement,input);assert.equal(submit.disabled,true);
 input.value='   ';await form.onsubmit({preventDefault(){}});assert.equal(view.calls.filter(([name])=>name==='devjay:profile').length,0);
 input.value=' Laptop ';input.oninput();assert.equal(submit.disabled,false);
 view.snapshot({...state([]),profiles:{...profileState,targets:['home','office']}});
 assert.equal(input.value,' Laptop ');assert.equal(view.calls.filter(([name])=>name==='devjay:profile').length,0);
 await form.onsubmit({preventDefault(){}});
 assert.equal(view.document.body.querySelector('.profile-modal').hidden,true);
 const sent=view.calls.find(([name])=>name==='devjay:profile')[1];assert.equal(sent.operation,'create');assert.equal(sent.name,'Laptop');
 assert.equal(view.root.querySelector('.profile-active'),null);
 assert.equal(view.root.querySelectorAll('select').find(s=>s.attrs['aria-label']==='Active profile').value,'home');
});

test('Manage profiles renames the chosen row independently of the selector and retains failed edits',async()=>{
 const view=app('?settings',false);view.snapshot({...state([]),profiles:profileState});
 const row=view.root.querySelectorAll('.profile-list-row')[1];row.querySelectorAll('button')[0].onclick();
 const dialog=view.document.body.querySelector('.profile-dialog'),input=dialog.querySelector('input');assert.equal(input.value,'Office');
 input.value='Studio';await dialog.querySelector('form').onsubmit({preventDefault(){}});
 const sent=view.calls.find(([name])=>name==='devjay:profile')[1];assert.equal(sent.id,'office');assert.equal(sent.operation,'rename');assert.equal(sent.name,'Studio');
 assert.equal(view.document.body.querySelector('.profile-modal').hidden,false);assert.equal(input.value,'Studio');assert.match(dialog.querySelector('.error').textContent,/Could not save/);
 assert.equal(dialog.querySelectorAll('button')[0].disabled,false);
});

test('Profile deletion protects the active row and requires a separate confirmation',async()=>{
 const view=app('?settings');view.snapshot({...state([]),profiles:profileState});
 const rows=view.root.querySelectorAll('.profile-list-row');assert.equal(rows[0].querySelectorAll('button')[1].disabled,true);
 const remove=rows[1].querySelectorAll('button')[1];assert.equal(remove.disabled,false);remove.onclick();
 const dialog=view.document.body.querySelector('.profile-dialog');assert.match(dialog.textContent,/Delete “Office”/);assert.equal(dialog.querySelector('input').parent.hidden,true);assert.equal(view.calls.filter(([name])=>name==='devjay:profile').length,0);
 dialog.querySelectorAll('button')[0].onclick();assert.equal(view.document.body.querySelector('.profile-modal').hidden,true);assert.equal(view.calls.filter(([name])=>name==='devjay:profile').length,0);
 remove.onclick();await dialog.querySelector('form').onsubmit({preventDefault(){}});
 const sent=view.calls.find(([name])=>name==='devjay:profile')[1];assert.equal(sent.id,'office');assert.equal(sent.operation,'delete');assert.equal(view.document.body.querySelector('.profile-modal').hidden,true);
});

test('Profile dialog traps Tab and closes on Escape without closing Settings',()=>{
 const view=app('?settings');view.snapshot({...state([]),profiles:profileState});
 view.root.querySelector('.profile-intro').querySelector('button').onclick();
 const dialog=view.document.body.querySelector('.profile-dialog'),modal=dialog.parent;
 const key=dialog.listeners.keydown[0],cancel=dialog.querySelectorAll('button')[0];
 assert.equal(view.root.inert,true);
 key({key:'Tab',shiftKey:true,preventDefault(){}});assert.equal(view.document.activeElement,cancel);
 key({key:'Tab',shiftKey:false,preventDefault(){}});assert.equal(view.document.activeElement,dialog.querySelector('input'));
 let stopped=false;key({key:'Escape',preventDefault(){},stopPropagation(){stopped=true;}});
 assert.equal(stopped,true);assert.equal(modal.hidden,true);assert.equal(view.root.inert,false);
 assert.equal(view.calls.filter(([name])=>name==='devjay:settings-close').length,0);
});

test('held shortcut hints follow card order, skip unavailable bindings and never navigate',()=>{
 const view=app(),items=Array.from({length:10},(_,i)=>site(`site-${i}`));
 const badges=()=>view.root.querySelectorAll('.card').map(card=>card.dataset.shortcut);
 view.snapshot(state(items));
 assert.deepEqual(badges(),Array(10).fill(''));
 const title=view.root.querySelector('.card-title');
 view.snapshot({...state(items),shortcutHints:0x1ff & ~(1<<2)});
 assert.deepEqual(badges(),['1','2','','4','5','6','7','8','9','']);
 assert.equal(view.root.querySelector('.card-title'),title); // No card content rebuild or focus change.
 assert.equal(view.document.activeElement,view.document.body);
 view.snapshot({...state([...items].reverse()),shortcutHints:0x1ff});
 assert.equal(view.root.querySelectorAll('.card')[0].dataset.id,'site-9');
 assert.deepEqual(badges(),['1','2','3','4','5','6','7','8','9','']);
 view.snapshot(state([...items].reverse()));
 assert.deepEqual(badges(),Array(10).fill(''));
 assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
});

test('shortcut release while invisible clears stale hints when the full strip returns',()=>{
 const view=app(),items=[site('a')];
 view.snapshot({...state(items),shortcutHints:1});
 view.event('devjay:visibility',false);
 view.snapshot({...state(items),shortcutHints:0});
 view.event('devjay:visibility',true);
 assert.equal(view.root.querySelector('.card').dataset.shortcut,'');
});

test('desktop Site and panel branch labels do not open the branch chooser',async()=>{
 const item=site('a','feature/topic');item.git.head='current-head';
 for(const search of ['?site=a','']) {
  const view=app(search);view.snapshot(state([item]));
  assert.equal(view.root.querySelector('.branch-switch'),null);
  const branch=view.root.querySelector('.branch');
  assert.equal(branch.tag,'div');assert.equal(branch.textContent,'feature/topic');
  assert.equal(cardMenuTarget(branch).kind,'auto');
  assert.equal(cardMenuTarget(view.root.querySelector('.path')).kind,'auto');
  if(!search){branch.closest('button').onclick();view.click(branch);await Promise.resolve();}
  assert.equal(view.calls.some(([name])=>name==='devjay:branches-open'),false);
  assert.deepEqual(view.calls.filter(([name])=>name==='devjay:activate'),search?[]:[['devjay:activate','a']]);
 }
});

test('metric headings stay noninteractive while counts keep their viewer and browser actions',async()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  item.git={branch:'main',changes:2,ahead:1,behind:0,github:'PR #12 · Approved',pullRequests:[{number:12}],reviews:[{number:12,blockers:1,warnings:0}]};
  view.snapshot(state([item]));
  const labels=view.root.querySelectorAll('.metric-label');
  assert.deepEqual(labels.map(node=>node.textContent || node.attrs['aria-label']),['Edits','Branch','Review']);
  for(const [index,label] of labels.entries()) {
   const name=['Edits','Branch','Review'][index];
   assert.equal(label.textContent,'');assert.equal(label.attrs.role,'img');
   assert.equal(label.title,name);assert.equal(label.dataset.appTooltip,name);
   assert.equal(label.querySelector('svg').attrs['aria-hidden'],'true');
  }
  assert.match(view.root.querySelector('.card-status').textContent,/2edits/);
  for(const label of labels){assert.equal(label.tag,'span');assert.equal(label.onclick,undefined);}
  const counts=view.root.querySelectorAll('.metric');
  for(const count of counts){await count.onclick();view.click(count);}
  assert.deepEqual(counts.map(node=>{const t=cardMenuTarget(node);return [t.kind,t.number];}),[['edits',0],['branch',0],['site',12]]);
  assert.deepEqual(view.calls.filter(([name])=>name==='devjay:viewer-open').map(([,req])=>req.kind),['edits','branch']);
  assert.equal(view.calls.filter(([name])=>name==='devjay:pr-open').length,1);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('only the newer-default-branch line adds rebase context while clicks still open history',async()=>{
 for(const search of ['', '?site=a']) for(const branch of ['develop','main']) {
  const view=app(search), item=site('a','feature');
  item.git={...item.git,head:'head',ahead:2,base:{branch,tracking:`origin/${branch}`,behind:3}};
  view.snapshot(state([item]));
  const row=view.root.querySelectorAll('.status-line').find(node=>node.dataset.baseBranch);
  assert.match(row.textContent,/3 newer commits/);
  assert.equal(cardMenuTarget(row.children[1]).baseBranch,branch);
  assert.equal(cardMenuTarget(row.children[0]).kind,'branch');
  assert.equal(cardMenuTarget(view.root.querySelector('.metric')).baseBranch,undefined);
  assert.equal(cardMenuTarget(view.root.querySelector('.branch')).baseBranch,undefined);
  await row.onclick();
  assert.equal(view.calls.find(([name])=>name==='devjay:viewer-open')[1].kind,'branch');
  assert.equal(view.calls.some(([name])=>name==='devjay:action-prepare'||name==='devjay:action-confirm'),false);
 }
});

test('incoming and outgoing metrics carry their own menu action without changing ordinary clicks',async()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search),item=site('a','feature');
  item.git={...item.git,head:'head',ahead:2,behind:3,base:{branch:'develop',tracking:'origin/develop',behind:4}};
  view.snapshot(state([item]));
  const metrics=view.root.querySelectorAll('.metric');
  assert.deepEqual(metrics.map(node=>cardMenuTarget(node.children.at(-1)).gitAction),['push','update']);
  assert.equal(cardMenuTarget(view.root.querySelector('.branch')).gitAction,undefined);
  for(const node of metrics) await node.onclick();
  assert.deepEqual(view.calls.filter(([name])=>name==='devjay:viewer-open').map(([,req])=>req.kind),['branch','branch']);
  assert.equal(view.calls.some(([name])=>name==='devjay:action-prepare'||name==='devjay:action-confirm'),false);
 }
});

test('Changes and History clicks send the originating card bounds for placement',async()=>{
 for (const search of ['', '?site=a']) {
  const view=app(search), item=site('a','feature');
  item.git={...item.git,changes:2,base:{branch:'develop',tracking:'origin/develop',behind:3}};
  view.snapshot(state([item]));
  const controls=[view.root.querySelector('.metric'),view.root.querySelectorAll('.status-line').find(n=>n.textContent.includes('newer commits'))];
  for(const control of controls) await control.onclick();
  const requests=JSON.parse(JSON.stringify(view.calls.filter(([name])=>name==='devjay:viewer-open').map(([,data])=>data)));
  assert.deepEqual(requests.map(r=>r.kind),['edits','branch']);
  for(const request of requests) assert.deepEqual(request.anchor,{x:10,y:20,width:270,height:140});
 }
});

test('rebased status offers publication context while its ordinary click opens history',async()=>{
 const view=app(''),item=site('a','feature');
 item.git={...item.git,head:'head',rebased:true,ahead:2,behind:1,pushTarget:'origin/feature'};
 view.snapshot(state([item]));
 const row=view.root.querySelectorAll('.status-line').find(node=>node.dataset.publishRebased);
 assert.equal(cardMenuTarget(row.children[1]).publishRebased,true);
 assert.equal(cardMenuTarget(row.children[0]).kind,'branch');
 assert.equal(cardMenuTarget(view.root.querySelector('.branch')).publishRebased,undefined);
 await row.onclick();
 assert.equal(view.calls.find(([name])=>name==='devjay:viewer-open')[1].kind,'branch');
 assert.equal(view.calls.some(([name])=>name==='devjay:action-prepare'||name==='devjay:action-confirm'),false);
});

test('Site rebased push button prepares only, follows busy state, and is absent from strip',async()=>{
 const item=site('a','feature');
 item.git={...item.git,head:'head',rebased:true,tracking:'origin/feature',pushTarget:'origin/feature'};
 const view=app('?site=a');view.snapshot(state([item]));
 const button=view.root.querySelector('.site-publish');
 assert.equal(button.textContent,'Push rebased branch…');assert.equal(button.disabled,false);
 assert.equal(cardMenuTarget(button).publishButton,true);
 const pending=button.onclick();
 button.onclick();
 view.snapshot(state([item]));
 assert.equal(view.root.querySelector('.site-publish').disabled,true,'snapshot cannot unlock an in-flight request');
 await pending;
 assert.deepEqual(JSON.parse(JSON.stringify(view.calls.filter(([name])=>name==='devjay:action-prepare'))),[['devjay:action-prepare',{siteID:'a',kind:'publish',branch:'feature',head:'head',deleteBranch:false,anchor:{x:10,y:20,width:270,height:140}}]]);
 assert.equal(view.calls.some(([name])=>name==='devjay:action-confirm'||name==='devjay:activate'),false);
 for(const status of ['preparing','running']) {
  view.snapshot({...state([item]),action:{siteID:'other',status}});
  assert.equal(view.root.querySelector('.site-publish').disabled,true);
 }
 view.snapshot({...state([item]),action:{siteID:'a',status:'failed'}});
 assert.equal(view.root.querySelector('.site-publish').disabled,false);
 const strip=app('');strip.snapshot(state([item]));assert.equal(strip.root.querySelector('.site-publish'),null);
 view.snapshot(state([{...item,git:{...item.git,rebased:false}}]));assert.equal(view.root.querySelector('.site-publish'),null);
});

test('Site publish button shows blockers and recovers from failed request delivery',async()=>{
 const item=site('a','feature');
 item.git={...item.git,head:'head',rebased:true,tracking:'origin/feature',pushTarget:'origin/feature'};
 const view=app('?site=a',false);
 for(const change of [{changes:1},{conflicts:1},{localNote:'unavailable'},{pushTarget:''},{head:''},{isMainBranch:true},{canReplaceRemote:true}]) {
  view.snapshot(state([{...item,git:{...item.git,...change}}]));
  const button=view.root.querySelector('.site-publish');
  assert.equal(button.disabled,true);assert.ok(button.title);
  await button.onclick();
 }
 assert.equal(view.calls.some(([name])=>name==='devjay:action-prepare'),false);
 view.snapshot(state([item]));await view.root.querySelector('.site-publish').onclick();
 assert.equal(view.root.querySelector('.site-publish').disabled,false);
 assert.match(view.root.querySelector('.notice').textContent,/Delivery failed/);
});

test('ending a panel drag renders the latest snapshot even after cancellation',()=>{
 const view=app();
 view.snapshot(state([site('a','old')]));
 view.drag.busy(true);
 view.snapshot(state([site('a','new')]));
 assert.equal(view.root.querySelector('.branch').textContent,'old');
 view.drag.busy(false);
 assert.equal(view.root.querySelector('.branch').textContent,'new');
});

test('hidden windows defer updates, pause motion, and render the latest state when visible',()=>{
 const view=app();
 view.snapshot(state([site('a','initial')]));
 view.event('devjay:visibility',false);
 assert.equal(view.document.body.dataset.monitorVisible,'false');
 view.snapshot(state([site('a','intermediate')]));
 view.snapshot(state([site('a','latest')]));
 assert.equal(view.root.querySelector('.branch').textContent,'initial');
 view.event('devjay:visibility',true);
 assert.equal(view.document.body.dataset.monitorVisible,'true');
 assert.equal(view.root.querySelector('.branch').textContent,'latest');
});

test('an initially hidden document still renders its first recovery snapshot',()=>{
 const view=app('?site=a');
 view.event('devjay:visibility',false);
 view.snapshot(state([site('a','initial')]));
 assert.equal(view.root.querySelector('.branch').textContent,'initial');
 assert.equal(view.document.body.dataset.monitorVisible,'false');
 assert.equal(view.readyCount,1,'first hidden render acknowledges recovery');
 view.snapshot(state([site('a','later')]));
 assert.equal(view.readyCount,1,'deferred updates do not acknowledge another render');
 assert.equal(view.root.querySelector('.branch').textContent,'initial');
 view.event('devjay:visibility',true);
 assert.equal(view.root.querySelector('.branch').textContent,'later');
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


test('PR tags live in card headers, open the PR, and disappear when the PR is gone',async()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  item.git={branch:'feature',github:'PR #1234 · Approved',pullRequests:[{number:1234,url:'https://github.com/a/b/pull/1234',state:'OPEN'}]};
  view.snapshot(state([item]));
  const tag=view.root.querySelector('.card-heading').querySelector('.pr-tag');
  assert.equal(tag.textContent,'#1234');assert.equal(tag.dataset.prNumber,'1234');
  assert.equal(view.root.querySelector('.card-settings'),null);
  await tag.onclick();
  const request=view.calls.find(([name])=>name==='devjay:pr-open')[1];
  assert.equal(request.siteID,'a');assert.equal(request.number,1234);
  assert.deepEqual(JSON.parse(JSON.stringify(request.anchor)),{x:10,y:20,width:270,height:140});
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'||name==='devjay:viewer-open'),false);
  view.snapshot(state([{...item,git:{branch:'feature',github:'No open pull request'}}]));
  assert.equal(view.root.querySelector('.pr-tag'),null);
  assert.equal(view.root.querySelector('.pr-status'),null);
 }
});

test('failed CI marks only its PR tag, preserves review state, and opens the same PR',async()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  item.git={branch:'feature',github:'PR #1234 · Approved; PR #1235 · Review required',pullRequests:[
   {number:1234,state:'Approved',ciState:'failure'},
   {number:1235,state:'Review required',ciState:'success'},
  ]};
  view.snapshot({...state([item]),preferences:{showIDEStatus:false}});
  const [failed,passed]=view.root.querySelectorAll('.pr-tag');
  assert.equal(failed.textContent,'× #1234');
  assert.equal(failed.classList.contains('ci-failed'),true);
  assert.equal(failed.dataset.appTooltip,'PR #1234 · Approved · CI failed');
  assert.equal(failed.attrs['aria-label'],'Open PR #1234 · Approved · CI failed in browser');
  assert.equal(passed.textContent,'#1235');assert.equal(passed.classList.contains('ci-failed'),false);
  assert.equal(passed.dataset.appTooltip,'PR #1235 · Review required · CI passed');
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'||name==='devjay:pr-open'),false);
  await failed.onclick();view.click(failed);
  assert.equal(view.calls.filter(([name])=>name==='devjay:pr-open').length,1);
  assert.equal(view.calls.find(([name])=>name==='devjay:pr-open')[1].number,1234);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'||name==='devjay:viewer-open'),false);
 }
});

test('CI-only updates clear failure on rerun or success and refresh the native PR tooltip',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  const show=ciState=>view.snapshot(state([{...item,git:{branch:'feature',pullRequests:[{number:1234,state:'Open',ciState}]}}]));
  const tag=()=>view.root.querySelector('.pr-tag');
  const refreshes=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
  show('failure');const first=tag();assert.equal(refreshes(),1);
  show('failure');assert.equal(tag(),first);assert.equal(refreshes(),1);
  show('pending');assert.notEqual(tag(),first);assert.equal(tag().textContent,'◷ #1234');
  assert.equal(tag().classList.contains('ci-failed'),false);assert.equal(refreshes(),2);
  assert.match(tag().dataset.appTooltip,/CI running or queued/);
  show('success');assert.equal(tag().textContent,'#1234');assert.equal(tag().classList.contains('ci-failed'),false);
  assert.equal(refreshes(),3);assert.match(tag().attrs['aria-label'],/CI passed/);
  show('unknown');assert.doesNotMatch(tag().attrs['aria-label'],/passed|failed/);
  assert.equal(tag().classList.contains('ci-failed'),false);assert.equal(refreshes(),4);
  view.snapshot(state([item]));assert.equal(tag(),null);assert.equal(refreshes(),5);
 }
});

test('CI outcomes distinguish action required, cancellation, neutral checks, and missing data',()=>{
 const view=app(),item=site('a');
 const cases=[
  ['action_required','! #1234','CI needs attention',true],
  ['cancelled','⊘ #1234','CI cancelled',false],
  ['neutral','#1234','CI checks skipped or neutral',false],
  ['none','#1234','No CI checks reported',false],
  [undefined,'#1234','CI status unavailable',false],
  ['unexpected','#1234','CI status unavailable',false],
 ];
 for(const [ciState,text,label,red] of cases) {
  view.snapshot(state([{...item,git:{pullRequests:[{number:1234,state:'Draft',isDraft:true,ciState}]}}]));
  const tag=view.root.querySelector('.pr-tag');
  assert.equal(tag.textContent,text);assert.equal(tag.classList.contains('ci-failed'),red);
  assert.equal(tag.dataset.appTooltip,`PR #1234 · Draft · ${label}`);
 }
 view.snapshot(state([{...item,git:{pullRequests:[{number:1234,state:'Merged'}]}}]));
 assert.equal(view.root.querySelector('.pr-tag').dataset.appTooltip,'PR #1234 · Merged');
});

test('edit and commit badges retain their category for click and context menus',async()=>{
 const view=app(),item=site('a');
 item.git={branch:'feature',ahead:2,behind:3,changes:25};view.snapshot(state([item]));
 const badges=view.root.querySelectorAll('.metric');
 assert.deepEqual(badges.map(n=>n.dataset.viewKind),['edits','branch','branch']);
 for(const badge of badges)await badge.onclick();
 assert.deepEqual(view.calls.filter(([name])=>name==='devjay:viewer-open').map(([,data])=>data.kind),['edits','branch','branch']);
});


test('PR navigation information appears only in the requesting card window',()=>{
 const panel=app(),siteView=app('?site=a');
 const result={siteID:'a',number:1234,sender:'control-panel',message:'Opened in your default browser.'};
 panel.event('devjay:pr-open-result',result);siteView.event('devjay:pr-open-result',result);
 assert.equal(panel.root.querySelector('.notice').textContent,result.message);
 assert.equal(panel.root.querySelector('.notice').classList.contains('error'),false);
 assert.equal(siteView.root.querySelector('.notice').textContent,'');
});

test('development app indicators update independently of Git and distinguish unavailable from no match',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  const indicator=()=>view.root.querySelector('.development-app');
  view.snapshot(state([item]));
  assert.equal(indicator().textContent,'App unknown');
  assert.equal(indicator().closest('[data-app-tooltip]'),undefined);
  const goLand={name:'GoLand',bundleID:'com.jetbrains.goland'};
  const iTerm={name:'iTerm',bundleID:'com.googlecode.iterm2'};
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[goLand]}]));
  assert.deepEqual(appLabels(view),['GoLand']);
  assert.equal(indicator().querySelector('[data-app-tooltip]').dataset.appTooltip,'GoLand');
  const previousIndicator=indicator();
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[goLand]}]));
  assert.equal(indicator(),previousIndicator,'unchanged readings keep the current DOM');
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[goLand,iTerm]}]));
  assert.deepEqual(appLabels(view),['GoLand','iTerm']);
  assert.deepEqual(indicator().querySelectorAll('[data-app-tooltip]').map(node=>node.dataset.appTooltip),['GoLand','iTerm']);
  assert.equal(indicator().parent.children.at(-1),indicator());
  assert.equal(indicator().tag,'div');
  assert.equal(indicator().onclick,undefined);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false,'observations never activate a Site');
  view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:[]}]));
  assert.equal(indicator().textContent,'No app detected');
  assert.equal(indicator().querySelector('[data-app-tooltip]'),null);
  view.snapshot(state([{...item,developmentAppsKnown:false,developmentApps:[goLand]}]));
  assert.equal(indicator().textContent,'App unknown','unavailable readings do not display stale app names');
 }
});

test('agent presence updates without app or Git changes and keeps full app and agent names in both views',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item=site('a');
  const goLand={name:'GoLand',bundleID:'com.jetbrains.goland'};
  const indicator=()=>view.root.querySelector('.development-app');
  const show=apps=>view.snapshot(state([{...item,developmentAppsKnown:true,developmentApps:apps}]));
  show([goLand]);
  assert.deepEqual(appLabels(view),['GoLand']);
  const before=indicator();
  show([{...goLand,agents:['Claude Code']}]);
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  assert.notEqual(indicator(),before,'agent-only changes refresh the indicator');
  const unchanged=indicator();
  show([{...goLand,agents:['Claude Code']}]);
  assert.equal(indicator(),unchanged,'unchanged agent readings preserve the DOM');
  show([
   {...goLand,agents:['Claude Code','Codex']},
   {name:'iTerm2',bundleID:'com.googlecode.iterm2',agents:['Codex']},
   {name:'ChatGPT',bundleID:'com.openai.chat'},
   {name:'Claude',bundleID:'com.anthropic.claudefordesktop',agents:[]},
  ]);
  assert.deepEqual(appLabels(view),['GoLand with Claude Code and Codex','iTerm2 with Codex']);
  assert.deepEqual(indicator().querySelectorAll('[data-app-tooltip]').map(node=>node.dataset.appTooltip),['GoLand with Claude Code and Codex','iTerm2 with Codex']);
  assert.doesNotMatch(indicator().textContent,/working|waiting|idle|blocked/i);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false,'presence updates do not activate a Site');
  show([{...goLand,agents:[]}]);
  assert.deepEqual(appLabels(view),['GoLand'],'cleared agent readings do not retain stale labels');
 }
});

test('recognized agent pairs hide unpaired apps without mutating observations and restore all apps when pairs disappear',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true};
  const observed=Object.freeze([
   Object.freeze({name:'ChatGPT'}),
   Object.freeze({name:'GoLand',agents:Object.freeze(['Claude Code'])}),
   Object.freeze({name:'VS Code',agents:Object.freeze(['Other Agent'])}),
   Object.freeze({name:'iTerm2',agents:Object.freeze(['Codex'])}),
   Object.freeze({name:'Claude',agents:Object.freeze([])}),
  ]);
  const before=JSON.stringify(observed);
  view.snapshot(state([{...item,developmentApps:observed}]));
  assert.deepEqual(appLabels(view),['GoLand with Claude Code','iTerm2 with Codex']);
  assert.equal(JSON.stringify(observed),before,'filtering leaves snapshot apps and agent arrays intact');
  const unpaired=observed.map(app=>app.name==='GoLand'||app.name==='iTerm2'?{...app,agents:[]}:app);
  view.snapshot(state([{...item,developmentApps:unpaired}]));
  assert.deepEqual(appLabels(view),['ChatGPT','GoLand','VS Code with Other Agent','iTerm2','Claude'],'unknown agents alone do not hide other observed apps');
  view.snapshot(state([{...item,developmentApps:observed}]));
  assert.deepEqual(appLabels(view),['GoLand with Claude Code','iTerm2 with Codex'],'returning pairs restore the filtered groups in original order');
  view.snapshot(state([{...item,developmentAppsKnown:false,developmentApps:observed}]));
  assert.equal(view.root.querySelector('.development-app').textContent,'App unknown');
  assert.deepEqual(appLabels(view),[],'unknown observations never show stale paired apps');
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('IDE status setting defaults on and emits boolean preference changes',async()=>{
 const view=app('?settings'), snapshot=state([site('a')]);
 view.snapshot(snapshot);
 const checkbox=view.root.querySelectorAll('[data-pref]').find(node=>node.dataset.pref==='showIDEStatus');
 assert.ok(checkbox);
 assert.equal(checkbox.parent.children[1].textContent,'Show development app status');
 assert.equal(checkbox.checked,true,'snapshots without the new preference keep status enabled');
 checkbox.checked=false;await checkbox.onchange();
 view.snapshot({...snapshot,preferences:{showIDEStatus:false}});
 assert.equal(checkbox.checked,false);
 checkbox.checked=true;await checkbox.onchange();
 view.snapshot({...snapshot,preferences:{showIDEStatus:true}});
 assert.equal(checkbox.checked,true);
 assert.deepEqual(view.calls.filter(([name])=>name==='devjay:showIDEStatus'),[
  ['devjay:showIDEStatus',false],['devjay:showIDEStatus',true],
 ]);
});

test('IDE status can be hidden and restored without app or Git changes in both card views',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  const snapshot=state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',bundleID:'com.jetbrains.goland',agents:['Claude Code']}]}]);
  view.snapshot(snapshot);
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  view.snapshot({...snapshot,preferences:{showIDEStatus:false}});
  assert.equal(view.root.querySelector('.development-app'),null);
  assert.equal(view.root.querySelector('.branch').textContent,'main');
  view.snapshot({...snapshot,preferences:{showIDEStatus:true}});
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

const appIcon='data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+aF9sAAAAASUVORK5CYII=';

test('app and nested agent icons activate their host and use the same context-menu identity',async()=>{
 for(const search of ['', '?site=a']) for(const icon of [appIcon,undefined]) {
  const view=app(search);
  view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'iTerm2',bundleID:'com.googlecode.iterm2',agents:['Codex'],icon}]}]));
  const indicator=view.root.querySelector('.development-app');
  const group=indicator.querySelector('button');
  assert.equal(indicator.onclick,undefined);
  assert.equal(group.attrs['aria-description'],'Show iTerm2');
  const targets=[group,...indicator.querySelectorAll('img'),...indicator.querySelectorAll('.development-app-initials')];
  for(const target of targets) {
   const menu=cardMenuTarget(target);
   assert.equal(menu.siteID,'a');assert.equal(menu.bundleID,'com.googlecode.iterm2');
   await target.closest('button').onclick();view.click(target);
  }
  const requests=view.calls.filter(([name])=>name==='devjay:app-activate');
  assert.equal(requests.length,targets.length);
  for(const [,req] of requests){assert.equal(req.siteID,'a');assert.equal(req.bundleID,'com.googlecode.iterm2');}
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'||name==='devjay:viewer-open'),false);
  view.click(indicator);
  assert.equal(view.calls.filter(([name])=>name==='devjay:activate').length,search?0:1,'row whitespace keeps desktop navigation');
 }
});

test('legacy Detailed preference is ignored and Settings exposes only the app-status toggle',()=>{
 const settings=app('?settings');
 settings.snapshot({...state([site('a')]),appStatusView:'detailed'});
 assert.equal(settings.root.querySelectorAll('select').some(node=>node.attrs['aria-label']==='App status view'),false);
 assert.equal(settings.calls.some(([name])=>name==='devjay:app-status-view'),false);
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],icon:appIcon}]};
  view.snapshot(state([item]));
  const indicator=view.root.querySelector('.development-app');
  const refreshes=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
  assert.equal(indicator.querySelectorAll('img').length,2);
  assert.equal(refreshes(),1);
  view.snapshot({...state([item]),appStatusView:'detailed'});
  assert.equal(view.root.querySelector('.development-app'),indicator,'legacy appearance does not rebuild the icons');
  assert.deepEqual(appLabels(view),['GoLand with Claude Code']);
  assert.equal(refreshes(),1,'ignored appearance does not trigger tooltip refreshes');
 }
});

test('agent changes retain or remove the correct capsule in both views',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],icon:appIcon}]};
  const indicator=()=>view.root.querySelector('.development-app');
  view.snapshot(state([item]));
  assert.equal(indicator().querySelectorAll('img').length,2);
  assert.equal(indicator().querySelector('.development-app-group').classList.contains('has-agents'),true);
  const original=indicator();
  view.snapshot(state([item]));assert.equal(indicator(),original);
  const changed={...item,developmentApps:[{...item.developmentApps[0],agents:[]}]};
  view.snapshot(state([changed]));
  assert.equal(indicator().querySelectorAll('img').length,1);
  assert.equal(indicator().querySelector('.development-app-group').classList.contains('has-agents'),false);
  view.snapshot({...state([changed]),preferences:{showIDEStatus:false}});assert.equal(indicator(),null);
  view.snapshot(state([changed]));assert.equal(indicator().querySelectorAll('img').length,1);
 }
});

test('host icons accept only PNG data and decorative icons fall back without changing group identity',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true};
  const icons=[appIcon,undefined,'https://example.com/icon.png','file:///Applications/App.app/icon.png','data:image/svg+xml;base64,PHN2Zz4=','data:image/png;base64,invalid!'];
  view.snapshot(state([{...item,developmentApps:icons.map((icon,index)=>({name:index===0?'GoLand':'Visual Studio',icon}))}]));
  const slots=view.root.querySelectorAll('.development-app-icon');
  assert.equal(slots.length,icons.length);
  assert.equal(view.root.querySelectorAll('img').length,1,'external, file and non-PNG host sources are never loaded');
  const image=slots[0].querySelector('img'), fallback=slots[0].querySelector('.development-app-initials');
  const group=slots[0].closest('[data-app-tooltip]');
  assert.equal(group.attrs['aria-label'],'GoLand');
  assert.equal(image.src,appIcon);assert.equal(image.alt,'');assert.equal(image.draggable,false);
  assert.equal(image.attrs['aria-hidden'],'true');assert.equal(slots[0].attrs['aria-hidden'],'true');
  assert.equal(image.width,22);assert.equal(image.height,22);
  assert.equal(fallback.textContent,'GL');
  assert.equal(slots[1].querySelector('.development-app-initials').textContent,'VS');
  assert.equal(image.hidden,true);
  image.onload();assert.equal(image.hidden,false);assert.equal(fallback.hidden,true);
  image.onerror();assert.equal(slots[0].querySelector('img'),null);assert.equal(fallback.hidden,false);
  assert.equal(fallback.closest('[data-app-tooltip]'),group);
  assert.equal(view.root.querySelectorAll('.development-app-icon').length,icons.length,'failed images retain their fixed icon slots');
 }
});

test('concise hover targets cover only grouped capsules or single icons',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[
   {name:'GoLand',agents:['Claude Code','Codex'],windowCount:2,icon:appIcon},
   {name:'iTerm2',agents:['Codex'],windowCount:1},
   {name:'ChatGPT',icon:appIcon},
  ]};
  view.snapshot(state([item]));
  const indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.closest('[data-app-tooltip]'),undefined,'row whitespace and outer padding are not hover targets');
  assert.equal(indicator.title,undefined);
  const groups=indicator.querySelectorAll('[data-app-tooltip]');
  assert.deepEqual(groups.map(group=>group.dataset.appTooltip),['GoLand with Claude Code and Codex','iTerm2 with Codex']);
  for(const group of groups) {
   assert.equal(group.title,undefined);
   assert.equal(group.attrs['aria-label'],group.dataset.appTooltip);
   assert.equal(group.querySelector('[data-app-tooltip]'),null,'one tooltip target covers the entire app group');
   assert.doesNotMatch(group.dataset.appTooltip,/waiting|working|idle|blocked|window|folder/i);
  }
  assert.equal(groups[0].tag,'button');
  assert.equal(groups[0].classList.contains('has-agents'),true);
  for(const group of groups) for(const child of [...group.querySelectorAll('img'),...group.querySelectorAll('.development-app-initials')]) {
   assert.equal(child.closest('[data-app-tooltip]'),group,'all child icons use their owning app’s single hover target');
  }
  view.snapshot(state([{...item,developmentApps:[{name:'ChatGPT',icon:appIcon}]}]));
  const single=view.root.querySelector('.development-app-group');
  assert.equal(single.dataset.appTooltip,'ChatGPT');
  assert.equal(single.classList.contains('has-agents'),false);
  assert.equal(single.querySelector('.development-app-agents'),null);
  assert.equal(single.querySelector('img').closest('[data-app-tooltip]'),single);
  assert.equal(single.parent.closest('[data-app-tooltip]'),undefined);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('bundled agent icons preserve host association and order independently of installed standalone apps',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[
   {name:'GoLand',agents:['Claude Code','Codex'],icon:appIcon},
   {name:'VS Code',agents:['Codex','Claude Code']},
   {name:'iTerm2',agents:['Other Agent']},
  ]}]));
  const groups=view.root.querySelectorAll('.development-app-group');
  assert.equal(groups.length,2);
  assert.deepEqual(groups[0].querySelectorAll('img').map(img=>[img.src,img.width,img.height]),[[appIcon,22,22],['/icons/claude.png',16,16],['/icons/codex.png',16,16]]);
  assert.deepEqual(groups[1].querySelectorAll('img').map(img=>img.src),['/icons/codex.png','/icons/claude.png']);
  const agent=groups[0].querySelector('.development-agent-icon');
  const image=agent.querySelector('img'), fallback=agent.querySelector('.development-app-initials');
  assert.equal(fallback.textContent,'CC');
  image.onload();assert.equal(fallback.hidden,true);
  image.onerror();assert.equal(fallback.hidden,false);assert.equal(agent.querySelector('img'),null);
  assert.equal(fallback.closest('[data-app-tooltip]'),groups[0]);
  view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'iTerm2',agents:['Other Agent']}]}]));
  assert.equal(view.root.querySelector('.development-agent-icon').querySelector('.development-app-initials').textContent,'OA');
 }
});

test('unknown and empty app readings retain short text without stale icons or hover targets',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentApps:[{name:'GoLand',icon:appIcon}]};
  const show=value=>view.snapshot(state([value]));
  show({...item,developmentAppsKnown:true});
  show({...item,developmentAppsKnown:false});
  let indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.textContent,'App unknown');assert.equal(indicator.querySelector('img'),null);
  assert.equal(indicator.querySelector('[data-app-tooltip]'),null);assert.equal(indicator.closest('[data-app-tooltip]'),undefined);
  show({...item,developmentAppsKnown:true,developmentApps:[]});
  indicator=view.root.querySelector('.development-app');
  assert.equal(indicator.textContent,'No app detected');assert.equal(indicator.querySelector('img'),null);
  assert.equal(indicator.querySelector('[data-app-tooltip]'),null);assert.equal(indicator.closest('[data-app-tooltip]'),undefined);
 }
});

test('tooltip refresh follows rendered app content and visibility changes, without Git-poll noise',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],windowCount:1,icon:appIcon}]};
  const updates=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh');
  const initial=state([item]);
  view.snapshot(initial);
  assert.equal(updates().length,1,'initial render refreshes after reload');
  assert.equal(updates()[0][1],undefined,'the event carries no state or icon payload');
  view.snapshot(initial);
  view.snapshot(state([{...item,git:{...item.git,checked:'later',branch:'new-branch',changes:4}}]));
  view.snapshot(state([{...item,developmentApps:[{...item.developmentApps[0],icon:undefined}]}]));
  assert.equal(updates().length,1,'unchanged, Git-only, and fixed-size icon changes do not refresh');
  const changed={...item,developmentApps:[{...item.developmentApps[0],agents:['Codex'],windowCount:2}]};
  view.snapshot(state([changed]));
  assert.equal(updates().length,2);
  assert.equal(view.root.querySelector('[data-app-tooltip]').dataset.appTooltip,'GoLand with Codex');
  view.snapshot({...state([changed]),preferences:{showIDEStatus:false}});assert.equal(updates().length,3);
  assert.equal(view.root.querySelector('.development-app'),null);
  view.snapshot(state([changed]));assert.equal(updates().length,4);
  view.snapshot(state([{...changed,path:'/tmp/moved',position:{label:'Desktop 4'}}]));assert.equal(updates().length,5);
 }
});

test('confirmed waiting-only changes mark the relevant agents and refresh or clear stationary hover',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true};
  const host={name:'GoLand',agents:['Claude Code','Codex'],icon:appIcon};
  const show=waitingAgents=>view.snapshot(state([{...item,developmentApps:[{...host,waitingAgents}]}]));
  const group=()=>view.root.querySelector('.development-app-group');
  const refreshes=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
  show(undefined);
  assert.equal(group().classList.contains('needs-input'),false);
  assert.equal(group().querySelector('.development-agent-input'),null);
  const before=group();
  show(['Claude Code']);
  assert.notEqual(group(),before,'waiting-only metadata changes rebuild the indicator');
  assert.equal(refreshes(),2);
  assert.equal(group().classList.contains('needs-input'),true);
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex · Claude Code needs input');
  assert.equal(group().attrs['aria-label'],group().dataset.appTooltip);
  const agentIcons=group().querySelectorAll('.development-agent-icon');
  assert.equal(agentIcons[0].querySelector('.development-agent-input').textContent,'?');
  assert.equal(agentIcons[1].querySelector('.development-agent-input'),null);
  assert.equal(group().querySelector('.development-app-icon').querySelector('.development-agent-input'),null,'host icon has no question marker');
  const unchanged=group();
  show(['Claude Code']);assert.equal(group(),unchanged);assert.equal(refreshes(),2);
  show(['Claude Code','Codex']);
  assert.equal(group().querySelectorAll('.development-agent-input').length,2);
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex · Claude Code and Codex need input');
  show([]);
  assert.equal(refreshes(),4);
  assert.equal(group().classList.contains('needs-input'),false);
  assert.equal(group().querySelector('.development-agent-input'),null);
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex');
  show(['Codex']);
  assert.equal(group().querySelectorAll('.development-agent-icon')[0].querySelector('.development-agent-input'),null);
  assert.ok(group().querySelectorAll('.development-agent-icon')[1].querySelector('.development-agent-input'));
  show(undefined);
  assert.equal(refreshes(),6);
  assert.equal(group().classList.contains('needs-input'),false);
  assert.doesNotMatch(group().dataset.appTooltip,/input|active|idle|not waiting/i,'missing waiting metadata makes no activity claim');
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('confirmed working updates the correct agent, clears on missing status, and refreshes hover without attention',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true};
  const host={name:'GoLand',agents:['Claude Code','Codex'],icon:appIcon};
  const show=workingAgents=>view.snapshot(state([{...item,developmentApps:[{...host,workingAgents}]}]));
  const group=()=>view.root.querySelector('.development-app-group');
  const refreshes=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
  show(undefined);assert.equal(group().querySelector('.development-app-working-dots'),null);
  show(['Claude Code']);
  assert.equal(group().classList.contains('is-working'),true);
  assert.equal(group().querySelector('.development-app-working-dots').children.length,3);
  assert.equal(group().querySelector('.development-app-working-dots').textContent,'');
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex · Claude Code is working');
  assert.equal(group().attrs['aria-label'],group().dataset.appTooltip);
  const icons=group().querySelectorAll('.development-agent-icon');
  assert.equal(icons[0].querySelector('.development-agent-working').textContent,'▶');
  assert.equal(icons[1].querySelector('.development-agent-working'),null);
  assert.equal(refreshes(),2);
  const before=group();show(['Claude Code']);assert.equal(group(),before);assert.equal(refreshes(),2);
  show(['Claude Code','Codex']);
  assert.equal(group().querySelectorAll('.development-agent-working').length,2);
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex · Claude Code and Codex are working');
  assert.equal(group().classList.contains('waiting-attention'),false);
  assert.equal(view.root.querySelector('.card-waiting-glow'),null);
  const label=group().querySelector('.development-app-working-dots');
  assert.equal(label.closest('[data-app-tooltip]'),group());assert.equal(label.attrs['aria-hidden'],'true');
  assert.equal(label.onclick,undefined);assert.equal(label.tabIndex,undefined);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
  view.click(label);
  assert.deepEqual(view.calls.filter(([name])=>name==='devjay:activate'),[]);
  show(undefined);
  assert.equal(refreshes(),4);
  assert.equal(group().classList.contains('is-working'),false);
  assert.equal(group().querySelector('.development-agent-working'),null);
  assert.equal(group().querySelector('.development-app-working-dots'),null);
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex');
 }
});

test('waiting takes priority over working while mixed agents retain individual state and pulse history',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), host={name:'GoLand',agents:['Claude Code','Codex']};
  const show=(workingAgents,waitingAgents)=>view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[{...host,workingAgents,waitingAgents}]}]));
  const group=()=>view.root.querySelector('.development-app-group');
  show(['Claude Code','Codex'],[]);assert.equal(view.root.querySelector('.card-waiting-glow'),null);
  show(['Claude Code','Codex'],['Claude Code']);
  assert.equal(group().classList.contains('needs-input'),true);assert.equal(group().classList.contains('is-working'),false);
  assert.equal(group().querySelector('.development-app-working-dots'),null,'mixed state must not show whole-group working dots');
  assert.equal(group().querySelectorAll('.development-agent-input').length,1);
  assert.equal(group().querySelectorAll('.development-agent-working').length,1);
  assert.equal(group().dataset.appTooltip,'GoLand with Claude Code and Codex · Claude Code needs input · Codex is working');
  assert.equal(group().style.animationDelay,'0ms');
  view.advance(1200);show([],['Claude Code']);
  assert.equal(group().style.animationDelay,'-1200ms','working changes do not restart waiting attention');
  show(['Claude Code'],[]);
  assert.equal(group().classList.contains('is-working'),true);assert.equal(group().querySelector('.development-agent-input'),null);
  assert.equal(view.root.querySelector('.card-waiting-glow'),null);
  show([],['Claude Code']);assert.equal(group().style.animationDelay,'0ms','a new wait after work gets attention');
 }
});

test('working never leaks through absent agents, unknown observations, or disabled app status',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true};
  for(const host of [
   {name:'GoLand',agents:['Claude Code'],workingAgents:['Codex','Other Agent']},
   {name:'iTerm2',agents:['Other Agent'],workingAgents:['Other Agent']},
   {name:'ChatGPT',workingAgents:['Codex']},
  ]) {
   view.snapshot(state([{...item,developmentApps:[host]}]));
   assert.equal(view.root.querySelector('.development-agent-working'),null);
   assert.equal(view.root.querySelector('.development-app-working-dots'),null);
   assert.doesNotMatch(appLabels(view).join(' '),/working/);
  }
  const observed={...item,developmentApps:[{name:'GoLand',agents:['Codex'],workingAgents:['Codex']}]};
  view.snapshot(state([observed]));assert.ok(view.root.querySelector('.development-agent-working'));
  view.snapshot(state([{...observed,developmentAppsKnown:false}]));assert.equal(view.root.querySelector('.development-agent-working'),null);
  view.snapshot(state([observed]));
  view.snapshot({...state([observed]),preferences:{showIDEStatus:false}});assert.equal(view.root.querySelector('.development-app'),null);
 }
});

test('unknown or stale waiting names never highlight an app or an absent agent',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  for(const host of [
   {name:'GoLand',agents:['Claude Code'],waitingAgents:['Codex','Other Agent']},
   {name:'iTerm2',agents:['Other Agent'],waitingAgents:['Other Agent']},
   {name:'ChatGPT',waitingAgents:['Codex']},
  ]) {
   view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[host]}]));
   const group=view.root.querySelector('.development-app-group');
   assert.equal(group.classList.contains('needs-input'),false);
   assert.equal(group.querySelector('.development-agent-input'),null);
   assert.doesNotMatch(group.dataset.appTooltip,/needs? input/);
  }
  view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code'],waitingAgents:['Claude Code','Codex','Other Agent']}]}]));
  const group=view.root.querySelector('.development-app-group');
  assert.equal(group.dataset.appTooltip,'GoLand with Claude Code · Claude Code needs input');
  assert.equal(group.querySelectorAll('.development-agent-input').length,1);
 }
});

test('waiting markers remain decorative within the host activation button',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  const snapshot=state([{...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand',agents:['Claude Code','Codex'],waitingAgents:['Claude Code','Codex']}]}]);
  view.snapshot(snapshot);
  const group=view.root.querySelector('.development-app-group');
  assert.equal(view.root.querySelectorAll('[data-app-tooltip]').length,1);
  assert.equal(group.parent.closest('[data-app-tooltip]'),undefined);
  for(const marker of group.querySelectorAll('.development-agent-input')) {
   assert.equal(marker.parent.classList.contains('development-agent-icon'),true);
   assert.equal(marker.closest('[data-app-tooltip]'),group);
   assert.equal(marker.attrs['aria-hidden'],'true');
   assert.equal(marker.onclick,undefined);assert.equal(marker.tabIndex,undefined);
   view.click(marker);
  }
  assert.deepEqual(view.calls.filter(([name])=>name==='devjay:activate'),[]);
  assert.equal(view.calls.some(([name])=>name==='devjay:viewer-open'),false);
  view.snapshot({...snapshot,preferences:{showIDEStatus:false}});
  assert.equal(view.root.querySelector('.development-agent-input'),null,'turning status off hides waiting presentation too');
 }
});

test('new waiting pulses once and rebuilt cards preserve the original four-second phase and deadline',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), host={name:'GoLand',bundleID:'goland',agents:['Claude Code','Codex']};
  const item={...site('a'),developmentAppsKnown:true,developmentApps:[host]};
  const show=(waitingAgents,branch='main')=>view.snapshot(state([{...item,git:{branch},developmentApps:[{...host,waitingAgents}]}]));
  const group=()=>view.root.querySelector('.development-app-group');
  show([]);assert.equal(group().classList.contains('waiting-attention'),false);
  show(['Claude Code']);
  assert.equal(group().classList.contains('waiting-attention'),true);
  assert.equal(group().style.animationDelay,'0ms');
  assert.equal(view.root.querySelector('.card-waiting-glow').style.animationDelay,'0ms');
  const first=group();
  view.advance(1250);show(['Claude Code']);
  assert.equal(group(),first,'ordinary unchanged snapshots retain the running animation');
  show(['Claude Code'],'git-changed');
  assert.notEqual(group(),first);
  assert.equal(group().style.animationDelay,'-1250ms','a Git rebuild resumes the elapsed phase');
  assert.equal(view.root.querySelector('.card-waiting-glow').style.animationDelay,'-1250ms','the card glow shares the capsule phase');
  for(let i=1;i<=5;i++) {
   view.advance(500);show(['Claude Code'],`branch-${i}`);
   assert.equal(group().style.animationDelay,`${-(1250+i*500)}ms`);
  }
  view.advance(250);show(['Claude Code'],'deadline');
  assert.equal(group().classList.contains('waiting-attention'),false,'renders cannot extend the four-second deadline');
  assert.equal(view.root.querySelector('.card-waiting-glow'),null,'rebuilt cards do not retain an expired glow');
  assert.equal(group().classList.contains('needs-input'),true,'waiting stays amber after attention ends');
  assert.match(group().dataset.appTooltip,/Claude Code needs input/);
  view.advance(30000);show(['Claude Code'],'much-later');
  assert.equal(group().classList.contains('waiting-attention'),false,'long-lived waiting does not repeat');
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('each newly waiting agent can request attention without replaying existing waiting agents',()=>{
 const view=app(), host={name:'GoLand',bundleID:'goland',agents:['Claude Code','Codex']};
 const show=waitingAgents=>view.snapshot(state([{...site('a'),developmentAppsKnown:true,developmentApps:[{...host,waitingAgents}]}]));
 const group=()=>view.root.querySelector('.development-app-group');
 show(['Claude Code']);view.advance(5000);
 show(['Claude Code','Codex']);
 assert.equal(group().classList.contains('waiting-attention'),true);
 assert.equal(group().style.animationDelay,'0ms','new Codex waiting starts a new pulse for this capsule');
 view.advance(1000);show(['Claude Code']);
 assert.equal(group().classList.contains('waiting-attention'),false,'clearing the new agent does not replay the older waiting agent');
 show(['Claude Code','Codex']);
 assert.equal(group().style.animationDelay,'0ms','Codex waiting again is a new transition');
 view.advance(700);show(['Claude Code','Codex','Unknown Agent']);
 assert.equal(group().style.animationDelay,'-700ms','unsupported waiting metadata does not trigger attention');
});

test('removal, unknown observation, and disabling status clear waiting transition history',()=>{
 for(const search of ['', '?site=a']) {
  for(const clear of ['waiting','agent','app','site','unknown','disabled']) {
   const view=app(search), host={name:'GoLand',bundleID:'goland',agents:['Claude Code'],waitingAgents:['Claude Code']};
   const item={...site('a'),developmentAppsKnown:true,developmentApps:[host]}, initial=state([item]);
   const group=()=>view.root.querySelector('.development-app-group');
   view.snapshot(initial);view.advance(5000);
   const cleared=clear==='disabled'?{...initial,preferences:{showIDEStatus:false}}:
    clear==='site'?state([]):state([{...item,...(clear==='unknown'?{developmentAppsKnown:false}:{developmentApps:clear==='app'?[]:[{...host,...(clear==='agent'?{agents:[]}:{waitingAgents:[]})}]})}]);
   view.snapshot(cleared);view.advance(100);view.snapshot(initial);
   assert.equal(group().classList.contains('waiting-attention'),true,`${search} ${clear}: returning waiting is new`);
   assert.equal(group().style.animationDelay,'0ms');
  }
 }
});

test('a different Site or app does not restart another capsule and deferred drag renders retain elapsed attention',()=>{
 const view=app(), host={name:'GoLand',bundleID:'goland',agents:['Claude Code'],waitingAgents:['Claude Code']};
 const a={...site('a'),developmentAppsKnown:true,developmentApps:[host]}, b={...site('b'),developmentAppsKnown:true,developmentApps:[host]};
 view.snapshot(state([a]));view.advance(1700);view.snapshot(state([a,b]));
 let groups=view.root.querySelectorAll('.development-app-group');
 assert.equal(groups[0].style.animationDelay,'0ms','unchanged existing DOM keeps its original animation');
 assert.equal(groups[1].style.animationDelay,'0ms','the new Site starts independently');
 const terminal={name:'iTerm2',bundleID:'iterm',agents:['Codex'],waitingAgents:['Codex']};
 view.advance(500);view.snapshot(state([{...a,developmentApps:[host,terminal]},b]));
 groups=view.root.querySelectorAll('.development-app-group');
 assert.equal(groups[0].style.animationDelay,'-2200ms');
 assert.equal(groups[1].style.animationDelay,'0ms','the new host starts independently');
 view.drag.busy(true);
 view.snapshot(state([{...a,developmentApps:[{...host,waitingAgents:[]}]}]));
 view.snapshot(state([a]));
 view.advance(900);view.drag.busy(false);
 const current=view.root.querySelector('.development-app-group');
 assert.notEqual(current,groups[0],'a clear/resume transition during drag rebuilds even when the final metadata matches');
 assert.equal(current.style.animationDelay,'-900ms','the deferred render does not start a fresh four seconds');
});

test('attention CSS is finite, keeps the capsule geometry stable, and disables motion when requested',async()=>{
 const css=await readFile(new URL('../cmd/devjay/frontend/style.css',import.meta.url),'utf8');
 assert.match(css,/\.development-app-group\.waiting-attention\s*\{\s*animation:\s*development-app-attention 4s ease-in-out;\s*\}/);
 const frames=css.match(/@keyframes development-app-attention\s*\{([\s\S]*?)\n\}/)?.[1];
 assert.ok(frames);
 assert.match(frames,/0%, 50%, 100%/);
 assert.doesNotMatch(frames,/transform|scale|width|height|padding|margin|opacity/,'attention only adjusts color and an inset highlight');
 assert.match(css,/@media \(prefers-reduced-motion: reduce\)\s*\{\s*\.development-app-group\.waiting-attention\s*\{\s*animation: none;/);
 assert.match(css,/\.card-waiting-glow\s*\{[^}]*position: absolute;[^}]*pointer-events: none;[^}]*inset: 2px;[^}]*overflow: hidden;[^}]*animation: card-waiting-attention 4s ease-in-out;/);
 assert.match(css,/\.card-waiting-glow::before\s*\{[^}]*radial-gradient\([^;]*;[^}]*animation: card-waiting-radiance 4s ease-in-out; animation-delay: inherit;/);
 assert.match(css,/@media \(prefers-reduced-motion: reduce\)\s*\{[^}]*\}[^}]*\.card-waiting-glow, \.card-waiting-glow::before\s*\{\s*animation: none;/);
});

test('the radiating glow is one decorative card child and clears with waiting or visibility',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search), host={name:'GoLand',agents:['Claude Code','Codex'],waitingAgents:['Claude Code','Codex']};
  const item={...site('a'),developmentAppsKnown:true,developmentApps:[host]};
  view.snapshot(state([item]));
  const glow=view.root.querySelector('.card-waiting-glow');
  assert.ok(glow);
  assert.equal(view.root.querySelectorAll('.card-waiting-glow').length,1,'both waiting agents share one card glow');
  assert.equal(glow.parent.firstElementChild,glow,'the glow is independent of the capsule hit target');
  assert.equal(glow.attrs['aria-hidden'],'true');
  assert.equal(glow.dataset.appTooltip,undefined);
  assert.equal(glow.title,undefined);assert.equal(glow.onclick,undefined);assert.equal(glow.tabIndex,undefined);
  assert.equal(glow.textContent,'');
  assert.equal(view.root.querySelectorAll('[data-app-tooltip]').length,1,'the glow creates no new hover area');
  assert.equal(view.root.querySelector('.development-app').parent.children.at(-1).className,'development-app');
  view.snapshot(state([{...item,developmentApps:[{...host,waitingAgents:[]}]}]));
  assert.equal(view.root.querySelector('.card-waiting-glow'),null);
  view.snapshot(state([item]));
  view.snapshot({...state([item]),preferences:{showIDEStatus:false}});
  assert.equal(view.root.querySelector('.card-waiting-glow'),null);
  assert.equal(view.calls.some(([name])=>name==='devjay:activate'),false);
 }
});

test('tooltip refresh includes card placement and order, and defers until the final panel-drag render',()=>{
 const view=app(), a={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand'}]}, b=site('b');
 const count=()=>view.calls.filter(([name])=>name==='devjay:app-tooltip-refresh').length;
 view.snapshot(state([a,b]));assert.equal(count(),1);
 view.snapshot(state([b,a]));assert.equal(count(),2);
 const custom={...state([b,a]),layout:'custom',placements:{a:{x:1,y:2,z:1},b:{x:3,y:4,z:2}}};
 view.snapshot(custom);assert.equal(count(),3);
 view.snapshot({...custom,placements:{...custom.placements,a:{x:40,y:50,z:3}}});assert.equal(count(),4);
 view.drag.busy(true);
 view.snapshot({...custom,sites:[b,{...a,developmentApps:[{name:'GoLand',agents:['Codex']}]}]});
 assert.equal(count(),4,'deferred snapshots do not refresh against stale DOM');
 view.drag.busy(false);assert.equal(count(),5);
 assert.deepEqual(appLabels(view),['GoLand with Codex']);
 view.snapshot(state([]));assert.equal(count(),6,'removing the last card dismisses its tooltip');
 assert.equal(view.root.querySelector('.development-app'),null);
});

test('settings, viewer, and action snapshots never request app tooltip refresh',()=>{
 for(const search of ['?settings','?viewer=viewer-1','?actions']) {
  const view=app(search), item={...site('a'),developmentAppsKnown:true,developmentApps:[{name:'GoLand'}]};
  view.snapshot(state([item]));
  view.snapshot(state([{...item,developmentApps:[{name:'GoLand',agents:['Codex']}]}]));
  view.snapshot({...state([item]),preferences:{showIDEStatus:false}});
  assert.equal(view.calls.some(([name])=>name==='devjay:app-tooltip-refresh'),false,search);
 }
});

test('approved and merged card PRs are green unless CI needs attention, and drafts remain neutral',()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search);
  for(const [stateName,isDraft,ciState,tone] of [
   ['Approved',false,'success','pr-approved'],['Approved',false,'pending','pr-approved'],
   ['Approved',false,'failure','ci-failed'],['Approved',false,'action_required','ci-failed'],
   ['Merged',false,undefined,'pr-merged'],['Merged',false,'success','pr-merged'],['Merged',false,'failure','ci-failed'],
   ['Approved',true,'success',''],['Review required',false,'success',''],
  ]) {
   view.snapshot(state([{...site('a'),git:{branch:'feature',pullRequests:[{number:42,state:stateName,isDraft,ciState}]}}]));
   const tag=view.root.querySelector('.pr-tag');
   assert.equal(tag.classList.contains('pr-approved'),tone==='pr-approved');
   assert.equal(tag.classList.contains('pr-merged'),tone==='pr-merged');
   assert.equal(tag.classList.contains('ci-failed'),tone==='ci-failed');
  }
 }
});

test('merged cards explicitly say PR merged and preserve browser click and menu targets',async()=>{
 for(const search of ['', '?site=a']) {
  const view=app(search),item=site('a','feature');
  item.git={...item.git,summary:'Remote branch removed',tone:'warning',head:'head',defaultBranch:'develop',
   pullRequests:[{number:42,state:'Merged'}],mergedPR:{number:42,head:'head',sameHead:true,base:'develop'}};
  view.snapshot(state([item]));
  const row=view.root.querySelector('.pr-status');
  assert.equal(row.textContent,'✓PR merged');
  assert.equal(cardMenuTarget(row.children[1]).number,42);
  await row.onclick();
  assert.deepEqual(JSON.parse(JSON.stringify(view.calls.filter(([name])=>name==='devjay:pr-open'))),[['devjay:pr-open',{siteID:'a',number:42,anchor:{x:10,y:20,width:270,height:140}}]]);
  assert.equal(view.calls.some(([name])=>name==='devjay:action-prepare'||name==='devjay:action-confirm'),false);
 }
});


test('Settings describes global shortcuts and clears registration errors after recovery', () => {
 const view=app('?settings');
 view.snapshot({...state([]),shortcutError:'Global shortcuts unavailable: ⌃⌥⌘M.'});
 const error=view.root.querySelectorAll('.help').find(n=>n.classList.contains('error'));
 assert.ok(view.root.textContent.includes('⌃⌥⌘1–9'));
 assert.equal(error.textContent,'Global shortcuts unavailable: ⌃⌥⌘M.');
 assert.equal(error.hidden,false);
 view.snapshot(state([]));
 assert.equal(error.hidden,true);
 assert.equal(error.textContent,'');
});
